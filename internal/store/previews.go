package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/justindeelux/gotham/internal/store/sqlc"
)

// Preview claim kinds, mirroring the preview_deliveries CHECK constraint.
const (
	PreviewClaimStart = "start"
	PreviewClaimClose = "close"
)

// PreviewClaimParams is one delivery claim: the preview-specific atomic
// transition the webhooks service asks for. LiveLimit caps how many distinct
// pull requests of the application may be live or in flight (zero disables the
// cap).
type PreviewClaimParams struct {
	ApplicationID pgtype.UUID
	PrNumber      int32
	Kind          string
	HeadSHA       string
	DeliveryID    string
	LiveLimit     int64
}

// PreviewClaimResult is the outcome of ClaimPreviewDelivery. Exactly one of
// Approved/Duplicate/Limit/Retryable describes the decision.
type PreviewClaimResult struct {
	// Approved means the delivery may proceed; Reservation is its in-flight
	// lease (start) or teardown marker (close).
	Approved bool
	// Duplicate means the same signed revision is already the live head, or an
	// earlier attempt for it is still in flight.
	Duplicate bool
	// Limit means approving a new preview would exceed LiveLimit.
	Limit bool
	// Retryable means a close transition is in progress for this pull request:
	// the delivery should be redelivered once it finishes.
	Retryable bool
	// Reservation is the inserted ledger row (Approved only).
	Reservation sqlc.PreviewDelivery
	// Binding is the current binding, nil when the application has none.
	Binding *sqlc.PreviewDeploy
}

// ClaimPreviewDelivery is the atomic per-(application, pull request) delivery
// decision:
//
//   - the application row is locked, serializing every preview transition of
//     that application (this is also the quota-allocation gate);
//   - expired ledger rows of the pull request are purged, so a crashed
//     delivery's lease can never suppress a later attempt;
//   - a start is a duplicate only when the delivery's head is the live
//     binding's CURRENT head, or an earlier attempt for that head is still in
//     flight — historical heads are not a handled set (a force-push back to
//     an earlier revision must deploy again);
//   - a start for a new preview is refused as Limit when the live-or-in-flight
//     count is already at LiveLimit;
//   - a close always reserves (until an earlier close is still in flight).
func (s *Store) ClaimPreviewDelivery(ctx context.Context, params PreviewClaimParams) (PreviewClaimResult, error) {
	var result PreviewClaimResult

	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return result, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := s.queries.WithTx(tx)

	// Lock the base application row: the quota allocation and the head
	// transition are one critical section per application.
	var locked pgtype.UUID
	if err := tx.QueryRow(ctx, "SELECT id FROM applications WHERE id = $1 FOR UPDATE", params.ApplicationID).Scan(&locked); err != nil {
		return result, err
	}

	if err := queries.PurgeExpiredPreviewDeliveriesFor(ctx, sqlc.PurgeExpiredPreviewDeliveriesForParams{
		ApplicationID: params.ApplicationID,
		PrNumber:      params.PrNumber,
	}); err != nil {
		return result, err
	}

	var binding *sqlc.PreviewDeploy
	row, err := queries.GetPreviewDeploy(ctx, sqlc.GetPreviewDeployParams{
		ApplicationID: params.ApplicationID,
		PrNumber:      params.PrNumber,
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
	case err != nil:
		return result, err
	default:
		binding = &row
		result.Binding = binding
	}

	reserve := func() error {
		row, err := queries.ReservePreviewDelivery(ctx, sqlc.ReservePreviewDeliveryParams{
			ApplicationID: params.ApplicationID,
			PrNumber:      params.PrNumber,
			Kind:          params.Kind,
			HeadSha:       params.HeadSHA,
			DeliveryID:    params.DeliveryID,
		})
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			result.Duplicate = true
		case err != nil:
			return err
		default:
			result.Approved = true
			result.Reservation = row
		}
		return nil
	}

	switch params.Kind {
	case PreviewClaimClose:
		if err := reserve(); err != nil {
			return PreviewClaimResult{}, err
		}
	case PreviewClaimStart:
		switch {
		case binding != nil && binding.State != "deleted" && binding.HeadSha == params.HeadSHA:
			// The live binding already names this revision.
			result.Duplicate = true
		case binding != nil && binding.State == "closing":
			// A close transition owns the preview right now; the reopen waits
			// for a redelivery once it finishes.
			result.Retryable = true
		default:
			live := binding != nil && binding.State != "deleted"
			if !live && params.LiveLimit > 0 {
				count, err := queries.CountLivePreviews(ctx, params.ApplicationID)
				if err != nil {
					return PreviewClaimResult{}, err
				}
				if count >= params.LiveLimit {
					result.Limit = true
					break
				}
			}
			if result.Limit {
				break
			}
			if err := reserve(); err != nil {
				return PreviewClaimResult{}, err
			}
		}
	default:
		return PreviewClaimResult{}, fmt.Errorf("store: unknown preview claim kind %q", params.Kind)
	}

	if err := tx.Commit(ctx); err != nil {
		return PreviewClaimResult{}, err
	}
	return result, nil
}

