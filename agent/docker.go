package agent

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// maxLogFrame bounds the allocation for a single multiplexed log frame.
const maxLogFrame = 16 << 20

// runCleanupTimeout bounds the best-effort removal of a container whose start
// failed, so a broken daemon cannot hang the RunImage error path.
const runCleanupTimeout = 30 * time.Second

// ErrInvalidPortMapping marks a malformed port mapping. The DockerService
// error mapper turns it into InvalidArgument so every caller sees bad input
// as bad input instead of an internal failure.
var ErrInvalidPortMapping = errors.New("docker: invalid port mapping")

// ErrDockerNotFound marks a Docker API 404 for a missing container or network.
// The DockerService error mapper turns it into NotFound so the control plane
// can answer 404 instead of 500. Missing images and repositories are split out
// into ErrDockerImageNotFound.
var ErrDockerNotFound = errors.New("docker: not found")

// ErrDockerUnavailable marks a transport failure reaching the Docker daemon
// (socket or TCP connection refused, reset or otherwise down). The
// DockerService error mapper turns it into Unavailable so the control plane
// answers 502 instead of 500.
var ErrDockerUnavailable = errors.New("docker: daemon unavailable")

// ErrDockerImageNotFound marks a Docker API 404 for a missing image or
// repository (a create with an absent image, a pull of an unknown repo). It is
// distinct from ErrDockerNotFound so a bad image reference is reported as bad
// input, not as a missing container.
var ErrDockerImageNotFound = errors.New("docker: image not found")

// ErrDockerContainerNotRunning marks an exec request against a container that
// exists but is stopped, restarting or otherwise not running. The terminal
// service reports it as an actionable precondition instead of a stream failure.
var ErrDockerContainerNotRunning = errors.New("docker: container is not running")

// ErrDockerNoShell marks a container image that exposes none of the supported
// shells. A container terminal never starts a half-working exec: the caller
// gets an explicit "no supported shell" error instead.
var ErrDockerNoShell = errors.New("docker: container has no supported shell")

// DockerClient talks to the Docker Engine API over a unix socket or TCP
// endpoint. It implements the subset of the engine API the agent exposes to
// the control plane.
type DockerClient struct {
	http    *http.Client
	baseURL string
	// dockerHost is target normalised to a DOCKER_HOST value, exported to the
	// language toolchains (pack, railpack) the node runs.
	dockerHost string
	// registryStateDir holds the node-local registry credentials. Empty
	// disables authenticated registry bootstrap.
	registryStateDir string

	// mu guards registryAuth, cached by EnsureRegistry and reused by every
	// push/pull of a node-registry image.
	mu           sync.Mutex
	registryAuth registryAuth
	// registryMu serialises registry bootstrap so concurrent builds do not
	// race creating the network, the htpasswd file or the container.
	registryMu sync.Mutex
}

// DockerClientOption tunes a DockerClient.
type DockerClientOption func(*DockerClient)

// WithRegistryStateDir sets the directory that holds the node-local registry
// credential (the agent state directory). An empty value leaves the registry
// unauthenticated and EnsureRegistry fails closed.
func WithRegistryStateDir(dir string) DockerClientOption {
	return func(c *DockerClient) {
		c.registryStateDir = strings.TrimSpace(dir)
	}
}

// NewDockerClient builds a client for target, a Docker endpoint string as
// understood by newDockerTransport. An empty target uses the default socket.
func NewDockerClient(target string, options ...DockerClientOption) (*DockerClient, error) {
	transport, baseURL, err := newDockerTransport(target)
	if err != nil {
		return nil, err
	}
	client := &DockerClient{
		http:       &http.Client{Transport: transport},
		baseURL:    baseURL,
		dockerHost: dockerHostFromTarget(target),
	}
	for _, option := range options {
		option(client)
	}
	return client, nil
}

// dockerHostFromTarget normalises a Docker endpoint into a DOCKER_HOST value
// for child processes. The default socket and an empty target are left empty so
// the CLI uses its own default.
func dockerHostFromTarget(target string) string {
	target = strings.TrimSpace(target)
	switch {
	case target == "":
		return ""
	case strings.HasPrefix(target, "unix://"), strings.HasPrefix(target, "tcp://"),
		strings.HasPrefix(target, "http://"), strings.HasPrefix(target, "https://"):
		return target
	case strings.HasPrefix(target, "/"):
		return "unix://" + target
	default:
		return target
	}
}

// Version returns the Docker Engine version reported by GET /version.
func (c *DockerClient) Version(ctx context.Context) (string, error) {
	var out struct {
		Version string `json:"Version"`
	}
	if err := c.doJSON(ctx, http.MethodGet, "/version", nil, &out); err != nil {
		return "", err
	}
	return out.Version, nil
}

// ListContainers returns the containers known to the engine, optionally
// including stopped ones.
func (c *DockerClient) ListContainers(ctx context.Context, all bool) ([]*agentv1.ContainerInfo, error) {
	path := "/containers/json?all=0"
	if all {
		path = "/containers/json?all=1"
	}
	var summaries []dockerContainerSummary
	if err := c.doJSON(ctx, http.MethodGet, path, nil, &summaries); err != nil {
		return nil, err
	}
	containers := make([]*agentv1.ContainerInfo, 0, len(summaries))
	for _, summary := range summaries {
		info := &agentv1.ContainerInfo{
			Id:     summary.ID,
			Name:   containerName(summary.Names),
			Image:  summary.Image,
			Status: summary.Status,
			State:  summary.State,
			Labels: summary.Labels,
			Health: containerHealth(summary.Status),
		}
		for _, port := range summary.Ports {
			// Only published TCP bindings matter for routing; a binding
			// without a public port is not reachable from another container.
			if port.PublicPort <= 0 || (port.Type != "" && port.Type != "tcp") {
				continue
			}
			info.Ports = append(info.Ports, &agentv1.PortBinding{
				Ip:          port.IP,
				PrivatePort: port.PrivatePort,
				PublicPort:  port.PublicPort,
			})
		}
		for _, mount := range summary.Mounts {
			info.Mounts = append(info.Mounts, &agentv1.ContainerMount{
				Source:      mount.Source,
				Destination: mount.Destination,
				ReadOnly:    !mount.RW,
			})
		}
		// The restart policy is not part of the container summary; only the
		// managed proxy container needs it, so it is inspected on demand
		// (bounded to one container) and left empty when the engine cannot
		// answer.
		if summary.Labels["gotham.component"] == "proxy" {
			info.RestartPolicy = c.restartPolicy(ctx, summary.ID)
		}
		if summary.Created > 0 {
			info.CreatedAt = timestamppb.New(time.Unix(summary.Created, 0))
		}
		containers = append(containers, info)
	}
	return containers, nil
}

// restartPolicy reads a container's native Docker restart policy. It is used
// only for the managed proxy container; an engine error yields an empty policy
// (unknown) rather than failing the whole listing.
func (c *DockerClient) restartPolicy(ctx context.Context, id string) string {
	var out struct {
		HostConfig struct {
			RestartPolicy struct {
				Name string `json:"Name"`
			} `json:"RestartPolicy"`
		} `json:"HostConfig"`
	}
	if err := c.doJSON(ctx, http.MethodGet, "/containers/"+url.PathEscape(id)+"/json", nil, &out); err != nil {
		return ""
	}
	return out.HostConfig.RestartPolicy.Name
}

// Start starts the container with the given id.
func (c *DockerClient) Start(ctx context.Context, id string) error {
	return c.action(ctx, id, "start")
}

// Stop stops the container with the given id.
func (c *DockerClient) Stop(ctx context.Context, id string) error {
	return c.action(ctx, id, "stop")
}

// Restart restarts the container with the given id.
func (c *DockerClient) Restart(ctx context.Context, id string) error {
	return c.action(ctx, id, "restart")
}

