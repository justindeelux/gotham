package store

import (
	"context"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/justindeelux/gotham/internal/store/sqlc"
)

func (s *Store) CreateGitHubApp(ctx context.Context, params sqlc.CreateGitHubAppParams) (sqlc.GithubApp, error) {
	return s.queries.CreateGitHubApp(ctx, params)
}

func (s *Store) GetGitHubAppByIDAndUser(ctx context.Context, params sqlc.GetGitHubAppByIDAndUserParams) (sqlc.GithubApp, error) {
	return s.queries.GetGitHubAppByIDAndUser(ctx, params)
}

func (s *Store) ListGitHubAppsByUser(ctx context.Context, userID pgtype.UUID) ([]sqlc.GithubApp, error) {
	return s.queries.ListGitHubAppsByUser(ctx, userID)
}

func (s *Store) DeleteGitHubApp(ctx context.Context, params sqlc.DeleteGitHubAppParams) (int64, error) {
	return s.queries.DeleteGitHubApp(ctx, params)
}

func (s *Store) GetGitHubAppByID(ctx context.Context, id pgtype.UUID) (sqlc.GithubApp, error) {
	return s.queries.GetGitHubAppByID(ctx, id)
}

func (s *Store) UpsertGitHubInstallation(ctx context.Context, params sqlc.UpsertGitHubInstallationParams) (sqlc.GithubInstallation, error) {
	return s.queries.UpsertGitHubInstallation(ctx, params)
}

func (s *Store) ListGitHubInstallations(ctx context.Context, githubAppID pgtype.UUID) ([]sqlc.GithubInstallation, error) {
	return s.queries.ListGitHubInstallations(ctx, githubAppID)
}

func (s *Store) DeleteGitHubInstallation(ctx context.Context, id pgtype.UUID) (int64, error) {
	return s.queries.DeleteGitHubInstallation(ctx, id)
}

func (s *Store) ListGitHubAppsByInstallationID(ctx context.Context, installationID int64) ([]sqlc.GithubApp, error) {
	return s.queries.ListGitHubAppsByInstallationID(ctx, installationID)
}

func (s *Store) UpsertGitHubRepoCache(ctx context.Context, params sqlc.UpsertGitHubRepoCacheParams) (sqlc.GithubRepoCache, error) {
	return s.queries.UpsertGitHubRepoCache(ctx, params)
}

func (s *Store) ListGitHubRepoCache(ctx context.Context, params sqlc.ListGitHubRepoCacheParams) ([]sqlc.GithubRepoCache, error) {
	return s.queries.ListGitHubRepoCache(ctx, params)
}

func (s *Store) DeleteGitHubRepoCache(ctx context.Context, params sqlc.DeleteGitHubRepoCacheParams) error {
	return s.queries.DeleteGitHubRepoCache(ctx, params)
}

func (s *Store) CountGitHubAppApplications(ctx context.Context, userID pgtype.UUID) (int64, error) {
	return s.queries.CountGitHubAppApplications(ctx, userID)
}

func (s *Store) ListGitHubAppApplicationNames(ctx context.Context, params sqlc.ListGitHubAppApplicationNamesParams) ([]string, error) {
	return s.queries.ListGitHubAppApplicationNames(ctx, params)
}

func (s *Store) ListGitHubAppPushTargets(ctx context.Context, params sqlc.ListGitHubAppPushTargetsParams) ([]sqlc.ListGitHubAppPushTargetsRow, error) {
	return s.queries.ListGitHubAppPushTargets(ctx, params)
}

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
