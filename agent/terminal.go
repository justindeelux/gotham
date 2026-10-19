package agent

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/creack/pty"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/justindeelux/gotham/agent/fsroot"
	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
)

const (
	// terminalTargetHost is the only target this package implements. A later
	// package adds container targets; anything else is refused rather than
	// silently falling back to the host.
	terminalTargetHost = "host"

	// terminalDefaultMaxSessions bounds concurrent host shells on one node.
	// A host PTY is an operator capability, not a per-user fan-out primitive.
	terminalDefaultMaxSessions = 8
	// terminalDefaultMaxInputBytes bounds one input message from the control
	// plane. Paste and key-repeat bursts are chunked by the caller.
	terminalDefaultMaxInputBytes = 64 << 10
	// terminalDefaultMaxDuration bounds one session's lifetime even while the
	// control plane still holds the stream open.
	terminalDefaultMaxDuration = 8 * time.Hour
	// terminalOutputChunk bounds one output message sent to the control plane.
	terminalOutputChunk = 32 << 10
	// terminalWriteTimeout bounds one write to the PTY master so a wedged shell
	// cannot pin the RPC handler forever.
	terminalWriteTimeout = 5 * time.Second
	// terminalKillGrace is how long a session gets to exit after SIGHUP before
	// its process group is killed outright.
	terminalKillGrace = 2 * time.Second
	// terminalStopWait bounds how long a handler waits for the output pump to
	// deliver the final event before returning anyway.
	terminalStopWait = terminalKillGrace + 2*time.Second

	terminalMinDimension = 1
	terminalMaxDimension = 1000

	// Upper bounds on caller-supplied path strings, matching the file
	// manager's own argument ceiling.
	terminalMaxRootBytes = 128
	terminalMaxCwdBytes  = 4096
)

// TerminalRoots resolves a FileService root-relative directory for an initial
// terminal cwd. *FileServer implements it.
type TerminalRoots interface {
	ResolveShellDir(rootName, rel string) (string, error)
}

// TerminalConfig configures a TerminalServer.
type TerminalConfig struct {
	// Logger receives session lifecycle lines. It never receives terminal
	// bytes or input.
	Logger *slog.Logger
	// Roots resolves the initial working directory. It is required; without a
	// file service there is no root policy to confine cwd to, so an open fails
	// with FailedPrecondition.
	Roots TerminalRoots
	// Shell is the absolute shell path to run. Empty selects the first usable
	// candidate from $SHELL, /bin/bash and /bin/sh.
	Shell string
	// MaxSessions bounds concurrent sessions; 0 uses the default.
	MaxSessions int
	// MaxInputBytes bounds one input message; 0 uses the default.
	MaxInputBytes int
	// MaxDuration bounds one session; 0 uses the default.
	MaxDuration time.Duration
}

// TerminalServer implements agentv1.TerminalServiceServer with a Linux PTY
// (github.com/creack/pty). Every stream owns exactly one shell: the PTY and its
// process group are killed and reaped when the stream ends, whether the shell
// exited, the control plane closed it, the context was cancelled or the session
// hit its lifetime cap.
type TerminalServer struct {
	agentv1.UnimplementedTerminalServiceServer

	log           *slog.Logger
	roots         TerminalRoots
	shell         string
	maxInputBytes int
	maxDuration   time.Duration
	sessions      chan struct{}

	// start is a seam for tests that must not spawn a real shell.
	start func(terminalStart) (*terminalSession, error)
}

// terminalStart is the validated input to a session.
type terminalStart struct {
	Shell string
	Dir   string
	Cols  uint16
	Rows  uint16
}

// NewTerminalServer returns a TerminalService implementation.
func NewTerminalServer(cfg TerminalConfig) *TerminalServer {
	log := cfg.Logger
	if log == nil {
		log = slog.Default()
	}
	maxSessions := cfg.MaxSessions
	if maxSessions <= 0 {
		maxSessions = terminalDefaultMaxSessions
	}
	maxInput := cfg.MaxInputBytes
	if maxInput <= 0 {
		maxInput = terminalDefaultMaxInputBytes
	}
	maxDuration := cfg.MaxDuration
	if maxDuration <= 0 {
		maxDuration = terminalDefaultMaxDuration
	}
	server := &TerminalServer{
		log:           log,
		roots:         cfg.Roots,
		shell:         cfg.Shell,
		maxInputBytes: maxInput,
		maxDuration:   maxDuration,
		sessions:      make(chan struct{}, maxSessions),
	}
	server.start = server.startSession
	return server
}

