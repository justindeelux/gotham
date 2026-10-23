package docker

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"

	"github.com/justindeelux/gotham/buildtool"
)

const (
	registryImage         = "registry:2"
	registryContainerName = "gotham-registry"
	registryNetworkName   = "gotham-registry"
	registryVolumeName    = "gotham-registry-data"
	registryPort          = "5000"
	registryHostIP        = "127.0.0.1"
	registryPreferredPort = 5000
	registryPortRange     = 32
	registryAuthHeader    = "X-Registry-Auth"
	anonymousRegistryAuth = "e30="
	maxDockerStreamLine   = 1 << 20
)

var (
	errContainerNotFound = errors.New("docker: container not found")
	errContainerExists   = errors.New("docker: container already exists")
)

type BuildOptions struct {
	Tag        string
	Dockerfile string
	BuildArgs  map[string]string
	Context    io.Reader
}

func (c *DockerClient) Build(ctx context.Context, opts BuildOptions, emit func([]byte) error) error {
	if strings.TrimSpace(opts.Tag) == "" {
		return errors.New("docker: build tag is required")
	}
	if opts.Context == nil {
		return errors.New("docker: build context is required")
	}

	dockerfile := strings.TrimSpace(opts.Dockerfile)
	if dockerfile == "" {
		dockerfile = "Dockerfile"
	}

	query := url.Values{}
	query.Set("t", opts.Tag)
	query.Set("dockerfile", dockerfile)
	query.Set("rm", "1")
	query.Set("forcerm", "1")
	if len(opts.BuildArgs) > 0 {
		encoded, err := json.Marshal(opts.BuildArgs)
		if err != nil {
			return fmt.Errorf("docker: marshal build args: %w", err)
		}
		query.Set("buildargs", string(encoded))
	}

	response, err := c.doHeader(ctx, http.MethodPost, "/build?"+query.Encode(), opts.Context, "application/x-tar")
	if err != nil {
		return err
	}
	defer func() { _ = response.Body.Close() }()

	return streamDockerMessages("build "+opts.Tag, response.Body, emit)
}

type emitWriter func([]byte) error

func (w emitWriter) Write(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	if err := w(p); err != nil {
		return 0, err
	}
	return len(p), nil
}

func (c *DockerClient) RunToolchain(ctx context.Context, engine string, contextTar io.Reader, tag string, buildArgs map[string]string, emit func([]byte) error) error {
	if contextTar == nil {
		return errors.New("docker: toolchain build context is required")
	}
	if strings.TrimSpace(tag) == "" {
		return errors.New("docker: toolchain image tag is required")
	}
	dir, err := os.MkdirTemp("", "gotham-toolchain-*")
	if err != nil {
		return fmt.Errorf("docker: create toolchain directory: %w", err)
	}
	defer func() { _ = os.RemoveAll(dir) }()

	if err := buildtool.ExtractTar(dir, contextTar); err != nil {
		return err
	}
	options := buildtool.Options{
		Dir:        dir,
		Tag:        tag,
		BuildArgs:  buildArgs,
		DockerHost: c.dockerHost,
		StateDir:   c.registryStateDir,
	}
	if emit != nil {
		options.LogWriter = emitWriter(emit)
	}
	return buildtool.Run(ctx, buildtool.Engine(engine), options)
}

func (c *DockerClient) TagImage(ctx context.Context, source, repository, tag string) error {
	if strings.TrimSpace(source) == "" || strings.TrimSpace(repository) == "" || strings.TrimSpace(tag) == "" {
		return errors.New("docker: tag source, repository and tag are required")
	}
	query := url.Values{}
	query.Set("repo", repository)
	query.Set("tag", tag)

	response, err := c.do(ctx, http.MethodPost, "/images/"+source+"/tag?"+query.Encode(), nil)
	if err != nil {
		return err
	}
	defer func() { _ = response.Body.Close() }()
	_, _ = io.Copy(io.Discard, response.Body)
	return nil
}

func (c *DockerClient) PushImage(ctx context.Context, repository, tag string, emit func([]byte) error) error {
	if strings.TrimSpace(repository) == "" || strings.TrimSpace(tag) == "" {
		return errors.New("docker: push repository and tag are required")
	}
	query := url.Values{}
	query.Set("tag", tag)

	response, err := c.doRegistry(ctx, "/images/"+repository+"/push?"+query.Encode())
	if err != nil {
		return err
	}
	defer func() { _ = response.Body.Close() }()

	return streamDockerMessages("push "+repository+":"+tag, response.Body, emit)
}

