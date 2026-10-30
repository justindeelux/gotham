package containers

import (
	"context"

	"github.com/google/uuid"
	"google.golang.org/grpc"

	"github.com/justindeelux/gotham/internal/servers"
	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
)

type DockerClient interface {
	ListContainers(ctx context.Context, in *agentv1.ListContainersRequest, opts ...grpc.CallOption) (*agentv1.ListContainersResponse, error)
	ListVolumes(ctx context.Context, in *agentv1.ListVolumesRequest, opts ...grpc.CallOption) (*agentv1.ListVolumesResponse, error)
	StartContainer(ctx context.Context, in *agentv1.ContainerActionRequest, opts ...grpc.CallOption) (*agentv1.ContainerActionResponse, error)
	StopContainer(ctx context.Context, in *agentv1.ContainerActionRequest, opts ...grpc.CallOption) (*agentv1.ContainerActionResponse, error)
	RestartContainer(ctx context.Context, in *agentv1.ContainerActionRequest, opts ...grpc.CallOption) (*agentv1.ContainerActionResponse, error)
	RemoveContainer(ctx context.Context, in *agentv1.ContainerActionRequest, opts ...grpc.CallOption) (*agentv1.ContainerActionResponse, error)
	RemoveVolume(ctx context.Context, in *agentv1.VolumeActionRequest, opts ...grpc.CallOption) (*agentv1.VolumeActionResponse, error)
	PullImage(ctx context.Context, in *agentv1.PullImageRequest, opts ...grpc.CallOption) (*agentv1.PullImageResponse, error)
	CreateContainer(ctx context.Context, in *agentv1.CreateContainerRequest, opts ...grpc.CallOption) (*agentv1.ContainerActionResponse, error)
	RunImage(ctx context.Context, in *agentv1.CreateContainerRequest, opts ...grpc.CallOption) (*agentv1.ContainerActionResponse, error)
	StreamLogs(ctx context.Context, in *agentv1.StreamLogsRequest, opts ...grpc.CallOption) (grpc.ServerStreamingClient[agentv1.LogChunk], error)
	VolumeArchiveCapabilities(ctx context.Context, in *agentv1.VolumeArchiveCapabilitiesRequest, opts ...grpc.CallOption) (*agentv1.VolumeArchiveCapabilitiesResponse, error)
	ArchiveVolumes(ctx context.Context, in *agentv1.ArchiveVolumesRequest, opts ...grpc.CallOption) (grpc.ServerStreamingClient[agentv1.ArchiveChunk], error)
	RestoreVolumes(ctx context.Context, opts ...grpc.CallOption) (grpc.ClientStreamingClient[agentv1.RestoreVolumesRequest, agentv1.RestoreVolumesResponse], error)
}

var _ DockerClient = agentv1.DockerServiceClient(nil)

type DialFunc func(ctx context.Context, server *servers.Server) (DockerClient, error)

type Registry interface {
	Get(ctx context.Context, id uuid.UUID) (*servers.Server, error)
}
