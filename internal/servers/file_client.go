package servers

import (
	"context"

	"github.com/google/uuid"
	"google.golang.org/grpc"

	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
)

const fileMessageBytes = 4 << 20

type FileClient struct {
	agentv1.FileServiceClient
	conn *grpc.ClientConn
}

var _ agentv1.FileServiceClient = (*FileClient)(nil)

func (c *FileClient) Close() error {
	if c == nil || c.conn == nil {
		return nil
	}
	return c.conn.Close()
}

func (c *FileClient) Conn() *grpc.ClientConn {
	if c == nil {
		return nil
	}
	return c.conn
}

func fileDialOptions() []DockerDialOption {
	return []DockerDialOption{WithDockerDialOptions(
		grpc.WithDefaultCallOptions(
			grpc.MaxCallRecvMsgSize(fileMessageBytes),
			grpc.MaxCallSendMsgSize(fileMessageBytes),
		),
	)}
}

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
