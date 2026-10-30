package server

import (
	"context"
	"errors"

	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/instance"
	"github.com/justindeelux/gotham/internal/servers"
	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
)

type hostDialer interface {
	DialHostClient(ctx context.Context, id uuid.UUID, opts ...servers.DockerDialOption) (*servers.HostClient, error)
}

// nodeHostResolver picks the host applier for one server: the control plane's
// own machine keeps the proven sudo helper, every other node goes through its
// agent so no existing control-plane install needs its sudoers regranted.
type nodeHostResolver struct {
	nodes  ServerService
	dialer hostDialer
}

func (r nodeHostResolver) NodeHost(ctx context.Context, id uuid.UUID) (instance.NodeHost, error) {
	row, err := r.nodes.Get(ctx, id)
	if err != nil {
		if errors.Is(err, servers.ErrNotFound) {
			return instance.NodeHost{}, instance.ErrNodeNotFound
		}
		return instance.NodeHost{}, err
	}
	if row.NodeID != nil && *row.NodeID == "localhost" {
		return instance.NodeHost{Applier: instance.NewSudoApplier(), Local: true}, nil
	}
	return instance.NodeHost{Applier: instance.NewAgentApplier(func(ctx context.Context) (agentv1.HostServiceClient, func(), error) {
		client, err := r.dialer.DialHostClient(ctx, id)
		if err != nil {
			return nil, nil, err
		}
		return client, func() { _ = client.Close() }, nil
	})}, nil
}
