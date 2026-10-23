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

const terminalToken = "e2e-terminal-token"

type terminalTestVerifier struct{ claims auth.Claims }

func (v terminalTestVerifier) VerifyAccessToken(token string) (*auth.Claims, error) {
	if token != terminalToken {
		return nil, fmt.Errorf("bad token")
	}
	copied := v.claims
	return &copied, nil
}

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

func terminalSend(t *testing.T, conn *websocket.Conn, opcode byte, payload []byte) {
	t.Helper()
	if err := websocket.Message.Send(conn, append([]byte{opcode}, payload...)); err != nil {
		t.Fatalf("send frame: %v", err)
	}
}

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

func terminalProcessAlive(pid int) bool {
	if runtime.GOOS == "linux" {
		raw, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
		if err != nil {
			return false
		}
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

type terminalOpened struct {
	Shell       string `json:"shell"`
	Cols        uint16 `json:"cols"`
	Rows        uint16 `json:"rows"`
	ServiceUser string `json:"service_user"`
}

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

func TestTerminalRealStackNoLeak(t *testing.T) {
	requireE2E(t)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

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

	sessionID, sessionPath := terminalCreate(t, client, httpServer.URL, serverID.String(),
		`{"target":"host","cols":100,"rows":30}`)
	conn, opened := terminalAttach(t, httpServer.URL, sessionPath)
	t.Logf("opened: shell=%s service_user=%s %dx%d", opened.Shell, opened.ServiceUser, opened.Cols, opened.Rows)

	terminalSend(t, conn, 0x01, []byte("echo "+canary+"\r"))
	terminalReadUntil(t, conn, canary)
	resize := make([]byte, 4)
	binary.BigEndian.PutUint16(resize[0:2], 90)
	binary.BigEndian.PutUint16(resize[2:4], 28)
	terminalSend(t, conn, 0x02, resize)
	terminalSend(t, conn, 0x01, []byte("stty size; pwd; id -un\r"))
	terminalReadUntil(t, conn, "28 90")

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
		if escape.StatusCode < 400 {
			t.Fatalf("create escaping cwd = %d", escape.StatusCode)
		}
	} else {
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

	terminalSend(t, conn, 0x03, nil)
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

	if _, lookErr := exec.LookPath("setsid"); lookErr != nil {
		t.Log("setsid is not available; the escaped-job fd regression did not run (Linux CI runs it)")
	} else {
		escapedPIDPath := filepath.Join(root, "escaped.pid")
		_, escapedPath := terminalCreate(t, client, httpServer.URL, serverID.String(),
			`{"target":"host","cols":80,"rows":24}`)
		escaped, _ := terminalAttach(t, httpServer.URL, escapedPath)
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

	if _, err := svc.Attach(ctx, userID, serverID, uuid.MustParse(sessionID)); err == nil {
		t.Fatal("closed session is still attachable")
	}
}
