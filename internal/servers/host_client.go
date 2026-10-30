package servers

import (
	"context"

	"github.com/google/uuid"
	"google.golang.org/grpc"

	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
)

// HostClient is the control plane's handle on one node agent's HostService.
// The agent runs the same root-owned helper as the local node, so the control
// plane keeps one host-settings model for every node.
type HostClient struct {
	agentv1.HostServiceClient
	conn *grpc.ClientConn
}

func (c *HostClient) Close() error {
	if c == nil || c.conn == nil {
		return nil
	}
	return c.conn.Close()
}

func (s *ServerService) DialHostClient(ctx context.Context, id uuid.UUID, opts ...DockerDialOption) (*HostClient, error) {
	target, serverName, err := s.AgentTarget(ctx, id)
	if err != nil {
		return nil, err
	}
	conn, err := s.dialResolved(ctx, target, serverName, opts)
	if err != nil {
		return nil, err
	}
	return &HostClient{HostServiceClient: agentv1.NewHostServiceClient(conn), conn: conn}, nil
}
