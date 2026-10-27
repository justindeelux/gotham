package docker

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const maxLogFrame = 16 << 20

const runCleanupTimeout = 30 * time.Second

var ErrInvalidPortMapping = errors.New("docker: invalid port mapping")

var ErrDockerNotFound = errors.New("docker: not found")

var ErrDockerUnavailable = errors.New("docker: daemon unavailable")

var ErrDockerImageNotFound = errors.New("docker: image not found")

var ErrDockerContainerNotRunning = errors.New("docker: container is not running")

var ErrDockerNoShell = errors.New("docker: container has no supported shell")

type DockerClient struct {
	http             *http.Client
	baseURL          string
	dockerHost       string
	log              *slog.Logger
	registryStateDir string

	mu           sync.Mutex
	registryAuth registryAuth
	registryMu   sync.Mutex
}

type DockerClientOption func(*DockerClient)

func WithRegistryStateDir(dir string) DockerClientOption {
	return func(c *DockerClient) {
		c.registryStateDir = strings.TrimSpace(dir)
	}
}

func WithLogger(log *slog.Logger) DockerClientOption {
	return func(c *DockerClient) {
		c.log = log
	}
}

func (c *DockerClient) logger() *slog.Logger {
	if c.log == nil {
		return slog.Default()
	}
	return c.log
}

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

func (c *DockerClient) Version(ctx context.Context) (string, error) {
	var out struct {
		Version string `json:"Version"`
	}
	if err := c.doJSON(ctx, http.MethodGet, "/version", nil, &out); err != nil {
		return "", err
	}
	return out.Version, nil
}

// DockerRootDir reports the engine's data root from /info. Monitoring uses it
// as engine mount evidence so a separate Docker data mount is reported
// independently from the root filesystem.
func (c *DockerClient) DockerRootDir(ctx context.Context) (string, error) {
	var out struct {
		DockerRootDir string `json:"DockerRootDir"`
	}
	if err := c.doJSON(ctx, http.MethodGet, "/info", nil, &out); err != nil {
		return "", err
	}
	return strings.TrimSpace(out.DockerRootDir), nil
}

// ContainerDetails is the per-container evidence monitoring reports. It is
// deliberately limited to state, health, restart/OOM evidence and lifecycle
// timestamps: labels and environment are handled by the caller.
type ContainerDetails struct {
	State        string
	Health       string
	RestartCount int64
	OOMKilled    bool
	CreatedAt    time.Time
	StartedAt    time.Time
	FinishedAt   time.Time
}

func (c *DockerClient) InspectContainer(ctx context.Context, id string) (ContainerDetails, error) {
	if strings.TrimSpace(id) == "" {
		return ContainerDetails{}, errors.New("docker: container id is required")
	}
	var out struct {
		Created time.Time `json:"Created"`
		State   struct {
			Status     string    `json:"Status"`
			OOMKilled  bool      `json:"OOMKilled"`
			Restarting bool      `json:"Restarting"`
			StartedAt  time.Time `json:"StartedAt"`
			FinishedAt time.Time `json:"FinishedAt"`
			Health     *struct {
				Status string `json:"Status"`
			} `json:"Health"`
		} `json:"State"`
		RestartCount int64 `json:"RestartCount"`
	}
	path := "/containers/" + url.PathEscape(id) + "/json"
	if err := c.doJSON(ctx, http.MethodGet, path, nil, &out); err != nil {
		return ContainerDetails{}, err
	}
	health := ""
	if out.State.Health != nil {
		health = out.State.Health.Status
	}
	return ContainerDetails{
		State:        out.State.Status,
		Health:       health,
		RestartCount: out.RestartCount,
		OOMKilled:    out.State.OOMKilled,
		CreatedAt:    out.Created,
		StartedAt:    out.State.StartedAt,
		FinishedAt:   out.State.FinishedAt,
	}, nil
}

