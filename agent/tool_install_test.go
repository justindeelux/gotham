package agent

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
	"github.com/justindeelux/gotham/toolcatalog"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// fakeToolStream is the send side of InstallTool/UninstallTool for tests; the
// unused grpc.ServerStream methods come from the embedded interface.
type fakeToolStream struct {
	grpc.ServerStream
	ctx     context.Context
	events  []*agentv1.ToolEvent
	sendErr error
}

func newFakeToolStream(ctx context.Context) *fakeToolStream {
	return &fakeToolStream{ctx: ctx}
}

func (s *fakeToolStream) Context() context.Context { return s.ctx }

func (s *fakeToolStream) Send(event *agentv1.ToolEvent) error {
	if s.sendErr != nil {
		return s.sendErr
	}
	s.events = append(s.events, event)
	return nil
}

// lines returns every output line, tagged by source, in order.
func (s *fakeToolStream) lines() []string {
	var lines []string
	for _, event := range s.events {
		if output := event.GetOutput(); output != nil {
			lines = append(lines, output.GetStream()+":"+output.GetLine())
		}
	}
	return lines
}

// result returns the final ToolResult, or nil when the stream carried none.
func (s *fakeToolStream) result() *agentv1.ToolResult {
	if len(s.events) == 0 {
		return nil
	}
	return s.events[len(s.events)-1].GetResult()
}

// recordedCommand is one command the executor ran.
type recordedCommand struct {
	argv []string
	root bool
}

// fakeToolRunner is a commandRunner for tests: it records every argv and, when
// step is set, delegates to it instead of the default success.
type fakeToolRunner struct {
	mu    sync.Mutex
	calls []recordedCommand
	step  func(ctx context.Context, argv []string, root bool, emit func(stream, line string) error) (int, error)
}

func (r *fakeToolRunner) Run(ctx context.Context, argv []string, root bool, emit func(string, string) error) (int, error) {
	r.mu.Lock()
	r.calls = append(r.calls, recordedCommand{argv: append([]string(nil), argv...), root: root})
	override := r.step
	r.mu.Unlock()
	if override != nil {
		return override(ctx, argv, root, emit)
	}
	if err := emit(streamStdout, "ok: "+strings.Join(argv, " ")); err != nil {
		return -1, err
	}
	return 0, nil
}

// commands returns the argv of every command the executor ran, in order.
func (r *fakeToolRunner) commands() [][]string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([][]string, 0, len(r.calls))
	for _, call := range r.calls {
		out = append(out, call.argv)
	}
	return out
}

// allRoot reports whether every recorded command was marked root.
func (r *fakeToolRunner) allRoot() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, call := range r.calls {
		if !call.root {
			return false
		}
	}
	return len(r.calls) > 0
}

// invoked returns the tool ids the executor asked the helper to act on, with
// the verb stripped, in order.
func (r *fakeToolRunner) invoked(verb string) []string {
	var ids []string
	for _, argv := range r.commands() {
		if len(argv) == 3 && argv[1] == verb {
			ids = append(ids, argv[2])
		}
	}
	return ids
}

// installedNone reports every tool absent; installedOnly reports exactly the
// named tools present.
func installedNone(string) bool { return false }

func TestToolServerRefusesPlatformToolUninstall(t *testing.T) {
	for _, id := range []string{toolcatalog.IDGit, toolcatalog.IDDocker} {
		for _, present := range []bool{false, true} {
			runner := &fakeToolRunner{}
			server := NewToolServer(withToolRunner(runner), withToolInstalled(func(string) bool { return present }))
			err := server.UninstallTool(&agentv1.InstallToolRequest{Id: id}, newFakeToolStream(context.Background()))
			if status.Code(err) != codes.FailedPrecondition || !strings.Contains(err.Error(), "required by the platform") {
				t.Fatalf("UninstallTool(%s, installed=%v) = %v; want platform refusal", id, present, err)
			}
			if len(runner.commands()) != 0 {
				t.Fatal("platform tool uninstall invoked the helper")
			}
		}
	}
}

