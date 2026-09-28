package agent

import (
	"bytes"
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

	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Node-side compose layout defaults. The control plane's internal/services
// package derives the project name and its internal/services.ProjectPrefix
// constant from the service id; the values are duplicated rather than imported
// because agent/ must not import internal/ (cross-package scope rule).
const (
	// defaultComposeRoot is the host directory the agent writes per-project
	// compose files under, one subdirectory per project. It lives under the
	// agent's state directory so the unprivileged gotham-agent user can write
	// it (systemd ProtectSystem makes most other paths read-only).
	defaultComposeRoot = "/var/lib/gotham-agent/compose"
	// defaultComposeBinary is the CLI the agent shells out to. The compose
	// plugin must be installed on the node (`docker compose`).
	defaultComposeBinary = "docker"
	// composeFileName is the per-project document name inside the project
	// directory. It is a fixed name: the control plane never supplies a path.
	composeFileName = "compose.yaml"
	// defaultComposeTimeout bounds one non-following compose command (up can
	// pull images, so the default is generous). A following log stream is
	// bounded by its client's context instead.
	defaultComposeTimeout = 10 * time.Minute
)

// Compose input bounds. The control plane is a trusted mTLS peer, but a
// compromised control plane must not be able to fill the node's disk, escape
// the compose root or make the agent allocate without bound.
const (
	// maxComposeYAML caps one project's compose document.
	maxComposeYAML = 1 << 20 // 1 MiB
	// maxComposeError caps how much compose CLI output one error message
	// carries. Compose errors are line-oriented and the control plane only
	// needs the first few lines; the bound stops a runaway node from
	// producing an unbounded gRPC status.
	maxComposeError = 4 << 10 // 4 KiB
	// maxComposeLogChunk caps one streamed log chunk.
	maxComposeLogChunk = 32 << 10 // 32 KiB
	// maxComposeServices caps a validated document's service count, so a
	// hostile document cannot turn validation into unbounded output.
	maxComposeServices = 256
)

// composeProjectPattern is the strict project-name pattern every request must
// match: "gotham-" followed by a canonical lowercase UUID. It is what makes
// the per-project directory safe — a name outside this pattern can never be an
// absolute path, contain "..", or address another agent directory.
var composeProjectPattern = regexp.MustCompile(`^gotham-[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// composeServicePattern is the compose service-name pattern (the CLI's own
// restricted identifier alphabet).
var composeServicePattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,62}$`)

// ComposeServerConfig wires a ComposeServer. Every field falls back to the
// node default when empty, so production passes the agent config and tests
// override Root and Binary.
type ComposeServerConfig struct {
	// Root is the directory the per-project compose directories live under.
	Root string
	// DockerHost is the Docker endpoint (`GOTHAM_AGENT_DOCKER_SOCK`, or a
	// DOCKER_HOST value) the compose CLI must talk to. Empty leaves the CLI's
	// own default. A bare unix path is normalized to a unix:// URL.
	DockerHost string
	// Binary overrides the CLI binary (tests point it at a fake).
	Binary string
	// Timeout bounds one non-following command; default 10 minutes.
	Timeout time.Duration
	// Logger defaults to slog.Default.
	Logger *slog.Logger
}

// ComposeServer implements agentv1.ComposeServiceServer: it owns the node's
// per-project compose directories and shells out to the docker compose CLI, so
// compose behavior (interpolation, networks, dependencies, volumes) is exactly
// the CLI's. Nothing about the runtime is re-implemented here.
//
// Trust boundary: the compose document is user-supplied by design, but a
// compromised control plane still cannot escape or exhaust the node through
// this surface. The project name must match composeProjectPattern (a canonical
// gotham-<uuid>, so it can never be a path), the document is capped at
// maxComposeYAML, written atomically with mode 0600 inside a no-follow
// traversal of the compose root, and commands are run with the agent's own
// Docker endpoint. Failures return bounded CLI output: it may quote the
// document the user supplied, and it is the control plane's job to redact the
// environment values it substituted before the message is stored or returned;
// the agent never logs command output itself.
type ComposeServer struct {
	agentv1.UnimplementedComposeServiceServer

	root       string
	dockerHost string
	binary     string
	timeout    time.Duration
	log        *slog.Logger
}

// Compile-time guarantee that ComposeServer satisfies the agent service.
var _ agentv1.ComposeServiceServer = (*ComposeServer)(nil)

// NewComposeServer returns a ComposeServer rooted at the configured directory.
func NewComposeServer(cfg ComposeServerConfig) *ComposeServer {
	root := strings.TrimSpace(cfg.Root)
	if root == "" {
		root = defaultComposeRoot
	}
	if absolute, err := filepath.Abs(root); err == nil {
		root = absolute
	}
	binary := strings.TrimSpace(cfg.Binary)
	if binary == "" {
		binary = defaultComposeBinary
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
		dockerHost: normalizeDockerHost(cfg.DockerHost),
		binary:     binary,
		timeout:    timeout,
		log:        logger,
	}
}

// ComposeValidate writes the project's compose file and validates it with
// `docker compose config`, starting nothing. It returns the service names in
// the CLI's dependency order and the declared named volumes, both read back
// from the CLI's own rendering rather than from a second YAML parser.
func (s *ComposeServer) ComposeValidate(ctx context.Context, req *agentv1.ComposeValidateRequest) (*agentv1.ComposeValidateResponse, error) {
	project, err := s.prepare(ctx, req.GetProjectName(), req.GetComposeYaml())
	if err != nil {
		return nil, err
	}
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
	return &agentv1.ComposeValidateResponse{Services: services, Volumes: volumes}, nil
}

// ComposeUp writes the compose file and starts the project. A restart request
// runs `docker compose restart` instead of a create/recreate pass, restarting
// only the project's existing containers.
func (s *ComposeServer) ComposeUp(ctx context.Context, req *agentv1.ComposeUpRequest) (*agentv1.ComposeUpResponse, error) {
	project, err := s.prepare(ctx, req.GetProjectName(), req.GetComposeYaml())
	if err != nil {
		return nil, err
	}
	// Validate before touching the running project so a malformed document
	// cannot bring a healthy project down.
	if _, err := s.configList(ctx, project, "--services"); err != nil {
		return nil, composeError("up", err)
	}
	var args []string
	if req.GetRestart() {
		args = []string{"restart"}
	} else {
		// --remove-orphans drops containers of services the document no
		// longer declares; it only ever touches this project's containers.
		// Named volumes are never removed by an up.
		args = []string{"up", "-d", "--remove-orphans"}
	}
	if _, err := s.run(ctx, project, args...); err != nil {
		return nil, composeError("up", err)
	}
	s.log.Info("compose: project up", "project", project, "restart", req.GetRestart())
	return &agentv1.ComposeUpResponse{}, nil
}

// ComposeDown stops and removes the project's containers and networks. It
// never passes --volumes: named volumes survive a down, so a stop is always
// recoverable and data safety does not depend on the caller.
func (s *ComposeServer) ComposeDown(ctx context.Context, req *agentv1.ComposeDownRequest) (*agentv1.ComposeDownResponse, error) {
	project, err := s.prepare(ctx, req.GetProjectName(), req.GetComposeYaml())
	if err != nil {
		return nil, err
	}
	if _, err := s.run(ctx, project, "down", "--remove-orphans"); err != nil {
		return nil, composeError("down", err)
	}
	s.log.Info("compose: project down; named volumes retained", "project", project)
	return &agentv1.ComposeDownResponse{}, nil
}

// ComposeLogs streams the merged stdout/stderr of one compose service (or of
// the whole project when service is empty) until the client cancels or the
// CLI exits.
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

	// A following stream is bounded by its client's context, not by the
	// command timeout: a log tail may legitimately live for hours.
	timeout := s.timeout
	if req.GetFollow() {
		timeout = 0
	}
	command, cancel := s.command(ctx, project, timeout, args...)
	defer cancel()
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
	writer := &composeStreamWriter{send: stream.Send}
	// Both pipes write through one mutex-guarded sender: gRPC streams do not
	// allow concurrent Send calls.
	var copies sync.WaitGroup
	copies.Add(2)
	copyPipe := func(pipe io.Reader) {
		defer copies.Done()
		_, _ = io.Copy(writer, pipe)
	}
	go copyPipe(stdout)
	go copyPipe(stderr)
	waitErr := command.Wait()
	copies.Wait()
	if writer.failed() != nil {
		// The client went away or the stream broke; the context cancel above
		// already stops the CLI.
		return nil
	}
	if waitErr != nil {
		// A follow stream ends when the client cancels; that is not a failure.
		if ctx.Err() != nil {
			return nil
		}
		return composeError("logs", waitErr)
	}
	return nil
}

