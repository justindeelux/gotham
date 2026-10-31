package notifications

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/justindeelux/gotham/internal/store"
	"github.com/justindeelux/gotham/internal/store/sqlc"
)

type Repository interface {
	CreateChannel(ctx context.Context, channel Channel) (Channel, error)
	GetChannel(ctx context.Context, channelID uuid.UUID) (Channel, error)
	ListChannels(ctx context.Context) ([]Channel, error)
	ListChannelsForEvent(ctx context.Context, resourceType string, resourceID uuid.UUID) ([]Channel, error)
	UpdateChannel(ctx context.Context, channel Channel) (Channel, error)
	DeleteChannel(ctx context.Context, channelID uuid.UUID) error
	CreateDelivery(ctx context.Context, delivery Delivery) error
	ListDeliveries(ctx context.Context, channelID uuid.UUID, limit int) ([]Delivery, error)

	// Durable delivery queue.
	PersistDeliveries(ctx context.Context, deliveries []Delivery) error
	PersistEvent(ctx context.Context, notification Notification, deliveries []Delivery) (bool, error)
	ClaimDeliveries(ctx context.Context, now, leaseUntil time.Time, limit int) ([]Delivery, error)
	ReclaimDeliveries(ctx context.Context, now time.Time, maxAttempts int) (int64, error)
	ExpireDeliveries(ctx context.Context, now time.Time) (int64, error)
	CleanupDeliveries(ctx context.Context, cutoff time.Time) (int64, error)
	FinishDelivery(ctx context.Context, delivery Delivery) error
	CancelDelivery(ctx context.Context, deliveryID uuid.UUID, reason string, at time.Time) error
	GetDelivery(ctx context.Context, deliveryID uuid.UUID) (Delivery, error)
	RetryDelivery(ctx context.Context, deliveryID uuid.UUID, at time.Time) (bool, error)
	PruneDelivered(ctx context.Context, retention int) (int64, error)
	HasDeliveredOpening(ctx context.Context, incidentID, channelID uuid.UUID) (bool, error)
	HasLaterIncidentTransition(ctx context.Context, incidentID, channelID uuid.UUID, after Delivery) (bool, error)
	ListTerminalFailures(ctx context.Context, limit int) ([]DeliveryFailure, error)
	ChannelHealth(ctx context.Context) (map[uuid.UUID]ChannelHealth, error)

	ResourceProject(ctx context.Context, resourceType string, resourceID uuid.UUID) (uuid.UUID, error)
	CreateNotification(ctx context.Context, notification Notification) (bool, error)
	ListNotificationRecipients(ctx context.Context, projectID uuid.UUID) ([]uuid.UUID, error)
	ListNotifications(ctx context.Context, userID uuid.UUID, limit int) ([]Notification, int64, error)
	MarkNotificationsSeen(ctx context.Context, userID uuid.UUID, seenAt time.Time) error
	IsOwner(ctx context.Context, userID uuid.UUID) (bool, error)
}

type storeRepository struct {
	store *store.Store
}

var _ Repository = (*storeRepository)(nil)

func newStoreRepository(st *store.Store) *storeRepository {
	return &storeRepository{store: st}
}

func (r *storeRepository) CreateChannel(ctx context.Context, channel Channel) (Channel, error) {
	row, err := r.store.CreateNotificationChannel(ctx, sqlc.CreateNotificationChannelParams{
		ID:           pgUUID(channel.ID),
		Name:         channel.Name,
		Kind:         string(channel.Kind),
		Config:       channel.SealedConfig,
		Enabled:      channel.Enabled,
		ResourceType: nullableText(channel.ResourceType),
		ResourceID:   pgUUID(channel.ResourceID),
		Events:       stringsFromEvents(channel.Events),
		CreatedBy:    pgUUID(channel.CreatedBy),
	})
	if err != nil {
		return Channel{}, fmt.Errorf("notifications: create channel: %w", err)
	}
	return channelFromRow(row), nil
}

func (r *storeRepository) GetChannel(ctx context.Context, channelID uuid.UUID) (Channel, error) {
	row, err := r.store.GetNotificationChannel(ctx, pgUUID(channelID))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Channel{}, ErrNotFound
		}
		return Channel{}, fmt.Errorf("notifications: get channel: %w", err)
	}
	return channelFromRow(row), nil
}

