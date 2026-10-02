package agent

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
)

// composeTestProject is the only project-name shape the agent accepts.
const composeTestProject = "gotham-3f2a4b6c-8d0e-4f1a-9b2c-3d4e5f607182"

const composeTestDocument = `services:
  web:
    image: nginx:1.23
    volumes:
      - data:/data
  worker:
    image: busybox:1.36
volumes:
  data:
`

// fakeDockerCLI installs a fake `docker` binary on PATH that records every
// invocation in a file and answers with canned compose output. Using a script
// keeps the tests on the real exec path (arguments, working directory, DOCKER_HOST,
// exit codes) without a Docker daemon.
func fakeDockerCLI(t *testing.T, script string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "bin")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir fake bin: %v", err)
	}
	logPath := filepath.Join(t.TempDir(), "calls.log")
	body := `#!/bin/sh
{
  echo "args=$@"
  echo "cwd=$PWD"
  echo "docker_host=${DOCKER_HOST:-}"
} >> "$FAKE_DOCKER_CALLS"
` + script + "\n"
	if err := os.WriteFile(filepath.Join(dir, "docker"), []byte(body), 0o755); err != nil {
		t.Fatalf("write fake docker: %v", err)
	}
	t.Setenv("FAKE_DOCKER_CALLS", logPath)
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return logPath
}

// composeCalls returns every recorded invocation.
func composeCalls(t *testing.T, logPath string) []string {
	t.Helper()
	content, err := os.ReadFile(logPath)
	if err != nil {
		return nil
	}
	lines := []string{}
	for _, line := range strings.Split(string(content), "\n") {
		if strings.HasPrefix(line, "args=") {
			lines = append(lines, line)
		}
	}
	return lines
}

// newTestComposeServer builds a ComposeServer rooted in a canonical temp dir
// (macOS /var is a symlink, and the agent's no-follow traversal needs real
// directories).
func newTestComposeServer(t *testing.T, cfg ComposeServerConfig) *ComposeServer {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("canonical temp dir: %v", err)
	}
	cfg.Root = root
	if cfg.Logger == nil {
		cfg.Logger = discardLogger()
	}
	return NewComposeServer(cfg)
}

// TestComposeValidateWritesAndInspects proves validation writes the document
// (mode 0600) into the project directory and returns the CLI's own service and
// volume lists.
func TestComposeValidateWritesAndInspects(t *testing.T) {
	logPath := fakeDockerCLI(t, `
case "$7" in
  --services) printf 'web\nworker\n' ;;
  --volumes) printf 'data\n' ;;
esac
exit 0
`)
	server := newTestComposeServer(t, ComposeServerConfig{})
	response, err := server.ComposeValidate(context.Background(), &agentv1.ComposeValidateRequest{
		ProjectName: composeTestProject,
		ComposeYaml: []byte(composeTestDocument),
	})
	if err != nil {
		t.Fatalf("ComposeValidate: %v", err)
	}
	if strings.Join(response.GetServices(), ",") != "web,worker" {
		t.Errorf("services = %v", response.GetServices())
	}
	if strings.Join(response.GetVolumes(), ",") != "data" {
		t.Errorf("volumes = %v", response.GetVolumes())
	}

	file := filepath.Join(server.root, composeTestProject, "compose.yaml")
	info, err := os.Stat(file)
	if err != nil {
		t.Fatalf("compose file: %v", err)
	}
	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Errorf("compose file mode = %o, want 600", mode)
	}
	content, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("read compose file: %v", err)
	}
	if string(content) != composeTestDocument {
		t.Errorf("compose file content = %q", content)
	}
	calls := composeCalls(t, logPath)
	if len(calls) != 2 {
		t.Fatalf("calls = %v, want config --services and --volumes", calls)
	}
	for _, call := range calls {
		if !strings.Contains(call, "-p "+composeTestProject) {
			t.Errorf("call missing project name: %q", call)
		}
	}
}

// TestComposeProjectValidation proves every path-like project name is rejected
// before anything is written.
func TestComposeProjectValidation(t *testing.T) {
	for _, name := range []string{
		"",
		"../../etc",
		"/etc",
		"gotham-../etc",
		"gotham-3F2A4B6C-8D0E-4F1A-9B2C-3D4E5F607182",
		"other-3f2a4b6c-8d0e-4f1a-9b2c-3d4e5f607182",
		"gotham-not-a-uuid",
	} {
		t.Run(name, func(t *testing.T) {
			server := newTestComposeServer(t, ComposeServerConfig{})
			_, err := server.ComposeValidate(context.Background(), &agentv1.ComposeValidateRequest{
				ProjectName: name,
				ComposeYaml: []byte(composeTestDocument),
			})
			if status.Code(err) != codes.InvalidArgument {
				t.Fatalf("code = %v, want InvalidArgument (%v)", status.Code(err), err)
			}
		})
	}
}