func (c *DockerClient) ListContainers(ctx context.Context, all bool, labels map[string]string, resolveDevices bool) ([]*agentv1.ContainerInfo, error) {
	path := "/containers/json?all=0"
	if all {
		path = "/containers/json?all=1"
	}
	if len(labels) > 0 {
		pairs := make([]string, 0, len(labels))
		for key, value := range labels {
			pairs = append(pairs, key+"="+value)
		}
		sort.Strings(pairs)
		filters, err := json.Marshal(map[string][]string{"label": pairs})
		if err != nil {
			return nil, fmt.Errorf("docker: encode label filter: %w", err)
		}
		path += "&filters=" + url.QueryEscape(string(filters))
	}
	var summaries []dockerContainerSummary
	if err := c.doJSON(ctx, http.MethodGet, path, nil, &summaries); err != nil {
		return nil, err
	}
	type deviceResult struct {
		device     string
		unverified bool
	}
	devices := map[string]deviceResult{}
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
			var resolved deviceResult
			asked := resolveDevices && mount.Type == "volume" && mount.Name != ""
			if asked {
				var ok bool
				resolved, ok = devices[mount.Name]
				if !ok {
					device, err := c.volumeDevice(ctx, mount.Name)
					resolved = deviceResult{device: device}
					if err != nil {
						resolved.unverified = true
						c.logger().Debug("docker: resolve volume device failed; leaving it unverified",
							"volume", mount.Name, "error", err)
					}
					devices[mount.Name] = resolved
				}
			}
			info.Mounts = append(info.Mounts, &agentv1.ContainerMount{
				Source:                 mount.Source,
				Destination:            mount.Destination,
				ReadOnly:               !mount.RW,
				Type:                   mount.Type,
				VolumeName:             mount.Name,
				VolumeDevice:           resolved.device,
				VolumeDeviceUnverified: resolved.unverified,
			})
		}
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

func (c *DockerClient) volumeDevice(ctx context.Context, name string) (string, error) {
	var out struct {
		Driver  string            `json:"Driver"`
		Options map[string]string `json:"Options"`
	}
	if err := c.doJSON(ctx, http.MethodGet, "/volumes/"+url.PathEscape(name), nil, &out); err != nil {
		return "", err
	}
	if out.Driver != "local" {
		return "", nil
	}
	device := strings.TrimSpace(out.Options["device"])
	if device == "" || !filepath.IsAbs(device) {
		return "", nil
	}
	opts := strings.ToLower(out.Options["o"])
	kind := strings.ToLower(strings.TrimSpace(out.Options["type"]))
	bind := kind == "none"
	for _, option := range strings.Split(opts, ",") {
		if strings.TrimSpace(option) == "bind" {
			bind = true
		}
	}
	if !bind {
		return "", nil
	}
	return filepath.Clean(device), nil
}

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

func (c *DockerClient) Start(ctx context.Context, id string) error {
	return c.action(ctx, id, "start")
}

func (c *DockerClient) Stop(ctx context.Context, id string) error {
	return c.action(ctx, id, "stop")
}

func (c *DockerClient) Restart(ctx context.Context, id string) error {
	return c.action(ctx, id, "restart")
}

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

func registryServerAddress(image string) string {
	ref := strings.TrimSpace(image)
	if host, _, ok := strings.Cut(ref, "/"); ok && (strings.Contains(host, ".") || strings.Contains(host, ":") || host == "localhost") {
		return host
	}
	return "https://index.docker.io/v1/"
}

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
		names = append(names, "docker.io/library/"+name)
	case !isRegistryHostHead(head):
		names = append(names, "docker.io/"+name)
	default:
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

func isRegistryHostHead(head string) bool {
	return strings.Contains(head, ".") || strings.Contains(head, ":") || strings.EqualFold(head, "localhost")
}

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

func (c *DockerClient) RunImage(ctx context.Context, req *agentv1.CreateContainerRequest) (string, error) {
	id, err := c.CreateContainer(ctx, req)
	if err != nil {
		return "", err
	}
	if err := c.Start(ctx, id); err != nil {
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), runCleanupTimeout)
		defer cancel()
		if removeErr := c.Remove(cleanupCtx, id); removeErr != nil {
			return "", fmt.Errorf("%w: container %s left behind (cleanup failed: %v)", err, id, removeErr)
		}
		return "", err
	}
	return id, nil
}

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

