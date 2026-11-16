package server

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/firewall"
	"github.com/justindeelux/gotham/internal/servers"
)

type firewallDialer interface {
	DialFirewallClient(ctx context.Context, id uuid.UUID, opts ...servers.DockerDialOption) (*servers.FirewallClient, error)
}

// firewallService wires the per-node firewall module to the server registry.
// The control-plane port is read from config so lockout protection follows a
// non-default port instead of a constant.
func (s *Server) firewallService() *firewall.Service {
	dialer, ok := s.servers.(firewallDialer)
	if !ok {
		return nil
	}
	var controlPlanePort uint32
	if port := s.cfg.Snapshot().Server.Port; port > 0 {
		controlPlanePort = uint32(port)
	}
	return firewall.NewService(firewall.Config{
		Dial: func(ctx context.Context, serverID uuid.UUID) (firewall.AgentConn, error) {
			client, err := dialer.DialFirewallClient(ctx, serverID)
			if errors.Is(err, servers.ErrNotFound) {
				return nil, firewall.ErrNotFound
			}
			if err != nil {
				return nil, fmt.Errorf("%w: %v", firewall.ErrUnavailable, err)
			}
			return client, nil
		},
		ControlPlanePort: controlPlanePort,
		ProtectControlPlane: func(ctx context.Context, serverID uuid.UUID) bool {
			row, err := s.servers.Get(ctx, serverID)
			if err != nil || row == nil || row.NodeID == nil {
				// Unknown is not proof that the control plane is elsewhere;
				// keeping the extra allow rule is the harmless direction.
				return true
			}
			return *row.NodeID == "localhost"
		},
		Logger: s.logger,
	})
}