// Remove deletes the container with the given id, forcing a stop when it is
// still running. A container that is already gone (404) is reported as
// success so callers can treat removal as idempotent. Named volumes are left
// in place: only anonymous volumes are cleaned up (v=true), which is what
// makes "delete the resource, keep the data" possible.
func (c *DockerClient) Remove(ctx context.Context, id string) error {
	if strings.TrimSpace(id) == "" {
		return errors.New("docker: container id is required")
	}
	path := "/containers/" + url.PathEscape(id) + "?force=true&v=true"
	response, err := c.doRaw(ctx, http.MethodDelete, path, nil, "")
	if err != nil {
		return err
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode == http.StatusNotFound {
		_, _ = io.Copy(io.Discard, response.Body)
		return nil
	}
	if !dockerOK(response.StatusCode) {
		return statusError(http.MethodDelete, path, response)
	}
	_, _ = io.Copy(io.Discard, response.Body)
	return nil
}

// RemoveVolume deletes one named volume, forcing removal when it is still
// referenced. A volume that is already gone (404) is reported as success so
// the expiry sweep is idempotent; every other engine error is returned.
func (c *DockerClient) RemoveVolume(ctx context.Context, name string) error {
	if strings.TrimSpace(name) == "" {
		return errors.New("docker: volume name is required")
	}
	path := "/volumes/" + url.PathEscape(name) + "?force=true"
	response, err := c.doRaw(ctx, http.MethodDelete, path, nil, "")
	if err != nil {
		return err
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode == http.StatusNotFound {
		_, _ = io.Copy(io.Discard, response.Body)
		return nil
	}
	if !dockerOK(response.StatusCode) {
		return statusError(http.MethodDelete, path, response)
	}
	_, _ = io.Copy(io.Discard, response.Body)
	return nil
}

// PullImage pulls image from its registry, consuming the progress stream. An
// error reported inside the progress payload is returned as an error.
// username/password carry one private-registry credential for this pull only:
// the Docker X-Registry-Auth header is built from them and the credential is
// never cached or persisted (empty means the node-local registry credential
// for node-owned images, anonymous otherwise). It answers the resolved image
// digest (sha256:...), "" when the engine reported none.
func (c *DockerClient) PullImage(ctx context.Context, image, username, password string) (string, error) {
	if strings.TrimSpace(image) == "" {
		return "", errors.New("docker: image is required")
	}
	query := url.Values{}
	query.Set("fromImage", image)

	authHeader, err := c.pullAuthHeader(ctx, image, username, password)
	if err != nil {
		return "", err
	}
	response, err := c.doRegistryRequest(ctx, http.MethodPost, "/images/create?"+query.Encode(), authHeader)
	if err != nil {
		return "", err
	}
	defer func() { _ = response.Body.Close() }()

	decoder := json.NewDecoder(response.Body)
	for {
		var message struct {
			Error string `json:"error"`
		}
		switch err := decoder.Decode(&message); {
		case errors.Is(err, io.EOF):
			return c.inspectImageDigest(ctx, image)
		case err != nil:
			return "", fmt.Errorf("docker: decode pull progress: %w", err)
		case message.Error != "":
			return "", fmt.Errorf("docker: pull %s: %s", image, message.Error)
		}
	}
}

// pullAuthHeader returns the X-Registry-Auth value for one image pull. An
// explicit per-pull credential wins and is scoped to the image's own registry
// host; otherwise the node-local registry credential (or anonymous) applies.
// The error text never carries the credential.
func (c *DockerClient) pullAuthHeader(ctx context.Context, image, username, password string) (string, error) {
	if strings.TrimSpace(username) != "" {
		auth := registryAuth{
			Address:  registryServerAddress(image),
			Username: username,
			Password: password,
		}
		return auth.header()
	}
	if err := c.ensureRegistryCredentialFor(ctx, image); err != nil {
		return "", err
	}
	return c.registryAuthHeader(image)
}

// registryServerAddress derives the Docker serveraddress for an image pull:
// the explicit registry host, or the Docker Hub default for bare names. It
// feeds the X-Registry-Auth config only; it is never used to route the pull.
func registryServerAddress(image string) string {
	ref := strings.TrimSpace(image)
	if host, _, ok := strings.Cut(ref, "/"); ok && (strings.Contains(host, ".") || strings.Contains(host, ":") || host == "localhost") {
		return host
	}
	return "https://index.docker.io/v1/"
}

// inspectImageDigest reads the manifest digest the engine recorded for image.
// Only a digest recorded under the pulled repository name (or its Docker Hub
// expansion for bare names) is answered: the first RepoDigests entry may
// belong to another repository sharing the image ID, and recording that would
// pin a later rollback to the wrong bits. "" means the engine reported no
// digest for this repository, never a failure of the pull itself.
func (c *DockerClient) inspectImageDigest(ctx context.Context, image string) (string, error) {
	var out struct {
		RepoDigests []string `json:"RepoDigests"`
	}
	path := "/images/" + url.PathEscape(image) + "/json"
	response, err := c.doRaw(ctx, http.MethodGet, path, nil, "")
	if err != nil {
		return "", err
	}
	defer func() { _ = response.Body.Close() }()
	if !dockerOK(response.StatusCode) {
		return "", statusError(http.MethodGet, path, response)
	}
	if err := json.NewDecoder(response.Body).Decode(&out); err != nil {
		return "", fmt.Errorf("docker: decode image inspect: %w", err)
	}
	for _, candidate := range digestRepoNames(image) {
		for _, entry := range out.RepoDigests {
			repo, digest, ok := strings.Cut(entry, "@")
			if !ok || !strings.HasPrefix(digest, "sha256:") {
				continue
			}
			if repo == candidate {
				return digest, nil
			}
		}
	}
	return "", nil
}

// digestRepoNames renders the repository names a pulled reference may appear
// under in RepoDigests: the name as pulled, the familiar spelling for
// explicit Hub references (docker.io/library/nginx is listed as nginx) and
// the Hub expansions for bare names (nginx pulls as docker.io/library/nginx).
// index.docker.io is the legacy Hub hostname the engine may normalize either
// way, so both spellings are candidates.
func digestRepoNames(image string) []string {
	name := strings.TrimSpace(image)
	if cut, _, ok := strings.Cut(name, "@"); ok {
		name = cut
	}
	if index := strings.LastIndex(name, ":"); index >= 0 && index > strings.LastIndex(name, "/") {
		name = name[:index]
	}
	names := []string{name}
	head, tail, found := strings.Cut(name, "/")
	switch {
	case !found:
		// Official image: nginx is docker.io/library/nginx on the Hub.
		names = append(names, "docker.io/library/"+name)
	case !isRegistryHostHead(head):
		// Bare namespaced path: team/app is docker.io/team/app on the Hub.
		names = append(names, "docker.io/"+name)
	default:
		// Explicit Hub spellings only: docker.io/library/nginx is listed
		// as nginx, and index.docker.io is the legacy Hub hostname the
		// engine may normalize either way. Any other registry keeps its
		// exact name: a familiar-looking entry for another repository must
		// never resolve a private pull.
		lower := strings.ToLower(head)
		if lower == "docker.io" || lower == "index.docker.io" {
			familiar := tail
			if stripped, ok := strings.CutPrefix(tail, "library/"); ok {
				familiar = stripped
			}
			names = append(names, familiar)
			if lower == "docker.io" {
				names = append(names, "index.docker.io/"+tail)
			} else {
				names = append(names, "docker.io/"+tail)
			}
		}
	}
	return names
}

// isRegistryHostHead reports whether the first path component of a reference
// names an explicit registry (a dot, a port or localhost), mirroring the
// control plane's split rule without importing it.
func isRegistryHostHead(head string) bool {
	return strings.Contains(head, ".") || strings.Contains(head, ":") || strings.EqualFold(head, "localhost")
}

// CreateContainer creates a container from req and returns its id.
func (c *DockerClient) CreateContainer(ctx context.Context, req *agentv1.CreateContainerRequest) (string, error) {
	if strings.TrimSpace(req.GetImage()) == "" {
		return "", errors.New("docker: image is required")
	}
	path := "/containers/create"
	if name := req.GetName(); name != "" {
		path += "?name=" + url.QueryEscape(name)
	}
	var out struct {
		ID string `json:"Id"`
	}
	body, err := buildCreateBody(req)
	if err != nil {
		return "", err
	}
	if err := c.doJSON(ctx, http.MethodPost, path, body, &out); err != nil {
		return "", err
	}
	if out.ID == "" {
		return "", errors.New("docker: create response missing Id")
	}
	return out.ID, nil
}

// RunImage creates the container described by req and starts it immediately.
// A start failure removes the container it just created: the id would otherwise
// be lost at the gRPC boundary, leaving an orphan that blocks a retry on the
// same name and that a later delete cannot find. Docker allows removing a
// created-but-not-started container.
func (c *DockerClient) RunImage(ctx context.Context, req *agentv1.CreateContainerRequest) (string, error) {
	id, err := c.CreateContainer(ctx, req)
	if err != nil {
		return "", err
	}
	if err := c.Start(ctx, id); err != nil {
		// The start context may already be canceled or past its deadline;
		// cleanup still has to run, so it gets its own bounded context.
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), runCleanupTimeout)
		defer cancel()
		if removeErr := c.Remove(cleanupCtx, id); removeErr != nil {
			return "", fmt.Errorf("%w: container %s left behind (cleanup failed: %v)", err, id, removeErr)
		}
		return "", err
	}
	return id, nil
}