// OpenTerminal runs one PTY-backed host shell for the lifetime of the stream.
func (s *TerminalServer) OpenTerminal(stream grpc.BidiStreamingServer[agentv1.TerminalRequest, agentv1.TerminalResponse]) error {
	ctx := stream.Context()

	first, err := stream.Recv()
	if err != nil {
		if errors.Is(err, io.EOF) {
			return status.Error(codes.InvalidArgument, "terminal: stream closed before open")
		}
		return terminalStreamError("receive open", err)
	}
	open := first.GetOpen()
	if open == nil {
		return status.Error(codes.InvalidArgument, "terminal: first message must be open")
	}
	if s.roots == nil {
		return status.Error(codes.FailedPrecondition, "terminal: the node has no file roots to resolve a working directory")
	}
	spec, err := s.prepare(open)
	if err != nil {
		return err
	}

	select {
	case s.sessions <- struct{}{}:
		defer func() { <-s.sessions }()
	default:
		return status.Errorf(codes.ResourceExhausted, "terminal: the node already runs %d sessions", cap(s.sessions))
	}

	session, err := s.start(spec)
	if err != nil {
		s.log.Warn("terminal: start shell", "error", err)
		return status.Error(codes.Internal, "terminal: could not start the shell")
	}
	defer session.terminate()

	if err := stream.Send(&agentv1.TerminalResponse{
		Event: &agentv1.TerminalResponse_Opened{Opened: &agentv1.TerminalOpened{
			Shell:       spec.Shell,
			Cols:        uint32(spec.Cols),
			Rows:        uint32(spec.Rows),
			ServiceUser: serviceUser(),
		}},
	}); err != nil {
		return err
	}

	output := make(chan error, 1)
	go func() { output <- s.pumpOutput(ctx, stream, session) }()

	incoming := make(chan terminalRecv, 1)
	go receiveTerminal(stream, incoming)

	lifetime := time.NewTimer(s.maxDuration)
	defer lifetime.Stop()

	// stop ends the shell and waits for the output pump to deliver its final
	// event, so no goroutine writes to the stream after the handler returns.
	stop := func() error {
		session.terminate()
		select {
		case err := <-output:
			return err
		case <-time.After(terminalStopWait):
			return nil
		}
	}

	for {
		select {
		case err := <-output:
			// The shell exited (or the PTY failed); the pump already sent the
			// exit event when it could.
			if err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, io.EOF) {
				return err
			}
			if ctx.Err() != nil {
				return status.FromContextError(ctx.Err()).Err()
			}
			return nil
		case <-ctx.Done():
			_ = stop()
			return status.FromContextError(ctx.Err()).Err()
		case <-lifetime.C:
			_ = stop()
			return status.Error(codes.DeadlineExceeded, "terminal: session reached its maximum lifetime")
		case msg, ok := <-incoming:
			if !ok {
				if ctx.Err() != nil {
					return status.FromContextError(ctx.Err()).Err()
				}
				return nil
			}
			if msg.err != nil {
				if errors.Is(msg.err, io.EOF) || errors.Is(msg.err, context.Canceled) {
					_ = stop()
					return nil
				}
				_ = stop()
				return terminalStreamError("receive", msg.err)
			}
			switch part := msg.req.GetPart().(type) {
			case *agentv1.TerminalRequest_Input:
				if len(part.Input) == 0 {
					continue
				}
				if len(part.Input) > s.maxInputBytes {
					return status.Errorf(codes.InvalidArgument, "terminal: input chunk exceeds %d bytes", s.maxInputBytes)
				}
				if err := session.write(part.Input); err != nil {
					_ = stop()
					return status.Error(codes.Internal, "terminal: writing to the shell failed")
				}
			case *agentv1.TerminalRequest_Resize:
				cols, rows, err := terminalDimensions(part.Resize.GetCols(), part.Resize.GetRows())
				if err != nil {
					return err
				}
				if err := session.resize(cols, rows); err != nil {
					_ = stop()
					return status.Error(codes.Internal, "terminal: resizing the shell failed")
				}
			case *agentv1.TerminalRequest_Close:
				_ = stop()
				return nil
			case *agentv1.TerminalRequest_Open:
				return status.Error(codes.InvalidArgument, "terminal: open is only valid as the first message")
			default:
				return status.Error(codes.InvalidArgument, "terminal: unknown request")
			}
		}
	}
}

// terminalRecv is one value from the receive goroutine.
type terminalRecv struct {
	req *agentv1.TerminalRequest
	err error
}