// MarkPreviewClosing persists a close intent on one binding, so a teardown
// that fails (or is never retried by the host) is re-attempted by the sweep.
func (s *Store) MarkPreviewClosing(ctx context.Context, previewID pgtype.UUID) (sqlc.PreviewDeploy, error) {
	return s.queries.MarkPreviewDeployClosing(ctx, previewID)
}

// MarkPreviewClosed completes a close atomically: the binding becomes deleted
// and the pull request's delivery ledger is cleared in one transaction. It is
// idempotent and runs on every close path — including the already-deleted
// retry — so a stale reservation can never poison a reopen.
func (s *Store) MarkPreviewClosed(ctx context.Context, applicationID pgtype.UUID, prNumber int32) (sqlc.PreviewDeploy, error) {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return sqlc.PreviewDeploy{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := s.queries.WithTx(tx)

	row, err := queries.MarkPreviewDeployClosed(ctx, sqlc.MarkPreviewDeployClosedParams{
		ApplicationID: applicationID,
		PrNumber:      prNumber,
	})
	if err != nil {
		return sqlc.PreviewDeploy{}, err
	}
	if err := queries.ClearPreviewDeliveries(ctx, sqlc.ClearPreviewDeliveriesParams{
		ApplicationID: applicationID,
		PrNumber:      prNumber,
	}); err != nil {
		return sqlc.PreviewDeploy{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return sqlc.PreviewDeploy{}, err
	}
	return row, nil
}

// ListClosingPreviewDeploys returns bindings whose close intent is older than
// the cutoff — the sweep's teardown-retry list.
func (s *Store) ListClosingPreviewDeploys(ctx context.Context, before pgtype.Timestamptz) ([]sqlc.PreviewDeploy, error) {
	return s.queries.ListClosingPreviewDeploys(ctx, before)
}

// PurgeExpiredPreviewDeliveries removes every expired reservation and reports
// how many rows went — the sweep's housekeeping pass.
func (s *Store) PurgeExpiredPreviewDeliveries(ctx context.Context) (int64, error) {
	return s.queries.PurgeExpiredPreviewDeliveries(ctx)
}

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

// ReleasePreviewDelivery removes a reservation that did not lead to a queued
// deployment. Removing an already-gone row is not an error.
func (s *Store) ReleasePreviewDelivery(ctx context.Context, id pgtype.UUID) error {
	return s.queries.ReleasePreviewDelivery(ctx, id)
}

// ClearPreviewDeliveries removes every reservation of one pull request.
func (s *Store) ClearPreviewDeliveries(ctx context.Context, applicationID pgtype.UUID, prNumber int32) error {
	return s.queries.ClearPreviewDeliveries(ctx, sqlc.ClearPreviewDeliveriesParams{
		ApplicationID: applicationID,
		PrNumber:      prNumber,
	})
}
