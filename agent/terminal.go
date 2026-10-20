package agent

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"os/user"
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
	// terminalTargetHost runs a host shell. terminalTargetContainer runs an
	// interactive exec inside a resolved application container. Anything else
	// is refused rather than silently falling back to the host.
	terminalTargetHost      = "host"
	terminalTargetContainer = "container"

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

// ContainerExec runs an interactive exec inside one container. *DockerClient
// implements it over the Docker Engine HTTP client; tests substitute a fake.
type ContainerExec interface {
	Exec(ctx context.Context, containerID string, opts ExecOptions) (ExecSession, error)
}

// TerminalConfig configures a TerminalServer.
type TerminalConfig struct {
	// Logger receives session lifecycle lines. It never receives terminal
	// bytes or input.
	Logger *slog.Logger
	// Roots resolves the initial working directory for host targets that name a
	// FileService root. A host session without a root does not use it and opens
	// in the agent's default directory, so a node with the file service
	// disabled can still run a shell. Container targets do not use it.
	Roots TerminalRoots
	// Shell is the absolute shell path to run for host targets. Empty selects
	// the first usable candidate from $SHELL, /bin/bash and /bin/sh.
	Shell string
	// Containers runs container targets. nil makes them fail with
	// FailedPrecondition instead of falling back to a host shell.
	Containers ContainerExec
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
	containers    ContainerExec

	// start is a seam for tests that must not spawn a real shell.
	start func(terminalStart) (terminalProcess, error)
}

// terminalStart is the validated input to a session.
type terminalStart struct {
	Target      string
	Shell       string
	Dir         string
	ContainerID string
	Cols        uint16
	Rows        uint16
}

