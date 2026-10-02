package builds

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/justindeelux/gotham/buildtool"
)

// LocalDockerBuilder builds images against a local Docker daemon through the
// Engine API. It is the dev/test ImageBuilder; production builds run on the
// node agent, whose gRPC implementation plugs into the same seam.
type LocalDockerBuilder struct {
	http    *http.Client
	baseURL string
	// dockerHost is the DOCKER_HOST value handed to a toolchain CLI. The
	// toolchain runs with a stripped environment, so it cannot inherit it.
	dockerHost string
}

// Compile-time guarantee that the local builder satisfies the seam.
var _ ImageBuilder = (*LocalDockerBuilder)(nil)

// NewLocalDockerBuilder dials the daemon named by DOCKER_HOST, falling back to
// the default unix socket.
func NewLocalDockerBuilder() (*LocalDockerBuilder, error) {
	return NewLocalDockerBuilderWithHost(os.Getenv("DOCKER_HOST"))
}

// NewLocalDockerBuilderWithHost is NewLocalDockerBuilder with an explicit
// endpoint: "unix:///path", "/path", "tcp://host:port" or "http(s)://host".
func NewLocalDockerBuilderWithHost(host string) (*LocalDockerBuilder, error) {
	transport, baseURL, err := newDockerTransport(host)
	if err != nil {
		return nil, err
	}
	return &LocalDockerBuilder{
		http:       &http.Client{Transport: transport},
		baseURL:    baseURL,
		dockerHost: dockerHostEnv(host),
	}, nil
}

// dockerHostEnv normalises a Docker endpoint into a DOCKER_HOST value for a
// child process. An empty host is left empty so the CLI uses its own default; a
// bare socket path is prefixed with unix://.
func dockerHostEnv(host string) string {
	host = strings.TrimSpace(host)
	switch {
	case host == "":
		return ""
	case strings.HasPrefix(host, "/"):
		return "unix://" + host
	default:
		return host
	}
}

// Build implements ImageBuilder by POSTing the context tarball to the daemon's
// build endpoint and following the returned JSON stream. Logs are captured even
// when the build fails, so the deploy log stream can show the failure reason.
func (b *LocalDockerBuilder) Build(ctx context.Context, contextTar []byte, opts ImageBuildOptions) (ImageBuildResult, error) {
	if len(contextTar) == 0 {
		return ImageBuildResult{}, fmt.Errorf("%w: empty build context", ErrValidation)
	}
	if strings.TrimSpace(opts.Tag) == "" {
		return ImageBuildResult{}, fmt.Errorf("%w: empty image tag", ErrValidation)
	}
	if opts.Engine == EngineRailpack || opts.Engine == EngineBuildpacks {
		return b.buildToolchain(ctx, contextTar, opts)
	}
	dockerfile := opts.Dockerfile
	if strings.TrimSpace(dockerfile) == "" {
		dockerfile = "Dockerfile"
	}

	query := url.Values{}
	query.Set("t", opts.Tag)
	query.Set("dockerfile", dockerfile)
	query.Set("rm", "1")
	// forcerm also removes the intermediate containers of failed steps, so a
	// failed build does not leak a container per attempt.
	query.Set("forcerm", "1")
	if opts.Target != "" {
		query.Set("target", opts.Target)
	}
	if err := setJSONQuery(query, "buildargs", opts.BuildArgs); err != nil {
		return ImageBuildResult{}, err
	}
	if err := setJSONQuery(query, "labels", opts.Labels); err != nil {
		return ImageBuildResult{}, err
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, b.baseURL+"/build?"+query.Encode(), bytes.NewReader(contextTar))
	if err != nil {
		return ImageBuildResult{}, fmt.Errorf("docker build: build request: %w", err)
	}
	request.Header.Set("Content-Type", "application/x-tar")

	response, err := b.http.Do(request)
	if err != nil {
		return ImageBuildResult{}, fmt.Errorf("docker build: %w", err)
	}
	defer func() { _ = response.Body.Close() }()

	stream := decodeBuildStream(response.Body)
	result := ImageBuildResult{Logs: stream.logs}
	switch {
	case stream.err != nil:
		return result, fmt.Errorf("docker build: %w", stream.err)
	case response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices:
		return result, fmt.Errorf("docker build: status %d", response.StatusCode)
	}

	digest, digestErr := b.imageDigest(ctx, opts.Tag)
	if digestErr != nil {
		if stream.digest == "" {
			return result, digestErr
		}
		digest = stream.digest
	}
	result.Digest = digest
	return result, nil
}