const execProbeTimeout = 10 * time.Second

const execResizeTimeout = 5 * time.Second

const execExitTimeout = 2 * time.Second

const execExitPoll = 50 * time.Millisecond

const execProbeMaxBytes = 4 << 10

const execKillTimeout = 8 * time.Second

const execStillRunningTimeout = 3 * time.Second

const ExecWrapperCommand = `printf '%s\n' "$$"; exec "$@"`

const ExecKillTreeScript = `root=$1
marker=$2
case "$root" in ''|*[!0-9]*) exit 2;; esac
[ "$root" -gt 1 ] || exit 2
case "$marker" in ''|*[!A-Za-z0-9-]*) marker="";; esac
if [ ! -d /proc ]; then
  echo "marker-scan-unavailable (no /proc)"
  exit 4
fi
read_stat() {
  stat_line=""
  { while IFS= read -r line || [ -n "$line" ]; do
      stat_line="$stat_line$line "
    done < "$1/stat"; } 2>/dev/null
  [ -n "$stat_line" ]
}
marker_match() {
  marker_data=$({ tr '\0' '\n' < "$1/environ"; } 2>/dev/null) || return 2
  case "$marker_data" in
    *"GOTHAM_TERMINAL_ID=$marker"*) return 0 ;;
    *) return 1 ;;
  esac
}
parse_stat() {
  stat_rest=${1##*) }
  set -- $stat_rest
  stat_state=$1
  stat_ppid=$2
  stat_sid=$4
}
marker_tools=""
inspect_failed=""
if [ -z "$marker" ]; then
  inspect_failed=1
elif command -v tr >/dev/null 2>&1 && command -v grep >/dev/null 2>&1; then
  marker_tools=1
else
  inspect_failed=1
fi
list=" $root "
add_pid() {
  case " $list " in
    *" $1 "*) ;;
    *) list="$list$1 " ;;
  esac
}
frontier="$root"
while [ -n "$frontier" ]; do
  next=""
  for d in /proc/[0-9]*; do
    pid=${d#/proc/}
    [ "$pid" -gt 1 ] || continue
    [ "$pid" = "$$" ] && continue
    read_stat "$d" || continue
    parse_stat "$stat_line"
    if [ "$stat_sid" = "$root" ]; then add_pid "$pid"; fi
    case " $frontier " in
      *" $stat_ppid "*)
        add_pid "$pid"
        next="$next$pid "
        ;;
    esac
  done
  frontier=$next
done
if [ -n "$marker" ] && [ -n "$marker_tools" ]; then
  for d in /proc/[0-9]*; do
    pid=${d#/proc/}
    [ "$pid" -gt 1 ] || continue
    [ "$pid" = "$$" ] && continue
    [ -r "$d/environ" ] || continue
    if marker_match "$d"; then add_pid "$pid"; fi
  done
fi
for sig in HUP TERM KILL; do
  for pid in $list; do
    [ "$pid" -gt 1 ] || continue
    [ "$pid" = "$$" ] && continue
    kill -"$sig" "$pid" 2>/dev/null
  done
  [ "$sig" = "KILL" ] && break
  sleep 0.2 2>/dev/null || :
done
sleep 0.2 2>/dev/null || :
attempt=0
survivors=""
while [ "$attempt" -lt 5 ]; do
  survivors=""
  for d in /proc/[0-9]*; do
    pid=${d#/proc/}
    [ "$pid" -gt 1 ] || continue
    [ "$pid" = "$$" ] && continue
    read_stat "$d" || continue
    parse_stat "$stat_line"
    [ "$stat_state" = "Z" ] && continue
    case " $list " in
      *" $pid "*) survivors="$survivors $pid"; continue ;;
    esac
    if [ "$stat_sid" = "$root" ]; then survivors="$survivors $pid"; continue; fi
    if [ -n "$marker" ] && [ -n "$marker_tools" ] && [ -r "$d/environ" ]; then
      if marker_match "$d"; then survivors="$survivors $pid"; fi
    fi
  done
  if [ -z "$survivors" ] && [ -z "$inspect_failed" ]; then exit 0; fi
  sleep 0.2 2>/dev/null || :
  attempt=$((attempt + 1))
done
if [ -n "$inspect_failed" ]; then
  echo "marker-scan-unavailable"
  [ -n "$survivors" ] && echo "survivors:$survivors"
  exit 4
fi
echo "survivors:$survivors"
exit 3`

