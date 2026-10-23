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

const DefaultManagedVolumeRoot = "/var/lib/gotham/volumes"

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

type DockerServer struct {
	agentv1.UnimplementedDockerServiceServer
	docker          Client
	log             *slog.Logger
	volumeRoot      string
	proxyVolumeRoot string
}

type DockerServerOption func(*DockerServer)

func WithManagedVolumeRoot(root string) DockerServerOption {
	return func(s *DockerServer) {
		if strings.TrimSpace(root) != "" {
			s.volumeRoot = root
		}
	}
}

func WithProxyVolumeRoot(root string) DockerServerOption {
	return func(s *DockerServer) {
		if strings.TrimSpace(root) != "" {
			s.proxyVolumeRoot = root
		}
	}
}

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

func (s *DockerServer) ListContainers(ctx context.Context, req *agentv1.ListContainersRequest) (*agentv1.ListContainersResponse, error) {
	containers, err := s.docker.ListContainers(ctx, req.GetAll(), req.GetLabels(), req.GetResolveVolumeDevices())
	if err != nil {
		return nil, dockerError("list containers", err)
	}
	return &agentv1.ListContainersResponse{Containers: containers}, nil
}

func (s *DockerServer) StartContainer(ctx context.Context, req *agentv1.ContainerActionRequest) (*agentv1.ContainerActionResponse, error) {
	return s.containerAction(ctx, req, "start", s.docker.Start)
}

func (s *DockerServer) StopContainer(ctx context.Context, req *agentv1.ContainerActionRequest) (*agentv1.ContainerActionResponse, error) {
	return s.containerAction(ctx, req, "stop", s.docker.Stop)
}

func (s *DockerServer) RestartContainer(ctx context.Context, req *agentv1.ContainerActionRequest) (*agentv1.ContainerActionResponse, error) {
	return s.containerAction(ctx, req, "restart", s.docker.Restart)
}

func (s *DockerServer) RemoveContainer(ctx context.Context, req *agentv1.ContainerActionRequest) (*agentv1.ContainerActionResponse, error) {
	return s.containerAction(ctx, req, "remove", s.docker.Remove)
}

var dbVolumePattern = regexp.MustCompile(`^gotham-db-[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

func (s *DockerServer) RemoveVolume(ctx context.Context, req *agentv1.VolumeActionRequest) (*agentv1.VolumeActionResponse, error) {
	name := req.GetName()
	if strings.TrimSpace(name) == "" {
		return nil, status.Error(codes.InvalidArgument, "volume name is required")
	}
	if !dbVolumePattern.MatchString(name) {
		return nil, status.Errorf(codes.InvalidArgument, "remove volume: %q is not a managed database volume", name)
	}
	if err := s.docker.RemoveVolume(ctx, name); err != nil {
		return nil, dockerError("remove volume", err)
	}
	return &agentv1.VolumeActionResponse{}, nil
}

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

const (
	maxContainerPidsLimit   = 1 << 20
	maxContainerMemoryBytes = 1 << 40
	maxContainerNanoCPUs    = 1024 * 1_000_000_000
)

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
				return dockerError("stream logs", message.Err)
			}
			if err := stream.Send(&agentv1.LogChunk{Data: message.Data}); err != nil {
				return err
			}
		}
	}
}

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

func dockerError(action string, err error) error {
	switch {
	case errors.Is(err, ErrInvalidPortMapping), errors.Is(err, ErrInvalidVolumeBind):
		return status.Errorf(codes.InvalidArgument, "%s: %v", action, err)
	case errors.Is(err, ErrDockerNotFound):
		return status.Errorf(codes.NotFound, "%s: %v", action, err)
	case errors.Is(err, ErrDockerImageNotFound):
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
