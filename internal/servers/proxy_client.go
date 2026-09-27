package servers

import (
	"context"

	"github.com/google/uuid"
	"google.golang.org/grpc"

	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
)

// ProxyClient is a ProxyService client bound to its connection. It embeds
// agentv1.ProxyServiceClient, so WriteProxyConfig is exposed directly, and
// owns the connection lifecycle like DockerClient. Close releases the
// connection when the caller is done; Conn exposes the raw connection for
// health checks.
type ProxyClient struct {
	agentv1.ProxyServiceClient
	conn *grpc.ClientConn
}

// Compile-time guarantee that ProxyClient exposes the full ProxyService
// surface.
var _ agentv1.ProxyServiceClient = (*ProxyClient)(nil)

// Close releases the underlying gRPC connection. It is safe to call twice;
// the second call returns the connection's close error.
func (c *ProxyClient) Close() error {
	if c == nil || c.conn == nil {
		return nil
	}
	return c.conn.Close()
}

// Conn returns the underlying gRPC connection.
func (c *ProxyClient) Conn() *grpc.ClientConn {
	if c == nil {
		return nil
	}
	return c.conn
}

// DialProxyClient dials the agent ProxyService at target (host or host:port)
// over mTLS. It shares the DockerService transport: the same Phase 2
// authority issues the ephemeral client certificate and verifies the agent's
// server certificate, and the same target normalization applies. A nil
// authority selects insecure transport for local development only. The caller
// owns the returned client and must Close it.
func DialProxyClient(ctx context.Context, target string, authority *Authority, opts ...DockerDialOption) (*ProxyClient, error) {
	conn, err := dialAgentConn(ctx, target, authority, opts...)
	if err != nil {
		return nil, err
	}
	return &ProxyClient{
		ProxyServiceClient: agentv1.NewProxyServiceClient(conn),
		conn:               conn,
	}, nil
}

// DialProxyClient dials the agent ProxyService for the server id, resolving
// the target and server name from the registry. An explicit
// WithDockerServerName option wins over the resolved name. The caller must
// Close the returned client.
func (s *ServerService) DialProxyClient(ctx context.Context, id uuid.UUID, opts ...DockerDialOption) (*ProxyClient, error) {
	target, serverName, err := s.AgentTarget(ctx, id)
	if err != nil {
		return nil, err
	}
	conn, err := s.dialResolved(ctx, target, serverName, opts)
	if err != nil {
		return nil, err
	}
	return &ProxyClient{
		ProxyServiceClient: agentv1.NewProxyServiceClient(conn),
		conn:               conn,
	}, nil
}

// DialProxyClientByNodeID dials the agent ProxyService for the node id. The
// caller must Close the returned client.
func (s *ServerService) DialProxyClientByNodeID(ctx context.Context, nodeID string, opts ...DockerDialOption) (*ProxyClient, error) {
	target, serverName, err := s.AgentTargetByNodeID(ctx, nodeID)
	if err != nil {
		return nil, err
	}
	conn, err := s.dialResolved(ctx, target, serverName, opts)
	if err != nil {
		return nil, err
	}
	return &ProxyClient{
		ProxyServiceClient: agentv1.NewProxyServiceClient(conn),
		conn:               conn,
	}, nil
}
