package agent

import (
	"context"
	"errors"
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

// fakeToolRunner is a commandRunner for tests: it records every argv and, when
// step is set, delegates to it instead of the default success.
type fakeToolRunner struct {
	mu    sync.Mutex
	calls [][]string
	step  func(ctx context.Context, argv []string, emit func(stream, line string) error) (int, error)
}

func (r *fakeToolRunner) Run(ctx context.Context, step toolcatalog.Step, emit func(string, string) error) (int, error) {
	r.mu.Lock()
	r.calls = append(r.calls, append([]string(nil), step.Argv...))
	override := r.step
	r.mu.Unlock()
	if override != nil {
		return override(ctx, step.Argv, emit)
	}
	if err := emit(streamStdout, "ok: "+strings.Join(step.Argv, " ")); err != nil {
		return -1, err
	}
	return 0, nil
}

// commands returns the argv of every step the executor ran, in order.
func (r *fakeToolRunner) commands() [][]string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([][]string(nil), r.calls...)
}

// commandIndex returns the position of the first step whose joined argv
// contains fragment, or -1.
func (r *fakeToolRunner) commandIndex(fragment string) int {
	for i, argv := range r.commands() {
		if strings.Contains(strings.Join(argv, " "), fragment) {
			return i
		}
	}
	return -1
}

// testToolEnv is a fixed install environment so plans are host-independent.
func testToolEnv() toolcatalog.Env {
	return toolcatalog.Env{
		Arch:    "amd64",
		Distro:  toolcatalog.DistroApt,
		BinDir:  "/usr/local/bin",
		TempDir: "/tmp",
	}
}

// installedNone reports every tool absent; installedOnly reports exactly the
// named tools present.
func installedNone(string) bool { return false }

func installedOnly(ids ...string) func(string) bool {
	present := make(map[string]bool, len(ids))
	for _, id := range ids {
		present[id] = true
	}
	return func(id string) bool { return present[id] }
}

// TestToolServerInstallRunsDependenciesFirst proves the dependency chain
// railpack => buildkit => docker is installed in order, each before the tool
// that needs it, and the stream closes with a success result.
func TestToolServerInstallRunsDependenciesFirst(t *testing.T) {
	runner := &fakeToolRunner{}
	server := NewToolServer(
		withToolRunner(runner),
		withToolEnv(testToolEnv()),
		withToolInstalled(installedNone),
	)

	stream := newFakeToolStream(context.Background())
	if err := server.InstallTool(&agentv1.InstallToolRequest{Id: toolcatalog.IDRailpack}, stream); err != nil {
		t.Fatalf("InstallTool(railpack): %v", err)
	}

	dockerInstall := runner.commandIndex("apt-get install -y docker.io")
	buildkitRun := runner.commandIndex("docker run")
	railpackInstall := runner.commandIndex("gotham-railpack-install.sh -y -b")
	switch {
	case dockerInstall < 0 || buildkitRun < 0 || railpackInstall < 0:
		t.Fatalf("missing a step; ran %v", runner.commands())
	case dockerInstall >= buildkitRun || buildkitRun >= railpackInstall:
		t.Fatalf("steps ran out of dependency order: docker=%d buildkit=%d railpack=%d",
			dockerInstall, buildkitRun, railpackInstall)
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
		withToolEnv(testToolEnv()),
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
	runner := &fakeToolRunner{step: func(_ context.Context, argv []string, emit func(string, string) error) (int, error) {
		if err := emit(streamStderr, "E: unable to locate package git"); err != nil {
			return -1, err
		}
		return 100, errors.New("exit status 100")
	}}
	server := NewToolServer(
		withToolRunner(runner),
		withToolEnv(testToolEnv()),
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
	runner := &fakeToolRunner{step: func(ctx context.Context, _ []string, _ func(string, string) error) (int, error) {
		<-ctx.Done()
		return -1, ctx.Err()
	}}
	server := NewToolServer(
		withToolRunner(runner),
		withToolEnv(testToolEnv()),
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
	runner := &fakeToolRunner{step: func(ctx context.Context, _ []string, _ func(string, string) error) (int, error) {
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
		withToolEnv(testToolEnv()),
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

// TestToolServerUninstallRefusesInUse proves an uninstall is refused while the
// tool is busy, before any command runs.
func TestToolServerUninstallRefusesInUse(t *testing.T) {
	runner := &fakeToolRunner{}
	server := NewToolServer(
		withToolRunner(runner),
		withToolEnv(testToolEnv()),
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
		withToolEnv(testToolEnv()),
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
	if runner.commandIndex("-r -b") < 0 {
		t.Fatalf("railpack uninstall did not run the remove flag: %v", runner.commands())
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
		withToolEnv(testToolEnv()),
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

// TestToolServerInstallTimeout proves a step that outlives the install timeout
// is killed and reported as a failed install, not left running.
func TestToolServerInstallTimeout(t *testing.T) {
	runner := &fakeToolRunner{step: func(ctx context.Context, _ []string, _ func(string, string) error) (int, error) {
		<-ctx.Done()
		return -1, ctx.Err()
	}}
	server := NewToolServer(
		withToolRunner(runner),
		withToolEnv(testToolEnv()),
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
