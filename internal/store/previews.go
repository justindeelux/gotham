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

// CountLivePreviewDeploys returns how many live (non-deleted) preview bindings
// one base application has — the per-application preview cap's read.
func (s *Store) CountLivePreviewDeploys(ctx context.Context, applicationID pgtype.UUID) (int64, error) {
	return s.queries.CountLivePreviewDeploys(ctx, applicationID)
}

// ListOrphanedPreviewDeploys returns live bindings whose sibling application
// is gone (or was never linked).
func (s *Store) ListOrphanedPreviewDeploys(ctx context.Context) ([]sqlc.PreviewDeploy, error) {
	return s.queries.ListOrphanedPreviewDeploys(ctx)
}

// MarkPreviewDeploysDeletedForSibling marks the bindings that point at a
// sibling application being deleted.
func (s *Store) MarkPreviewDeploysDeletedForSibling(ctx context.Context, previewApplicationID pgtype.UUID) error {
	return s.queries.MarkPreviewDeploysDeletedForSibling(ctx, previewApplicationID)
}

// ListOrphanedPreviewApplications returns preview applications with no
// binding, created before the given cutoff (the sweep grace period).
func (s *Store) ListOrphanedPreviewApplications(ctx context.Context, createdBefore pgtype.Timestamptz) ([]pgtype.UUID, error) {
	return s.queries.ListOrphanedPreviewApplications(ctx, createdBefore)
}

// ReservePreviewDelivery inserts a preview delivery reservation. pgx.ErrNoRows
// means the same signed revision (or a concurrent close) already reserved it.
func (s *Store) ReservePreviewDelivery(ctx context.Context, params sqlc.ReservePreviewDeliveryParams) (sqlc.PreviewDelivery, error) {
	return s.queries.ReservePreviewDelivery(ctx, params)
}

// ReleasePreviewDelivery removes a reservation that did not lead to a queued
// deployment. Removing an already-gone row is not an error.
func (s *Store) ReleasePreviewDelivery(ctx context.Context, id pgtype.UUID) error {
	return s.queries.ReleasePreviewDelivery(ctx, id)
}

// ClearPreviewDeliveries removes every reservation of one pull request at a
// lifecycle transition, so the opposite transition can reserve again.
func (s *Store) ClearPreviewDeliveries(ctx context.Context, applicationID pgtype.UUID, prNumber int32) error {
	return s.queries.ClearPreviewDeliveries(ctx, sqlc.ClearPreviewDeliveriesParams{
		ApplicationID: applicationID,
		PrNumber:      prNumber,
	})
}
