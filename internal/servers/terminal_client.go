package servers

import (
	"context"

	"github.com/google/uuid"
	"google.golang.org/grpc"

	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
)

type TerminalClient struct {
	agentv1.TerminalServiceClient
	conn *grpc.ClientConn
}

var _ agentv1.TerminalServiceClient = (*TerminalClient)(nil)

func (c *TerminalClient) Close() error {
	if c == nil || c.conn == nil {
		return nil
	}
	return c.conn.Close()
}

func (c *TerminalClient) Conn() *grpc.ClientConn {
	if c == nil {
		return nil
	}
	return c.conn
}

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
