package terminal

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
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
	valid  *atomic.Bool
}

func (f *fakeVerifier) VerifyAccessToken(token string) (*auth.Claims, error) {
	if token != f.token {
		return nil, errors.New("invalid token")
	}
	if f.valid != nil && !f.valid.Load() {
		return nil, errors.New("token expired")
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

// sharedBufClient lets several sessions reuse one test connection. Closing a
// session must not tear down the connection other sessions use.
type sharedBufClient struct{ *bufTerminalClient }

func (sharedBufClient) Close() error { return nil }

// fakeAuthorizer is a controllable terminal.Authorizer for relay tests.
type fakeAuthorizer struct {
	mu           sync.Mutex
	allow        bool
	marker       Marker
	err          error
	target       Target
	revalidateFn func(context.Context) (bool, error)
}

func (f *fakeAuthorizer) Authorize(_ context.Context, _ *auth.Claims, target Target) (Marker, error) {
	f.mu.Lock()
	allow, err, marker := f.allow, f.err, f.marker
	f.target = target
	f.mu.Unlock()
	if err != nil {
		return Marker{}, err
	}
	if !allow {
		return Marker{}, errors.New("not a platform operator")
	}
	return marker, nil
}

func (f *fakeAuthorizer) Revalidate(ctx context.Context, _ *auth.Claims, _ Marker, target Target) (bool, error) {
	f.mu.Lock()
	allow, err, hook := f.allow, f.err, f.revalidateFn
	f.target = target
	f.mu.Unlock()
	if hook != nil {
		return hook(ctx)
	}
	if err != nil {
		return false, err
	}
	return allow, nil
}

func (f *fakeAuthorizer) setAllow(allow bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.allow = allow
}

func (f *fakeAuthorizer) setErr(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.err = err
}

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
	dial         DialFunc
	operator     bool
	revalidateFn func(context.Context) (bool, error)
	pendingTTL   time.Duration
	maxDuration  time.Duration
	revalidate   time.Duration
	ping         time.Duration
	idle         time.Duration
	writeTimeout time.Duration
	applications ApplicationTargets
	containers   agent.ContainerExec
}

type fixture struct {
	svc        *Service
	server     *httptest.Server
	verifier   *fakeVerifier
	userID     uuid.UUID
	serverID   uuid.UUID
	authorizer *fakeAuthorizer
}

func newFixture(t *testing.T, options fixtureOptions) *fixture {
	t.Helper()
	userID := uuid.New()
	serverID := uuid.New()
	if options.dial == nil {
		terminalServer := agent.NewTerminalServer(agent.TerminalConfig{
			Logger:     discardLogger(),
			Roots:      fixedRoots{dir: t.TempDir()},
			Shell:      shellPath(t),
			Containers: options.containers,
		})
		shared := newBufClient(t, terminalServer)
		options.dial = func(context.Context, *servers.Server) (TerminalClient, error) {
			return sharedBufClient{shared}, nil
		}
	}
	svc := NewService(Config{
		Registry:     &fakeRegistry{server: &servers.Server{ID: serverID}},
		Dial:         options.dial,
		Applications: options.applications,
		Logger:       discardLogger(),
		PendingTTL:   options.pendingTTL,
		MaxDuration:  options.maxDuration,
	})
	verifier := &fakeVerifier{userID: userID, token: "good-token"}
	authorizer := &fakeAuthorizer{
		allow:        options.operator,
		marker:       Marker{SignInAt: time.Unix(1, 0)},
		revalidateFn: options.revalidateFn,
	}
	f := &fixture{svc: svc, verifier: verifier, userID: userID, serverID: serverID, authorizer: authorizer}
	router := chi.NewRouter()
	userIDFrom := func(context.Context) (uuid.UUID, bool) { return userID, true }
	passThrough := func(next http.Handler) http.Handler { return next }
	timers := handlerOptions{
		revalidatePeriod: options.revalidate,
		pingPeriod:       options.ping,
		idleTimeout:      options.idle,
		writeTimeout:     options.writeTimeout,
	}
	mount(router, passThrough, svc, verifier, authorizer, userIDFrom, discardLogger(), timers)
	if options.applications != nil {
		mountApplications(router, passThrough, svc, verifier, authorizer, userIDFrom, discardLogger(), timers)
	}
	f.server = httptest.NewServer(router)
	t.Cleanup(f.server.Close)
	return f
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

func TestCreateHostWithoutRootRoute(t *testing.T) {
	t.Parallel()
	f := newFixture(t, fixtureOptions{operator: true})
	// Exactly what the Terminal page and the Server detail Terminal tab send:
	// no File manager context at all.
	body := `{"target":"host","cols":120,"rows":32}`
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
	if created.Target != TargetHost || created.Root != "" || created.Cwd != "" {
		t.Fatalf("created = %+v, want a host session with no root/cwd", created)
	}
	if created.WebSocketPath == "" || created.Subprotocol != Subprotocol {
		t.Fatalf("unexpected create response: %+v", created)
	}
}

// TestCreateResponseJSONShape pins the key set of the 201 body against
// docs/terminal-session-api.md: root/cwd are omitted when empty (a rootless
// host session and every container session), while the required fields are
// always present. The web fixtures in terminal-api.test.ts use the same shape.
func TestCreateResponseJSONShape(t *testing.T) {
	t.Parallel()
	base := Session{
		ID:        uuid.New(),
		ServerID:  uuid.New(),
		Target:    TargetHost,
		Cols:      120,
		Rows:      32,
		ExpiresAt: time.Now(),
	}
	container := base
	container.Target = TargetContainer
	container.ApplicationID = uuid.New()
	container.Service = "web"
	container.ContainerID = "a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2"
	container.ContainerName = "gotham-web-1"
	container.TargetLabel = "web · gotham-web-1"

	hostWithRoot := base
	hostWithRoot.Root = "volumes"
	hostWithRoot.Cwd = "my-app/data"

	cases := []struct {
		name    string
		session *Session
		present []string
		absent  []string
	}{
		{
			name:    "host without a root",
			session: &base,
			present: []string{"id", "server_id", "target", "cols", "rows", "expires_at", "websocket_path", "subprotocol"},
			absent:  []string{"root", "cwd", "application_id", "service", "container_id", "container_name", "target_label"},
		},
		{
			name:    "host with a root",
			session: &hostWithRoot,
			present: []string{"id", "server_id", "target", "root", "cwd", "cols", "rows", "expires_at", "websocket_path", "subprotocol"},
			absent:  []string{"application_id", "service", "container_id", "container_name", "target_label"},
		},
		{
			name:    "container",
			session: &container,
			present: []string{"id", "server_id", "target", "cols", "rows", "expires_at", "websocket_path", "subprotocol", "application_id", "service", "container_id", "container_name", "target_label"},
			absent:  []string{"root", "cwd"},
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			encoded, err := json.Marshal(newCreateResponse(testCase.session, "/api/v1/terminal/ws"))
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			var keys map[string]json.RawMessage
			if err := json.Unmarshal(encoded, &keys); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			for _, key := range testCase.present {
				if _, ok := keys[key]; !ok {
					t.Errorf("response is missing %q: %s", key, encoded)
				}
			}
			for _, key := range testCase.absent {
				if _, ok := keys[key]; ok {
					t.Errorf("response must omit %q: %s", key, encoded)
				}
			}
		})
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
		{`{"target":"host","cwd":"app","cols":80,"rows":24}`, http.StatusBadRequest},
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

func expectClose(t *testing.T, conn *websocket.Conn, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if err := conn.SetReadDeadline(time.Now().Add(200 * time.Millisecond)); err != nil {
			return
		}
		var payload []byte
		err := websocket.Message.Receive(conn, &payload)
		if err == nil {
			continue
		}
		var netErr net.Error
		if errors.As(err, &netErr) && netErr.Timeout() {
			continue
		}
		return
	}
	t.Fatal("connection stayed open")
}

func expectErrorAndClose(t *testing.T, conn *websocket.Conn, want string, timeout time.Duration) {
	t.Helper()
	sawError := false
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if err := conn.SetReadDeadline(time.Now().Add(200 * time.Millisecond)); err != nil {
			break
		}
		var payload []byte
		err := websocket.Message.Receive(conn, &payload)
		if err == nil {
			if len(payload) > 0 && payload[0] == opError {
				sawError = true
				if !strings.Contains(string(payload[1:]), want) {
					t.Fatalf("error frame = %q, want %q", payload[1:], want)
				}
			}
			continue
		}
		var netErr net.Error
		if errors.As(err, &netErr) && netErr.Timeout() {
			continue
		}
		if !sawError {
			t.Fatal("stream closed without a revocation frame")
		}
		return
	}
	t.Fatal("connection stayed open")
}

func waitSessionDone(t *testing.T, session *Session) {
	t.Helper()
	select {
	case <-session.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("session was not released")
	}
}

func TestNonUpgradeGetDoesNotBurnSession(t *testing.T) {
	t.Parallel()
	f := newFixture(t, fixtureOptions{operator: true})
	session := f.create(t, CreateRequest{Target: "host", Root: "volumes", Cols: 80, Rows: 24})
	path := f.server.URL + "/v1/servers/" + session.ServerID.String() + "/terminal/sessions/" + session.ID.String() + "/ws"

	request, _ := http.NewRequest(http.MethodGet, path, nil)
	request.Header.Set("Authorization", "Bearer good-token")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("plain GET: %v", err)
	}
	_ = response.Body.Close()
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("plain GET status = %d, want 400 (not an upgrade)", response.StatusCode)
	}

	conn, err := f.dial(t, session, f.server.URL)
	if err != nil {
		t.Fatalf("attach after a non-upgrade GET: %v", err)
	}
	defer func() { _ = conn.Close() }()
	if opcode, _ := readFrame(t, conn); opcode != opOpened {
		t.Fatalf("first frame opcode = %#x, want opened", opcode)
	}
}