// Logs streams the logs of the container with the given id. Each channel item
// is either one Docker multiplexed frame payload or a terminal decode error.
// The channel is closed when the stream ends or ctx is canceled.
func (c *DockerClient) Logs(ctx context.Context, id string, follow bool, tail int64) (<-chan LogMessage, error) {
	if strings.TrimSpace(id) == "" {
		return nil, errors.New("docker: container id is required")
	}
	query := url.Values{}
	query.Set("stdout", "1")
	query.Set("stderr", "1")
	query.Set("follow", boolParam(follow))
	if tail > 0 {
		query.Set("tail", strconv.FormatInt(tail, 10))
	} else {
		query.Set("tail", "all")
	}

	response, err := c.do(ctx, http.MethodGet, "/containers/"+url.PathEscape(id)+"/logs?"+query.Encode(), nil)
	if err != nil {
		return nil, err
	}

	out := make(chan LogMessage)
	go func() {
		defer close(out)
		defer func() { _ = response.Body.Close() }()
		decodeLogStream(ctx, response.Body, out)
	}()
	return out, nil
}

// action performs a POST to a container lifecycle endpoint.
func (c *DockerClient) action(ctx context.Context, id, verb string) error {
	if strings.TrimSpace(id) == "" {
		return errors.New("docker: container id is required")
	}
	response, err := c.do(ctx, http.MethodPost, "/containers/"+url.PathEscape(id)+"/"+verb, nil)
	if err != nil {
		return err
	}
	defer func() { _ = response.Body.Close() }()
	_, _ = io.Copy(io.Discard, response.Body)
	return nil
}

// execProbeTimeout bounds the non-interactive shell probe run in a container.
const execProbeTimeout = 10 * time.Second

// execResizeTimeout bounds one exec resize call.
const execResizeTimeout = 5 * time.Second

// execExitTimeout bounds how long ExitCode waits for the daemon to publish the
// process's final status after the attached stream ended.
const execExitTimeout = 2 * time.Second

// execExitPoll is the interval between exit-status reads.
const execExitPoll = 50 * time.Millisecond

// execProbeMaxBytes bounds the shell probe output kept in memory.
const execProbeMaxBytes = 4 << 10

// execKillTimeout bounds the fixed helper exec that ends a terminal's process
// tree. It is a safety net, not a retry budget.
const execKillTimeout = 8 * time.Second

// execStillRunningTimeout bounds how long Kill waits for Docker to report the
// exec as no longer Running after the helper signalled it.
const execStillRunningTimeout = 3 * time.Second

// execWrapperCommand records the process id that will become the interactive
// shell and then execs the requested command in place. The id is the only
// output consumed before the terminal is handed to the user; it is used by the
// fixed kill helper on close. No caller input is interpolated into it.
const execWrapperCommand = `printf '%s\n' "$$"; exec "$@"`

// execKillTreeScript ends a container exec's shell and every process below it.
// The root pid is passed as $1 (a validated decimal number) and the script is
// fixed; no caller input is interpolated. It snapshots the descendant tree
// before signalling, so foreground/background jobs are included even though an
// interactive shell puts them in their own process groups, then sends
// HUP/TERM/KILL with a short grace. It never signals pid 1.
const execKillTreeScript = `root=$1
case "$root" in ''|*[!0-9]*) exit 2;; esac
[ "$root" -gt 1 ] || exit 2
list=" $root "
frontier="$root"
while [ -n "$frontier" ]; do
  next=""
  for d in /proc/[0-9]*; do
    [ -r "$d/status" ] || continue
    pid=${d#/proc/}
    ppid=""
    while IFS=' ' read -r key value rest; do
      if [ "$key" = "PPid:" ]; then ppid=$value; break; fi
    done < "$d/status" 2>/dev/null
    [ -n "$ppid" ] || continue
    case " $frontier " in
      *" $ppid "*)
        case "$list" in
          *" $pid "*) ;;
          *) list="$list$pid "; next="$next$pid " ;;
        esac
        ;;
    esac
  done
  frontier=$next
done
for sig in HUP TERM KILL; do
  for pid in $list; do
    if [ "$pid" = "$root" ] && [ "$sig" != "KILL" ]; then
      kill -"$sig" "$pid" 2>/dev/null
      continue
    fi
    kill -"$sig" "$pid" 2>/dev/null
  done
  kill -"$sig" -"$root" 2>/dev/null
  [ "$sig" = "KILL" ] && break
  sleep 0.2
done
exit 0`

// shellProbeCommand asks the container's POSIX shell for the first supported
// interactive shell. It is a fixed command written by the agent; no caller
// input is interpolated into it.
const shellProbeCommand = `for s in bash sh ash; do p=$(command -v "$s" 2>/dev/null) && { echo "$p"; exit 0; }; done; exit 127`

// ExecOptions describes one interactive exec. Cmd empty selects the first
// supported shell inside the container. User and WorkingDir default to the
// container's configured values.
type ExecOptions struct {
	Cmd        []string
	Env        []string
	User       string
	WorkingDir string
	Cols       uint16
	Rows       uint16
}

// ExecSession is one attached interactive exec. Read/Write are the container
// process's stdio over the hijacked Docker connection. Close ends the exec
// process tree (not only the attachment) and releases the connection, without
// stopping the container. Resize changes the TTY window and ExitCode reports
// the process status once it has ended.
type ExecSession interface {
	io.Reader
	io.Writer
	io.Closer
	Resize(ctx context.Context, cols, rows uint16) error
	ExitCode(ctx context.Context) (int32, string, error)
	// Kill ends the exec process tree and verifies the exec is no longer
	// Running. It is idempotent; the application container is never stopped.
	Kill(ctx context.Context) error
	// Shell is the shell executable the agent chose inside the container.
	Shell() string
	// User is the container user the process runs as ("root" when unset).
	User() string
}

// containerInspect is the subset of GET /containers/{id}/json the exec path
// needs.
type containerInspect struct {
	State struct {
		Running bool   `json:"Running"`
		Status  string `json:"Status"`
	} `json:"State"`
	Config struct {
		User       string   `json:"User"`
		WorkingDir string   `json:"WorkingDir"`
		Env        []string `json:"Env"`
	} `json:"Config"`
}

