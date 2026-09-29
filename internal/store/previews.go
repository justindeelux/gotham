package store

import (
	"context"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/justindeelux/gotham/internal/store/sqlc"
)

// UpsertPreviewDeploy stores one pull request's preview binding, refreshing an
// existing (application_id, pr_number) row in place. The unique pair is what
// makes a redelivered PR event idempotent.
func (s *Store) UpsertPreviewDeploy(ctx context.Context, params sqlc.UpsertPreviewDeployParams) (sqlc.PreviewDeploy, error) {
	return s.queries.UpsertPreviewDeploy(ctx, params)
}

// GetPreviewDeploy returns one preview binding, or pgx.ErrNoRows when the PR
// has no preview yet.
func (s *Store) GetPreviewDeploy(ctx context.Context, applicationID pgtype.UUID, prNumber int32) (sqlc.PreviewDeploy, error) {
	return s.queries.GetPreviewDeploy(ctx, sqlc.GetPreviewDeployParams{
		ApplicationID: applicationID,
		PrNumber:      prNumber,
	})
}

// ListPreviewDeploysByApplication returns one application's preview bindings,
// newest first (the previews screen and the teardown path).
func (s *Store) ListPreviewDeploysByApplication(ctx context.Context, applicationID pgtype.UUID) ([]sqlc.PreviewDeploy, error) {
	return s.queries.ListPreviewDeploysByApplication(ctx, applicationID)
}

// MarkPreviewDeployDeleted marks a preview torn down and returns the row.
func (s *Store) MarkPreviewDeployDeleted(ctx context.Context, id pgtype.UUID) (sqlc.PreviewDeploy, error) {
	return s.queries.MarkPreviewDeployDeleted(ctx, id)
}

// ListStalePreviewDeploys returns live previews whose last activity is older
// than before — the orphan sweep's work list.
func (s *Store) ListStalePreviewDeploys(ctx context.Context, before pgtype.Timestamptz) ([]sqlc.PreviewDeploy, error) {
	return s.queries.ListStalePreviewDeploys(ctx, before)
}