func TestOriginSchemeMustMatch(t *testing.T) {
	t.Parallel()
	f := newFixture(t, fixtureOptions{operator: true})
	session := f.create(t, CreateRequest{Target: "host", Root: "volumes", Cols: 80, Rows: 24})
	path := f.server.URL + "/v1/servers/" + session.ServerID.String() + "/terminal/sessions/" + session.ID.String() + "/ws"
	httpsOrigin := "https://" + strings.TrimPrefix(f.server.URL, "http://")

	request, _ := http.NewRequest(http.MethodGet, path, nil)
	request.Header.Set("Authorization", "Bearer good-token")
	request.Header.Set("Origin", httpsOrigin)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	_ = response.Body.Close()
	if response.StatusCode != http.StatusForbidden {
		t.Fatalf("https origin on an http connection status = %d, want 403", response.StatusCode)
	}
}

func TestRevocationClosesStream(t *testing.T) {
	t.Parallel()
	f := newFixture(t, fixtureOptions{operator: true, revalidate: 40 * time.Millisecond})
	session := f.create(t, CreateRequest{Target: "host", Root: "volumes", Cols: 80, Rows: 24})
	conn, err := f.dial(t, session, f.server.URL)
	if err != nil {
		t.Fatalf("attach: %v", err)
	}
	defer func() { _ = conn.Close() }()
	if opcode, _ := readFrame(t, conn); opcode != opOpened {
		t.Fatalf("first frame opcode = %#x, want opened", opcode)
	}
	f.authorizer.setAllow(false)
	expectErrorAndClose(t, conn, "terminal session was revoked", 5*time.Second)
	waitSessionDone(t, session)
}