// Exec starts an interactive exec in containerID and returns its attached
// stream. The container must exist and be running. When opts.Cmd is empty the
// agent probes the image for a supported shell and answers ErrDockerNoShell
// when none exists; it never starts a broken terminal. Closing the returned
// session ends the exec attachment only: the application container is never
// stopped or restarted.
func (c *DockerClient) Exec(ctx context.Context, containerID string, opts ExecOptions) (ExecSession, error) {
	if strings.TrimSpace(containerID) == "" {
		return nil, errors.New("docker: container id is required")
	}
	inspect, err := c.inspectContainer(ctx, containerID)
	if err != nil {
		return nil, err
	}
	if !inspect.State.Running {
		state := strings.TrimSpace(inspect.State.Status)
		if state == "" {
			state = "stopped"
		}
		return nil, fmt.Errorf("%w: %s", ErrDockerContainerNotRunning, state)
	}

	user := strings.TrimSpace(opts.User)
	if user == "" {
		user = strings.TrimSpace(inspect.Config.User)
	}
	workdir := strings.TrimSpace(opts.WorkingDir)
	if workdir == "" {
		workdir = strings.TrimSpace(inspect.Config.WorkingDir)
	}

	// The probe runs through /bin/sh and finds the interactive shell. It also
	// proves the fixed wrapper below has a POSIX shell to run.
	shellPath, err := c.probeShell(ctx, containerID, user, workdir, inspect.Config.Env)
	if err != nil {
		return nil, err
	}
	command := opts.Cmd
	if len(command) == 0 {
		command = []string{shellPath}
	}
	// Record the pid that becomes the shell (`exec` keeps it), then run the
	// requested command. The fixed helper terminates that whole tree on Close;
	// the daemon alone does not end an exec process when its attach stream goes
	// away.
	wrapped := append([]string{"/bin/sh", "-c", execWrapperCommand, "gotham-terminal"}, command...)

	env := append([]string(nil), opts.Env...)
	env = append(env, "TERM=xterm-256color")
	execID, err := c.createExec(ctx, containerID, wrapped, user, workdir, env, true)
	if err != nil {
		return nil, err
	}
	stream, err := c.startExec(ctx, execID, true)
	if err != nil {
		return nil, err
	}
	reader := bufio.NewReaderSize(stream, 32<<10)
	pid, err := readExecPID(reader, execProbeTimeout)
	if err != nil {
		_ = stream.Close()
		return nil, fmt.Errorf("docker: exec did not report its process id: %w", err)
	}
	session := &dockerExecSession{
		client:      c,
		id:          execID,
		containerID: containerID,
		pid:         pid,
		stream:      stream,
		reader:      reader,
		shell:       shellPath,
		user:        displayContainerUser(user),
		execUser:    user,
		workdir:     workdir,
		env:         env,
		ctx:         ctx,
	}
	if opts.Cols > 0 && opts.Rows > 0 {
		if err := session.Resize(ctx, opts.Cols, opts.Rows); err != nil {
			_ = session.Kill(context.Background())
			_ = session.Close()
			return nil, err
		}
	}
	return session, nil
}

// readExecPID consumes the wrapper's first output line: the decimal pid that
// becomes the interactive shell. Reading it before handing the session to the
// terminal keeps it out of the user's terminal.
func readExecPID(reader *bufio.Reader, timeout time.Duration) (int, error) {
	type result struct {
		pid int
		err error
	}
	done := make(chan result, 1)
	go func() {
		line, err := reader.ReadString('\n')
		if err != nil {
			done <- result{err: err}
			return
		}
		pid, err := strconv.Atoi(strings.TrimSpace(line))
		if err != nil || pid <= 1 {
			done <- result{err: fmt.Errorf("invalid pid %q", strings.TrimSpace(line))}
			return
		}
		done <- result{pid: pid}
	}()
	select {
	case value := <-done:
		return value.pid, value.err
	case <-time.After(timeout):
		return 0, errors.New("timed out waiting for the exec process id")
	}
}

// inspectContainer reads the container's run state and configured user.
func (c *DockerClient) inspectContainer(ctx context.Context, containerID string) (containerInspect, error) {
	var out containerInspect
	path := "/containers/" + url.PathEscape(containerID) + "/json"
	if err := c.doJSON(ctx, http.MethodGet, path, nil, &out); err != nil {
		return out, err
	}
	return out, nil
}

// probeShell runs a fixed non-interactive command inside the container to find
// a supported shell. Running it detached from the caller's stream keeps the
// interactive session free of probe output.
func (c *DockerClient) probeShell(ctx context.Context, containerID, user, workdir string, imageEnv []string) (string, error) {
	env := append(append([]string(nil), imageEnv...), "TERM=xterm-256color")
	execID, err := c.createExec(ctx, containerID, []string{"/bin/sh", "-c", shellProbeCommand}, user, workdir, env, true)
	if err != nil {
		// A container without /bin/sh cannot offer any supported shell. A
		// missing container or unreachable daemon keeps its own identity.
		if errors.Is(err, ErrDockerNotFound) || errors.Is(err, ErrDockerUnavailable) {
			return "", err
		}
		return "", fmt.Errorf("%w: %v", ErrDockerNoShell, err)
	}
	probeCtx, cancel := context.WithTimeout(ctx, execProbeTimeout)
	defer cancel()
	stream, err := c.startExec(probeCtx, execID, true)
	if err != nil {
		if errors.Is(err, ErrDockerNotFound) || errors.Is(err, ErrDockerUnavailable) {
			return "", err
		}
		return "", fmt.Errorf("%w: %v", ErrDockerNoShell, err)
	}
	type probeResult struct {
		output string
		err    error
	}
	readDone := make(chan probeResult, 1)
	go func() {
		output, readErr := readProbeOutput(stream, execProbeMaxBytes)
		readDone <- probeResult{output: output, err: readErr}
	}()
	var probe probeResult
	select {
	case probe = <-readDone:
	case <-probeCtx.Done():
		// A wedged probe must not pin the open path: close the stream so the
		// reader unblocks and report the image as unsupported.
		_ = stream.Close()
		probe = <-readDone
	}
	_ = stream.Close()
	code, _, exitErr := c.execExitCode(probeCtx, execID)
	if probe.err != nil || exitErr != nil || code != 0 {
		return "", fmt.Errorf("%w: the image exposes no /bin/sh", ErrDockerNoShell)
	}
	output := probe.output
	shell := firstLine(output)
	if !validContainerShell(shell) {
		return "", fmt.Errorf("%w: the image exposes no supported shell", ErrDockerNoShell)
	}
	return shell, nil
}

// readProbeOutput reads at most limit bytes from a probe stream.
func readProbeOutput(stream io.Reader, limit int) (string, error) {
	var buffer bytes.Buffer
	_, err := io.Copy(&buffer, io.LimitReader(stream, int64(limit)))
	return buffer.String(), err
}

// firstLine returns the first non-empty output line.
func firstLine(output string) string {
	for _, line := range strings.Split(output, "\n") {
		if line = strings.TrimSpace(strings.TrimSuffix(line, "\r")); line != "" {
			return line
		}
	}
	return ""
}

// validContainerShell accepts the absolute path the probe printed. Anything
// with whitespace or a non-absolute shape is refused so a weird image cannot
// smuggle an argument into the exec command.
func validContainerShell(shell string) bool {
	if !strings.HasPrefix(shell, "/") || strings.ContainsAny(shell, " \t\r\n\x00") {
		return false
	}
	return true
}

// displayContainerUser renders the container user for the opened event.
func displayContainerUser(user string) string {
	if user = strings.TrimSpace(user); user == "" {
		return "root"
	}
	return user
}

// createExec creates one exec instance and returns its id.
func (c *DockerClient) createExec(ctx context.Context, containerID string, command []string, user, workdir string, env []string, tty bool) (string, error) {
	body := map[string]any{
		"AttachStdin":  true,
		"AttachStdout": true,
		"AttachStderr": true,
		"Tty":          tty,
		"Cmd":          command,
	}
	if len(env) > 0 {
		body["Env"] = env
	}
	if user != "" {
		body["User"] = user
	}
	if workdir != "" {
		body["WorkingDir"] = workdir
	}
	var out struct {
		ID string `json:"Id"`
	}
	path := "/containers/" + url.PathEscape(containerID) + "/exec"
	if err := c.doJSON(ctx, http.MethodPost, path, body, &out); err != nil {
		return "", err
	}
	if out.ID == "" {
		return "", errors.New("docker: exec create response missing Id")
	}
	return out.ID, nil
}