func (r *storeRepository) ListChannels(ctx context.Context) ([]Channel, error) {
	rows, err := r.store.ListNotificationChannels(ctx)
	if err != nil {
		return nil, fmt.Errorf("notifications: list channels: %w", err)
	}
	return channelsFromRows(rows), nil
}

func (r *storeRepository) ListChannelsForEvent(ctx context.Context, resourceType string, resourceID uuid.UUID) ([]Channel, error) {
	rows, err := r.store.ListNotificationChannelsForEvent(ctx, sqlc.ListNotificationChannelsForEventParams{
		ResourceType: nullableText(resourceType),
		ResourceID:   pgUUID(resourceID),
	})
	if err != nil {
		return nil, fmt.Errorf("notifications: list channels for event: %w", err)
	}
	return channelsFromRows(rows), nil
}

func (r *storeRepository) UpdateChannel(ctx context.Context, channel Channel) (Channel, error) {
	row, err := r.store.UpdateNotificationChannel(ctx, sqlc.UpdateNotificationChannelParams{
		ID:           pgUUID(channel.ID),
		Name:         channel.Name,
		Kind:         string(channel.Kind),
		Config:       channel.SealedConfig,
		Enabled:      channel.Enabled,
		ResourceType: nullableText(channel.ResourceType),
		ResourceID:   pgUUID(channel.ResourceID),
		Events:       stringsFromEvents(channel.Events),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Channel{}, ErrNotFound
		}
		return Channel{}, fmt.Errorf("notifications: update channel: %w", err)
	}
	return channelFromRow(row), nil
}

func (r *storeRepository) DeleteChannel(ctx context.Context, channelID uuid.UUID) error {
	affected, err := r.store.DeleteNotificationChannel(ctx, pgUUID(channelID))
	if err != nil {
		return fmt.Errorf("notifications: delete channel: %w", err)
	}
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *storeRepository) CreateDelivery(ctx context.Context, delivery Delivery) error {
	return r.store.InTx(ctx, func(tx *store.Store) error {
		if err := tx.LockNotificationChannelHistory(ctx, delivery.ChannelID.String()); err != nil {
			return fmt.Errorf("notifications: lock delivery history: %w", err)
		}
		if _, err := tx.CreateNotificationDelivery(ctx, deliveryParams(delivery)); err != nil {
			return fmt.Errorf("notifications: create delivery: %w", err)
		}
		if _, err := tx.PruneNotificationDeliveries(ctx, sqlc.PruneNotificationDeliveriesParams{
			ChannelID: pgUUID(delivery.ChannelID),
			Limit:     deliveryRetention,
		}); err != nil {
			return fmt.Errorf("notifications: prune deliveries: %w", err)
		}
		return nil
	})
}

func deliveryParams(delivery Delivery) sqlc.CreateNotificationDeliveryParams {
	completed := delivery.CompletedAt
	params := sqlc.CreateNotificationDeliveryParams{
		ID:           pgUUID(delivery.ID),
		ChannelID:    pgUUID(delivery.ChannelID),
		EventType:    delivery.EventType,
		Status:       string(delivery.Status),
		ErrorSummary: delivery.ErrorSummary,
		CreatedAt:    ts(delivery.CreatedAt),
		DedupeKey:    delivery.DedupeKey,
		Payload:      delivery.Payload,
		IncidentID:   pgUUID(delivery.IncidentID),
		Column10:     ts(delivery.NextAttemptAt),
		CompletedAt:  tsPtr(completed),
		Column12:     ts(delivery.ExpiresAt),
		Attempts:     int32(delivery.Attempts),
	}
	return params
}

func pendingDeliveryParams(deliveries []Delivery) sqlc.CreatePendingDeliveriesParams {
	params := sqlc.CreatePendingDeliveriesParams{
		Ids:         make([]pgtype.UUID, 0, len(deliveries)),
		ChannelIds:  make([]pgtype.UUID, 0, len(deliveries)),
		DedupeKeys:  make([]string, 0, len(deliveries)),
		Payloads:    make([]string, 0, len(deliveries)),
		IncidentIds: make([]pgtype.UUID, 0, len(deliveries)),
		EventType:   deliveries[0].EventType,
		CreatedAt:   ts(deliveries[0].CreatedAt),
		ExpiresAt:   ts(deliveries[0].ExpiresAt),
	}
	for _, delivery := range deliveries {
		params.Ids = append(params.Ids, pgUUID(delivery.ID))
		params.ChannelIds = append(params.ChannelIds, pgUUID(delivery.ChannelID))
		params.DedupeKeys = append(params.DedupeKeys, delivery.DedupeKey)
		params.Payloads = append(params.Payloads, delivery.Payload)
		params.IncidentIds = append(params.IncidentIds, pgUUID(delivery.IncidentID))
	}
	return params
}