// receiveTerminal forwards stream messages until the stream ends. It exits
// when the handler returns because gRPC unblocks Recv then.
func receiveTerminal(stream grpc.BidiStreamingServer[agentv1.TerminalRequest, agentv1.TerminalResponse], out chan<- terminalRecv) {
	defer close(out)
	for {
		req, err := stream.Recv()
		if err != nil {
			out <- terminalRecv{err: err}
			return
		}
		select {
		case out <- terminalRecv{req: req}:
		case <-stream.Context().Done():
			return
		}
	}
}

// prepare validates an open request and resolves its working directory.
func (s *TerminalServer) prepare(open *agentv1.TerminalOpen) (terminalStart, error) {
	switch strings.TrimSpace(open.GetTarget()) {
	case "", terminalTargetHost:
	default:
		return terminalStart{}, status.Errorf(codes.InvalidArgument, "terminal: unsupported target %q", open.GetTarget())
	}
	if len(open.GetRoot()) == 0 || len(open.GetRoot()) > terminalMaxRootBytes {
		return terminalStart{}, status.Error(codes.InvalidArgument, "terminal: a bounded root name is required")
	}
	if len(open.GetCwd()) > terminalMaxCwdBytes || strings.IndexByte(open.GetCwd(), 0) >= 0 {
		return terminalStart{}, status.Error(codes.InvalidArgument, "terminal: invalid working directory")
	}
	cols, rows, err := terminalDimensions(open.GetCols(), open.GetRows())
	if err != nil {
		return terminalStart{}, err
	}
	dir, err := s.roots.ResolveShellDir(open.GetRoot(), open.GetCwd())
	if err != nil {
		return terminalStart{}, terminalDirError(err)
	}
	shell, err := resolveTerminalShell(s.shell)
	if err != nil {
		s.log.Error("terminal: no usable shell", "configured", s.shell, "error", err)
		return terminalStart{}, status.Error(codes.FailedPrecondition, "terminal: no usable shell is installed on the node")
	}
	return terminalStart{Shell: shell, Dir: dir, Cols: cols, Rows: rows}, nil
}

// terminalDimensions validates a PTY size.
func terminalDimensions(cols, rows uint32) (uint16, uint16, error) {
	if cols < terminalMinDimension || cols > terminalMaxDimension ||
		rows < terminalMinDimension || rows > terminalMaxDimension {
		return 0, 0, status.Errorf(codes.InvalidArgument,
			"terminal: dimensions must be between %d and %d columns/rows", terminalMinDimension, terminalMaxDimension)
	}
	return uint16(cols), uint16(rows), nil
}

// terminalDirError maps an fsroot resolution failure onto a gRPC status.
func terminalDirError(err error) error {
	switch {
	case errors.Is(err, fsroot.ErrNotFound):
		return status.Errorf(codes.NotFound, "terminal: working directory not found: %v", err)
	case errors.Is(err, fsroot.ErrEscape), errors.Is(err, fsroot.ErrInvalid):
		return status.Errorf(codes.InvalidArgument, "terminal: invalid working directory: %v", err)
	case errors.Is(err, fsroot.ErrPermission):
		return status.Errorf(codes.PermissionDenied, "terminal: working directory is not readable")
	default:
		if status.Code(err) != codes.Unknown {
			return err
		}
		return status.Errorf(codes.InvalidArgument, "terminal: invalid working directory: %v", err)
	}
}

// terminalStreamError maps a transport error without leaking stream details.
func terminalStreamError(action string, err error) error {
	switch {
	case errors.Is(err, context.Canceled):
		return status.Error(codes.Canceled, "terminal: "+action+": canceled")
	case errors.Is(err, context.DeadlineExceeded):
		return status.Error(codes.DeadlineExceeded, "terminal: "+action+": deadline exceeded")
	default:
		return status.Errorf(codes.Unavailable, "terminal: %s: %v", action, err)
	}
}

// pumpOutput reads the PTY until it closes and then reports the exit status.
// It is the only goroutine that sends on the stream.
func (s *TerminalServer) pumpOutput(
	ctx context.Context,
	stream grpc.BidiStreamingServer[agentv1.TerminalRequest, agentv1.TerminalResponse],
	session *terminalSession,
) error {
	buf := make([]byte, terminalOutputChunk)
	for {
		n, err := session.pty.Read(buf)
		if n > 0 {
			chunk := make([]byte, n)
			copy(chunk, buf[:n])
			if sendErr := stream.Send(&agentv1.TerminalResponse{
				Event: &agentv1.TerminalResponse_Output{Output: chunk},
			}); sendErr != nil {
				session.terminate()
				return sendErr
			}
		}
		if err != nil {
			break
		}
		if ctx.Err() != nil {
			session.terminate()
			return ctx.Err()
		}
	}

	code, signal := session.wait()
	return stream.Send(&agentv1.TerminalResponse{
		Event: &agentv1.TerminalResponse_Exited{Exited: &agentv1.TerminalExited{
			ExitCode: code,
			Signal:   signal,
		}},
	})
}

