package githubapp

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/justindeelux/gotham/internal/store"
	"github.com/justindeelux/gotham/internal/store/sqlc"
)

// storeRepository adapts *store.Store to Repository. Sealed secrets cross this
// boundary still sealed; the service opens them only for signing and
// verification.
type storeRepository struct {
	store *store.Store
}

// newStoreRepository builds the PostgreSQL-backed repository.
func newStoreRepository(st *store.Store) *storeRepository {
	return &storeRepository{store: st}
}

// CreateApp stores a new GitHub App with its sealed secrets.
func (r *storeRepository) CreateApp(ctx context.Context, app GitHubApp, webhookSecret, privateKey string) (GitHubApp, error) {
	row, err := r.store.CreateGitHubApp(ctx, sqlc.CreateGitHubAppParams{
		UserID:              pgUUID(app.UserID),
		AppID:               app.AppID,
		Slug:                app.Slug,
		Name:                app.Name,
		BaseUrl:             app.BaseURL,
		ApiBaseUrl:          app.APIBaseURL,
		ClientID:            app.ClientID,
		WebhookSecretCipher: webhookSecret,
		PrivateKeyCipher:    privateKey,
	})
	if err != nil {
		return GitHubApp{}, fmt.Errorf("githubapp: create: %w", err)
	}
	return appFromRow(row), nil
}

// GetApp loads one app owned by userID with its installations.
func (r *storeRepository) GetApp(ctx context.Context, id, userID uuid.UUID) (GitHubApp, error) {
	row, err := r.store.GetGitHubAppByIDAndUser(ctx, sqlc.GetGitHubAppByIDAndUserParams{
		ID:     pgUUID(id),
		UserID: pgUUID(userID),
	})
	if err != nil {
		return GitHubApp{}, fmt.Errorf("%w: %w", ErrNotFound, err)
	}
	app := appFromRow(row)
	insts, err := r.store.ListGitHubInstallations(ctx, pgUUID(id))
	if err != nil {
		return GitHubApp{}, fmt.Errorf("githubapp: installations: %w", err)
	}
	for _, inst := range insts {
		app.Installations = append(app.Installations, installationFromRow(inst))
	}
	return app, nil
}

// GetSealed loads one app owned by userID with its still-sealed secrets.
func (r *storeRepository) GetSealed(ctx context.Context, id, userID uuid.UUID) (sealedApp, error) {
	row, err := r.store.GetGitHubAppByIDAndUser(ctx, sqlc.GetGitHubAppByIDAndUserParams{
		ID:     pgUUID(id),
		UserID: pgUUID(userID),
	})
	if err != nil {
		return sealedApp{}, fmt.Errorf("%w: %w", ErrNotFound, err)
	}
	return sealedFromRow(row), nil
}

// GetSealedByID loads one app with its still-sealed secrets without a user
// scope, for webhook handling where the signature is the authentication.
func (r *storeRepository) GetSealedByID(ctx context.Context, id uuid.UUID) (sealedApp, error) {
	row, err := r.store.GetGitHubAppByID(ctx, pgUUID(id))
	if err != nil {
		return sealedApp{}, fmt.Errorf("%w: %w", ErrNotFound, err)
	}
	return sealedFromRow(row), nil
}

// ListApps returns every app owned by userID with installations.
func (r *storeRepository) ListApps(ctx context.Context, userID uuid.UUID) ([]GitHubApp, error) {
	rows, err := r.store.ListGitHubAppsByUser(ctx, pgUUID(userID))
	if err != nil {
		return nil, fmt.Errorf("githubapp: list: %w", err)
	}
	apps := make([]GitHubApp, 0, len(rows))
	for _, row := range rows {
		app := appFromRow(row)
		insts, err := r.store.ListGitHubInstallations(ctx, pgUUID(app.ID))
		if err != nil {
			return nil, fmt.Errorf("githubapp: installations: %w", err)
		}
		for _, inst := range insts {
			app.Installations = append(app.Installations, installationFromRow(inst))
		}
		apps = append(apps, app)
	}
	return apps, nil
}

