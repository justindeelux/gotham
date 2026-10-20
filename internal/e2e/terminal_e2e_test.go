package e2e

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
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
// a real PTY, the production control-plane service and WebSocket routes, the
// real FileService roots, and every Redis channel observed while a unique
// canary is typed.
//
// What is REAL here: the node agent (in-process gRPC server over mTLS, the
// same one the binary runs), the PTY and its kernel cleanup, the
// control-plane terminal.Service and HTTP/WebSocket routes, the relay frames,
// the FileService root resolution, and the Redis server the subscriber talks
// to.
//
// What is NOT: PostgreSQL is not used (no store is opened); the token
// verifier, the authorizer and the server registry are stubs, so this test
// proves the terminal path, not the account/team permission logic (that lives
// in internal/server and internal/deploy tests); the control plane's realtime
// Redis hub is not mounted (there is no deploy/log traffic here). The
// subscriber uses PSUBSCRIBE * rather than the hub, so it observes every
// channel on this Redis; a publisher configured with a different Redis address
// would be outside this check (terminal code has no such publisher).
//
// It is gated by GOTHAM_E2E=1 because it needs Redis, Docker (for the agent
// construction path) and, for the fd/escaped-job assertions, Linux /proc.

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

// terminalCaptureStderr redirects the process stderr for the test duration, so
// a production log line written through os.Stderr (not an injected logger) is
// visible to the canary assertion. The test is not parallel.
func terminalCaptureStderr(t *testing.T) *terminalLogBuffer {
	t.Helper()
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("stderr pipe: %v", err)
	}
	previous := os.Stderr
	os.Stderr = writer
	buffer := &terminalLogBuffer{}
	done := make(chan struct{})
	go func() {
		_, _ = io.Copy(buffer, reader)
		close(done)
	}()
	t.Cleanup(func() {
		os.Stderr = previous
		_ = writer.Close()
		<-done
		_ = reader.Close()
	})
	return buffer
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

// terminalOpenPTMX counts this process's /dev/ptmx master descriptors (the
// agent runs in-process). A failed inspection fails the test.
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

// terminalProcessAlive reports whether the recorded pid is a LIVE process. pid
// comes from the shell's $!, so it is exact and unique per run. A zombie is
// NOT alive: in a container the test binary is pid 1 and never reaps an orphan
// that the agent SIGKILLed, and signal 0 alone would keep reporting it.
func terminalProcessAlive(pid int) bool {
	if runtime.GOOS == "linux" {
		raw, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
		if err != nil {
			return false
		}
		// The state is the first field after the parenthesized command name.
		if index := strings.LastIndexByte(string(raw), ')'); index >= 0 && index+2 < len(raw) {
			state := raw[index+2]
			return state != 'Z' && state != 'X'
		}
		return false
	}
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return process.Signal(syscall.Signal(0)) == nil
}

