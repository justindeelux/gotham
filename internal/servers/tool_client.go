package servers

import (
	"context"

	"github.com/google/uuid"
	"google.golang.org/grpc"

	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
)

type ToolClient struct {
	agentv1.ToolServiceClient
	conn *grpc.ClientConn
}

var _ agentv1.ToolServiceClient = (*ToolClient)(nil)

func (c *ToolClient) Close() error {
	if c == nil || c.conn == nil {
		return nil
	}
	return c.conn.Close()
}

func (c *ToolClient) Conn() *grpc.ClientConn {
	if c == nil {
		return nil
	}
	return c.conn
}

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
