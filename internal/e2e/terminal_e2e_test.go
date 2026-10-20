package e2e

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"golang.org/x/net/websocket"

	"github.com/justindeelux/gotham/agent"
	"github.com/justindeelux/gotham/internal/auth"
	"github.com/justindeelux/gotham/internal/servers"
	"github.com/justindeelux/gotham/internal/terminal"
)

// JUS-251: the terminal journeys against the real stack — a real mTLS agent,
// a real PTY, the production control-plane service and WebSocket routes, a
// real Redis connection and the real FileService roots. The test proves the
// three things fakes cannot:
//
//  1. a host session starts with no root and no cwd (JUS-253),
//  2. terminal input/output NEVER appears in the control-plane or agent logs
//     or in Redis pub/sub, and
//  3. closing the session kills its background job and releases the PTY
//     master descriptor (JUS-254), while an escaping cwd is refused before a
//     shell starts.
//
// It is gated by GOTHAM_E2E=1 because it needs Redis (and, on Linux, /proc).

// terminalTokenProtocol mirrors the route contract's token subprotocol.
const terminalToken = "e2e-terminal-token"

// terminalTestVerifier accepts exactly one token for exactly one account.
type terminalTestVerifier struct{ claims auth.Claims }

func (v terminalTestVerifier) VerifyAccessToken(token string) (*auth.Claims, error) {
	if token != terminalToken {
		return nil, fmt.Errorf("bad token")
	}
	copied := v.claims
	return &copied, nil
}

// terminalTestAuthorizer never revokes and accepts only host targets.
type terminalTestAuthorizer struct{}

func (terminalTestAuthorizer) Authorize(_ context.Context, claims *auth.Claims, target terminal.Target) (terminal.Marker, error) {
	if target.Kind != terminal.TargetHost || claims == nil {
		return terminal.Marker{}, fmt.Errorf("not allowed")
	}
	return terminal.Marker{SignInAt: time.Now()}, nil
}

func (terminalTestAuthorizer) Revalidate(context.Context, *auth.Claims, terminal.Marker, terminal.Target) (bool, error) {
	return true, nil
}

// terminalLogBuffer keeps every log line the terminal stack wrote.
type terminalLogBuffer struct {
	mu   sync.Mutex
	data strings.Builder
}

func (b *terminalLogBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.data.Write(p)
}

func (b *terminalLogBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.data.String()
}

// terminalFrame reads one server frame with a bounded deadline.
func terminalFrame(t *testing.T, conn *websocket.Conn, timeout time.Duration) (byte, []byte) {
	t.Helper()
	if err := conn.SetReadDeadline(time.Now().Add(timeout)); err != nil {
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

// terminalSend writes one client frame.
func terminalSend(t *testing.T, conn *websocket.Conn, opcode byte, payload []byte) {
	t.Helper()
	if err := websocket.Message.Send(conn, append([]byte{opcode}, payload...)); err != nil {
		t.Fatalf("send frame: %v", err)
	}
}

// terminalReadUntil accumulates output frames until marker appears.
func terminalReadUntil(t *testing.T, conn *websocket.Conn, marker string) {
	t.Helper()
	var output strings.Builder
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		opcode, payload := terminalFrame(t, conn, 15*time.Second)
		switch opcode {
		case 0x01:
			output.Write(payload)
			if strings.Contains(output.String(), marker) {
				return
			}
		case 0x02:
			t.Fatalf("shell exited before %q: %s", marker, payload)
		case 0x03:
			t.Fatalf("terminal error before %q: %s", marker, payload)
		}
	}
	t.Fatalf("timed out waiting for %q; output %q", marker, output.String())
}

// terminalPWDFD processes are the test process (the agent runs in-process).
func terminalOpenPTMX(t *testing.T) int {
	t.Helper()
	if runtime.GOOS == "linux" {
		entries, err := os.ReadDir("/proc/self/fd")
		if err != nil {
			t.Fatalf("read /proc/self/fd: %v", err)
		}
		count := 0
		for _, entry := range entries {
			target, err := os.Readlink(filepath.Join("/proc/self/fd", entry.Name()))
			if err == nil && strings.Contains(target, "ptmx") {
				count++
			}
		}
		return count
	}
	out, err := exec.Command("lsof", "-p", fmt.Sprintf("%d", os.Getpid()), "-Fn").Output()
	if err != nil {
		t.Fatalf("lsof: %v", err)
	}
	count := 0
	for _, line := range strings.Split(string(out), "\n") {
		if strings.Contains(line, "/dev/ptmx") {
			count++
		}
	}
	return count
}

// terminalProcessExists checks for the uniquely named background job.
func terminalProcessExists(marker string) bool {
	out, err := exec.Command("ps", "-eo", "args=").Output()
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(out), "\n") {
		if strings.Contains(line, marker) && !strings.Contains(line, "ps -eo") {
			return true
		}
	}
	return false
}