func TestTransientRevalidationErrorKeepsShell(t *testing.T) {
	t.Parallel()
	f := newFixture(t, fixtureOptions{operator: true, revalidate: 30 * time.Millisecond})
	session := f.create(t, CreateRequest{Target: "host", Root: "volumes", Cols: 80, Rows: 24})
	conn, err := f.dial(t, session, f.server.URL)
	if err != nil {
		t.Fatalf("attach: %v", err)
	}
	defer func() { _ = conn.Close() }()
	if opcode, _ := readFrame(t, conn); opcode != opOpened {
		t.Fatalf("first frame opcode = %#x, want opened", opcode)
	}

	// A database blip must not end the shell; fewer than the fail-closed
	// threshold of consecutive errors keeps it alive.
	f.authorizer.setErr(errors.New("database is down"))
	time.Sleep(40 * time.Millisecond)
	sendFrameTest(t, conn, opInput, []byte("echo survived-blip\r"))
	readUntilOutput(t, conn, "survived-blip")

	f.authorizer.setErr(nil)
	sendFrameTest(t, conn, opInput, []byte("echo after-blip\r"))
	readUntilOutput(t, conn, "after-blip")
}

func TestPersistentRevalidationErrorFailsClosed(t *testing.T) {
	t.Parallel()
	f := newFixture(t, fixtureOptions{operator: true, revalidate: 30 * time.Millisecond})
	session := f.create(t, CreateRequest{Target: "host", Root: "volumes", Cols: 80, Rows: 24})
	conn, err := f.dial(t, session, f.server.URL)
	if err != nil {
		t.Fatalf("attach: %v", err)
	}
	defer func() { _ = conn.Close() }()
	if opcode, _ := readFrame(t, conn); opcode != opOpened {
		t.Fatalf("first frame opcode = %#x, want opened", opcode)
	}
	f.authorizer.setErr(errors.New("database is down"))
	expectErrorAndClose(t, conn, "could not be confirmed", 5*time.Second)
	waitSessionDone(t, session)
}

