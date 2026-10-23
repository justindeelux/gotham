package terminal

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
	"golang.org/x/sys/unix"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/justindeelux/gotham/agent/docker"
	"github.com/justindeelux/gotham/agent/fsroot"
	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
)

const (
	terminalTargetHost      = "host"
	terminalTargetContainer = "container"

	terminalDefaultMaxSessions   = 8
	terminalDefaultMaxInputBytes = 64 << 10
	terminalDefaultMaxDuration   = 8 * time.Hour
	terminalOutputChunk          = 32 << 10
	terminalWriteTimeout         = 5 * time.Second
	terminalKillGrace            = 2 * time.Second
	terminalGraceProbe           = terminalKillGrace / 4
	terminalScanRounds           = 4
	terminalScanInterval         = 100 * time.Millisecond
	terminalSessionBudget        = terminalKillGrace + 2*time.Second
	ShutdownWait                 = 3 * time.Second
	terminalStopWait             = terminalKillGrace + 2*time.Second

	terminalMinDimension = 1
	terminalMaxDimension = 1000

	terminalMaxRootBytes = 128
	terminalMaxCwdBytes  = 4096
)

type TerminalRoots interface {
	ResolveShellDir(rootName, rel string) (string, error)
}

type ContainerExec interface {
	Exec(ctx context.Context, containerID string, opts docker.ExecOptions) (docker.ExecSession, error)
}

type TerminalConfig struct {
	Logger              *slog.Logger
	Roots               TerminalRoots
	Shell               string
	IntegrationDisabled bool
	Containers          ContainerExec
	MaxSessions         int
	MaxInputBytes       int
	MaxDuration         time.Duration
}

type TerminalServer struct {
	agentv1.UnimplementedTerminalServiceServer

	log           *slog.Logger
	roots         TerminalRoots
	shell         string
	integration   bool
	maxInputBytes int
	maxDuration   time.Duration
	sessions      chan struct{}
	containers    ContainerExec

	walks sync.WaitGroup

	start func(terminalStart) (terminalProcess, error)
}

type terminalStart struct {
	Target      string
	Shell       string
	Dir         string
	ContainerID string
	Cols        uint16
	Rows        uint16
}

type terminalProcess interface {
	read(p []byte) (int, error)
	write(data []byte) error
	resize(cols, rows uint16) error
	terminate()
	wait() (int32, string)
}

func NewTerminalServer(cfg TerminalConfig) *TerminalServer {
	sweepStaleTerminalRcDirs(time.Now())
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
		integration:   !cfg.IntegrationDisabled,
		containers:    cfg.Containers,
		maxInputBytes: maxInput,
		maxDuration:   maxDuration,
		sessions:      make(chan struct{}, maxSessions),
	}
	server.start = server.startSession
	return server
}

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

type terminalRecv struct {
	req *agentv1.TerminalRequest
	err error
}

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
	shell, err := resolveTerminalShell(s.shell, s.integration)
	if err != nil {
		s.log.Error("terminal: no usable shell", "configured", s.shell, "error", err)
		return terminalStart{}, status.Error(codes.FailedPrecondition, "terminal: no usable shell is installed on the node")
	}
	return terminalStart{Target: terminalTargetHost, Shell: shell, Dir: dir, Cols: cols, Rows: rows}, nil
}

func defaultTerminalDir() string {
	return resolveDefaultTerminalDir(serviceHomeDir())
}

func serviceHomeDir() string {
	current, err := user.Current()
	if err != nil {
		return ""
	}
	return current.HomeDir
}

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
	if err := unix.Access(resolved, unix.X_OK); err != nil {
		return "/"
	}
	return resolved
}

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
	if shell != "" && !docker.ValidContainerShell(shell) {
		return terminalStart{}, status.Error(codes.InvalidArgument, "terminal: invalid container shell path")
	}
	return terminalStart{Target: terminalTargetContainer, ContainerID: containerID, Shell: shell, Cols: cols, Rows: rows}, nil
}

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