func TestToolServerInstallUpdatesOutdatedGit(t *testing.T) {
	for _, version := range []string{"2.34.1", "2.37.0", "2.43.0"} {
		t.Run(version, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, "git"), []byte("#!/bin/sh\necho 'git version "+version+"'\n"), 0o755); err != nil {
				t.Fatal(err)
			}
			t.Setenv("PATH", dir)
			runner := &fakeToolRunner{}
			server := NewToolServer(withToolRunner(runner))
			stream := newFakeToolStream(context.Background())
			if err := server.InstallTool(&agentv1.InstallToolRequest{Id: toolcatalog.IDGit}, stream); err != nil {
				t.Fatal(err)
			}
			want := 0
			if version == "2.34.1" {
				want = 1
			}
			if got := len(runner.invoked(helperInstallVerb)); got != want {
				t.Fatalf("git %s ran %d install commands; want %d", version, got, want)
			}
			if result := stream.result(); result == nil || result.GetStatus() != toolStatusSucceeded {
				t.Fatalf("result = %v; want succeeded", result)
			}
		})
	}
}

func installedOnly(ids ...string) func(string) bool {
	present := make(map[string]bool, len(ids))
	for _, id := range ids {
		present[id] = true
	}
	return func(id string) bool { return present[id] }
}

// TestToolServerInstallRunsDependenciesFirst proves the dependency chain
// railpack => buildkit => docker is installed in order, each as the fixed
// helper verb with a catalog id and elevated to root, and the stream closes
// with a success result.
func TestToolServerInstallRunsDependenciesFirst(t *testing.T) {
	runner := &fakeToolRunner{}
	server := NewToolServer(
		withToolRunner(runner),
		withToolHelper("/opt/gotham/gotham-toolctl"),
		withToolInstalled(installedNone),
	)

	stream := newFakeToolStream(context.Background())
	if err := server.InstallTool(&agentv1.InstallToolRequest{Id: toolcatalog.IDRailpack}, stream); err != nil {
		t.Fatalf("InstallTool(railpack): %v", err)
	}

	if got, want := runner.invoked(helperInstallVerb), []string{toolcatalog.IDDocker, toolcatalog.IDBuildKit, toolcatalog.IDRailpack}; !equalStrings(got, want) {
		t.Fatalf("install order = %v; want %v", got, want)
	}
	for _, argv := range runner.commands() {
		if len(argv) != 3 || argv[0] != "/opt/gotham/gotham-toolctl" {
			t.Fatalf("helper argv = %v; want [/opt/gotham/gotham-toolctl install-tool <id>]", argv)
		}
	}
	if !runner.allRoot() {
		t.Fatal("a helper step ran without root; the helper is the privileged path")
	}

	if result := stream.result(); result == nil || result.GetStatus() != toolStatusSucceeded || result.GetExitCode() != 0 {
		t.Fatalf("final result = %v; want succeeded/0", result)
	}
}

// TestToolServerInstallIdempotent proves a tool that is already installed is
// skipped: no command runs and the stream still succeeds.
func TestToolServerInstallIdempotent(t *testing.T) {
	runner := &fakeToolRunner{}
	server := NewToolServer(
		withToolRunner(runner),
		withToolInstalled(installedOnly(toolcatalog.IDRailpack, toolcatalog.IDBuildKit, toolcatalog.IDDocker)),
	)

	stream := newFakeToolStream(context.Background())
	if err := server.InstallTool(&agentv1.InstallToolRequest{Id: toolcatalog.IDRailpack}, stream); err != nil {
		t.Fatalf("InstallTool(railpack): %v", err)
	}
	if commands := runner.commands(); len(commands) != 0 {
		t.Fatalf("installed tool re-ran %d commands: %v", len(commands), commands)
	}
	if result := stream.result(); result == nil || result.GetStatus() != toolStatusSucceeded {
		t.Fatalf("final result = %v; want succeeded", result)
	}
}

// TestToolServerInstallFailureReportsExitCode proves a failed command is
// reported in-band with its own exit code and the command's stderr, not as a
// gRPC error, so the control plane can show what went wrong.
func TestToolServerInstallFailureReportsExitCode(t *testing.T) {
	runner := &fakeToolRunner{step: func(_ context.Context, _ []string, _ bool, emit func(string, string) error) (int, error) {
		if err := emit(streamStderr, "E: unable to locate package git"); err != nil {
			return -1, err
		}
		return 100, errors.New("exit status 100")
	}}
	server := NewToolServer(
		withToolRunner(runner),
		withToolInstalled(installedNone),
	)

	stream := newFakeToolStream(context.Background())
	if err := server.InstallTool(&agentv1.InstallToolRequest{Id: toolcatalog.IDGit}, stream); err != nil {
		t.Fatalf("InstallTool(git): %v", err)
	}
	result := stream.result()
	if result == nil || result.GetStatus() != toolStatusFailed || result.GetExitCode() != 100 {
		t.Fatalf("final result = %v; want failed/100", result)
	}
	found := false
	for _, line := range stream.lines() {
		if strings.Contains(line, "unable to locate package git") {
			found = true
		}
	}
	if !found {
		t.Fatalf("stderr line missing from stream: %v", stream.lines())
	}
}