func TestCloseDoesNotSendRevokedFrame(t *testing.T) {
	t.Parallel()
	f := newFixture(t, fixtureOptions{
		operator:   true,
		revalidate: 10 * time.Millisecond,
		// Emulate a lookup that fails because the relay context was cancelled
		// by the normal close: that must not be reported as a revocation.
		revalidateFn: func(ctx context.Context) (bool, error) { return ctx.Err() == nil, nil },
	})
	session := f.create(t, CreateRequest{Target: "host", Root: "volumes", Cols: 80, Rows: 24})
	conn, err := f.dial(t, session, f.server.URL)
	if err != nil {
		t.Fatalf("attach: %v", err)
	}
	defer func() { _ = conn.Close() }()
	if opcode, _ := readFrame(t, conn); opcode != opOpened {
		t.Fatalf("first frame opcode = %#x, want opened", opcode)
	}
	// Let a few revalidation ticks pass, then close while a tick may be in
	// flight.
	time.Sleep(25 * time.Millisecond)
	sendFrameTest(t, conn, opClose, nil)

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if err := conn.SetReadDeadline(time.Now().Add(200 * time.Millisecond)); err != nil {
			return
		}
		var payload []byte
		err := websocket.Message.Receive(conn, &payload)
		if err != nil {
			var netErr net.Error
			if errors.As(err, &netErr) && netErr.Timeout() {
				continue
			}
			return
		}
		if len(payload) > 0 && payload[0] == opError {
			t.Fatalf("normal close emitted a revocation frame: %s", payload[1:])
		}
	}
	t.Fatal("connection did not close")
}

func TestShellSurvivesAccessTokenExpiry(t *testing.T) {
	t.Parallel()
	f := newFixture(t, fixtureOptions{operator: true, revalidate: 40 * time.Millisecond})
	valid := &atomic.Bool{}
	valid.Store(true)
	f.verifier.valid = valid
	session := f.create(t, CreateRequest{Target: "host", Root: "volumes", Cols: 80, Rows: 24})
	conn, err := f.dial(t, session, f.server.URL)
	if err != nil {
		t.Fatalf("attach: %v", err)
	}
	defer func() { _ = conn.Close() }()
	if opcode, _ := readFrame(t, conn); opcode != opOpened {
		t.Fatalf("first frame opcode = %#x, want opened", opcode)
	}

	// The handshake token expires; the browser cannot refresh it on an open
	// socket, so the live shell must survive while its session/operator state
	// is valid.
	valid.Store(false)
	time.Sleep(3 * 40 * time.Millisecond)

	sendFrameTest(t, conn, opInput, []byte("echo still-alive\r"))
	readUntilOutput(t, conn, "still-alive")
	sendFrameTest(t, conn, opInput, []byte("exit 0\r"))
	for {
		opcode, payload := readFrame(t, conn)
		if opcode == opExited {
			break
		}
		if opcode == opError {
			t.Fatalf("stream was closed after token expiry: %s", payload)
		}
	}
	waitSessionDone(t, session)
}

