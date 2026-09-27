package store

import (
	"context"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/justindeelux/gotham/internal/store/sqlc"
)

// ListProxiedApplications returns every application that declares a base
// domain, ordered by creation time so the proxy generator's input and its
// duplicate-domain disposition are deterministic. Each row carries the
// container of the newest running deployment (empty when none is running)
// and the disabled flag set by the domain-uniqueness migration.
func (s *Store) ListProxiedApplications(ctx context.Context) ([]sqlc.ListProxiedApplicationsRow, error) {
	return s.queries.ListProxiedApplications(ctx)
}

// LatestProxyConfigVersions returns a node's pushed configuration versions,
// newest first, capped at limit.
func (s *Store) LatestProxyConfigVersions(ctx context.Context, serverID pgtype.UUID, limit int32) ([]sqlc.ProxyConfigVersion, error) {
	return s.queries.LatestProxyConfigVersions(ctx, sqlc.LatestProxyConfigVersionsParams{
		ServerID: serverID,
		Limit:    limit,
	})
}

// InsertProxyConfigVersion records one successfully pushed configuration.
func (s *Store) InsertProxyConfigVersion(ctx context.Context, serverID pgtype.UUID, files []byte, contentHash string) (sqlc.ProxyConfigVersion, error) {
	return s.queries.InsertProxyConfigVersion(ctx, sqlc.InsertProxyConfigVersionParams{
		ServerID:    serverID,
		Files:       files,
		ContentHash: contentHash,
	})
}

// PruneProxyConfigVersions drops a node's versions older than before.
func (s *Store) PruneProxyConfigVersions(ctx context.Context, serverID pgtype.UUID, before pgtype.Timestamptz) error {
	return s.queries.PruneProxyConfigVersions(ctx, sqlc.PruneProxyConfigVersionsParams{
		ServerID:  serverID,
		CreatedAt: before,
	})
}
