package servers

import (
	"context"

	"github.com/google/uuid"
	"google.golang.org/grpc"

	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
)

// FirewallClient is the control plane's handle on one node agent's
// FirewallService. The firewall state stays on the node: the control plane
// only dials it.
type FirewallClient struct {
	agentv1.FirewallServiceClient
	conn *grpc.ClientConn
}

func (c *FirewallClient) Close() error {
	if c == nil || c.conn == nil {
		return nil
	}
	return c.conn.Close()
}

func (s *ServerService) DialFirewallClient(ctx context.Context, id uuid.UUID, opts ...DockerDialOption) (*FirewallClient, error) {
	target, serverName, err := s.AgentTarget(ctx, id)
	if err != nil {
		return nil, err
	}
	conn, err := s.dialResolved(ctx, target, serverName, opts)
	if err != nil {
		return nil, err
	}
	return &FirewallClient{FirewallServiceClient: agentv1.NewFirewallServiceClient(conn), conn: conn}, nil
}
