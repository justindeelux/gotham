package terminal

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"golang.org/x/net/websocket"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"

	"github.com/justindeelux/gotham/agent"
	"github.com/justindeelux/gotham/internal/auth"
	"github.com/justindeelux/gotham/internal/servers"
	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
)

type fakeVerifier struct {
	userID uuid.UUID
	token  string
}

func (f *fakeVerifier) VerifyAccessToken(token string) (*auth.Claims, error) {
	if token != f.token {
		return nil, errors.New("invalid token")
	}
	return &auth.Claims{
		Role:             "admin",
		RegisteredClaims: jwt.RegisteredClaims{Subject: f.userID.String()},
	}, nil
}

type fixedRoots struct{ dir string }

func (f fixedRoots) ResolveShellDir(_, _ string) (string, error) { return f.dir, nil }

type bufTerminalClient struct {
	agentv1.TerminalServiceClient
	conn *grpc.ClientConn
}

func (c *bufTerminalClient) Close() error { return c.conn.Close() }

// failingClient stands in for an agent that predates TerminalService.
type failingClient struct{ err error }

func (c failingClient) OpenTerminal(context.Context, ...grpc.CallOption) (agentv1.TerminalService_OpenTerminalClient, error) {
	return nil, c.err
}

func (c failingClient) Close() error { return nil }

func newBufClient(t *testing.T, service agentv1.TerminalServiceServer) *bufTerminalClient {
	t.Helper()
	listener := bufconn.Listen(1 << 20)
	server := grpc.NewServer()
	agentv1.RegisterTerminalServiceServer(server, service)
	go func() { _ = server.Serve(listener) }()
	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
			return listener.Dial()
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("dial bufconn: %v", err)
	}
	t.Cleanup(func() {
		_ = conn.Close()
		server.Stop()
		_ = listener.Close()
	})
	return &bufTerminalClient{TerminalServiceClient: agentv1.NewTerminalServiceClient(conn), conn: conn}
}

func shellPath(t *testing.T) string {
	t.Helper()
	for _, candidate := range []string{"/bin/sh", "/bin/bash", "/bin/dash"} {
		if info, err := os.Stat(candidate); err == nil && info.Mode().IsRegular() {
			return candidate
		}
	}
	t.Skip("no POSIX shell installed")
	return ""
}

type fixtureOptions struct {
	dial        DialFunc
	operator    bool
	pendingTTL  time.Duration
	maxDuration time.Duration
}

type fixture struct {
	svc      *Service
	server   *httptest.Server
	verifier *fakeVerifier
	userID   uuid.UUID
	serverID uuid.UUID
}

func newFixture(t *testing.T, options fixtureOptions) *fixture {
	t.Helper()
	userID := uuid.New()
	serverID := uuid.New()
	if options.dial == nil {
		terminalServer := agent.NewTerminalServer(agent.TerminalConfig{
			Logger: discardLogger(),
			Roots:  fixedRoots{dir: t.TempDir()},
			Shell:  shellPath(t),
		})
		options.dial = func(context.Context, *servers.Server) (TerminalClient, error) {
			return newBufClient(t, terminalServer), nil
		}
	}
	svc := NewService(Config{
		Registry:    &fakeRegistry{server: &servers.Server{ID: serverID}},
		Dial:        options.dial,
		Logger:      discardLogger(),
		PendingTTL:  options.pendingTTL,
		MaxDuration: options.maxDuration,
	})
	verifier := &fakeVerifier{userID: userID, token: "good-token"}
	router := chi.NewRouter()
	Mount(router,
		func(next http.Handler) http.Handler { return next },
		svc, verifier,
		func(context.Context, *auth.Claims) bool { return options.operator },
		func(context.Context) (uuid.UUID, bool) { return userID, true },
		discardLogger(),
	)
	server := httptest.NewServer(router)
	t.Cleanup(server.Close)
	return &fixture{svc: svc, server: server, verifier: verifier, userID: userID, serverID: serverID}
}

