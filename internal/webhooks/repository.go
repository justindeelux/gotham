package webhooks

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/justindeelux/gotham/internal/providers"
	"github.com/justindeelux/gotham/internal/store"
	"github.com/justindeelux/gotham/internal/store/sqlc"
)

type Hook struct {
	ID            uuid.UUID
	ApplicationID uuid.UUID
	Provider      string
	Repo          string
	HookID        string
	URL           string
	CreatedAt     time.Time
}

type Target struct {
	ApplicationID uuid.UUID
	UserID        uuid.UUID
	TeamID        uuid.UUID
	Provider      string
	Repo          string
	Branch        string
	CloneURL      string
	Name          string
	BaseDomain    string
	HookID        string
	Secret        string
	URL           string
}

type Preview struct {
	ID                   uuid.UUID
	ApplicationID        uuid.UUID
	TeamID               uuid.UUID
	Provider             string
	Repo                 string
	PRNumber             int
	Branch               string
	HeadSHA              string
	PreviewApplicationID uuid.UUID
	Host                 string
	State                string
	CreatedAt            time.Time
	UpdatedAt            time.Time
	DeletedAt            time.Time
}

const (
	ReservationStart = "start"
	ReservationClose = "close"
)

type DeliveryReservation struct {
	ID            uuid.UUID
	ApplicationID uuid.UUID
	PRNumber      int
	Kind          string
	HeadSHA       string
	DeliveryID    string
	ReceivedAt    time.Time
	ExpiresAt     time.Time
}

type PreviewClaim struct {
	ApplicationID uuid.UUID
	PRNumber      int
	Kind          string
	HeadSHA       string
	DeliveryID    string
	LiveLimit     int
}

type PreviewClaimResult struct {
	Approved    bool
	Duplicate   bool
	Limit       bool
	Retryable   bool
	Reservation DeliveryReservation
	Binding     *Preview
}

const (
	BindingRefusedLease   = "lease"
	BindingRefusedClosing = "closing"
	BindingRefusedLimit   = "limit"
)

type PreviewBindingWrite struct {
	ApplicationID        uuid.UUID
	PRNumber             int
	ReservationID        uuid.UUID
	LeaseHeadSHA         string
	HeadSHA              string
	ConsumeLease         bool
	LiveLimit            int
	TeamID               uuid.UUID
	Provider             string
	Repo                 string
	Branch               string
	Host                 string
	State                string
	PreviewApplicationID uuid.UUID
}

type PreviewBindingWriteResult struct {
	Binding *Preview
	Refused string
}

type Event struct {
	ID            uuid.UUID
	ApplicationID uuid.UUID
	Provider      string
	Event         string
	DeliveryID    string
	Ref           string
	CommitSHA     string
	DeploymentID  uuid.UUID
	ReceivedAt    time.Time
}

type Repository interface {
	GetApplication(ctx context.Context, appID uuid.UUID) (Application, error)
	GetWebhook(ctx context.Context, appID uuid.UUID) (Hook, error)
	CreateWebhook(ctx context.Context, hook Hook, secret string) (Hook, error)
	DeleteWebhook(ctx context.Context, appID uuid.UUID) (Hook, error)
	Targets(ctx context.Context, provider, repo string) ([]Target, error)
	ClaimEvent(ctx context.Context, event Event) (Event, error)
	ReleaseEvent(ctx context.Context, eventID uuid.UUID) error
	LinkEventDeployment(ctx context.Context, eventID, deploymentID uuid.UUID) error
	ClaimPreviewDelivery(ctx context.Context, claim PreviewClaim) (PreviewClaimResult, error)
	ReleasePreviewDelivery(ctx context.Context, reservationID uuid.UUID) error
	ClearPreviewDeliveries(ctx context.Context, appID uuid.UUID, prNumber int) error
	MarkPreviewClosing(ctx context.Context, previewID uuid.UUID) (Preview, error)
	MarkPreviewClosed(ctx context.Context, appID uuid.UUID, prNumber int) (Preview, error)
	ListClosingPreviews(ctx context.Context, before time.Time) ([]Preview, error)
	PurgeExpiredPreviewReservations(ctx context.Context) (int, error)
	WritePreviewBinding(ctx context.Context, write PreviewBindingWrite) (PreviewBindingWriteResult, error)
	GetPreview(ctx context.Context, appID uuid.UUID, prNumber int) (Preview, error)
	GetPreviewByApplication(ctx context.Context, previewAppID uuid.UUID) (Preview, error)
	UpsertPreview(ctx context.Context, preview Preview) (Preview, error)
	ListPreviews(ctx context.Context, appID uuid.UUID) ([]Preview, error)
	ListOrphanedPreviews(ctx context.Context) ([]Preview, error)
	ListOrphanedPreviewApplications(ctx context.Context, createdBefore time.Time) ([]uuid.UUID, error)
	MarkPreviewsDeletedForSibling(ctx context.Context, previewAppID uuid.UUID) error
}

