package notifications

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/justindeelux/gotham/internal/store"
	"github.com/justindeelux/gotham/internal/store/sqlc"
)

// Repository persists notification channels. It is implemented over
// *store.Store (sqlc) in production and by fakes in tests.
type Repository interface {
	// CreateChannel stores a new channel and returns the stored row.
	CreateChannel(ctx context.Context, channel Channel) (Channel, error)
	// GetChannel returns one channel of a team, or ErrNotFound when the row
	// is missing or belongs to another team.
	GetChannel(ctx context.Context, teamID, channelID uuid.UUID) (Channel, error)
	// ListChannels returns a team's channels, oldest first.
	ListChannels(ctx context.Context, teamID uuid.UUID) ([]Channel, error)
	// ListChannelsForEvent returns the enabled channels of a team that cover
	// one resource: team-wide rows plus the overrides scoped to exactly that
	// resource.
	ListChannelsForEvent(ctx context.Context, teamID uuid.UUID, resourceType string, resourceID uuid.UUID) ([]Channel, error)
	// UpdateChannel persists the mutable fields and returns the stored row.
	UpdateChannel(ctx context.Context, channel Channel) (Channel, error)
	// DeleteChannel removes one channel of a team, or ErrNotFound when no row
	// matched.
	DeleteChannel(ctx context.Context, teamID, channelID uuid.UUID) error
	// CreateDelivery appends one attempt and trims the channel's history to
	// the retention cap, in one transaction.
	CreateDelivery(ctx context.Context, delivery Delivery) error
	// ListDeliveries returns the newest attempts of one team-scoped channel,
	// newest first. A foreign team's channel matches no row.
	ListDeliveries(ctx context.Context, teamID, channelID uuid.UUID, limit int) ([]Delivery, error)
}

// storeRepository adapts *store.Store to Repository.
type storeRepository struct {
	store *store.Store
}

// Compile-time guarantee.
var _ Repository = (*storeRepository)(nil)

// newStoreRepository builds the PostgreSQL-backed repository.
func newStoreRepository(st *store.Store) *storeRepository {
	return &storeRepository{store: st}
}

// CreateChannel implements Repository.
func (r *storeRepository) CreateChannel(ctx context.Context, channel Channel) (Channel, error) {
	row, err := r.store.CreateNotificationChannel(ctx, sqlc.CreateNotificationChannelParams{
		ID:           pgUUID(channel.ID),
		TeamID:       pgUUID(channel.TeamID),
		Name:         channel.Name,
		Kind:         string(channel.Kind),
		Config:       channel.SealedConfig,
		Enabled:      channel.Enabled,
		ResourceType: nullableText(channel.ResourceType),
		ResourceID:   pgUUID(channel.ResourceID),
		Events:       stringsFromEvents(channel.Events),
	})
	if err != nil {
		return Channel{}, fmt.Errorf("notifications: create channel: %w", err)
	}
	return channelFromRow(row), nil
}

// GetChannel implements Repository, mapping a missing row to ErrNotFound.
func (r *storeRepository) GetChannel(ctx context.Context, teamID, channelID uuid.UUID) (Channel, error) {
	row, err := r.store.GetNotificationChannel(ctx, sqlc.GetNotificationChannelParams{
		ID:     pgUUID(channelID),
		TeamID: pgUUID(teamID),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Channel{}, ErrNotFound
		}
		return Channel{}, fmt.Errorf("notifications: get channel: %w", err)
	}
	return channelFromRow(row), nil
}

// ListChannels implements Repository.
func (r *storeRepository) ListChannels(ctx context.Context, teamID uuid.UUID) ([]Channel, error) {
	rows, err := r.store.ListNotificationChannels(ctx, pgUUID(teamID))
	if err != nil {
		return nil, fmt.Errorf("notifications: list channels: %w", err)
	}
	return channelsFromRows(rows), nil
}

// ListChannelsForEvent implements Repository.
func (r *storeRepository) ListChannelsForEvent(ctx context.Context, teamID uuid.UUID, resourceType string, resourceID uuid.UUID) ([]Channel, error) {
	rows, err := r.store.ListNotificationChannelsForEvent(ctx, sqlc.ListNotificationChannelsForEventParams{
		TeamID:       pgUUID(teamID),
		ResourceType: nullableText(resourceType),
		ResourceID:   pgUUID(resourceID),
	})
	if err != nil {
		return nil, fmt.Errorf("notifications: list channels for event: %w", err)
	}
	return channelsFromRows(rows), nil
}