// startExec starts an exec and hijacks the connection. Docker answers 101 and
// the response body is the duplex stdio stream.
func (c *DockerClient) startExec(ctx context.Context, execID string, tty bool) (io.ReadWriteCloser, error) {
	encoded, err := json.Marshal(map[string]any{"Detach": false, "Tty": tty})
	if err != nil {
		return nil, fmt.Errorf("docker: marshal exec start: %w", err)
	}
	path := "/exec/" + url.PathEscape(execID) + "/start"
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(encoded))
	if err != nil {
		return nil, fmt.Errorf("docker: %s %s: %w", http.MethodPost, path, err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Connection", "Upgrade")
	request.Header.Set("Upgrade", "tcp")
	request.Header.Set("Accept", "application/vnd.docker.raw-stream")

	response, err := c.http.Do(request)
	if err != nil {
		return nil, dockerTransportError(http.MethodPost, path, err)
	}
	if response.StatusCode != http.StatusSwitchingProtocols {
		return nil, statusError(http.MethodPost, path, response)
	}
	stream, ok := response.Body.(io.ReadWriteCloser)
	if !ok {
		_ = response.Body.Close()
		return nil, errors.New("docker: exec start did not return a duplex stream")
	}
	return stream, nil
}

// execExitCode reads the exec's final status, waiting briefly for the daemon to
// publish it after the attached stream ended. A missing exec (the container was
// removed or replaced) reports -1 without an error: the stream has already
// ended, and the terminal contract treats that as a normal close.
func (c *DockerClient) execExitCode(ctx context.Context, execID string) (int32, string, error) {
	if strings.TrimSpace(execID) == "" {
		return -1, "", errors.New("docker: exec id is required")
	}
	waitCtx, cancel := context.WithTimeout(ctx, execExitTimeout)
	defer cancel()
	path := "/exec/" + url.PathEscape(execID) + "/json"
	for {
		var out struct {
			ExitCode int32 `json:"ExitCode"`
			Running  bool  `json:"Running"`
		}
		err := c.doJSON(waitCtx, http.MethodGet, path, nil, &out)
		if err != nil {
			if errors.Is(err, ErrDockerNotFound) {
				return -1, "", nil
			}
			return -1, "", err
		}
		if !out.Running {
			return out.ExitCode, "", nil
		}
		select {
		case <-waitCtx.Done():
			return -1, "", nil
		case <-time.After(execExitPoll):
		}
	}
}

// dockerExecSession is one attached exec. It owns the hijacked connection and
// the shell's process id recorded by the fixed wrapper.
type dockerExecSession struct {
	client      *DockerClient
	id          string
	containerID string
	pid         int
	stream      io.ReadWriteCloser
	reader      *bufio.Reader
	shell       string
	user        string
	execUser    string
	workdir     string
	env         []string
	ctx         context.Context

	closeOnce sync.Once
	closeErr  error
	killMu    sync.Mutex
	killed    bool
}

// Read reads the exec process's TTY output. The buffered reader preserves any
// bytes that arrived together with the wrapper's pid line.
func (s *dockerExecSession) Read(p []byte) (int, error) { return s.reader.Read(p) }

// Write sends bytes to the exec process's stdin.
func (s *dockerExecSession) Write(p []byte) (int, error) { return s.stream.Write(p) }

// Close ends the exec process tree, verifies the exec is no longer Running and
// then releases the attachment. It never stops the application container.
func (s *dockerExecSession) Close() error {
	s.closeOnce.Do(func() {
		// The helper must run before the attach connection is closed: the
		// daemon keeps an exec alive after the client disconnects.
		_ = s.Kill(context.Background())
		s.closeErr = s.stream.Close()
	})
	return s.closeErr
}

// Kill ends the exec's shell and every descendant, then verifies through the
// Docker exec inspect endpoint that the exec is no longer Running. It is
// idempotent and uses a detached, bounded context so it still works when the
// terminal stream was cancelled. It never stops the application container.
func (s *dockerExecSession) Kill(ctx context.Context) error {
	s.killMu.Lock()
	defer s.killMu.Unlock()
	if s.killed || s.pid <= 1 || s.client == nil || s.containerID == "" {
		return nil
	}
	callCtx, cancel := context.WithTimeout(ctx, execKillTimeout)
	defer cancel()
	killErr := s.client.killExecTree(callCtx, s.containerID, s.pid, s.execUser, s.workdir, s.env)
	running, verifyErr := s.client.execRunning(callCtx, s.id)
	if verifyErr == nil && !running {
		s.killed = true
		return nil
	}
	// Give the daemon a short window to publish the shutdown, then check once
	// more. A still-running exec is a real cleanup failure and is reported.
	deadline := time.Now().Add(execStillRunningTimeout)
	for running && time.Now().Before(deadline) {
		select {
		case <-callCtx.Done():
			return fmt.Errorf("docker: verify exec stopped: %w", callCtx.Err())
		case <-time.After(execExitPoll):
		}
		if verifyErr != nil {
			break
		}
		running, verifyErr = s.client.execRunning(callCtx, s.id)
	}
	if verifyErr != nil {
		return fmt.Errorf("docker: verify exec stopped: %w", verifyErr)
	}
	if running {
		if killErr != nil {
			return fmt.Errorf("docker: exec still running after kill helper: %v", killErr)
		}
		return errors.New("docker: exec still running after kill helper")
	}
	s.killed = true
	return nil
}

// execRunning reports whether the exec is still Running. A missing exec (the
// container was removed) is not running.
func (c *DockerClient) execRunning(ctx context.Context, execID string) (bool, error) {
	if strings.TrimSpace(execID) == "" {
		return false, errors.New("docker: exec id is required")
	}
	var out struct {
		Running bool `json:"Running"`
	}
	path := "/exec/" + url.PathEscape(execID) + "/json"
	if err := c.doJSON(ctx, http.MethodGet, path, nil, &out); err != nil {
		if errors.Is(err, ErrDockerNotFound) {
			return false, nil
		}
		return false, err
	}
	return out.Running, nil
}

// killExecTree runs the fixed kill helper in the same container as the terminal
// exec. The helper receives the validated numeric pid as $1; no caller input is
// interpolated into the script.
func (c *DockerClient) killExecTree(ctx context.Context, containerID string, pid int, user, workdir string, env []string) error {
	if pid <= 1 {
		return nil
	}
	command := []string{"/bin/sh", "-c", execKillTreeScript, "gotham-terminal-kill", strconv.Itoa(pid)}
	execID, err := c.createExec(ctx, containerID, command, user, workdir, env, false)
	if err != nil {
		return err
	}
	stream, err := c.startExec(ctx, execID, false)
	if err != nil {
		return err
	}
	// The helper is finite, but bound the read anyway: a wedged container must
	// not pin the close path after the attach connection is gone.
	readDone := make(chan struct{})
	go func() {
		_, _ = io.Copy(io.Discard, io.LimitReader(stream, execProbeMaxBytes))
		close(readDone)
	}()
	select {
	case <-readDone:
	case <-ctx.Done():
		_ = stream.Close()
		<-readDone
	}
	_ = stream.Close()
	code, _, exitErr := c.execExitCode(ctx, execID)
	if exitErr != nil {
		return exitErr
	}
	if code != 0 {
		return fmt.Errorf("docker: kill helper exited with code %d", code)
	}
	return nil
}

// Resize applies a new TTY window size through the Docker exec resize endpoint.
func (s *dockerExecSession) Resize(ctx context.Context, cols, rows uint16) error {
	if s.client == nil || s.id == "" {
		return errors.New("docker: exec session is not attached")
	}
	if cols == 0 || rows == 0 {
		return errors.New("docker: exec dimensions must be positive")
	}
	query := url.Values{}
	query.Set("w", strconv.FormatUint(uint64(cols), 10))
	query.Set("h", strconv.FormatUint(uint64(rows), 10))
	callCtx, cancel := context.WithTimeout(ctx, execResizeTimeout)
	defer cancel()
	return s.client.doJSON(callCtx, http.MethodPost,
		"/exec/"+url.PathEscape(s.id)+"/resize?"+query.Encode(), nil, nil)
}

// ExitCode waits for the exec's final status.
func (s *dockerExecSession) ExitCode(ctx context.Context) (int32, string, error) {
	if s.client == nil || s.id == "" {
		return -1, "", errors.New("docker: exec session is not attached")
	}
	return s.client.execExitCode(ctx, s.id)
}

// Shell reports the shell executable the agent chose.
func (s *dockerExecSession) Shell() string { return s.shell }

// User reports the container user the exec runs as.
func (s *dockerExecSession) User() string { return s.user }

// doJSON issues a request, optionally encoding body as JSON, and decodes a JSON
// response into out when out is non-nil.
func (c *DockerClient) doJSON(ctx context.Context, method, path string, body, out any) error {
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("docker: marshal request: %w", err)
		}
		reader = bytes.NewReader(encoded)
	}

	response, err := c.do(ctx, method, path, reader)
	if err != nil {
		return err
	}
	defer func() { _ = response.Body.Close() }()

	if out == nil {
		_, _ = io.Copy(io.Discard, response.Body)
		return nil
	}
	if err := json.NewDecoder(response.Body).Decode(out); err != nil {
		return fmt.Errorf("docker: decode response: %w", err)
	}
	return nil
}