type Application struct {
	ID         uuid.UUID
	UserID     uuid.UUID
	TeamID     uuid.UUID
	Provider   string
	Repo       string
	Branch     string
	CloneURL   string
	Name       string
	BaseDomain string
}

type storeRepository struct {
	store  *store.Store
	secret string
	logger *slog.Logger
}

func newStoreRepository(st *store.Store, secret string, logger *slog.Logger) *storeRepository {
	if logger == nil {
		logger = slog.Default()
	}
	return &storeRepository{store: st, secret: secret, logger: logger}
}

func (r *storeRepository) GetApplication(ctx context.Context, appID uuid.UUID) (Application, error) {
	row, err := r.store.GetApplication(ctx, pgUUID(appID))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Application{}, ErrNotFound
		}
		return Application{}, err
	}
	primary := ""
	if domains, err := r.store.ListApplicationDomainsByApplication(ctx, pgUUID(appID)); err == nil {
		for _, domain := range domains {
			if !domain.Disabled && domain.IsPrimary {
				primary = domain.Domain
				break
			}
		}
		if primary == "" {
			for _, domain := range domains {
				if !domain.Disabled {
					primary = domain.Domain
					break
				}
			}
		}
	} else {
		return Application{}, err
	}
	return Application{
		ID:         uuidFromPG(row.ID),
		UserID:     uuidFromPG(row.UserID),
		TeamID:     uuidFromPG(row.TeamID),
		Provider:   row.Provider,
		Repo:       row.Repo,
		Branch:     row.Branch,
		CloneURL:   row.CloneUrl,
		Name:       row.Name,
		BaseDomain: primary,
	}, nil
}

func (r *storeRepository) GetWebhook(ctx context.Context, appID uuid.UUID) (Hook, error) {
	row, err := r.store.GetApplicationWebhookByApp(ctx, pgUUID(appID))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Hook{}, ErrNotFound
		}
		return Hook{}, err
	}
	return hookFromRow(row), nil
}

func (r *storeRepository) CreateWebhook(ctx context.Context, hook Hook, secret string) (Hook, error) {
	sealed, err := providers.SealSecret(r.secret, secret)
	if err != nil {
		return Hook{}, err
	}
	row, err := r.store.CreateApplicationWebhook(ctx, sqlc.CreateApplicationWebhookParams{
		ApplicationID: pgUUID(hook.ApplicationID),
		Provider:      hook.Provider,
		Repo:          hook.Repo,
		HookID:        hook.HookID,
		Secret:        sealed,
		Url:           hook.URL,
	})
	if err != nil {
		if isUniqueViolation(err) {
			return Hook{}, ErrConflict
		}
		return Hook{}, err
	}
	return hookFromRow(row), nil
}

func (r *storeRepository) DeleteWebhook(ctx context.Context, appID uuid.UUID) (Hook, error) {
	row, err := r.store.DeleteApplicationWebhook(ctx, pgUUID(appID))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Hook{}, ErrNotFound
		}
		return Hook{}, err
	}
	return hookFromRow(row), nil
}

