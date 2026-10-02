package providers

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/justindeelux/gotham/internal/store"
	"github.com/justindeelux/gotham/internal/store/sqlc"
)

// Repository persists provider connections and their cached repository lists.
// Credential fields on Provider are plaintext across this boundary; the store
// adapter seals them before they reach the database and opens them on read.
type Repository interface {
	// Create stores a new provider connection.
	Create(ctx context.Context, p Provider) (Provider, error)
	// Get returns the provider with id when it is owned by userID.
	Get(ctx context.Context, id, userID uuid.UUID) (Provider, error)
	// List returns every provider connection owned by userID.
	List(ctx context.Context, userID uuid.UUID) ([]Provider, error)
	// UpdateToken replaces the stored access/refresh tokens.
	UpdateToken(ctx context.Context, id uuid.UUID, accessToken, refreshToken string, expiresAt *time.Time) (Provider, error)
	// ReplaceRepos overwrites the cached repository list of a provider.
	ReplaceRepos(ctx context.Context, providerID uuid.UUID, repos []Repo) error
	// ListCachedRepos returns the cached repository list of a provider.
	ListCachedRepos(ctx context.Context, providerID uuid.UUID) ([]Repo, error)
}

// storeRepository adapts *store.Store to Repository, sealing credentials.
type storeRepository struct {
	store  *store.Store
	cipher *secretCipher
}

// newStoreRepository builds the PostgreSQL-backed repository.
func newStoreRepository(st *store.Store, c *secretCipher) *storeRepository {
	return &storeRepository{store: st, cipher: c}
}

// Create seals the credential fields and stores the row.
func (r *storeRepository) Create(ctx context.Context, p Provider) (Provider, error) {
	clientSecret, err := r.cipher.seal(p.ClientSecret)
	if err != nil {
		return Provider{}, err
	}
	accessToken, err := r.cipher.seal(p.AccessToken)
	if err != nil {
		return Provider{}, err
	}
	refreshToken, err := r.cipher.seal(p.RefreshToken)
	if err != nil {
		return Provider{}, err
	}

	row, err := r.store.CreateProvider(ctx, sqlc.CreateProviderParams{
		UserID:         pgUUID(p.UserID),
		Name:           p.Name,
		BaseUrl:        p.BaseURL,
		ClientID:       p.ClientID,
		ClientSecret:   clientSecret,
		RedirectUrl:    p.RedirectURL,
		AccessToken:    accessToken,
		RefreshToken:   refreshToken,
		TokenExpiresAt: pgTime(p.TokenExpiresAt),
		Scopes:         p.Scopes,
	})
	if err != nil {
		return Provider{}, fmt.Errorf("providers: create: %w", err)
	}
	return r.providerFromRow(row)
}

// Get loads one provider owned by userID, mapping a missing row to ErrNotFound.
func (r *storeRepository) Get(ctx context.Context, id, userID uuid.UUID) (Provider, error) {
	row, err := r.store.GetProviderByIDAndUser(ctx, sqlc.GetProviderByIDAndUserParams{
		ID:     pgUUID(id),
		UserID: pgUUID(userID),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Provider{}, ErrNotFound
		}
		return Provider{}, fmt.Errorf("providers: get: %w", err)
	}
	return r.providerFromRow(row)
}

// List loads every provider owned by userID.
func (r *storeRepository) List(ctx context.Context, userID uuid.UUID) ([]Provider, error) {
	rows, err := r.store.ListProvidersByUser(ctx, pgUUID(userID))
	if err != nil {
		return nil, fmt.Errorf("providers: list: %w", err)
	}

	providers := make([]Provider, 0, len(rows))
	for _, row := range rows {
		provider, err := r.providerFromRow(row)
		if err != nil {
			return nil, err
		}
		providers = append(providers, provider)
	}
	return providers, nil
}

// UpdateToken seals and stores a refreshed token pair.
func (r *storeRepository) UpdateToken(ctx context.Context, id uuid.UUID, accessToken, refreshToken string, expiresAt *time.Time) (Provider, error) {
	sealedAccess, err := r.cipher.seal(accessToken)
	if err != nil {
		return Provider{}, err
	}
	sealedRefresh, err := r.cipher.seal(refreshToken)
	if err != nil {
		return Provider{}, err
	}

	row, err := r.store.UpdateProviderToken(ctx, sqlc.UpdateProviderTokenParams{
		ID:             pgUUID(id),
		AccessToken:    sealedAccess,
		RefreshToken:   sealedRefresh,
		TokenExpiresAt: pgTime(expiresAt),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Provider{}, ErrNotFound
		}
		return Provider{}, fmt.Errorf("providers: update token: %w", err)
	}
	return r.providerFromRow(row)
}