const ExecKillMarkerScript = `marker=$1
case "$marker" in ''|*[!A-Za-z0-9-]*) exit 2;; esac
if [ ! -d /proc ]; then
  echo "marker-scan-unavailable (no /proc)"
  exit 4
fi
if ! command -v tr >/dev/null 2>&1 || ! command -v grep >/dev/null 2>&1; then
  echo "marker-scan-unavailable (no tr/grep)"
  exit 4
fi
marker_match() {
  marker_data=$({ tr '\0' '\n' < "$1/environ"; } 2>/dev/null) || return 2
  case "$marker_data" in
    *"GOTHAM_TERMINAL_ID=$marker"*) return 0 ;;
    *) return 1 ;;
  esac
}
list=""
add_pid() {
  case " $list " in
    *" $1 "*) ;;
    *) list="$list$1 " ;;
  esac
}
for d in /proc/[0-9]*; do
  pid=${d#/proc/}
  [ "$pid" -gt 1 ] || continue
  [ "$pid" = "$$" ] && continue
  [ -r "$d/environ" ] || continue
  if marker_match "$d"; then add_pid "$pid"; fi
done
for sig in HUP TERM KILL; do
  for pid in $list; do
    [ "$pid" -gt 1 ] || continue
    [ "$pid" = "$$" ] && continue
    kill -"$sig" "$pid" 2>/dev/null
  done
  [ "$sig" = "KILL" ] && break
  sleep 0.2 2>/dev/null || :
done
sleep 0.2 2>/dev/null || :
attempt=0
survivors=""
while [ "$attempt" -lt 5 ]; do
  survivors=""
  for d in /proc/[0-9]*; do
    pid=${d#/proc/}
    [ "$pid" -gt 1 ] || continue
    [ "$pid" = "$$" ] && continue
    [ -r "$d/environ" ] || continue
    if marker_match "$d"; then survivors="$survivors $pid"; fi
  done
  [ -z "$survivors" ] && exit 0
  sleep 0.2 2>/dev/null || :
  attempt=$((attempt + 1))
done
echo "survivors:$survivors"
exit 3`

const ShellProbeCommand = `for s in bash sh ash; do p=$(command -v "$s" 2>/dev/null) && { echo "$p"; exit 0; }; done; exit 127`

type ExecOptions struct {
	Cmd         []string
	Env         []string
	User        string
	WorkingDir  string
	Cols        uint16
	Rows        uint16
	Integration bool
}

type ExecSession interface {
	io.Reader
	io.Writer
	io.Closer
	Resize(ctx context.Context, cols, rows uint16) error
	ExitCode(ctx context.Context) (int32, string, error)
	Kill(ctx context.Context) error
	Shell() string
	User() string
}

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

	shellPath, err := c.probeShell(ctx, containerID, user, workdir, inspect.Config.Env)
	if err != nil {
		return nil, err
	}
	command := opts.Cmd
	if len(command) == 0 {
		command = []string{shellPath}
	}
	interactiveShell := command[0]
	if opts.Integration {
		command = ContainerBashCommand(command, opts.Integration)
	}
	wrapped := append([]string{"/bin/sh", "-c", ExecWrapperCommand, "gotham-terminal"}, command...)

	marker := uuid.NewString()
	env := append([]string(nil), opts.Env...)
	env = append(env, "GOTHAM_TERMINAL_ID="+marker)
	env = TerminalEnvDescription(env, inspect.Config.Env)
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
		shell:       interactiveShell,
		user:        DisplayContainerUser(user),
		execUser:    user,
		workdir:     workdir,
		marker:      marker,
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

