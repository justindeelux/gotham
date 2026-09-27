package store

import (
	"context"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/justindeelux/gotham/internal/store/sqlc"
)

// CreateApplicationWebhook stores the hook installed for an application. The
// unique index on application_id makes a concurrent duplicate install surface
// as a unique-constraint violation.
func (s *Store) CreateApplicationWebhook(ctx context.Context, params sqlc.CreateApplicationWebhookParams) (sqlc.ApplicationWebhook, error) {
	return s.queries.CreateApplicationWebhook(ctx, params)
}

// GetApplicationWebhookByApp returns the hook of an application, or
// pgx.ErrNoRows when the application has none.
func (s *Store) GetApplicationWebhookByApp(ctx context.Context, applicationID pgtype.UUID) (sqlc.ApplicationWebhook, error) {
	return s.queries.GetApplicationWebhookByApp(ctx, applicationID)
}

// DeleteApplicationWebhook removes the hook of an application and returns the
// deleted row, or pgx.ErrNoRows when there was none (callers treat that as an
// already-deleted, idempotent success).
func (s *Store) DeleteApplicationWebhook(ctx context.Context, applicationID pgtype.UUID) (sqlc.ApplicationWebhook, error) {
	return s.queries.DeleteApplicationWebhook(ctx, applicationID)
}

// GetApplicationForUser returns the application when the caller owns it;
// pgx.ErrNoRows covers both "missing" and "someone else's", so application IDs
// cannot be probed through the management routes.
func (s *Store) GetApplicationForUser(ctx context.Context, id, userID pgtype.UUID) (sqlc.Application, error) {
	return s.queries.GetApplicationForUser(ctx, sqlc.GetApplicationForUserParams{ID: id, UserID: userID})
}

// ListWebhookTargetsForRepo returns every hook watching repo (matched
// case-insensitively — Git hosts differ in how they capitalise owner names)
// together with the application it belongs to. repo is lowercased here so
// callers never have to remember to.
func (s *Store) ListWebhookTargetsForRepo(ctx context.Context, provider, repo string) ([]sqlc.ListWebhookTargetsForRepoRow, error) {
	return s.queries.ListWebhookTargetsForRepo(ctx, sqlc.ListWebhookTargetsForRepoParams{
		Provider: provider,
		Repo:     strings.ToLower(strings.TrimSpace(repo)),
	})
}

// CreateWebhookEvent claims one delivery. The partial unique indexes on
// (application_id, commit_sha) and (application_id, delivery_id) raise a
// unique-constraint violation when the delivery is a duplicate, which is how
// webhook spam is turned into a no-op.
func (s *Store) CreateWebhookEvent(ctx context.Context, params sqlc.CreateWebhookEventParams) (sqlc.WebhookEvent, error) {
	return s.queries.CreateWebhookEvent(ctx, params)
}

// UpdateWebhookEventDeployment links a claimed delivery to the deployment it
// queued.
func (s *Store) UpdateWebhookEventDeployment(ctx context.Context, eventID, deploymentID pgtype.UUID) error {
	return s.queries.UpdateWebhookEventDeployment(ctx, sqlc.UpdateWebhookEventDeploymentParams{
		ID:           eventID,
		DeploymentID: deploymentID,
	})
}

// DeleteWebhookEvent removes a claimed delivery. It is idempotent: a row that
// is already gone is not an error.
func (s *Store) DeleteWebhookEvent(ctx context.Context, id pgtype.UUID) error {
	return s.queries.DeleteWebhookEvent(ctx, id)
}