// terminalProcess is the small surface OpenTerminal needs from either a host
// PTY session or a container exec session.
type terminalProcess interface {
	read(p []byte) (int, error)
	write(data []byte) error
	resize(cols, rows uint16) error
	terminate()
	wait() (int32, string)
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
		containers:    cfg.Containers,
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

	var session terminalProcess
	if spec.Target == terminalTargetContainer {
		session, err = s.startContainer(ctx, spec)
	} else {
		session, err = s.start(spec)
	}
	if err != nil {
		s.log.Warn("terminal: start session", "target", spec.Target, "error", err)
		return terminalStartError(err)
	}
	defer session.terminate()

	shell, user := terminalSessionIdentity(session, spec)
	if err := stream.Send(&agentv1.TerminalResponse{
		Event: &agentv1.TerminalResponse_Opened{Opened: &agentv1.TerminalOpened{
			Shell:       shell,
			Cols:        uint32(spec.Cols),
			Rows:        uint32(spec.Rows),
			ServiceUser: user,
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
	// It is idempotent and runs on every exit path once the pump exists; a
	// pump result already consumed by the loop is not read twice.
	var (
		stopOnce     sync.Once
		stopErr      error
		pumpConsumed bool
	)
	stop := func() error {
		stopOnce.Do(func() {
			session.terminate()
			if pumpConsumed {
				return
			}
			select {
			case stopErr = <-output:
				pumpConsumed = true
			case <-time.After(terminalStopWait):
			}
		})
		return stopErr
	}
	defer func() { _ = stop() }()

	for {
		select {
		case err := <-output:
			pumpConsumed = true
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
			return status.FromContextError(ctx.Err()).Err()
		case <-lifetime.C:
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
					return nil
				}
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
					return status.Error(codes.Internal, "terminal: writing to the shell failed")
				}
			case *agentv1.TerminalRequest_Resize:
				cols, rows, err := terminalDimensions(part.Resize.GetCols(), part.Resize.GetRows())
				if err != nil {
					return err
				}
				if err := session.resize(cols, rows); err != nil {
					return status.Error(codes.Internal, "terminal: resizing the shell failed")
				}
			case *agentv1.TerminalRequest_Close:
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

// prepare validates an open request and resolves its target.
func (s *TerminalServer) prepare(open *agentv1.TerminalOpen) (terminalStart, error) {
	switch target := strings.TrimSpace(open.GetTarget()); target {
	case "", terminalTargetHost:
		return s.prepareHost(open)
	case terminalTargetContainer:
		return s.prepareContainer(open)
	default:
		return terminalStart{}, status.Errorf(codes.InvalidArgument, "terminal: unsupported target %q", target)
	}
}

// prepareHost validates a host shell open and resolves its initial directory.
//
// A root and cwd are optional. With both omitted the shell starts in a default
// directory the agent resolves from the service user's passwd entry — never
// from the request, a browser value or the agent environment — so a host shell
// does not depend on file roots being configured. A cwd without a root is
// refused. A provided root keeps the file manager's exact allow-list, relative
// path, escape and existence policy.
func (s *TerminalServer) prepareHost(open *agentv1.TerminalOpen) (terminalStart, error) {
	root := open.GetRoot()
	cwd := open.GetCwd()
	if len(root) > terminalMaxRootBytes {
		return terminalStart{}, status.Error(codes.InvalidArgument, "terminal: invalid file root")
	}
	if len(cwd) > terminalMaxCwdBytes || strings.IndexByte(cwd, 0) >= 0 {
		return terminalStart{}, status.Error(codes.InvalidArgument, "terminal: invalid working directory")
	}
	cols, rows, err := terminalDimensions(open.GetCols(), open.GetRows())
	if err != nil {
		return terminalStart{}, err
	}
	var dir string
	if root == "" {
		if cwd != "" {
			return terminalStart{}, status.Error(codes.InvalidArgument,
				"terminal: a working directory requires a file root")
		}
		dir = defaultTerminalDir()
	} else {
		if s.roots == nil {
			return terminalStart{}, status.Error(codes.FailedPrecondition,
				"terminal: the node has no file roots to resolve a working directory")
		}
		dir, err = s.roots.ResolveShellDir(root, cwd)
		if err != nil {
			return terminalStart{}, terminalDirError(err)
		}
	}
	shell, err := resolveTerminalShell(s.shell)
	if err != nil {
		s.log.Error("terminal: no usable shell", "configured", s.shell, "error", err)
		return terminalStart{}, status.Error(codes.FailedPrecondition, "terminal: no usable shell is installed on the node")
	}
	return terminalStart{Target: terminalTargetHost, Shell: shell, Dir: dir, Cols: cols, Rows: rows}, nil
}

// defaultTerminalDir resolves the initial working directory for a host shell
// opened without a file root. It is decided on the agent, from the service
// user's passwd entry — never from the request, a browser value or the agent
// environment. The home directory is resolved (symlinks evaluated) and must be
// an existing directory; anything else falls back to the filesystem root. The
// result is set directly on the child process, never interpolated into a shell
// command.
func defaultTerminalDir() string {
	return resolveDefaultTerminalDir(serviceHomeDir())
}

// serviceHomeDir reads the service user's home directory from its passwd entry.
func serviceHomeDir() string {
	current, err := user.Current()
	if err != nil {
		return ""
	}
	return current.HomeDir
}

// resolveDefaultTerminalDir validates one default-directory candidate. It is
// separate from defaultTerminalDir so the fallback paths are testable: an
// empty or relative home, a missing target, a symlink that does not resolve to
// a directory, and a file all fall back to "/".
func resolveDefaultTerminalDir(home string) string {
	if home == "" || !filepath.IsAbs(home) {
		return "/"
	}
	resolved, err := filepath.EvalSymlinks(home)
	if err != nil {
		return "/"
	}
	info, err := os.Stat(resolved)
	if err != nil || !info.IsDir() {
		return "/"
	}
	return resolved
}

// prepareContainer validates a container exec open. The container id is
// resolved by the control plane from the application's running deployment; the
// agent still verifies the container exists and is running before starting.
func (s *TerminalServer) prepareContainer(open *agentv1.TerminalOpen) (terminalStart, error) {
	containerID := strings.TrimSpace(open.GetContainerId())
	if !validContainerID(containerID) {
		return terminalStart{}, status.Error(codes.InvalidArgument, "terminal: a valid container id is required")
	}
	if s.containers == nil {
		return terminalStart{}, status.Error(codes.FailedPrecondition,
			"terminal: container sessions are not available on this node")
	}
	if strings.TrimSpace(open.GetRoot()) != "" || strings.TrimSpace(open.GetCwd()) != "" {
		return terminalStart{}, status.Error(codes.InvalidArgument,
			"terminal: container targets do not take a file root or working directory")
	}
	cols, rows, err := terminalDimensions(open.GetCols(), open.GetRows())
	if err != nil {
		return terminalStart{}, err
	}
	shell := strings.TrimSpace(open.GetShell())
	if shell != "" && !validContainerShell(shell) {
		return terminalStart{}, status.Error(codes.InvalidArgument, "terminal: invalid container shell path")
	}
	return terminalStart{Target: terminalTargetContainer, ContainerID: containerID, Shell: shell, Cols: cols, Rows: rows}, nil
}

// validContainerID requires the full lowercase hexadecimal Docker id the
// control plane resolves. Names, short prefixes and upper-case spellings are
// refused so the agent never resolves an ambiguous or caller-chosen name.
func validContainerID(id string) bool {
	if len(id) != 64 {
		return false
	}
	for i := 0; i < len(id); i++ {
		c := id[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
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
	session terminalProcess,
) error {
	buf := make([]byte, terminalOutputChunk)
	for {
		n, err := session.read(buf)
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
func (s *TerminalServer) startSession(spec terminalStart) (terminalProcess, error) {
	cmd := exec.Command(spec.Shell) // #nosec G204 -- the shell is chosen by the node, not the caller.
	cmd.Dir = spec.Dir
	cmd.Env = terminalEnv()
	master, err := pty.StartWithSize(cmd, &pty.Winsize{Cols: spec.Cols, Rows: spec.Rows})
	if err != nil {
		return nil, fmt.Errorf("start %s: %w", spec.Shell, err)
	}
	return newTerminalSession(cmd, master), nil
}

// startContainer opens an interactive exec in the resolved container.
func (s *TerminalServer) startContainer(ctx context.Context, spec terminalStart) (terminalProcess, error) {
	if s.containers == nil {
		return nil, errors.New("container exec is not configured")
	}
	opts := ExecOptions{Cols: spec.Cols, Rows: spec.Rows}
	if spec.Shell != "" {
		opts.Cmd = []string{spec.Shell}
	}
	stream, err := s.containers.Exec(ctx, spec.ContainerID, opts)
	if err != nil {
		return nil, err
	}
	return &containerTerminalSession{
		stream:      stream,
		ctx:         ctx,
		log:         s.log,
		containerID: spec.ContainerID,
		done:        make(chan struct{}),
	}, nil
}

// containerTerminalSession adapts one Docker exec to terminalProcess.
type containerTerminalSession struct {
	stream      ExecSession
	ctx         context.Context
	log         *slog.Logger
	containerID string

	termOnce sync.Once
	waitOnce sync.Once
	code     int32
	signal   string
	done     chan struct{}
}

// read reads the exec process's output.
func (s *containerTerminalSession) read(p []byte) (int, error) { return s.stream.Read(p) }

// write sends input to the exec process with a bounded wait. The hijacked
// Docker stream exposes no write deadline, so a wedged container can otherwise
// block the handler, which would also make it deaf to context cancellation.
// The stream is closed on timeout to unblock the writer.
func (s *containerTerminalSession) write(data []byte) error {
	done := make(chan error, 1)
	go func() {
		_, err := s.stream.Write(data)
		done <- err
	}()
	select {
	case err := <-done:
		return err
	case <-time.After(terminalWriteTimeout):
		_ = s.stream.Close()
		return errors.New("terminal: writing to the container timed out")
	case <-s.ctx.Done():
		_ = s.stream.Close()
		return s.ctx.Err()
	}
}

// resize applies a new window size to the container TTY.
func (s *containerTerminalSession) resize(cols, rows uint16) error {
	return s.stream.Resize(s.ctx, cols, rows)
}

// terminate ends the exec process tree and releases the attachment. Closing the
// hijacked connection alone does not stop the container process, so Close runs
// the explicit cleanup with a detached, bounded context (the terminal stream's
// context is usually already cancelled on this path) and returns any failure.
// A failure is logged at ERROR with the container id, never silently treated as
// success. The application container keeps running: only the interactive
// process and its jobs go away.
func (s *containerTerminalSession) terminate() {
	s.termOnce.Do(func() {
		if err := s.stream.Close(); err != nil && s.log != nil {
			s.log.Error("terminal: container exec cleanup incomplete",
				"container_id", s.containerID, "error", err)
		}
	})
}

// wait reports the exec process's exit status. The exit lookup runs on a
// context detached from the stream so a canceled relay still reaps the status.
func (s *containerTerminalSession) wait() (int32, string) {
	s.waitOnce.Do(func() {
		ctx, cancel := context.WithTimeout(context.WithoutCancel(s.ctx), terminalStopWait)
		defer cancel()
		code, signal, err := s.stream.ExitCode(ctx)
		if err != nil {
			code, signal = -1, ""
		}
		s.code, s.signal = code, signal
		close(s.done)
	})
	<-s.done
	return s.code, s.signal
}

// Shell reports the shell executable the agent chose in the container.
func (s *containerTerminalSession) Shell() string { return s.stream.Shell() }

// User reports the container user the exec runs as.
func (s *containerTerminalSession) User() string { return s.stream.User() }

// terminalSessionIdentity reports the shell and the user shown in the opened
// event for either target.
func terminalSessionIdentity(session terminalProcess, spec terminalStart) (string, string) {
	if identity, ok := session.(interface {
		Shell() string
		User() string
	}); ok {
		return identity.Shell(), identity.User()
	}
	if spec.Target == terminalTargetContainer {
		return spec.Shell, displayContainerUser("")
	}
	return spec.Shell, serviceUser()
}

// terminalStartError maps a container exec failure onto a bounded gRPC status.
// The prefixes are stable so the control plane can report an actionable reason
// without echoing Docker's raw message.
func terminalStartError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, ErrDockerNotFound):
		return status.Error(codes.NotFound, "terminal: the container no longer exists")
	case errors.Is(err, ErrDockerContainerNotRunning):
		return status.Error(codes.FailedPrecondition, "terminal: the container is not running")
	case errors.Is(err, ErrDockerNoShell):
		return status.Error(codes.FailedPrecondition, "terminal: the container has no supported shell")
	case errors.Is(err, ErrDockerUnavailable):
		return status.Error(codes.Unavailable, "terminal: the docker daemon is unavailable")
	default:
		return status.Error(codes.Internal, "terminal: could not start the session")
	}
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
// kill if the child ignores SIGHUP. It is idempotent and never waits. A child
// that already exited is never signalled again: its pid could have been reused
// as another process group. The reaping goroutine guarantees no zombie remains
// even when no output pump ever ran (for example a failed opened event).
func (s *terminalSession) terminate() {
	s.termOnce.Do(func() {
		select {
		case <-s.done:
			_ = s.pty.Close()
		default:
			_ = killTerminalProcessGroup(s.pid, terminalHangup)
			_ = s.pty.Close()
			go func() {
				select {
				case <-s.done:
				case <-time.After(terminalKillGrace):
					_ = killTerminalProcessGroup(s.pid, terminalKill)
				}
			}()
		}
		go func() { _, _ = s.wait() }()
	})
}

// read reads the PTY's output.
func (s *terminalSession) read(p []byte) (int, error) { return s.pty.Read(p) }

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