// TestComposeInputBounds proves the document size and emptiness are bounded.
func TestComposeInputBounds(t *testing.T) {
	server := newTestComposeServer(t, ComposeServerConfig{})
	if _, err := server.ComposeValidate(context.Background(), &agentv1.ComposeValidateRequest{
		ProjectName: composeTestProject,
	}); status.Code(err) != codes.InvalidArgument {
		t.Errorf("empty document code = %v, want InvalidArgument", status.Code(err))
	}
	oversized := make([]byte, maxComposeYAML+1)
	for i := range oversized {
		oversized[i] = 'x'
	}
	if _, err := server.ComposeValidate(context.Background(), &agentv1.ComposeValidateRequest{
		ProjectName: composeTestProject,
		ComposeYaml: oversized,
	}); status.Code(err) != codes.InvalidArgument {
		t.Errorf("oversized document code = %v, want InvalidArgument", status.Code(err))
	}
}

// TestAgentRecvCapExceedsComposeLimit is the FX-3 R2/C3 guard: a compose
// document at the application limit must survive gRPC framing and be rejected by
// its own validation (InvalidArgument), not by the transport's receive cap
// (ResourceExhausted). It also proves the receive cap still rejects a message
// well past it.
func TestAgentRecvCapExceedsComposeLimit(t *testing.T) {
	if agentMaxRecvMsgSize <= maxComposeYAML {
		t.Fatalf("agentMaxRecvMsgSize = %d, must exceed maxComposeYAML = %d", agentMaxRecvMsgSize, maxComposeYAML)
	}

	fake := &fakeDockerClient{}
	creds, err := ServerCredentials(nil, nil, "", true)
	if err != nil {
		t.Fatalf("ServerCredentials: %v", err)
	}
	server, err := NewServer("127.0.0.1:0", creds, NewDockerServer(fake, discardLogger()), discardLogger(),
		WithComposeService(newTestComposeServer(t, ComposeServerConfig{})))
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = server.Serve(ctx) }()
	t.Cleanup(func() { cancel() })

	conn, err := grpc.NewClient(server.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = conn.Close() }()

	callCtx, callCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer callCancel()
	client := agentv1.NewComposeServiceClient(conn)

	// A document one byte over the application limit is rejected by validation.
	justOver := make([]byte, maxComposeYAML+1)
	for i := range justOver {
		justOver[i] = 'x'
	}
	if _, err := client.ComposeValidate(callCtx, &agentv1.ComposeValidateRequest{
		ProjectName: composeTestProject,
		ComposeYaml: justOver,
	}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("just-over-limit document = %v, want InvalidArgument (not a transport cap)", err)
	}

	// A payload one byte past the transport receive cap is rejected by gRPC.
	overCap := make([]byte, agentMaxRecvMsgSize+1)
	for i := range overCap {
		overCap[i] = 'x'
	}
	if _, err := client.ComposeValidate(callCtx, &agentv1.ComposeValidateRequest{
		ProjectName: composeTestProject,
		ComposeYaml: overCap,
	}); status.Code(err) != codes.ResourceExhausted {
		t.Fatalf("over-cap payload = %v, want ResourceExhausted", err)
	}

	// A document exactly at the application limit fits the receive cap and is
	// validated as a document (it is not valid compose, so it fails on content,
	// never on transport size).
	exact := make([]byte, maxComposeYAML)
	copy(exact, "services:\n  web:\n    image: nginx\n")
	for i := len("services:\n  web:\n    image: nginx\n"); i < len(exact); i++ {
		exact[i] = ' '
	}
	if _, err := client.ComposeValidate(callCtx, &agentv1.ComposeValidateRequest{
		ProjectName: composeTestProject,
		ComposeYaml: exact,
	}); status.Code(err) == codes.ResourceExhausted {
		t.Fatalf("exact-limit document = ResourceExhausted, want it to reach validation")
	}
}

// TestComposeUpDownCommands proves the exact compose verbs: up recreates with
// orphan cleanup, restart restarts, and down never removes volumes.
func TestComposeUpDownCommands(t *testing.T) {
	logPath := fakeDockerCLI(t, "exit 0")
	server := newTestComposeServer(t, ComposeServerConfig{})

	if _, err := server.ComposeUp(context.Background(), &agentv1.ComposeUpRequest{
		ProjectName: composeTestProject,
		ComposeYaml: []byte(composeTestDocument),
	}); err != nil {
		t.Fatalf("ComposeUp: %v", err)
	}
	if _, err := server.ComposeUp(context.Background(), &agentv1.ComposeUpRequest{
		ProjectName: composeTestProject,
		ComposeYaml: []byte(composeTestDocument),
		Restart:     true,
	}); err != nil {
		t.Fatalf("ComposeUp(restart): %v", err)
	}
	if _, err := server.ComposeDown(context.Background(), &agentv1.ComposeDownRequest{
		ProjectName: composeTestProject,
		ComposeYaml: []byte(composeTestDocument),
	}); err != nil {
		t.Fatalf("ComposeDown: %v", err)
	}

	calls := composeCalls(t, logPath)
	joined := strings.Join(calls, "\n")
	if !strings.Contains(joined, "up -d --remove-orphans") {
		t.Errorf("no up -d --remove-orphans call:\n%s", joined)
	}
	if !strings.Contains(joined, "restart -d") && !strings.Contains(joined, "restart") {
		t.Errorf("no restart call:\n%s", joined)
	}
	if !strings.Contains(joined, "down --remove-orphans") {
		t.Errorf("no down --remove-orphans call:\n%s", joined)
	}
	for _, call := range calls {
		if strings.Contains(call, "--volumes") || strings.Contains(call, " -v") {
			t.Errorf("down must never remove volumes: %q", call)
		}
	}
	// Every call must reference the project's compose file and name.
	for _, call := range calls {
		if !strings.Contains(call, filepath.Join(server.root, composeTestProject, "compose.yaml")) &&
			!strings.Contains(call, "-f ") {
			t.Errorf("call missing -f: %q", call)
		}
	}
}

