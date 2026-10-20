package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
)

// testContainerID is a full lowercase Docker id (the only shape the agent accepts).
const testContainerID = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

// fakeContainerExec hands the terminal server one controllable exec session.
type fakeContainerExec struct {
	sessions chan *fakeExecSession
	err      error
}

func (f *fakeContainerExec) Exec(_ context.Context, containerID string, opts ExecOptions) (ExecSession, error) {
	if f.err != nil {
		return nil, f.err
	}
	reader, writer := io.Pipe()
	session := &fakeExecSession{
		containerID: containerID,
		reader:      reader,
		output:      writer,
		cols:        opts.Cols,
		rows:        opts.Rows,
		closed:      make(chan struct{}),
		exitCode:    0,
	}
	f.sessions <- session
	return session, nil
}

type fakeExecSession struct {
	containerID string
	reader      *io.PipeReader
	output      *io.PipeWriter

	mu       sync.Mutex
	input    strings.Builder
	cols     uint16
	rows     uint16
	exitCode int32
	closeErr error
	killed   bool
	closed   chan struct{}
	once     sync.Once
}

// Kill records that the exec process tree was ended. The fake's Close is
// independent so the test can assert the terminal calls Kill before releasing.
func (s *fakeExecSession) Kill(context.Context) error {
	s.mu.Lock()
	s.killed = true
	s.mu.Unlock()
	return nil
}

func (s *fakeExecSession) Read(p []byte) (int, error) { return s.reader.Read(p) }
func (s *fakeExecSession) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.input.Write(p)
}

func (s *fakeExecSession) Close() error {
	s.once.Do(func() {
		s.closeErr = s.reader.Close()
		close(s.closed)
	})
	return s.closeErr
}

func (s *fakeExecSession) Resize(_ context.Context, cols, rows uint16) error {
	s.mu.Lock()
	s.cols, s.rows = cols, rows
	s.mu.Unlock()
	return nil
}

func (s *fakeExecSession) ExitCode(context.Context) (int32, string, error) {
	<-s.closed
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.exitCode, "", nil
}

func (s *fakeExecSession) Shell() string { return "/bin/sh" }
func (s *fakeExecSession) User() string  { return "app" }

func (s *fakeExecSession) dimensions() (uint16, uint16) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cols, s.rows
}

// TestTerminalContainerStreamsResizesAndExits covers the transport contract for
// a container target with a faithful fake exec: output, input, resize and the
// final exit event travel on the same stream as a host shell.
func TestTerminalContainerStreamsResizesAndExits(t *testing.T) {
	exec := &fakeContainerExec{sessions: make(chan *fakeExecSession, 1)}
	terminal := NewTerminalServer(TerminalConfig{
		Logger:     discardLogger(),
		Containers: exec,
	})
	client := startTerminalServer(t, terminal)

	stream, err := client.OpenTerminal(context.Background())
	if err != nil {
		t.Fatalf("OpenTerminal: %v", err)
	}
	if err := stream.Send(&agentv1.TerminalRequest{Part: &agentv1.TerminalRequest_Open{Open: &agentv1.TerminalOpen{
		Target: "container", ContainerId: testContainerID, Cols: 80, Rows: 24,
	}}}); err != nil {
		t.Fatalf("send open: %v", err)
	}
	opened, err := stream.Recv()
	if err != nil {
		t.Fatalf("recv opened: %v", err)
	}
	if got := opened.GetOpened(); got == nil || got.GetShell() != "/bin/sh" || got.GetServiceUser() != "app" {
		t.Fatalf("opened = %+v, want shell /bin/sh as user app", opened.GetOpened())
	}
	session := <-exec.sessions

	if _, err := session.output.Write([]byte("container-ready\r\n")); err != nil {
		t.Fatalf("write output: %v", err)
	}
	waitForOutput(t, stream, "container-ready")

	if err := stream.Send(&agentv1.TerminalRequest{Part: &agentv1.TerminalRequest_Input{Input: []byte("uname\r")}}); err != nil {
		t.Fatalf("send input: %v", err)
	}
	waitForCondition(t, 5*time.Second, func() bool {
		session.mu.Lock()
		defer session.mu.Unlock()
		return strings.Contains(session.input.String(), "uname\r")
	}, "input did not reach the container exec")

	if err := stream.Send(&agentv1.TerminalRequest{Part: &agentv1.TerminalRequest_Resize{
		Resize: &agentv1.TerminalResize{Cols: 120, Rows: 40},
	}}); err != nil {
		t.Fatalf("send resize: %v", err)
	}
	waitForCondition(t, 5*time.Second, func() bool {
		cols, rows := session.dimensions()
		return cols == 120 && rows == 40
	}, "resize did not reach the container exec")

	if err := stream.Send(&agentv1.TerminalRequest{Part: &agentv1.TerminalRequest_Close{
		Close: &agentv1.TerminalClose{Reason: "test"},
	}}); err != nil {
		t.Fatalf("send close: %v", err)
	}
	exited := recvUntil(t, stream, func(response *agentv1.TerminalResponse) bool {
		return response.GetExited() != nil
	})
	if exited.GetExited().GetExitCode() != 0 {
		t.Fatalf("exit code = %d, want 0", exited.GetExited().GetExitCode())
	}
}

