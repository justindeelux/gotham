package terminal

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"

	"github.com/justindeelux/gotham/agent/files"
	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
)

func terminalTestFixture(t *testing.T) (*files.FileServer, *TerminalServer, string, string) {
	t.Helper()
	root := t.TempDir()
	state := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "file.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "escape")); err != nil {
		t.Fatal(err)
	}
	cfg := files.Config{
		FileRoots:         root,
		ManagedVolumeRoot: filepath.Join(state, "volumes"),
		ComposeRoot:       filepath.Join(state, "compose"),
		CertDir:           filepath.Join(state, "certs"),
	}
	fileServer, err := files.NewFileServer(cfg, discardLogger())
	if err != nil {
		t.Fatalf("NewFileServer: %v", err)
	}
	t.Cleanup(func() { _ = fileServer.Close() })
	roots, err := fileServer.ListRoots(context.Background(), &agentv1.ListRootsRequest{})
	if err != nil || len(roots.GetRoots()) == 0 {
		t.Fatalf("ListRoots: %v", err)
	}
	rootName := roots.GetRoots()[0].GetName()
	terminal := NewTerminalServer(TerminalConfig{
		Logger: discardLogger(),
		Roots:  fileServer,
		Shell:  testShell(t),
	})
	return fileServer, terminal, rootName, root
}

func testShell(t *testing.T) string {
	t.Helper()
	for _, candidate := range []string{"/bin/sh", "/bin/bash", "/bin/dash"} {
		if info, err := os.Stat(candidate); err == nil && info.Mode().IsRegular() {
			return candidate
		}
	}
	t.Skip("no POSIX shell installed")
	return ""
}

func startTerminalServer(t *testing.T, sessions *TerminalServer) agentv1.TerminalServiceClient {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	server := grpc.NewServer()
	agentv1.RegisterTerminalServiceServer(server, sessions)
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(server.Stop)
	conn, err := grpc.NewClient(listener.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return agentv1.NewTerminalServiceClient(conn)
}

type terminalResult struct {
	response *agentv1.TerminalResponse
	err      error
}

func recvUntil(t *testing.T, stream agentv1.TerminalService_OpenTerminalClient, pred func(*agentv1.TerminalResponse) bool) *agentv1.TerminalResponse {
	t.Helper()
	const timeout = 10 * time.Second
	results := make(chan terminalResult, 1)
	go func() {
		var output strings.Builder
		for {
			response, err := stream.Recv()
			if err != nil {
				results <- terminalResult{err: err}
				return
			}
			if chunk := response.GetOutput(); len(chunk) > 0 {
				output.Write(chunk)
			}
			if pred(response) {
				results <- terminalResult{response: response}
				return
			}
		}
	}()
	select {
	case result := <-results:
		if result.err != nil {
			t.Fatalf("terminal stream: %v", result.err)
		}
		return result.response
	case <-time.After(timeout):
		t.Fatalf("terminal stream timed out after %s", timeout)
		return nil
	}
}

func waitForOutput(t *testing.T, stream agentv1.TerminalService_OpenTerminalClient, marker string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	var accumulated strings.Builder
	for time.Now().Before(deadline) {
		type received struct {
			response *agentv1.TerminalResponse
			err      error
		}
		results := make(chan received, 1)
		go func() {
			response, err := stream.Recv()
			results <- received{response: response, err: err}
		}()
		var value received
		select {
		case value = <-results:
		case <-time.After(time.Until(deadline)):
			t.Fatalf("timed out waiting for %q; output %q", marker, accumulated.String())
		}
		if value.err != nil {
			t.Fatalf("waiting for %q: %v; output %q", marker, value.err, accumulated.String())
		}
		if chunk := value.response.GetOutput(); len(chunk) > 0 {
			accumulated.Write(chunk)
			if strings.Contains(accumulated.String(), marker) {
				return
			}
		}
		if exited := value.response.GetExited(); exited != nil {
			t.Fatalf("shell exited (code %d, signal %q) before %q; output %q",
				exited.GetExitCode(), exited.GetSignal(), marker, accumulated.String())
		}
	}
	t.Fatalf("timed out waiting for %q; output %q", marker, accumulated.String())
}

func TestTerminalServerWaitsForCleanup(t *testing.T) {
	terminal := NewTerminalServer(TerminalConfig{Logger: discardLogger()})
	started := make(chan struct{})
	release := make(chan struct{})
	terminal.walks.Add(1)
	go func() {
		defer terminal.walks.Done()
		close(started)
		<-release
	}()
	<-started
	if terminal.WaitTerminations(50 * time.Millisecond) {
		t.Fatal("WaitTerminations returned before the walk finished")
	}
	close(release)
	if !terminal.WaitTerminations(2 * time.Second) {
		t.Fatal("WaitTerminations did not observe the finished walk")
	}
}

func TestTerminalRequiresOpenFirst(t *testing.T) {
	t.Parallel()
	_, terminal, _, _ := terminalTestFixture(t)
	client := startTerminalServer(t, terminal)
	stream, err := client.OpenTerminal(context.Background())
	if err != nil {
		t.Fatalf("OpenTerminal: %v", err)
	}
	if err := stream.Send(&agentv1.TerminalRequest{
		Part: &agentv1.TerminalRequest_Input{Input: []byte("id\n")},
	}); err != nil {
		t.Fatalf("send input: %v", err)
	}
	_, err = stream.Recv()
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("code = %v, want InvalidArgument (%v)", status.Code(err), err)
	}
}