// TestToolServerInstallCanceled proves a cancelled stream is reported as a
// cancelled RPC, not as a failed install, and the command sees the
// cancellation.
func TestToolServerInstallCanceled(t *testing.T) {
	runner := &fakeToolRunner{step: func(ctx context.Context, _ []string, _ bool, _ func(string, string) error) (int, error) {
		<-ctx.Done()
		return -1, ctx.Err()
	}}
	server := NewToolServer(
		withToolRunner(runner),
		withToolInstalled(installedNone),
	)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := server.InstallTool(&agentv1.InstallToolRequest{Id: toolcatalog.IDGit}, newFakeToolStream(ctx))
	if status.Code(err) != codes.Canceled {
		t.Fatalf("InstallTool(canceled) = %v; want Canceled", err)
	}
}

// TestToolServerInstallSecondCallBusy proves only one install runs per node: a
// second call while the first is in flight is refused with ResourceExhausted
// rather than queued or run concurrently.
func TestToolServerInstallSecondCallBusy(t *testing.T) {
	release := make(chan struct{})
	started := make(chan struct{})
	var once sync.Once
	runner := &fakeToolRunner{step: func(ctx context.Context, _ []string, _ bool, _ func(string, string) error) (int, error) {
		once.Do(func() { close(started) })
		select {
		case <-release:
			return 0, nil
		case <-ctx.Done():
			return -1, ctx.Err()
		}
	}}
	server := NewToolServer(
		withToolRunner(runner),
		withToolInstalled(installedNone),
	)

	done := make(chan error, 1)
	go func() {
		done <- server.InstallTool(&agentv1.InstallToolRequest{Id: toolcatalog.IDGit}, newFakeToolStream(context.Background()))
	}()
	<-started

	err := server.InstallTool(&agentv1.InstallToolRequest{Id: toolcatalog.IDGit}, newFakeToolStream(context.Background()))
	if status.Code(err) != codes.ResourceExhausted {
		t.Fatalf("second InstallTool = %v; want ResourceExhausted", err)
	}

	close(release)
	if err := <-done; err != nil {
		t.Fatalf("first InstallTool: %v", err)
	}
}

// TestToolServerInstallTimeout proves a command that outlives the install
// timeout is killed and reported as a failed install, not left running.
func TestToolServerInstallTimeout(t *testing.T) {
	runner := &fakeToolRunner{step: func(ctx context.Context, _ []string, _ bool, _ func(string, string) error) (int, error) {
		<-ctx.Done()
		return -1, ctx.Err()
	}}
	server := NewToolServer(
		withToolRunner(runner),
		withToolInstalled(installedNone),
		withToolTimeout(20*time.Millisecond),
	)

	stream := newFakeToolStream(context.Background())
	if err := server.InstallTool(&agentv1.InstallToolRequest{Id: toolcatalog.IDGit}, stream); err != nil {
		t.Fatalf("InstallTool(timeout): %v", err)
	}
	result := stream.result()
	if result == nil || result.GetStatus() != toolStatusFailed || result.GetExitCode() != -1 {
		t.Fatalf("final result = %v; want failed/-1", result)
	}
}

// TestToolServerUninstallRefusesInUse proves an uninstall is refused while the
// tool is busy, before any command runs.
func TestToolServerUninstallRefusesInUse(t *testing.T) {
	runner := &fakeToolRunner{}
	server := NewToolServer(
		withToolRunner(runner),
		withToolInstalled(installedOnly(toolcatalog.IDBuildKit, toolcatalog.IDDocker)),
		withToolInUse(func(id string) (bool, string) {
			if id == toolcatalog.IDBuildKit {
				return true, "a build is running"
			}
			return false, ""
		}),
	)

	err := server.UninstallTool(&agentv1.InstallToolRequest{Id: toolcatalog.IDBuildKit}, newFakeToolStream(context.Background()))
	if status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("UninstallTool(buildkit in use) = %v; want FailedPrecondition", err)
	}
	if commands := runner.commands(); len(commands) != 0 {
		t.Fatalf("uninstall ran commands for an in-use tool: %v", commands)
	}
}

