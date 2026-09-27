package proxy

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/justindeelux/gotham/internal/store"
)

// ProxiedApplication is one routing input row: an application that declares a
// base domain, together with the node it runs on and the pinned port Traefik
// must reach.
type ProxiedApplication struct {
	// ID identifies the application (kept in generated names for traceability).
	ID uuid.UUID
	// ServerID is the node hosting the application; uuid.Nil means unassigned.
	ServerID uuid.UUID
	// BaseDomain is the raw stored domain, validated during generation.
	BaseDomain string
	// Port is the container port, HostPort the pinned host port the node
	// publishes. Both must be set for the application to be routable:
	// without a pinned host port Docker assigns an ephemeral one that no
	// generated configuration can know.
	Port     int32
	HostPort int32
}

// ApplicationSource lists the routing input. The production implementation is
// *store.Store (through storeSource); tests substitute a fake.
type ApplicationSource interface {
	ListProxiedApplications(ctx context.Context) ([]ProxiedApplication, error)
}

// storeSource adapts the shared store to the ApplicationSource seam.
type storeSource struct {
	store *store.Store
}

// ListProxiedApplications maps the stored application rows to routing input.
func (s storeSource) ListProxiedApplications(ctx context.Context) ([]ProxiedApplication, error) {
	rows, err := s.store.ListProxiedApplications(ctx)
	if err != nil {
		return nil, err
	}
	apps := make([]ProxiedApplication, 0, len(rows))
	for _, row := range rows {
		apps = append(apps, ProxiedApplication{
			ID:         uuidFromPG(row.ID),
			ServerID:   uuidFromPG(row.ServerID),
			BaseDomain: row.BaseDomain,
			Port:       row.Port,
			HostPort:   row.HostPort,
		})
	}
	return apps, nil
}

// uuidFromPG converts a pgx UUID to uuid.UUID.
func uuidFromPG(id pgtype.UUID) uuid.UUID {
	if !id.Valid {
		return uuid.Nil
	}
	return uuid.UUID(id.Bytes)
}