func TestTerminalOpenValidation(t *testing.T) {
	t.Parallel()
	_, terminal, rootName, _ := terminalTestFixture(t)
	client := startTerminalServer(t, terminal)

	cases := []struct {
		name string
		open *agentv1.TerminalOpen
		want codes.Code
	}{
		{"unsupported target", &agentv1.TerminalOpen{Target: "docker", Root: rootName, Cols: 80, Rows: 24}, codes.InvalidArgument},
		{"container exec not configured", &agentv1.TerminalOpen{Target: "container", ContainerId: testContainerID, Cols: 80, Rows: 24}, codes.FailedPrecondition},
		{"missing container id", &agentv1.TerminalOpen{Target: "container", Cols: 80, Rows: 24}, codes.InvalidArgument},
		{"container name", &agentv1.TerminalOpen{Target: "container", ContainerId: "web", Cols: 80, Rows: 24}, codes.InvalidArgument},
		{"short container id", &agentv1.TerminalOpen{Target: "container", ContainerId: "abc123def456", Cols: 80, Rows: 24}, codes.InvalidArgument},
		{"upper-case container id", &agentv1.TerminalOpen{Target: "container", ContainerId: "0123456789ABCDEF0123456789abcdef0123456789abcdef0123456789abcdef", Cols: 80, Rows: 24}, codes.InvalidArgument},
		{"zero columns", &agentv1.TerminalOpen{Target: "host", Root: rootName, Cols: 0, Rows: 24}, codes.InvalidArgument},
		{"huge rows", &agentv1.TerminalOpen{Target: "host", Root: rootName, Cols: 80, Rows: 1001}, codes.InvalidArgument},
		{"cwd without root", &agentv1.TerminalOpen{Target: "host", Cwd: "app", Cols: 80, Rows: 24}, codes.InvalidArgument},
		{"over-long root", &agentv1.TerminalOpen{Target: "host", Root: strings.Repeat("a", terminalMaxRootBytes+1), Cols: 80, Rows: 24}, codes.InvalidArgument},
		{"unknown root", &agentv1.TerminalOpen{Target: "host", Root: "nope", Cols: 80, Rows: 24}, codes.InvalidArgument},
		{"absolute cwd", &agentv1.TerminalOpen{Target: "host", Root: rootName, Cwd: "/etc", Cols: 80, Rows: 24}, codes.InvalidArgument},
		{"parent escape", &agentv1.TerminalOpen{Target: "host", Root: rootName, Cwd: "../x", Cols: 80, Rows: 24}, codes.InvalidArgument},
		{"symlink escape", &agentv1.TerminalOpen{Target: "host", Root: rootName, Cwd: "escape", Cols: 80, Rows: 24}, codes.InvalidArgument},
		{"missing cwd", &agentv1.TerminalOpen{Target: "host", Root: rootName, Cwd: "missing", Cols: 80, Rows: 24}, codes.NotFound},
		{"file cwd", &agentv1.TerminalOpen{Target: "host", Root: rootName, Cwd: "file.txt", Cols: 80, Rows: 24}, codes.InvalidArgument},
		{"nul cwd", &agentv1.TerminalOpen{Target: "host", Root: rootName, Cwd: "a\x00b", Cols: 80, Rows: 24}, codes.InvalidArgument},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			stream, err := client.OpenTerminal(context.Background())
			if err != nil {
				t.Fatalf("OpenTerminal: %v", err)
			}
			if err := stream.Send(&agentv1.TerminalRequest{
				Part: &agentv1.TerminalRequest_Open{Open: testCase.open},
			}); err != nil {
				t.Fatalf("send open: %v", err)
			}
			_, err = stream.Recv()
			if status.Code(err) != testCase.want {
				t.Fatalf("code = %v, want %v (%v)", status.Code(err), testCase.want, err)
			}
		})
	}
}