func TestIdleConnectionCloses(t *testing.T) {
	t.Parallel()
	f := newFixture(t, fixtureOptions{
		operator:   true,
		revalidate: time.Hour,
		ping:       20 * time.Millisecond,
		idle:       120 * time.Millisecond,
	})
	session := f.create(t, CreateRequest{Target: "host", Root: "volumes", Cols: 80, Rows: 24})
	conn, err := f.dial(t, session, f.server.URL)
	if err != nil {
		t.Fatalf("attach: %v", err)
	}
	defer func() { _ = conn.Close() }()
	if opcode, _ := readFrame(t, conn); opcode != opOpened {
		t.Fatalf("first frame opcode = %#x, want opened", opcode)
	}
	expectClose(t, conn, 5*time.Second)
	waitSessionDone(t, session)
}

func TestSessionsAreIsolated(t *testing.T) {
	t.Parallel()
	f := newFixture(t, fixtureOptions{operator: true})
	first := f.create(t, CreateRequest{Target: "host", Root: "volumes", Cols: 80, Rows: 24})
	second := f.create(t, CreateRequest{Target: "host", Root: "volumes", Cols: 80, Rows: 24})

	firstConn, err := f.dial(t, first, f.server.URL)
	if err != nil {
		t.Fatalf("attach first: %v", err)
	}
	defer func() { _ = firstConn.Close() }()
	secondConn, err := f.dial(t, second, f.server.URL)
	if err != nil {
		t.Fatalf("attach second: %v", err)
	}
	defer func() { _ = secondConn.Close() }()
	if opcode, _ := readFrame(t, firstConn); opcode != opOpened {
		t.Fatalf("first frame opcode = %#x, want opened", opcode)
	}
	if opcode, _ := readFrame(t, secondConn); opcode != opOpened {
		t.Fatalf("second first frame opcode = %#x, want opened", opcode)
	}

	sendFrameTest(t, firstConn, opInput, []byte("echo iso-one\r"))
	readUntilOutput(t, firstConn, "iso-one")
	sendFrameTest(t, secondConn, opInput, []byte("echo iso-two\r"))
	readUntilOutput(t, secondConn, "iso-two")

	// Force a marker on the first stream and inspect everything it emitted:
	// the second session's marker must never appear there.
	output := ""
	sendFrameTest(t, firstConn, opInput, []byte("echo iso-three\r"))
	for !strings.Contains(output, "iso-three") {
		opcode, payload := readFrame(t, firstConn)
		switch opcode {
		case opOutput:
			output += string(payload)
		case opExited, opError:
			t.Fatalf("first stream ended unexpectedly: %s", payload)
		}
	}
	if strings.Contains(output, "iso-two") {
		t.Fatalf("cross-session output leak: first stream saw %q", output)
	}
}