// TestComposeDockerHost proves the agent's Docker endpoint reaches the CLI as
// a normalized DOCKER_HOST.
func TestComposeDockerHost(t *testing.T) {
	logPath := fakeDockerCLI(t, "exit 0")
	server := newTestComposeServer(t, ComposeServerConfig{DockerHost: "/var/run/docker.sock"})
	if _, err := server.ComposeUp(context.Background(), &agentv1.ComposeUpRequest{
		ProjectName: composeTestProject,
		ComposeYaml: []byte(composeTestDocument),
	}); err != nil {
		t.Fatalf("ComposeUp: %v", err)
	}
	content, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read calls: %v", err)
	}
	if !strings.Contains(string(content), "docker_host=unix:///var/run/docker.sock") {
		t.Errorf("DOCKER_HOST not forwarded as a unix URL:\n%s", content)
	}

	// A TCP endpoint passes through unchanged.
	logPath = fakeDockerCLI(t, "exit 0")
	server = newTestComposeServer(t, ComposeServerConfig{DockerHost: "tcp://127.0.0.1:2375"})
	if _, err := server.ComposeUp(context.Background(), &agentv1.ComposeUpRequest{
		ProjectName: composeTestProject,
		ComposeYaml: []byte(composeTestDocument),
	}); err != nil {
		t.Fatalf("ComposeUp(tcp): %v", err)
	}
	content, err = os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read calls: %v", err)
	}
	if !strings.Contains(string(content), "docker_host=tcp://127.0.0.1:2375") {
		t.Errorf("DOCKER_HOST not forwarded:\n%s", content)
	}
}

// TestComposePsParsesJSONLines proves the ps output parser handles the CLI's
// JSON-lines shape, both name fields and comma-separated ports.
func TestComposePsParsesJSONLines(t *testing.T) {
	fakeDockerCLI(t, `
printf '%s\n' '{"ID":"abc123","Name":"proj-web-1","Service":"web","Image":"nginx:1.23","State":"running","Status":"Up 2 minutes","Health":"healthy","Ports":"0.0.0.0:8080->80/tcp, 443/tcp"}'
printf '%s\n' '{"ID":"def456","Names":"proj-worker-1","Service":"worker","Image":"busybox:1.36","State":"exited","Status":"Exited (0) 1 second ago","Ports":""}'
exit 0
`)
	file := filepath.Join(t.TempDir(), "compose.yaml")
	if err := os.WriteFile(file, []byte(composeTestDocument), 0o600); err != nil {
		t.Fatalf("write compose file: %v", err)
	}
	server := newTestComposeServer(t, ComposeServerConfig{})
	if _, err := server.ComposeUp(context.Background(), &agentv1.ComposeUpRequest{
		ProjectName: composeTestProject,
		ComposeYaml: []byte(composeTestDocument),
	}); err != nil {
		t.Fatalf("seed project: %v", err)
	}
	response, err := server.ComposePs(context.Background(), &agentv1.ComposePsRequest{ProjectName: composeTestProject})
	if err != nil {
		t.Fatalf("ComposePs: %v", err)
	}
	containers := response.GetContainers()
	if len(containers) != 2 {
		t.Fatalf("containers = %+v", containers)
	}
	web := containers[0]
	if web.GetService() != "web" || web.GetName() != "proj-web-1" || web.GetState() != "running" ||
		web.GetHealth() != "healthy" || len(web.GetPorts()) != 2 {
		t.Errorf("web = %+v", web)
	}
	worker := containers[1]
	if worker.GetName() != "proj-worker-1" || worker.GetState() != "exited" || len(worker.GetPorts()) != 0 {
		t.Errorf("worker = %+v", worker)
	}

	// ps before any deploy reports a clear precondition error.
	if _, err := server.ComposePs(context.Background(), &agentv1.ComposePsRequest{
		ProjectName: "gotham-00000000-0000-4000-8000-000000000000",
	}); status.Code(err) != codes.FailedPrecondition {
		t.Errorf("ps for an unwritten project = %v, want FailedPrecondition", status.Code(err))
	}
}