func (r *storeRepository) PersistDeliveries(ctx context.Context, deliveries []Delivery) error {
	if len(deliveries) == 0 {
		return nil
	}
	if _, err := r.store.CreatePendingDeliveries(ctx, pendingDeliveryParams(deliveries)); err != nil {
		return fmt.Errorf("notifications: persist deliveries: %w", err)
	}
	return nil
}

// PersistEvent writes the inbox row and all event/channel deliveries in one
// transaction, so a crash or failure cannot leave an inbox event with no work.
func (r *storeRepository) PersistEvent(ctx context.Context, notification Notification, deliveries []Delivery) (bool, error) {
	inserted := false
	err := r.store.InTx(ctx, func(tx *store.Store) error {
		id, err := tx.CreateNotification(ctx, notificationParams(notification))
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return nil
			}
			return fmt.Errorf("notifications: create notification: %w", err)
		}
		inserted = id.Valid
		if len(deliveries) > 0 {
			if _, err := tx.CreatePendingDeliveries(ctx, pendingDeliveryParams(deliveries)); err != nil {
				return fmt.Errorf("notifications: persist deliveries: %w", err)
			}
		}
		if inserted {
			if _, err := tx.PruneNotifications(ctx, notificationRetention); err != nil {
				return fmt.Errorf("notifications: prune notifications: %w", err)
			}
		}
		return nil
	})
	if err != nil {
		return false, err
	}
	return inserted, nil
}

func (r *storeRepository) PruneDelivered(ctx context.Context, retention int) (int64, error) {
	channels, err := r.store.ListNotificationChannels(ctx)
	if err != nil {
		return 0, fmt.Errorf("notifications: list channels for prune: %w", err)
	}
	var total int64
	for _, channel := range channels {
		count, err := r.store.PruneNotificationDeliveries(ctx, sqlc.PruneNotificationDeliveriesParams{
			ChannelID: channel.ID,
			Limit:     int32(retention),
		})
		if err != nil {
			return total, fmt.Errorf("notifications: prune delivered history: %w", err)
		}
		total += count
	}
	return total, nil
}

func (r *storeRepository) HasLaterIncidentTransition(ctx context.Context, incidentID, channelID uuid.UUID, after Delivery) (bool, error) {
	ok, err := r.store.HasLaterIncidentTransition(ctx, sqlc.HasLaterIncidentTransitionParams{
		IncidentID: pgUUID(incidentID),
		ChannelID:  pgUUID(channelID),
		Column3:    ts(after.CreatedAt),
		Column4:    pgUUID(after.ID),
	})
	if err != nil {
		return false, fmt.Errorf("notifications: check later incident transition: %w", err)
	}
	return ok, nil
}

func (r *storeRepository) HasDeliveredOpening(ctx context.Context, incidentID, channelID uuid.UUID) (bool, error) {
	ok, err := r.store.HasDeliveredIncidentOpening(ctx, sqlc.HasDeliveredIncidentOpeningParams{
		IncidentID: pgUUID(incidentID),
		ChannelID:  pgUUID(channelID),
	})
	if err != nil {
		return false, fmt.Errorf("notifications: check delivered opening: %w", err)
	}
	return ok, nil
}

func notificationParams(notification Notification) sqlc.CreateNotificationParams {
	return sqlc.CreateNotificationParams{
		ID:           pgUUID(notification.ID),
		EventKey:     string(notification.EventKey),
		ResourceType: nullableText(notification.ResourceType),
		ResourceID:   pgUUID(notification.ResourceID),
		ProjectID:    pgUUID(notification.ProjectID),
		Name:         notification.Name,
		Host:         notification.Host,
		Error:        notification.Error,
		Detail:       notification.Detail,
		CreatedAt:    ts(notification.CreatedAt),
		DedupeKey:    notification.DedupeKey,
	}
}

func (r *storeRepository) ClaimDeliveries(ctx context.Context, now, leaseUntil time.Time, limit int) ([]Delivery, error) {
	rows, err := r.store.ClaimNotificationDeliveries(ctx, sqlc.ClaimNotificationDeliveriesParams{
		LeaseUntil: ts(leaseUntil),
		Now:        ts(now),
		Batch:      int32(limit),
	})
	if err != nil {
		return nil, fmt.Errorf("notifications: claim deliveries: %w", err)
	}
	return deliveriesFromRows(rows), nil
}