// DeleteApp removes an app and, by foreign key, its installations and cache.
func (r *storeRepository) DeleteApp(ctx context.Context, id, userID uuid.UUID) error {
	n, err := r.store.DeleteGitHubApp(ctx, sqlc.DeleteGitHubAppParams{
		ID:     pgUUID(id),
		UserID: pgUUID(userID),
	})
	if err != nil {
		return fmt.Errorf("githubapp: delete: %w", err)
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// UpsertInstallation records an installation of an app.
func (r *storeRepository) UpsertInstallation(ctx context.Context, appID uuid.UUID, installationID int64, account string) (Installation, error) {
	row, err := r.store.UpsertGitHubInstallation(ctx, sqlc.UpsertGitHubInstallationParams{
		GithubAppID:    pgUUID(appID),
		InstallationID: installationID,
		Account:        account,
	})
	if err != nil {
		return Installation{}, fmt.Errorf("githubapp: installation: %w", err)
	}
	return installationFromRow(row), nil
}

// ListInstallations returns the installations of one app.
func (r *storeRepository) ListInstallations(ctx context.Context, appID uuid.UUID) ([]Installation, error) {
	rows, err := r.store.ListGitHubInstallations(ctx, pgUUID(appID))
	if err != nil {
		return nil, fmt.Errorf("githubapp: installations: %w", err)
	}
	insts := make([]Installation, 0, len(rows))
	for _, row := range rows {
		insts = append(insts, installationFromRow(row))
	}
	return insts, nil
}

// DeleteInstallation removes one installation (and its cache by foreign key)
// after GitHub reports it deleted.
func (r *storeRepository) DeleteInstallation(ctx context.Context, installationID int64, appID uuid.UUID) error {
	rows, err := r.store.ListGitHubInstallations(ctx, pgUUID(appID))
	if err != nil {
		return fmt.Errorf("githubapp: installations: %w", err)
	}
	for _, row := range rows {
		if row.InstallationID == installationID {
			if _, err := r.store.DeleteGitHubInstallation(ctx, row.ID); err != nil {
				return fmt.Errorf("githubapp: delete installation: %w", err)
			}
			return nil
		}
	}
	return nil
}

// AppsByInstallationID returns every app holding an installation (with sealed
// secrets), so webhook verification can try each candidate secret.
func (r *storeRepository) AppsByInstallationID(ctx context.Context, installationID int64) ([]sealedApp, error) {
	rows, err := r.store.ListGitHubAppsByInstallationID(ctx, installationID)
	if err != nil {
		return nil, fmt.Errorf("githubapp: by installation: %w", err)
	}
	apps := make([]sealedApp, 0, len(rows))
	for _, row := range rows {
		apps = append(apps, sealedFromRow(row))
	}
	return apps, nil
}

// ReplaceRepoCache atomically overwrites the cached repository list of one
// installation: repos revoked upstream disappear instead of lingering.
func (r *storeRepository) ReplaceRepoCache(ctx context.Context, appID uuid.UUID, installationID int64, repos []Repo) error {
	params := make([]sqlc.UpsertGitHubRepoCacheParams, 0, len(repos))
	for _, repo := range repos {
		params = append(params, sqlc.UpsertGitHubRepoCacheParams{
			GithubAppID:    pgUUID(appID),
			InstallationID: installationID,
			ExternalID:     repo.ExternalID,
			Name:           repo.Name,
			FullName:       repo.FullName,
			Private:        repo.Private,
			DefaultBranch:  repo.DefaultBranch,
			CloneUrl:       repo.CloneURL,
			SshUrl:         repo.SSHURL,
			HtmlUrl:        repo.HTMLURL,
		})
	}
	if err := r.store.ReplaceGitHubRepoCache(ctx, pgUUID(appID), installationID, params); err != nil {
		return fmt.Errorf("githubapp: cache: %w", err)
	}
	return nil
}

// ListRepoCache returns the cached repository list of one installation.
func (r *storeRepository) ListRepoCache(ctx context.Context, appID uuid.UUID, installationID int64) ([]Repo, error) {
	rows, err := r.store.ListGitHubRepoCache(ctx, sqlc.ListGitHubRepoCacheParams{
		GithubAppID:    pgUUID(appID),
		InstallationID: installationID,
	})
	if err != nil {
		return nil, fmt.Errorf("githubapp: cache: %w", err)
	}
	repos := make([]Repo, 0, len(rows))
	for _, row := range rows {
		repos = append(repos, Repo{
			ExternalID:    row.ExternalID,
			Name:          row.Name,
			FullName:      row.FullName,
			Private:       row.Private,
			DefaultBranch: row.DefaultBranch,
			CloneURL:      row.CloneUrl,
			SSHURL:        row.SshUrl,
			HTMLURL:       row.HtmlUrl,
		})
	}
	return repos, nil
}

// CountApplicationsForApp counts the caller's github_app applications whose
// repo is granted to this connection's installations.
func (r *storeRepository) CountApplicationsForApp(ctx context.Context, userID, appID uuid.UUID) (int64, error) {
	n, err := r.store.CountGitHubAppApplicationsForApp(ctx, sqlc.CountGitHubAppApplicationsForAppParams{
		UserID:      pgUUID(userID),
		GithubAppID: pgUUID(appID),
	})
	if err != nil {
		return 0, fmt.Errorf("githubapp: count applications: %w", err)
	}
	return n, nil
}

// PushTargets returns the github_app applications of the app's owner watching
// repo (lowercased owner/name), for webhook push routing.
func (r *storeRepository) PushTargets(ctx context.Context, appID, userID uuid.UUID, repo string) ([]AppPushTarget, error) {
	rows, err := r.store.ListGitHubAppPushTargets(ctx, sqlc.ListGitHubAppPushTargetsParams{
		UserID: pgUUID(userID),
		Repo:   repo,
	})
	if err != nil {
		return nil, fmt.Errorf("githubapp: push targets: %w", err)
	}
	targets := make([]AppPushTarget, 0, len(rows))
	for _, row := range rows {
		targets = append(targets, AppPushTarget{
			ApplicationID: uuidFromPG(row.ID),
			Branch:        row.Branch,
		})
	}
	return targets, nil
}

func appFromRow(row sqlc.GithubApp) GitHubApp {
	return GitHubApp{
		ID:         uuidFromPG(row.ID),
		UserID:     uuidFromPG(row.UserID),
		AppID:      row.AppID,
		Slug:       row.Slug,
		Name:       row.Name,
		BaseURL:    row.BaseUrl,
		APIBaseURL: row.ApiBaseUrl,
		ClientID:   row.ClientID,
		CreatedAt:  row.CreatedAt.Time,
		UpdatedAt:  row.UpdatedAt.Time,
	}
}

func sealedFromRow(row sqlc.GithubApp) sealedApp {
	return sealedApp{
		GitHubApp:     appFromRow(row),
		WebhookSecret: row.WebhookSecretCipher,
		PrivateKey:    row.PrivateKeyCipher,
	}
}

func installationFromRow(row sqlc.GithubInstallation) Installation {
	return Installation{
		ID:             uuidFromPG(row.ID),
		InstallationID: row.InstallationID,
		Account:        row.Account,
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