func TestTerminalDefaultWorkingDirectory(t *testing.T) {
	_, terminal, _, _ := terminalTestFixture(t)
	client := startTerminalServer(t, terminal)
	stream, err := client.OpenTerminal(context.Background())
	if err != nil {
		t.Fatalf("OpenTerminal: %v", err)
	}
	defer func() { _ = stream.CloseSend() }()
	if err := stream.Send(&agentv1.TerminalRequest{Part: &agentv1.TerminalRequest_Open{Open: &agentv1.TerminalOpen{
		Target: "host", Cols: 80, Rows: 24,
	}}}); err != nil {
		t.Fatalf("send open: %v", err)
	}
	if _, err := stream.Recv(); err != nil {
		t.Fatalf("recv opened without a root: %v", err)
	}
	expected := resolveDefaultTerminalDir(serviceHomeDir())
	if err := stream.Send(&agentv1.TerminalRequest{Part: &agentv1.TerminalRequest_Input{
		Input: []byte("printf 'GOTHAM_PWD=%s\\n' \"$PWD\"\r"),
	}}); err != nil {
		t.Fatalf("send pwd: %v", err)
	}
	waitForOutput(t, stream, "GOTHAM_PWD="+expected)
	if err := stream.Send(&agentv1.TerminalRequest{Part: &agentv1.TerminalRequest_Input{Input: []byte("exit 0\r")}}); err != nil {
		t.Fatalf("send exit: %v", err)
	}
	exited := recvUntil(t, stream, func(response *agentv1.TerminalResponse) bool {
		return response.GetExited() != nil
	})
	if exited.GetExited().GetExitCode() != 0 {
		t.Fatalf("exit code = %d, want 0", exited.GetExited().GetExitCode())
	}
}

func TestTerminalHostWithoutFileRoots(t *testing.T) {
	terminal := NewTerminalServer(TerminalConfig{
		Logger: discardLogger(),
		Shell:  testShell(t),
	})
	client := startTerminalServer(t, terminal)

	named, err := client.OpenTerminal(context.Background())
	if err != nil {
		t.Fatalf("OpenTerminal: %v", err)
	}
	if err := named.Send(&agentv1.TerminalRequest{Part: &agentv1.TerminalRequest_Open{Open: &agentv1.TerminalOpen{
		Target: "host", Root: "volumes", Cols: 80, Rows: 24,
	}}}); err != nil {
		t.Fatalf("send open with root: %v", err)
	}
	if _, err := named.Recv(); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("open with a root and no file service = %v, want FailedPrecondition", err)
	}

	stream, err := client.OpenTerminal(context.Background())
	if err != nil {
		t.Fatalf("OpenTerminal without root: %v", err)
	}
	defer func() { _ = stream.CloseSend() }()
	if err := stream.Send(&agentv1.TerminalRequest{Part: &agentv1.TerminalRequest_Open{Open: &agentv1.TerminalOpen{
		Target: "host", Cols: 80, Rows: 24,
	}}}); err != nil {
		t.Fatalf("send open: %v", err)
	}
	if _, err := stream.Recv(); err != nil {
		t.Fatalf("recv opened without file roots: %v", err)
	}
	expected := resolveDefaultTerminalDir(serviceHomeDir())
	if err := stream.Send(&agentv1.TerminalRequest{Part: &agentv1.TerminalRequest_Input{
		Input: []byte("printf 'GOTHAM_PWD=%s\\n' \"$PWD\"\r"),
	}}); err != nil {
		t.Fatalf("send pwd: %v", err)
	}
	waitForOutput(t, stream, "GOTHAM_PWD="+expected)
}

