package servers

import (
	"context"

	"github.com/google/uuid"
	"google.golang.org/grpc"

	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
)

// ToolClient is a ToolService client bound to its connection. It embeds
// agentv1.ToolServiceClient, so ListTools, InstallTool and UninstallTool are
// exposed directly. Close releases the connection when the caller is done.
//
// The control plane speaks to the tool executor the same way it speaks to
// DockerService and FileService: over the Phase 2 mTLS dial, never directly.
type ToolClient struct {
	agentv1.ToolServiceClient
	conn *grpc.ClientConn
}

// Compile-time guarantee that ToolClient exposes the full ToolService surface,
// including the InstallTool/UninstallTool server-streaming RPCs.
var _ agentv1.ToolServiceClient = (*ToolClient)(nil)

// Close releases the underlying gRPC connection. It is safe to call twice; the
// second call returns the connection's close error.
func (c *ToolClient) Close() error {
	if c == nil || c.conn == nil {
		return nil
	}
	return c.conn.Close()
}

// Conn returns the underlying gRPC connection, for health or state checks.
func (c *ToolClient) Conn() *grpc.ClientConn {
	if c == nil {
		return nil
	}
	return c.conn
}

// DialToolClient dials the agent ToolService for the server id, resolving the
// target and server name from the registry and presenting the Phase 2 client
// credentials. An explicit WithDockerServerName option wins over the resolved
// name. The caller must Close the returned client.
func (s *ServerService) DialToolClient(ctx context.Context, id uuid.UUID, opts ...DockerDialOption) (*ToolClient, error) {
	target, serverName, err := s.AgentTarget(ctx, id)
	if err != nil {
		return nil, err
	}
	conn, err := s.dialResolved(ctx, target, serverName, opts)
	if err != nil {
		return nil, err
	}
	return &ToolClient{
		ToolServiceClient: agentv1.NewToolServiceClient(conn),
		conn:              conn,
	}, nil
}