// TestComposePsIgnoresStderrWarnings proves a CLI warning on stderr cannot
// corrupt a parsed output (compose prints warnings there).
func TestComposePsIgnoresStderrWarnings(t *testing.T) {
	fakeDockerCLI(t, `
printf 'WARN[0000] a warning that must not corrupt the output\n' >&2
printf '%s\n' '{"ID":"a","Service":"web","State":"running"}'
exit 0
`)
	server := newTestComposeServer(t, ComposeServerConfig{})
	if _, err := server.ComposeUp(context.Background(), &agentv1.ComposeUpRequest{
		ProjectName: composeTestProject,
		ComposeYaml: []byte(composeTestDocument),
	}); err != nil {
		t.Fatalf("seed project: %v", err)
	}
	response, err := server.ComposePs(context.Background(), &agentv1.ComposePsRequest{ProjectName: composeTestProject})
	if err != nil {
		t.Fatalf("ComposePs: %v", err)
	}
	if len(response.GetContainers()) != 1 || response.GetContainers()[0].GetService() != "web" {
		t.Fatalf("containers = %+v", response.GetContainers())
	}
}

// TestComposePsAcceptsJSONArray proves the newer array shape decodes too.
func TestComposePsAcceptsJSONArray(t *testing.T) {
	containers, err := parseComposePs([]byte(`[{"ID":"a","Service":"web","State":"running","Ports":"80/tcp"}]`))
	if err != nil {
		t.Fatalf("parseComposePs: %v", err)
	}
	if len(containers) != 1 || containers[0].GetService() != "web" {
		t.Fatalf("containers = %+v", containers)
	}
	if empty, err := parseComposePs(nil); err != nil || len(empty) != 0 {
		t.Fatalf("parseComposePs(nil) = %v, %v", empty, err)
	}
}

// fakeComposeStream is the send side of ComposeLogs for tests; the unused
// grpc.ServerStream methods come from the embedded interface.
type fakeComposeStream struct {
	grpc.ServerStream
	ctx     context.Context
	chunks  [][]byte
	frames  []*agentv1.ComposeLogChunk
	sendErr error
}

func (s *fakeComposeStream) Context() context.Context { return s.ctx }

func (s *fakeComposeStream) Send(chunk *agentv1.ComposeLogChunk) error {
	if s.sendErr != nil {
		return s.sendErr
	}
	s.frames = append(s.frames, chunk)
	s.chunks = append(s.chunks, chunk.GetData())
	return nil
}

// TestComposeLogsRefusesUnknownServiceBeforeCLI proves a syntactically valid
// but unknown selector is rejected before the logs command runs: otherwise
// compose's stderr refusal would be forwarded as the first output chunk and the
// control plane would commit a successful stream for a failed command.
func TestComposeLogsRefusesUnknownServiceBeforeCLI(t *testing.T) {
	logPath := fakeDockerCLI(t, `
case "$6" in
  config) printf 'web\n' ;;
  logs) printf 'no such service: missing\n' 1>&2; exit 1 ;;
esac
exit 0
`)
	server := newTestComposeServer(t, ComposeServerConfig{})
	if _, err := server.ComposeUp(context.Background(), &agentv1.ComposeUpRequest{
		ProjectName: composeTestProject,
		ComposeYaml: []byte(composeTestDocument),
	}); err != nil {
		t.Fatalf("seed project: %v", err)
	}
	err := server.ComposeLogs(&agentv1.ComposeLogsRequest{
		ProjectName: composeTestProject,
		Service:     "missing",
	}, &fakeComposeStream{ctx: context.Background()})
	if status.Code(err) != codes.InvalidArgument || !strings.Contains(status.Convert(err).Message(), "no such compose service") {
		t.Fatalf("ComposeLogs(unknown) = %v, want InvalidArgument", err)
	}
	for _, call := range composeCalls(t, logPath) {
		if strings.Contains(call, "logs") {
			t.Fatalf("the logs command ran for an unknown selector: %q", call)
		}
	}
}

// TestComposeLogsQuietDescendantCancellation proves context cancellation ends
// a stream even when a quiet descendant of the CLI keeps the inherited pipes
// open: the drain must not wait for it before reaping the command.
func TestComposeLogsQuietDescendantCancellation(t *testing.T) {
	fakeDockerCLI(t, `
case "$6" in
  config) printf 'web\n' ;;
  logs)
    printf 'first line\n'
    sleep 8 &
    wait
    ;;
esac
exit 0
`)
	server := newTestComposeServer(t, ComposeServerConfig{})
	if _, err := server.ComposeUp(context.Background(), &agentv1.ComposeUpRequest{
		ProjectName: composeTestProject,
		ComposeYaml: []byte(composeTestDocument),
	}); err != nil {
		t.Fatalf("seed project: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stream := &slowComposeStream{ctx: ctx}
	done := make(chan error, 1)
	go func() {
		done <- server.ComposeLogs(&agentv1.ComposeLogsRequest{ProjectName: composeTestProject, Follow: true}, stream)
	}()
	waitForBytes(t, stream, 1)
	start := time.Now()
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("cancelled stream = %v, want nil", err)
		}
		if elapsed := time.Since(start); elapsed > 5*time.Second {
			t.Fatalf("cancellation took %s; a quiet descendant kept the pipes open", elapsed)
		}
	case <-time.After(6 * time.Second):
		t.Fatal("a cancelled stream with a quiet descendant did not return")
	}
}

