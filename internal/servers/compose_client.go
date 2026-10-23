package servers

import (
	"context"

	"github.com/google/uuid"
	"google.golang.org/grpc"

	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
)

type ComposeClient struct {
	agentv1.ComposeServiceClient
	conn *grpc.ClientConn
}

var _ agentv1.ComposeServiceClient = (*ComposeClient)(nil)

func (c *ComposeClient) Close() error {
	if c == nil || c.conn == nil {
		return nil
	}
	return c.conn.Close()
}

func (c *ComposeClient) Conn() *grpc.ClientConn {
	if c == nil {
		return nil
	}
	return c.conn
}

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
