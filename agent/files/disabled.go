package files

import (
	"context"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
)

// DisabledReason is the stable detail the control plane matches to tell
// "file manager off on this node" apart from an old agent that does not
// register the file service at all.
const DisabledReason = "file_manager_disabled"

// DisabledFileServer registers the file service while the file manager is
// switched off, so every call answers FailedPrecondition instead of
// Unimplemented.
type DisabledFileServer struct {
	agentv1.UnimplementedFileServiceServer
}

func NewDisabledFileServer() *DisabledFileServer {
	return &DisabledFileServer{}
}

func disabledError() error {
	return status.Error(codes.FailedPrecondition, DisabledReason)
}

func (*DisabledFileServer) ListRoots(context.Context, *agentv1.ListRootsRequest) (*agentv1.ListRootsResponse, error) {
	return nil, disabledError()
}

func (*DisabledFileServer) List(context.Context, *agentv1.ListRequest) (*agentv1.ListResponse, error) {
	return nil, disabledError()
}

func (*DisabledFileServer) Stat(context.Context, *agentv1.StatRequest) (*agentv1.StatResponse, error) {
	return nil, disabledError()
}

func (*DisabledFileServer) Search(context.Context, *agentv1.SearchRequest) (*agentv1.SearchResponse, error) {
	return nil, disabledError()
}

func (*DisabledFileServer) Read(*agentv1.ReadRequest, grpc.ServerStreamingServer[agentv1.ReadChunk]) error {
	return disabledError()
}

func (*DisabledFileServer) DirectorySize(context.Context, *agentv1.DirectorySizeRequest) (*agentv1.DirectorySizeResponse, error) {
	return nil, disabledError()
}

func (*DisabledFileServer) Write(grpc.ClientStreamingServer[agentv1.WriteRequest, agentv1.WriteResponse]) error {
	return disabledError()
}

func (*DisabledFileServer) Copy(*agentv1.CopyRequest, grpc.ServerStreamingServer[agentv1.Progress]) error {
	return disabledError()
}

func (*DisabledFileServer) Delete(*agentv1.DeleteRequest, grpc.ServerStreamingServer[agentv1.Progress]) error {
	return disabledError()
}

func (*DisabledFileServer) Mkdir(context.Context, *agentv1.MkdirRequest) (*agentv1.MkdirResponse, error) {
	return nil, disabledError()
}

func (*DisabledFileServer) Rename(context.Context, *agentv1.RenameRequest) (*agentv1.RenameResponse, error) {
	return nil, disabledError()
}

func (*DisabledFileServer) Chmod(context.Context, *agentv1.ChmodRequest) (*agentv1.ChmodResponse, error) {
	return nil, disabledError()
}

func (*DisabledFileServer) Compress(*agentv1.CompressRequest, grpc.ServerStreamingServer[agentv1.Progress]) error {
	return disabledError()
}

func (*DisabledFileServer) Extract(*agentv1.ExtractRequest, grpc.ServerStreamingServer[agentv1.Progress]) error {
	return disabledError()
}

func (*DisabledFileServer) ListTrash(context.Context, *agentv1.ListTrashRequest) (*agentv1.ListTrashResponse, error) {
	return nil, disabledError()
}

func (*DisabledFileServer) Restore(context.Context, *agentv1.RestoreRequest) (*agentv1.RestoreResponse, error) {
	return nil, disabledError()
}

func (*DisabledFileServer) Purge(context.Context, *agentv1.PurgeRequest) (*agentv1.PurgeResponse, error) {
	return nil, disabledError()
}

func (*DisabledFileServer) EmptyTrash(context.Context, *agentv1.EmptyTrashRequest) (*agentv1.EmptyTrashResponse, error) {
	return nil, disabledError()
}