func terminalDimensions(cols, rows uint32) (uint16, uint16, error) {
	if cols < terminalMinDimension || cols > terminalMaxDimension ||
		rows < terminalMinDimension || rows > terminalMaxDimension {
		return 0, 0, status.Errorf(codes.InvalidArgument,
			"terminal: dimensions must be between %d and %d columns/rows", terminalMinDimension, terminalMaxDimension)
	}
	return uint16(cols), uint16(rows), nil
}

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

func (s *TerminalServer) startSession(spec terminalStart) (terminalProcess, error) {
	setup, err := terminalShellSetupFor(spec.Shell, s.integration)
	if err != nil {
		if s.log != nil {
			s.log.Warn("terminal: shell integration unavailable", "error", err)
		}
		setup = terminalShellSetup{}
	}
	cmd := exec.Command(spec.Shell, setup.args...)
	cmd.Dir = spec.Dir
	cmd.Env = append(terminalEnv(), setup.env...)
	master, err := pty.StartWithSize(cmd, &pty.Winsize{Cols: spec.Cols, Rows: spec.Rows})
	if err != nil {
		if setup.cleanup != nil {
			setup.cleanup()
		}
		return nil, fmt.Errorf("start %s: %w", spec.Shell, err)
	}
	if err := terminalNonblock(master); err != nil {
		_ = killTerminalProcessGroup(cmd.Process.Pid, terminalKill)
		_ = master.Close()
		go func() { _, _ = cmd.Process.Wait() }()
		if setup.cleanup != nil {
			setup.cleanup()
		}
		return nil, fmt.Errorf("start %s: %w", spec.Shell, err)
	}
	return newTerminalSession(cmd, master, s.log, &s.walks, setup.cleanup), nil
}

func (s *TerminalServer) startContainer(ctx context.Context, spec terminalStart) (terminalProcess, error) {
	if s.containers == nil {
		return nil, errors.New("container exec is not configured")
	}
	opts := docker.ExecOptions{Cols: spec.Cols, Rows: spec.Rows, Integration: s.integration}
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

type containerTerminalSession struct {
	stream      docker.ExecSession
	ctx         context.Context
	log         *slog.Logger
	containerID string

	termOnce sync.Once
	waitOnce sync.Once
	code     int32
	signal   string
	done     chan struct{}
}

func (s *containerTerminalSession) read(p []byte) (int, error) { return s.stream.Read(p) }

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

func (s *containerTerminalSession) resize(cols, rows uint16) error {
	return s.stream.Resize(s.ctx, cols, rows)
}

func (s *containerTerminalSession) terminate() {
	s.termOnce.Do(func() {
		if err := s.stream.Close(); err != nil && s.log != nil {
			s.log.Error("terminal: container exec cleanup incomplete",
				"container_id", s.containerID, "error", err)
		}
	})
}

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

func (s *containerTerminalSession) Shell() string { return s.stream.Shell() }

func (s *containerTerminalSession) User() string { return s.stream.User() }

func terminalSessionIdentity(session terminalProcess, spec terminalStart) (string, string) {
	if identity, ok := session.(interface {
		Shell() string
		User() string
	}); ok {
		return identity.Shell(), identity.User()
	}
	if spec.Target == terminalTargetContainer {
		return spec.Shell, docker.DisplayContainerUser("")
	}
	return spec.Shell, serviceUser()
}

func terminalStartError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, docker.ErrDockerNotFound):
		return status.Error(codes.NotFound, "terminal: the container no longer exists")
	case errors.Is(err, docker.ErrDockerContainerNotRunning):
		return status.Error(codes.FailedPrecondition, "terminal: the container is not running")
	case errors.Is(err, docker.ErrDockerNoShell):
		return status.Error(codes.FailedPrecondition, "terminal: the container has no supported shell")
	case errors.Is(err, docker.ErrDockerUnavailable):
		return status.Error(codes.Unavailable, "terminal: the docker daemon is unavailable")
	default:
		return status.Error(codes.Internal, "terminal: could not start the session")
	}
}

func resolveTerminalShell(configured string, integration bool) (string, error) {
	return resolveTerminalShellWith(configured, integration, exec.LookPath, os.Stat)
}