// TestComposeFailedVerbOutputIsDiagnosticBounded proves a verb whose stdout is
// only ever quoted into a diagnostic (up/down) does not retain the parsed
// output budget: a failing up writing megabytes stays bounded.
func TestComposeFailedVerbOutputIsDiagnosticBounded(t *testing.T) {
	fakeDockerCLI(t, `
case "$6" in
  config) printf 'web\n' ;;
  up)
    head -c 4194304 /dev/zero | tr '\0' 'u'
    printf 'up failed\n' 1>&2
    exit 1
    ;;
esac
exit 0
`)
	server := newTestComposeServer(t, ComposeServerConfig{})

	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	_, err := server.ComposeUp(context.Background(), &agentv1.ComposeUpRequest{
		ProjectName: composeTestProject,
		ComposeYaml: []byte(composeTestDocument),
	})
	runtime.ReadMemStats(&after)
	if status.Code(err) != codes.Internal {
		t.Fatalf("code = %v, want Internal", status.Code(err))
	}
	if grown := after.TotalAlloc - before.TotalAlloc; grown > 1<<20 {
		t.Fatalf("a failed up retained %d bytes of stdout", grown)
	}
	if message := status.Convert(err).Message(); len(message) > maxComposeError+200 {
		t.Fatalf("diagnostic is unbounded: %d bytes", len(message))
	}
}

// acceptanceFailStream fails the acceptance Send once the CLI has written its
// pid file, so a test can prove the started command was reaped.
type acceptanceFailStream struct {
	grpc.ServerStream
	ctx     context.Context
	pidFile string
	sendErr error
}

func (s *acceptanceFailStream) Context() context.Context { return s.ctx }

func (s *acceptanceFailStream) Send(chunk *agentv1.ComposeLogChunk) error {
	if chunk.GetReady() {
		deadline := time.Now().Add(10 * time.Second)
		for {
			if _, err := os.Stat(s.pidFile); err == nil {
				break
			}
			if time.Now().After(deadline) {
				return errors.New("the CLI never wrote its pid")
			}
			time.Sleep(5 * time.Millisecond)
		}
	}
	return s.sendErr
}

// TestComposeLogsAcceptanceFailureReapsCLI proves a client that disappears
// during acceptance does not leave an unreaped child in the long-lived agent:
// the started command is cancelled and waited before the RPC returns.
func TestComposeLogsAcceptanceFailureReapsCLI(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "cli.pid")
	t.Setenv("FAKE_PID_FILE", pidFile)
	fakeDockerCLI(t, `
case "$6" in
  logs)
    echo $$ > "$FAKE_PID_FILE"
    sleep 5 &
    wait
    ;;
esac
exit 0
`)
	server := newTestComposeServer(t, ComposeServerConfig{})
	if _, err := server.ComposeUp(context.Background(), &agentv1.ComposeUpRequest{
		ProjectName: composeTestProject,
		ComposeYaml: []byte(composeTestDocument),
	}); err != nil {
		t.Fatalf("seed project: %v", err)
	}

	stream := &acceptanceFailStream{ctx: context.Background(), pidFile: pidFile, sendErr: errors.New("transport gone")}
	if err := server.ComposeLogs(&agentv1.ComposeLogsRequest{
		ProjectName: composeTestProject,
		Follow:      true,
	}, stream); err != nil {
		t.Fatalf("ComposeLogs = %v, want nil (client gone)", err)
	}

	content, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatalf("read the CLI pid: %v", err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(content)))
	if err != nil {
		t.Fatalf("parse the CLI pid %q: %v", content, err)
	}
	var status syscall.WaitStatus
	waited, waitErr := syscall.Wait4(pid, &status, syscall.WNOHANG, nil)
	if waitErr == nil && waited == pid {
		t.Fatalf("the CLI process %d was left unreaped after the RPC returned", pid)
	}
	if !errors.Is(waitErr, syscall.ECHILD) {
		t.Fatalf("Wait4(%d) = %d, %v; want ECHILD (already reaped)", pid, waited, waitErr)
	}
}