// startSession starts the configured shell on a new PTY.
func (s *TerminalServer) startSession(spec terminalStart) (*terminalSession, error) {
	cmd := exec.Command(spec.Shell) // #nosec G204 -- the shell is chosen by the node, not the caller.
	cmd.Dir = spec.Dir
	cmd.Env = terminalEnv()
	master, err := pty.StartWithSize(cmd, &pty.Winsize{Cols: spec.Cols, Rows: spec.Rows})
	if err != nil {
		return nil, fmt.Errorf("start %s: %w", spec.Shell, err)
	}
	return newTerminalSession(cmd, master), nil
}

// resolveTerminalShell picks the shell the node runs. The configured value
// must be an absolute, executable regular file; otherwise the first usable
// candidate from $SHELL, /bin/bash and /bin/sh wins.
func resolveTerminalShell(configured string) (string, error) {
	candidates := []string{configured}
	if strings.TrimSpace(configured) == "" {
		candidates = []string{os.Getenv("SHELL"), "/bin/bash", "/bin/sh"}
	}
	for _, candidate := range candidates {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" || !filepath.IsAbs(candidate) {
			continue
		}
		info, err := os.Stat(candidate)
		if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 {
			continue
		}
		return candidate, nil
	}
	return "", errors.New("no absolute executable shell found")
}

// terminalSafeEnv is the environment a host shell inherits. It is an allowlist
// on purpose: the agent process holds secrets (the enrollment token, credential
// paths) and a root shell must never inherit them.
var terminalSafeEnv = map[string]bool{
	"PATH": true, "HOME": true, "USER": true, "LOGNAME": true, "SHELL": true,
	"LANG": true, "LANGUAGE": true, "TZ": true, "TMPDIR": true, "PWD": true,
	"XDG_RUNTIME_DIR": true, "TERM_PROGRAM": true,
}

// terminalEnv builds the child environment from the allowlist plus a terminal
// description. LC_* locale variables are kept so UTF-8 and collation behave
// like the operator's locale.
func terminalEnv() []string {
	env := make([]string, 0, 16)
	for _, entry := range os.Environ() {
		name, _, ok := strings.Cut(entry, "=")
		if !ok {
			continue
		}
		if terminalSafeEnv[name] || strings.HasPrefix(name, "LC_") {
			env = append(env, entry)
		}
	}
	env = append(env, "TERM=xterm-256color", "COLORTERM=truecolor")
	return env
}

// serviceUser reports the OS identity the shell runs as.
func serviceUser() string {
	if name := strings.TrimSpace(os.Getenv("USER")); name != "" {
		return name
	}
	return currentUsername()
}

// terminalSession owns one PTY and its child process.
type terminalSession struct {
	cmd  *exec.Cmd
	pty  *os.File
	pid  int
	done chan struct{}

	termOnce sync.Once
	waitOnce sync.Once
	state    *os.ProcessState
	waitErr  error
}

func newTerminalSession(cmd *exec.Cmd, master *os.File) *terminalSession {
	return &terminalSession{
		cmd:  cmd,
		pty:  master,
		pid:  cmd.Process.Pid,
		done: make(chan struct{}),
	}
}

// terminate asks the process group to exit, closes the PTY and schedules a hard
// kill if the child ignores SIGHUP. It is idempotent and never waits.
func (s *terminalSession) terminate() {
	s.termOnce.Do(func() {
		_ = killTerminalProcessGroup(s.pid, terminalHangup)
		_ = s.pty.Close()
		go func() {
			select {
			case <-s.done:
			case <-time.After(terminalKillGrace):
				_ = killTerminalProcessGroup(s.pid, terminalKill)
			}
		}()
	})
}

// write sends input to the PTY master with a bounded deadline so a shell that
// stops reading cannot pin the handler.
func (s *terminalSession) write(data []byte) error {
	_ = s.pty.SetWriteDeadline(time.Now().Add(terminalWriteTimeout))
	_, err := s.pty.Write(data)
	return err
}

// resize applies a new window size.
func (s *terminalSession) resize(cols, rows uint16) error {
	return pty.Setsize(s.pty, &pty.Winsize{Cols: cols, Rows: rows})
}

// wait reaps the child and reports its exit status.
func (s *terminalSession) wait() (int32, string) {
	s.waitOnce.Do(func() {
		s.waitErr = s.cmd.Wait()
		s.state = s.cmd.ProcessState
		close(s.done)
	})
	<-s.done
	return terminalProcessStatus(s.state)
}