func (f *fixture) create(t *testing.T, req CreateRequest) *Session {
	t.Helper()
	session, err := f.svc.Create(context.Background(), f.userID, f.serverID, req)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	return session
}

func (f *fixture) dial(t *testing.T, session *Session, origin string) (*websocket.Conn, error) {
	t.Helper()
	url := "ws" + strings.TrimPrefix(f.server.URL, "http") +
		"/v1/servers/" + session.ServerID.String() + "/terminal/sessions/" + session.ID.String() + "/ws"
	config, err := websocket.NewConfig(url, origin)
	if err != nil {
		t.Fatalf("websocket config: %v", err)
	}
	config.Protocol = []string{Subprotocol, TokenProtocolPrefix + "good-token"}
	return websocket.DialConfig(config)
}

func readFrame(t *testing.T, conn *websocket.Conn) (byte, []byte) {
	t.Helper()
	if err := conn.SetReadDeadline(time.Now().Add(10 * time.Second)); err != nil {
		t.Fatalf("read deadline: %v", err)
	}
	var payload []byte
	if err := websocket.Message.Receive(conn, &payload); err != nil {
		t.Fatalf("receive frame: %v", err)
	}
	if len(payload) == 0 {
		t.Fatal("empty frame")
	}
	return payload[0], payload[1:]
}

func sendFrameTest(t *testing.T, conn *websocket.Conn, opcode byte, payload []byte) {
	t.Helper()
	frame := append([]byte{opcode}, payload...)
	if err := websocket.Message.Send(conn, frame); err != nil {
		t.Fatalf("send frame: %v", err)
	}
}

func readUntilOutput(t *testing.T, conn *websocket.Conn, marker string) {
	t.Helper()
	var output strings.Builder
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		opcode, payload := readFrame(t, conn)
		switch opcode {
		case opOutput:
			output.Write(payload)
			if strings.Contains(output.String(), marker) {
				return
			}
		case opExited:
			t.Fatalf("shell exited before %q: %s", marker, payload)
		case opError:
			t.Fatalf("terminal error before %q: %s", marker, payload)
		}
	}
	t.Fatalf("timed out waiting for %q; output %q", marker, output.String())
}

func TestCreateAndCloseRoutes(t *testing.T) {
	t.Parallel()
	f := newFixture(t, fixtureOptions{operator: true})
	body := `{"target":"host","root":"volumes","cwd":"app","cols":120,"rows":32}`
	response, err := http.Post(f.server.URL+"/v1/servers/"+f.serverID.String()+"/terminal/sessions",
		"application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("POST status = %d, want 201", response.StatusCode)
	}
	var created createResponse
	if err := json.NewDecoder(response.Body).Decode(&created); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if created.ID == "" || created.Subprotocol != Subprotocol {
		t.Fatalf("unexpected create response: %+v", created)
	}

	request, _ := http.NewRequest(http.MethodDelete,
		f.server.URL+"/v1/servers/"+f.serverID.String()+"/terminal/sessions/"+created.ID, nil)
	deleted, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("DELETE: %v", err)
	}
	defer func() { _ = deleted.Body.Close() }()
	if deleted.StatusCode != http.StatusNoContent {
		t.Fatalf("DELETE status = %d, want 204", deleted.StatusCode)
	}
}

func TestCreateRejectsInvalidInput(t *testing.T) {
	t.Parallel()
	f := newFixture(t, fixtureOptions{operator: true})
	cases := []struct {
		body string
		want int
	}{
		{`{"target":"host","root":"volumes","cols":0,"rows":24}`, http.StatusBadRequest},
		{`{"target":"container","root":"volumes","cols":80,"rows":24}`, http.StatusNotImplemented},
		{`{"target":"host","root":"../etc","cols":80,"rows":24}`, http.StatusBadRequest},
		{`{"target":"host","root":"volumes","unknown":true}`, http.StatusBadRequest},
	}
	for _, testCase := range cases {
		response, err := http.Post(f.server.URL+"/v1/servers/"+f.serverID.String()+"/terminal/sessions",
			"application/json", strings.NewReader(testCase.body))
		if err != nil {
			t.Fatalf("POST: %v", err)
		}
		_ = response.Body.Close()
		if response.StatusCode != testCase.want {
			t.Fatalf("POST %s status = %d, want %d", testCase.body, response.StatusCode, testCase.want)
		}
	}
}

