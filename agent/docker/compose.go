package docker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/justindeelux/gotham/agent/proxy"
	"github.com/justindeelux/gotham/composeguard"
	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"gopkg.in/yaml.v3"
)

const (
	DefaultComposeRoot    = "/var/lib/gotham-agent/compose"
	defaultComposeBinary  = "docker"
	composeFileName       = "compose.yaml"
	defaultComposeTimeout = 10 * time.Minute
)

const (
	maxComposeYAML     = 1 << 20
	maxComposeError    = 4 << 10
	maxComposeOutput   = 1 << 20
	maxComposeLogChunk = 32 << 10
	maxComposeServices = 256
)

var composeProjectPattern = regexp.MustCompile(`^gotham-[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

var composeServicePattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,62}$`)

var composeVolumePattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]*$`)

type ComposeServerConfig struct {
	Root       string
	DockerHost string
	Binary     string
	Timeout    time.Duration
	VolumeRoot string
	Logger     *slog.Logger
}

type ComposeServer struct {
	agentv1.UnimplementedComposeServiceServer

	root       string
	volumeRoot string
	dockerHost string
	binary     string
	timeout    time.Duration
	log        *slog.Logger
	locks      projectLocks
}

type projectLocks struct {
	mu      sync.Mutex
	entries map[string]*projectLock
}

type projectLock struct {
	mu   sync.Mutex
	refs int
}

func (l *projectLocks) acquire(project string) func() {
	l.mu.Lock()
	if l.entries == nil {
		l.entries = make(map[string]*projectLock)
	}
	entry, ok := l.entries[project]
	if !ok {
		entry = &projectLock{}
		l.entries[project] = entry
	}
	entry.refs++
	l.mu.Unlock()

	entry.mu.Lock()
	return func() {
		l.mu.Lock()
		entry.refs--
		if entry.refs == 0 {
			delete(l.entries, project)
		}
		l.mu.Unlock()
		entry.mu.Unlock()
	}
}

var _ agentv1.ComposeServiceServer = (*ComposeServer)(nil)

func NewComposeServer(cfg ComposeServerConfig) *ComposeServer {
	root := strings.TrimSpace(cfg.Root)
	if root == "" {
		root = DefaultComposeRoot
	}
	if absolute, err := filepath.Abs(root); err == nil {
		root = absolute
	}
	binary := strings.TrimSpace(cfg.Binary)
	if binary == "" {
		binary = defaultComposeBinary
	}
	volumeRoot := strings.TrimSpace(cfg.VolumeRoot)
	if volumeRoot == "" {
		volumeRoot = DefaultManagedVolumeRoot
	}
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = defaultComposeTimeout
	}
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &ComposeServer{
		root:       root,
		volumeRoot: volumeRoot,
		dockerHost: normalizeDockerHost(cfg.DockerHost),
		binary:     binary,
		timeout:    timeout,
		log:        logger,
	}
}

func (s *ComposeServer) ComposeValidate(ctx context.Context, req *agentv1.ComposeValidateRequest) (*agentv1.ComposeValidateResponse, error) {
	project, release, err := s.prepare(ctx, req.GetProjectName(), req.GetComposeYaml(), req.GetUnconfined())
	if err != nil {
		return nil, err
	}
	defer release()
	services, err := s.configList(ctx, project, "--services")
	if err != nil {
		return nil, composeError("validate", err)
	}
	if len(services) > maxComposeServices {
		return nil, status.Errorf(codes.InvalidArgument,
			"validate: document declares %d services (max %d)", len(services), maxComposeServices)
	}
	volumes, err := s.configList(ctx, project, "--volumes")
	if err != nil {
		return nil, composeError("validate", err)
	}
	return &agentv1.ComposeValidateResponse{
		Services:         services,
		Volumes:          volumes,
		ConfinedEnforced: !req.GetUnconfined(),
	}, nil
}

func (s *ComposeServer) ComposeUp(ctx context.Context, req *agentv1.ComposeUpRequest) (*agentv1.ComposeUpResponse, error) {
	project, release, err := s.prepare(ctx, req.GetProjectName(), req.GetComposeYaml(), req.GetUnconfined())
	if err != nil {
		return nil, err
	}
	defer release()
	if !req.GetUnconfined() {
		if err := s.ensureManagedVolumeDirs(project, req.GetComposeYaml()); err != nil {
			return nil, err
		}
	}
	if _, err := s.configList(ctx, project, "--services"); err != nil {
		return nil, composeError("up", err)
	}
	var args []string
	if req.GetRestart() {
		args = []string{"restart"}
	} else {
		args = []string{"up", "-d", "--remove-orphans"}
	}
	if _, err := s.run(ctx, project, args...); err != nil {
		return nil, composeError("up", err)
	}
	s.log.Info("compose: project up", "project", project, "restart", req.GetRestart())
	return &agentv1.ComposeUpResponse{}, nil
}

func (s *ComposeServer) ComposeDown(ctx context.Context, req *agentv1.ComposeDownRequest) (*agentv1.ComposeDownResponse, error) {
	project, release, err := s.prepare(ctx, req.GetProjectName(), req.GetComposeYaml(), req.GetUnconfined())
	if err != nil {
		return nil, err
	}
	defer release()
	if _, err := s.run(ctx, project, "down", "--remove-orphans"); err != nil {
		return nil, composeError("down", err)
	}
	s.log.Info("compose: project down; named volumes retained", "project", project)
	return &agentv1.ComposeDownResponse{}, nil
}

func (s *ComposeServer) ComposeLogs(req *agentv1.ComposeLogsRequest, stream grpc.ServerStreamingServer[agentv1.ComposeLogChunk]) error {
	project, err := validateComposeProject(req.GetProjectName())
	if err != nil {
		return err
	}
	service := strings.TrimSpace(req.GetService())
	if service != "" && !composeServicePattern.MatchString(service) {
		return status.Errorf(codes.InvalidArgument, "invalid compose service name %q", service)
	}
	if err := s.requireProject(project); err != nil {
		return err
	}
	ctx := stream.Context()
	if service != "" {
		declared, err := s.configList(ctx, project, "--services")
		if err != nil {
			return composeError("logs", err)
		}
		if !containsLine(declared, service) {
			return status.Errorf(codes.InvalidArgument, "no such compose service %q in this project", service)
		}
	}
	args := []string{"logs", "--no-color"}
	if req.GetTail() > 0 {
		args = append(args, "--tail", strconv.FormatInt(req.GetTail(), 10))
	}
	if req.GetFollow() {
		args = append(args, "--follow")
	}
	if service != "" {
		args = append(args, service)
	}

	timeout := s.timeout
	if req.GetFollow() {
		timeout = 0
	}
	commandCtx, cancelCommand := context.WithCancel(ctx)
	defer cancelCommand()
	if timeout > 0 {
		var cancelTimeout context.CancelFunc
		commandCtx, cancelTimeout = context.WithTimeout(commandCtx, timeout)
		defer cancelTimeout()
	}
	command := s.newCommand(commandCtx, project, args...)
	stdout, err := command.StdoutPipe()
	if err != nil {
		return status.Errorf(codes.Internal, "logs: %v", err)
	}
	stderr, err := command.StderrPipe()
	if err != nil {
		return status.Errorf(codes.Internal, "logs: %v", err)
	}
	if err := command.Start(); err != nil {
		return composeError("logs", err)
	}
	if err := stream.Send(&agentv1.ComposeLogChunk{Ready: true}); err != nil {
		cancelCommand()
		_ = stdout.Close()
		_ = stderr.Close()
		_ = command.Wait()
		return nil
	}
	writer := &composeStreamWriter{send: stream.Send, stop: func() {
		cancelCommand()
		_ = stdout.Close()
		_ = stderr.Close()
	}}
	drainDone := make(chan struct{})
	defer close(drainDone)
	go func() {
		select {
		case <-commandCtx.Done():
			_ = stdout.Close()
			_ = stderr.Close()
		case <-drainDone:
		}
	}()
	var (
		copies    sync.WaitGroup
		copyMutex sync.Mutex
		copyErr   error
	)
	copies.Add(2)
	copyPipe := func(pipe io.Reader) {
		defer copies.Done()
		if _, err := io.Copy(writer, pipe); err != nil {
			copyMutex.Lock()
			copyErr = err
			copyMutex.Unlock()
		}
	}
	go copyPipe(stdout)
	go copyPipe(stderr)
	copies.Wait()
	waitErr := command.Wait()
	copyMutex.Lock()
	drainErr := copyErr
	copyMutex.Unlock()

	if writer.failed() != nil {
		return nil
	}
	if ctx.Err() != nil {
		return nil
	}
	if waitErr != nil {
		return composeError("logs", waitErr)
	}
	if drainErr != nil {
		return status.Errorf(codes.Internal, "logs: read compose output: %v", drainErr)
	}
	return nil
}

func containsLine(lines []string, value string) bool {
	for _, line := range lines {
		if line == value {
			return true
		}
	}
	return false
}

func (s *ComposeServer) ComposePs(ctx context.Context, req *agentv1.ComposePsRequest) (*agentv1.ComposePsResponse, error) {
	project, err := validateComposeProject(req.GetProjectName())
	if err != nil {
		return nil, err
	}
	if err := s.requireProject(project); err != nil {
		return nil, err
	}
	output, err := s.run(ctx, project, "ps", "--format", "json")
	if err != nil {
		return nil, composeError("ps", err)
	}
	containers, err := parseComposePs(output)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "ps: %v", err)
	}
	return &agentv1.ComposePsResponse{Containers: containers}, nil
}

func (s *ComposeServer) prepare(ctx context.Context, projectName string, composeYAML []byte, unconfined bool) (string, func(), error) {
	project, composeYAML, err := s.validateComposeInput(ctx, projectName, composeYAML, unconfined)
	if err != nil {
		return "", nil, err
	}
	release := s.locks.acquire(project)
	if err := s.writeDocument(project, composeYAML); err != nil {
		release()
		return "", nil, err
	}
	return project, release, nil
}

func (s *ComposeServer) validateComposeInput(ctx context.Context, projectName string, composeYAML []byte, unconfined bool) (string, []byte, error) {
	if err := ctx.Err(); err != nil {
		return "", nil, status.FromContextError(err).Err()
	}
	project, err := validateComposeProject(projectName)
	if err != nil {
		return "", nil, err
	}
	if len(composeYAML) == 0 {
		return "", nil, status.Error(codes.InvalidArgument, "compose document is required")
	}
	if len(composeYAML) > maxComposeYAML {
		return "", nil, status.Errorf(codes.InvalidArgument, "compose document exceeds %d bytes", maxComposeYAML)
	}
	if err := composeguard.CheckSweepLabels(string(composeYAML)); err != nil {
		return "", nil, status.Errorf(codes.InvalidArgument, "compose: %v", err)
	}
	if !unconfined {
		appID := strings.TrimPrefix(project, "gotham-")
		document := string(composeYAML)
		if err := composeguard.Validate(document, composeguard.Options{
			AppID:       appID,
			ManagedRoot: s.volumeRoot,
		}); err != nil {
			return "", nil, status.Errorf(codes.InvalidArgument, "compose: confined document rejected: %v", err)
		}
		if err := composeguard.CheckNoInterpolation(document); err != nil {
			return "", nil, status.Errorf(codes.InvalidArgument, "compose: confined document rejected: %v", err)
		}
	}
	return project, composeYAML, nil
}

type composeVolumeDeclaration struct {
	DriverOpts struct {
		Device string `yaml:"device"`
	} `yaml:"driver_opts"`
}

func (s *ComposeServer) ensureManagedVolumeDirs(project string, document []byte) error {
	appID := strings.TrimPrefix(project, "gotham-")
	var doc struct {
		Volumes map[string]composeVolumeDeclaration `yaml:"volumes"`
	}
	if err := yaml.Unmarshal(document, &doc); err != nil {
		return status.Errorf(codes.InvalidArgument, "compose: volumes: %v", err)
	}
	base := filepath.Join(filepath.Clean(s.volumeRoot), appID)
	for name, declaration := range doc.Volumes {
		device := strings.TrimSpace(declaration.DriverOpts.Device)
		if device == "" {
			continue
		}
		if !composeVolumePattern.MatchString(name) {
			return status.Errorf(codes.InvalidArgument, "compose: volume name %q is not valid", name)
		}
		cleaned := filepath.Clean(device)
		want := filepath.Join(base, name)
		if cleaned != want {
			return status.Errorf(codes.InvalidArgument,
				"compose: volume %q device %q must be %s", name, device, want)
		}
		if err := rejectSymlinkComponents(base, cleaned); err != nil {
			return status.Errorf(codes.InvalidArgument, "compose: volume %q device %q: %v", name, device, err)
		}
		if err := os.MkdirAll(cleaned, 0o755); err != nil {
			return status.Errorf(codes.Internal, "compose: create volume %q device: %v", name, err)
		}
	}
	return nil
}

func (s *ComposeServer) writeDocument(project string, composeYAML []byte) error {
	dir, err := proxy.OpenTrustedDir(filepath.Join(s.root, project), true)
	if err != nil {
		return status.Errorf(codes.Internal, "compose directory: %v", err)
	}
	defer func() { _ = dir.Close() }()
	if err := proxy.WriteFileInDir(dir, composeFileName, composeYAML, 0o600); err != nil {
		return status.Errorf(codes.Internal, "write compose file: %v", err)
	}
	return nil
}

func (s *ComposeServer) requireProject(project string) error {
	_, err := os.Stat(s.composeFile(project))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return status.Error(codes.FailedPrecondition, "project has no compose file; deploy it first")
		}
		return status.Errorf(codes.Internal, "compose file: %v", err)
	}
	return nil
}

func (s *ComposeServer) configList(ctx context.Context, project string, flag string) ([]string, error) {
	output, err := s.run(ctx, project, "config", flag)
	if err != nil {
		return nil, err
	}
	return nonEmptyLines(output), nil
}

func (s *ComposeServer) run(ctx context.Context, project string, args ...string) ([]byte, error) {
	command, cancel := s.command(ctx, project, s.timeout, args...)
	defer cancel()
	verb := firstArg(args)
	parsed := verb == "config" || verb == "ps"
	stdoutLimit := maxComposeError
	if parsed {
		stdoutLimit = maxComposeOutput
	}
	stdout := &boundedBuffer{limit: stdoutLimit}
	stderr := &boundedBuffer{limit: maxComposeError}
	command.Stdout = stdout
	command.Stderr = stderr
	if err := command.Run(); err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		output := append(append([]byte{}, stderr.Bytes()...), stdout.Bytes()...)
		return nil, &composeCommandError{err: err, output: output}
	}
	if parsed && stdout.Overflowed() {
		return nil, status.Errorf(codes.Internal,
			"compose %s output exceeds %d bytes", verb, maxComposeOutput)
	}
	return stdout.Bytes(), nil
}

func firstArg(args []string) string {
	if len(args) == 0 {
		return "command"
	}
	return args[0]
}

type boundedBuffer struct {
	buf      []byte
	limit    int
	overflow bool
}

func (b *boundedBuffer) Write(p []byte) (int, error) {
	if len(b.buf) >= b.limit {
		b.overflow = true
		return len(p), nil
	}
	room := b.limit - len(b.buf)
	if len(p) > room {
		b.buf = append(b.buf, p[:room]...)
		b.overflow = true
		return len(p), nil
	}
	b.buf = append(b.buf, p...)
	return len(p), nil
}

func (b *boundedBuffer) Bytes() []byte { return b.buf }

func (b *boundedBuffer) Overflowed() bool { return b.overflow }

func (s *ComposeServer) command(ctx context.Context, project string, timeout time.Duration, args ...string) (*exec.Cmd, context.CancelFunc) {
	if timeout > 0 {
		ctx, cancel := context.WithTimeout(ctx, timeout)
		return s.newCommand(ctx, project, args...), cancel
	}
	return s.newCommand(ctx, project, args...), func() {}
}

func (s *ComposeServer) newCommand(ctx context.Context, project string, args ...string) *exec.Cmd {
	full := append([]string{"compose", "-f", s.composeFile(project), "-p", project}, args...)
	command := exec.CommandContext(ctx, s.binary, full...)
	command.Dir = filepath.Join(s.root, project)
	if s.dockerHost != "" {
		command.Env = append(os.Environ(), "DOCKER_HOST="+s.dockerHost)
	}
	command.WaitDelay = 5 * time.Second
	return command
}

func (s *ComposeServer) composeFile(project string) string {
	return filepath.Join(s.root, project, composeFileName)
}

func validateComposeProject(name string) (string, error) {
	if name == "" {
		return "", status.Error(codes.InvalidArgument, "project_name is required")
	}
	if !composeProjectPattern.MatchString(name) {
		return "", status.Error(codes.InvalidArgument, "project_name must match gotham-<uuid>")
	}
	return name, nil
}

type composeCommandError struct {
	err    error
	output []byte
}

func (e *composeCommandError) Error() string {
	message := strings.TrimSpace(string(e.output))
	if len(message) > maxComposeError {
		message = message[:maxComposeError] + "…"
	}
	if message == "" {
		return e.err.Error()
	}
	return e.err.Error() + ": " + message
}

func (e *composeCommandError) Unwrap() error { return e.err }

func composeError(action string, err error) error {
	switch {
	case errors.Is(err, context.Canceled):
		return status.Error(codes.Canceled, action+": context canceled")
	case errors.Is(err, context.DeadlineExceeded):
		return status.Error(codes.DeadlineExceeded, action+": deadline exceeded")
	default:
		return status.Errorf(codes.Internal, "%s: docker compose failed: %v", action, err)
	}
}

type composeStreamWriter struct {
	mu   sync.Mutex
	send func(*agentv1.ComposeLogChunk) error
	stop func()
	err  error
}

func (w *composeStreamWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.err != nil {
		return 0, w.err
	}
	written := 0
	for len(p) > 0 {
		size := len(p)
		if size > maxComposeLogChunk {
			size = maxComposeLogChunk
		}
		if err := w.send(&agentv1.ComposeLogChunk{Data: p[:size]}); err != nil {
			w.err = err
			if w.stop != nil {
				w.stop()
			}
			return written, err
		}
		written += size
		p = p[size:]
	}
	return written, nil
}

func (w *composeStreamWriter) failed() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.err
}

type composePsContainer struct {
	ID      string `json:"ID"`
	Name    string `json:"Name"`
	Names   string `json:"Names"`
	Service string `json:"Service"`
	Image   string `json:"Image"`
	State   string `json:"State"`
	Status  string `json:"Status"`
	Health  string `json:"Health"`
	Ports   string `json:"Ports"`
}

func parseComposePs(output []byte) ([]*agentv1.ComposeContainer, error) {
	trimmed := strings.TrimSpace(string(output))
	containers := []*agentv1.ComposeContainer{}
	if trimmed == "" {
		return containers, nil
	}
	entries := []composePsContainer{}
	if strings.HasPrefix(trimmed, "[") {
		if err := json.Unmarshal([]byte(trimmed), &entries); err != nil {
			return nil, fmt.Errorf("malformed ps output: %w", err)
		}
	} else {
		for _, line := range strings.Split(trimmed, "\n") {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			var entry composePsContainer
			if err := json.Unmarshal([]byte(line), &entry); err != nil {
				return nil, fmt.Errorf("malformed ps output: %w", err)
			}
			entries = append(entries, entry)
		}
	}
	for _, entry := range entries {
		name := entry.Name
		if name == "" {
			name = entry.Names
		}
		containers = append(containers, &agentv1.ComposeContainer{
			Service:     entry.Service,
			ContainerId: entry.ID,
			Name:        name,
			Image:       entry.Image,
			State:       entry.State,
			Status:      entry.Status,
			Health:      entry.Health,
			Ports:       splitComposePorts(entry.Ports),
		})
	}
	return containers, nil
}

func splitComposePorts(ports string) []string {
	ports = strings.TrimSpace(ports)
	if ports == "" {
		return nil
	}
	parts := strings.Split(ports, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		if part = strings.TrimSpace(part); part != "" {
			result = append(result, part)
		}
	}
	sort.Strings(result)
	return result
}

func nonEmptyLines(output []byte) []string {
	lines := []string{}
	for _, line := range strings.Split(string(output), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}

func normalizeDockerHost(host string) string {
	host = strings.TrimSpace(host)
	if host == "" {
		return ""
	}
	if strings.HasPrefix(host, "/") {
		return "unix://" + host
	}
	return host
}