// ReplaceRepos swaps the cached repository list of a provider. The new list is
// built fully before the store replaces the old one in a single transaction, so
// a failed or partial refresh leaves the previous complete list intact.
func (r *storeRepository) ReplaceRepos(ctx context.Context, providerID uuid.UUID, repos []Repo) error {
	params := make([]sqlc.UpsertRepoCacheParams, 0, len(repos))
	for _, repo := range repos {
		params = append(params, sqlc.UpsertRepoCacheParams{
			ProviderID:    pgUUID(providerID),
			ExternalID:    repo.ExternalID,
			Name:          repo.Name,
			FullName:      repo.FullName,
			Private:       repo.Private,
			DefaultBranch: repo.DefaultBranch,
			CloneUrl:      repo.CloneURL,
			SshUrl:        repo.SSHURL,
			HtmlUrl:       repo.HTMLURL,
		})
	}
	if err := r.store.ReplaceRepoCache(ctx, pgUUID(providerID), params); err != nil {
		return fmt.Errorf("providers: replace repo cache: %w", err)
	}
	return nil
}

// ListCachedRepos reads a provider's cached repository list.
func (r *storeRepository) ListCachedRepos(ctx context.Context, providerID uuid.UUID) ([]Repo, error) {
	rows, err := r.store.ListRepoCacheByProvider(ctx, pgUUID(providerID))
	if err != nil {
		return nil, fmt.Errorf("providers: list repo cache: %w", err)
	}

	repos := make([]Repo, 0, len(rows))
	for _, row := range rows {
		repos = append(repos, repoFromRow(row))
	}
	return repos, nil
}

// providerFromRow maps a stored row to the domain type, opening the credentials.
func (r *storeRepository) providerFromRow(row sqlc.Provider) (Provider, error) {
	clientSecret, err := r.cipher.open(row.ClientSecret)
	if err != nil {
		return Provider{}, err
	}
	accessToken, err := r.cipher.open(row.AccessToken)
	if err != nil {
		return Provider{}, err
	}
	refreshToken, err := r.cipher.open(row.RefreshToken)
	if err != nil {
		return Provider{}, err
	}

	return Provider{
		ID:             uuidFromPG(row.ID),
		UserID:         uuidFromPG(row.UserID),
		Name:           row.Name,
		BaseURL:        row.BaseUrl,
		ClientID:       row.ClientID,
		ClientSecret:   clientSecret,
		RedirectURL:    row.RedirectUrl,
		AccessToken:    accessToken,
		RefreshToken:   refreshToken,
		TokenExpiresAt: timeFromPG(row.TokenExpiresAt),
		Scopes:         row.Scopes,
		CreatedAt:      row.CreatedAt.Time,
		UpdatedAt:      row.UpdatedAt.Time,
	}, nil
}

// repoFromRow maps a cached repository row to the domain type.
func repoFromRow(row sqlc.ReposCache) Repo {
	return Repo{
		ExternalID:    row.ExternalID,
		Name:          row.Name,
		FullName:      row.FullName,
		Private:       row.Private,
		DefaultBranch: row.DefaultBranch,
		CloneURL:      row.CloneUrl,
		SSHURL:        row.SshUrl,
		HTMLURL:       row.HtmlUrl,
	}
}

// pgUUID converts a uuid.UUID to the pgx type (invalid for the nil UUID).
func pgUUID(id uuid.UUID) pgtype.UUID {
	if id == uuid.Nil {
		return pgtype.UUID{}
	}
	return pgtype.UUID{Bytes: id, Valid: true}
}

// uuidFromPG converts a pgx UUID to uuid.UUID.
func uuidFromPG(id pgtype.UUID) uuid.UUID {
	if !id.Valid {
		return uuid.Nil
	}
	return id.Bytes
}

// pgTime converts an optional time to the pgx type.
func pgTime(t *time.Time) pgtype.Timestamptz {
	if t == nil || t.IsZero() {
		return pgtype.Timestamptz{}
	}
	return pgtype.Timestamptz{Time: *t, Valid: true}
}

// timeFromPG converts a pgx timestamptz to an optional time.
func timeFromPG(t pgtype.Timestamptz) *time.Time {
	if !t.Valid {
		return nil
	}
	value := t.Time
	return &value
}
