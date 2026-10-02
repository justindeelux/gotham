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

// Hook is the webhook installed for an application. Secret is deliberately
// absent: it is used to verify deliveries and never leaves this package.
type Hook struct {
	ID            uuid.UUID
	ApplicationID uuid.UUID
	Provider      string
	Repo          string
	HookID        string
	URL           string
	CreatedAt     time.Time
}

// Target is an installed hook joined with the application watching it — the
// record an incoming delivery is matched against. UserID is the application's
// creator, whose stored provider connection authenticates the API calls a
// delivery triggers (the preview comment); TeamID and the name/domain are what
// the preview decision and the sibling clone are derived from.
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

// Preview is the binding between a pull request and the sibling application
// that serves its preview (preview_deploys). The base application is the row
// whose webhook watched the PR; PreviewApplicationID is the clone.
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

// Reservation kinds: a start delivery holds an in-flight lease for one PR head
// revision, a close delivery holds the teardown marker.
const (
	ReservationStart = "start"
	ReservationClose = "close"
)

// DeliveryReservation is one row of preview_deliveries: the preview-specific
// in-flight lease. It deliberately lives outside webhook_events (the push
// ledger): a PR head must never suppress — or be suppressed by — a push of the
// same commit. A lease is removed when its delivery finishes and expires if
// its process dies, so it is never a permanent handled set.
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

// PreviewClaim is one delivery decision request for the preview-specific
// atomic transition (see Repository.ClaimPreviewDelivery).
type PreviewClaim struct {
	ApplicationID uuid.UUID
	PRNumber      int
	Kind          string
	HeadSHA       string
	DeliveryID    string
	// LiveLimit caps distinct live-or-in-flight pull requests. Zero disables
	// the cap.
	LiveLimit int
}

// PreviewClaimResult is the outcome of a claim. Exactly one of
// Approved/Duplicate/Limit/Retryable is set.
type PreviewClaimResult struct {
	Approved  bool
	Duplicate bool
	Limit     bool
	Retryable bool
	// Reservation is the inserted ledger row (Approved only).
	Reservation DeliveryReservation
	// Binding is the current binding, nil when the PR has none.
	Binding *Preview
}

// Fence refusals reported by WritePreviewBinding.
const (
	// BindingRefusedLease: the claim lease is gone or expired (a stale
	// worker); the write must not promote a binding.
	BindingRefusedLease = "lease"
	// BindingRefusedClosing: a close transition owns the preview.
	BindingRefusedClosing = "closing"
	// BindingRefusedLimit: the preview quota is full.
	BindingRefusedLimit = "limit"
)

// PreviewBindingWrite is one fenced binding promotion: the pending write of a
// preview worker, authorized by the claim lease it was issued.
type PreviewBindingWrite struct {
	ApplicationID uuid.UUID
	PRNumber      int
	// ReservationID is the claim lease that authorizes the write.
	ReservationID uuid.UUID
	// LeaseHeadSHA is the revision the lease was issued for; HeadSHA is what
	// the binding records (an intermediate promotion keeps the previously
	// queued revision).
	LeaseHeadSHA string
	HeadSHA      string
	// ConsumeLease removes the lease on success (the final promotion).
	ConsumeLease bool
	// LiveLimit re-checks the quota when the write would make the binding
	// live (zero disables the check).
	LiveLimit            int
	TeamID               uuid.UUID
	Provider             string
	Repo                 string
	Branch               string
	Host                 string
	State                string
	PreviewApplicationID uuid.UUID
}

// PreviewBindingWriteResult is the outcome of a fenced write. Binding is set
// when it went through; Refused names the fence that stopped it otherwise.
type PreviewBindingWriteResult struct {
	Binding *Preview
	Refused string
}

// Event is one claimed delivery: the anti-spam ledger row that makes a
// repeated commit SHA a no-op.
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

