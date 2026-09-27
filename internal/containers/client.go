package containers

import (
	"context"

	"github.com/google/uuid"
	"google.golang.org/grpc"

	"github.com/justindeelux/gotham/internal/servers"
	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
)

// DockerClient is the subset of the agent DockerService contract the control
// plane uses. It mirrors agentv1.DockerServiceClient exactly so the generated
// client plugs in directly once the mTLS dial lands:
//
//	client, err := agentv1.NewDockerServiceClient(conn)
//	svc.SetDial(func(context.Context, *servers.Server) (containers.DockerClient, error) {
//		return client, err
//	})
//
// Declaring the interface locally (instead of depending on the sibling P3-CONN
// package) keeps this package testable with a mock and mergeable in any order.
type DockerClient interface {
	ListContainers(ctx context.Context, in *agentv1.ListContainersRequest, opts ...grpc.CallOption) (*agentv1.ListContainersResponse, error)
	StartContainer(ctx context.Context, in *agentv1.ContainerActionRequest, opts ...grpc.CallOption) (*agentv1.ContainerActionResponse, error)
	StopContainer(ctx context.Context, in *agentv1.ContainerActionRequest, opts ...grpc.CallOption) (*agentv1.ContainerActionResponse, error)
	RestartContainer(ctx context.Context, in *agentv1.ContainerActionRequest, opts ...grpc.CallOption) (*agentv1.ContainerActionResponse, error)
	PullImage(ctx context.Context, in *agentv1.PullImageRequest, opts ...grpc.CallOption) (*agentv1.PullImageResponse, error)
	CreateContainer(ctx context.Context, in *agentv1.CreateContainerRequest, opts ...grpc.CallOption) (*agentv1.ContainerActionResponse, error)
	RunImage(ctx context.Context, in *agentv1.CreateContainerRequest, opts ...grpc.CallOption) (*agentv1.ContainerActionResponse, error)
	StreamLogs(ctx context.Context, in *agentv1.StreamLogsRequest, opts ...grpc.CallOption) (grpc.ServerStreamingClient[agentv1.LogChunk], error)
}

// Compile-time assertion that the generated client satisfies the local
// interface, so the real client plugs in at merge time without adaptation.
var _ DockerClient = agentv1.DockerServiceClient(nil)

// DialFunc opens a DockerClient for the agent on the given server. The real
// mTLS implementation is provided by the sibling P3-CONN package; until it is
// wired via Config.Dial or Service.SetDial, operations fail with
// ErrAgentUnavailable.
type DialFunc func(ctx context.Context, server *servers.Server) (DockerClient, error)

// Registry resolves a managed server so operations route to the right agent.
// It is satisfied by *servers.ServerService and by the server package's
// ServerService interface.
type Registry interface {
	Get(ctx context.Context, id uuid.UUID) (*servers.Server, error)
}
