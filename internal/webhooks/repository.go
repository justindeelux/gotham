package webhooks

import (
	"context"
	"errors"
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
	// surfaces as ErrDuplicate and must stop the delivery.
	ClaimEvent(ctx context.Context, event Event) (Event, error)
	// ReleaseEvent removes a claim whose deployment was never queued, so the
	// host's retry of that delivery is not mistaken for spam.
	ReleaseEvent(ctx context.Context, eventID uuid.UUID) error
	// LinkEventDeployment attaches the queued deployment to a claimed event.
	LinkEventDeployment(ctx context.Context, eventID, deploymentID uuid.UUID) error
	// GetPreview returns the preview binding of one pull request, or
	// ErrNotFound when the PR has no preview yet.
	GetPreview(ctx context.Context, appID uuid.UUID, prNumber int) (Preview, error)
	// UpsertPreview stores or refreshes the binding of one pull request. The
	// unique (application_id, pr_number) pair keeps a redelivered event
	// idempotent.
	UpsertPreview(ctx context.Context, preview Preview) (Preview, error)
	// ListPreviews returns an application's preview bindings, newest first.
	ListPreviews(ctx context.Context, appID uuid.UUID) ([]Preview, error)
	// MarkPreviewDeleted marks a preview torn down and returns the row.
	MarkPreviewDeleted(ctx context.Context, previewID uuid.UUID) (Preview, error)
	// ListStalePreviews returns live previews whose last activity is older
	// than before — the orphan sweep's work list.
	ListStalePreviews(ctx context.Context, before time.Time) ([]Preview, error)
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
}

// newStoreRepository builds the PostgreSQL-backed repository. secret is the
// key providers.SealSecret seals hook secrets with; an empty value keeps the
// behaviour of the deploy package (deterministic, configuration-dependent key)
// rather than silently invalidating installed hooks on restart.
func newStoreRepository(st *store.Store, secret string) *storeRepository {
	return &storeRepository{store: st, secret: secret}
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
			// whole request and hiding the remaining ones.
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

// ClaimEvent records a delivery, mapping the partial unique indexes to
// ErrDuplicate.
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
		if isUniqueViolation(err) {
			return Event{}, ErrDuplicate
		}
		return Event{}, err
	}
	return Event{
		ID:            uuidFromPG(row.ID),
		ApplicationID: uuidFromPG(row.ApplicationID),
		Provider:      row.Provider,
		Event:         row.Event,
		DeliveryID:    row.DeliveryID,
		Ref:           row.Ref,
		CommitSHA:     row.CommitSha,
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

// MarkPreviewDeleted marks a preview torn down and returns the row.
func (r *storeRepository) MarkPreviewDeleted(ctx context.Context, previewID uuid.UUID) (Preview, error) {
	row, err := r.store.MarkPreviewDeployDeleted(ctx, pgUUID(previewID))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Preview{}, ErrNotFound
		}
		return Preview{}, err
	}
	return previewFromRow(row), nil
}

// ListStalePreviews returns live previews idle since before.
func (r *storeRepository) ListStalePreviews(ctx context.Context, before time.Time) ([]Preview, error) {
	rows, err := r.store.ListStalePreviewDeploys(ctx, pgtype.Timestamptz{Time: before, Valid: true})
	if err != nil {
		return nil, err
	}
	previews := make([]Preview, 0, len(rows))
	for _, row := range rows {
		previews = append(previews, previewFromRow(row))
	}
	return previews, nil
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