// Repository persists hooks and delivery claims. It is implemented over
// *store.Store in production and by fakes in tests.
type Repository interface {
	// GetApplication returns the application, or ErrNotFound. Team
	// authorization is the service's job (it resolves the request scope), so
	// the lookup itself is creator-independent.
	GetApplication(ctx context.Context, appID uuid.UUID) (Application, error)
	// GetWebhook returns the hook of an application, or ErrNotFound.
	GetWebhook(ctx context.Context, appID uuid.UUID) (Hook, error)
	// CreateWebhook stores the first hook of an application. A second row for
	// the same application is a unique-constraint violation (ErrConflict).
	CreateWebhook(ctx context.Context, hook Hook, secret string) (Hook, error)
	// DeleteWebhook removes the hook of an application and returns it, or
	// ErrNotFound when there was nothing to delete.
	DeleteWebhook(ctx context.Context, appID uuid.UUID) (Hook, error)
	// Targets returns every hook watching repo (case-insensitive) with the
	// application it belongs to, secrets opened.
	Targets(ctx context.Context, provider, repo string) ([]Target, error)
	// ClaimEvent records a delivery. A duplicate commit SHA or delivery ID
	// surfaces as ErrDuplicate together with the existing event, whose
	// DeploymentID tells the caller whether the winning claim is durable.
	ClaimEvent(ctx context.Context, event Event) (Event, error)
	// ReleaseEvent removes a claim whose deployment was never queued, so the
	// host's retry of that delivery is not mistaken for spam.
	ReleaseEvent(ctx context.Context, eventID uuid.UUID) error
	// LinkEventDeployment attaches the queued deployment to a claimed event.
	LinkEventDeployment(ctx context.Context, eventID, deploymentID uuid.UUID) error
	// ClaimPreviewDelivery runs the atomic per-(application, PR) delivery
	// decision: lock the application, purge its expired leases, dedupe a start
	// against the live binding's current head (or an in-flight attempt for
	// that head only), enforce the live-preview cap for a new preview, and
	// insert the in-flight lease. The decision is carried by the result
	// (Approved/Duplicate/Limit/Retryable); a returned error means the
	// decision could not be made.
	ClaimPreviewDelivery(ctx context.Context, claim PreviewClaim) (PreviewClaimResult, error)
	// ReleasePreviewDelivery removes a lease that never queued a deployment,
	// so the host's retry can reserve again. An already-gone reservation is a
	// success.
	ReleasePreviewDelivery(ctx context.Context, reservationID uuid.UUID) error
	// ClearPreviewDeliveries removes every reservation of one pull request at
	// a lifecycle transition (close), so a reopen can reserve again.
	ClearPreviewDeliveries(ctx context.Context, appID uuid.UUID, prNumber int) error
	// MarkPreviewClosing persists the close intent on one binding, so a failed
	// teardown is re-attempted by the sweep.
	MarkPreviewClosing(ctx context.Context, previewID uuid.UUID) (Preview, error)
	// MarkPreviewClosed completes a close atomically: the binding becomes
	// deleted and the PR's ledger is cleared in one transaction. It is
	// idempotent and runs on the already-deleted retry path too, so a stale
	// reservation can never poison a reopen.
	MarkPreviewClosed(ctx context.Context, appID uuid.UUID, prNumber int) (Preview, error)
	// ListClosingPreviews returns bindings whose close intent is older than
	// before — the sweep's teardown-retry list.
	ListClosingPreviews(ctx context.Context, before time.Time) ([]Preview, error)
	// PurgeExpiredPreviewReservations removes every expired ledger row and
	// reports how many went.
	PurgeExpiredPreviewReservations(ctx context.Context) (int, error)
	// WritePreviewBinding stores a preview worker's binding under the
	// base-application lock, fenced by its claim lease (still present and
	// unexpired), the closing state (a close owns the preview) and the quota.
	// The refused write is reported, never applied; production promotion goes
	// through this method.
	WritePreviewBinding(ctx context.Context, write PreviewBindingWrite) (PreviewBindingWriteResult, error)
	// GetPreview returns the preview binding of one pull request, or
	// ErrNotFound when the PR has no preview yet.
	GetPreview(ctx context.Context, appID uuid.UUID, prNumber int) (Preview, error)
	// GetPreviewByApplication returns the live binding a preview sibling
	// application backs, or ErrNotFound. The terminal deploy comment hook uses
	// it; a sibling backs at most one binding, and a deleted binding is not a
	// target.
	GetPreviewByApplication(ctx context.Context, previewAppID uuid.UUID) (Preview, error)
	// UpsertPreview stores or refreshes the binding without the promotion
	// fence. It is the seeding seam for tests and the audit tooling; the
	// delivery path uses ClaimPreviewDelivery + WritePreviewBinding.
	UpsertPreview(ctx context.Context, preview Preview) (Preview, error)
	// ListPreviews returns an application's preview bindings, newest first.
	ListPreviews(ctx context.Context, appID uuid.UUID) ([]Preview, error)
	// ListOrphanedPreviews returns live bindings whose sibling application is
	// gone (or was never linked) — the orphan sweep's binding pass.
	ListOrphanedPreviews(ctx context.Context) ([]Preview, error)
	// ListOrphanedPreviewApplications returns preview sibling applications
	// with no binding at all, created before the grace cutoff — the orphan
	// sweep's application pass.
	ListOrphanedPreviewApplications(ctx context.Context, createdBefore time.Time) ([]uuid.UUID, error)
	// MarkPreviewsDeletedForSibling marks the bindings that point at a sibling
	// application being deleted directly (the user-facing delete path).
	MarkPreviewsDeletedForSibling(ctx context.Context, previewAppID uuid.UUID) error
}