func (r *storeRepository) ReclaimDeliveries(ctx context.Context, now time.Time, maxAttempts int) (int64, error) {
	count, err := r.store.ReclaimNotificationDeliveries(ctx, sqlc.ReclaimNotificationDeliveriesParams{
		MaxAttempts: int32(maxAttempts),
		Now:         ts(now),
	})
	if err != nil {
		return 0, fmt.Errorf("notifications: reclaim deliveries: %w", err)
	}
	return count, nil
}

func (r *storeRepository) ExpireDeliveries(ctx context.Context, now time.Time) (int64, error) {
	count, err := r.store.ExpireNotificationDeliveries(ctx, ts(now))
	if err != nil {
		return 0, fmt.Errorf("notifications: expire deliveries: %w", err)
	}
	return count, nil
}

func (r *storeRepository) CleanupDeliveries(ctx context.Context, cutoff time.Time) (int64, error) {
	count, err := r.store.CleanupNotificationDeliveries(ctx, ts(cutoff))
	if err != nil {
		return 0, fmt.Errorf("notifications: cleanup deliveries: %w", err)
	}
	return count, nil
}

func (r *storeRepository) FinishDelivery(ctx context.Context, delivery Delivery) error {
	if err := r.store.FinishNotificationDelivery(ctx, sqlc.FinishNotificationDeliveryParams{
		Status:        string(delivery.Status),
		ErrorSummary:  delivery.ErrorSummary,
		NextAttemptAt: ts(delivery.NextAttemptAt),
		CompletedAt:   tsPtr(delivery.CompletedAt),
		UpdatedAt:     ts(delivery.UpdatedAt),
		ID:            pgUUID(delivery.ID),
	}); err != nil {
		return fmt.Errorf("notifications: finish delivery: %w", err)
	}
	return nil
}

func (r *storeRepository) CancelDelivery(ctx context.Context, deliveryID uuid.UUID, reason string, at time.Time) error {
	if err := r.store.CancelNotificationDelivery(ctx, sqlc.CancelNotificationDeliveryParams{
		ErrorSummary: reason,
		CompletedAt:  ts(at),
		ID:           pgUUID(deliveryID),
	}); err != nil {
		return fmt.Errorf("notifications: cancel delivery: %w", err)
	}
	return nil
}

func (r *storeRepository) GetDelivery(ctx context.Context, deliveryID uuid.UUID) (Delivery, error) {
	row, err := r.store.GetNotificationDelivery(ctx, pgUUID(deliveryID))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Delivery{}, ErrNotFound
		}
		return Delivery{}, fmt.Errorf("notifications: get delivery: %w", err)
	}
	return deliveryFromRow(row), nil
}

func (r *storeRepository) RetryDelivery(ctx context.Context, deliveryID uuid.UUID, at time.Time) (bool, error) {
	affected, err := r.store.RetryNotificationDelivery(ctx, sqlc.RetryNotificationDeliveryParams{
		Now:       ts(at),
		ExpiresAt: ts(at.Add(maxDeliveryAge)),
		ID:        pgUUID(deliveryID),
	})
	if err != nil {
		return false, fmt.Errorf("notifications: retry delivery: %w", err)
	}
	return affected > 0, nil
}

func (r *storeRepository) ListTerminalFailures(ctx context.Context, limit int) ([]DeliveryFailure, error) {
	rows, err := r.store.ListTerminalDeliveryFailures(ctx, int32(limit))
	if err != nil {
		return nil, fmt.Errorf("notifications: list terminal failures: %w", err)
	}
	failures := make([]DeliveryFailure, 0, len(rows))
	for _, row := range rows {
		failures = append(failures, DeliveryFailure{
			ID:           uuidFromPG(row.ID),
			ChannelID:    uuidFromPG(row.ChannelID),
			ChannelName:  row.ChannelName,
			ChannelKind:  Kind(row.ChannelKind),
			EventType:    row.EventType,
			ErrorSummary: row.ErrorSummary,
			Attempts:     int(row.Attempts),
			FailedAt:     row.CompletedAt.Time,
			Payload:      row.Payload,
		})
	}
	return failures, nil
}