// TestComposeLogsStreams proves logs stream chunk by chunk, that a service
// selector is passed through and that the first frame is the acceptance frame.
func TestComposeLogsStreams(t *testing.T) {
	logPath := fakeDockerCLI(t, `
case "$6" in
  config) printf 'web\nworker\n' ;;
  logs)
    printf 'worker-1  | alive\n'
    printf 'worker-1  | alive again\n'
    ;;
esac
exit 0
`)
	server := newTestComposeServer(t, ComposeServerConfig{})
	if _, err := server.ComposeUp(context.Background(), &agentv1.ComposeUpRequest{
		ProjectName: composeTestProject,
		ComposeYaml: []byte(composeTestDocument),
	}); err != nil {
		t.Fatalf("seed project: %v", err)
	}
	stream := &fakeComposeStream{ctx: context.Background()}
	if err := server.ComposeLogs(&agentv1.ComposeLogsRequest{
		ProjectName: composeTestProject,
		Service:     "worker",
		Tail:        50,
	}, stream); err != nil {
		t.Fatalf("ComposeLogs: %v", err)
	}
	joined := string(joinChunks(stream.chunks))
	if !strings.Contains(joined, "alive again") {
		t.Errorf("logs = %q", joined)
	}
	if len(stream.frames) == 0 || !stream.frames[0].GetReady() {
		t.Fatalf("the first frame must be the acceptance frame: %+v", stream.frames)
	}
	for _, frame := range stream.frames {
		if frame.GetReady() && len(frame.GetData()) > 0 {
			t.Errorf("an acceptance frame must carry no data: %+v", frame)
		}
	}
	calls := composeCalls(t, logPath)
	if !strings.Contains(strings.Join(calls, "\n"), "--tail 50") {
		t.Errorf("tail not forwarded:\n%v", calls)
	}
	if !strings.Contains(strings.Join(calls, "\n"), "logs --no-color") {
		t.Errorf("logs call missing:\n%v", calls)
	}
	if !strings.Contains(strings.Join(calls, "\n"), " worker") {
		t.Errorf("service selector not forwarded:\n%v", calls)
	}

	// An invalid service name is rejected before the CLI runs.
	if err := server.ComposeLogs(&agentv1.ComposeLogsRequest{
		ProjectName: composeTestProject,
		Service:     "bad service",
	}, &fakeComposeStream{ctx: context.Background()}); status.Code(err) != codes.InvalidArgument {
		t.Errorf("invalid service code = %v, want InvalidArgument", status.Code(err))
	}

	// A follow request forwards --follow (the fake CLI exits immediately, so
	// the stream closes without needing a cancel).
	if err := server.ComposeLogs(&agentv1.ComposeLogsRequest{
		ProjectName: composeTestProject,
		Follow:      true,
	}, &fakeComposeStream{ctx: context.Background()}); err != nil {
		t.Fatalf("ComposeLogs(follow): %v", err)
	}
	if !strings.Contains(strings.Join(composeCalls(t, logPath), "\n"), "--follow") {
		t.Errorf("--follow not forwarded")
	}
}

// TestComposeCLIFailureIsBounded proves a failing CLI invocation becomes an
// Internal status whose message is bounded and never carries the command line.
func TestComposeCLIFailureIsBounded(t *testing.T) {
	fakeDockerCLI(t, `
printf 'error: service "web" refers to undefined volume\n' >&2
i=0
while [ $i -lt 400 ]; do
  printf 'padding line %s\n' "$i" >&2
  i=$((i+1))
done
exit 1
`)
	server := newTestComposeServer(t, ComposeServerConfig{})
	_, err := server.ComposeValidate(context.Background(), &agentv1.ComposeValidateRequest{
		ProjectName: composeTestProject,
		ComposeYaml: []byte(composeTestDocument),
	})
	if status.Code(err) != codes.Internal {
		t.Fatalf("code = %v, want Internal", status.Code(err))
	}
	message := status.Convert(err).Message()
	if len(message) > maxComposeError+200 {
		t.Errorf("error message is unbounded: %d bytes", len(message))
	}
	if !strings.Contains(message, "undefined volume") {
		t.Errorf("error lost the CLI diagnosis: %q", message)
	}
}