func (c *DockerClient) inspectContainer(ctx context.Context, containerID string) (containerInspect, error) {
	var out containerInspect
	path := "/containers/" + url.PathEscape(containerID) + "/json"
	if err := c.doJSON(ctx, http.MethodGet, path, nil, &out); err != nil {
		return out, err
	}
	return out, nil
}

func (c *DockerClient) probeShell(ctx context.Context, containerID, user, workdir string, imageEnv []string) (string, error) {
	env := append(append([]string(nil), imageEnv...), "TERM=xterm-256color")
	execID, err := c.createExec(ctx, containerID, []string{"/bin/sh", "-c", ShellProbeCommand}, user, workdir, env, true)
	if err != nil {
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
	if !ValidContainerShell(shell) {
		return "", fmt.Errorf("%w: the image exposes no supported shell", ErrDockerNoShell)
	}
	return shell, nil
}

func readProbeOutput(stream io.Reader, limit int) (string, error) {
	var buffer bytes.Buffer
	_, err := io.Copy(&buffer, io.LimitReader(stream, int64(limit)))
	return buffer.String(), err
}

func firstLine(output string) string {
	for _, line := range strings.Split(output, "\n") {
		if line = strings.TrimSpace(strings.TrimSuffix(line, "\r")); line != "" {
			return line
		}
	}
	return ""
}

func ValidContainerShell(shell string) bool {
	if !strings.HasPrefix(shell, "/") || strings.ContainsAny(shell, " \t\r\n\x00") {
		return false
	}
	return true
}

func DisplayContainerUser(user string) string {
	if user = strings.TrimSpace(user); user == "" {
		return "root"
	}
	return user
}

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
	marker      string
	ctx         context.Context

	closeOnce sync.Once
	closeErr  error
	killMu    sync.Mutex
	killed    bool
}

func (s *dockerExecSession) Read(p []byte) (int, error) { return s.reader.Read(p) }

func (s *dockerExecSession) Write(p []byte) (int, error) { return s.stream.Write(p) }

func (s *dockerExecSession) Close() error {
	s.closeOnce.Do(func() {
		cleanupErr := s.Kill(context.Background())
		s.closeErr = errors.Join(cleanupErr, s.stream.Close())
	})
	return s.closeErr
}

func (s *dockerExecSession) Kill(ctx context.Context) error {
	s.killMu.Lock()
	defer s.killMu.Unlock()
	if s.killed || s.pid <= 1 || s.client == nil || s.containerID == "" {
		return nil
	}
	callCtx, cancel := context.WithTimeout(ctx, execKillTimeout)
	defer cancel()
	running, verifyErr := s.client.execRunning(callCtx, s.id)
	if verifyErr != nil {
		return fmt.Errorf("docker: inspect exec %s: %w", s.id, verifyErr)
	}
	var helperOutput string
	var killErr error
	if running {
		helperOutput, killErr = s.client.killExecTree(callCtx, s.containerID, s.pid, s.execUser, s.workdir, s.marker)
	} else {
		helperOutput, killErr = s.client.killExecMarker(callCtx, s.containerID, s.execUser, s.workdir, s.marker)
	}
	running, verifyErr = s.client.execRunning(callCtx, s.id)
	if verifyErr == nil && !running {
		if errors.Is(killErr, ErrDockerNotFound) {
			s.killed = true
			return nil
		}
		if killErr != nil {
			return fmt.Errorf("docker: exec %s cleanup incomplete: %w", s.id, killErr)
		}
		s.killed = true
		return nil
	}
	deadline := time.Now().Add(execStillRunningTimeout)
	for running && time.Now().Before(deadline) {
		select {
		case <-callCtx.Done():
			return fmt.Errorf("docker: verify exec %s stopped: %w", s.id, callCtx.Err())
		case <-time.After(execExitPoll):
		}
		if verifyErr != nil {
			break
		}
		running, verifyErr = s.client.execRunning(callCtx, s.id)
	}
	if verifyErr != nil {
		return fmt.Errorf("docker: verify exec %s stopped: %w", s.id, verifyErr)
	}
	if running {
		if killErr != nil {
			return fmt.Errorf("docker: exec %s still running after kill helper (%s): %w", s.id, helperOutput, killErr)
		}
		return fmt.Errorf("docker: exec %s still running after kill helper", s.id)
	}
	if killErr != nil && !errors.Is(killErr, ErrDockerNotFound) {
		return fmt.Errorf("docker: exec %s cleanup incomplete (%s): %w", s.id, helperOutput, killErr)
	}
	s.killed = true
	return nil
}

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