// Application is the slice of an application the hook lifecycle needs.
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

// storeRepository adapts *store.Store to Repository, sealing hook secrets at
// rest with the same AES-256-GCM helper the application secrets use.
type storeRepository struct {
	store  *store.Store
	secret string
	logger *slog.Logger
}

// newStoreRepository builds the PostgreSQL-backed repository. secret is the
// key providers.SealSecret seals hook secrets with; an empty value keeps the
// behaviour of the deploy package (deterministic, configuration-dependent key)
// rather than silently invalidating installed hooks on restart. logger is used
// to surface a hook whose secret cannot be opened (never its material).
func newStoreRepository(st *store.Store, secret string, logger *slog.Logger) *storeRepository {
	if logger == nil {
		logger = slog.Default()
	}
	return &storeRepository{store: st, secret: secret, logger: logger}
}

// GetApplication loads one application by ID.
func (r *storeRepository) GetApplication(ctx context.Context, appID uuid.UUID) (Application, error) {
	row, err := r.store.GetApplication(ctx, pgUUID(appID))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Application{}, ErrNotFound
		}
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
		BaseDomain: row.BaseDomain,
	}, nil
}

// GetWebhook loads the hook of an application with its secret opened.
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

// CreateWebhook stores the hook of an application, sealing its secret.
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

// DeleteWebhook removes the hook of an application.
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

