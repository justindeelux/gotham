package files

import (
	"context"

	"github.com/google/uuid"
	"google.golang.org/grpc"

	"github.com/justindeelux/gotham/internal/servers"
	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
)

// FileClient is the subset of the agent FileService contract the control plane
// uses on the read side. It mirrors agentv1.FileServiceClient for those RPCs,
// so *servers.FileClient plugs in directly at merge time:
//
//	client, err := servers.DialFileClient(ctx, target, authority)
//	svc.SetDial(func(context.Context, *servers.Server) (files.FileClient, error) {
//		return client, err
//	})
//
// Declaring the interface locally keeps this package testable with a mock and
// independently mergeable, exactly like containers.DockerClient. The write
// RPCs (Write, Copy, Delete, Mkdir, Rename, Chmod) join it in step 4.
type FileClient interface {
	ListRoots(ctx context.Context, in *agentv1.ListRootsRequest, opts ...grpc.CallOption) (*agentv1.ListRootsResponse, error)
	List(ctx context.Context, in *agentv1.ListRequest, opts ...grpc.CallOption) (*agentv1.ListResponse, error)
	Stat(ctx context.Context, in *agentv1.StatRequest, opts ...grpc.CallOption) (*agentv1.StatResponse, error)
	Read(ctx context.Context, in *agentv1.ReadRequest, opts ...grpc.CallOption) (grpc.ServerStreamingClient[agentv1.ReadChunk], error)
}

// Compile-time assertion that the generated client satisfies the local
// interface, so the real client plugs in without adaptation.
var _ FileClient = agentv1.FileServiceClient(nil)

// DialFunc opens a FileClient for the agent on the given server. The real mTLS
// implementation is *servers.ServerService.DialFileClient; until it is wired
// via Config.Dial or Service.SetDial, operations fail with ErrAgentUnavailable.
type DialFunc func(ctx context.Context, server *servers.Server) (FileClient, error)

// Registry resolves a managed server so operations route to the right agent.
// It is satisfied by *servers.ServerService and by the server package's
// ServerService interface.
type Registry interface {
	Get(ctx context.Context, id uuid.UUID) (*servers.Server, error)
}
