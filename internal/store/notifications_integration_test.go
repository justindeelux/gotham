package store_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/justindeelux/gotham/internal/store"
	"github.com/justindeelux/gotham/internal/store/sqlc"
)

// TestNotificationChannelPersistence runs migration 00021 and exercises the
// channel queries: team-scoped reads, the dispatcher read's resource matching,
// updates and deletes. It skips when no database is reachable.
func TestNotificationChannelPersistence(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	dsn := testDSN()
	if err := store.Migrate(ctx, dsn, store.MigrateUp); err != nil {
		t.Skipf("Postgres not available: %v", err)
	}
	pool, err := store.Open(ctx, dsn)
	if err != nil {
		t.Skipf("Postgres not available: %v", err)
	}
	t.Cleanup(pool.Close)
	st := store.New(pool)

	email := fmt.Sprintf("be-8.3-notify-%d@example.com", time.Now().UnixNano())
	user, err := st.CreateUser(ctx, email, nil)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		if _, err := pool.Exec(cleanupCtx, "DELETE FROM users WHERE id = $1", user.ID); err != nil {
			t.Logf("cleanup user: %v", err)
		}
		if _, err := pool.Exec(cleanupCtx, "DELETE FROM teams WHERE id = $1", user.ID); err != nil {
			t.Logf("cleanup team: %v", err)
		}
	})
	teamID := user.ID

	channelID := uuid.New()
	created, err := st.CreateNotificationChannel(ctx, sqlc.CreateNotificationChannelParams{
		ID:           pgtype.UUID{Bytes: channelID, Valid: true},
		TeamID:       teamID,
		Name:         "team alerts",
		Kind:         "discord",
		Config:       "sealed-config",
		Enabled:      true,
		ResourceType: nil,
		ResourceID:   pgtype.UUID{},
		Events:       []string{"deploy_failure", "backup_failure"},
	})
	if err != nil {
		t.Fatalf("CreateNotificationChannel: %v", err)
	}
	if len(created.Events) != 2 || created.Events[0] != "deploy_failure" {
		t.Errorf("events = %v, want the stored subscription", created.Events)
	}
	if created.ResourceType != nil {
		t.Errorf("resource_type = %v, want NULL for a team-wide channel", *created.ResourceType)
	}
	if created.CreatedAt.Time.IsZero() {
		t.Error("created_at is not set")
	}

	// A foreign team cannot read the row.
	if _, err := st.GetNotificationChannel(ctx, sqlc.GetNotificationChannelParams{
		ID:     created.ID,
		TeamID: pgtype.UUID{Bytes: uuid.New(), Valid: true},
	}); !errors.Is(err, pgx.ErrNoRows) {
		t.Errorf("foreign get err = %v, want pgx.ErrNoRows", err)
	}
	if _, err := st.ListNotificationChannels(ctx, pgtype.UUID{Bytes: uuid.New(), Valid: true}); err != nil {
		t.Errorf("foreign list err = %v, want an empty page", err)
	}

	// A resource override: only the matching application may select it.
	appID := uuid.New()
	resourceType := "application"
	override, err := st.CreateNotificationChannel(ctx, sqlc.CreateNotificationChannelParams{
		ID:           pgtype.UUID{Bytes: uuid.New(), Valid: true},
		TeamID:       teamID,
		Name:         "app override",
		Kind:         "slack",
		Config:       "sealed-override",
		Enabled:      true,
		ResourceType: &resourceType,
		ResourceID:   pgtype.UUID{Bytes: appID, Valid: true},
		Events:       []string{"deploy_success", "deploy_failure", "backup_success", "backup_failure"},
	})
	if err != nil {
		t.Fatalf("CreateNotificationChannel (override): %v", err)
	}
	if override.ResourceType == nil || *override.ResourceType != "application" {
		t.Errorf("resource_type = %v, want application", override.ResourceType)
	}

	// The dispatcher read for the override's resource returns both rows.
	forEvent, err := st.ListNotificationChannelsForEvent(ctx, sqlc.ListNotificationChannelsForEventParams{
		TeamID:       teamID,
		ResourceType: &resourceType,
		ResourceID:   pgtype.UUID{Bytes: appID, Valid: true},
	})
	if err != nil {
		t.Fatalf("ListNotificationChannelsForEvent: %v", err)
	}
	if len(forEvent) != 2 {
		t.Fatalf("channels for the app event = %d, want the team-wide and the override", len(forEvent))
	}

	// Another resource only sees the team-wide channel.
	otherApp := uuid.New()
	forEvent, err = st.ListNotificationChannelsForEvent(ctx, sqlc.ListNotificationChannelsForEventParams{
		TeamID:       teamID,
		ResourceType: &resourceType,
		ResourceID:   pgtype.UUID{Bytes: otherApp, Valid: true},
	})
	if err != nil {
		t.Fatalf("ListNotificationChannelsForEvent (other app): %v", err)
	}
	if len(forEvent) != 1 || forEvent[0].ID.Bytes != channelID {
		t.Fatalf("channels for another app = %d, want only the team-wide row", len(forEvent))
	}

	// A disabled channel drops out of the dispatcher read.
	disabled := false
	if _, err := st.UpdateNotificationChannel(ctx, sqlc.UpdateNotificationChannelParams{
		ID:           created.ID,
		TeamID:       teamID,
		Name:         "team alerts",
		Kind:         "discord",
		Config:       "sealed-config",
		Enabled:      disabled,
		ResourceType: nil,
		ResourceID:   pgtype.UUID{},
		Events:       []string{"deploy_failure"},
	}); err != nil {
		t.Fatalf("UpdateNotificationChannel: %v", err)
	}
	forEvent, err = st.ListNotificationChannelsForEvent(ctx, sqlc.ListNotificationChannelsForEventParams{
		TeamID:       teamID,
		ResourceType: &resourceType,
		ResourceID:   pgtype.UUID{Bytes: otherApp, Valid: true},
	})
	if err != nil {
		t.Fatalf("ListNotificationChannelsForEvent (disabled): %v", err)
	}
	if len(forEvent) != 0 {
		t.Fatalf("channels = %d, want none: the only team-wide row is disabled", len(forEvent))
	}

	// The list still returns both configured channels.
	all, err := st.ListNotificationChannels(ctx, teamID)
	if err != nil {
		t.Fatalf("ListNotificationChannels: %v", err)
	}
	if len(all) != 2 {
		t.Errorf("channels = %d, want 2", len(all))
	}

	// Delete is team-scoped and reports the affected rows.
	affected, err := st.DeleteNotificationChannel(ctx, sqlc.DeleteNotificationChannelParams{
		ID:     override.ID,
		TeamID: pgtype.UUID{Bytes: uuid.New(), Valid: true},
	})
	if err != nil {
		t.Fatalf("DeleteNotificationChannel (foreign): %v", err)
	}
	if affected != 0 {
		t.Errorf("foreign delete affected = %d, want 0", affected)
	}
	affected, err = st.DeleteNotificationChannel(ctx, sqlc.DeleteNotificationChannelParams{
		ID:     override.ID,
		TeamID: teamID,
	})
	if err != nil {
		t.Fatalf("DeleteNotificationChannel: %v", err)
	}
	if affected != 1 {
		t.Errorf("delete affected = %d, want 1", affected)
	}

	// Deleting the team cascades the remaining channels.
	if err := st.DeleteTeam(ctx, teamID); err != nil {
		t.Fatalf("DeleteTeam: %v", err)
	}
	remaining, err := st.ListNotificationChannels(ctx, teamID)
	if err != nil {
		t.Fatalf("ListNotificationChannels after team delete: %v", err)
	}
	if len(remaining) != 0 {
		t.Errorf("channels after team delete = %d, want 0 (ON DELETE CASCADE)", len(remaining))
	}
}