// TestComposeOperationsAreSerializedPerProject proves a concurrent request
// cannot replace the document between another request's write and its CLI run:
// the agent serializes complete document-dependent operations per project.
func TestComposeOperationsAreSerializedPerProject(t *testing.T) {
	dir := t.TempDir()
	started := filepath.Join(dir, "started")
	resume := filepath.Join(dir, "resume")
	t.Setenv("FAKE_STARTED", started)
	t.Setenv("FAKE_RESUME", resume)

	// The script re-reads the compose file after the gate, so it reports the
	// document that is on disk when its command actually runs.
	fakeDockerCLI(t, `
doc=$(cat "$3")
case "$doc" in
  *first*)
    touch "$FAKE_STARTED"
    i=0
    while [ ! -f "$FAKE_RESUME" ] && [ $i -lt 400 ]; do sleep 0.05; i=$((i+1)); done
    ;;
esac
case "$doc" in
  *first*) printf 'first\n' ;;
  *second*) printf 'second\n' ;;
esac
exit 0
`)
	server := newTestComposeServer(t, ComposeServerConfig{})

	firstDone := make(chan []string, 1)
	firstErr := make(chan error, 1)
	go func() {
		response, err := server.ComposeValidate(context.Background(), &agentv1.ComposeValidateRequest{
			ProjectName: composeTestProject,
			ComposeYaml: []byte("services:\n  first:\n    image: nginx:1.23\n"),
		})
		if err != nil {
			firstErr <- err
			return
		}
		firstDone <- response.GetServices()
	}()

	// Wait until request A's CLI is running against its own document.
	waitForFile(t, started)

	secondDone := make(chan []string, 1)
	secondErr := make(chan error, 1)
	go func() {
		response, err := server.ComposeValidate(context.Background(), &agentv1.ComposeValidateRequest{
			ProjectName: composeTestProject,
			ComposeYaml: []byte("services:\n  second:\n    image: nginx:1.23\n"),
		})
		if err != nil {
			secondErr <- err
			return
		}
		secondDone <- response.GetServices()
	}()

	// Request B must not have replaced the document while A holds the
	// project lock.
	time.Sleep(300 * time.Millisecond)
	select {
	case services := <-secondDone:
		t.Fatalf("a concurrent request completed while another operation was in flight: %v", services)
	default:
	}
	if err := os.WriteFile(resume, []byte("go"), 0o600); err != nil {
		t.Fatalf("resume: %v", err)
	}

	select {
	case err := <-firstErr:
		t.Fatalf("first validate: %v", err)
	case services := <-firstDone:
		if strings.Join(services, ",") != "first" {
			t.Fatalf("the first operation executed another request's document: %v", services)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("first validate did not finish")
	}
	select {
	case err := <-secondErr:
		t.Fatalf("second validate: %v", err)
	case services := <-secondDone:
		if strings.Join(services, ",") != "second" {
			t.Fatalf("the second operation executed another request's document: %v", services)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("second validate did not finish")
	}
}

// TestComposeValidateAndUpAreSerialized proves a validate overlapping an up
// cannot have its document replaced between the write and the CLI run either.
func TestComposeValidateAndUpAreSerialized(t *testing.T) {
	dir := t.TempDir()
	started := filepath.Join(dir, "started")
	resume := filepath.Join(dir, "resume")
	t.Setenv("FAKE_STARTED", started)
	t.Setenv("FAKE_RESUME", resume)

	fakeDockerCLI(t, `
doc=$(cat "$3")
case "$doc" in
  *first*)
    touch "$FAKE_STARTED"
    i=0
    while [ ! -f "$FAKE_RESUME" ] && [ $i -lt 400 ]; do sleep 0.05; i=$((i+1)); done
    ;;
esac
case "$doc" in
  *first*) printf 'first\n' ;;
  *second*) printf 'second\n' ;;
esac
exit 0
`)
	server := newTestComposeServer(t, ComposeServerConfig{})

	firstDone := make(chan []string, 1)
	go func() {
		response, err := server.ComposeValidate(context.Background(), &agentv1.ComposeValidateRequest{
			ProjectName: composeTestProject,
			ComposeYaml: []byte("services:\n  first:\n    image: nginx:1.23\n"),
		})
		if err != nil {
			firstDone <- []string{"error: " + err.Error()}
			return
		}
		firstDone <- response.GetServices()
	}()
	waitForFile(t, started)

	upDone := make(chan error, 1)
	go func() {
		_, err := server.ComposeUp(context.Background(), &agentv1.ComposeUpRequest{
			ProjectName: composeTestProject,
			ComposeYaml: []byte("services:\n  second:\n    image: nginx:1.23\n"),
		})
		upDone <- err
	}()

	// The up must not replace the document while the validate holds the
	// project lock.
	time.Sleep(300 * time.Millisecond)
	select {
	case err := <-upDone:
		t.Fatalf("a concurrent up completed while a validate was in flight: %v", err)
	default:
	}
	if err := os.WriteFile(resume, []byte("go"), 0o600); err != nil {
		t.Fatalf("resume: %v", err)
	}
	select {
	case services := <-firstDone:
		if strings.Join(services, ",") != "first" {
			t.Fatalf("the validate executed another request's document: %v", services)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("the validate did not finish")
	}
	select {
	case err := <-upDone:
		if err != nil {
			t.Fatalf("up: %v", err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("the up did not finish")
	}
}

// waitForFile polls until path exists.
func waitForFile(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("file %s never appeared", path)
}

// TestComposeLogsDrainsSlowConsumer proves the full CLI output is delivered
// even when the consumer is slow: the pipes are drained to EOF before Wait,
// and a send failure cancels the CLI promptly.
func TestComposeLogsDrainsSlowConsumer(t *testing.T) {
	// ~2 MiB of output, far past the pipe buffer, so a Wait-first order would
	// discard the tail. Only the logs subcommand produces the payload.
	const totalBytes = 2 << 20
	fakeDockerCLI(t, `
case "$6" in
  config) printf 'web\n' ;;
  logs) head -c `+fmt.Sprint(totalBytes)+` /dev/zero | tr '\0' 'x' ;;
esac
exit 0
`)
	server := newTestComposeServer(t, ComposeServerConfig{})
	if _, err := server.ComposeUp(context.Background(), &agentv1.ComposeUpRequest{
		ProjectName: composeTestProject,
		ComposeYaml: []byte(composeTestDocument),
	}); err != nil {
		t.Fatalf("seed project: %v", err)
	}
	stream := &slowComposeStream{ctx: context.Background(), delay: 2 * time.Millisecond}
	if err := server.ComposeLogs(&agentv1.ComposeLogsRequest{ProjectName: composeTestProject}, stream); err != nil {
		t.Fatalf("ComposeLogs: %v", err)
	}
	if received := stream.receivedBytes(); received != totalBytes {
		t.Fatalf("delivered %d bytes of %d: the tail was truncated", received, totalBytes)
	}

	// A consumer that fails cancels the CLI and the drain returns instead of
	// hanging.
	stream = &slowComposeStream{ctx: context.Background(), failAfter: 1, sendErr: errors.New("client gone")}
	done := make(chan error, 1)
	go func() {
		done <- server.ComposeLogs(&agentv1.ComposeLogsRequest{ProjectName: composeTestProject}, stream)
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("ComposeLogs after a send failure = %v, want nil (client gone)", err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("a failing consumer did not stop the log stream")
	}
}

// TestComposeLogsFollowCancellation proves a follow stream ends cleanly when
// the client cancels and does not leave the CLI running.
func TestComposeLogsFollowCancellation(t *testing.T) {
	fakeDockerCLI(t, `
case "$6" in
  config) printf 'web\n' ;;
  logs)
    i=0
    while [ $i -lt 4000 ]; do
      printf 'tick\n'
      sleep 0.05
      i=$((i+1))
    done
    ;;
esac
exit 0
`)
	server := newTestComposeServer(t, ComposeServerConfig{})
	if _, err := server.ComposeUp(context.Background(), &agentv1.ComposeUpRequest{
		ProjectName: composeTestProject,
		ComposeYaml: []byte(composeTestDocument),
	}); err != nil {
		t.Fatalf("seed project: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	stream := &slowComposeStream{ctx: ctx}
	done := make(chan error, 1)
	go func() {
		done <- server.ComposeLogs(&agentv1.ComposeLogsRequest{ProjectName: composeTestProject, Follow: true}, stream)
	}()
	waitForBytes(t, stream, 1)
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("cancelled follow stream = %v, want nil", err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("a cancelled follow stream did not return")
	}
}

// TestComposeOutputCaptureIsBounded proves the CLI diagnostic capture is
// capped while it is written: a failing CLI writing megabytes retains only the
// bound, and a parsed output that exceeds its cap fails instead of being
// truncated.
func TestComposeOutputCaptureIsBounded(t *testing.T) {
	fakeDockerCLI(t, `
head -c 4194304 /dev/zero | tr '\0' 'x' 1>&2
printf 'undefined volume\n' 1>&2
exit 1
`)
	server := newTestComposeServer(t, ComposeServerConfig{})

	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	_, err := server.ComposeValidate(context.Background(), &agentv1.ComposeValidateRequest{
		ProjectName: composeTestProject,
		ComposeYaml: []byte(composeTestDocument),
	})
	runtime.ReadMemStats(&after)
	if status.Code(err) != codes.Internal {
		t.Fatalf("code = %v, want Internal", status.Code(err))
	}
	if grown := after.TotalAlloc - before.TotalAlloc; grown > 1<<20 {
		t.Fatalf("the CLI diagnostic capture allocated %d bytes for a 4 MiB stream", grown)
	}

	// A parsed output beyond the cap fails instead of producing a wrong list.
	fakeDockerCLI(t, `
head -c 2097152 /dev/zero | tr '\0' 'y'
exit 0
`)
	server = newTestComposeServer(t, ComposeServerConfig{})
	_, err = server.ComposeValidate(context.Background(), &agentv1.ComposeValidateRequest{
		ProjectName: composeTestProject,
		ComposeYaml: []byte(composeTestDocument),
	})
	if status.Code(err) != codes.Internal || !strings.Contains(status.Convert(err).Message(), "exceeds") {
		t.Fatalf("oversized parsed output = %v, want an Internal exceeds error", err)
	}
}

// slowComposeStream is a fake ComposeLogs stream with a controllable consumer:
// an optional per-chunk delay, a byte counter and a scripted send failure.
type slowComposeStream struct {
	grpc.ServerStream
	ctx       context.Context
	delay     time.Duration
	failAfter int
	sendErr   error

	// mu guards the counters: Send runs on the agent's copy goroutines while
	// the test polls from its own goroutine (go test -race).
	mu       sync.Mutex
	sends    int
	received int
}

func (s *slowComposeStream) Context() context.Context { return s.ctx }

func (s *slowComposeStream) Send(chunk *agentv1.ComposeLogChunk) error {
	if s.delay > 0 {
		time.Sleep(s.delay)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sends++
	if s.failAfter > 0 && s.sends > s.failAfter {
		return s.sendErr
	}
	s.received += len(chunk.GetData())
	return nil
}

// receivedBytes returns the number of data bytes sent so far.
func (s *slowComposeStream) receivedBytes() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.received
}

// waitForBytes polls until the stream received at least want bytes.
func waitForBytes(t *testing.T, stream *slowComposeStream, want int) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if stream.receivedBytes() >= want {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("stream received %d bytes, want at least %d", stream.receivedBytes(), want)
}

// joinChunks flattens streamed chunks.
func joinChunks(chunks [][]byte) []byte {
	var out []byte
	for _, chunk := range chunks {
		out = append(out, chunk...)
	}
	return out
}
