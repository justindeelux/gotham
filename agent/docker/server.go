package docker

import (
	"context"
	"errors"
	"log/slog"
	"regexp"
	"strings"

	"github.com/justindeelux/gotham/agent/proxy"
	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// DefaultManagedVolumeRoot is the parent of every application bind mount
// the node accepts. It must match the control plane's
// GOTHAM_MANAGED_VOLUME_ROOT.
const DefaultManagedVolumeRoot = "/var/lib/gotham/volumes"

// Client is the subset of DockerClient the gRPC server needs. It is an
// interface so tests can substitute a fake.
type Client interface {
	Version(ctx context.Context) (string, error)
	ListContainers(ctx context.Context, all bool, labels map[string]string, resolveDevices bool) ([]*agentv1.ContainerInfo, error)
	Start(ctx context.Context, id string) error
	Stop(ctx context.Context, id string) error
	Restart(ctx context.Context, id string) error
	Remove(ctx context.Context, id string) error
	RemoveVolume(ctx context.Context, name string) error
	PullImage(ctx context.Context, image, username, password string) (string, error)
	CreateContainer(ctx context.Context, req *agentv1.CreateContainerRequest) (string, error)
	RunImage(ctx context.Context, req *agentv1.CreateContainerRequest) (string, error)
	Logs(ctx context.Context, id string, follow bool, tail int64) (<-chan LogMessage, error)
}

// DockerServer implements agentv1.DockerServiceServer on top of a Client.
type DockerServer struct {
	agentv1.UnimplementedDockerServiceServer
	docker Client
	log    *slog.Logger
	// volumeRoot is the parent of every application bind mount the node
	// accepts; a request carrying an application bind outside it is refused.
	volumeRoot string
	// proxyVolumeRoot is the node's own Traefik directory: a proxy container
	// may mount it (the config and ACME directories), unlike an application.
	proxyVolumeRoot string
}

// DockerServerOption tunes a DockerServer.
type DockerServerOption func(*DockerServer)

// WithManagedVolumeRoot confines every application bind DockerServer accepts
// to <root>/<app id>. Pass the configured Config.ManagedVolumeRoot.
func WithManagedVolumeRoot(root string) DockerServerOption {
	return func(s *DockerServer) {
		if strings.TrimSpace(root) != "" {
			s.volumeRoot = root
		}
	}
}

// WithProxyVolumeRoot overrides the directory a proxy container may mount. It
// exists for embedded/test harnesses that run the proxy out of a temporary
// root; the production agent never passes it, so the default stays the fixed
// constant that matches internal/proxy.TraefikDir and no environment or config
// value can widen the allowlist.
func WithProxyVolumeRoot(root string) DockerServerOption {
	return func(s *DockerServer) {
		if strings.TrimSpace(root) != "" {
			s.proxyVolumeRoot = root
		}
	}
}

// NewDockerServer returns a DockerService implementation backed by docker.
func NewDockerServer(docker Client, log *slog.Logger, options ...DockerServerOption) *DockerServer {
	if log == nil {
		log = slog.Default()
	}
	server := &DockerServer{
		docker:          docker,
		log:             log,
		volumeRoot:      DefaultManagedVolumeRoot,
		proxyVolumeRoot: proxy.DefaultRoot,
	}
	for _, option := range options {
		option(server)
	}
	return server
}

// ListContainers returns the node's containers.
func (s *DockerServer) ListContainers(ctx context.Context, req *agentv1.ListContainersRequest) (*agentv1.ListContainersResponse, error) {
	containers, err := s.docker.ListContainers(ctx, req.GetAll(), req.GetLabels(), req.GetResolveVolumeDevices())
	if err != nil {
		return nil, dockerError("list containers", err)
	}
	return &agentv1.ListContainersResponse{Containers: containers}, nil
}

// StartContainer starts a container.
func (s *DockerServer) StartContainer(ctx context.Context, req *agentv1.ContainerActionRequest) (*agentv1.ContainerActionResponse, error) {
	return s.containerAction(ctx, req, "start", s.docker.Start)
}

// StopContainer stops a container.
func (s *DockerServer) StopContainer(ctx context.Context, req *agentv1.ContainerActionRequest) (*agentv1.ContainerActionResponse, error) {
	return s.containerAction(ctx, req, "stop", s.docker.Stop)
}

// RestartContainer restarts a container.
func (s *DockerServer) RestartContainer(ctx context.Context, req *agentv1.ContainerActionRequest) (*agentv1.ContainerActionResponse, error) {
	return s.containerAction(ctx, req, "restart", s.docker.Restart)
}

// RemoveContainer deletes a container on the node. Removal is idempotent: a
// container that is already gone is reported as success, so the control plane
// can retry a delete without checking the container first.
func (s *DockerServer) RemoveContainer(ctx context.Context, req *agentv1.ContainerActionRequest) (*agentv1.ContainerActionResponse, error) {
	return s.containerAction(ctx, req, "remove", s.docker.Remove)
}

// dbVolumePattern pins the only named-volume namespace RemoveVolume may touch:
// a database volume is "gotham-db-{database id}". The control plane names it,
// so a compromised control plane cannot use this call as a generic node
// storage deletion primitive.
var dbVolumePattern = regexp.MustCompile(`^gotham-db-[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// RemoveVolume deletes one managed database volume. The name is validated
// against the gotham-db-{uuid} shape before Docker is reached, so the call
// cannot delete an arbitrary volume even if the control plane is compromised.
// Removal is idempotent: a volume that is already gone is success.
func (s *DockerServer) RemoveVolume(ctx context.Context, req *agentv1.VolumeActionRequest) (*agentv1.VolumeActionResponse, error) {
	name := req.GetName()
	if strings.TrimSpace(name) == "" {
		return nil, status.Error(codes.InvalidArgument, "volume name is required")
	}
	// Match the raw name: the pattern is anchored, so a padded or
	// suffix-injected name ("x-gotham-db-…", "…\n") is rejected rather than
	// normalised into something Docker would accept.
	if !dbVolumePattern.MatchString(name) {
		return nil, status.Errorf(codes.InvalidArgument, "remove volume: %q is not a managed database volume", name)
	}
	if err := s.docker.RemoveVolume(ctx, name); err != nil {
		return nil, dockerError("remove volume", err)
	}
	return &agentv1.VolumeActionResponse{}, nil
}

// PullImage pulls an image onto the node. A per-pull registry credential
// travels only in this request: it is turned into the Docker auth header and
// never persisted on the node. It answers the resolved image digest so the
// control plane can record it on the deployment.
func (s *DockerServer) PullImage(ctx context.Context, req *agentv1.PullImageRequest) (*agentv1.PullImageResponse, error) {
	if req.GetImage() == "" {
		return nil, status.Error(codes.InvalidArgument, "image is required")
	}
	digest, err := s.docker.PullImage(ctx, req.GetImage(), req.GetUsername(), req.GetPassword())
	if err != nil {
		return nil, dockerError("pull image", err)
	}
	return &agentv1.PullImageResponse{Digest: digest}, nil
}

// Resource bounds for optional CreateContainerRequest limits. Zero keeps
// Docker's default (the previous behaviour); negative values and values above
// these caps are rejected so a test-only or future caller cannot ask the node
// for an unbounded or nonsensical allocation.
const (
	maxContainerPidsLimit   = 1 << 20
	maxContainerMemoryBytes = 1 << 40
	maxContainerNanoCPUs    = 1024 * 1_000_000_000
)

// validateContainerResources checks the optional resource limits.
func validateContainerResources(req *agentv1.CreateContainerRequest) error {
	if value := req.GetPidsLimit(); value < 0 {
		return status.Error(codes.InvalidArgument, "pids_limit must not be negative")
	} else if value > maxContainerPidsLimit {
		return status.Errorf(codes.InvalidArgument, "pids_limit exceeds %d", maxContainerPidsLimit)
	}
	if value := req.GetMemoryBytes(); value < 0 {
		return status.Error(codes.InvalidArgument, "memory_bytes must not be negative")
	} else if value > maxContainerMemoryBytes {
		return status.Errorf(codes.InvalidArgument, "memory_bytes exceeds %d", maxContainerMemoryBytes)
	}
	if value := req.GetNanoCpus(); value < 0 {
		return status.Error(codes.InvalidArgument, "nano_cpus must not be negative")
	} else if value > maxContainerNanoCPUs {
		return status.Errorf(codes.InvalidArgument, "nano_cpus exceeds %d", maxContainerNanoCPUs)
	}
	return nil
}

// CreateContainer creates a container without starting it.
func (s *DockerServer) CreateContainer(ctx context.Context, req *agentv1.CreateContainerRequest) (*agentv1.ContainerActionResponse, error) {
	if req.GetImage() == "" {
		return nil, status.Error(codes.InvalidArgument, "image is required")
	}
	if err := validateContainerResources(req); err != nil {
		return nil, err
	}
	if err := validateContainerVolumes(s.volumeRoot, s.proxyVolumeRoot, req); err != nil {
		return nil, dockerError("create container", err)
	}
	id, err := s.docker.CreateContainer(ctx, req)
	if err != nil {
		return nil, dockerError("create container", err)
	}
	return &agentv1.ContainerActionResponse{ContainerId: id}, nil
}

// RunImage creates and starts a container.
func (s *DockerServer) RunImage(ctx context.Context, req *agentv1.CreateContainerRequest) (*agentv1.ContainerActionResponse, error) {
	if req.GetImage() == "" {
		return nil, status.Error(codes.InvalidArgument, "image is required")
	}
	if err := validateContainerResources(req); err != nil {
		return nil, err
	}
	if err := validateContainerVolumes(s.volumeRoot, s.proxyVolumeRoot, req); err != nil {
		return nil, dockerError("run image", err)
	}
	id, err := s.docker.RunImage(ctx, req)
	if err != nil {
		return nil, dockerError("run image", err)
	}
	return &agentv1.ContainerActionResponse{ContainerId: id}, nil
}

// StreamLogs streams container log chunks until the container's log stream ends
// or the client cancels the RPC.
func (s *DockerServer) StreamLogs(req *agentv1.StreamLogsRequest, stream grpc.ServerStreamingServer[agentv1.LogChunk]) error {
	if req.GetContainerId() == "" {
		return status.Error(codes.InvalidArgument, "container_id is required")
	}
	ctx := stream.Context()
	chunks, err := s.docker.Logs(ctx, req.GetContainerId(), req.GetFollow(), req.GetTail())
	if err != nil {
		return dockerError("stream logs", err)
	}
	for {
		select {
		case <-ctx.Done():
			return status.FromContextError(ctx.Err()).Err()
		case message, ok := <-chunks:
			if !ok {
				return nil
			}
			if message.Err != nil {
				// A decode error is a failed stream, not a clean end: report
				// it so the control plane never treats a truncated stream as
				// success.
				return dockerError("stream logs", message.Err)
			}
			if err := stream.Send(&agentv1.LogChunk{Data: message.Data}); err != nil {
				return err
			}
		}
	}
}

// containerAction runs a lifecycle method after validating the container id.
func (s *DockerServer) containerAction(
	ctx context.Context,
	req *agentv1.ContainerActionRequest,
	verb string,
	action func(context.Context, string) error,
) (*agentv1.ContainerActionResponse, error) {
	id := req.GetContainerId()
	if id == "" {
		return nil, status.Error(codes.InvalidArgument, "container_id is required")
	}
	if err := action(ctx, id); err != nil {
		return nil, dockerError(verb+" container", err)
	}
	return &agentv1.ContainerActionResponse{ContainerId: id}, nil
}

// dockerError maps a Docker client error onto a gRPC status error. A
// malformed port mapping is invalid input for every caller, not an internal
// failure; a Docker 404 is NotFound and an unreachable daemon is Unavailable,
// so the control plane can answer 404/502 instead of 500.
func dockerError(action string, err error) error {
	switch {
	case errors.Is(err, ErrInvalidPortMapping), errors.Is(err, ErrInvalidVolumeBind):
		return status.Errorf(codes.InvalidArgument, "%s: %v", action, err)
	case errors.Is(err, ErrDockerNotFound):
		return status.Errorf(codes.NotFound, "%s: %v", action, err)
	case errors.Is(err, ErrDockerImageNotFound):
		// A missing image is a bad image reference, not a missing resource:
		// the control plane answers 400 rather than 404.
		return status.Errorf(codes.InvalidArgument, "%s: %v", action, err)
	case errors.Is(err, ErrDockerUnavailable):
		return status.Errorf(codes.Unavailable, "%s: %v", action, err)
	case errors.Is(err, context.Canceled):
		return status.Error(codes.Canceled, action+": context canceled")
	case errors.Is(err, context.DeadlineExceeded):
		return status.Error(codes.DeadlineExceeded, action+": deadline exceeded")
	default:
		return status.Errorf(codes.Internal, "%s: %v", action, err)
	}
}