// buildToolchain extracts the raw source context to a temporary directory and
// runs the toolchain in this process, then reports the built image's digest.
// It is the dev/test counterpart of the node agent's toolchain build. The
// toolchain runs with a stripped environment, so the builder passes its Docker
// endpoint explicitly as DOCKER_HOST.
func (b *LocalDockerBuilder) buildToolchain(ctx context.Context, contextTar []byte, opts ImageBuildOptions) (ImageBuildResult, error) {
	dir, err := os.MkdirTemp("", "gotham-buildtool-*")
	if err != nil {
		return ImageBuildResult{}, fmt.Errorf("docker build: create toolchain dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(dir) }()

	if err := buildtool.ExtractTar(dir, bytes.NewReader(contextTar)); err != nil {
		return ImageBuildResult{}, err
	}
	var logs bytes.Buffer
	runErr := buildtool.Run(ctx, buildtool.Engine(opts.Engine), buildtool.Options{
		Dir:        dir,
		Tag:        opts.Tag,
		BuildArgs:  opts.BuildArgs,
		LogWriter:  &logs,
		DockerHost: b.dockerHost,
	})
	result := ImageBuildResult{Logs: logs.String()}
	if runErr != nil {
		return result, runErr
	}
	digest, err := b.imageDigest(ctx, opts.Tag)
	if err != nil {
		return result, err
	}
	result.Digest = digest
	return result, nil
}

// imageDigest resolves the built image's content digest, or its image ID when
// the image has not been pushed to a registry.
func (b *LocalDockerBuilder) imageDigest(ctx context.Context, tag string) (string, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, b.baseURL+"/images/"+tag+"/json", nil)
	if err != nil {
		return "", fmt.Errorf("docker build: build inspect request: %w", err)
	}
	response, err := b.http.Do(request)
	if err != nil {
		return "", fmt.Errorf("docker build: inspect %s: %w", tag, err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		message, _ := io.ReadAll(io.LimitReader(response.Body, 4<<10))
		return "", fmt.Errorf("docker build: inspect %s: status %d: %s", tag, response.StatusCode, strings.TrimSpace(string(message)))
	}
	var out struct {
		ID          string   `json:"Id"`
		RepoDigests []string `json:"RepoDigests"`
	}
	if err := json.NewDecoder(response.Body).Decode(&out); err != nil {
		return "", fmt.Errorf("docker build: decode inspect: %w", err)
	}
	if len(out.RepoDigests) > 0 {
		return out.RepoDigests[0], nil
	}
	if out.ID == "" {
		return "", fmt.Errorf("docker build: inspect %s: no image id", tag)
	}
	return out.ID, nil
}

// setJSONQuery encodes a non-empty string map as a JSON query parameter.
func setJSONQuery(query url.Values, key string, values map[string]string) error {
	if len(values) == 0 {
		return nil
	}
	encoded, err := json.Marshal(values)
	if err != nil {
		return fmt.Errorf("docker build: encode %s: %w", key, err)
	}
	query.Set(key, string(encoded))
	return nil
}

// buildStream is the part of the Docker build JSON stream the builder needs.
type buildStream struct {
	logs   string
	digest string
	err    error
}

// decodeBuildStream consumes the newline-delimited build event stream, keeping
// the concatenated log output, the first reported image ID and the first error.
func decodeBuildStream(source io.Reader) buildStream {
	var out buildStream
	var logs strings.Builder
	scanner := bufio.NewScanner(source)
	scanner.Buffer(make([]byte, 0, 64<<10), 4<<20)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var event struct {
			Stream      string `json:"stream"`
			Error       string `json:"error"`
			ErrorDetail *struct {
				Message string `json:"message"`
			} `json:"errorDetail"`
			Aux *struct {
				ID string `json:"ID"`
			} `json:"aux"`
		}
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			logs.WriteString(line)
			logs.WriteByte('\n')
			continue
		}
		logs.WriteString(event.Stream)
		if event.Aux != nil && event.Aux.ID != "" && out.digest == "" {
			out.digest = event.Aux.ID
		}
		if out.err != nil {
			continue
		}
		switch {
		case event.ErrorDetail != nil && event.ErrorDetail.Message != "":
			out.err = errors.New(event.ErrorDetail.Message)
		case event.Error != "":
			out.err = errors.New(event.Error)
		}
	}
	if err := scanner.Err(); err != nil {
		out.logs = logs.String()
		if out.err == nil {
			out.err = fmt.Errorf("read build stream: %w", err)
		}
		return out
	}
	out.logs = logs.String()
	return out
}