// UpdateChannel implements Repository, mapping a missing row to ErrNotFound.
func (r *storeRepository) UpdateChannel(ctx context.Context, channel Channel) (Channel, error) {
	row, err := r.store.UpdateNotificationChannel(ctx, sqlc.UpdateNotificationChannelParams{
		ID:           pgUUID(channel.ID),
		TeamID:       pgUUID(channel.TeamID),
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

// DeleteChannel implements Repository, mapping zero affected rows to
// ErrNotFound.
func (r *storeRepository) DeleteChannel(ctx context.Context, teamID, channelID uuid.UUID) error {
	affected, err := r.store.DeleteNotificationChannel(ctx, sqlc.DeleteNotificationChannelParams{
		ID:     pgUUID(channelID),
		TeamID: pgUUID(teamID),
	})
	if err != nil {
		return fmt.Errorf("notifications: delete channel: %w", err)
	}
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}

// CreateDelivery implements Repository: it appends one attempt and trims the
// channel's history to the retention cap in a single transaction, so a write
// can never leave the channel over the cap. The per-channel advisory lock
// serializes concurrent writers of the same channel: without it, two
// READ COMMITTED transactions would each keep their own newest row and leave
// the channel at cap+1.
func (r *storeRepository) CreateDelivery(ctx context.Context, delivery Delivery) error {
	return r.store.InTx(ctx, func(tx *store.Store) error {
		if err := tx.LockNotificationChannelHistory(ctx, delivery.ChannelID.String()); err != nil {
			return fmt.Errorf("notifications: lock delivery history: %w", err)
		}
		if _, err := tx.CreateNotificationDelivery(ctx, sqlc.CreateNotificationDeliveryParams{
			ID:           pgUUID(delivery.ID),
			ChannelID:    pgUUID(delivery.ChannelID),
			EventType:    delivery.EventType,
			Status:       string(delivery.Status),
			ErrorSummary: delivery.ErrorSummary,
			CreatedAt:    pgtype.Timestamptz{Time: delivery.CreatedAt, Valid: true},
		}); err != nil {
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

// ListDeliveries implements Repository.
func (r *storeRepository) ListDeliveries(ctx context.Context, teamID, channelID uuid.UUID, limit int) ([]Delivery, error) {
	rows, err := r.store.ListNotificationDeliveries(ctx, sqlc.ListNotificationDeliveriesParams{
		ChannelID: pgUUID(channelID),
		TeamID:    pgUUID(teamID),
		Limit:     int32(limit),
	})
	if err != nil {
		return nil, fmt.Errorf("notifications: list deliveries: %w", err)
	}
	deliveries := make([]Delivery, 0, len(rows))
	for _, row := range rows {
		deliveries = append(deliveries, deliveryFromRow(row))
	}
	return deliveries, nil
}

// deliveryFromRow maps a sqlc row to the domain delivery.
func deliveryFromRow(row sqlc.NotificationDelivery) Delivery {
	return Delivery{
		ID:           uuidFromPG(row.ID),
		ChannelID:    uuidFromPG(row.ChannelID),
		EventType:    row.EventType,
		Status:       DeliveryStatus(row.Status),
		ErrorSummary: row.ErrorSummary,
		CreatedAt:    row.CreatedAt.Time,
	}
}

// channelFromRow maps a sqlc row to the domain channel.
func channelFromRow(row sqlc.NotificationChannel) Channel {
	events := make([]EventKey, 0, len(row.Events))
	for _, event := range row.Events {
		events = append(events, EventKey(event))
	}
	channel := Channel{
		ID:           uuidFromPG(row.ID),
		TeamID:       uuidFromPG(row.TeamID),
		Name:         row.Name,
		Kind:         Kind(row.Kind),
		Enabled:      row.Enabled,
		Events:       events,
		SealedConfig: row.Config,
		CreatedAt:    row.CreatedAt.Time,
		UpdatedAt:    row.UpdatedAt.Time,
	}
	if row.ResourceType != nil {
		channel.ResourceType = *row.ResourceType
	}
	channel.ResourceID = uuidFromPG(row.ResourceID)
	return channel
}

// channelsFromRows maps a page of sqlc rows.
func channelsFromRows(rows []sqlc.NotificationChannel) []Channel {
	channels := make([]Channel, 0, len(rows))
	for _, row := range rows {
		channels = append(channels, channelFromRow(row))
	}
	return channels
}

// stringsFromEvents converts the event subscription for sqlc.
func stringsFromEvents(events []EventKey) []string {
	values := make([]string, 0, len(events))
	for _, event := range events {
		values = append(values, string(event))
	}
	return values
}

// nullableText maps an empty string to a SQL NULL.
func nullableText(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

// pgUUID converts a domain UUID for sqlc; the zero UUID becomes NULL.
func pgUUID(id uuid.UUID) pgtype.UUID {
	if id == uuid.Nil {
		return pgtype.UUID{}
	}
	return pgtype.UUID{Bytes: id, Valid: true}
}

// uuidFromPG converts a sqlc UUID column to the domain type (NULL → zero).
func uuidFromPG(v pgtype.UUID) uuid.UUID {
	if !v.Valid {
		return uuid.Nil
	}
	return uuid.UUID(v.Bytes)
}
