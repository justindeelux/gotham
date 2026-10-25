package store

import (
	"context"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/justindeelux/gotham/internal/store/sqlc"
)

func (s *Store) CreateNotificationChannel(ctx context.Context, params sqlc.CreateNotificationChannelParams) (sqlc.NotificationChannel, error) {
	return s.queries.CreateNotificationChannel(ctx, params)
}

func (s *Store) GetNotificationChannel(ctx context.Context, id pgtype.UUID) (sqlc.NotificationChannel, error) {
	return s.queries.GetNotificationChannel(ctx, id)
}

func (s *Store) ListNotificationChannels(ctx context.Context) ([]sqlc.NotificationChannel, error) {
	return s.queries.ListNotificationChannels(ctx)
}

func (s *Store) ListNotificationChannelsForEvent(ctx context.Context, params sqlc.ListNotificationChannelsForEventParams) ([]sqlc.NotificationChannel, error) {
	return s.queries.ListNotificationChannelsForEvent(ctx, params)
}

func (s *Store) UpdateNotificationChannel(ctx context.Context, params sqlc.UpdateNotificationChannelParams) (sqlc.NotificationChannel, error) {
	return s.queries.UpdateNotificationChannel(ctx, params)
}

func (s *Store) DeleteNotificationChannel(ctx context.Context, id pgtype.UUID) (int64, error) {
	return s.queries.DeleteNotificationChannel(ctx, id)
}

func (s *Store) CreateNotificationDelivery(ctx context.Context, params sqlc.CreateNotificationDeliveryParams) (sqlc.NotificationDelivery, error) {
	return s.queries.CreateNotificationDelivery(ctx, params)
}

func (s *Store) LockNotificationChannelHistory(ctx context.Context, channelID string) error {
	return s.queries.LockNotificationChannelHistory(ctx, channelID)
}

func (s *Store) ListNotificationDeliveries(ctx context.Context, params sqlc.ListNotificationDeliveriesParams) ([]sqlc.NotificationDelivery, error) {
	return s.queries.ListNotificationDeliveries(ctx, params)
}

func (s *Store) PruneNotificationDeliveries(ctx context.Context, params sqlc.PruneNotificationDeliveriesParams) (int64, error) {
	return s.queries.PruneNotificationDeliveries(ctx, params)
}