func resolveTerminalShellWith(
	configured string,
	integration bool,
	lookPath func(string) (string, error),
	stat func(string) (os.FileInfo, error),
) (string, error) {
	if strings.TrimSpace(configured) != "" {
		candidates := []string{configured}
		if shell, ok := firstUsableShell(candidates, stat); ok {
			return shell, nil
		}
		return "", errors.New("configured shell is not an absolute executable file")
	}
	var candidates []string
	if integration {
		if shell := strings.TrimSpace(os.Getenv("SHELL")); bashShell(shell) || zshShell(shell) {
			candidates = append(candidates, shell)
		}
		candidates = append(candidates, "/bin/bash", "/usr/bin/bash")
		if path, err := lookPath("bash"); err == nil {
			candidates = append(candidates, path)
		}
		candidates = append(candidates, "/bin/zsh", "/usr/bin/zsh")
		if path, err := lookPath("zsh"); err == nil {
			candidates = append(candidates, path)
		}
	}
	candidates = append(candidates, os.Getenv("SHELL"), "/bin/sh")
	if shell, ok := firstUsableShell(candidates, stat); ok {
		return shell, nil
	}
	return "", errors.New("no absolute executable shell found")
}

func firstUsableShell(candidates []string, stat func(string) (os.FileInfo, error)) (string, bool) {
	for _, candidate := range candidates {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" || !filepath.IsAbs(candidate) {
			continue
		}
		info, err := stat(candidate)
		if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 {
			continue
		}
		return candidate, true
	}
	return "", false
}

var terminalSafeEnv = map[string]bool{
	"PATH": true, "HOME": true, "USER": true, "LOGNAME": true, "SHELL": true,
	"LANG": true, "LANGUAGE": true, "TZ": true, "TMPDIR": true, "PWD": true,
	"XDG_RUNTIME_DIR": true, "TERM_PROGRAM": true,
}

func terminalEnv() []string {
	env := make([]string, 0, 20)
	for _, entry := range os.Environ() {
		name, _, ok := strings.Cut(entry, "=")
		if !ok {
			continue
		}
		if terminalSafeEnv[name] || strings.HasPrefix(name, "LC_") {
			env = append(env, entry)
		}
	}
	if home := serviceHomeDir(); home != "" {
		env = docker.TerminalEnvDefault(env, "HOME", home)
	}
	if name := currentUsername(); name != "" {
		env = docker.TerminalEnvDefault(env, "USER", name)
		env = docker.TerminalEnvDefault(env, "LOGNAME", name)
	}
	return docker.TerminalEnvDescription(env)
}

func serviceUser() string {
	if name := strings.TrimSpace(os.Getenv("USER")); name != "" {
		return name
	}
	return currentUsername()
}

type terminalSession struct {
	cmd   *exec.Cmd
	pty   *os.File
	pid   int
	sid   int
	log   *slog.Logger
	walks *sync.WaitGroup
	done  chan struct{}

	cleanup     func()
	cleanupOnce sync.Once

	leaderStart uint64

	termOnce sync.Once
	waitOnce sync.Once
	state    *os.ProcessState
	waitErr  error
}

func newTerminalSession(cmd *exec.Cmd, master *os.File, log *slog.Logger, walks *sync.WaitGroup, cleanup func()) *terminalSession {
	pid := cmd.Process.Pid
	return &terminalSession{
		cmd:         cmd,
		pty:         master,
		pid:         pid,
		sid:         pid,
		log:         log,
		walks:       walks,
		cleanup:     cleanup,
		leaderStart: terminalProcessStartTime(pid),
		done:        make(chan struct{}),
	}
}

func (s *terminalSession) removeCleanup() {
	if s.cleanup == nil {
		return
	}
	s.cleanupOnce.Do(s.cleanup)
}

func (s *TerminalServer) WaitTerminations(timeout time.Duration) bool {
	done := make(chan struct{})
	go func() {
		s.walks.Wait()
		close(done)
	}()
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-done:
		return true
	case <-timer.C:
		return false
	}
}

