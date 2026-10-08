package store

import (
	"context"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/justindeelux/gotham/internal/store/sqlc"
)

// CreateGitHubApp stores a GitHub App connection and returns the row. The
// cipher fields carry ciphertext produced by the githubapp service.
func (s *Store) CreateGitHubApp(ctx context.Context, params sqlc.CreateGitHubAppParams) (sqlc.GithubApp, error) {
	return s.queries.CreateGitHubApp(ctx, params)
}

// GetGitHubAppByIDAndUser returns the GitHub App with the given ID when it is
// owned by userID.
func (s *Store) GetGitHubAppByIDAndUser(ctx context.Context, params sqlc.GetGitHubAppByIDAndUserParams) (sqlc.GithubApp, error) {
	return s.queries.GetGitHubAppByIDAndUser(ctx, params)
}

// ListGitHubAppsByUser returns every GitHub App owned by userID, newest first.
func (s *Store) ListGitHubAppsByUser(ctx context.Context, userID pgtype.UUID) ([]sqlc.GithubApp, error) {
	return s.queries.ListGitHubAppsByUser(ctx, userID)
}

// DeleteGitHubApp removes a GitHub App owned by userID, returning rows deleted.
func (s *Store) DeleteGitHubApp(ctx context.Context, params sqlc.DeleteGitHubAppParams) (int64, error) {
	return s.queries.DeleteGitHubApp(ctx, params)
}

// GetGitHubAppByID returns one GitHub App without a user scope, for webhook
// handling where the signature is the authentication.
func (s *Store) GetGitHubAppByID(ctx context.Context, id pgtype.UUID) (sqlc.GithubApp, error) {
	return s.queries.GetGitHubAppByID(ctx, id)
}

// UpsertGitHubInstallation records an installation of a GitHub App.
func (s *Store) UpsertGitHubInstallation(ctx context.Context, params sqlc.UpsertGitHubInstallationParams) (sqlc.GithubInstallation, error) {
	return s.queries.UpsertGitHubInstallation(ctx, params)
}

// ListGitHubInstallations returns the installations of one GitHub App.
func (s *Store) ListGitHubInstallations(ctx context.Context, githubAppID pgtype.UUID) ([]sqlc.GithubInstallation, error) {
	return s.queries.ListGitHubInstallations(ctx, githubAppID)
}

// DeleteGitHubInstallation removes one installation row.
func (s *Store) DeleteGitHubInstallation(ctx context.Context, id pgtype.UUID) (int64, error) {
	return s.queries.DeleteGitHubInstallation(ctx, id)
}

// ListGitHubAppsByInstallationID returns every app holding an installation,
// for webhook verification.
func (s *Store) ListGitHubAppsByInstallationID(ctx context.Context, installationID int64) ([]sqlc.GithubApp, error) {
	return s.queries.ListGitHubAppsByInstallationID(ctx, installationID)
}

// UpsertGitHubRepoCache inserts or refreshes one cached repository.
func (s *Store) UpsertGitHubRepoCache(ctx context.Context, params sqlc.UpsertGitHubRepoCacheParams) (sqlc.GithubRepoCache, error) {
	return s.queries.UpsertGitHubRepoCache(ctx, params)
}

// ListGitHubRepoCache returns an installation's cached repositories ordered by
// full name.
func (s *Store) ListGitHubRepoCache(ctx context.Context, params sqlc.ListGitHubRepoCacheParams) ([]sqlc.GithubRepoCache, error) {
	return s.queries.ListGitHubRepoCache(ctx, params)
}

// DeleteGitHubRepoCache removes every cached repository of an installation.
func (s *Store) DeleteGitHubRepoCache(ctx context.Context, params sqlc.DeleteGitHubRepoCacheParams) error {
	return s.queries.DeleteGitHubRepoCache(ctx, params)
}

// CountGitHubAppApplications counts the caller's applications on the
// github_app source.
func (s *Store) CountGitHubAppApplications(ctx context.Context, userID pgtype.UUID) (int64, error) {
	return s.queries.CountGitHubAppApplications(ctx, userID)
}

// ListGitHubAppApplicationNames returns the names of the caller's
// applications linked to one connection, for the disconnect warning.
func (s *Store) ListGitHubAppApplicationNames(ctx context.Context, params sqlc.ListGitHubAppApplicationNamesParams) ([]string, error) {
	return s.queries.ListGitHubAppApplicationNames(ctx, params)
}

// ListGitHubAppPushTargets returns the github_app applications of one owner
// watching a repository, for webhook push routing.
func (s *Store) ListGitHubAppPushTargets(ctx context.Context, params sqlc.ListGitHubAppPushTargetsParams) ([]sqlc.ListGitHubAppPushTargetsRow, error) {
	return s.queries.ListGitHubAppPushTargets(ctx, params)
}

// ReplaceGitHubRepoCache atomically replaces an installation's cached
// repositories: it locks the parent app row, deletes the old list and inserts
// the new one in one transaction, so repos revoked upstream disappear instead
// of lingering, and a partial failure keeps the previous complete list.
func (s *Store) ReplaceGitHubRepoCache(ctx context.Context, appID pgtype.UUID, installationID int64, repos []sqlc.UpsertGitHubRepoCacheParams) error {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var locked pgtype.UUID
	if err := tx.QueryRow(ctx, "SELECT id FROM github_apps WHERE id = $1 FOR UPDATE", appID).Scan(&locked); err != nil {
		return err
	}

	queries := s.queries.WithTx(tx)
	if err := queries.DeleteGitHubRepoCache(ctx, sqlc.DeleteGitHubRepoCacheParams{
		GithubAppID:    appID,
		InstallationID: installationID,
	}); err != nil {
		return err
	}
	for _, repo := range repos {
		if _, err := queries.UpsertGitHubRepoCache(ctx, repo); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}
