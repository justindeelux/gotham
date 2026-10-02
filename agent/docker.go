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

// ErrInvalidPortMapping marks a malformed port mapping. The DockerService
// error mapper turns it into InvalidArgument so every caller sees bad input
// as bad input instead of an internal failure.
var ErrInvalidPortMapping = errors.New("docker: invalid port mapping")

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

// PullImage pulls image from its registry, consuming the progress stream. An
// error reported inside the progress payload is returned as an error.
func (c *DockerClient) PullImage(ctx context.Context, image string) error {
	if strings.TrimSpace(image) == "" {
		return errors.New("docker: image is required")
	}
	query := url.Values{}
	query.Set("fromImage", image)

	if err := c.ensureRegistryCredentialFor(image); err != nil {
		return err
	}
	authHeader, err := c.registryAuthHeader(image)
	if err != nil {
		return err
	}
	response, err := c.doRegistryRequest(ctx, http.MethodPost, "/images/create?"+query.Encode(), authHeader)
	if err != nil {
		return err
	}
	defer func() { _ = response.Body.Close() }()

	decoder := json.NewDecoder(response.Body)
	for {
		var message struct {
			Error string `json:"error"`
		}
		switch err := decoder.Decode(&message); {
		case errors.Is(err, io.EOF):
			return nil
		case err != nil:
			return fmt.Errorf("docker: decode pull progress: %w", err)
		case message.Error != "":
			return fmt.Errorf("docker: pull %s: %s", image, message.Error)
		}
	}
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
func (c *DockerClient) RunImage(ctx context.Context, req *agentv1.CreateContainerRequest) (string, error) {
	id, err := c.CreateContainer(ctx, req)
	if err != nil {
		return "", err
	}
	if err := c.Start(ctx, id); err != nil {
		return id, err
	}
	return id, nil
}

// Logs streams the logs of the container with the given id. Each channel item
// is the payload of one Docker multiplexed frame. The channel is closed when
// the stream ends or ctx is canceled.
func (c *DockerClient) Logs(ctx context.Context, id string, follow bool, tail int64) (<-chan []byte, error) {
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

	out := make(chan []byte)
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
// pull. Images for any other registry are left anonymous.
func (c *DockerClient) ensureRegistryCredentialFor(image string) error {
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
	auth, _, err := prepareRegistryAuth(c.registryStateDir)
	if err != nil {
		return err
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
		return nil, fmt.Errorf("docker: %s %s: %w", method, path, err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set(registryAuthHeader, authHeader)

	response, err := c.http.Do(request)
	if err != nil {
		return nil, fmt.Errorf("docker: %s %s: %w", method, path, err)
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
		return nil, fmt.Errorf("docker: %s %s: %w", method, path, err)
	}
	if body != nil && contentType != "" {
		request.Header.Set("Content-Type", contentType)
	}

	response, err := c.http.Do(request)
	if err != nil {
		return nil, fmt.Errorf("docker: %s %s: %w", method, path, err)
	}
	return response, nil
}

// statusError reads a bounded error body, closes the response, and formats the
// status failure.
func statusError(method, path string, response *http.Response) error {
	defer func() { _ = response.Body.Close() }()
	message, _ := io.ReadAll(io.LimitReader(response.Body, 4<<10))
	return fmt.Errorf("docker: %s %s: status %d: %s", method, path, response.StatusCode, strings.TrimSpace(string(message)))
}

// dockerOK reports whether status indicates success. Docker returns 304 for
// idempotent lifecycle calls such as starting an already-running container.
func dockerOK(status int) bool {
	return (status >= http.StatusOK && status < http.StatusMultipleChoices) || status == http.StatusNotModified
}

// decodeLogStream reads Docker's multiplexed log format and emits payloads on
// out. A stream that is not multiplexed (a TTY container) is copied raw after
// the first frame looks invalid.
func decodeLogStream(ctx context.Context, source io.Reader, out chan<- []byte) {
	reader := bufio.NewReader(source)
	header := make([]byte, 8)
	for {
		if _, err := io.ReadFull(reader, header); err != nil {
			return
		}
		size := binary.BigEndian.Uint32(header[4:8])
		if !validLogHeader(header, size) {
			if !emit(ctx, out, header) {
				return
			}
			copyRaw(ctx, reader, out)
			return
		}
		payload := make([]byte, size)
		if _, err := io.ReadFull(reader, payload); err != nil {
			return
		}
		if !emit(ctx, out, payload) {
			return
		}
	}
}

// validLogHeader reports whether header is a well-formed multiplexed frame
// header: a stream type of stdin/stdout/stderr followed by a bounded size.
func validLogHeader(header []byte, size uint32) bool {
	return header[0] <= 2 && header[1] == 0 && header[2] == 0 && header[3] == 0 && size <= maxLogFrame
}

// copyRaw forwards a non-multiplexed stream verbatim.
func copyRaw(ctx context.Context, reader *bufio.Reader, out chan<- []byte) {
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
			return
		}
	}
}

// emit sends data on out, aborting when ctx is canceled.
func emit(ctx context.Context, out chan<- []byte, data []byte) bool {
	if len(data) == 0 {
		return true
	}
	select {
	case out <- data:
		return true
	case <-ctx.Done():
		return false
	}
}

// dockerContainerSummary is the subset of GET /containers/json we consume.
type dockerContainerSummary struct {
	ID      string               `json:"Id"`
	Names   []string             `json:"Names"`
	Image   string               `json:"Image"`
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
	return body, nil
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
