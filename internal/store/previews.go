package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/justindeelux/gotham/internal/store/sqlc"
)

const (
	PreviewClaimStart = "start"
	PreviewClaimClose = "close"
)

type PreviewClaimParams struct {
	ApplicationID pgtype.UUID
	PrNumber      int32
	Kind          string
	HeadSHA       string
	DeliveryID    string
	LiveLimit     int64
}

type PreviewClaimResult struct {
	Approved    bool
	Duplicate   bool
	Limit       bool
	Retryable   bool
	Reservation sqlc.PreviewDelivery
	Binding     *sqlc.PreviewDeploy
}

func (s *Store) ClaimPreviewDelivery(ctx context.Context, params PreviewClaimParams) (PreviewClaimResult, error) {
	var result PreviewClaimResult

	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return result, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := s.queries.WithTx(tx)

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
			result.Retryable = true
		case binding != nil && binding.State != "deleted" && binding.HeadSha == params.HeadSHA:
			result.Duplicate = true
		default:
			live := binding != nil && binding.State != "deleted"
			if !live && params.LiveLimit > 0 {
				count, err := queries.CountLivePreviews(ctx, sqlc.CountLivePreviewsParams{
					ApplicationID: params.ApplicationID,
					PrNumber:      params.PrNumber,
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

type PreviewBindingWriteParams struct {
	ApplicationID        pgtype.UUID
	PrNumber             int32
	ReservationID        pgtype.UUID
	LeaseHeadSHA         string
	HeadSHA              string
	ConsumeLease         bool
	LiveLimit            int64
	TeamID               pgtype.UUID
	Provider             string
	Repo                 string
	Branch               string
	Host                 string
	State                string
	PreviewApplicationID pgtype.UUID
}

const (
	PreviewWriteLeaseRefused   = "lease"
	PreviewWriteClosingRefused = "closing"
	PreviewWriteLimitRefused   = "limit"
)

type PreviewBindingWriteResult struct {
	Binding sqlc.PreviewDeploy
	Refused string
}

func (s *Store) WritePreviewBinding(ctx context.Context, params PreviewBindingWriteParams) (PreviewBindingWriteResult, error) {
	var result PreviewBindingWriteResult

	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return result, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := s.queries.WithTx(tx)

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

func lockApplication(ctx context.Context, tx pgx.Tx, applicationID pgtype.UUID) error {
	var locked pgtype.UUID
	return tx.QueryRow(ctx, "SELECT id FROM applications WHERE id = $1 FOR UPDATE", applicationID).Scan(&locked)
}

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

func (s *Store) ListClosingPreviewDeploys(ctx context.Context, before pgtype.Timestamptz) ([]sqlc.PreviewDeploy, error) {
	return s.queries.ListClosingPreviewDeploys(ctx, before)
}

func (s *Store) PurgeExpiredPreviewDeliveries(ctx context.Context) (int64, error) {
	return s.queries.PurgeExpiredPreviewDeliveries(ctx)
}

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

func (s *Store) GetLivePreviewDeployBySibling(ctx context.Context, previewApplicationID pgtype.UUID) (sqlc.PreviewDeploy, error) {
	return s.queries.GetLivePreviewDeployBySibling(ctx, previewApplicationID)
}

func (s *Store) GetPreviewDeploy(ctx context.Context, applicationID pgtype.UUID, prNumber int32) (sqlc.PreviewDeploy, error) {
	return s.queries.GetPreviewDeploy(ctx, sqlc.GetPreviewDeployParams{
		ApplicationID: applicationID,
		PrNumber:      prNumber,
	})
}

func (s *Store) ListPreviewDeploysByApplication(ctx context.Context, applicationID pgtype.UUID) ([]sqlc.PreviewDeploy, error) {
	return s.queries.ListPreviewDeploysByApplication(ctx, applicationID)
}

func (s *Store) HasLivePreviewBindings(ctx context.Context, applicationID pgtype.UUID) (bool, error) {
	return s.queries.HasLivePreviewBindings(ctx, applicationID)
}

func (s *Store) ListOrphanedPreviewDeploys(ctx context.Context) ([]sqlc.PreviewDeploy, error) {
	return s.queries.ListOrphanedPreviewDeploys(ctx)
}

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

func (s *Store) ListOrphanedPreviewApplications(ctx context.Context, createdBefore pgtype.Timestamptz) ([]pgtype.UUID, error) {
	return s.queries.ListOrphanedPreviewApplications(ctx, createdBefore)
}

func (s *Store) ReleasePreviewDelivery(ctx context.Context, id pgtype.UUID) error {
	return s.queries.ReleasePreviewDelivery(ctx, id)
}

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