func (r *storeRepository) Targets(ctx context.Context, provider, repo string) ([]Target, error) {
	rows, err := r.store.ListWebhookTargetsForRepo(ctx, provider, repo)
	if err != nil {
		return nil, err
	}
	targets := make([]Target, 0, len(rows))
	for _, row := range rows {
		secret, err := providers.OpenSecret(r.secret, row.Secret)
		if err != nil {
			r.logger.Warn("webhooks: stored hook secret could not be opened; deliveries for this hook cannot be verified",
				"application_id", uuidFromPG(row.ApplicationID), "provider", row.Provider,
				"repo", row.Repo, "hook_id", row.HookID, "error", err)
			continue
		}
		targets = append(targets, Target{
			ApplicationID: uuidFromPG(row.ApplicationID),
			UserID:        uuidFromPG(row.UserID),
			TeamID:        uuidFromPG(row.TeamID),
			Provider:      row.Provider,
			Repo:          row.Repo,
			Branch:        row.Branch,
			CloneURL:      row.CloneUrl,
			Name:          row.Name,
			BaseDomain:    row.BaseDomain,
			HookID:        row.HookID,
			Secret:        secret,
			URL:           row.Url,
		})
	}
	return targets, nil
}

func (r *storeRepository) ClaimEvent(ctx context.Context, event Event) (Event, error) {
	row, err := r.store.CreateWebhookEvent(ctx, sqlc.CreateWebhookEventParams{
		ApplicationID: pgUUID(event.ApplicationID),
		Provider:      event.Provider,
		Event:         event.Event,
		DeliveryID:    event.DeliveryID,
		Ref:           event.Ref,
		CommitSha:     event.CommitSHA,
	})
	if err != nil {
		if !isUniqueViolation(err) {
			return Event{}, err
		}
		existing, lookupErr := r.store.GetWebhookEventForDelivery(ctx, sqlc.GetWebhookEventForDeliveryParams{
			ApplicationID: pgUUID(event.ApplicationID),
			CommitSha:     event.CommitSHA,
			DeliveryID:    event.DeliveryID,
		})
		if lookupErr != nil {
			return Event{}, ErrDuplicate
		}
		return Event{
			ID:            uuidFromPG(existing.ID),
			ApplicationID: uuidFromPG(existing.ApplicationID),
			Provider:      existing.Provider,
			Event:         existing.Event,
			DeliveryID:    existing.DeliveryID,
			Ref:           existing.Ref,
			CommitSHA:     existing.CommitSha,
			DeploymentID:  uuidFromPG(existing.DeploymentID),
			ReceivedAt:    timeFromPG(existing.ReceivedAt),
		}, ErrDuplicate
	}
	return Event{
		ID:            uuidFromPG(row.ID),
		ApplicationID: uuidFromPG(row.ApplicationID),
		Provider:      row.Provider,
		Event:         row.Event,
		DeliveryID:    row.DeliveryID,
		Ref:           row.Ref,
		CommitSHA:     row.CommitSha,
		DeploymentID:  uuidFromPG(row.DeploymentID),
		ReceivedAt:    timeFromPG(row.ReceivedAt),
	}, nil
}

func (r *storeRepository) LinkEventDeployment(ctx context.Context, eventID, deploymentID uuid.UUID) error {
	return r.store.UpdateWebhookEventDeployment(ctx, pgUUID(eventID), pgUUID(deploymentID))
}

func (r *storeRepository) ReleaseEvent(ctx context.Context, eventID uuid.UUID) error {
	return r.store.DeleteWebhookEvent(ctx, pgUUID(eventID))
}

func (r *storeRepository) WritePreviewBinding(ctx context.Context, write PreviewBindingWrite) (PreviewBindingWriteResult, error) {
	result, err := r.store.WritePreviewBinding(ctx, store.PreviewBindingWriteParams{
		ApplicationID:        pgUUID(write.ApplicationID),
		PrNumber:             int32(write.PRNumber),
		ReservationID:        pgUUID(write.ReservationID),
		LeaseHeadSHA:         write.LeaseHeadSHA,
		HeadSHA:              write.HeadSHA,
		ConsumeLease:         write.ConsumeLease,
		LiveLimit:            int64(write.LiveLimit),
		TeamID:               pgUUID(write.TeamID),
		Provider:             write.Provider,
		Repo:                 write.Repo,
		Branch:               write.Branch,
		Host:                 write.Host,
		State:                write.State,
		PreviewApplicationID: pgUUID(write.PreviewApplicationID),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return PreviewBindingWriteResult{}, fmt.Errorf("%w: application not found", ErrRetryable)
		}
		return PreviewBindingWriteResult{}, err
	}
	mapped := PreviewBindingWriteResult{Refused: result.Refused}
	if result.Refused == "" {
		binding := previewFromRow(result.Binding)
		mapped.Binding = &binding
	}
	return mapped, nil
}