// terminalWaitPIDFile reads a pid written by the shell.
func terminalWaitPIDFile(t *testing.T, path string) int {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		raw, err := os.ReadFile(path)
		if err == nil {
			if pid, convErr := strconv.Atoi(strings.TrimSpace(string(raw))); convErr == nil && pid > 0 {
				return pid
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("pid file %s was never written", path)
	return 0
}

// terminalCreate opens one pending host session and returns its id and
// websocket path.
func terminalCreate(t *testing.T, client *http.Client, baseURL, serverID, body string) (string, string) {
	t.Helper()
	response, err := client.Post(baseURL+"/api/v1/servers/"+serverID+"/terminal/sessions",
		"application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusCreated {
		raw, _ := io.ReadAll(response.Body)
		t.Fatalf("create session = %d, want 201: %s", response.StatusCode, raw)
	}
	var created struct {
		ID            string `json:"id"`
		WebSocketPath string `json:"websocket_path"`
	}
	if err := json.NewDecoder(response.Body).Decode(&created); err != nil {
		t.Fatalf("decode create: %v", err)
	}
	return created.ID, created.WebSocketPath
}

// terminalOpened is the agent's opened frame payload.
type terminalOpened struct {
	Shell       string `json:"shell"`
	Cols        uint16 `json:"cols"`
	Rows        uint16 `json:"rows"`
	ServiceUser string `json:"service_user"`
}

// terminalAttach dials one pending session and validates the opened frame.
func terminalAttach(t *testing.T, baseURL, path string) (*websocket.Conn, terminalOpened) {
	t.Helper()
	config, err := websocket.NewConfig("ws"+strings.TrimPrefix(baseURL, "http")+path, baseURL)
	if err != nil {
		t.Fatalf("ws config: %v", err)
	}
	config.Protocol = []string{terminal.Subprotocol, terminal.TokenProtocolPrefix + terminalToken}
	conn, err := websocket.DialConfig(config)
	if err != nil {
		t.Fatalf("attach: %v", err)
	}
	opcode, payload := terminalFrame(t, conn, 15*time.Second)
	if opcode != 0x04 {
		t.Fatalf("first frame = %#x, want opened", opcode)
	}
	var opened terminalOpened
	if err := json.Unmarshal(payload, &opened); err != nil {
		t.Fatalf("decode opened: %v", err)
	}
	if opened.ServiceUser == "" {
		t.Fatal("opened frame has no service_user")
	}
	return conn, opened
}

// terminalWaitPTMX polls the descriptor count back to baseline.
func terminalWaitPTMX(t *testing.T, baseline int, timeout time.Duration) bool {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if terminalOpenPTMX(t) == baseline {
			return true
		}
		time.Sleep(200 * time.Millisecond)
	}
	return false
}

// TestTerminalRealStackNoLeak is the canary + cleanup proof.
func TestTerminalRealStackNoLeak(t *testing.T) {
	requireE2E(t)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	// Capture every logging channel the relay could use: injected loggers,
	// slog.Default() resolved at call time, and direct os.Stderr writes.
	stderr := terminalCaptureStderr(t)
	previousDefault := slog.Default()
	defaultLogs := &terminalLogBuffer{}
	slog.SetDefault(slog.New(slog.NewTextHandler(defaultLogs, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(previousDefault) })

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
	client := &http.Client{Timeout: 30 * time.Second}

	// Redis: subscribe to EVERY channel for the whole test. Terminal bytes
	// must never be published, so this pattern sees nothing from the shell.
	// (The test CP has no realtime hub; PSUBSCRIBE * is strictly wider and
	// observes any publish to this Redis, whoever makes it.)
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
	t.Logf("ptmx baseline: %d", baselinePTMX)
	canary := "GOTHAM-CANARY-" + uuid.New().String()[:8]

	// 1. A host session with NO root and NO cwd (JUS-253). The opened frame
	//    reports the shell and the service_user the shell runs as.
	sessionID, sessionPath := terminalCreate(t, client, httpServer.URL, serverID.String(),
		`{"target":"host","cols":100,"rows":30}`)
	conn, opened := terminalAttach(t, httpServer.URL, sessionPath)
	t.Logf("opened: shell=%s service_user=%s %dx%d", opened.Shell, opened.ServiceUser, opened.Cols, opened.Rows)

	// 2. Type a unique canary; the real PTY echoes it and `stty size` agrees
	//    with the resize frame the route relays. The shell identity is the
	//    agent's service user, reported by the opened frame's service_user.
	terminalSend(t, conn, 0x01, []byte("echo "+canary+"\r"))
	terminalReadUntil(t, conn, canary)
	resize := make([]byte, 4)
	binary.BigEndian.PutUint16(resize[0:2], 90)
	binary.BigEndian.PutUint16(resize[2:4], 28)
	terminalSend(t, conn, 0x02, resize)
	terminalSend(t, conn, 0x01, []byte("stty size; pwd; id -un\r"))
	terminalReadUntil(t, conn, "28 90")

	// 3. The canary is absent from every logging channel and from Redis.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		pubMu.Lock()
		for _, message := range published {
			if strings.Contains(message, canary) {
				pubMu.Unlock()
				t.Fatalf("terminal bytes leaked into Redis: %s", message)
			}
		}
		pubMu.Unlock()
		time.Sleep(50 * time.Millisecond)
	}
	for name, logs := range map[string]string{
		"agent logger":      agentLogs.String(),
		"control-plane":     cpLogs.String(),
		"slog.Default()":    defaultLogs.String(),
		"process os.Stderr": stderr.String(),
	} {
		if strings.Contains(logs, canary) {
			t.Errorf("terminal input leaked into %s", name)
		}
	}

	// 4. A cwd through a symlink that leaves the root is refused and starts no
	//    shell.
	escapeBody := `{"target":"host","root":"volumes","cwd":"escape","cols":80,"rows":24}`
	escape, err := client.Post(httpServer.URL+"/api/v1/servers/"+serverID.String()+"/terminal/sessions",
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
	//    descriptor is released. The pid comes from the shell's $!, so it is
	//    exact and unique for this run.
	pidPath := filepath.Join(root, "job.pid")
	terminalSend(t, conn, 0x01, []byte("sleep 7641 & echo $! > "+pidPath+"\r"))
	jobPID := terminalWaitPIDFile(t, pidPath)
	t.Cleanup(func() {
		if terminalProcessAlive(jobPID) {
			_ = syscall.Kill(jobPID, syscall.SIGKILL)
		}
	})
	if !terminalProcessAlive(jobPID) {
		t.Fatal("background job never started")
	}

	terminalSend(t, conn, 0x03, nil) // explicit close frame
	_ = conn.Close()
	goneBy := time.Now().Add(15 * time.Second)
	for time.Now().Before(goneBy) && terminalProcessAlive(jobPID) {
		time.Sleep(200 * time.Millisecond)
	}
	if terminalProcessAlive(jobPID) {
		t.Fatalf("background job %d survived session close (JUS-254)", jobPID)
	}
	if !terminalWaitPTMX(t, baselinePTMX, 15*time.Second) {
		t.Fatalf("PTY master descriptor leaked: %d open, baseline %d",
			terminalOpenPTMX(t), baselinePTMX)
	}

	// 6. The mutation that re-blocks the PTY master read is only visible while
	//    a process outside the shell's session still holds the slave open:
	//    a setsid-escaped job. It may survive close (documented residual), but
	//    the non-blocking master must still release the fd. Linux-only because
	//    setsid comes from util-linux.
	if _, lookErr := exec.LookPath("setsid"); lookErr != nil {
		t.Log("setsid is not available; the escaped-job fd regression did not run (Linux CI runs it)")
	} else {
		escapedPIDPath := filepath.Join(root, "escaped.pid")
		_, escapedPath := terminalCreate(t, client, httpServer.URL, serverID.String(),
			`{"target":"host","cols":80,"rows":24}`)
		escaped, _ := terminalAttach(t, httpServer.URL, escapedPath)
		// `exec sleep` keeps the recorded pid after sh writes it, so cleanup can
		// kill exactly this process; the job is in its own session (setsid).
		terminalSend(t, escaped, 0x01,
			[]byte("(setsid sh -c 'echo $$ > "+escapedPIDPath+"; exec sleep 7642' &)\r"))
		escapedPID := terminalWaitPIDFile(t, escapedPIDPath)
		t.Cleanup(func() {
			if terminalProcessAlive(escapedPID) {
				_ = syscall.Kill(escapedPID, syscall.SIGKILL)
			}
		})
		if !terminalProcessAlive(escapedPID) {
			t.Fatal("setsid-escaped job never started")
		}
		terminalSend(t, escaped, 0x03, nil)
		_ = escaped.Close()
		if !terminalWaitPTMX(t, baselinePTMX, 15*time.Second) {
			t.Fatalf("PTY master stayed open with a setsid-escaped job: %d, baseline %d (mutation 2b)",
				terminalOpenPTMX(t), baselinePTMX)
		}
	}

	// The session registry no longer holds the closed session.
	if _, err := svc.Attach(ctx, userID, serverID, uuid.MustParse(sessionID)); err == nil {
		t.Fatal("closed session is still attachable")
	}
}