func (c *DockerClient) killExecTree(ctx context.Context, containerID string, pid int, user, workdir, marker string) (string, error) {
	if pid <= 1 {
		return "", nil
	}
	command := []string{"/bin/sh", "-c", ExecKillTreeScript, "gotham-terminal-kill", strconv.Itoa(pid), marker}
	return c.runKillHelper(ctx, containerID, command, user, workdir)
}

func (c *DockerClient) killExecMarker(ctx context.Context, containerID, user, workdir, marker string) (string, error) {
	command := []string{"/bin/sh", "-c", ExecKillMarkerScript, "gotham-terminal-kill", marker}
	return c.runKillHelper(ctx, containerID, command, user, workdir)
}

func (c *DockerClient) runKillHelper(ctx context.Context, containerID string, command []string, user, workdir string) (string, error) {
	env := []string{"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"}
	execID, err := c.createExec(ctx, containerID, command, user, workdir, env, true)
	if err != nil {
		return "", err
	}
	stream, err := c.startExec(ctx, execID, true)
	if err != nil {
		return "", err
	}
	var output bytes.Buffer
	readDone := make(chan error, 1)
	go func() {
		_, readErr := io.Copy(&output, io.LimitReader(stream, execProbeMaxBytes))
		readDone <- readErr
	}()
	var readErr error
	select {
	case readErr = <-readDone:
	case <-ctx.Done():
		_ = stream.Close()
		readErr = <-readDone
	}
	_ = stream.Close()
	if readErr != nil && ctx.Err() != nil {
		return output.String(), ctx.Err()
	}
	code, _, exitErr := c.execExitCode(ctx, execID)
	if exitErr != nil {
		return output.String(), exitErr
	}
	if code != 0 {
		return output.String(), fmt.Errorf("docker: kill helper exited with code %d: %s", code, strings.TrimSpace(output.String()))
	}
	return output.String(), nil
}

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

func (s *dockerExecSession) ExitCode(ctx context.Context) (int32, string, error) {
	if s.client == nil || s.id == "" {
		return -1, "", errors.New("docker: exec session is not attached")
	}
	return s.client.execExitCode(ctx, s.id)
}

func (s *dockerExecSession) Shell() string { return s.shell }

func (s *dockerExecSession) User() string { return s.user }

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

func (c *DockerClient) do(ctx context.Context, method, path string, body io.Reader) (*http.Response, error) {
	return c.doHeader(ctx, method, path, body, "application/json")
}

func (c *DockerClient) doRegistry(ctx context.Context, path string) (*http.Response, error) {
	authHeader, err := c.registryAuthHeader("")
	if err != nil {
		return nil, err
	}
	return c.doRegistryRequest(ctx, http.MethodPost, path, authHeader)
}

func (c *DockerClient) registryAuthHeader(image string) (string, error) {
	c.mu.Lock()
	reg := c.registryAuth
	c.mu.Unlock()
	if reg.Username == "" || (image != "" && !registryOwnsImage(reg.Address, image)) {
		return anonymousRegistryAuth, nil
	}
	return reg.header()
}

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
	c.mu.Lock()
	cached = c.registryAuth
	c.mu.Unlock()
	if cached.Username != "" {
		return nil
	}

	info, err := c.inspectRegistryContainer(ctx)
	if err != nil {
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
		return nil
	}
	auth.Address = addr
	c.mu.Lock()
	c.registryAuth = auth
	c.mu.Unlock()
	return nil
}

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