func TestTerminalCwdWithoutRootDoesNotStart(t *testing.T) {
	_, terminal, _, _ := terminalTestFixture(t)
	started := 0
	terminal.start = func(terminalStart) (terminalProcess, error) {
		started++
		return nil, errors.New("a shell must not start")
	}
	client := startTerminalServer(t, terminal)
	stream, err := client.OpenTerminal(context.Background())
	if err != nil {
		t.Fatalf("OpenTerminal: %v", err)
	}
	if err := stream.Send(&agentv1.TerminalRequest{Part: &agentv1.TerminalRequest_Open{Open: &agentv1.TerminalOpen{
		Target: "host", Cwd: "app", Cols: 80, Rows: 24,
	}}}); err != nil {
		t.Fatalf("send open: %v", err)
	}
	if _, err := stream.Recv(); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("cwd without root = %v, want InvalidArgument", err)
	}
	if started != 0 {
		t.Fatalf("started = %d shells, want 0", started)
	}
}

func TestResolveDefaultTerminalDir(t *testing.T) {
	base := t.TempDir()
	home := filepath.Join(base, "home")
	if err := os.Mkdir(home, 0o755); err != nil {
		t.Fatal(err)
	}
	resolvedHome, err := filepath.EvalSymlinks(home)
	if err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(base, "home-link")
	if err := os.Symlink(home, link); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(base, "file")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	fileLink := filepath.Join(base, "file-link")
	if err := os.Symlink(file, fileLink); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name string
		home string
		want string
	}{
		{"directory", home, resolvedHome},
		{"symlink to directory", link, resolvedHome},
		{"empty", "", "/"},
		{"relative", "home", "/"},
		{"missing", filepath.Join(base, "missing"), "/"},
		{"file", file, "/"},
		{"symlink to file", fileLink, "/"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := resolveDefaultTerminalDir(testCase.home); got != testCase.want {
				t.Fatalf("resolveDefaultTerminalDir(%q) = %q, want %q", testCase.home, got, testCase.want)
			}
		})
	}

	t.Run("unsearchable directory", func(t *testing.T) {
		if os.Geteuid() == 0 {
			t.Skip("root may chdir into a directory without search permission")
		}
		locked := filepath.Join(base, "locked")
		if err := os.Mkdir(locked, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(locked, 0o000); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(locked, 0o700) })
		if got := resolveDefaultTerminalDir(locked); got != "/" {
			t.Fatalf("resolveDefaultTerminalDir(unsearchable) = %q, want /", got)
		}
	})
}

func TestTerminalEchoResizeAndExit(t *testing.T) {
	_, terminal, rootName, _ := terminalTestFixture(t)
	client := startTerminalServer(t, terminal)
	stream, err := client.OpenTerminal(context.Background())
	if err != nil {
		t.Fatalf("OpenTerminal: %v", err)
	}
	if err := stream.Send(&agentv1.TerminalRequest{Part: &agentv1.TerminalRequest_Open{Open: &agentv1.TerminalOpen{
		Target: "host", Root: rootName, Cwd: "sub", Cols: 80, Rows: 24,
	}}}); err != nil {
		t.Fatalf("send open: %v", err)
	}
	opened, err := stream.Recv()
	if err != nil {
		t.Fatalf("recv opened: %v", err)
	}
	if opened.GetOpened() == nil {
		t.Fatalf("first event = %T, want opened", opened.GetEvent())
	}
	if opened.GetOpened().GetCols() != 80 || opened.GetOpened().GetRows() != 24 {
		t.Fatalf("opened dimensions = %dx%d, want 80x24", opened.GetOpened().GetCols(), opened.GetOpened().GetRows())
	}
	if opened.GetOpened().GetServiceUser() == "" {
		t.Fatal("opened service_user is empty")
	}

	if err := stream.Send(&agentv1.TerminalRequest{Part: &agentv1.TerminalRequest_Input{Input: []byte("echo gotham-pty-ok\r")}}); err != nil {
		t.Fatalf("send input: %v", err)
	}
	waitForOutput(t, stream, "gotham-pty-ok")

	if err := stream.Send(&agentv1.TerminalRequest{Part: &agentv1.TerminalRequest_Input{Input: []byte("echo gotham-pty-é✓ñ\r")}}); err != nil {
		t.Fatalf("send utf8 input: %v", err)
	}
	waitForOutput(t, stream, "gotham-pty-é✓ñ")

	if err := stream.Send(&agentv1.TerminalRequest{Part: &agentv1.TerminalRequest_Resize{Resize: &agentv1.TerminalResize{Cols: 100, Rows: 40}}}); err != nil {
		t.Fatalf("send resize: %v", err)
	}
	if err := stream.Send(&agentv1.TerminalRequest{Part: &agentv1.TerminalRequest_Input{Input: []byte("exit 7\r")}}); err != nil {
		t.Fatalf("send exit: %v", err)
	}
	exited := recvUntil(t, stream, func(response *agentv1.TerminalResponse) bool {
		return response.GetExited() != nil
	})
	if exited.GetExited().GetExitCode() != 7 {
		t.Fatalf("exit code = %d, want 7", exited.GetExited().GetExitCode())
	}
}