func (c *DockerClient) ImageDigest(ctx context.Context, ref string) (string, error) {
	response, err := c.do(ctx, http.MethodGet, "/images/"+ref+"/json", nil)
	if err != nil {
		return "", err
	}
	defer func() { _ = response.Body.Close() }()

	var info struct {
		RepoDigests []string `json:"RepoDigests"`
	}
	if err := json.NewDecoder(response.Body).Decode(&info); err != nil {
		return "", fmt.Errorf("docker: decode image inspect: %w", err)
	}

	repository := stripImageTag(ref)
	for _, entry := range info.RepoDigests {
		digest, found := strings.CutPrefix(entry, repository+"@")
		if found && digest != "" {
			return digest, nil
		}
	}
	return "", fmt.Errorf("docker: image %s has no pushed digest", ref)
}

func (c *DockerClient) EnsureRegistry(ctx context.Context) (string, error) {
	c.registryMu.Lock()
	defer c.registryMu.Unlock()

	auth, htpasswdPath, reused, err := prepareRegistryAuth(c.registryStateDir)
	if err != nil {
		return "", err
	}
	if err := c.ensureRegistryNetwork(ctx); err != nil {
		return "", err
	}

	info, err := c.inspectRegistryContainer(ctx)
	switch {
	case errors.Is(err, errContainerNotFound):
		if err := c.bootstrapRegistry(ctx, htpasswdPath); err != nil {
			return "", err
		}
		if info, err = c.inspectRegistryContainer(ctx); err != nil {
			return "", fmt.Errorf("docker: inspect registry container: %w", err)
		}
	case err != nil:
		return "", err
	case !registryManaged(info):
		return "", fmt.Errorf("docker: container %s exists but is not managed by gotham", registryContainerName)
	case !registryBindingExplicit(info) || !registryIsolated(info) ||
		!registryMountsPath(info, htpasswdPath) || !reused:
		if err := c.removeRegistryContainer(ctx, info); err != nil {
			return "", err
		}
		if err := c.bootstrapRegistry(ctx, htpasswdPath); err != nil {
			return "", err
		}
		if info, err = c.inspectRegistryContainer(ctx); err != nil {
			return "", fmt.Errorf("docker: inspect registry container: %w", err)
		}
	}

	if !info.State.Running {
		if err := c.Start(ctx, registryContainerName); err != nil {
			if neverStarted(info) {
				_ = c.removeRegistryContainer(ctx, info)
			}
			return "", fmt.Errorf("docker: start registry container: %w", err)
		}
		if info, err = c.inspectRegistryContainer(ctx); err != nil {
			return "", fmt.Errorf("docker: inspect registry container: %w", err)
		}
	}

	address := registryHostPort(info)
	if address == "" {
		return "", errors.New("docker: registry container has no published port")
	}
	auth.Address = address
	c.mu.Lock()
	c.registryAuth = auth
	c.mu.Unlock()
	return address, nil
}

func (c *DockerClient) ensureRegistryNetwork(ctx context.Context) error {
	exists, err := c.registryNetworkExists(ctx)
	if err != nil {
		return err
	}
	if exists {
		return nil
	}

	body := map[string]any{
		"Name":           registryNetworkName,
		"Driver":         "bridge",
		"CheckDuplicate": true,
		"Labels": map[string]string{
			"gotham.managed": "true",
			"gotham.role":    "registry",
		},
	}
	encoded, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("docker: marshal registry network body: %w", err)
	}
	createPath := "/networks/create"
	response, err := c.doRaw(ctx, http.MethodPost, createPath, bytes.NewReader(encoded), "application/json")
	if err != nil {
		return fmt.Errorf("docker: create registry network: %w", err)
	}
	if response.StatusCode == http.StatusConflict {
		_, _ = io.Copy(io.Discard, response.Body)
		_ = response.Body.Close()
		if _, err := c.registryNetworkExists(ctx); err != nil {
			return err
		}
		return nil
	}
	defer func() { _ = response.Body.Close() }()
	if !dockerOK(response.StatusCode) {
		return statusError(http.MethodPost, createPath, response)
	}
	_, _ = io.Copy(io.Discard, response.Body)
	return nil
}

