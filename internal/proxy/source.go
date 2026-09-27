package proxy

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/justindeelux/gotham/internal/store"
)

// ProxiedApplication is one routing input row: an application that declares a
// base domain, together with the node it runs on and the endpoint of its
// newest running deployment.
type ProxiedApplication struct {
	// ID identifies the application (kept in generated names for traceability).
	ID uuid.UUID
	// ServerID is the node hosting the application; uuid.Nil means unassigned.
	ServerID uuid.UUID
	// BaseDomain is the raw stored domain, normalized and validated during
	// generation.
	BaseDomain string
	// Disabled marks a legacy duplicate binding disabled by migration 00012:
	// the value is preserved but never routed until its owner resolves the
	// conflict.
	Disabled bool
	// Port is the container port; HostPort the pinned host port, if any.
	// HostPort 0 means Docker assigned an ephemeral port that only the
	// running container's published bindings can reveal.
	Port     int32
	HostPort int32
	// ContainerID is the container of the newest running deployment, empty
	// when no deployment is running yet (the route is pending, not broken).
	ContainerID string
}

// ApplicationSource lists the routing input. The production implementation is
// *store.Store (through storeSource); tests substitute a fake.
type ApplicationSource interface {
	ListProxiedApplications(ctx context.Context) ([]ProxiedApplication, error)
}

// NodeSource lists every registered node. A global sync includes nodes that
// currently host no proxied application, so removing the last domain is
// repaired too (BE-6.1 F9).
type NodeSource interface {
	ListNodes(ctx context.Context) ([]uuid.UUID, error)
}

// storeSource adapts the shared store to the source seams.
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
			ID:          uuidFromPG(row.ID),
			ServerID:    uuidFromPG(row.ServerID),
			BaseDomain:  row.BaseDomain,
			Disabled:    row.BaseDomainDisabled,
			Port:        row.Port,
			HostPort:    row.HostPort,
			ContainerID: row.ContainerID,
		})
	}
	return apps, nil
}

// ListNodes maps the server registry to node ids.
func (s storeSource) ListNodes(ctx context.Context) ([]uuid.UUID, error) {
	rows, err := s.store.ListServers(ctx)
	if err != nil {
		return nil, err
	}
	nodes := make([]uuid.UUID, 0, len(rows))
	for _, row := range rows {
		if id := uuidFromPG(row.ID); id != uuid.Nil {
			nodes = append(nodes, id)
		}
	}
	return nodes, nil
}

// uuidFromPG converts a pgx UUID to uuid.UUID.
func uuidFromPG(id pgtype.UUID) uuid.UUID {
	if !id.Valid {
		return uuid.Nil
	}
	return uuid.UUID(id.Bytes)
}

// pgUUID converts a uuid.UUID to the pgx type (invalid for uuid.Nil).
func pgUUID(id uuid.UUID) pgtype.UUID {
	if id == uuid.Nil {
		return pgtype.UUID{}
	}
	return pgtype.UUID{Bytes: id, Valid: true}
}