func (r *storeRepository) GetPreview(ctx context.Context, appID uuid.UUID, prNumber int) (Preview, error) {
	row, err := r.store.GetPreviewDeploy(ctx, pgUUID(appID), int32(prNumber))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Preview{}, ErrNotFound
		}
		return Preview{}, err
	}
	return previewFromRow(row), nil
}

func (r *storeRepository) GetPreviewByApplication(ctx context.Context, previewAppID uuid.UUID) (Preview, error) {
	row, err := r.store.GetLivePreviewDeployBySibling(ctx, pgUUID(previewAppID))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Preview{}, ErrNotFound
		}
		return Preview{}, err
	}
	return previewFromRow(row), nil
}

func (r *storeRepository) UpsertPreview(ctx context.Context, preview Preview) (Preview, error) {
	row, err := r.store.UpsertPreviewDeploy(ctx, sqlc.UpsertPreviewDeployParams{
		ApplicationID:        pgUUID(preview.ApplicationID),
		TeamID:               pgUUID(preview.TeamID),
		Provider:             preview.Provider,
		Repo:                 preview.Repo,
		PrNumber:             int32(preview.PRNumber),
		Branch:               preview.Branch,
		HeadSha:              preview.HeadSHA,
		PreviewApplicationID: pgUUID(preview.PreviewApplicationID),
		Host:                 preview.Host,
		State:                preview.State,
	})
	if err != nil {
		return Preview{}, err
	}
	return previewFromRow(row), nil
}

func (r *storeRepository) ListPreviews(ctx context.Context, appID uuid.UUID) ([]Preview, error) {
	rows, err := r.store.ListPreviewDeploysByApplication(ctx, pgUUID(appID))
	if err != nil {
		return nil, err
	}
	previews := make([]Preview, 0, len(rows))
	for _, row := range rows {
		previews = append(previews, previewFromRow(row))
	}
	return previews, nil
}

func (r *storeRepository) ClaimPreviewDelivery(ctx context.Context, claim PreviewClaim) (PreviewClaimResult, error) {
	result, err := r.store.ClaimPreviewDelivery(ctx, store.PreviewClaimParams{
		ApplicationID: pgUUID(claim.ApplicationID),
		PrNumber:      int32(claim.PRNumber),
		Kind:          claim.Kind,
		HeadSHA:       claim.HeadSHA,
		DeliveryID:    claim.DeliveryID,
		LiveLimit:     int64(claim.LiveLimit),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return PreviewClaimResult{}, fmt.Errorf("%w: application not found", ErrRetryable)
		}
		return PreviewClaimResult{}, err
	}
	mapped := PreviewClaimResult{
		Approved:  result.Approved,
		Duplicate: result.Duplicate,
		Limit:     result.Limit,
		Retryable: result.Retryable,
	}
	if result.Approved {
		mapped.Reservation = reservationFromRow(result.Reservation)
	}
	if result.Binding != nil {
		binding := previewFromRow(*result.Binding)
		mapped.Binding = &binding
	}
	return mapped, nil
}

func (r *storeRepository) ReleasePreviewDelivery(ctx context.Context, reservationID uuid.UUID) error {
	return r.store.ReleasePreviewDelivery(ctx, pgUUID(reservationID))
}

func (r *storeRepository) ClearPreviewDeliveries(ctx context.Context, appID uuid.UUID, prNumber int) error {
	return r.store.ClearPreviewDeliveries(ctx, pgUUID(appID), int32(prNumber))
}

func (r *storeRepository) MarkPreviewClosing(ctx context.Context, previewID uuid.UUID) (Preview, error) {
	row, err := r.store.MarkPreviewClosing(ctx, pgUUID(previewID))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Preview{}, ErrNotFound
		}
		return Preview{}, err
	}
	return previewFromRow(row), nil
}

func (r *storeRepository) MarkPreviewClosed(ctx context.Context, appID uuid.UUID, prNumber int) (Preview, error) {
	row, err := r.store.MarkPreviewClosed(ctx, pgUUID(appID), int32(prNumber))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Preview{}, ErrNotFound
		}
		return Preview{}, err
	}
	return previewFromRow(row), nil
}

