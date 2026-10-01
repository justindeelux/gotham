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
//   - a start that arrives while a close reservation is live is Retryable,
//     even when no binding exists yet (F-1): the close either wins the
//     promotion or the start is redelivered after it completes;
//   - a close always reserves; when there is no live binding a leftover marker
//     is replaced so a redelivery re-runs the idempotent ledger clear instead
//     of being acked as a duplicate (MEDIUM-1), while an earlier in-flight
//     close of a live preview still dedupes.
func (s *Store) ClaimPreviewDelivery(ctx context.Context, params PreviewClaimParams) (PreviewClaimResult, error) {
	var result PreviewClaimResult

	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return result, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := s.queries.WithTx(tx)

	// Lock the base application row: the quota allocation, the head transition
	// and every close path are one critical section per application.
	if err := lockApplication(ctx, tx, params.ApplicationID); err != nil {
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

	// F-1: a live close reservation owns the pull request even before any
	// binding exists. A start that arrives while the close is in flight must
	// stay retryable (never approve a sibling the close cannot see) instead of
	// provisioning one and relying on the promotion fence to refuse it.
	closeInFlight := false
	if params.Kind == PreviewClaimStart {
		closeInFlight, err = queries.HasLivePreviewClose(ctx, sqlc.HasLivePreviewCloseParams{
			ApplicationID: params.ApplicationID,
			PrNumber:      params.PrNumber,
		})
		if err != nil {
			return PreviewClaimResult{}, err
		}
	}

	switch params.Kind {
	case PreviewClaimClose:
		if binding == nil || binding.State == "deleted" {
			// No live binding to tear down: a leftover close marker can only be
			// a previous close attempt that failed before clearing the ledger
			// (MEDIUM-1) or a concurrent no-binding close, whose clear is
			// idempotent. Replace it so the redelivery re-runs the close
			// instead of being acked as a duplicate.
			if err := queries.DeletePreviewCloseReservation(ctx, sqlc.DeletePreviewCloseReservationParams{
				ApplicationID: params.ApplicationID,
				PrNumber:      params.PrNumber,
			}); err != nil {
				return PreviewClaimResult{}, err
			}
		}
		if err := reserve(); err != nil {
			return PreviewClaimResult{}, err
		}
	case PreviewClaimStart:
		switch {
		case closeInFlight:
			result.Retryable = true
		case binding != nil && binding.State == "closing":
			// A close transition owns the preview right now — even at the
			// delivery's own head. A same-SHA reopen must stay retryable until
			// the teardown completes; afterward the same delivery recreates.
			result.Retryable = true
		case binding != nil && binding.State != "deleted" && binding.HeadSha == params.HeadSHA:
			// The live binding already names this revision.
			result.Duplicate = true
		default:
			live := binding != nil && binding.State != "deleted"
			if !live && params.LiveLimit > 0 {
				count, err := queries.CountLivePreviews(ctx, sqlc.CountLivePreviewsParams{
					ApplicationID: params.ApplicationID,
					// The PR's own in-flight lease already holds its slot and
					// must not deny a new head of the same PR.
					PrNumber: params.PrNumber,
				})
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

// PreviewBindingWriteParams is one fenced binding promotion: the pending
// binding write of a preview worker, authorized by the claim lease it was
// issued.
type PreviewBindingWriteParams struct {
	ApplicationID pgtype.UUID
	PrNumber      int32
	// ReservationID is the claim lease that authorizes this write.
	ReservationID pgtype.UUID
	// LeaseHeadSHA is the revision the lease was issued for (the delivery's
	// signed head); HeadSHA is the revision the binding records (an
	// intermediate promotion keeps the previously queued head).
	LeaseHeadSHA string
	HeadSHA      string
	// ConsumeLease deletes the lease on success (the final promotion);
	// an intermediate write keeps it.
	ConsumeLease bool
	// LiveLimit re-checks the quota when the write would make a deleted or
	// missing binding live (zero disables the check).
	LiveLimit int64
	// Binding fields.
	TeamID               pgtype.UUID
	Provider             string
	Repo                 string
	Branch               string
	Host                 string
	State                string
	PreviewApplicationID pgtype.UUID
}

// Refusal reasons reported by WritePreviewBinding.
const (
	// PreviewWriteLeaseRefused means the claim lease is gone or expired: the
	// worker is stale and must not promote a binding.
	PreviewWriteLeaseRefused = "lease"
	// PreviewWriteClosingRefused means a close transition owns the preview.
	PreviewWriteClosingRefused = "closing"
	// PreviewWriteLimitRefused means the preview quota is full.
	PreviewWriteLimitRefused = "limit"
)

// PreviewBindingWriteResult is the outcome of WritePreviewBinding: Binding is
// set when the write went through, Refused names the fence that stopped it
// otherwise.
type PreviewBindingWriteResult struct {
	Binding sqlc.PreviewDeploy
	Refused string
}

// WritePreviewBinding is the fenced counterpart of a plain upsert: it runs
// under the base-application lock, verifies the worker's claim lease still
// exists and is unexpired, refuses to touch a `closing` binding (a close owns
// it), re-checks the live-preview quota when the write would make the binding
// live, and only then stores the binding (consuming the lease on the final
// promotion). A worker whose lease expired while it provisioned therefore
// cannot promote a sixth live preview, and a close that completed in the
// meantime can never be overwritten back to active.
func (s *Store) WritePreviewBinding(ctx context.Context, params PreviewBindingWriteParams) (PreviewBindingWriteResult, error) {
	var result PreviewBindingWriteResult

	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return result, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := s.queries.WithTx(tx)

	// The same lock the claim takes: promotion serializes with allocation and
	// with every close path, so a completed close can never be overwritten.
	if err := lockApplication(ctx, tx, params.ApplicationID); err != nil {
		return result, err
	}

	lease, err := queries.GetPreviewDelivery(ctx, params.ReservationID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		result.Refused = PreviewWriteLeaseRefused
		return result, nil
	case err != nil:
		return result, err
	}
	if lease.Kind != PreviewClaimStart ||
		lease.ApplicationID != params.ApplicationID ||
		lease.PrNumber != params.PrNumber ||
		lease.HeadSha != params.LeaseHeadSHA {
		result.Refused = PreviewWriteLeaseRefused
		return result, nil
	}

	// F-1: a live close reservation owns this pull request even when no binding
	// exists yet. The close claim and this write serialize on the application
	// lock, so a start that claimed before the close can never promote after it
	// — the close either won the lock first (this check refuses) or the
	// promotion won (the close sees the binding and tears it down). A completed
	// close clears the marker (and the start lease) atomically, so a legitimate
	// reopen at the same revision is not blocked.
	closing, err := queries.HasLivePreviewClose(ctx, sqlc.HasLivePreviewCloseParams{
		ApplicationID: params.ApplicationID,
		PrNumber:      params.PrNumber,
	})
	if err != nil {
		return PreviewBindingWriteResult{}, err
	}
	if closing {
		result.Refused = PreviewWriteClosingRefused
		return result, nil
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
	}
	if binding != nil && binding.State == "closing" {
		// N4/R-1: a close transition owns this preview; never write over it.
		result.Refused = PreviewWriteClosingRefused
		return result, nil
	}

	live := binding != nil && binding.State != "deleted"
	if !live && params.LiveLimit > 0 {
		count, err := queries.CountLivePreviews(ctx, sqlc.CountLivePreviewsParams{
			ApplicationID: params.ApplicationID,
			PrNumber:      params.PrNumber,
		})
		if err != nil {
			return result, err
		}
		if count >= params.LiveLimit {
			result.Refused = PreviewWriteLimitRefused
			return result, nil
		}
	}

	stored, err := queries.UpsertPreviewDeploy(ctx, sqlc.UpsertPreviewDeployParams{
		ApplicationID:        params.ApplicationID,
		TeamID:               params.TeamID,
		Provider:             params.Provider,
		Repo:                 params.Repo,
		PrNumber:             params.PrNumber,
		Branch:               params.Branch,
		HeadSha:              params.HeadSHA,
		PreviewApplicationID: params.PreviewApplicationID,
		Host:                 params.Host,
		State:                params.State,
	})
	if err != nil {
		return PreviewBindingWriteResult{}, err
	}
	if params.ConsumeLease {
		if err := queries.ReleasePreviewDelivery(ctx, params.ReservationID); err != nil {
			return PreviewBindingWriteResult{}, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return PreviewBindingWriteResult{}, err
	}
	result.Binding = stored
	return result, nil
}

// lockApplication takes the per-application row lock every preview transition
// serializes on (claim, promotion, close intent, close completion, ledger
// clear). It is always the only lock a transition takes before writing, so the
// ordering is consistent and deadlock-free.
func lockApplication(ctx context.Context, tx pgx.Tx, applicationID pgtype.UUID) error {
	var locked pgtype.UUID
	return tx.QueryRow(ctx, "SELECT id FROM applications WHERE id = $1 FOR UPDATE", applicationID).Scan(&locked)
}

// MarkPreviewClosing persists a close intent on one binding, under the
// base-application lock: a promotion that won the lock first is then closed by
// this transition, and one that follows is refused by the closing state.
func (s *Store) MarkPreviewClosing(ctx context.Context, previewID pgtype.UUID) (sqlc.PreviewDeploy, error) {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return sqlc.PreviewDeploy{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := s.queries.WithTx(tx)

	var applicationID pgtype.UUID
	if err := tx.QueryRow(ctx, "SELECT application_id FROM preview_deploys WHERE id = $1", previewID).Scan(&applicationID); err != nil {
		return sqlc.PreviewDeploy{}, err
	}
	if err := lockApplication(ctx, tx, applicationID); err != nil {
		return sqlc.PreviewDeploy{}, err
	}
	row, err := queries.MarkPreviewDeployClosing(ctx, previewID)
	if err != nil {
		return sqlc.PreviewDeploy{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return sqlc.PreviewDeploy{}, err
	}
	return row, nil
}

// MarkPreviewClosed completes a close atomically under the base-application
// lock: the binding becomes deleted and the pull request's delivery ledger is
// cleared in one transaction. It is idempotent and runs on every close path —
// including the already-deleted retry — so a stale reservation can never
// poison a reopen, and a concurrent promotion can never overwrite the
// completed close (it either waits, or is refused by the deleted state).
func (s *Store) MarkPreviewClosed(ctx context.Context, applicationID pgtype.UUID, prNumber int32) (sqlc.PreviewDeploy, error) {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return sqlc.PreviewDeploy{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := s.queries.WithTx(tx)

	if err := lockApplication(ctx, tx, applicationID); err != nil {
		return sqlc.PreviewDeploy{}, err
	}
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
// existing (application_id, pr_number) row in place, under the base-application
// lock the other preview transitions serialize on (claim, promotion, close
// intent, close completion, ledger clear). The unique pair is what makes a
// redelivered PR event idempotent.
func (s *Store) UpsertPreviewDeploy(ctx context.Context, params sqlc.UpsertPreviewDeployParams) (sqlc.PreviewDeploy, error) {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return sqlc.PreviewDeploy{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := lockApplication(ctx, tx, params.ApplicationID); err != nil {
		return sqlc.PreviewDeploy{}, err
	}
	row, err := s.queries.WithTx(tx).UpsertPreviewDeploy(ctx, params)
	if err != nil {
		return sqlc.PreviewDeploy{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return sqlc.PreviewDeploy{}, err
	}
	return row, nil
}

// GetLivePreviewDeployBySibling returns the live binding a preview sibling
// application backs, or pgx.ErrNoRows. It is the terminal deploy hook's
// lookup: an application that is not a live preview resolves to nothing.
func (s *Store) GetLivePreviewDeployBySibling(ctx context.Context, previewApplicationID pgtype.UUID) (sqlc.PreviewDeploy, error) {
	return s.queries.GetLivePreviewDeployBySibling(ctx, previewApplicationID)
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

// MarkPreviewDeploysDeletedForSibling closes the bindings that point at a
// sibling application being deleted (the user-facing delete path): each
// binding is marked deleted and its pull request's ledger cleared, under the
// base-application lock. Clearing the ledger is what keeps the fence premise
// intact — a lease left behind could otherwise authorize a stale promotion.
func (s *Store) MarkPreviewDeploysDeletedForSibling(ctx context.Context, previewApplicationID pgtype.UUID) error {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := s.queries.WithTx(tx)

	rows, err := queries.ListLivePreviewDeploysForSibling(ctx, previewApplicationID)
	if err != nil {
		return err
	}
	// Lock every affected base application in a stable order (a preview
	// sibling normally backs one binding; the ordering keeps concurrent calls
	// deadlock-free when it does not).
	locked := make(map[[16]byte]bool, len(rows))
	for _, row := range rows {
		var key [16]byte
		copy(key[:], row.ApplicationID.Bytes[:])
		if locked[key] {
			continue
		}
		if err := lockApplication(ctx, tx, row.ApplicationID); err != nil {
			return err
		}
		locked[key] = true
	}
	for _, row := range rows {
		if _, err := queries.MarkPreviewDeployClosed(ctx, sqlc.MarkPreviewDeployClosedParams{
			ApplicationID: row.ApplicationID,
			PrNumber:      row.PrNumber,
		}); err != nil {
			return err
		}
		if err := queries.ClearPreviewDeliveries(ctx, sqlc.ClearPreviewDeliveriesParams{
			ApplicationID: row.ApplicationID,
			PrNumber:      row.PrNumber,
		}); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
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
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := lockApplication(ctx, tx, applicationID); err != nil {
		return err
	}
	if err := s.queries.WithTx(tx).ClearPreviewDeliveries(ctx, sqlc.ClearPreviewDeliveriesParams{
		ApplicationID: applicationID,
		PrNumber:      prNumber,
	}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
