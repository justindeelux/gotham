package store

import (
	"context"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/justindeelux/gotham/internal/store/sqlc"
)

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
func (s *Store) ListToolchainJobsByServer(ctx context.Context, params sqlc.ListToolchainJobsByServerParams) ([]sqlc.ToolchainJob, error) {
	return s.queries.ListToolchainJobsByServer(ctx, params)
}

// ClaimNextQueuedToolchainJob atomically marks the oldest queued job running
// and returns it. pgx.ErrNoRows means the queue is empty.
func (s *Store) ClaimNextQueuedToolchainJob(ctx context.Context) (sqlc.ToolchainJob, error) {
	return s.queries.ClaimNextQueuedToolchainJob(ctx)
}

// AppendToolchainJobLog appends one log chunk to a job.
func (s *Store) AppendToolchainJobLog(ctx context.Context, params sqlc.AppendToolchainJobLogParams) (sqlc.ToolchainJob, error) {
	return s.queries.AppendToolchainJobLog(ctx, params)
}

// FinishToolchainJob closes a job with its terminal status and a final log chunk.
func (s *Store) FinishToolchainJob(ctx context.Context, params sqlc.FinishToolchainJobParams) (sqlc.ToolchainJob, error) {
	return s.queries.FinishToolchainJob(ctx, params)
}

// CancelToolchainJob cancels one queued job. A running or terminal job is left
// untouched, so this returns pgx.ErrNoRows for a job that cannot be cancelled.
func (s *Store) CancelToolchainJob(ctx context.Context, id pgtype.UUID) (sqlc.ToolchainJob, error) {
	return s.queries.CancelToolchainJob(ctx, id)
}

// FailInterruptedToolchainJobs marks every running job failed with reason,
// returning the affected ids. It runs once on boot.
func (s *Store) FailInterruptedToolchainJobs(ctx context.Context, reason string) ([]pgtype.UUID, error) {
	return s.queries.FailInterruptedToolchainJobs(ctx, reason)
}
