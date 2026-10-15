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
// independently mergeable, exactly like containers.DockerClient.
type FileClient interface {
	ListRoots(ctx context.Context, in *agentv1.ListRootsRequest, opts ...grpc.CallOption) (*agentv1.ListRootsResponse, error)
	List(ctx context.Context, in *agentv1.ListRequest, opts ...grpc.CallOption) (*agentv1.ListResponse, error)
	Stat(ctx context.Context, in *agentv1.StatRequest, opts ...grpc.CallOption) (*agentv1.StatResponse, error)
	// Search is a bounded, name-only, non-symlink-following search under one
	// directory.
	Search(ctx context.Context, in *agentv1.SearchRequest, opts ...grpc.CallOption) (*agentv1.SearchResponse, error)
	Read(ctx context.Context, in *agentv1.ReadRequest, opts ...grpc.CallOption) (grpc.ServerStreamingClient[agentv1.ReadChunk], error)
	// DirectorySize returns one folder's recursive size (JUS-162).
	DirectorySize(ctx context.Context, in *agentv1.DirectorySizeRequest, opts ...grpc.CallOption) (*agentv1.DirectorySizeResponse, error)
	// Write is a client stream: the first message is the header (path, size,
	// expect_etag, overwrite, mode), the following messages carry the body in
	// bounded chunks, and Write produces the committed entry.
	Write(ctx context.Context, opts ...grpc.CallOption) (grpc.ClientStreamingClient[agentv1.WriteRequest, agentv1.WriteResponse], error)
	Copy(ctx context.Context, in *agentv1.CopyRequest, opts ...grpc.CallOption) (grpc.ServerStreamingClient[agentv1.Progress], error)
	Delete(ctx context.Context, in *agentv1.DeleteRequest, opts ...grpc.CallOption) (grpc.ServerStreamingClient[agentv1.Progress], error)
	Mkdir(ctx context.Context, in *agentv1.MkdirRequest, opts ...grpc.CallOption) (*agentv1.MkdirResponse, error)
	Rename(ctx context.Context, in *agentv1.RenameRequest, opts ...grpc.CallOption) (*agentv1.RenameResponse, error)
	Chmod(ctx context.Context, in *agentv1.ChmodRequest, opts ...grpc.CallOption) (*agentv1.ChmodResponse, error)
	// Compress and Extract stream archive progress like Copy/Delete.
	Compress(ctx context.Context, in *agentv1.CompressRequest, opts ...grpc.CallOption) (grpc.ServerStreamingClient[agentv1.Progress], error)
	Extract(ctx context.Context, in *agentv1.ExtractRequest, opts ...grpc.CallOption) (grpc.ServerStreamingClient[agentv1.Progress], error)
	// The recycle-bin RPCs: ListTrash reads a root's bin, and Restore, Purge and
	// EmptyTrash mutate it.
	ListTrash(ctx context.Context, in *agentv1.ListTrashRequest, opts ...grpc.CallOption) (*agentv1.ListTrashResponse, error)
	Restore(ctx context.Context, in *agentv1.RestoreRequest, opts ...grpc.CallOption) (*agentv1.RestoreResponse, error)
	Purge(ctx context.Context, in *agentv1.PurgeRequest, opts ...grpc.CallOption) (*agentv1.PurgeResponse, error)
	EmptyTrash(ctx context.Context, in *agentv1.EmptyTrashRequest, opts ...grpc.CallOption) (*agentv1.EmptyTrashResponse, error)
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
