package files

import (
	"context"

	"github.com/google/uuid"
	"google.golang.org/grpc"

	"github.com/justindeelux/gotham/internal/servers"
	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
)

type FileClient interface {
	ListRoots(ctx context.Context, in *agentv1.ListRootsRequest, opts ...grpc.CallOption) (*agentv1.ListRootsResponse, error)
	List(ctx context.Context, in *agentv1.ListRequest, opts ...grpc.CallOption) (*agentv1.ListResponse, error)
	Stat(ctx context.Context, in *agentv1.StatRequest, opts ...grpc.CallOption) (*agentv1.StatResponse, error)
	Search(ctx context.Context, in *agentv1.SearchRequest, opts ...grpc.CallOption) (*agentv1.SearchResponse, error)
	Read(ctx context.Context, in *agentv1.ReadRequest, opts ...grpc.CallOption) (grpc.ServerStreamingClient[agentv1.ReadChunk], error)
	DirectorySize(ctx context.Context, in *agentv1.DirectorySizeRequest, opts ...grpc.CallOption) (*agentv1.DirectorySizeResponse, error)
	Write(ctx context.Context, opts ...grpc.CallOption) (grpc.ClientStreamingClient[agentv1.WriteRequest, agentv1.WriteResponse], error)
	Copy(ctx context.Context, in *agentv1.CopyRequest, opts ...grpc.CallOption) (grpc.ServerStreamingClient[agentv1.Progress], error)
	Delete(ctx context.Context, in *agentv1.DeleteRequest, opts ...grpc.CallOption) (grpc.ServerStreamingClient[agentv1.Progress], error)
	Mkdir(ctx context.Context, in *agentv1.MkdirRequest, opts ...grpc.CallOption) (*agentv1.MkdirResponse, error)
	Rename(ctx context.Context, in *agentv1.RenameRequest, opts ...grpc.CallOption) (*agentv1.RenameResponse, error)
	Chmod(ctx context.Context, in *agentv1.ChmodRequest, opts ...grpc.CallOption) (*agentv1.ChmodResponse, error)
	Compress(ctx context.Context, in *agentv1.CompressRequest, opts ...grpc.CallOption) (grpc.ServerStreamingClient[agentv1.Progress], error)
	Extract(ctx context.Context, in *agentv1.ExtractRequest, opts ...grpc.CallOption) (grpc.ServerStreamingClient[agentv1.Progress], error)
	ListTrash(ctx context.Context, in *agentv1.ListTrashRequest, opts ...grpc.CallOption) (*agentv1.ListTrashResponse, error)
	Restore(ctx context.Context, in *agentv1.RestoreRequest, opts ...grpc.CallOption) (*agentv1.RestoreResponse, error)
	Purge(ctx context.Context, in *agentv1.PurgeRequest, opts ...grpc.CallOption) (*agentv1.PurgeResponse, error)
	EmptyTrash(ctx context.Context, in *agentv1.EmptyTrashRequest, opts ...grpc.CallOption) (*agentv1.EmptyTrashResponse, error)
	TransferCapabilities(ctx context.Context, in *agentv1.TransferCapabilitiesRequest, opts ...grpc.CallOption) (*agentv1.TransferCapabilitiesResponse, error)
	PrepareBundle(ctx context.Context, in *agentv1.PrepareBundleRequest, opts ...grpc.CallOption) (*agentv1.PrepareBundleResponse, error)
	ReadArtifact(ctx context.Context, in *agentv1.ReadArtifactRequest, opts ...grpc.CallOption) (grpc.ServerStreamingClient[agentv1.ArtifactChunk], error)
	ReceiveArtifact(ctx context.Context, opts ...grpc.CallOption) (grpc.ClientStreamingClient[agentv1.ReceiveArtifactRequest, agentv1.ReceiveArtifactResponse], error)
	ApplyArtifact(ctx context.Context, in *agentv1.ApplyArtifactRequest, opts ...grpc.CallOption) (grpc.ServerStreamingClient[agentv1.ApplyArtifactEvent], error)
	DiscardArtifact(ctx context.Context, in *agentv1.DiscardArtifactRequest, opts ...grpc.CallOption) (*agentv1.DiscardArtifactResponse, error)
}

var _ FileClient = agentv1.FileServiceClient(nil)

type DialFunc func(ctx context.Context, server *servers.Server) (FileClient, error)

type Registry interface {
	Get(ctx context.Context, id uuid.UUID) (*servers.Server, error)
}