// do issues a JSON request and validates the HTTP status. The caller owns the
// response body.
func (c *DockerClient) do(ctx context.Context, method, path string, body io.Reader) (*http.Response, error) {
	return c.doHeader(ctx, method, path, body, "application/json")
}

// doRegistry issues an image push with the node-local registry credential (the
// anonymous config when none is configured) and validates the HTTP status. The
// engine rejects a push that carries no auth header at all (moby/moby#50614,
// Docker 28+), so a header is always sent. The caller owns the response body.
func (c *DockerClient) doRegistry(ctx context.Context, path string) (*http.Response, error) {
	authHeader, err := c.registryAuthHeader("")
	if err != nil {
		return nil, err
	}
	return c.doRegistryRequest(ctx, http.MethodPost, path, authHeader)
}

// registryAuthHeader returns the X-Registry-Auth value for an image operation.
// The cached node-local registry credential is used when it owns image (or when
// image is empty, meaning a push to the node registry); every other image,
// including the registry:2 bootstrap pull from Docker Hub, is anonymous so the
// node credential is never leaked to another registry.
func (c *DockerClient) registryAuthHeader(image string) (string, error) {
	c.mu.Lock()
	reg := c.registryAuth
	c.mu.Unlock()
	if reg.Username == "" || (image != "" && !registryOwnsImage(reg.Address, image)) {
		return anonymousRegistryAuth, nil
	}
	return reg.header()
}

// ensureRegistryCredentialFor loads the persisted registry credential when
// image targets the node-local registry but no credential is cached yet — for
// example an agent that restarted between a build and the deploy's confirming
// pull. The address is taken from the image reference, so it is first confirmed
// against the live registry container's published address; a stale reference
// (or a different local process on the same port) is left anonymous rather than
// handed the node credential.
func (c *DockerClient) ensureRegistryCredentialFor(ctx context.Context, image string) error {
	c.mu.Lock()
	cached := c.registryAuth
	c.mu.Unlock()
	if cached.Username != "" {
		return nil
	}
	addr := loopbackRegistryAddress(image)
	if addr == "" {
		return nil
	}
	c.registryMu.Lock()
	defer c.registryMu.Unlock()
	// Re-check under the lock: a concurrent EnsureRegistry may have cached it.
	c.mu.Lock()
	cached = c.registryAuth
	c.mu.Unlock()
	if cached.Username != "" {
		return nil
	}

	info, err := c.inspectRegistryContainer(ctx)
	if err != nil {
		// No live gotham-managed registry to authenticate against.
		return nil
	}
	if !registryManaged(info) || registryHostPort(info) != addr {
		return nil
	}

	auth, ok, err := persistedRegistryAuth(c.registryStateDir)
	if err != nil {
		return err
	}
	if !ok {
		// No stored credential yet, or the htpasswd no longer matches: leave
		// this pull anonymous. EnsureRegistry owns credential generation and
		// container recreation, so the pull path never writes.
		return nil
	}
	auth.Address = addr
	c.mu.Lock()
	c.registryAuth = auth
	c.mu.Unlock()
	return nil
}

// loopbackRegistryAddress returns the host:port of image when it targets the
// node-local registry published on loopback, or "" for every other image. The
// node registry is always published on registryHostIP, so requiring that
// prefix distinguishes it from a Docker Hub or third-party reference.
func loopbackRegistryAddress(image string) string {
	ref := strings.TrimSpace(image)
	slash := strings.Index(ref, "/")
	if slash < 0 {
		return ""
	}
	host := ref[:slash]
	if strings.HasPrefix(host, registryHostIP+":") {
		return host
	}
	return ""
}

// registryOwnsImage reports whether image is hosted by the node-local registry
// at addr. The registry always carries a host:port, so a prefix match is exact.
func registryOwnsImage(addr, image string) bool {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return false
	}
	ref := strings.TrimSpace(image)
	return ref == addr || strings.HasPrefix(ref, addr+"/")
}

// doRegistryRequest issues a registry operation with an explicit
// X-Registry-Auth header and validates the HTTP status. The caller owns the
// response body.
func (c *DockerClient) doRegistryRequest(ctx context.Context, method, path, authHeader string) (*http.Response, error) {
	request, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, nil)
	if err != nil {
		return nil, fmt.Errorf("docker: %s %s: %w", method, redactQuerySecrets(path), err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set(registryAuthHeader, authHeader)

	response, err := c.http.Do(request)
	if err != nil {
		return nil, dockerTransportError(method, path, err)
	}
	if !dockerOK(response.StatusCode) {
		return nil, statusError(method, path, response)
	}
	return response, nil
}

// doHeader issues a request with an explicit content type and validates the
// HTTP status. The caller owns the response body.
func (c *DockerClient) doHeader(ctx context.Context, method, path string, body io.Reader, contentType string) (*http.Response, error) {
	response, err := c.doRaw(ctx, method, path, body, contentType)
	if err != nil {
		return nil, err
	}
	if !dockerOK(response.StatusCode) {
		return nil, statusError(method, path, response)
	}
	return response, nil
}

// doRaw issues a request without validating the HTTP status so callers can
// branch on codes such as 404 and 409. contentType is applied only when body
// is non-nil. The caller owns the response body.
func (c *DockerClient) doRaw(ctx context.Context, method, path string, body io.Reader, contentType string) (*http.Response, error) {
	request, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return nil, fmt.Errorf("docker: %s %s: %w", method, redactQuerySecrets(path), err)
	}
	if body != nil && contentType != "" {
		request.Header.Set("Content-Type", contentType)
	}

	response, err := c.http.Do(request)
	if err != nil {
		return nil, dockerTransportError(method, path, err)
	}
	return response, nil
}

// dockerTransportError classifies a transport failure reaching the daemon. A
// canceled or expired caller context keeps its identity so the gRPC mapper can
// report Canceled/DeadlineExceeded; every other transport failure means the
// daemon is unreachable.
func dockerTransportError(method, path string, err error) error {
	path = redactQuerySecrets(path)
	// http.Client.Do wraps transport failures in *url.Error, whose Error()
	// embeds the full request URL including the (secret) buildargs query.
	// Unwrap it so the value can never reach the error text.
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		err = urlErr.Err
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return fmt.Errorf("docker: %s %s: %w", method, path, err)
	}
	return fmt.Errorf("%w: docker: %s %s: %v", ErrDockerUnavailable, method, path, err)
}

// statusError reads a bounded error body, closes the response, and formats the
// status failure. A 404 is classified as a missing container or a missing
// image so the gRPC boundary can answer the right code.
func statusError(method, path string, response *http.Response) error {
	defer func() { _ = response.Body.Close() }()
	path = redactQuerySecrets(path)
	raw, _ := io.ReadAll(io.LimitReader(response.Body, 4<<10))
	message := strings.TrimSpace(string(raw))
	if response.StatusCode == http.StatusNotFound {
		return notFoundError(method, path, message)
	}
	return fmt.Errorf("docker: %s %s: status %d: %s", method, path, response.StatusCode, message)
}

// redactQuerySecrets masks request query parameters that can carry secrets (the
// Docker build's buildargs JSON) so an error message can never expose them. The
// real request still sends the value.
func redactQuerySecrets(path string) string {
	question := strings.IndexByte(path, '?')
	if question < 0 {
		return path
	}
	values, err := url.ParseQuery(path[question+1:])
	if err != nil || !values.Has("buildargs") {
		return path
	}
	values.Set("buildargs", "REDACTED")
	return path[:question+1] + values.Encode()
}