func (r *storeRepository) ListClosingPreviews(ctx context.Context, before time.Time) ([]Preview, error) {
	rows, err := r.store.ListClosingPreviewDeploys(ctx, pgtype.Timestamptz{Time: before, Valid: true})
	if err != nil {
		return nil, err
	}
	previews := make([]Preview, 0, len(rows))
	for _, row := range rows {
		previews = append(previews, previewFromRow(row))
	}
	return previews, nil
}

func (r *storeRepository) PurgeExpiredPreviewReservations(ctx context.Context) (int, error) {
	n, err := r.store.PurgeExpiredPreviewDeliveries(ctx)
	return int(n), err
}

func (r *storeRepository) ListOrphanedPreviews(ctx context.Context) ([]Preview, error) {
	rows, err := r.store.ListOrphanedPreviewDeploys(ctx)
	if err != nil {
		return nil, err
	}
	previews := make([]Preview, 0, len(rows))
	for _, row := range rows {
		previews = append(previews, previewFromRow(row))
	}
	return previews, nil
}

func (r *storeRepository) ListOrphanedPreviewApplications(ctx context.Context, createdBefore time.Time) ([]uuid.UUID, error) {
	rows, err := r.store.ListOrphanedPreviewApplications(ctx, pgtype.Timestamptz{Time: createdBefore, Valid: true})
	if err != nil {
		return nil, err
	}
	ids := make([]uuid.UUID, 0, len(rows))
	for _, row := range rows {
		id := uuidFromPG(row)
		if id != uuid.Nil {
			ids = append(ids, id)
		}
	}
	return ids, nil
}

func (r *storeRepository) MarkPreviewsDeletedForSibling(ctx context.Context, previewAppID uuid.UUID) error {
	return r.store.MarkPreviewDeploysDeletedForSibling(ctx, pgUUID(previewAppID))
}

func reservationFromRow(row sqlc.PreviewDelivery) DeliveryReservation {
	return DeliveryReservation{
		ID:            uuidFromPG(row.ID),
		ApplicationID: uuidFromPG(row.ApplicationID),
		PRNumber:      int(row.PrNumber),
		Kind:          row.Kind,
		HeadSHA:       row.HeadSha,
		DeliveryID:    row.DeliveryID,
		ReceivedAt:    timeFromPG(row.ReceivedAt),
		ExpiresAt:     timeFromPG(row.ExpiresAt),
	}
}

func previewFromRow(row sqlc.PreviewDeploy) Preview {
	return Preview{
		ID:                   uuidFromPG(row.ID),
		ApplicationID:        uuidFromPG(row.ApplicationID),
		TeamID:               uuidFromPG(row.TeamID),
		Provider:             row.Provider,
		Repo:                 row.Repo,
		PRNumber:             int(row.PrNumber),
		Branch:               row.Branch,
		HeadSHA:              row.HeadSha,
		PreviewApplicationID: uuidFromPG(row.PreviewApplicationID),
		Host:                 row.Host,
		State:                row.State,
		CreatedAt:            timeFromPG(row.CreatedAt),
		UpdatedAt:            timeFromPG(row.UpdatedAt),
		DeletedAt:            timeFromPG(row.DeletedAt),
	}
}

func hookFromRow(row sqlc.ApplicationWebhook) Hook {
	return Hook{
		ID:            uuidFromPG(row.ID),
		ApplicationID: uuidFromPG(row.ApplicationID),
		Provider:      row.Provider,
		Repo:          row.Repo,
		HookID:        row.HookID,
		URL:           row.Url,
		CreatedAt:     timeFromPG(row.CreatedAt),
	}
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

func pgUUID(id uuid.UUID) pgtype.UUID {
	if id == uuid.Nil {
		return pgtype.UUID{}
	}
	return pgtype.UUID{Bytes: id, Valid: true}
}

func uuidFromPG(v pgtype.UUID) uuid.UUID {
	if !v.Valid {
		return uuid.Nil
	}
	return uuid.UUID(v.Bytes)
}

func timeFromPG(v pgtype.Timestamptz) time.Time {
	if !v.Valid {
		return time.Time{}
	}
	return v.Time
}