func TestTerminalCtrlC(t *testing.T) {
	_, terminal, rootName, _ := terminalTestFixture(t)
	client := startTerminalServer(t, terminal)
	stream, err := client.OpenTerminal(context.Background())
	if err != nil {
		t.Fatalf("OpenTerminal: %v", err)
	}
	defer func() { _ = stream.CloseSend() }()
	if err := stream.Send(&agentv1.TerminalRequest{Part: &agentv1.TerminalRequest_Open{Open: &agentv1.TerminalOpen{
		Target: "host", Root: rootName, Cols: 80, Rows: 24,
	}}}); err != nil {
		t.Fatalf("send open: %v", err)
	}
	if _, err := stream.Recv(); err != nil {
		t.Fatalf("recv opened: %v", err)
	}
	if err := stream.Send(&agentv1.TerminalRequest{Part: &agentv1.TerminalRequest_Input{Input: []byte("sleep 30\r")}}); err != nil {
		t.Fatalf("send sleep: %v", err)
	}
	time.Sleep(300 * time.Millisecond)
	if err := stream.Send(&agentv1.TerminalRequest{Part: &agentv1.TerminalRequest_Input{Input: []byte{0x03}}}); err != nil {
		t.Fatalf("send ctrl-c: %v", err)
	}
	if err := stream.Send(&agentv1.TerminalRequest{Part: &agentv1.TerminalRequest_Input{Input: []byte("echo gotham-after-intr\r")}}); err != nil {
		t.Fatalf("send echo: %v", err)
	}
	waitForOutput(t, stream, "gotham-after-intr")
}

