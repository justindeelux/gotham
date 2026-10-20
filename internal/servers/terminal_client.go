package servers

import (
	"context"

	"github.com/google/uuid"
	"google.golang.org/grpc"

	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
)

// TerminalClient is a TerminalService client bound to its connection. It embeds
// agentv1.TerminalServiceClient, so the streaming RPC is exposed directly.
// Close releases the connection when the caller is done; Conn exposes the raw
// connection for health checks.
type TerminalClient struct {
	agentv1.TerminalServiceClient
	conn *grpc.ClientConn
}

// Compile-time guarantee that TerminalClient exposes the full TerminalService
// surface.
var _ agentv1.TerminalServiceClient = (*TerminalClient)(nil)

// Close releases the underlying gRPC connection. It is safe to call twice; the
// second call returns the connection's close error.
func (c *TerminalClient) Close() error {
	if c == nil || c.conn == nil {
		return nil
	}
	return c.conn.Close()
}

// Conn returns the underlying gRPC connection.
func (c *TerminalClient) Conn() *grpc.ClientConn {
	if c == nil {
		return nil
	}
	return c.conn
}

// DialTerminalClient dials the agent TerminalService at target (host or
// host:port) over the shared mTLS transport. A nil authority selects insecure
// transport for local development only. The caller owns the returned client and
// must Close it when the stream ends.
func DialTerminalClient(ctx context.Context, target string, authority *Authority, opts ...DockerDialOption) (*TerminalClient, error) {
	conn, err := dialAgentConn(ctx, target, authority, opts...)
	if err != nil {
		return nil, err
	}
	return &TerminalClient{
		TerminalServiceClient: agentv1.NewTerminalServiceClient(conn),
		conn:                  conn,
	}, nil
}

// DialTerminalClient dials the agent TerminalService for the server id,
// resolving the target and server name from the registry. An explicit
// WithDockerServerName option wins over the resolved name. The caller must
// Close the returned client.
func (s *ServerService) DialTerminalClient(ctx context.Context, id uuid.UUID, opts ...DockerDialOption) (*TerminalClient, error) {
	target, serverName, err := s.AgentTarget(ctx, id)
	if err != nil {
		return nil, err
	}
	conn, err := s.dialResolved(ctx, target, serverName, opts)
	if err != nil {
		return nil, err
	}
	return &TerminalClient{
		TerminalServiceClient: agentv1.NewTerminalServiceClient(conn),
		conn:                  conn,
	}, nil
}