// ComposePs lists the project's containers with their compose service name,
// state and health as the CLI reports them.
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

// prepare validates a project name, checks the document size and writes the
// compose file atomically inside the project directory. Every command that
// needs the document calls it, so the node always runs the file the control
// plane last sent. The file is mode 0600: a rendered compose document carries
// the service's environment values.
func (s *ComposeServer) prepare(ctx context.Context, projectName string, composeYAML []byte) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", status.FromContextError(err).Err()
	}
	project, err := validateComposeProject(projectName)
	if err != nil {
		return "", err
	}
	if len(composeYAML) == 0 {
		return "", status.Error(codes.InvalidArgument, "compose document is required")
	}
	if len(composeYAML) > maxComposeYAML {
		return "", status.Errorf(codes.InvalidArgument, "compose document exceeds %d bytes", maxComposeYAML)
	}
	dir, err := openTrustedDir(filepath.Join(s.root, project), true)
	if err != nil {
		return "", status.Errorf(codes.Internal, "compose directory: %v", err)
	}
	defer func() { _ = dir.Close() }()
	if err := writeFileInDir(dir, composeFileName, composeYAML, 0o600); err != nil {
		return "", status.Errorf(codes.Internal, "write compose file: %v", err)
	}
	return project, nil
}

// requireProject reports a clear error when the project was never written (a
// log or ps request for a service that was never deployed).
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

