package store

import (
	"context"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/justindeelux/gotham/internal/store/sqlc"
)

func (s *Store) CreateApplicationWebhook(ctx context.Context, params sqlc.CreateApplicationWebhookParams) (sqlc.ApplicationWebhook, error) {
	return s.queries.CreateApplicationWebhook(ctx, params)
}

func (s *Store) GetApplicationWebhookByApp(ctx context.Context, applicationID pgtype.UUID) (sqlc.ApplicationWebhook, error) {
	return s.queries.GetApplicationWebhookByApp(ctx, applicationID)
}

func (s *Store) DeleteApplicationWebhook(ctx context.Context, applicationID pgtype.UUID) (sqlc.ApplicationWebhook, error) {
	return s.queries.DeleteApplicationWebhook(ctx, applicationID)
}

func (s *Store) GetApplicationForUser(ctx context.Context, id, userID pgtype.UUID) (sqlc.Application, error) {
	return s.queries.GetApplicationForUser(ctx, sqlc.GetApplicationForUserParams{ID: id, UserID: userID})
}

func (s *Store) ListWebhookTargetsForRepo(ctx context.Context, provider, repo string) ([]sqlc.ListWebhookTargetsForRepoRow, error) {
	return s.queries.ListWebhookTargetsForRepo(ctx, sqlc.ListWebhookTargetsForRepoParams{
		Provider: provider,
		Repo:     strings.ToLower(strings.TrimSpace(repo)),
	})
}

func (s *Store) CreateWebhookEvent(ctx context.Context, params sqlc.CreateWebhookEventParams) (sqlc.WebhookEvent, error) {
	return s.queries.CreateWebhookEvent(ctx, params)
}

func (s *Store) GetWebhookEventForDelivery(ctx context.Context, params sqlc.GetWebhookEventForDeliveryParams) (sqlc.WebhookEvent, error) {
	return s.queries.GetWebhookEventForDelivery(ctx, params)
}

func (s *Store) UpdateWebhookEventDeployment(ctx context.Context, eventID, deploymentID pgtype.UUID) error {
	return s.queries.UpdateWebhookEventDeployment(ctx, sqlc.UpdateWebhookEventDeploymentParams{
		ID:           eventID,
		DeploymentID: deploymentID,
	})
}

func (s *Store) DeleteWebhookEvent(ctx context.Context, id pgtype.UUID) error {
	return s.queries.DeleteWebhookEvent(ctx, id)
}