// notFoundError classifies a Docker 404. A container miss is ErrDockerNotFound.
// Only the pull path (/images/create) and a create whose image is absent
// (/containers/create with Docker's "No such image") are ErrDockerImageNotFound;
// the build helpers (tag, push, digest) and every other operation keep the
// container classification, so a 404 on a freshly built image stays NotFound.
func notFoundError(method, path, message string) error {
	lower := strings.ToLower(message)
	switch {
	case strings.Contains(lower, "no such container"):
		return fmt.Errorf("%w: docker: %s %s: status 404: %s", ErrDockerNotFound, method, path, message)
	case strings.HasPrefix(path, "/images/create"):
		return fmt.Errorf("%w: docker: %s %s: status 404: %s", ErrDockerImageNotFound, method, path, message)
	case strings.HasPrefix(path, "/containers/create") && strings.Contains(lower, "no such image"):
		return fmt.Errorf("%w: docker: %s %s: status 404: %s", ErrDockerImageNotFound, method, path, message)
	default:
		return fmt.Errorf("%w: docker: %s %s: status 404: %s", ErrDockerNotFound, method, path, message)
	}
}

// dockerOK reports whether status indicates success. Docker returns 304 for
// idempotent lifecycle calls such as starting an already-running container.
func dockerOK(status int) bool {
	return (status >= http.StatusOK && status < http.StatusMultipleChoices) || status == http.StatusNotModified
}

// LogMessage is one item of a container log stream: either a payload chunk or
// a terminal decode error. The gRPC boundary turns the error into a status so a
// truncated or broken stream is never reported as a clean end. It is exported
// because the dockerClient seam is implemented by external test fakes.
type LogMessage struct {
	Data []byte
	Err  error
}

// decodeLogStream reads Docker's multiplexed log format and emits payloads on
// out. A stream that is not multiplexed (a TTY container) is copied raw after
// the first frame looks invalid. A short stream is delivered rather than
// dropped: a partial read that cannot be a multiplexed header is treated as raw
// content, and a partial frame payload is emitted before the truncation is
// reported. Read errors that are not a clean EOF or a short read are reported
// as a terminal error.
func decodeLogStream(ctx context.Context, source io.Reader, out chan<- LogMessage) {
	reader := bufio.NewReader(source)
	header := make([]byte, 8)
	// sawFrame records that at least one well-formed multiplexed frame has
	// been read. After that, a partial header is a truncated multiplexed
	// stream, not a short raw line.
	sawFrame := false
	for {
		first, err := reader.Peek(1)
		if err != nil {
			if errors.Is(err, io.EOF) {
				return
			}
			report(ctx, out, err)
			return
		}
		if first[0] > 2 {
			// A first byte outside Docker's stream-type set cannot begin a
			// multiplexed header, so this is a raw TTY stream. Copy it
			// immediately instead of waiting for eight bytes: a short line on
			// a following stream is delivered without stalling.
			copyRaw(ctx, reader, out)
			return
		}
		n, err := io.ReadFull(reader, header)
		if err != nil {
			switch {
			case errors.Is(err, io.EOF):
				return
			case errors.Is(err, io.ErrUnexpectedEOF):
				if sawFrame {
					// A partial header after a valid frame is a truncated
					// multiplexed stream, not a short raw line.
					report(ctx, out, fmt.Errorf("truncated log frame: %w", err))
					return
				}
				// Fewer than eight bytes before any frame: too short for a
				// multiplexed header, so this is a short raw line. Deliver
				// what arrived.
				if n > 0 && !emit(ctx, out, header[:n]) {
					return
				}
				return
			default:
				if n > 0 && !emit(ctx, out, header[:n]) {
					return
				}
				report(ctx, out, err)
				return
			}
		}
		size := binary.BigEndian.Uint32(header[4:8])
		if !validLogHeader(header, size) {
			if !emit(ctx, out, header) {
				return
			}
			copyRaw(ctx, reader, out)
			return
		}
		sawFrame = true
		payload := make([]byte, size)
		n, err = io.ReadFull(reader, payload)
		if n > 0 && !emit(ctx, out, payload[:n]) {
			return
		}
		if err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
				report(ctx, out, fmt.Errorf("truncated log frame: %w", err))
			} else {
				report(ctx, out, err)
			}
			return
		}
	}
}

// validLogHeader reports whether header is a well-formed multiplexed frame
// header: a stream type of stdin/stdout/stderr followed by a bounded size.
func validLogHeader(header []byte, size uint32) bool {
	return header[0] <= 2 && header[1] == 0 && header[2] == 0 && header[3] == 0 && size <= maxLogFrame
}

// copyRaw forwards a non-multiplexed stream verbatim. A read error that is not
// a clean EOF is reported as a terminal error.
func copyRaw(ctx context.Context, reader *bufio.Reader, out chan<- LogMessage) {
	buffer := make([]byte, 32<<10)
	for {
		n, err := reader.Read(buffer)
		if n > 0 {
			chunk := make([]byte, n)
			copy(chunk, buffer[:n])
			if !emit(ctx, out, chunk) {
				return
			}
		}
		if err != nil {
			if !errors.Is(err, io.EOF) {
				report(ctx, out, err)
			}
			return
		}
	}
}

// emit sends a data chunk on out, aborting when ctx is canceled.
func emit(ctx context.Context, out chan<- LogMessage, data []byte) bool {
	if len(data) == 0 {
		return true
	}
	select {
	case out <- LogMessage{Data: data}:
		return true
	case <-ctx.Done():
		return false
	}
}

// report sends a terminal stream error on out. A canceled context is a normal
// end, not a failure, so nothing is reported once ctx is done.
func report(ctx context.Context, out chan<- LogMessage, err error) {
	if err == nil || ctx.Err() != nil {
		return
	}
	select {
	case out <- LogMessage{Err: err}:
	case <-ctx.Done():
	}
}

// dockerContainerSummary is the subset of GET /containers/json we consume.
type dockerContainerSummary struct {
	ID      string               `json:"Id"`
	Names   []string             `json:"Names"`
	Image   string               `json:"Image"`
	ImageID string               `json:"ImageID"`
	Status  string               `json:"Status"`
	State   string               `json:"State"`
	Created int64                `json:"Created"`
	Labels  map[string]string    `json:"Labels"`
	Ports   []dockerPortSummary  `json:"Ports"`
	Mounts  []dockerMountSummary `json:"Mounts"`
}

// dockerMountSummary is one bind mount of a container summary.
type dockerMountSummary struct {
	Source      string `json:"Source"`
	Destination string `json:"Destination"`
	Mode        string `json:"Mode"`
	RW          bool   `json:"RW"`
}

// dockerPortSummary is one published port of a container summary.
type dockerPortSummary struct {
	IP          string `json:"IP"`
	PrivatePort int32  `json:"PrivatePort"`
	PublicPort  int32  `json:"PublicPort"`
	Type        string `json:"Type"`
}

// containerName returns the first container name without its leading slash.
func containerName(names []string) string {
	if len(names) == 0 {
		return ""
	}
	return strings.TrimPrefix(names[0], "/")
}

// containerHealth extracts the Docker health status from a container summary's
// human-readable status, which carries one of the following suffixes when the
// container declares a healthcheck:
//
//	"(health: starting)" / "(healthy)" / "(unhealthy)"
//
// A container without a healthcheck has no marker and returns "". This mirrors
// the summary contract Docker has kept stable; the exact value (rather than a
// substring match) is what the control plane gates database readiness on.
func containerHealth(status string) string {
	switch {
	case strings.Contains(status, "(healthy)"):
		return "healthy"
	case strings.Contains(status, "(unhealthy)"):
		return "unhealthy"
	case strings.Contains(status, "(health: starting)"):
		return "starting"
	default:
		return ""
	}
}

// dockerCreateBody is the Docker API container-create payload.
type dockerCreateBody struct {
	Image            string               `json:"Image"`
	Env              []string             `json:"Env,omitempty"`
	Cmd              []string             `json:"Cmd,omitempty"`
	Entrypoint       []string             `json:"Entrypoint,omitempty"`
	Labels           map[string]string    `json:"Labels,omitempty"`
	ExposedPorts     map[string]struct{}  `json:"ExposedPorts,omitempty"`
	HostConfig       *dockerHostConfig    `json:"HostConfig,omitempty"`
	NetworkingConfig *dockerNetworkingCfg `json:"NetworkingConfig,omitempty"`
	Healthcheck      *dockerHealthcheck   `json:"Healthcheck,omitempty"`
}