// configList renders one `docker compose config <flag>` listing.
func (s *ComposeServer) configList(ctx context.Context, project string, flag string) ([]string, error) {
	output, err := s.run(ctx, project, "config", flag)
	if err != nil {
		return nil, err
	}
	return nonEmptyLines(output), nil
}

// run executes one compose subcommand in the project's directory and returns
// its stdout, bounded by the configured timeout. stderr is captured separately
// so a CLI warning (compose prints them on stderr) can never corrupt a
// parsed output such as `ps --format json`; on failure both streams are
// bounded into the error message.
func (s *ComposeServer) run(ctx context.Context, project string, args ...string) ([]byte, error) {
	command, cancel := s.command(ctx, project, s.timeout, args...)
	defer cancel()
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
		// The caller's context is the authority on cancellation: a killed CLI
		// reports an exit signal, not the context error.
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		output := append(append([]byte{}, stderr.Bytes()...), stdout.Bytes()...)
		return nil, &composeCommandError{err: err, output: output}
	}
	return stdout.Bytes(), nil
}

// command builds one compose invocation: the CLI, the project's compose file,
// the fixed project name, the caller's arguments, the agent's Docker endpoint
// and a bounded lifetime. A zero timeout leaves the command bounded by ctx
// alone.
func (s *ComposeServer) command(ctx context.Context, project string, timeout time.Duration, args ...string) (*exec.Cmd, context.CancelFunc) {
	if timeout > 0 {
		ctx, cancel := context.WithTimeout(ctx, timeout)
		return s.newCommand(ctx, project, args...), cancel
	}
	return s.newCommand(ctx, project, args...), func() {}
}

// newCommand builds the exec.Cmd for one compose invocation.
func (s *ComposeServer) newCommand(ctx context.Context, project string, args ...string) *exec.Cmd {
	full := append([]string{"compose", "-f", s.composeFile(project), "-p", project}, args...)
	command := exec.CommandContext(ctx, s.binary, full...)
	command.Dir = filepath.Join(s.root, project)
	if s.dockerHost != "" {
		command.Env = append(os.Environ(), "DOCKER_HOST="+s.dockerHost)
	}
	// WaitDelay makes a cancelled context kill the CLI and close its pipes,
	// so a follow stream never leaks a process when the client disappears.
	command.WaitDelay = 5 * time.Second
	return command
}

// composeFile is the absolute path of one project's compose document. Project
// names are validated before every call, so the join can never escape Root.
func (s *ComposeServer) composeFile(project string) string {
	return filepath.Join(s.root, project, composeFileName)
}

// validateComposeProject checks a project name against the strict pattern.
func validateComposeProject(name string) (string, error) {
	if name == "" {
		return "", status.Error(codes.InvalidArgument, "project_name is required")
	}
	if !composeProjectPattern.MatchString(name) {
		return "", status.Error(codes.InvalidArgument, "project_name must match gotham-<uuid>")
	}
	return name, nil
}

// composeCommandError carries a failed CLI invocation and its bounded output.
type composeCommandError struct {
	err    error
	output []byte
}

// Error renders the bounded CLI output, never the raw command line.
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

// Unwrap keeps errors.Is/As working for context cancellation.
func (e *composeCommandError) Unwrap() error { return e.err }

// composeError maps a failed CLI invocation onto a gRPC status. Invalid input
// is reported as InvalidArgument; context errors keep their code; everything
// else is Internal. The message never contains the agent's environment or the
// command line, only bounded CLI output.
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

// composeStreamWriter forwards log output to the gRPC send function in bounded
// chunks. It is safe for the stdout and stderr copy goroutines to share it: a
// mutex serializes Send calls, which is what a gRPC server stream requires.
type composeStreamWriter struct {
	mu   sync.Mutex
	send func(*agentv1.ComposeLogChunk) error
	err  error
}

// Write forwards p in chunks of at most maxComposeLogChunk.
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
			return written, err
		}
		written += size
		p = p[size:]
	}
	return written, nil
}

// failed returns the first send error, if any.
func (w *composeStreamWriter) failed() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.err
}

// composePsContainer is the subset of `docker compose ps --format json` the
// agent decodes. The CLI reports one JSON object per line; both the singular
// and plural name fields are decoded because the CLI has used both.
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

// parseComposePs parses the CLI's container list. Compose versions have
// emitted a JSON array or JSON lines; an empty project prints nothing.
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

// splitComposePorts splits the CLI's comma-separated port rendering.
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

// nonEmptyLines splits CLI line output, trimming blanks.
func nonEmptyLines(output []byte) []string {
	lines := []string{}
	for _, line := range strings.Split(string(output), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}

// normalizeDockerHost makes a bare unix socket path a valid DOCKER_HOST URL;
// other values pass through unchanged.
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