// Targets loads every hook watching repo, joined with its application.
func (r *storeRepository) Targets(ctx context.Context, provider, repo string) ([]Target, error) {
	rows, err := r.store.ListWebhookTargetsForRepo(ctx, provider, repo)
	if err != nil {
		return nil, err
	}
	targets := make([]Target, 0, len(rows))
	for _, row := range rows {
		secret, err := providers.OpenSecret(r.secret, row.Secret)
		if err != nil {
			// A secret that cannot be opened (rotated key, corrupted row) can
			// never verify a delivery; skip the target instead of failing the
			// whole request and hiding the remaining ones. Log it: otherwise
			// every delivery to this hook answers 401 with no trace of why
			// automatic deploys silently stopped working.
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

// ClaimEvent records a delivery. A duplicate commit SHA or delivery ID
// surfaces as ErrDuplicate together with the existing event, so the caller can
// tell whether the winning claim already produced a deployment (a durable
// no-op) or is still in flight (the duplicate must not be acknowledged yet).
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
			// The colliding claim was released concurrently: report the
			// duplicate with no row, which the caller answers retryable.
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

// LinkEventDeployment attaches a deployment ID to a claimed delivery.
func (r *storeRepository) LinkEventDeployment(ctx context.Context, eventID, deploymentID uuid.UUID) error {
	return r.store.UpdateWebhookEventDeployment(ctx, pgUUID(eventID), pgUUID(deploymentID))
}

// ReleaseEvent removes a claim again. A claim that is already gone is a
// success: releasing twice must not fail.
func (r *storeRepository) ReleaseEvent(ctx context.Context, eventID uuid.UUID) error {
	return r.store.DeleteWebhookEvent(ctx, pgUUID(eventID))
}

// WritePreviewBinding runs the store's fenced promotion and maps its refusal
// to the domain result.
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

// GetPreview loads the preview binding of one (application, pull request).
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

// GetPreviewByApplication loads the live binding a preview sibling backs.
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

// UpsertPreview stores or refreshes the binding of one pull request.
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

// ListPreviews returns an application's preview bindings, newest first.
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

// ClaimPreviewDelivery runs the store's atomic claim transaction and maps its
// outcome to the domain result.
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
			// The base application vanished between the hook join and the
			// claim: nothing durable exists to act on.
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

// ReleasePreviewDelivery removes a reservation that never queued a
// deployment.
func (r *storeRepository) ReleasePreviewDelivery(ctx context.Context, reservationID uuid.UUID) error {
	return r.store.ReleasePreviewDelivery(ctx, pgUUID(reservationID))
}

// ClearPreviewDeliveries removes a pull request's reservations.
func (r *storeRepository) ClearPreviewDeliveries(ctx context.Context, appID uuid.UUID, prNumber int) error {
	return r.store.ClearPreviewDeliveries(ctx, pgUUID(appID), int32(prNumber))
}

// MarkPreviewClosing persists a close intent on one binding.
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

// MarkPreviewClosed completes a close atomically (binding deleted, ledger
// cleared).
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

// ListClosingPreviews returns bindings whose close intent is older than
// before.
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

// PurgeExpiredPreviewReservations removes every expired ledger row.
func (r *storeRepository) PurgeExpiredPreviewReservations(ctx context.Context) (int, error) {
	n, err := r.store.PurgeExpiredPreviewDeliveries(ctx)
	return int(n), err
}

// ListOrphanedPreviews returns live bindings without a live sibling.
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

// ListOrphanedPreviewApplications returns preview siblings created before the
// cutoff that no binding references.
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

// MarkPreviewsDeletedForSibling marks the bindings of a sibling being
// deleted.
func (r *storeRepository) MarkPreviewsDeletedForSibling(ctx context.Context, previewAppID uuid.UUID) error {
	return r.store.MarkPreviewDeploysDeletedForSibling(ctx, pgUUID(previewAppID))
}

// reservationFromRow maps a stored reservation row to the domain type.
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

// previewFromRow maps a stored preview row to the domain type.
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

// hookFromRow maps a stored hook row to the domain type, opening the secret.
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

// isUniqueViolation reports whether err is a PostgreSQL unique-constraint
// violation (SQLSTATE 23505).
func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

// pgUUID converts a domain UUID for sqlc. The zero UUID becomes an invalid
// pgtype value, which serialises as NULL for nullable columns.
func pgUUID(id uuid.UUID) pgtype.UUID {
	if id == uuid.Nil {
		return pgtype.UUID{}
	}
	return pgtype.UUID{Bytes: id, Valid: true}
}

// uuidFromPG converts a sqlc UUID column to the domain type (NULL → zero).
func uuidFromPG(v pgtype.UUID) uuid.UUID {
	if !v.Valid {
		return uuid.Nil
	}
	return uuid.UUID(v.Bytes)
}

// timeFromPG converts a sqlc timestamp column to the domain type.
func timeFromPG(v pgtype.Timestamptz) time.Time {
	if !v.Valid {
		return time.Time{}
	}
	return v.Time
}
