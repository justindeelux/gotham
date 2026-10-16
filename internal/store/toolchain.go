package store

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/justindeelux/gotham/internal/store/sqlc"
)

// runningServerUniqueIndex is the partial unique index that serializes a node's
// jobs (migration 00055).
const runningServerUniqueIndex = "toolchain_jobs_running_server_unique"

// isUniqueViolation reports whether err is a Postgres unique-violation (23505),
// optionally on a specific constraint/index name.
func isUniqueViolation(err error, constraint string) bool {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "23505" {
		return false
	}
	return constraint == "" || pgErr.ConstraintName == constraint
}

// Thin wrappers over the generated toolchain-job queries (JUS-176). Store keeps
// its sqlc handle private, so the toolchain domain package reads and writes the
// queue through methods like these.

// CreateToolchainJob enqueues one install/uninstall job. When the node already
// has an active job for the same tool and action the unique index makes the
// insert a no-op, so this returns pgx.ErrNoRows and the caller reports the tool
// as skipped.
func (s *Store) CreateToolchainJob(ctx context.Context, params sqlc.CreateToolchainJobParams) (sqlc.ToolchainJob, error) {
	return s.queries.CreateToolchainJob(ctx, params)
}

// GetToolchainJobForServer loads one job scoped to its node, so a job id from
// another node answers not-found.
func (s *Store) GetToolchainJobForServer(ctx context.Context, params sqlc.GetToolchainJobForServerParams) (sqlc.ToolchainJob, error) {
	return s.queries.GetToolchainJobForServer(ctx, params)
}

// ListToolchainJobsByServer returns up to limit of a node's jobs, newest first.
// It never loads job logs.
func (s *Store) ListToolchainJobsByServer(ctx context.Context, params sqlc.ListToolchainJobsByServerParams) ([]sqlc.ToolchainJob, error) {
	return s.queries.ListToolchainJobsByServer(ctx, params)
}

// ClaimNextQueuedToolchainJob atomically claims the oldest queued job whose
// node has no running job, recording the claiming instance and a lease. A
// concurrent claim that loses the per-node unique-index race (23505) is a
// no-op: it reports pgx.ErrNoRows, the same as an empty queue.
func (s *Store) ClaimNextQueuedToolchainJob(ctx context.Context, params sqlc.ClaimNextQueuedToolchainJobParams) (sqlc.ToolchainJob, error) {
	job, err := s.queries.ClaimNextQueuedToolchainJob(ctx, params)
	if err != nil && isUniqueViolation(err, runningServerUniqueIndex) {
		return sqlc.ToolchainJob{}, pgx.ErrNoRows
	}
	return job, err
}

// RenewToolchainJobLease extends a running job's lease while this instance owns
// it, returning the number of rows updated (0 = the lease lapsed).
func (s *Store) RenewToolchainJobLease(ctx context.Context, params sqlc.RenewToolchainJobLeaseParams) (int64, error) {
	return s.queries.RenewToolchainJobLease(ctx, params)
}

// FailStaleToolchainJobs fails running jobs whose lease lapsed, returning their
// ids.
func (s *Store) FailStaleToolchainJobs(ctx context.Context) ([]pgtype.UUID, error) {
	return s.queries.FailStaleToolchainJobs(ctx)
}

// AppendToolchainJobLog appends one log line as its own row.
func (s *Store) AppendToolchainJobLog(ctx context.Context, params sqlc.AppendToolchainJobLogParams) error {
	return s.queries.AppendToolchainJobLog(ctx, params)
}

// AppendToolchainJobLogTail appends one log line after whatever is logged.
func (s *Store) AppendToolchainJobLogTail(ctx context.Context, params sqlc.AppendToolchainJobLogTailParams) error {
	return s.queries.AppendToolchainJobLogTail(ctx, params)
}

// ListToolchainJobLogs returns a page of a job's log, oldest first.
func (s *Store) ListToolchainJobLogs(ctx context.Context, params sqlc.ListToolchainJobLogsParams) ([]sqlc.ListToolchainJobLogsRow, error) {
	return s.queries.ListToolchainJobLogs(ctx, params)
}

// FinishToolchainJob closes a job with its terminal status.
func (s *Store) FinishToolchainJob(ctx context.Context, params sqlc.FinishToolchainJobParams) (sqlc.ToolchainJob, error) {
	return s.queries.FinishToolchainJob(ctx, params)
}

// CancelToolchainJob cancels one queued job. A running or terminal job is left
// untouched, so this returns pgx.ErrNoRows for a job that cannot be cancelled.
func (s *Store) CancelToolchainJob(ctx context.Context, id pgtype.UUID) (sqlc.ToolchainJob, error) {
	return s.queries.CancelToolchainJob(ctx, id)
}