func registryOwnsImage(addr, image string) bool {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return false
	}
	ref := strings.TrimSpace(image)
	return ref == addr || strings.HasPrefix(ref, addr+"/")
}

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

func dockerTransportError(method, path string, err error) error {
	path = redactQuerySecrets(path)
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		err = urlErr.Err
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return fmt.Errorf("docker: %s %s: %w", method, path, err)
	}
	return fmt.Errorf("%w: docker: %s %s: %v", ErrDockerUnavailable, method, path, err)
}

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

func dockerOK(status int) bool {
	return (status >= http.StatusOK && status < http.StatusMultipleChoices) || status == http.StatusNotModified
}

type LogMessage struct {
	Data []byte
	Err  error
}

func decodeLogStream(ctx context.Context, source io.Reader, out chan<- LogMessage) {
	reader := bufio.NewReader(source)
	header := make([]byte, 8)
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
					report(ctx, out, fmt.Errorf("truncated log frame: %w", err))
					return
				}
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

func validLogHeader(header []byte, size uint32) bool {
	return header[0] <= 2 && header[1] == 0 && header[2] == 0 && header[3] == 0 && size <= maxLogFrame
}

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

func report(ctx context.Context, out chan<- LogMessage, err error) {
	if err == nil || ctx.Err() != nil {
		return
	}
	select {
	case out <- LogMessage{Err: err}:
	case <-ctx.Done():
	}
}

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

type dockerMountSummary struct {
	Source      string `json:"Source"`
	Destination string `json:"Destination"`
	Type        string `json:"Type"`
	Name        string `json:"Name"`
	Mode        string `json:"Mode"`
	RW          bool   `json:"RW"`
}

type dockerPortSummary struct {
	IP          string `json:"IP"`
	PrivatePort int32  `json:"PrivatePort"`
	PublicPort  int32  `json:"PublicPort"`
	Type        string `json:"Type"`
}

func containerName(names []string) string {
	if len(names) == 0 {
		return ""
	}
	return strings.TrimPrefix(names[0], "/")
}

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

type dockerHealthcheck struct {
	Test        []string `json:"Test,omitempty"`
	Interval    int64    `json:"Interval,omitempty"`
	Timeout     int64    `json:"Timeout,omitempty"`
	StartPeriod int64    `json:"StartPeriod,omitempty"`
	Retries     int      `json:"Retries,omitempty"`
}

type dockerHostConfig struct {
	Binds         []string                `json:"Binds,omitempty"`
	PortBindings  map[string][]dockerPort `json:"PortBindings,omitempty"`
	RestartPolicy *dockerRestartPolicy    `json:"RestartPolicy,omitempty"`
	NetworkMode   string                  `json:"NetworkMode,omitempty"`
	PidsLimit     int64                   `json:"PidsLimit,omitempty"`
	Memory        int64                   `json:"Memory,omitempty"`
	NanoCpus      int64                   `json:"NanoCpus,omitempty"`
}

type dockerPort struct {
	HostIP   string `json:"HostIp,omitempty"`
	HostPort string `json:"HostPort,omitempty"`
}

type dockerRestartPolicy struct {
	Name string `json:"Name"`
}

type dockerNetworkingCfg struct {
	EndpointsConfig map[string]struct{} `json:"EndpointsConfig,omitempty"`
}

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
	host.PidsLimit = req.GetPidsLimit()
	host.Memory = req.GetMemoryBytes()
	host.NanoCpus = req.GetNanoCpus()
	if len(host.Binds) > 0 || len(host.PortBindings) > 0 || host.RestartPolicy != nil ||
		host.PidsLimit > 0 || host.Memory > 0 || host.NanoCpus > 0 {
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

func canonicalIPv4(ip string) bool {
	parsed := net.ParseIP(ip)
	if parsed == nil || parsed.To4() == nil {
		return false
	}
	return parsed.String() == ip
}

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

func boolParam(value bool) string {
	if value {
		return "1"
	}
	return "0"
}