func (r *storeRepository) ChannelHealth(ctx context.Context) (map[uuid.UUID]ChannelHealth, error) {
	rows, err := r.store.ListNotificationChannelHealth(ctx)
	if err != nil {
		return nil, fmt.Errorf("notifications: list channel health: %w", err)
	}
	latest, err := r.store.ListLatestDeliveryFailures(ctx)
	if err != nil {
		return nil, fmt.Errorf("notifications: list latest delivery failures: %w", err)
	}
	errorsByChannel := make(map[uuid.UUID]string, len(latest))
	lastFailureByChannel := make(map[uuid.UUID]time.Time, len(latest))
	for _, row := range latest {
		id := uuidFromPG(row.ChannelID)
		errorsByChannel[id] = row.ErrorSummary
		lastFailureByChannel[id] = row.CompletedAt.Time
	}
	health := make(map[uuid.UUID]ChannelHealth, len(rows))
	for _, row := range rows {
		id := uuidFromPG(row.ChannelID)
		item := ChannelHealth{
			Status:    "ok",
			Pending:   row.PendingCount,
			LastError: errorsByChannel[id],
		}
		if row.LastDeliveredAt.Valid {
			at := row.LastDeliveredAt.Time
			item.LastDeliveredAt = &at
		}
		if at, ok := lastFailureByChannel[id]; ok {
			item.LastFailedAt = &at
		}
		switch {
		case item.LastFailedAt != nil && (item.LastDeliveredAt == nil || item.LastFailedAt.After(*item.LastDeliveredAt)):
			item.Status = "failing"
		case item.Pending > 0:
			item.Status = "pending"
		}
		health[id] = item
	}
	return health, nil
}

func (r *storeRepository) ListDeliveries(ctx context.Context, channelID uuid.UUID, limit int) ([]Delivery, error) {
	rows, err := r.store.ListNotificationDeliveries(ctx, sqlc.ListNotificationDeliveriesParams{
		ChannelID: pgUUID(channelID),
		Limit:     int32(limit),
	})
	if err != nil {
		return nil, fmt.Errorf("notifications: list deliveries: %w", err)
	}
	return deliveriesFromRows(rows), nil
}

func deliveriesFromRows(rows []sqlc.NotificationDelivery) []Delivery {
	deliveries := make([]Delivery, 0, len(rows))
	for _, row := range rows {
		deliveries = append(deliveries, deliveryFromRow(row))
	}
	return deliveries
}

func deliveryFromRow(row sqlc.NotificationDelivery) Delivery {
	delivery := Delivery{
		ID:            uuidFromPG(row.ID),
		ChannelID:     uuidFromPG(row.ChannelID),
		EventType:     row.EventType,
		Status:        DeliveryState(row.Status),
		ErrorSummary:  row.ErrorSummary,
		CreatedAt:     row.CreatedAt.Time,
		DedupeKey:     row.DedupeKey,
		Payload:       row.Payload,
		IncidentID:    uuidFromPG(row.IncidentID),
		Attempts:      int(row.Attempts),
		NextAttemptAt: row.NextAttemptAt.Time,
		ExpiresAt:     row.ExpiresAt.Time,
		UpdatedAt:     row.UpdatedAt.Time,
	}
	if row.LeaseUntil.Valid {
		at := row.LeaseUntil.Time
		delivery.LeaseUntil = &at
	}
	if row.CompletedAt.Valid {
		at := row.CompletedAt.Time
		delivery.CompletedAt = &at
	}
	return delivery
}

func (r *storeRepository) IsOwner(ctx context.Context, userID uuid.UUID) (bool, error) {
	user, err := r.store.GetUserByID(ctx, pgUUID(userID))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, nil
		}
		return false, fmt.Errorf("notifications: resolve user role: %w", err)
	}
	return user.Role == "owner", nil
}

func (r *storeRepository) ResourceProject(ctx context.Context, resourceType string, resourceID uuid.UUID) (uuid.UUID, error) {
	return r.store.ResourceProject(ctx, resourceType, resourceID)
}

func (r *storeRepository) CreateNotification(ctx context.Context, notification Notification) (bool, error) {
	var inserted bool
	err := r.store.InTx(ctx, func(tx *store.Store) error {
		id, err := tx.CreateNotification(ctx, notificationParams(notification))
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return nil
			}
			return fmt.Errorf("notifications: create notification: %w", err)
		}
		inserted = id.Valid
		if _, err := tx.PruneNotifications(ctx, notificationRetention); err != nil {
			return fmt.Errorf("notifications: prune notifications: %w", err)
		}
		return nil
	})
	if err != nil {
		return false, err
	}
	return inserted, nil
}

