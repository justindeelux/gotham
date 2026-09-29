package store_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	_ "github.com/jackc/pgx/v5/stdlib"

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

// TestNotificationChannelResourceConstraint applies the migrations on a
// disposable database and proves the resource-pair CHECK rejects both half
// pairs, including the NULL-type/non-NULL-id case a plain IN test would let
// through as SQL NULL.
func TestNotificationChannelResourceConstraint(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	base := testDSN()
	admin, err := sql.Open("pgx", dsnForDatabase(base, "postgres"))
	if err != nil {
		t.Fatalf("open maintenance connection: %v", err)
	}
	t.Cleanup(func() { _ = admin.Close() })
	if err := admin.PingContext(ctx); err != nil {
		if testDSNExplicit() {
			t.Fatalf("GOTHAM_TEST_DSN is set but Postgres is unavailable: %v", err)
		}
		t.Skipf("Postgres not available: %v", err)
	}

	scratch := fmt.Sprintf("be_8_3_notify_%d", time.Now().UnixNano())
	if _, err := admin.ExecContext(ctx, "CREATE DATABASE "+scratch); err != nil {
		if testDSNExplicit() {
			t.Fatalf("GOTHAM_TEST_DSN is set but a disposable database cannot be created: %v", err)
		}
		t.Skipf("cannot create a disposable database (needs CREATEDB): %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanupCancel()
		if _, err := admin.ExecContext(cleanupCtx, "DROP DATABASE IF EXISTS "+scratch+" WITH (FORCE)"); err != nil {
			t.Logf("drop scratch database: %v", err)
		}
	})

	scratchDSN := dsnForDatabase(base, scratch)
	if err := store.Migrate(ctx, scratchDSN, store.MigrateUp); err != nil {
		t.Fatalf("migrate scratch database: %v", err)
	}
	pool, err := store.Open(ctx, scratchDSN)
	if err != nil {
		t.Fatalf("open scratch pool: %v", err)
	}
	t.Cleanup(pool.Close)

	var teamID pgtype.UUID
	if err := pool.QueryRow(ctx, `INSERT INTO teams (name) VALUES ('be-8.3 constraint') RETURNING id`).Scan(&teamID); err != nil {
		t.Fatalf("insert team: %v", err)
	}

	insert := func(name string, resourceType any, resourceID any) error {
		t.Helper()
		_, err := pool.Exec(ctx, `
			INSERT INTO notification_channels (team_id, name, kind, config, resource_type, resource_id)
			VALUES ($1, $2, 'discord', 'sealed', $3, $4)`, teamID, name, resourceType, resourceID)
		return err
	}
	assertCheckViolation := func(label string, err error) {
		t.Helper()
		var pgErr *pgconn.PgError
		if !errors.As(err, &pgErr) || pgErr.Code != "23514" {
			t.Fatalf("%s: err = %v, want a CHECK violation (23514)", label, err)
		}
	}

	// Half pairs are rejected: the NULL-type case is the regression this test
	// guards (the arm must be FALSE, not SQL NULL).
	assertCheckViolation("type NULL with id set", insert("null type", nil, pgtype.UUID{Bytes: uuid.New(), Valid: true}))
	assertCheckViolation("type set with id NULL", insert("null id", "application", nil))
	assertCheckViolation("unknown type with id", insert("unknown type", "service", pgtype.UUID{Bytes: uuid.New(), Valid: true}))

	// Complete pairs stay valid.
	if err := insert("team wide", nil, nil); err != nil {
		t.Fatalf("team-wide insert: %v", err)
	}
	if err := insert("override", "database", pgtype.UUID{Bytes: uuid.New(), Valid: true}); err != nil {
		t.Fatalf("override insert: %v", err)
	}
}
