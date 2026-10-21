package tools

import (
	"bufio"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"

	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
	"google.golang.org/grpc"
)

// Output streams the install/uninstall executor tags its lines with. "stdout"
// and "stderr" name the command's own output; "agent" marks a line the executor
// itself produced (the command line, a skip notice, a timeout), so a consumer
// never mistakes it for command output.
const (
	streamStdout = "stdout"
	streamStderr = "stderr"
	streamAgent  = "agent"
)

// commandRunner runs one privileged install command, streaming each output
// line to emit. It is an interface so the executor is driven by a fake in
// tests instead of the host.
type commandRunner interface {
	// Run executes argv, elevated to root when root is set, calls emit for
	// each line, and returns the process exit code. A non-nil error means the
	// command could not be started, was cancelled, or emit failed; the exit
	// code is -1 then.
	Run(ctx context.Context, argv []string, root bool, emit func(stream, line string) error) (int, error)
}

// execRunner runs a command as a child process. A root command is elevated
// through the fixed `sudo -n` policy — the same shape as the agent's update
// wrapper: an explicit argv, never a shell string — so the sudoers drop-in is
// the only thing that widens the agent's reach. The argv names the root-owned
// gotham-toolctl helper with a catalog id; the helper owns every executable
// argument, so a tool id is never interpolated into a command here.
type execRunner struct{}

// Run implements commandRunner over os/exec, keeping stdout and stderr tags.
//
// The whole output is drained before command.Wait: os/exec closes the pipes
// when Wait runs, so reading them concurrently would drop trailing lines (the
// actual error message) or fail with "file already closed". A failed send
// cancels the derived context, which kills the child, closes the pipes and
// unblocks the reads.
func (execRunner) Run(ctx context.Context, argv []string, root bool, emit func(string, string) error) (int, error) {
	if len(argv) == 0 {
		return -1, errors.New("agent: empty install command")
	}
	elevated := argv
	if root {
		elevated = append([]string{"sudo", "-n"}, argv...)
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	command := exec.CommandContext(ctx, elevated[0], elevated[1:]...)
	command.Env = os.Environ()
	// A cancelled request must not orphan the helper's transient unit: SIGKILL
	// (the CommandContext default) would leave it running. Ask the helper to
	// stop the unit, then bound how long a wedged child may delay the return.
	command.Cancel = func() error {
		if command.Process == nil {
			return nil
		}
		return command.Process.Signal(syscall.SIGTERM)
	}
	command.WaitDelay = 5 * time.Second

	stdout, err := command.StdoutPipe()
	if err != nil {
		return -1, err
	}
	stderr, err := command.StderrPipe()
	if err != nil {
		return -1, err
	}
	if err := emit(streamAgent, "$ "+strings.Join(elevated, " ")); err != nil {
		return -1, err
	}
	if err := command.Start(); err != nil {
		return -1, err
	}

	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		emitErr error
	)
	relay := func(source io.Reader, name string, breaker func()) {
		defer wg.Done()
		scanner := bufio.NewScanner(source)
		scanner.Buffer(make([]byte, 0, 64<<10), 1<<20)
		for scanner.Scan() {
			if err := emit(name, scanner.Text()); err != nil {
				mu.Lock()
				emitErr = err
				mu.Unlock()
				breaker()
				return
			}
		}
	}
	wg.Add(2)
	go relay(stdout, streamStdout, cancel)
	go relay(stderr, streamStderr, cancel)

	// Drain both pipes to EOF before reaping the child (see the doc comment).
	wg.Wait()
	waitErr := command.Wait()

	mu.Lock()
	defer mu.Unlock()
	if emitErr != nil {
		return -1, emitErr
	}
	// A cancelled or timed-out context is not a successful command, even if the
	// child exited 0 on its own (a graceful SIGTERM lets it run an exit trap).
	if ctxErr := ctx.Err(); ctxErr != nil {
		return -1, ctxErr
	}
	if waitErr == nil {
		return 0, nil
	}
	var exit *exec.ExitError
	if errors.As(waitErr, &exit) {
		return exit.ExitCode(), waitErr
	}
	return -1, waitErr
}

// toolSender serialises writes to one install/uninstall stream: a command's two
// output pipes emit concurrently and gRPC Send is not safe for concurrent use.
type toolSender struct {
	stream grpc.ServerStreamingServer[agentv1.ToolEvent]
	mu     sync.Mutex
	err    error
}

// emit sends one output line, remembering the first send failure so the
// executor stops streaming against a dead consumer.
func (s *toolSender) emit(stream, line string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return s.err
	}
	s.err = s.stream.Send(&agentv1.ToolEvent{
		Event: &agentv1.ToolEvent_Output{Output: &agentv1.ToolOutput{Line: line, Stream: stream}},
	})
	return s.err
}

// note sends one executor-produced line, ignoring its own failure: the next
// emit or result returns the recorded error.
func (s *toolSender) note(line string) { _ = s.emit(streamAgent, line) }

// result closes the stream with the final status and exit code.
func (s *toolSender) result(status string, code int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return s.err
	}
	s.err = s.stream.Send(&agentv1.ToolEvent{
		Event: &agentv1.ToolEvent_Result{Result: &agentv1.ToolResult{Status: status, ExitCode: int32(code)}},
	})
	return s.err
}