func (r *storeRepository) ListNotificationRecipients(ctx context.Context, projectID uuid.UUID) ([]uuid.UUID, error) {
	rows, err := r.store.ListNotificationRecipients(ctx, pgUUID(projectID))
	if err != nil {
		return nil, fmt.Errorf("notifications: list notification recipients: %w", err)
	}
	recipients := make([]uuid.UUID, 0, len(rows))
	for _, row := range rows {
		recipients = append(recipients, uuidFromPG(row))
	}
	return recipients, nil
}

func (r *storeRepository) ListNotifications(ctx context.Context, userID uuid.UUID, limit int) ([]Notification, int64, error) {
	rows, err := r.store.ListVisibleNotifications(ctx, sqlc.ListVisibleNotificationsParams{
		ID:    pgUUID(userID),
		Limit: int32(limit),
	})
	if err != nil {
		return nil, 0, fmt.Errorf("notifications: list notifications: %w", err)
	}
	unread, err := r.store.CountUnreadNotifications(ctx, pgUUID(userID))
	if err != nil {
		return nil, 0, fmt.Errorf("notifications: count unread notifications: %w", err)
	}
	notifications := make([]Notification, 0, len(rows))
	for _, row := range rows {
		notifications = append(notifications, notificationFromRow(row))
	}
	return notifications, unread, nil
}

func (r *storeRepository) MarkNotificationsSeen(ctx context.Context, userID uuid.UUID, seenAt time.Time) error {
	if err := r.store.MarkNotificationsSeen(ctx, sqlc.MarkNotificationsSeenParams{
		ID:      pgUUID(userID),
		Column2: pgtype.Timestamptz{Time: seenAt, Valid: true},
	}); err != nil {
		return fmt.Errorf("notifications: mark notifications seen: %w", err)
	}
	return nil
}

func notificationFromRow(row sqlc.Notification) Notification {
	notification := Notification{
		ID:         uuidFromPG(row.ID),
		EventKey:   EventKey(row.EventKey),
		ResourceID: uuidFromPG(row.ResourceID),
		ProjectID:  uuidFromPG(row.ProjectID),
		Name:       row.Name,
		Host:       row.Host,
		Error:      row.Error,
		Detail:     row.Detail,
		CreatedAt:  row.CreatedAt.Time,
	}
	if row.ResourceType != nil {
		notification.ResourceType = *row.ResourceType
	}
	return notification
}

func channelFromRow(row sqlc.NotificationChannel) Channel {
	events := make([]EventKey, 0, len(row.Events))
	for _, event := range row.Events {
		events = append(events, EventKey(event))
	}
	channel := Channel{
		ID:           uuidFromPG(row.ID),
		Name:         row.Name,
		Kind:         Kind(row.Kind),
		Enabled:      row.Enabled,
		Events:       events,
		SealedConfig: row.Config,
		CreatedBy:    uuidFromPG(row.CreatedBy),
		CreatedAt:    row.CreatedAt.Time,
		UpdatedAt:    row.UpdatedAt.Time,
	}
	if row.ResourceType != nil {
		channel.ResourceType = *row.ResourceType
	}
	channel.ResourceID = uuidFromPG(row.ResourceID)
	return channel
}

func channelsFromRows(rows []sqlc.NotificationChannel) []Channel {
	channels := make([]Channel, 0, len(rows))
	for _, row := range rows {
		channels = append(channels, channelFromRow(row))
	}
	return channels
}

func stringsFromEvents(events []EventKey) []string {
	values := make([]string, 0, len(events))
	for _, event := range events {
		values = append(values, string(event))
	}
	return values
}

func nullableText(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

func ts(at time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: at, Valid: !at.IsZero()}
}

func tsPtr(at *time.Time) pgtype.Timestamptz {
	if at == nil {
		return pgtype.Timestamptz{}
	}
	return ts(*at)
}

func pgUUID(id uuid.UUID) pgtype.UUID {
	if id == uuid.Nil {
		return pgtype.UUID{}
	}
	return pgtype.UUID{Bytes: id, Valid: true}
}

func uuidFromPG(v pgtype.UUID) uuid.UUID {
	if !v.Valid {
		return uuid.Nil
	}
	return uuid.UUID(v.Bytes)
}