func TestGoroutinesReleasedAfterRelay(t *testing.T) {
	t.Parallel()
	f := newFixture(t, fixtureOptions{operator: true})
	// Warm up so lazily-created gRPC and WebSocket goroutines are not counted.
	runExitSession(t, f)
	baseline := runtime.NumGoroutine()
	for i := 0; i < 3; i++ {
		runExitSession(t, f)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if runtime.NumGoroutine() <= baseline+2 {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("goroutines after relays = %d, baseline %d", runtime.NumGoroutine(), baseline)
}

func runExitSession(t *testing.T, f *fixture) {
	t.Helper()
	session := f.create(t, CreateRequest{Target: "host", Root: "volumes", Cols: 80, Rows: 24})
	conn, err := f.dial(t, session, f.server.URL)
	if err != nil {
		t.Fatalf("attach: %v", err)
	}
	if opcode, _ := readFrame(t, conn); opcode != opOpened {
		t.Fatalf("first frame opcode = %#x, want opened", opcode)
	}
	sendFrameTest(t, conn, opInput, []byte("exit 0\r"))
	for {
		opcode, _ := readFrame(t, conn)
		if opcode == opExited {
			break
		}
	}
	waitSessionDone(t, session)
	_ = conn.Close()
}

func TestRelayErrorWorkingDirectory(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		err  error
		want string
	}{
		{
			name: "fsroot invalid cwd",
			err:  status.Error(codes.InvalidArgument, "terminal: invalid working directory: fsroot: invalid path: \"a\" escapes the root"),
			want: "invalid working directory",
		},
		{
			name: "bare cwd rejection",
			err:  status.Error(codes.InvalidArgument, "terminal: invalid working directory"),
			want: "invalid working directory",
		},
		{
			name: "other invalid argument",
			err:  status.Error(codes.InvalidArgument, "terminal: input chunk exceeds 4 bytes"),
			want: "the terminal request was rejected",
		},
		{
			name: "old agent without root support",
			err:  status.Error(codes.InvalidArgument, "terminal: a bounded root name is required"),
			want: "the node agent needs an update to open a terminal without a folder",
		},
		{
			name: "missing cwd",
			err:  status.Error(codes.NotFound, "terminal: working directory not found: gone"),
			want: "the working directory no longer exists",
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := relayError(testCase.err, Target{Kind: TargetHost}); got != testCase.want {
				t.Fatalf("relayError(%v) = %q, want %q", testCase.err, got, testCase.want)
			}
		})
	}
}

// rawWS is a minimal RFC 6455 client used to send protocol-level ping and close
// control frames, which golang.org/x/net/websocket's public client cannot do.
type rawWS struct {
	conn net.Conn
	br   *bufio.Reader
}

func dialRawWS(t *testing.T, baseURL, path, token string) *rawWS {
	t.Helper()
	host := strings.TrimPrefix(baseURL, "http://")
	conn, err := net.Dial("tcp", host)
	if err != nil {
		t.Fatalf("dial raw ws: %v", err)
	}
	key := base64.StdEncoding.EncodeToString([]byte("0123456789abcdef"))
	request := fmt.Sprintf(
		"GET %s HTTP/1.1\r\nHost: %s\r\nUpgrade: websocket\r\nConnection: Upgrade\r\n"+
			"Sec-WebSocket-Key: %s\r\nSec-WebSocket-Version: 13\r\n"+
			"Sec-WebSocket-Protocol: %s, %s%s\r\nOrigin: %s\r\n\r\n",
		path, host, key, Subprotocol, TokenProtocolPrefix, token, baseURL,
	)
	if _, err := conn.Write([]byte(request)); err != nil {
		_ = conn.Close()
		t.Fatalf("send handshake: %v", err)
	}
	reader := bufio.NewReader(conn)
	status, err := reader.ReadString('\n')
	if err != nil {
		_ = conn.Close()
		t.Fatalf("read handshake status: %v", err)
	}
	if !strings.Contains(status, "101") {
		_ = conn.Close()
		t.Fatalf("handshake status = %q, want 101", strings.TrimSpace(status))
	}
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			_ = conn.Close()
			t.Fatalf("read handshake headers: %v", err)
		}
		if line == "\r\n" {
			break
		}
	}
	return &rawWS{conn: conn, br: reader}
}

