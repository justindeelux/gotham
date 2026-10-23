package providers

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/justindeelux/gotham/internal/store"
	"github.com/justindeelux/gotham/internal/store/sqlc"
)

type Repository interface {
	Create(ctx context.Context, p Provider) (Provider, error)
	Get(ctx context.Context, id, userID uuid.UUID) (Provider, error)
	List(ctx context.Context, userID uuid.UUID) ([]Provider, error)
	UpdateToken(ctx context.Context, id uuid.UUID, accessToken, refreshToken string, expiresAt *time.Time) (Provider, error)
	Delete(ctx context.Context, id, userID uuid.UUID) error
	ReplaceRepos(ctx context.Context, providerID uuid.UUID, repos []Repo) error
	ListCachedRepos(ctx context.Context, providerID uuid.UUID) ([]Repo, error)
}

type storeRepository struct {
	store  *store.Store
	cipher *secretCipher
}

func newStoreRepository(st *store.Store, c *secretCipher) *storeRepository {
	return &storeRepository{store: st, cipher: c}
}

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
		if isUniqueViolation(err) {
			return Provider{}, fmt.Errorf("%w: this provider is already connected", ErrConflict)
		}
		return Provider{}, fmt.Errorf("providers: create: %w", err)
	}
	return r.providerFromRow(row)
}

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

func (r *storeRepository) Delete(ctx context.Context, id, userID uuid.UUID) error {
	if err := r.store.DeleteProviderByIDAndUser(ctx, sqlc.DeleteProviderByIDAndUserParams{
		ID:     pgUUID(id),
		UserID: pgUUID(userID),
	}); err != nil {
		return fmt.Errorf("providers: delete: %w", err)
	}
	return nil
}

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

func uuidFromPG(id pgtype.UUID) uuid.UUID {
	if !id.Valid {
		return uuid.Nil
	}
	return id.Bytes
}

func pgTime(t *time.Time) pgtype.Timestamptz {
	if t == nil || t.IsZero() {
		return pgtype.Timestamptz{}
	}
	return pgtype.Timestamptz{Time: *t, Valid: true}
}

func timeFromPG(t pgtype.Timestamptz) *time.Time {
	if !t.Valid {
		return nil
	}
	value := t.Time
	return &value
}
