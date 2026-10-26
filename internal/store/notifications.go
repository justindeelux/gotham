package store

import (
	"context"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/justindeelux/gotham/internal/store/sqlc"
)

func (s *Store) CreateNotification(ctx context.Context, params sqlc.CreateNotificationParams) error {
	return s.queries.CreateNotification(ctx, params)
}

func (s *Store) PruneNotifications(ctx context.Context, limit int32) (int64, error) {
	return s.queries.PruneNotifications(ctx, limit)
}

func (s *Store) ListVisibleNotifications(ctx context.Context, params sqlc.ListVisibleNotificationsParams) ([]sqlc.Notification, error) {
	return s.queries.ListVisibleNotifications(ctx, params)
}

func (s *Store) CountUnreadNotifications(ctx context.Context, id pgtype.UUID) (int64, error) {
	return s.queries.CountUnreadNotifications(ctx, id)
}

func (s *Store) MarkNotificationsSeen(ctx context.Context, params sqlc.MarkNotificationsSeenParams) error {
	return s.queries.MarkNotificationsSeen(ctx, params)
}

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
