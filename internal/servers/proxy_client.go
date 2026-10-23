package servers

import (
	"context"

	"github.com/google/uuid"
	"google.golang.org/grpc"

	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
)

type ProxyClient struct {
	agentv1.ProxyServiceClient
	conn *grpc.ClientConn
}

var _ agentv1.ProxyServiceClient = (*ProxyClient)(nil)

func (c *ProxyClient) Close() error {
	if c == nil || c.conn == nil {
		return nil
	}
	return c.conn.Close()
}

func (c *ProxyClient) Conn() *grpc.ClientConn {
	if c == nil {
		return nil
	}
	return c.conn
}

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
