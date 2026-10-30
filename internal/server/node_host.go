package server

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/cleanup"
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

// cleanupHost maps the per-node host applier to the cleanup service's narrow
// view. Transport failures become the sentinels the service turns into a
// per-category notice instead of a failed node.
type cleanupHost struct {
	nodes nodeHostResolver
}

func (h cleanupHost) CleanupStatus(
	ctx context.Context, serverID uuid.UUID,
) (cleanup.HostCleanupStatus, error) {
	applier, err := h.applier(ctx, serverID)
	if err != nil {
		return cleanup.HostCleanupStatus{}, err
	}
	status, err := applier.CleanupStatus(ctx)
	if err != nil {
		return cleanup.HostCleanupStatus{}, cleanupHostError(err)
	}
	return cleanup.HostCleanupStatus{
		Journal: cleanup.HostCleanupCategory{
			Supported: status.Journal.Supported,
			Manager:   status.Journal.Manager,
			Bytes:     status.Journal.Bytes,
		},
		PackageCache: cleanup.HostCleanupCategory{
			Supported: status.PackageCache.Supported,
			Manager:   status.PackageCache.Manager,
			Bytes:     status.PackageCache.Bytes,
		},
	}, nil
}

func (h cleanupHost) CleanupHost(
	ctx context.Context, serverID uuid.UUID, category string, minAge time.Duration,
) (int64, error) {
	applier, err := h.applier(ctx, serverID)
	if err != nil {
		return 0, err
	}
	reclaimed, err := applier.CleanupHost(ctx, category, minAge)
	if err != nil {
		return 0, cleanupHostError(err)
	}
	return reclaimed, nil
}

func (h cleanupHost) applier(ctx context.Context, serverID uuid.UUID) (instance.HostApplier, error) {
	host, err := h.nodes.NodeHost(ctx, serverID)
	if err != nil {
		return nil, err
	}
	if host.Applier == nil {
		return nil, instance.ErrNodeNotFound
	}
	return host.Applier, nil
}

func cleanupHostError(err error) error {
	switch {
	case errors.Is(err, instance.ErrHelperOutdated):
		return cleanup.ErrHostHelperOutdated
	case errors.Is(err, instance.ErrAgentOutdated):
		return cleanup.ErrHostAgentOutdated
	case errors.Is(err, instance.ErrHost), errors.Is(err, instance.ErrUnsupported):
		return cleanup.ErrHostUnsupported
	default:
		return err
	}
}