// TestTerminalContainerErrorsAreActionable proves missing/stopped images and
// Docker failures map onto stable gRPC codes and phrases.
func TestTerminalContainerErrorsAreActionable(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want codes.Code
		text string
	}{
		{"missing container", fmt.Errorf("%w: gone", ErrDockerNotFound), codes.NotFound, "no longer exists"},
		{"stopped container", fmt.Errorf("%w: exited", ErrDockerContainerNotRunning), codes.FailedPrecondition, "not running"},
		{"no shell", fmt.Errorf("%w: distroless", ErrDockerNoShell), codes.FailedPrecondition, "no supported shell"},
		{"docker unavailable", fmt.Errorf("%w: socket", ErrDockerUnavailable), codes.Unavailable, "docker daemon"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			exec := &fakeContainerExec{sessions: make(chan *fakeExecSession, 1), err: testCase.err}
			terminal := NewTerminalServer(TerminalConfig{Logger: discardLogger(), Containers: exec})
			client := startTerminalServer(t, terminal)
			stream, err := client.OpenTerminal(context.Background())
			if err != nil {
				t.Fatalf("OpenTerminal: %v", err)
			}
			if err := stream.Send(&agentv1.TerminalRequest{Part: &agentv1.TerminalRequest_Open{Open: &agentv1.TerminalOpen{
				Target: "container", ContainerId: testContainerID, Cols: 80, Rows: 24,
			}}}); err != nil {
				t.Fatalf("send open: %v", err)
			}
			_, err = stream.Recv()
			if status.Code(err) != testCase.want {
				t.Fatalf("code = %v, want %v (%v)", status.Code(err), testCase.want, err)
			}
			if !strings.Contains(status.Convert(err).Message(), testCase.text) {
				t.Fatalf("message = %q, want it to contain %q", status.Convert(err).Message(), testCase.text)
			}
		})
	}
}

// TestDockerClientExecHijacksStream covers the Docker Engine client directly:
// container inspect, shell probe, interactive exec, resize and exit status.
func TestDockerClientExecHijacksStream(t *testing.T) {
	api := newFakeExecAPI(t)
	client, err := NewDockerClient(api.server.URL)
	if err != nil {
		t.Fatalf("NewDockerClient: %v", err)
	}

	session, err := client.Exec(context.Background(), "c1", ExecOptions{Cols: 100, Rows: 30})
	if err != nil {
		t.Fatalf("Exec: %v", err)
	}
	if session.Shell() != "/bin/sh" {
		t.Errorf("shell = %q, want /bin/sh", session.Shell())
	}
	if session.User() != "app" {
		t.Errorf("user = %q, want app", session.User())
	}
	line, err := bufio.NewReader(session).ReadString('\n')
	if err != nil {
		t.Fatalf("read banner: %v", err)
	}
	if !strings.Contains(line, "ready") {
		t.Fatalf("banner = %q, want the exec output", line)
	}
	if _, err := session.Write([]byte("echo hi\n")); err != nil {
		t.Fatalf("write input: %v", err)
	}
	if err := session.Resize(context.Background(), 132, 43); err != nil {
		t.Fatalf("Resize: %v", err)
	}
	if err := session.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	code, signal, err := session.ExitCode(context.Background())
	if err != nil || code != 7 || signal != "" {
		t.Fatalf("exit = (%d, %q, %v), want (7, \"\", nil)", code, signal, err)
	}

	if got := api.resize(); got != "w=132&h=43" && got != "h=43&w=132" {
		t.Fatalf("resize query = %q", got)
	}
	api.mu.Lock()
	creates := append([][]string(nil), api.commands...)
	api.mu.Unlock()
	input := api.input()
	if len(creates) != 3 {
		t.Fatalf("exec creates = %d, want the probe, the interactive exec and the kill helper", len(creates))
	}
	// The probe must be the fixed shell-probe command, never caller input.
	if len(creates[0]) != 3 || creates[0][0] != "/bin/sh" || creates[0][1] != "-c" || creates[0][2] != shellProbeCommand {
		t.Fatalf("probe command = %v", creates[0])
	}
	if len(creates[1]) != 5 || creates[1][0] != "/bin/sh" || creates[1][1] != "-c" ||
		creates[1][2] != execWrapperCommand || creates[1][3] != "gotham-terminal" || creates[1][4] != "/bin/sh" {
		t.Fatalf("interactive command = %v, want the fixed wrapper plus the probed shell", creates[1])
	}
	if len(creates[2]) != 5 || creates[2][0] != "/bin/sh" || creates[2][1] != "-c" ||
		creates[2][2] != execKillTreeScript || creates[2][3] != "gotham-terminal-kill" {
		t.Fatalf("kill command = %v, want the fixed kill helper", creates[2])
	}
	if _, err := strconv.Atoi(creates[2][4]); err != nil {
		t.Fatalf("kill pid = %q, want a decimal process id", creates[2][4])
	}
	if input != "echo hi\n" {
		t.Fatalf("exec input = %q, want the written bytes", input)
	}
}