// TestToolServerUninstallRefusesDependency proves a tool another installed tool
// depends on is refused, while a leaf tool is removable.
func TestToolServerUninstallRefusesDependency(t *testing.T) {
	runner := &fakeToolRunner{}
	server := NewToolServer(
		withToolRunner(runner),
		withToolInstalled(installedOnly(toolcatalog.IDBuildKit, toolcatalog.IDDocker, toolcatalog.IDRailpack)),
	)

	err := server.UninstallTool(&agentv1.InstallToolRequest{Id: toolcatalog.IDBuildKit}, newFakeToolStream(context.Background()))
	if status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("UninstallTool(buildkit with railpack installed) = %v; want FailedPrecondition", err)
	}

	stream := newFakeToolStream(context.Background())
	if err := server.UninstallTool(&agentv1.InstallToolRequest{Id: toolcatalog.IDRailpack}, stream); err != nil {
		t.Fatalf("UninstallTool(railpack): %v", err)
	}
	if got, want := runner.invoked(helperUninstallVerb), []string{toolcatalog.IDRailpack}; !equalStrings(got, want) {
		t.Fatalf("uninstall invoked %v; want %v", got, want)
	}
	if result := stream.result(); result == nil || result.GetStatus() != toolStatusSucceeded {
		t.Fatalf("final result = %v; want succeeded", result)
	}
}

// TestToolServerUninstallNotInstalledIsNoOp proves removing an absent tool runs
// no command and still succeeds, so an uninstall is idempotent.
func TestToolServerUninstallNotInstalledIsNoOp(t *testing.T) {
	runner := &fakeToolRunner{}
	server := NewToolServer(
		withToolRunner(runner),
		withToolInstalled(installedNone),
	)

	stream := newFakeToolStream(context.Background())
	if err := server.UninstallTool(&agentv1.InstallToolRequest{Id: toolcatalog.IDPack}, stream); err != nil {
		t.Fatalf("UninstallTool(pack): %v", err)
	}
	if commands := runner.commands(); len(commands) != 0 {
		t.Fatalf("uninstall of an absent tool ran %v", commands)
	}
	if result := stream.result(); result == nil || result.GetStatus() != toolStatusSucceeded {
		t.Fatalf("final result = %v; want succeeded", result)
	}
}

// TestToolServerUninstallHoldsBuildGuard proves the tracker's uninstall mark is
// held for the whole uninstall, so a build cannot start into the removal window.
func TestToolServerUninstallHoldsBuildGuard(t *testing.T) {
	tracker := NewToolUseTracker()
	runner := &fakeToolRunner{step: func(_ context.Context, _ []string, _ bool, _ func(string, string) error) (int, error) {
		if err := tracker.BeginBuild(); err == nil {
			t.Error("a build started while an uninstall was in flight")
			tracker.EndBuild()
		}
		return 0, nil
	}}
	server := NewToolServer(
		withToolRunner(runner),
		withToolInstalled(installedOnly(toolcatalog.IDPack)),
		WithToolUse(tracker),
	)

	if err := server.UninstallTool(&agentv1.InstallToolRequest{Id: toolcatalog.IDPack}, newFakeToolStream(context.Background())); err != nil {
		t.Fatalf("UninstallTool(pack): %v", err)
	}
}

// TestToolServerInstallUnknownTool proves an unknown id is bad input, not an
// install failure.
func TestToolServerInstallUnknownTool(t *testing.T) {
	server := NewToolServer(withToolRunner(&fakeToolRunner{}))
	err := server.InstallTool(&agentv1.InstallToolRequest{Id: "nope"}, newFakeToolStream(context.Background()))
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("InstallTool(nope) = %v; want InvalidArgument", err)
	}
	if err := server.InstallTool(&agentv1.InstallToolRequest{}, newFakeToolStream(context.Background())); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("InstallTool(empty) = %v; want InvalidArgument", err)
	}
}

