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

const (
	streamStdout = "stdout"
	streamStderr = "stderr"
	streamAgent  = "agent"
)

type commandRunner interface {
	Run(ctx context.Context, argv []string, root bool, emit func(stream, line string) error) (int, error)
}

type execRunner struct{}

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

	wg.Wait()
	waitErr := command.Wait()

	mu.Lock()
	defer mu.Unlock()
	if emitErr != nil {
		return -1, emitErr
	}
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

type toolSender struct {
	stream grpc.ServerStreamingServer[agentv1.ToolEvent]
	mu     sync.Mutex
	err    error
}

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

func (s *toolSender) note(line string) { _ = s.emit(streamAgent, line) }

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
