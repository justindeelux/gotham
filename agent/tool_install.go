package agent

import (
	"bufio"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"

	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
	"github.com/justindeelux/gotham/toolcatalog"
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

// commandRunner runs one install step, streaming each output line to emit. It
// is an interface so the executor is driven by a fake in tests instead of the
// host.
type commandRunner interface {
	// Run executes step, calls emit for each line, and returns the process
	// exit code. A non-nil error means the command could not be started, was
	// cancelled, or emit failed; the exit code is -1 then.
	Run(ctx context.Context, step toolcatalog.Step, emit func(stream, line string) error) (int, error)
}

// execRunner runs a step as a child process. A Root step is elevated through
// the fixed `sudo -n` policy — the same shape as the agent's update wrapper: an
// explicit argv, never a shell string, never a tool id interpolated into a
// command — so the sudoers drop-in is the only thing that widens the agent's
// reach.
type execRunner struct{}

// Run implements commandRunner over os/exec, merging nothing: stdout and stderr
// keep their own tags so the control plane can style each line by source.
func (execRunner) Run(ctx context.Context, step toolcatalog.Step, emit func(string, string) error) (int, error) {
	if len(step.Argv) == 0 {
		return -1, errors.New("agent: empty install command")
	}
	argv := step.Argv
	if step.Root {
		argv = append([]string{"sudo", "-n"}, argv...)
	}

	// A cancelled send means the consumer is gone; cancelling this derived
	// context kills the child instead of letting it block on a full pipe.
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	command := exec.CommandContext(ctx, argv[0], argv[1:]...)
	command.Dir = step.Dir
	command.Env = append(os.Environ(), step.Env...)

	stdout, err := command.StdoutPipe()
	if err != nil {
		return -1, err
	}
	stderr, err := command.StderrPipe()
	if err != nil {
		return -1, err
	}
	if err := emit(streamAgent, "$ "+strings.Join(argv, " ")); err != nil {
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
	relay := func(source io.Reader, name string) {
		defer wg.Done()
		scanner := bufio.NewScanner(source)
		scanner.Buffer(make([]byte, 0, 64<<10), 1<<20)
		for scanner.Scan() {
			if err := emit(name, scanner.Text()); err != nil {
				mu.Lock()
				emitErr = err
				mu.Unlock()
				cancel()
				return
			}
		}
	}
	wg.Add(2)
	go relay(stdout, streamStdout)
	go relay(stderr, streamStderr)

	waitErr := command.Wait()
	wg.Wait()

	mu.Lock()
	defer mu.Unlock()
	if emitErr != nil {
		return -1, emitErr
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

// toolSender serialises writes to one install/uninstall stream: a step's two
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
