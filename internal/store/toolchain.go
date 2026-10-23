package store

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/justindeelux/gotham/internal/store/sqlc"
)

const runningServerUniqueIndex = "toolchain_jobs_running_server_unique"

var ErrClaimContended = errors.New("store: toolchain claim lost the per-node race")

func isUniqueViolation(err error, constraint string) bool {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "23505" {
		return false
	}
	return constraint == "" || pgErr.ConstraintName == constraint
}

func (s *Store) CreateToolchainJob(ctx context.Context, params sqlc.CreateToolchainJobParams) (sqlc.ToolchainJob, error) {
	return s.queries.CreateToolchainJob(ctx, params)
}

func (s *Store) GetToolchainJobForServer(ctx context.Context, params sqlc.GetToolchainJobForServerParams) (sqlc.ToolchainJob, error) {
	return s.queries.GetToolchainJobForServer(ctx, params)
}

func (s *Store) ListToolchainJobsByServer(ctx context.Context, params sqlc.ListToolchainJobsByServerParams) ([]sqlc.ToolchainJob, error) {
	return s.queries.ListToolchainJobsByServer(ctx, params)
}

func (s *Store) ClaimNextQueuedToolchainJob(ctx context.Context, params sqlc.ClaimNextQueuedToolchainJobParams) (sqlc.ToolchainJob, error) {
	job, err := s.queries.ClaimNextQueuedToolchainJob(ctx, params)
	if err != nil && isUniqueViolation(err, runningServerUniqueIndex) {
		return sqlc.ToolchainJob{}, ErrClaimContended
	}
	return job, err
}

func (s *Store) RenewToolchainJobLease(ctx context.Context, params sqlc.RenewToolchainJobLeaseParams) (int64, error) {
	return s.queries.RenewToolchainJobLease(ctx, params)
}

func (s *Store) FailStaleToolchainJobs(ctx context.Context) ([]pgtype.UUID, error) {
	return s.queries.FailStaleToolchainJobs(ctx)
}

func (s *Store) AppendToolchainJobLog(ctx context.Context, params sqlc.AppendToolchainJobLogParams) error {
	return s.queries.AppendToolchainJobLog(ctx, params)
}

func (s *Store) AppendToolchainJobLogTail(ctx context.Context, params sqlc.AppendToolchainJobLogTailParams) error {
	return s.queries.AppendToolchainJobLogTail(ctx, params)
}

func (s *Store) ListToolchainJobLogs(ctx context.Context, params sqlc.ListToolchainJobLogsParams) ([]sqlc.ListToolchainJobLogsRow, error) {
	return s.queries.ListToolchainJobLogs(ctx, params)
}

func (s *Store) FinishToolchainJob(ctx context.Context, params sqlc.FinishToolchainJobParams) (sqlc.ToolchainJob, error) {
	return s.queries.FinishToolchainJob(ctx, params)
}

func (s *Store) CancelToolchainJob(ctx context.Context, id pgtype.UUID) (sqlc.ToolchainJob, error) {
	return s.queries.CancelToolchainJob(ctx, id)
}