func TestTerminalCloseKillsProcessGroup(t *testing.T) {
	_, terminal, rootName, root := terminalTestFixture(t)
	client := startTerminalServer(t, terminal)
	stream, err := client.OpenTerminal(context.Background())
	if err != nil {
		t.Fatalf("OpenTerminal: %v", err)
	}
	if err := stream.Send(&agentv1.TerminalRequest{Part: &agentv1.TerminalRequest_Open{Open: &agentv1.TerminalOpen{
		Target: "host", Root: rootName, Cols: 80, Rows: 24,
	}}}); err != nil {
		t.Fatalf("send open: %v", err)
	}
	if _, err := stream.Recv(); err != nil {
		t.Fatalf("recv opened: %v", err)
	}
	pidFile := filepath.Join(root, "shell.pid")
	if err := stream.Send(&agentv1.TerminalRequest{Part: &agentv1.TerminalRequest_Input{
		Input: []byte("echo $$ > " + pidFile + "; exec sleep 300\r"),
	}}); err != nil {
		t.Fatalf("send command: %v", err)
	}
	pid := waitForPID(t, pidFile)

	if err := stream.Send(&agentv1.TerminalRequest{Part: &agentv1.TerminalRequest_Close{Close: &agentv1.TerminalClose{Reason: "test_close"}}}); err != nil {
		t.Fatalf("send close: %v", err)
	}
	deadline := time.Now().Add(6 * time.Second)
	for time.Now().Before(deadline) {
		if !processExists(pid) {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("shell process %d still exists after close", pid)
}

func TestTerminalConcurrencyLimit(t *testing.T) {
	_, terminal, rootName, _ := terminalTestFixture(t)
	terminal.sessions = make(chan struct{}, 1)
	client := startTerminalServer(t, terminal)
	first, err := client.OpenTerminal(context.Background())
	if err != nil {
		t.Fatalf("OpenTerminal first: %v", err)
	}
	defer func() { _ = first.CloseSend() }()
	open := &agentv1.TerminalRequest{Part: &agentv1.TerminalRequest_Open{Open: &agentv1.TerminalOpen{
		Target: "host", Root: rootName, Cols: 80, Rows: 24,
	}}}
	if err := first.Send(open); err != nil {
		t.Fatalf("send open: %v", err)
	}
	if _, err := first.Recv(); err != nil {
		t.Fatalf("recv opened: %v", err)
	}

	second, err := client.OpenTerminal(context.Background())
	if err != nil {
		t.Fatalf("OpenTerminal second: %v", err)
	}
	if err := second.Send(open); err != nil {
		t.Fatalf("send open second: %v", err)
	}
	_, err = second.Recv()
	if status.Code(err) != codes.ResourceExhausted {
		t.Fatalf("second session code = %v, want ResourceExhausted", status.Code(err))
	}
}

func TestTerminalInputLimit(t *testing.T) {
	_, terminal, rootName, _ := terminalTestFixture(t)
	terminal.maxInputBytes = 4
	client := startTerminalServer(t, terminal)
	stream, err := client.OpenTerminal(context.Background())
	if err != nil {
		t.Fatalf("OpenTerminal: %v", err)
	}
	if err := stream.Send(&agentv1.TerminalRequest{Part: &agentv1.TerminalRequest_Open{Open: &agentv1.TerminalOpen{
		Target: "host", Root: rootName, Cols: 80, Rows: 24,
	}}}); err != nil {
		t.Fatalf("send open: %v", err)
	}
	if _, err := stream.Recv(); err != nil {
		t.Fatalf("recv opened: %v", err)
	}
	if err := stream.Send(&agentv1.TerminalRequest{Part: &agentv1.TerminalRequest_Input{Input: []byte("12345")}}); err != nil {
		t.Fatalf("send input: %v", err)
	}
	for {
		_, err = stream.Recv()
		if err != nil {
			break
		}
	}
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("code = %v, want InvalidArgument (%v)", status.Code(err), err)
	}
}

func TestResolveShellDir(t *testing.T) {
	fileServer, _, rootName, root := terminalTestFixture(t)
	t.Parallel()
	cases := []struct {
		name    string
		rel     string
		wantErr bool
	}{
		{"root", "", false},
		{"dot", ".", false},
		{"sub", "sub", false},
		{"parent escape", "../", true},
		{"absolute", "/etc", true},
		{"symlink escape", "escape", true},
		{"missing", "missing", true},
		{"file", "file.txt", true},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			got, err := fileServer.ResolveShellDir(rootName, testCase.rel)
			if testCase.wantErr {
				if err == nil {
					t.Fatalf("ResolveShellDir(%q) = %q, want error", testCase.rel, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("ResolveShellDir(%q): %v", testCase.rel, err)
			}
			resolvedRoot, evalErr := filepath.EvalSymlinks(root)
			if evalErr != nil {
				t.Fatal(evalErr)
			}
			if !withinPath(resolvedRoot, got) {
				t.Fatalf("resolved %q outside root %q", got, resolvedRoot)
			}
		})
	}
}

func waitForPID(t *testing.T, path string) int {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(path)
		if err == nil {
			if pid, scanErr := strconv.Atoi(strings.TrimSpace(string(data))); scanErr == nil && pid > 0 {
				return pid
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("pid file %s did not appear", path)
	return 0
}

func processExists(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}

func openFDCount(t *testing.T) int {
	t.Helper()
	for _, directory := range []string{"/proc/self/fd", "/dev/fd"} {
		entries, err := os.ReadDir(directory)
		if err == nil {
			return len(entries)
		}
	}
	t.Skip("cannot count file descriptors on this platform")
	return 0
}

func openTerminalSession(t *testing.T, client agentv1.TerminalServiceClient, rootName string) agentv1.TerminalService_OpenTerminalClient {
	t.Helper()
	stream, err := client.OpenTerminal(context.Background())
	if err != nil {
		t.Fatalf("OpenTerminal: %v", err)
	}
	if err := stream.Send(&agentv1.TerminalRequest{Part: &agentv1.TerminalRequest_Open{Open: &agentv1.TerminalOpen{
		Target: "host", Root: rootName, Cols: 80, Rows: 24,
	}}}); err != nil {
		t.Fatalf("send open: %v", err)
	}
	if _, err := stream.Recv(); err != nil {
		t.Fatalf("recv opened: %v", err)
	}
	return stream
}

func runAgentSession(t *testing.T, client agentv1.TerminalServiceClient, rootName string) {
	t.Helper()
	stream := openTerminalSession(t, client, rootName)
	if err := stream.Send(&agentv1.TerminalRequest{Part: &agentv1.TerminalRequest_Input{Input: []byte("exit 0\r")}}); err != nil {
		t.Fatalf("send exit: %v", err)
	}
	recvUntil(t, stream, func(response *agentv1.TerminalResponse) bool {
		return response.GetExited() != nil
	})
	_ = stream.CloseSend()
	for {
		if _, err := stream.Recv(); err != nil {
			break
		}
	}
}

func TestTerminalSessionIsolation(t *testing.T) {
	_, terminal, rootName, _ := terminalTestFixture(t)
	client := startTerminalServer(t, terminal)
	first := openTerminalSession(t, client, rootName)
	defer func() { _ = first.CloseSend() }()
	second := openTerminalSession(t, client, rootName)
	defer func() { _ = second.CloseSend() }()

	if err := first.Send(&agentv1.TerminalRequest{Part: &agentv1.TerminalRequest_Input{Input: []byte("echo agent-iso-one\r")}}); err != nil {
		t.Fatalf("send first: %v", err)
	}
	waitForOutput(t, first, "agent-iso-one")

	if err := second.Send(&agentv1.TerminalRequest{Part: &agentv1.TerminalRequest_Input{Input: []byte("echo agent-iso-two\r")}}); err != nil {
		t.Fatalf("send second: %v", err)
	}
	waitForOutput(t, second, "agent-iso-two")

	if err := first.Send(&agentv1.TerminalRequest{Part: &agentv1.TerminalRequest_Input{Input: []byte("echo agent-iso-three\r")}}); err != nil {
		t.Fatalf("send third: %v", err)
	}
	var output strings.Builder
	for !strings.Contains(output.String(), "agent-iso-three") {
		response, err := first.Recv()
		if err != nil {
			t.Fatalf("recv first: %v", err)
		}
		output.Write(response.GetOutput())
	}
	if strings.Contains(output.String(), "agent-iso-two") {
		t.Fatalf("cross-session output leak: %q", output.String())
	}
}

func TestTerminalReleasesResources(t *testing.T) {
	_, terminal, rootName, _ := terminalTestFixture(t)
	client := startTerminalServer(t, terminal)
	runAgentSession(t, client, rootName)
	baselineFD := openFDCount(t)
	baselineGoroutines := runtime.NumGoroutine()

	for i := 0; i < 4; i++ {
		runAgentSession(t, client, rootName)
	}

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if openFDCount(t) <= baselineFD+2 && runtime.NumGoroutine() <= baselineGoroutines+2 {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("resources after sessions: fds %d (baseline %d), goroutines %d (baseline %d)",
		openFDCount(t), baselineFD, runtime.NumGoroutine(), baselineGoroutines)
}

func TestTerminalDisconnectKillsProcessGroup(t *testing.T) {
	_, terminal, rootName, root := terminalTestFixture(t)
	client := startTerminalServer(t, terminal)
	ctx, cancel := context.WithCancel(context.Background())
	stream, err := client.OpenTerminal(ctx)
	if err != nil {
		cancel()
		t.Fatalf("OpenTerminal: %v", err)
	}
	if err := stream.Send(&agentv1.TerminalRequest{Part: &agentv1.TerminalRequest_Open{Open: &agentv1.TerminalOpen{
		Target: "host", Root: rootName, Cols: 80, Rows: 24,
	}}}); err != nil {
		cancel()
		t.Fatalf("send open: %v", err)
	}
	if _, err := stream.Recv(); err != nil {
		cancel()
		t.Fatalf("recv opened: %v", err)
	}
	pidFile := filepath.Join(root, "disconnect.pid")
	if err := stream.Send(&agentv1.TerminalRequest{Part: &agentv1.TerminalRequest_Input{
		Input: []byte("echo $$ > " + pidFile + "; exec sleep 300\r"),
	}}); err != nil {
		cancel()
		t.Fatalf("send command: %v", err)
	}
	pid := waitForPID(t, pidFile)

	cancel()
	deadline := time.Now().Add(6 * time.Second)
	for time.Now().Before(deadline) {
		if !processExists(pid) {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("shell process %d still exists after disconnect", pid)
}

func TestResolveShellDirFilesystemRoot(t *testing.T) {
	state := t.TempDir()
	fileServer, err := files.NewFileServer(files.Config{
		FileFullAccess:    true,
		ManagedVolumeRoot: filepath.Join(state, "volumes"),
		ComposeRoot:       filepath.Join(state, "compose"),
		CertDir:           filepath.Join(state, "certs"),
	}, discardLogger())
	if err != nil {
		t.Fatalf("files.NewFileServer(full access): %v", err)
	}
	t.Cleanup(func() { _ = fileServer.Close() })

	roots, err := fileServer.ListRoots(context.Background(), &agentv1.ListRootsRequest{})
	if err != nil || len(roots.GetRoots()) != 1 {
		t.Fatalf("ListRoots: %v, roots=%v", err, roots.GetRoots())
	}
	root := roots.GetRoots()[0]
	if root.GetName() != files.RootSystem || root.GetPath() != "/" {
		t.Fatalf("full-access root = %q at %q, want %q at /", root.GetName(), root.GetPath(), files.RootSystem)
	}

	for _, cwd := range []string{"tmp", "usr", "etc"} {
		got, err := fileServer.ResolveShellDir(root.GetName(), cwd)
		if err != nil {
			t.Fatalf("ResolveShellDir(%q): %v", cwd, err)
		}
		if !withinPath("/", got) {
			t.Fatalf("ResolveShellDir(%q) = %q, outside /", cwd, got)
		}
	}

	for _, cwd := range []string{"../tmp", "tmp/../../etc"} {
		if got, err := fileServer.ResolveShellDir(root.GetName(), cwd); err == nil {
			t.Fatalf("ResolveShellDir(%q) = %q, want an escape refusal", cwd, got)
		}
	}
	if got, err := fileServer.ResolveShellDir(root.GetName(), "/tmp"); err == nil {
		t.Fatalf("ResolveShellDir(absolute) = %q, want a refusal", got)
	}
	if got, err := fileServer.ResolveShellDir(root.GetName(), "gotham-definitely-missing-dir"); err == nil {
		t.Fatalf("ResolveShellDir(missing) = %q, want a refusal", got)
	}
}

func TestTerminalOpenUnderFilesystemRoot(t *testing.T) {
	state := t.TempDir()
	fileServer, err := files.NewFileServer(files.Config{
		FileFullAccess:    true,
		ManagedVolumeRoot: filepath.Join(state, "volumes"),
		ComposeRoot:       filepath.Join(state, "compose"),
		CertDir:           filepath.Join(state, "certs"),
	}, discardLogger())
	if err != nil {
		t.Fatalf("files.NewFileServer(full access): %v", err)
	}
	t.Cleanup(func() { _ = fileServer.Close() })

	terminal := NewTerminalServer(TerminalConfig{
		Logger: discardLogger(),
		Roots:  fileServer,
		Shell:  testShell(t),
	})
	client := startTerminalServer(t, terminal)
	stream, err := client.OpenTerminal(context.Background())
	if err != nil {
		t.Fatalf("OpenTerminal: %v", err)
	}
	if err := stream.Send(&agentv1.TerminalRequest{Part: &agentv1.TerminalRequest_Open{Open: &agentv1.TerminalOpen{
		Target: "host", Root: files.RootSystem, Cwd: "tmp", Cols: 80, Rows: 24,
	}}}); err != nil {
		t.Fatalf("send open: %v", err)
	}
	if _, err := stream.Recv(); err != nil {
		t.Fatalf("recv opened under / with cwd tmp: %v", err)
	}
	if err := stream.Send(&agentv1.TerminalRequest{Part: &agentv1.TerminalRequest_Input{Input: []byte("echo root-cwd-ok\r")}}); err != nil {
		t.Fatalf("send input: %v", err)
	}
	waitForOutput(t, stream, "root-cwd-ok")
	if err := stream.Send(&agentv1.TerminalRequest{Part: &agentv1.TerminalRequest_Input{Input: []byte("exit 0\r")}}); err != nil {
		t.Fatalf("send exit: %v", err)
	}
	exited := recvUntil(t, stream, func(response *agentv1.TerminalResponse) bool {
		return response.GetExited() != nil
	})
	if exited.GetExited().GetExitCode() != 0 {
		t.Fatalf("exit code = %d, want 0", exited.GetExited().GetExitCode())
	}
}
