package store

import (
	"context"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/justindeelux/gotham/internal/store/sqlc"
)

// Notification-channel persistence (Phase 8, BE-8.3). The channel config is
// stored sealed; these wrappers only move rows, the notifications package owns
// sealing, redaction and validation.

// CreateNotificationChannel stores a channel and returns the stored row.
func (s *Store) CreateNotificationChannel(ctx context.Context, params sqlc.CreateNotificationChannelParams) (sqlc.NotificationChannel, error) {
	return s.queries.CreateNotificationChannel(ctx, params)
}

// GetNotificationChannel returns a team's channel, or pgx.ErrNoRows when the
// row does not exist or belongs to another team.
func (s *Store) GetNotificationChannel(ctx context.Context, params sqlc.GetNotificationChannelParams) (sqlc.NotificationChannel, error) {
	return s.queries.GetNotificationChannel(ctx, params)
}

// ListNotificationChannels returns a team's channels, oldest first.
func (s *Store) ListNotificationChannels(ctx context.Context, teamID pgtype.UUID) ([]sqlc.NotificationChannel, error) {
	return s.queries.ListNotificationChannels(ctx, teamID)
}

// ListNotificationChannelsForEvent returns the enabled channels of a team that
// cover one resource: team-wide rows plus the overrides scoped to exactly that
// resource.
func (s *Store) ListNotificationChannelsForEvent(ctx context.Context, params sqlc.ListNotificationChannelsForEventParams) ([]sqlc.NotificationChannel, error) {
	return s.queries.ListNotificationChannelsForEvent(ctx, params)
}

// UpdateNotificationChannel updates a team's channel and returns the stored
// row.
func (s *Store) UpdateNotificationChannel(ctx context.Context, params sqlc.UpdateNotificationChannelParams) (sqlc.NotificationChannel, error) {
	return s.queries.UpdateNotificationChannel(ctx, params)
}

// DeleteNotificationChannel removes a team's channel and reports how many rows
// were affected.
func (s *Store) DeleteNotificationChannel(ctx context.Context, params sqlc.DeleteNotificationChannelParams) (int64, error) {
	return s.queries.DeleteNotificationChannel(ctx, params)
}

// CreateNotificationDelivery stores one delivery attempt and returns the row.
func (s *Store) CreateNotificationDelivery(ctx context.Context, params sqlc.CreateNotificationDeliveryParams) (sqlc.NotificationDelivery, error) {
	return s.queries.CreateNotificationDelivery(ctx, params)
}

// ListNotificationDeliveries returns the newest attempts of one team-scoped
// channel. A channel of another team matches no row.
func (s *Store) ListNotificationDeliveries(ctx context.Context, params sqlc.ListNotificationDeliveriesParams) ([]sqlc.NotificationDelivery, error) {
	return s.queries.ListNotificationDeliveries(ctx, params)
}

// PruneNotificationDeliveries trims one channel's history to the newest
// params.Limit rows and reports how many rows were removed.
func (s *Store) PruneNotificationDeliveries(ctx context.Context, params sqlc.PruneNotificationDeliveriesParams) (int64, error) {
	return s.queries.PruneNotificationDeliveries(ctx, params)
}