// TestTerminalRealStackNoLeak is the canary + cleanup proof.
func TestTerminalRealStackNoLeak(t *testing.T) {
	requireE2E(t)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	root := t.TempDir()
	volumes := filepath.Join(root, "volumes")
	compose := filepath.Join(root, "compose")
	work := filepath.Join(volumes, "work")
	for _, dir := range []string{work, compose} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}
	// A symlink inside the writable root that leaves it. The agent must refuse
	// it before any shell starts.
	if err := os.Symlink("/etc", filepath.Join(volumes, "escape")); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	agentLogs := &terminalLogBuffer{}
	cpLogs := &terminalLogBuffer{}
	agentLogger := slog.New(slog.NewTextHandler(agentLogs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	cpLogger := slog.New(slog.NewTextHandler(cpLogs, &slog.HandlerOptions{Level: slog.LevelDebug}))

	files, err := agent.NewFileServer(agent.Config{
		ManagedVolumeRoot: volumes,
		ComposeRoot:       compose,
		FileFullAccess:    false,
	}, agentLogger)
	if err != nil {
		t.Fatalf("file server: %v", err)
	}
	t.Cleanup(func() { _ = files.Close() })

	engine, err := agent.NewDockerClient(e2eDockerSock(), agent.WithRegistryStateDir(t.TempDir()))
	if err != nil {
		t.Fatalf("docker client: %v", err)
	}

	nodeID := "terminal-e2e-" + uuid.New().String()[:8]
	agentAddr, authority := startLocalAgentWithOptions(t, ctx, engine, nodeID,
		agent.WithTerminalService(agent.NewTerminalServer(agent.TerminalConfig{
			Logger:      agentLogger,
			Roots:       files,
			MaxSessions: 4,
		})))

	serverID := uuid.New()
	node := &servers.Server{
		ID:     serverID,
		Name:   "terminal-e2e",
		IP:     agentAddr,
		Port:   22,
		Status: servers.StatusReady,
		NodeID: &nodeID,
	}
	userID := uuid.New()
	svc := terminal.NewService(terminal.Config{
		Registry: &staticRegistry{server: node},
		Dial: func(dialCtx context.Context, srv *servers.Server) (terminal.TerminalClient, error) {
			return servers.DialTerminalClient(dialCtx, srv.IP, authority,
				servers.WithDockerServerName(nodeID))
		},
		Logger: cpLogger,
	})
	t.Cleanup(svc.CloseAll)

	router := chi.NewRouter()
	passThrough := func(next http.Handler) http.Handler { return next }
	userIDFrom := func(context.Context) (uuid.UUID, bool) { return userID, true }
	router.Route("/api", func(r chi.Router) {
		terminal.Mount(r, passThrough, svc,
			terminalTestVerifier{claims: auth.Claims{Subject: userID.String(), SessionID: uuid.NewString()}},
			terminalTestAuthorizer{}, userIDFrom, cpLogger)
	})
	httpServer := httptest.NewServer(router)
	t.Cleanup(httpServer.Close)

	// Redis: subscribe to EVERY channel for the whole test. Terminal bytes
	// must never be published, so this pattern sees nothing from the shell.
	rdb := redis.NewClient(&redis.Options{Addr: e2eRedisAddr()})
	t.Cleanup(func() { _ = rdb.Close() })
	pubsub := rdb.PSubscribe(ctx, "*")
	t.Cleanup(func() { _ = pubsub.Close() })
	if _, err := pubsub.Receive(ctx); err != nil {
		t.Fatalf("subscribe *: %v", err)
	}
	var published []string
	var pubMu sync.Mutex
	go func() {
		for message := range pubsub.Channel() {
			pubMu.Lock()
			published = append(published, message.Channel+"="+message.Payload)
			pubMu.Unlock()
		}
	}()

	baselinePTMX := terminalOpenPTMX(t)
	canary := "GOTHAM-CANARY-" + uuid.New().String()[:8]

	// 1. A host session with NO root and NO cwd (JUS-253).
	createBody := `{"target":"host","cols":100,"rows":30}`
	response, err := http.Post(httpServer.URL+"/api/v1/servers/"+serverID.String()+"/terminal/sessions",
		"application/json", strings.NewReader(createBody))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("create without root/cwd = %d, want 201 (JUS-253)", response.StatusCode)
	}
	var created struct {
		ID            string `json:"id"`
		WebSocketPath string `json:"websocket_path"`
	}
	if err := json.NewDecoder(response.Body).Decode(&created); err != nil {
		t.Fatalf("decode create: %v", err)
	}
	_ = response.Body.Close()

	wsURL := "ws" + strings.TrimPrefix(httpServer.URL, "http") + created.WebSocketPath
	config, err := websocket.NewConfig(wsURL, httpServer.URL)
	if err != nil {
		t.Fatalf("ws config: %v", err)
	}
	config.Protocol = []string{terminal.Subprotocol, terminal.TokenProtocolPrefix + terminalToken}
	conn, err := websocket.DialConfig(config)
	if err != nil {
		t.Fatalf("attach: %v", err)
	}
	if opcode, _ := terminalFrame(t, conn, 15*time.Second); opcode != 0x04 {
		t.Fatalf("first frame = %#x, want opened", opcode)
	}

	// 2. Type a unique canary; the real PTY echoes it and `stty size` agrees
	//    with the resize frame the route relays.
	terminalSend(t, conn, 0x01, []byte("echo "+canary+"\r"))
	terminalReadUntil(t, conn, canary)
	resize := make([]byte, 4)
	binary.BigEndian.PutUint16(resize[0:2], 90)
	binary.BigEndian.PutUint16(resize[2:4], 28)
	terminalSend(t, conn, 0x02, resize)
	terminalSend(t, conn, 0x01, []byte("stty size; pwd\r"))
	terminalReadUntil(t, conn, "28 90")

	// 3. The canary is absent from both logs and from every Redis channel.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		pubMu.Lock()
		for _, message := range published {
			if strings.Contains(message, canary) {
				t.Fatalf("terminal bytes leaked into Redis: %s", message)
			}
		}
		pubMu.Unlock()
		time.Sleep(50 * time.Millisecond)
	}
	if strings.Contains(agentLogs.String(), canary) {
		t.Error("terminal input leaked into the agent log")
	}
	if strings.Contains(cpLogs.String(), canary) {
		t.Error("terminal input leaked into the control-plane log")
	}

	// 4. A cwd through a symlink that leaves the root is refused and starts no
	//    shell.
	escapeBody := `{"target":"host","root":"volumes","cwd":"escape","cols":80,"rows":24}`
	escape, err := http.Post(httpServer.URL+"/api/v1/servers/"+serverID.String()+"/terminal/sessions",
		"application/json", strings.NewReader(escapeBody))
	if err != nil {
		t.Fatalf("create escaping cwd: %v", err)
	}
	var escapeSession struct {
		WebSocketPath string `json:"websocket_path"`
	}
	_ = json.NewDecoder(escape.Body).Decode(&escapeSession)
	_ = escape.Body.Close()
	if escape.StatusCode != http.StatusCreated {
		// Either refusal point is acceptable; a preflight refusal needs no
		// stream assertion.
		if escape.StatusCode < 400 {
			t.Fatalf("create escaping cwd = %d", escape.StatusCode)
		}
	} else {
		// Create is pending-only: the agent resolves the directory when the
		// stream opens, so the refusal arrives as the terminal error frame and
		// no shell runs.
		escCfg, cfgErr := websocket.NewConfig("ws"+strings.TrimPrefix(httpServer.URL, "http")+escapeSession.WebSocketPath, httpServer.URL)
		if cfgErr != nil {
			t.Fatalf("escape ws config: %v", cfgErr)
		}
		escCfg.Protocol = []string{terminal.Subprotocol, terminal.TokenProtocolPrefix + terminalToken}
		escConn, dialErr := websocket.DialConfig(escCfg)
		if dialErr != nil {
			t.Fatalf("attach escaping cwd: %v", dialErr)
		}
		escOp, escPayload := terminalFrame(t, escConn, 15*time.Second)
		_ = escConn.Close()
		if escOp != 0x03 {
			t.Fatalf("escaping cwd frame = %#x %q, want an error frame", escOp, escPayload)
		}
		if !strings.Contains(strings.ToLower(string(escPayload)), "working directory") {
			t.Fatalf("escaping cwd error = %q, want a working-directory refusal", escPayload)
		}
	}

	// 5. Close with a background job: no process survives and the PTY master
	//    descriptor is released.
	jobSeconds := "7641"
	terminalSend(t, conn, 0x01, []byte("sleep "+jobSeconds+" &\r"))
	waitFor := time.Now().Add(10 * time.Second)
	for time.Now().Before(waitFor) && !terminalProcessExists("sleep "+jobSeconds) {
		time.Sleep(100 * time.Millisecond)
	}
	if !terminalProcessExists("sleep " + jobSeconds) {
		t.Fatal("background job never started")
	}

	terminalSend(t, conn, 0x03, nil) // explicit close frame
	_ = conn.Close()
	waitGone := time.Now().Add(15 * time.Second)
	for time.Now().Before(waitGone) && terminalProcessExists("sleep "+jobSeconds) {
		time.Sleep(200 * time.Millisecond)
	}
	if terminalProcessExists("sleep " + jobSeconds) {
		t.Fatalf("background job sleep %s survived session close (JUS-254)", jobSeconds)
	}

	released := false
	waitFD := time.Now().Add(15 * time.Second)
	for time.Now().Before(waitFD) {
		if terminalOpenPTMX(t) == baselinePTMX {
			released = true
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	if !released {
		t.Fatalf("PTY master descriptor leaked: %d open, baseline %d", terminalOpenPTMX(t), baselinePTMX)
	}

	// The session registry no longer holds it either.
	if _, err := svc.Attach(ctx, userID, serverID, uuid.MustParse(created.ID)); err == nil {
		t.Fatal("closed session is still attachable")
	}
}