func (c *DockerClient) registryNetworkExists(ctx context.Context) (bool, error) {
	path := "/networks/" + registryNetworkName
	response, err := c.doRaw(ctx, http.MethodGet, path, nil, "")
	if err != nil {
		return false, fmt.Errorf("docker: inspect registry network: %w", err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode == http.StatusNotFound {
		_, _ = io.Copy(io.Discard, response.Body)
		return false, nil
	}
	if !dockerOK(response.StatusCode) {
		return false, statusError(http.MethodGet, path, response)
	}
	var network struct {
		Labels map[string]string `json:"Labels"`
	}
	if err := json.NewDecoder(response.Body).Decode(&network); err != nil {
		return false, fmt.Errorf("docker: decode registry network: %w", err)
	}
	if network.Labels["gotham.managed"] != "true" {
		return true, fmt.Errorf("docker: network %s exists but is not managed by gotham", registryNetworkName)
	}
	return true, nil
}

func (c *DockerClient) bootstrapRegistry(ctx context.Context, htpasswdPath string) error {
	port, err := pickRegistryPort()
	if err != nil {
		return err
	}
	if _, err := c.PullImage(ctx, registryImage, "", ""); err != nil {
		return fmt.Errorf("docker: pull %s: %w", registryImage, err)
	}
	if err := c.createRegistryContainer(ctx, port, htpasswdPath); err != nil {
		if errors.Is(err, errContainerExists) {
			return nil
		}
		return err
	}
	return nil
}

func pickRegistryPort() (string, error) {
	for port := registryPreferredPort; port < registryPreferredPort+registryPortRange; port++ {
		address := net.JoinHostPort(registryHostIP, strconv.Itoa(port))
		listener, err := net.Listen("tcp", address)
		if err != nil {
			continue
		}
		if err := listener.Close(); err != nil {
			return "", fmt.Errorf("docker: release registry port %s: %w", address, err)
		}
		return strconv.Itoa(port), nil
	}
	return "", fmt.Errorf("docker: no free registry port in %d-%d",
		registryPreferredPort, registryPreferredPort+registryPortRange-1)
}

func (c *DockerClient) inspectRegistryContainer(ctx context.Context) (*containerRuntimeInfo, error) {
	path := "/containers/" + registryContainerName + "/json"
	response, err := c.doRaw(ctx, http.MethodGet, path, nil, "")
	if err != nil {
		return nil, fmt.Errorf("docker: inspect container: %w", err)
	}
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode == http.StatusNotFound {
		return nil, errContainerNotFound
	}
	if !dockerOK(response.StatusCode) {
		return nil, statusError(http.MethodGet, path, response)
	}

	var info containerRuntimeInfo
	if err := json.NewDecoder(response.Body).Decode(&info); err != nil {
		return nil, fmt.Errorf("docker: decode container inspect: %w", err)
	}
	return &info, nil
}

func (c *DockerClient) createRegistryContainer(ctx context.Context, port, htpasswdPath string) error {
	body := dockerCreateBody{
		Image: registryImage,
		Env: []string{
			"REGISTRY_AUTH=htpasswd",
			"REGISTRY_AUTH_HTPASSWD_REALM=" + registryAuthRealm,
			"REGISTRY_AUTH_HTPASSWD_PATH=" + registryHtpasswdPath,
		},
		Labels: map[string]string{
			"gotham.managed": "true",
			"gotham.role":    "registry",
		},
		ExposedPorts: map[string]struct{}{registryPort + "/tcp": {}},
		HostConfig: &dockerHostConfig{
			Binds: []string{
				registryVolumeName + ":/var/lib/registry",
				htpasswdPath + ":" + registryHtpasswdPath + ":ro",
			},
			NetworkMode: registryNetworkName,
			PortBindings: map[string][]dockerPort{
				registryPort + "/tcp": {{HostIP: registryHostIP, HostPort: port}},
			},
			RestartPolicy: &dockerRestartPolicy{Name: "unless-stopped"},
		},
	}
	encoded, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("docker: marshal registry create body: %w", err)
	}

	path := "/containers/create?name=" + url.QueryEscape(registryContainerName)
	response, err := c.doRaw(ctx, http.MethodPost, path, bytes.NewReader(encoded), "application/json")
	if err != nil {
		return fmt.Errorf("docker: create registry container: %w", err)
	}
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode == http.StatusConflict {
		_, _ = io.Copy(io.Discard, response.Body)
		return errContainerExists
	}
	if !dockerOK(response.StatusCode) {
		return statusError(http.MethodPost, path, response)
	}
	_, _ = io.Copy(io.Discard, response.Body)
	return nil
}

func (c *DockerClient) removeRegistryContainer(ctx context.Context, info *containerRuntimeInfo) error {
	if !registryManaged(info) {
		return fmt.Errorf("docker: container %s exists but is not managed by gotham", registryContainerName)
	}
	path := "/containers/" + registryContainerName + "?force=1"
	response, err := c.doRaw(ctx, http.MethodDelete, path, nil, "")
	if err != nil {
		return fmt.Errorf("docker: remove registry container: %w", err)
	}
	defer func() { _ = response.Body.Close() }()

	if !dockerOK(response.StatusCode) {
		return statusError(http.MethodDelete, path, response)
	}
	_, _ = io.Copy(io.Discard, response.Body)
	return nil
}

type containerRuntimeInfo struct {
	State struct {
		Running   bool   `json:"Running"`
		StartedAt string `json:"StartedAt"`
	} `json:"State"`
	Config struct {
		Labels map[string]string `json:"Labels"`
	} `json:"Config"`
	HostConfig struct {
		PortBindings map[string][]dockerPortBinding `json:"PortBindings"`
		NetworkMode  string                         `json:"NetworkMode"`
	} `json:"HostConfig"`
	NetworkSettings struct {
		Ports    map[string][]dockerPortBinding `json:"Ports"`
		Networks map[string]struct{}            `json:"Networks"`
	} `json:"NetworkSettings"`
	Mounts []dockerMountBinding `json:"Mounts"`
}

type dockerMountBinding struct {
	Source      string `json:"Source"`
	Destination string `json:"Destination"`
}

type dockerPortBinding struct {
	HostIP   string `json:"HostIp"`
	HostPort string `json:"HostPort"`
}

func registryManaged(info *containerRuntimeInfo) bool {
	return info.Config.Labels["gotham.managed"] == "true"
}

func registryBindingExplicit(info *containerRuntimeInfo) bool {
	bindings := info.HostConfig.PortBindings[registryPort+"/tcp"]
	return len(bindings) > 0 && bindings[0].HostIP == registryHostIP && bindings[0].HostPort != ""
}

func registryMountsPath(info *containerRuntimeInfo, want string) bool {
	for _, mount := range info.Mounts {
		if mount.Destination == registryHtpasswdPath && mount.Source == want {
			return true
		}
	}
	return false
}

func registryIsolated(info *containerRuntimeInfo) bool {
	if info.HostConfig.NetworkMode == registryNetworkName {
		return true
	}
	if len(info.NetworkSettings.Networks) != 1 {
		return false
	}
	_, ok := info.NetworkSettings.Networks[registryNetworkName]
	return ok
}

func neverStarted(info *containerRuntimeInfo) bool {
	return info.State.StartedAt == "" || strings.HasPrefix(info.State.StartedAt, "0000-00-00")
}

func registryHostPort(info *containerRuntimeInfo) string {
	for _, binding := range info.NetworkSettings.Ports[registryPort+"/tcp"] {
		if binding.HostPort == "" {
			continue
		}
		host := binding.HostIP
		if host == "" || host == "0.0.0.0" {
			host = registryHostIP
		}
		return net.JoinHostPort(host, binding.HostPort)
	}
	return ""
}

func stripImageTag(ref string) string {
	tagAt := strings.LastIndex(ref, ":")
	if tagAt > strings.LastIndex(ref, "/") {
		return ref[:tagAt]
	}
	return ref
}

type dockerStreamMessage struct {
	Stream string `json:"stream"`
	Status string `json:"status"`
	Error  string `json:"error"`
}

func streamDockerMessages(verb string, body io.Reader, emit func([]byte) error) error {
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 64<<10), maxDockerStreamLine)

	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		var message dockerStreamMessage
		if err := json.Unmarshal(line, &message); err != nil {
			if err := emit(append(append([]byte(nil), line...), '\n')); err != nil {
				return err
			}
			continue
		}
		if message.Error != "" {
			return fmt.Errorf("docker: %s: %s", verb, message.Error)
		}
		text := message.Stream
		if text == "" {
			text = message.Status
		}
		if text == "" {
			continue
		}
		if !strings.HasSuffix(text, "\n") {
			text += "\n"
		}
		if err := emit([]byte(text)); err != nil {
			return err
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("docker: read %s output: %w", verb, err)
	}
	return nil
}