// TestDockerClientExecRejectsStoppedContainer covers the explicit precondition.
func TestDockerClientExecRejectsStoppedContainer(t *testing.T) {
	api := newFakeExecAPI(t)
	api.running = false
	client, err := NewDockerClient(api.server.URL)
	if err != nil {
		t.Fatalf("NewDockerClient: %v", err)
	}
	if _, err := client.Exec(context.Background(), "c1", ExecOptions{Cols: 80, Rows: 24}); !errors.Is(err, ErrDockerContainerNotRunning) {
		t.Fatalf("Exec(stopped) = %v, want ErrDockerContainerNotRunning", err)
	}
}

// TestDockerClientExecReportsMissingShell covers an image with no POSIX shell.
func TestDockerClientExecReportsMissingShell(t *testing.T) {
	api := newFakeExecAPI(t)
	api.probeExit = 127
	api.probeOutput = ""
	client, err := NewDockerClient(api.server.URL)
	if err != nil {
		t.Fatalf("NewDockerClient: %v", err)
	}
	if _, err := client.Exec(context.Background(), "c1", ExecOptions{Cols: 80, Rows: 24}); !errors.Is(err, ErrDockerNoShell) {
		t.Fatalf("Exec(no shell) = %v, want ErrDockerNoShell", err)
	}
}

