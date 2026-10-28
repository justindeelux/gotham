package store

import (
	"context"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/justindeelux/gotham/internal/store/sqlc"
)

func (s *Store) CreateNotification(ctx context.Context, params sqlc.CreateNotificationParams) (pgtype.UUID, error) {
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

func (s *Store) ListNotificationRecipients(ctx context.Context, projectID pgtype.UUID) ([]pgtype.UUID, error) {
	return s.queries.ListNotificationRecipients(ctx, projectID)
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

func (s *Store) CreatePendingDeliveries(ctx context.Context, params sqlc.CreatePendingDeliveriesParams) ([]sqlc.NotificationDelivery, error) {
	return s.queries.CreatePendingDeliveries(ctx, params)
}

func (s *Store) ClaimNotificationDeliveries(ctx context.Context, params sqlc.ClaimNotificationDeliveriesParams) ([]sqlc.NotificationDelivery, error) {
	return s.queries.ClaimNotificationDeliveries(ctx, params)
}

func (s *Store) ReclaimNotificationDeliveries(ctx context.Context, params sqlc.ReclaimNotificationDeliveriesParams) (int64, error) {
	return s.queries.ReclaimNotificationDeliveries(ctx, params)
}

func (s *Store) ExpireNotificationDeliveries(ctx context.Context, now pgtype.Timestamptz) (int64, error) {
	return s.queries.ExpireNotificationDeliveries(ctx, now)
}

func (s *Store) CleanupNotificationDeliveries(ctx context.Context, cutoff pgtype.Timestamptz) (int64, error) {
	return s.queries.CleanupNotificationDeliveries(ctx, cutoff)
}

func (s *Store) FinishNotificationDelivery(ctx context.Context, params sqlc.FinishNotificationDeliveryParams) error {
	return s.queries.FinishNotificationDelivery(ctx, params)
}

func (s *Store) CancelNotificationDelivery(ctx context.Context, params sqlc.CancelNotificationDeliveryParams) error {
	return s.queries.CancelNotificationDelivery(ctx, params)
}

func (s *Store) GetNotificationDelivery(ctx context.Context, id pgtype.UUID) (sqlc.NotificationDelivery, error) {
	return s.queries.GetNotificationDelivery(ctx, id)
}

func (s *Store) RetryNotificationDelivery(ctx context.Context, params sqlc.RetryNotificationDeliveryParams) (int64, error) {
	return s.queries.RetryNotificationDelivery(ctx, params)
}

func (s *Store) HasLaterIncidentTransition(ctx context.Context, params sqlc.HasLaterIncidentTransitionParams) (bool, error) {
	return s.queries.HasLaterIncidentTransition(ctx, params)
}

func (s *Store) HasDeliveredIncidentOpening(ctx context.Context, params sqlc.HasDeliveredIncidentOpeningParams) (bool, error) {
	return s.queries.HasDeliveredIncidentOpening(ctx, params)
}

func (s *Store) ListTerminalDeliveryFailures(ctx context.Context, rowLimit int32) ([]sqlc.ListTerminalDeliveryFailuresRow, error) {
	return s.queries.ListTerminalDeliveryFailures(ctx, rowLimit)
}

func (s *Store) ListNotificationChannelHealth(ctx context.Context) ([]sqlc.ListNotificationChannelHealthRow, error) {
	return s.queries.ListNotificationChannelHealth(ctx)
}

func (s *Store) ListLatestDeliveryFailures(ctx context.Context) ([]sqlc.ListLatestDeliveryFailuresRow, error) {
	return s.queries.ListLatestDeliveryFailures(ctx)
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