func (s *terminalSession) terminate() {
	s.termOnce.Do(func() {
		select {
		case <-s.done:
			_ = s.pty.Close()
		default:
			_ = killTerminalProcessGroup(s.pid, terminalHangup)
			_ = s.pty.Close()
		}
		go func() { _, _ = s.wait() }()
		s.startEndSession()
	})
}

func (s *terminalSession) startEndSession() {
	if s.walks == nil {
		go s.endSession()
		return
	}
	s.walks.Add(1)
	go func() {
		defer s.walks.Done()
		s.endSession()
	}()
}

func (s *terminalSession) endSession() {
	deadline := time.Now().Add(terminalSessionBudget)
	members, complete := terminalSessionMembers(s.sid, s.leaderStart, deadline)
	if len(members) > 0 {
		signalTerminalSession(members, terminalTerminate)
	}
	if !complete {
		s.warnIncompleteScan(len(members))
	}
	if complete && len(members) == 0 {
		return
	}
	s.killSessionAfterGrace(members, complete, deadline)
}

func (s *terminalSession) warnIncompleteScan(found int) {
	if s.log == nil {
		return
	}
	s.log.Warn("terminal: session scan incomplete; falling back to the process group",
		"session_id", s.sid, "members_found", found)
}

func (s *terminalSession) killSessionAfterGrace(kept []terminalSessionMember, complete bool, deadline time.Time) {
	defer func() { closeTerminalSessionMembers(kept) }()
	grace := time.Now().Add(terminalKillGrace)
	for complete {
		remaining := time.Until(grace)
		if remaining <= 0 {
			break
		}
		if remaining > terminalGraceProbe {
			remaining = terminalGraceProbe
		}
		time.Sleep(remaining)
		found, ok := terminalSessionMembers(s.sid, s.leaderStart, deadline)
		if !ok {
			kept = mergeTerminalSessionMembers(kept, found)
			s.warnIncompleteScan(len(found))
			complete = false
			break
		}
		closeTerminalSessionMembers(found)
		if len(found) == 0 && !anyTerminalSessionMemberAlive(kept) {
			return
		}
	}
	if remaining := time.Until(grace); remaining > 0 {
		time.Sleep(remaining)
	}
	signalTerminalSession(kept, terminalKill)
	closeTerminalSessionMembers(kept)
	kept = nil
	if !complete {
		s.hardKillGroupIfLive()
		return
	}
	for round := 0; round < terminalScanRounds; round++ {
		if time.Now().After(deadline) {
			return
		}
		found, ok := terminalSessionMembers(s.sid, s.leaderStart, deadline)
		if !ok {
			signalTerminalSession(found, terminalKill)
			closeTerminalSessionMembers(found)
			s.warnIncompleteScan(len(found))
			s.hardKillGroupIfLive()
			return
		}
		if len(found) == 0 {
			return
		}
		signalTerminalSession(found, terminalKill)
		closeTerminalSessionMembers(found)
		if round == terminalScanRounds-1 {
			return
		}
		time.Sleep(terminalScanInterval)
	}
}

func (s *terminalSession) hardKillGroupIfLive() {
	select {
	case <-s.done:
	default:
		_ = killTerminalProcessGroup(s.pid, terminalKill)
	}
}

func (s *terminalSession) read(p []byte) (int, error) { return s.pty.Read(p) }

func (s *terminalSession) write(data []byte) error {
	_ = s.pty.SetWriteDeadline(time.Now().Add(terminalWriteTimeout))
	_, err := s.pty.Write(data)
	return err
}

func (s *terminalSession) resize(cols, rows uint16) error {
	return pty.Setsize(s.pty, &pty.Winsize{Cols: cols, Rows: rows})
}

func (s *terminalSession) wait() (int32, string) {
	s.waitOnce.Do(func() {
		s.waitErr = s.cmd.Wait()
		s.state = s.cmd.ProcessState
		s.removeCleanup()
		close(s.done)
	})
	<-s.done
	return terminalProcessStatus(s.state)
}