// TestDockerClientExecRealContainer is the required live integration check: it
// starts a throwaway container on the local Docker daemon, opens an interactive
// exec, runs a command, resizes it, closes the exec and removes the container.
// It never reuses the shared development Postgres/Redis containers.
func TestDockerClientExecRealContainer(t *testing.T) {
	if os.Getenv("GOTHAM_E2E") != "1" {
		t.Skip("set GOTHAM_E2E=1 to run the live container exec test")
	}
	client, err := NewDockerClient("")
	if err != nil {
		t.Skipf("docker client: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	if _, err := client.Version(ctx); err != nil {
		t.Skipf("docker daemon unavailable: %v", err)
	}
	if _, err := client.PullImage(ctx, "alpine:3", "", ""); err != nil {
		t.Logf("pull alpine:3: %v (using a local image if present)", err)
	}
	name := "gotham-exec-test-" + uuid.NewString()[:8]
	id, err := client.RunImage(ctx, &agentv1.CreateContainerRequest{
		Image:   "alpine:3",
		Name:    name,
		Command: []string{"sleep", "300"},
	})
	if err != nil {
		t.Skipf("run throwaway container: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanupCancel()
		_ = client.Remove(cleanupCtx, id)
	})

	session, err := client.Exec(ctx, id, ExecOptions{Cols: 100, Rows: 30})
	if err != nil {
		t.Fatalf("Exec: %v", err)
	}
	if _, err := session.Write([]byte("echo container-exec-ok; exit 0\n")); err != nil {
		t.Fatalf("write: %v", err)
	}
	output := readExecUntil(session, "container-exec-ok", 15*time.Second)
	if !strings.Contains(output, "container-exec-ok") {
		t.Fatalf("output = %q, want the command result", output)
	}
	if err := session.Resize(ctx, 132, 43); err != nil {
		t.Fatalf("resize: %v", err)
	}
	if err := session.Close(); err != nil {
		t.Fatalf("close exec: %v", err)
	}
	code, _, err := session.ExitCode(ctx)
	if err != nil {
		t.Fatalf("exit code: %v", err)
	}
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	// Closing the exec must not stop the application container.
	if _, err := client.inspectContainer(ctx, id); err != nil {
		t.Fatalf("inspect after exec close: %v", err)
	}
}

// readExecUntil reads until marker appears or the deadline expires.
func readExecUntil(reader io.Reader, marker string, timeout time.Duration) string {
	done := make(chan string, 1)
	go func() {
		var builder strings.Builder
		buffer := make([]byte, 4096)
		for {
			n, err := reader.Read(buffer)
			if n > 0 {
				builder.Write(buffer[:n])
				if strings.Contains(builder.String(), marker) {
					done <- builder.String()
					return
				}
			}
			if err != nil {
				done <- builder.String()
				return
			}
		}
	}()
	select {
	case output := <-done:
		return output
	case <-time.After(timeout):
		return ""
	}
}

// fakeExecAPI is a minimal Docker Engine API for the exec path. The start
// endpoint hijacks the connection exactly like the daemon.
type fakeExecAPI struct {
	server *httptest.Server

	mu          sync.Mutex
	running     bool
	probeExit   int32
	probeOutput string
	commands    [][]string
	inputs      strings.Builder
	resizeQuery string
}

func newFakeExecAPI(t *testing.T) *fakeExecAPI {
	t.Helper()
	api := &fakeExecAPI{running: true, probeOutput: "/bin/sh\n"}
	mux := http.NewServeMux()
	mux.HandleFunc("/containers/c1/json", func(w http.ResponseWriter, _ *http.Request) {
		api.mu.Lock()
		running := api.running
		api.mu.Unlock()
		writeJSON(t, w, map[string]any{
			"State":  map[string]any{"Running": running, "Status": "exited"},
			"Config": map[string]any{"User": "app", "WorkingDir": "/srv"},
		})
	})
	mux.HandleFunc("/containers/c1/exec", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Cmd []string `json:"Cmd"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		api.mu.Lock()
		api.commands = append(api.commands, body.Cmd)
		id := fmt.Sprintf("exec%d", len(api.commands))
		api.mu.Unlock()
		writeJSON(t, w, map[string]string{"Id": id})
	})
	start := func(w http.ResponseWriter, r *http.Request) {
		id := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/exec/"), "/start")
		hijacker, ok := w.(http.Hijacker)
		if !ok {
			t.Errorf("test server cannot hijack")
			return
		}
		conn, buffer, err := hijacker.Hijack()
		if err != nil {
			t.Errorf("hijack: %v", err)
			return
		}
		defer func() { _ = conn.Close() }()
		_, _ = buffer.WriteString("HTTP/1.1 101 Switching Protocols\r\nConnection: Upgrade\r\nUpgrade: tcp\r\n\r\n")
		_ = buffer.Flush()
		if id == "exec1" {
			api.mu.Lock()
			output, code := api.probeOutput, api.probeExit
			api.mu.Unlock()
			_, _ = conn.Write([]byte(output))
			api.mu.Lock()
			api.probeExit = code
			api.mu.Unlock()
			return
		}
		if id != "exec2" {
			// The kill helper runs non-interactively and exits immediately.
			return
		}
		_, _ = conn.Write([]byte("1234\nshell-ready\r\n"))
		reader := bufio.NewReader(conn)
		for {
			line, err := reader.ReadString('\n')
			if line != "" {
				api.mu.Lock()
				api.inputs.WriteString(line)
				api.mu.Unlock()
			}
			if err != nil {
				return
			}
		}
	}
	mux.HandleFunc("/exec/", func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/start"):
			start(w, r)
		case strings.HasSuffix(r.URL.Path, "/resize"):
			api.mu.Lock()
			api.resizeQuery = r.URL.RawQuery
			api.mu.Unlock()
			w.WriteHeader(http.StatusOK)
		default:
			id := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/exec/"), "/json")
			code := int32(0)
			if id == "exec2" {
				code = 7
			}
			api.mu.Lock()
			if id == "exec1" {
				code = api.probeExit
			}
			api.mu.Unlock()
			writeJSON(t, w, map[string]any{"ExitCode": code, "Running": false})
		}
	})
	api.server = httptest.NewServer(mux)
	t.Cleanup(api.server.Close)
	return api
}

func (a *fakeExecAPI) resize() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.resizeQuery
}

func (a *fakeExecAPI) input() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.inputs.String()
}

// waitForCondition polls cond until it is true or the deadline expires.
func waitForCondition(t *testing.T, timeout time.Duration, cond func() bool, message string) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal(message)
}