func (c *rawWS) writeFrame(opcode byte, payload []byte) error {
	frame := []byte{0x80 | opcode}
	switch {
	case len(payload) < 126:
		frame = append(frame, byte(0x80|len(payload)))
	case len(payload) <= 0xffff:
		frame = append(frame, 0x80|126, byte(len(payload)>>8), byte(len(payload)))
	default:
		return errors.New("raw test frame too large")
	}
	mask := []byte{0x12, 0x34, 0x56, 0x78}
	frame = append(frame, mask...)
	for i, value := range payload {
		frame = append(frame, value^mask[i%4])
	}
	_, err := c.conn.Write(frame)
	return err
}

func (c *rawWS) readFrame(timeout time.Duration) (byte, []byte, error) {
	if err := c.conn.SetReadDeadline(time.Now().Add(timeout)); err != nil {
		return 0, nil, err
	}
	first, err := c.br.ReadByte()
	if err != nil {
		return 0, nil, err
	}
	second, err := c.br.ReadByte()
	if err != nil {
		return 0, nil, err
	}
	length := int(second & 0x7f)
	switch length {
	case 126:
		var size [2]byte
		if _, err := io.ReadFull(c.br, size[:]); err != nil {
			return 0, nil, err
		}
		length = int(size[0])<<8 | int(size[1])
	case 127:
		var size [8]byte
		if _, err := io.ReadFull(c.br, size[:]); err != nil {
			return 0, nil, err
		}
		length = 0
		for _, value := range size {
			length = length<<8 | int(value)
		}
	}
	payload := make([]byte, length)
	if _, err := io.ReadFull(c.br, payload); err != nil {
		return 0, nil, err
	}
	return first & 0x0f, payload, nil
}

func TestProtocolPingAfterWriteDeadlineKeepsSession(t *testing.T) {
	f := newFixture(t, fixtureOptions{
		operator:     true,
		revalidate:   time.Hour,
		ping:         time.Hour,
		idle:         time.Hour,
		writeTimeout: 80 * time.Millisecond,
	})
	session := f.create(t, CreateRequest{Target: "host", Root: "volumes", Cols: 80, Rows: 24})
	path := "/v1/servers/" + session.ServerID.String() + "/terminal/sessions/" + session.ID.String() + "/ws"
	client := dialRawWS(t, f.server.URL, path, "good-token")
	defer func() { _ = client.conn.Close() }()

	wsOpcode, payload, err := client.readFrame(5 * time.Second)
	if err != nil {
		t.Fatalf("read opened: %v", err)
	}
	if wsOpcode != 0x2 || len(payload) == 0 || payload[0] != opOpened {
		t.Fatalf("first frame = ws %#x payload %x, want a binary opened frame", wsOpcode, payload)
	}

	// Wait past the server's write deadline. A stale deadline would make the
	// read path's pong write fail and kill the shell.
	time.Sleep(200 * time.Millisecond)
	if err := client.writeFrame(0x9, []byte("hi")); err != nil {
		t.Fatalf("send protocol ping: %v", err)
	}
	for {
		wsOpcode, payload, err := client.readFrame(5 * time.Second)
		if err != nil {
			t.Fatalf("pong not received; session died: %v", err)
		}
		if wsOpcode == 0xA {
			if string(payload) != "hi" {
				t.Fatalf("pong payload = %q, want %q", payload, "hi")
			}
			break
		}
	}

	// The data path must still work after the ping.
	input := append([]byte{opInput}, []byte("echo ping-ok\r")...)
	if err := client.writeFrame(0x2, input); err != nil {
		t.Fatalf("send input: %v", err)
	}
	var output strings.Builder
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) && !strings.Contains(output.String(), "ping-ok") {
		wsOpcode, payload, err := client.readFrame(5 * time.Second)
		if err != nil {
			t.Fatalf("read output: %v", err)
		}
		if wsOpcode == 0x2 && len(payload) > 0 && payload[0] == opOutput {
			output.Write(payload[1:])
		}
	}
	if !strings.Contains(output.String(), "ping-ok") {
		t.Fatalf("output = %q, want ping-ok", output.String())
	}

	// A client close still ends the session cleanly.
	if err := client.writeFrame(0x8, []byte{0x03, 0xe8}); err != nil {
		t.Fatalf("send close: %v", err)
	}
	waitSessionDone(t, session)
}