func TestAttachPreflight(t *testing.T) {
	t.Parallel()
	f := newFixture(t, fixtureOptions{operator: true})
	session := f.create(t, CreateRequest{Target: "host", Root: "volumes", Cols: 80, Rows: 24})
	path := f.server.URL + "/v1/servers/" + session.ServerID.String() + "/terminal/sessions/" + session.ID.String() + "/ws"

	get := func(mutate func(*http.Request)) int {
		request, _ := http.NewRequest(http.MethodGet, path, nil)
		request.Header.Set("Authorization", "Bearer good-token")
		mutate(request)
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			t.Fatalf("GET: %v", err)
		}
		_ = response.Body.Close()
		return response.StatusCode
	}

	if got := get(func(r *http.Request) { r.Header.Set("Origin", "https://evil.example") }); got != http.StatusForbidden {
		t.Fatalf("bad origin status = %d, want 403", got)
	}
	if got := get(func(r *http.Request) { r.Header.Del("Authorization") }); got != http.StatusUnauthorized {
		t.Fatalf("missing token status = %d, want 401", got)
	}
	if got := get(func(r *http.Request) { r.Header.Set("Authorization", "Bearer wrong") }); got != http.StatusUnauthorized {
		t.Fatalf("bad token status = %d, want 401", got)
	}

	otherUser := uuid.New()
	f.verifier.userID = otherUser
	if got := get(func(*http.Request) {}); got != http.StatusNotFound {
		t.Fatalf("foreign owner status = %d, want 404", got)
	}
	f.verifier.userID = f.userID

	fNoOperator := newFixture(t, fixtureOptions{operator: false})
	sessionNoOperator := fNoOperator.create(t, CreateRequest{Target: "host", Root: "volumes", Cols: 80, Rows: 24})
	noOperatorPath := fNoOperator.server.URL + "/v1/servers/" + sessionNoOperator.ServerID.String() +
		"/terminal/sessions/" + sessionNoOperator.ID.String() + "/ws"
	noOperatorRequest, _ := http.NewRequest(http.MethodGet, noOperatorPath, nil)
	noOperatorRequest.Header.Set("Authorization", "Bearer good-token")
	noOperatorResponse, err := http.DefaultClient.Do(noOperatorRequest)
	if err != nil {
		t.Fatalf("non-operator GET: %v", err)
	}
	_ = noOperatorResponse.Body.Close()
	if noOperatorResponse.StatusCode != http.StatusForbidden {
		t.Fatalf("non-operator attach status = %d, want 403", noOperatorResponse.StatusCode)
	}
}

