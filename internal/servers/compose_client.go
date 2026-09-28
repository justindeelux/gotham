package servers

import (
	"context"

	"github.com/google/uuid"
	"google.golang.org/grpc"

	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
)

// ComposeClient is a ComposeService client bound to its connection. It embeds
// agentv1.ComposeServiceClient, so the five compose RPCs (including the
// ComposeLogs server stream) are exposed directly, and owns the connection
// lifecycle like DockerClient and ProxyClient. Close releases the connection
// when the caller is done.
type ComposeClient struct {
	agentv1.ComposeServiceClient
	conn *grpc.ClientConn
}

// Compile-time guarantee that ComposeClient exposes the full ComposeService
// surface.
var _ agentv1.ComposeServiceClient = (*ComposeClient)(nil)

// Close releases the underlying gRPC connection. It is safe to call twice; the
// second call returns the connection's close error.
func (c *ComposeClient) Close() error {
	if c == nil || c.conn == nil {
		return nil
	}
	return c.conn.Close()
}

// Conn returns the underlying gRPC connection.
func (c *ComposeClient) Conn() *grpc.ClientConn {
	if c == nil {
		return nil
	}
	return c.conn
}

// DialComposeClient dials the agent ComposeService at target (host or
// host:port) over mTLS. It shares the DockerService transport: the same Phase
// 2 authority issues the ephemeral client certificate and verifies the agent's
// server certificate, and the same target normalization applies. A nil
// authority selects insecure transport for local development only. The caller
// owns the returned client and must Close it.
func DialComposeClient(ctx context.Context, target string, authority *Authority, opts ...DockerDialOption) (*ComposeClient, error) {
	conn, err := dialAgentConn(ctx, target, authority, opts...)
	if err != nil {
		return nil, err
	}
	return &ComposeClient{
		ComposeServiceClient: agentv1.NewComposeServiceClient(conn),
		conn:                 conn,
	}, nil
}

// DialComposeClient dials the agent ComposeService for the server id,
// resolving the target and server name from the registry. An explicit
// WithDockerServerName option wins over the resolved name. The caller must
// Close the returned client.
func (s *ServerService) DialComposeClient(ctx context.Context, id uuid.UUID, opts ...DockerDialOption) (*ComposeClient, error) {
	target, serverName, err := s.AgentTarget(ctx, id)
	if err != nil {
		return nil, err
	}
	conn, err := s.dialResolved(ctx, target, serverName, opts)
	if err != nil {
		return nil, err
	}
	return &ComposeClient{
		ComposeServiceClient: agentv1.NewComposeServiceClient(conn),
		conn:                 conn,
	}, nil
}

// DialComposeClientByNodeID dials the agent ComposeService for the node id.
// The caller must Close the returned client.
func (s *ServerService) DialComposeClientByNodeID(ctx context.Context, nodeID string, opts ...DockerDialOption) (*ComposeClient, error) {
	target, serverName, err := s.AgentTargetByNodeID(ctx, nodeID)
	if err != nil {
		return nil, err
	}
	conn, err := s.dialResolved(ctx, target, serverName, opts)
	if err != nil {
		return nil, err
	}
	return &ComposeClient{
		ComposeServiceClient: agentv1.NewComposeServiceClient(conn),
		conn:                 conn,
	}, nil
}
