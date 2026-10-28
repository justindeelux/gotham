package store

import (
	"context"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/justindeelux/gotham/internal/store/sqlc"
)

func (s *Store) GetInstanceMonitoringPolicy(ctx context.Context) (sqlc.MonitoringPolicy, error) {
	return s.queries.GetInstanceMonitoringPolicy(ctx)
}

func (s *Store) GetNodeMonitoringPolicy(ctx context.Context, nodeID pgtype.UUID) (sqlc.MonitoringPolicy, error) {
	return s.queries.GetNodeMonitoringPolicy(ctx, nodeID)
}

func (s *Store) UpsertInstanceMonitoringPolicy(ctx context.Context, params sqlc.UpsertInstanceMonitoringPolicyParams) (sqlc.MonitoringPolicy, error) {
	return s.queries.UpsertInstanceMonitoringPolicy(ctx, params)
}

func (s *Store) UpsertNodeMonitoringPolicy(ctx context.Context, params sqlc.UpsertNodeMonitoringPolicyParams) (sqlc.MonitoringPolicy, error) {
	return s.queries.UpsertNodeMonitoringPolicy(ctx, params)
}

func (s *Store) DeleteInstanceMonitoringPolicy(ctx context.Context) (int64, error) {
	return s.queries.DeleteInstanceMonitoringPolicy(ctx)
}

func (s *Store) DeleteNodeMonitoringPolicy(ctx context.Context, nodeID pgtype.UUID) (int64, error) {
	return s.queries.DeleteNodeMonitoringPolicy(ctx, nodeID)
}

func (s *Store) GetFilesystemIdentity(ctx context.Context, params sqlc.GetFilesystemIdentityParams) (sqlc.FilesystemIdentity, error) {
	return s.queries.GetFilesystemIdentity(ctx, params)
}

func (s *Store) CreateFilesystemIdentity(ctx context.Context, params sqlc.CreateFilesystemIdentityParams) (sqlc.FilesystemIdentity, error) {
	return s.queries.CreateFilesystemIdentity(ctx, params)
}

func (s *Store) UpdateFilesystemIdentity(ctx context.Context, params sqlc.UpdateFilesystemIdentityParams) (sqlc.FilesystemIdentity, error) {
	return s.queries.UpdateFilesystemIdentity(ctx, params)
}

func (s *Store) ListFilesystemIdentitiesByNode(ctx context.Context, nodeID pgtype.UUID) ([]sqlc.FilesystemIdentity, error) {
	return s.queries.ListFilesystemIdentitiesByNode(ctx, nodeID)
}

func (s *Store) ListFilesystemIdentities(ctx context.Context) ([]sqlc.FilesystemIdentity, error) {
	return s.queries.ListFilesystemIdentities(ctx)
}

func (s *Store) ListFilesystemUsageBuckets(ctx context.Context, params sqlc.ListFilesystemUsageBucketsParams) ([]sqlc.FilesystemUsageBucket, error) {
	return s.queries.ListFilesystemUsageBuckets(ctx, params)
}

func (s *Store) UpsertFilesystemUsageBucket(ctx context.Context, params sqlc.UpsertFilesystemUsageBucketParams) error {
	return s.queries.UpsertFilesystemUsageBucket(ctx, params)
}

func (s *Store) DeleteFilesystemUsageBefore(ctx context.Context, before pgtype.Timestamptz) (int64, error) {
	return s.queries.DeleteFilesystemUsageBefore(ctx, before)
}
