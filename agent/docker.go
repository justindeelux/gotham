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
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// maxLogFrame bounds the allocation for a single multiplexed log frame.
const maxLogFrame = 16 << 20

// DockerClient talks to the Docker Engine API over a unix socket or TCP
// endpoint. It implements the subset of the engine API the agent exposes to
// the control plane.
type DockerClient struct {
	http    *http.Client
	baseURL string
}

// NewDockerClient builds a client for target, a Docker endpoint string as
// understood by newDockerTransport. An empty target uses the default socket.
func NewDockerClient(target string) (*DockerClient, error) {
	transport, baseURL, err := newDockerTransport(target)
	if err != nil {
		return nil, err
	}
	return &DockerClient{http: &http.Client{Transport: transport}, baseURL: baseURL}, nil
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
		if summary.Created > 0 {
			info.CreatedAt = timestamppb.New(time.Unix(summary.Created, 0))
		}
		containers = append(containers, info)
	}
	return containers, nil
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

// PullImage pulls image from its registry, consuming the progress stream. An
// error reported inside the progress payload is returned as an error.
func (c *DockerClient) PullImage(ctx context.Context, image string) error {
	if strings.TrimSpace(image) == "" {
		return errors.New("docker: image is required")
	}
	query := url.Values{}
	query.Set("fromImage", image)

	response, err := c.do(ctx, http.MethodPost, "/images/create?"+query.Encode(), nil)
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
	if err := c.doJSON(ctx, http.MethodPost, path, buildCreateBody(req), &out); err != nil {
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

// do issues a request and validates the HTTP status. The caller owns the
// response body.
func (c *DockerClient) do(ctx context.Context, method, path string, body io.Reader) (*http.Response, error) {
	request, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return nil, fmt.Errorf("docker: build request: %w", err)
	}
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}

	response, err := c.http.Do(request)
	if err != nil {
		return nil, fmt.Errorf("docker: %s %s: %w", method, path, err)
	}
	if !dockerOK(response.StatusCode) {
		defer func() { _ = response.Body.Close() }()
		message, _ := io.ReadAll(io.LimitReader(response.Body, 4<<10))
		return nil, fmt.Errorf("docker: %s %s: status %d: %s", method, path, response.StatusCode, strings.TrimSpace(string(message)))
	}
	return response, nil
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
	ID      string            `json:"Id"`
	Names   []string          `json:"Names"`
	Image   string            `json:"Image"`
	Status  string            `json:"Status"`
	State   string            `json:"State"`
	Created int64             `json:"Created"`
	Labels  map[string]string `json:"Labels"`
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
	Binds        []string                `json:"Binds,omitempty"`
	PortBindings map[string][]dockerPort `json:"PortBindings,omitempty"`
}

// dockerPort is one host-side port binding.
type dockerPort struct {
	HostPort string `json:"HostPort"`
}

// dockerNetworkingCfg attaches the container to the named networks.
type dockerNetworkingCfg struct {
	EndpointsConfig map[string]struct{} `json:"EndpointsConfig,omitempty"`
}

// buildCreateBody maps the proto request onto the Docker container-create body.
func buildCreateBody(req *agentv1.CreateContainerRequest) *dockerCreateBody {
	body := &dockerCreateBody{
		Image:      req.GetImage(),
		Env:        req.GetEnv(),
		Cmd:        req.GetCommand(),
		Entrypoint: req.GetEntrypoint(),
		Labels:     req.GetLabels(),
	}

	host := &dockerHostConfig{}
	for _, spec := range req.GetPorts() {
		hostPort, containerPort, ok := splitPortSpec(spec)
		if !ok {
			continue
		}
		key := containerPort + "/tcp"
		if body.ExposedPorts == nil {
			body.ExposedPorts = map[string]struct{}{}
		}
		body.ExposedPorts[key] = struct{}{}
		if host.PortBindings == nil {
			host.PortBindings = map[string][]dockerPort{}
		}
		host.PortBindings[key] = append(host.PortBindings[key], dockerPort{HostPort: hostPort})
	}
	if binds := req.GetVolumes(); len(binds) > 0 {
		host.Binds = binds
	}
	if len(host.Binds) > 0 || len(host.PortBindings) > 0 {
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
	return body
}

// splitPortSpec parses a "host:container" mapping. A bare "container" spec maps
// to a Docker-assigned host port.
func splitPortSpec(spec string) (host, container string, ok bool) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return "", "", false
	}
	if host, container, found := strings.Cut(spec, ":"); found {
		host = strings.TrimSpace(host)
		container = strings.TrimSpace(container)
		if !allDigits(container) {
			return "", "", false
		}
		return host, container, true
	}
	if !allDigits(spec) {
		return "", "", false
	}
	return "", spec, true
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