// TestExecRunnerDrainsOutputBeforeWait is the regression for the pipe/Wait
// race: reading the pipes concurrently with command.Wait drops trailing lines
// (often the error message). Every emitted line must be captured.
func TestExecRunnerDrainsOutputBeforeWait(t *testing.T) {
	runner := execRunner{}
	var stdout, stderr []string
	// printf is a real argument vector here (no shell), and the format string
	// holds no tool input; 500 lines is enough that a Wait/read race truncates.
	format := strings.Repeat("line\n", 500)
	code, err := runner.Run(context.Background(), []string{"printf", format}, false,
		func(stream, line string) error {
			switch stream {
			case streamStdout:
				stdout = append(stdout, line)
			case streamStderr:
				stderr = append(stderr, line)
			}
			return nil
		})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if code != 0 {
		t.Fatalf("exit code = %d; want 0", code)
	}
	if len(stdout) != 500 {
		t.Fatalf("captured %d stdout lines; want 500 (trailing output was truncated)", len(stdout))
	}
	if len(stderr) != 0 {
		t.Fatalf("unexpected stderr: %v", stderr)
	}
}

// TestExecRunnerReportsExitCode proves a non-zero exit is returned with its
// code so the executor can report it.
func TestExecRunnerReportsExitCode(t *testing.T) {
	runner := execRunner{}
	code, err := runner.Run(context.Background(), []string{"false"}, false, func(string, string) error { return nil })
	if err == nil {
		t.Fatal("Run(false) = nil error; want a non-zero exit error")
	}
	if code != 1 {
		t.Fatalf("exit code = %d; want 1", code)
	}
}

// TestExecRunnerElevatesRootSteps proves a root command is run through the
// fixed `sudo -n` prefix with the same argv, and that a bogus sudo refusal is
// surfaced rather than silently running unelevated.
func TestExecRunnerElevatesRootSteps(t *testing.T) {
	dir := t.TempDir()
	sudo := filepath.Join(dir, "sudo")
	// A stand-in sudo: drop the -n flag and exec the rest.
	script := "#!/bin/sh\n[ \"$1\" = \"-n\" ] || exit 9\nshift\nexec \"$@\"\n"
	if err := os.WriteFile(sudo, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake sudo: %v", err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	runner := execRunner{}
	var stdout []string
	var agentLines []string
	code, err := runner.Run(context.Background(), []string{"printf", "root\n"}, true, func(stream, line string) error {
		switch stream {
		case streamStdout:
			stdout = append(stdout, line)
		case streamAgent:
			agentLines = append(agentLines, line)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("Run(root): %v", err)
	}
	if code != 0 {
		t.Fatalf("exit code = %d; want 0", code)
	}
	if len(stdout) != 1 || stdout[0] != "root" {
		t.Fatalf("stdout = %v; want [root]", stdout)
	}
	if len(agentLines) == 0 || !strings.HasPrefix(agentLines[0], "$ sudo -n ") {
		t.Fatalf("agent command line = %v; want a `$ sudo -n ...` prefix", agentLines)
	}
}

// TestExecRunnerStopsChildGracefullyOnCancel proves a cancelled request sends
// SIGTERM (not SIGKILL), so the helper can stop its transient unit; the child's
// TERM trap must run and the runner must still report the cancellation.
func TestExecRunnerStopsChildGracefullyOnCancel(t *testing.T) {
	dir := t.TempDir()
	ready := filepath.Join(dir, "ready")
	termed := filepath.Join(dir, "termed")
	script := filepath.Join(dir, "loop.sh")
	body := "#!/bin/sh\n" +
		"echo ready > \"" + ready + "\"\n" +
		"trap 'echo term > \"" + termed + "\"; exit 0' TERM\n" +
		"while :; do sleep 0.2; done\n"
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatalf("write child script: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := execRunner{}.Run(ctx, []string{"sh", script}, false, func(string, string) error { return nil })
		done <- err
	}()

	deadline := time.Now().Add(10 * time.Second)
	for {
		if _, err := os.Stat(ready); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("child never became ready")
		}
		time.Sleep(20 * time.Millisecond)
	}
	cancel()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("Run after cancel = nil; want the context error")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Run did not return after cancel")
	}
	if _, err := os.Stat(termed); err != nil {
		t.Fatalf("child did not run its TERM trap (SIGKILL instead of SIGTERM?): %v", err)
	}
}

// equalStrings compares two string slices without importing slices in every
// test file.
func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
