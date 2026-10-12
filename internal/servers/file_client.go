package servers

import (
	"context"

	"github.com/google/uuid"
	"google.golang.org/grpc"

	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
)

// fileMessageBytes bounds a single gRPC message on a FileService connection.
// Read chunks are 256 KiB and the control plane never sends a body through this
// dialer (the upload path reuses the plain HTTP handler, not this connection),
// so 4 MiB is the generated default made explicit: a future chunk-size change
// cannot silently exceed an implicit limit.
const fileMessageBytes = 4 << 20

// FileClient is a FileService client bound to its connection. It embeds
// agentv1.FileServiceClient, so every read RPC is exposed directly. Close
// releases the connection when the caller is done; Conn exposes the raw
// connection for health checks.
type FileClient struct {
	agentv1.FileServiceClient
	conn *grpc.ClientConn
}

// Compile-time guarantee that FileClient exposes the full FileService surface.
var _ agentv1.FileServiceClient = (*FileClient)(nil)

// Close releases the underlying gRPC connection. It is safe to call twice;
// the second call returns the connection's close error.
func (c *FileClient) Close() error {
	if c == nil || c.conn == nil {
		return nil
	}
	return c.conn.Close()
}

// Conn returns the underlying gRPC connection.
func (c *FileClient) Conn() *grpc.ClientConn {
	if c == nil {
		return nil
	}
	return c.conn
}

// fileDialOptions makes the FileService message-size limit explicit. The
// options are appended last so a caller's override wins when one is supplied,
// matching the proxy dialer.
func fileDialOptions() []DockerDialOption {
	return []DockerDialOption{WithDockerDialOptions(
		grpc.WithDefaultCallOptions(
			grpc.MaxCallRecvMsgSize(fileMessageBytes),
			grpc.MaxCallSendMsgSize(fileMessageBytes),
		),
	)}
}

// DialFileClient dials the agent FileService at target (host or host:port) over
// the shared mTLS transport: the same Phase 2 authority issues the ephemeral
// client certificate and verifies the agent's server certificate, and the same
// target normalization applies. A nil authority selects insecure transport for
// local development only. The caller owns the returned client and must Close it.
func DialFileClient(ctx context.Context, target string, authority *Authority, opts ...DockerDialOption) (*FileClient, error) {
	conn, err := dialAgentConn(ctx, target, authority, append(opts, fileDialOptions()...)...)
	if err != nil {
		return nil, err
	}
	return &FileClient{
		FileServiceClient: agentv1.NewFileServiceClient(conn),
		conn:              conn,
	}, nil
}

// DialFileClient dials the agent FileService for the server id, resolving the
// target and server name from the registry. An explicit WithDockerServerName
// option wins over the resolved name. The caller must Close the returned client.
func (s *ServerService) DialFileClient(ctx context.Context, id uuid.UUID, opts ...DockerDialOption) (*FileClient, error) {
	target, serverName, err := s.AgentTarget(ctx, id)
	if err != nil {
		return nil, err
	}
	conn, err := s.dialResolved(ctx, target, serverName, append(opts, fileDialOptions()...))
	if err != nil {
		return nil, err
	}
	return &FileClient{
		FileServiceClient: agentv1.NewFileServiceClient(conn),
		conn:              conn,
	}, nil
}