func TestAttachIsSingleUse(t *testing.T) {
	t.Parallel()
	f := newFixture(t, fixtureOptions{operator: true})
	session := f.create(t, CreateRequest{Target: "host", Root: "volumes", Cols: 80, Rows: 24})
	conn, err := f.dial(t, session, f.server.URL)
	if err != nil {
		t.Fatalf("first attach: %v", err)
	}
	defer func() { _ = conn.Close() }()
	if opcode, _ := readFrame(t, conn); opcode != opOpened {
		t.Fatalf("first frame opcode = %#x, want opened", opcode)
	}

	path := f.server.URL + "/v1/servers/" + session.ServerID.String() + "/terminal/sessions/" + session.ID.String() + "/ws"
	request, _ := http.NewRequest(http.MethodGet, path, nil)
	request.Header.Set("Authorization", "Bearer good-token")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("second attach: %v", err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusConflict {
		t.Fatalf("second attach status = %d, want 409", response.StatusCode)
	}
}

func TestRelayEndToEnd(t *testing.T) {
	f := newFixture(t, fixtureOptions{operator: true})
	session := f.create(t, CreateRequest{Target: "host", Root: "volumes", Cols: 80, Rows: 24})
	conn, err := f.dial(t, session, f.server.URL)
	if err != nil {
		t.Fatalf("attach: %v", err)
	}
	defer func() { _ = conn.Close() }()

	opcode, payload := readFrame(t, conn)
	if opcode != opOpened {
		t.Fatalf("first frame opcode = %#x, want opened", opcode)
	}
	var opened openedEvent
	if err := json.Unmarshal(payload, &opened); err != nil {
		t.Fatalf("decode opened: %v", err)
	}
	if opened.Cols != 80 || opened.Rows != 24 {
		t.Fatalf("opened dimensions = %dx%d, want 80x24", opened.Cols, opened.Rows)
	}

	sendFrameTest(t, conn, opInput, []byte("echo gotham-relay-ok\r"))
	readUntilOutput(t, conn, "gotham-relay-ok")

	resize := make([]byte, 4)
	binary.BigEndian.PutUint16(resize[0:2], 100)
	binary.BigEndian.PutUint16(resize[2:4], 40)
	sendFrameTest(t, conn, opResize, resize)
	sendFrameTest(t, conn, opInput, []byte("echo gotham-resize-ok\r"))
	readUntilOutput(t, conn, "gotham-resize-ok")

	sendFrameTest(t, conn, opInput, []byte("exit 3\r"))
	for {
		opcode, payload = readFrame(t, conn)
		if opcode == opExited {
			var exited exitedEvent
			if err := json.Unmarshal(payload, &exited); err != nil {
				t.Fatalf("decode exited: %v", err)
			}
			if exited.ExitCode != 3 {
				t.Fatalf("exit code = %d, want 3", exited.ExitCode)
			}
			break
		}
	}
	select {
	case <-session.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("session was not released after the shell exited")
	}
	if _, err := f.svc.Attach(context.Background(), f.userID, f.serverID, session.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("attach after exit = %v, want ErrNotFound", err)
	}
}

func TestMalformedResizeEndsStream(t *testing.T) {
	f := newFixture(t, fixtureOptions{operator: true})
	session := f.create(t, CreateRequest{Target: "host", Root: "volumes", Cols: 80, Rows: 24})
	conn, err := f.dial(t, session, f.server.URL)
	if err != nil {
		t.Fatalf("attach: %v", err)
	}
	defer func() { _ = conn.Close() }()
	if opcode, _ := readFrame(t, conn); opcode != opOpened {
		t.Fatalf("first frame opcode = %#x, want opened", opcode)
	}
	sendFrameTest(t, conn, opResize, []byte{0x00})
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	for {
		var payload []byte
		err := websocket.Message.Receive(conn, &payload)
		if err == nil {
			// Prompt or echo output may already be buffered; keep reading until
			// the server closes the stream.
			continue
		}
		var netErr net.Error
		if errors.As(err, &netErr) && netErr.Timeout() {
			t.Fatal("stream stayed open after a malformed resize frame")
		}
		return
	}
}

func TestUnsupportedAgentEndsWithError(t *testing.T) {
	f := newFixture(t, fixtureOptions{
		operator: true,
		dial: func(context.Context, *servers.Server) (TerminalClient, error) {
			return failingClient{err: status.Error(codes.Unimplemented, "unknown service")}, nil
		},
	})
	session := f.create(t, CreateRequest{Target: "host", Root: "volumes", Cols: 80, Rows: 24})
	conn, err := f.dial(t, session, f.server.URL)
	if err != nil {
		t.Fatalf("attach: %v", err)
	}
	defer func() { _ = conn.Close() }()
	opcode, payload := readFrame(t, conn)
	if opcode != opError {
		t.Fatalf("opcode = %#x, want error", opcode)
	}
	if !strings.Contains(string(payload), "does not support") {
		t.Fatalf("error frame = %q, want unsupported message", payload)
	}
}