// dockerHealthcheck is the Docker API HealthConfig. Interval, Timeout and
// StartPeriod are int64 nanoseconds, as the engine expects; Retries is a
// count. Test is the exec-form command with "CMD" prepended.
type dockerHealthcheck struct {
	Test        []string `json:"Test,omitempty"`
	Interval    int64    `json:"Interval,omitempty"`
	Timeout     int64    `json:"Timeout,omitempty"`
	StartPeriod int64    `json:"StartPeriod,omitempty"`
	Retries     int      `json:"Retries,omitempty"`
}

// dockerHostConfig carries bind mounts and port bindings.
type dockerHostConfig struct {
	Binds         []string                `json:"Binds,omitempty"`
	PortBindings  map[string][]dockerPort `json:"PortBindings,omitempty"`
	RestartPolicy *dockerRestartPolicy    `json:"RestartPolicy,omitempty"`
	// NetworkMode pins the container to one network. The registry uses it to
	// stay off the default bridge that workloads share.
	NetworkMode string `json:"NetworkMode,omitempty"`
}

// dockerPort is one host-side port binding.
type dockerPort struct {
	HostIP   string `json:"HostIp,omitempty"`
	HostPort string `json:"HostPort,omitempty"`
}

// dockerRestartPolicy is the Docker restart policy for a container.
type dockerRestartPolicy struct {
	Name string `json:"Name"`
}

// dockerNetworkingCfg attaches the container to the named networks.
type dockerNetworkingCfg struct {
	EndpointsConfig map[string]struct{} `json:"EndpointsConfig,omitempty"`
}

// buildCreateBody maps the proto request onto the Docker container-create
// body. A malformed port mapping is an error rather than a silently dropped
// binding: dropping it would leave the container without a mapping the caller
// believes exists.
func buildCreateBody(req *agentv1.CreateContainerRequest) (*dockerCreateBody, error) {
	body := &dockerCreateBody{
		Image:      req.GetImage(),
		Env:        req.GetEnv(),
		Cmd:        req.GetCommand(),
		Entrypoint: req.GetEntrypoint(),
		Labels:     req.GetLabels(),
	}

	host := &dockerHostConfig{}
	for _, spec := range req.GetPorts() {
		hostIP, hostPort, containerPort, err := parsePortSpec(spec)
		if err != nil {
			return nil, err
		}
		key := containerPort + "/tcp"
		if body.ExposedPorts == nil {
			body.ExposedPorts = map[string]struct{}{}
		}
		body.ExposedPorts[key] = struct{}{}
		if host.PortBindings == nil {
			host.PortBindings = map[string][]dockerPort{}
		}
		host.PortBindings[key] = append(host.PortBindings[key], dockerPort{HostIP: hostIP, HostPort: hostPort})
	}
	if binds := req.GetVolumes(); len(binds) > 0 {
		host.Binds = binds
	}
	if policy := strings.TrimSpace(req.GetRestartPolicy()); policy != "" {
		host.RestartPolicy = &dockerRestartPolicy{Name: policy}
	}
	if len(host.Binds) > 0 || len(host.PortBindings) > 0 || host.RestartPolicy != nil {
		body.HostConfig = host
	}

	if networks := req.GetNetworks(); len(networks) > 0 {
		endpoints := make(map[string]struct{}, len(networks))
		for _, network := range networks {
			if network != "" {
				endpoints[network] = struct{}{}
			}
		}
		if len(endpoints) > 0 {
			body.NetworkingConfig = &dockerNetworkingCfg{EndpointsConfig: endpoints}
		}
	}
	if health := toDockerHealthcheck(req.GetHealthcheck()); health != nil {
		body.Healthcheck = health
	}
	return body, nil
}

// toDockerHealthcheck maps the proto healthcheck onto the Docker HealthConfig.
// The command is exec-form ("CMD" plus the argv), so no shell is involved and
// no quoting rules apply. Seconds become the nanoseconds the engine expects. A
// nil request or an empty test disables the healthcheck (nil, not a zero
// config, so an image with its own healthcheck is left untouched).
func toDockerHealthcheck(h *agentv1.ContainerHealthcheck) *dockerHealthcheck {
	if h == nil || len(h.GetTest()) == 0 {
		return nil
	}
	return &dockerHealthcheck{
		Test:        append([]string{"CMD"}, h.GetTest()...),
		Interval:    h.GetIntervalSeconds() * int64(time.Second),
		Timeout:     h.GetTimeoutSeconds() * int64(time.Second),
		StartPeriod: h.GetStartPeriodSeconds() * int64(time.Second),
		Retries:     int(h.GetRetries()),
	}
}

// parsePortSpec parses a publish mapping: "container", "host:container" or
// "host-ip:host:container" (the production Traefik ping uses the last form to
// bind 8080 to loopback only). A bare "container" spec maps to a
// Docker-assigned host port, and an explicit host port of 0 asks Docker for an
// ephemeral one. Container ports must be 1..65535, host ports 0..65535 and a
// host IP must be a canonical unbracketed IPv4 literal (IPv6 literals cannot
// be expressed unambiguously in this colon-separated form). Malformed input is
// an error, never a dropped binding.
func parsePortSpec(spec string) (hostIP, hostPort, containerPort string, err error) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return "", "", "", fmt.Errorf("%w: empty mapping", ErrInvalidPortMapping)
	}
	parts := strings.Split(spec, ":")
	switch len(parts) {
	case 1:
		if err := validatePort(parts[0], false); err != nil {
			return "", "", "", fmt.Errorf("%w: %q", ErrInvalidPortMapping, spec)
		}
		return "", "", parts[0], nil
	case 2:
		hostPort = strings.TrimSpace(parts[0])
		containerPort = strings.TrimSpace(parts[1])
		if err := validatePort(containerPort, false); err != nil {
			return "", "", "", fmt.Errorf("%w: %q", ErrInvalidPortMapping, spec)
		}
		if hostPort != "" {
			if err := validatePort(hostPort, true); err != nil {
				return "", "", "", fmt.Errorf("%w: %q", ErrInvalidPortMapping, spec)
			}
		}
		return "", hostPort, containerPort, nil
	case 3:
		hostIP = strings.TrimSpace(parts[0])
		hostPort = strings.TrimSpace(parts[1])
		containerPort = strings.TrimSpace(parts[2])
		if err := validatePort(containerPort, false); err != nil {
			return "", "", "", fmt.Errorf("%w: %q", ErrInvalidPortMapping, spec)
		}
		if err := validatePort(hostPort, true); err != nil {
			return "", "", "", fmt.Errorf("%w: %q", ErrInvalidPortMapping, spec)
		}
		if !canonicalIPv4(hostIP) {
			return "", "", "", fmt.Errorf("%w: %q has no canonical IPv4 host ip", ErrInvalidPortMapping, spec)
		}
		return hostIP, hostPort, containerPort, nil
	default:
		return "", "", "", fmt.Errorf("%w: %q", ErrInvalidPortMapping, spec)
	}
}

// validatePort enforces 1..65535 (0..65535 when allowZeroHost is set: a host
// port of 0 asks Docker for an ephemeral publication).
func validatePort(raw string, allowZeroHost bool) error {
	if !allDigits(raw) {
		return ErrInvalidPortMapping
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return ErrInvalidPortMapping
	}
	minimum := 1
	if allowZeroHost {
		minimum = 0
	}
	if value < minimum || value > 65535 {
		return ErrInvalidPortMapping
	}
	return nil
}

// canonicalIPv4 reports whether ip is a plain IPv4 literal in canonical form
// (rejecting bracketed and zero-padded spellings).
func canonicalIPv4(ip string) bool {
	parsed := net.ParseIP(ip)
	if parsed == nil || parsed.To4() == nil {
		return false
	}
	return parsed.String() == ip
}

// allDigits reports whether s is a non-empty string of ASCII digits.
func allDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// boolParam renders a bool as "1" or "0" for Docker query parameters.
func boolParam(value bool) string {
	if value {
		return "1"
	}
	return "0"
}
