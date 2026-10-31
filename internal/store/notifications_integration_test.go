package store_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

	"github.com/justindeelux/gotham/internal/store"
	"github.com/justindeelux/gotham/internal/store/sqlc"
)

func TestNotificationChannelPersistence(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	dsn := testDSN()
	if err := store.ProbeOnce(ctx, dsn); err != nil {
		if testDSNExplicit() {
			t.Fatalf("GOTHAM_TEST_DSN is set but Postgres is unavailable: %v", err)
		}
		t.Skipf("Postgres not available: %v", err)
	}
	pool, err := store.Open(ctx, dsn)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(pool.Close)
	if err := store.Migrate(ctx, dsn, store.MigrateUp); err != nil {
		t.Fatalf("migration failed: %v", err)
	}
	st := store.New(pool)
	if _, err := pool.Exec(ctx, "DELETE FROM notification_channels"); err != nil {
		t.Fatalf("reset notification channels: %v", err)
	}

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
	})

	channelID := uuid.New()
	created, err := st.CreateNotificationChannel(ctx, sqlc.CreateNotificationChannelParams{
		ID:           pgtype.UUID{Bytes: channelID, Valid: true},
		Name:         "alerts",
		Kind:         "discord",
		Config:       "sealed-config",
		Enabled:      true,
		ResourceType: nil,
		ResourceID:   pgtype.UUID{},
		Events:       []string{"deploy_failure", "backup_failure"},
		CreatedBy:    user.ID,
	})
	if err != nil {
		t.Fatalf("CreateNotificationChannel: %v", err)
	}
	if len(created.Events) != 2 || created.Events[0] != "deploy_failure" {
		t.Errorf("events = %v, want the stored subscription", created.Events)
	}
	if created.CreatedBy != user.ID {
		t.Errorf("created_by = %v, want the creating user", created.CreatedBy)
	}
	if created.ResourceType != nil {
		t.Errorf("resource_type = %v, want NULL for a instance-wide channel", *created.ResourceType)
	}
	if created.CreatedAt.Time.IsZero() {
		t.Error("created_at is not set")
	}

	appID := uuid.New()
	resourceType := "application"
	override, err := st.CreateNotificationChannel(ctx, sqlc.CreateNotificationChannelParams{
		ID:           pgtype.UUID{Bytes: uuid.New(), Valid: true},
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

	forEvent, err := st.ListNotificationChannelsForEvent(ctx, sqlc.ListNotificationChannelsForEventParams{
		ResourceType: &resourceType,
		ResourceID:   pgtype.UUID{Bytes: appID, Valid: true},
	})
	if err != nil {
		t.Fatalf("ListNotificationChannelsForEvent: %v", err)
	}
	if len(forEvent) != 2 {
		t.Fatalf("channels for the app event = %d, want the instance-wide and the override", len(forEvent))
	}

	otherApp := uuid.New()
	forEvent, err = st.ListNotificationChannelsForEvent(ctx, sqlc.ListNotificationChannelsForEventParams{
		ResourceType: &resourceType,
		ResourceID:   pgtype.UUID{Bytes: otherApp, Valid: true},
	})
	if err != nil {
		t.Fatalf("ListNotificationChannelsForEvent (other app): %v", err)
	}
	if len(forEvent) != 1 || forEvent[0].ID.Bytes != channelID {
		t.Fatalf("channels for another app = %d, want only the instance-wide row", len(forEvent))
	}

	disabled := false
	if _, err := st.UpdateNotificationChannel(ctx, sqlc.UpdateNotificationChannelParams{
		ID:           created.ID,
		Name:         "alerts",
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
		ResourceType: &resourceType,
		ResourceID:   pgtype.UUID{Bytes: otherApp, Valid: true},
	})
	if err != nil {
		t.Fatalf("ListNotificationChannelsForEvent (disabled): %v", err)
	}
	if len(forEvent) != 0 {
		t.Fatalf("channels = %d, want none: the only instance-wide row is disabled", len(forEvent))
	}

	all, err := st.ListNotificationChannels(ctx)
	if err != nil {
		t.Fatalf("ListNotificationChannels: %v", err)
	}
	if len(all) != 2 {
		t.Errorf("channels = %d, want 2", len(all))
	}

	for i := 0; i < 55; i++ {
		if _, err := st.CreateNotificationDelivery(ctx, sqlc.CreateNotificationDeliveryParams{
			ID:           pgtype.UUID{Bytes: uuid.New(), Valid: true},
			ChannelID:    created.ID,
			EventType:    "deploy_failure",
			Status:       "delivered",
			ErrorSummary: "connection failed",
			CreatedAt:    pgtype.Timestamptz{Time: time.Now().UTC().Add(time.Duration(i) * time.Second), Valid: true},
		}); err != nil {
			t.Fatalf("CreateNotificationDelivery: %v", err)
		}
		if _, err := st.PruneNotificationDeliveries(ctx, sqlc.PruneNotificationDeliveriesParams{
			ChannelID: created.ID,
			Limit:     50,
		}); err != nil {
			t.Fatalf("PruneNotificationDeliveries: %v", err)
		}
	}
	history, err := st.ListNotificationDeliveries(ctx, sqlc.ListNotificationDeliveriesParams{
		ChannelID: created.ID,
		Limit:     100,
	})
	if err != nil {
		t.Fatalf("ListNotificationDeliveries: %v", err)
	}
	if len(history) != 50 {
		t.Fatalf("history = %d rows, want the 50-row cap", len(history))
	}
	if !history[0].CreatedAt.Time.After(history[len(history)-1].CreatedAt.Time) {
		t.Error("history is not newest-first")
	}
	affected, err := st.DeleteNotificationChannel(ctx, override.ID)
	if err != nil {
		t.Fatalf("DeleteNotificationChannel: %v", err)
	}
	if affected != 1 {
		t.Errorf("delete affected = %d, want 1", affected)
	}

	if _, err := st.DeleteNotificationChannel(ctx, created.ID); err != nil {
		t.Fatalf("DeleteNotificationChannel: %v", err)
	}
	remaining, err := st.ListNotificationChannels(ctx)
	if err != nil {
		t.Fatalf("ListNotificationChannels after channel delete: %v", err)
	}
	if len(remaining) != 0 {
		t.Errorf("channels after channel delete = %d, want 0", len(remaining))
	}
	remainingHistory, err := st.ListNotificationDeliveries(ctx, sqlc.ListNotificationDeliveriesParams{
		ChannelID: created.ID,
		Limit:     100,
	})
	if err != nil {
		t.Fatalf("ListNotificationDeliveries after channel delete: %v", err)
	}
	if len(remainingHistory) != 0 {
		t.Errorf("deliveries after channel delete = %d, want 0 (ON DELETE CASCADE)", len(remainingHistory))
	}
}

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

	insert := func(name string, resourceType any, resourceID any) error {
		t.Helper()
		_, err := pool.Exec(ctx, `
			INSERT INTO notification_channels (name, kind, config, resource_type, resource_id)
			VALUES ($1, 'discord', 'sealed', $2, $3)`, name, resourceType, resourceID)
		return err
	}
	assertCheckViolation := func(label string, err error) {
		t.Helper()
		var pgErr *pgconn.PgError
		if !errors.As(err, &pgErr) || pgErr.Code != "23514" {
			t.Fatalf("%s: err = %v, want a CHECK violation (23514)", label, err)
		}
	}

	assertCheckViolation("type NULL with id set", insert("null type", nil, pgtype.UUID{Bytes: uuid.New(), Valid: true}))
	assertCheckViolation("type set with id NULL", insert("null id", "application", nil))
	assertCheckViolation("unknown type with id", insert("unknown type", "service", pgtype.UUID{Bytes: uuid.New(), Valid: true}))

	if err := insert("instance wide", nil, nil); err != nil {
		t.Fatalf("instance-wide insert: %v", err)
	}
	if err := insert("override", "database", pgtype.UUID{Bytes: uuid.New(), Valid: true}); err != nil {
		t.Fatalf("override insert: %v", err)
	}
}

func newDeliveryTestChannel(t *testing.T) (*store.Store, uuid.UUID) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	dsn := testDSN()
	if err := store.ProbeOnce(ctx, dsn); err != nil {
		if testDSNExplicit() {
			t.Fatalf("GOTHAM_TEST_DSN is set but Postgres is unavailable: %v", err)
		}
		t.Skipf("Postgres not available: %v", err)
	}
	pool, err := store.Open(ctx, dsn)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(pool.Close)
	if err := store.Migrate(ctx, dsn, store.MigrateUp); err != nil {
		t.Fatalf("migration failed: %v", err)
	}
	st := store.New(pool)

	email := fmt.Sprintf("jus195-delivery-%d@example.com", time.Now().UnixNano())
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
	})

	channelID := uuid.New()
	if _, err := st.CreateNotificationChannel(ctx, sqlc.CreateNotificationChannelParams{
		ID:           pgtype.UUID{Bytes: channelID, Valid: true},
		Name:         "history",
		Kind:         "discord",
		Config:       "sealed",
		Enabled:      true,
		ResourceType: nil,
		ResourceID:   pgtype.UUID{},
		Events:       []string{"deploy_failure"},
	}); err != nil {
		t.Fatalf("CreateNotificationChannel: %v", err)
	}
	return st, channelID
}

func TestNotificationDeliveryRetentionUnderConcurrency(t *testing.T) {
	st, channelID := newDeliveryTestChannel(t)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	const cap = 50
	const writers = 8
	const each = 25
	channelKey := pgtype.UUID{Bytes: channelID, Valid: true}

	var wg sync.WaitGroup
	errs := make(chan error, writers)
	for w := 0; w < writers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < each; i++ {
				err := st.InTx(ctx, func(tx *store.Store) error {
					if err := tx.LockNotificationChannelHistory(ctx, channelID.String()); err != nil {
						return err
					}
					if _, err := tx.CreateNotificationDelivery(ctx, sqlc.CreateNotificationDeliveryParams{
						ID:           pgtype.UUID{Bytes: uuid.New(), Valid: true},
						ChannelID:    channelKey,
						EventType:    "deploy_failure",
						Status:       "delivered",
						ErrorSummary: "connection failed",
						CreatedAt:    pgtype.Timestamptz{Time: time.Now().UTC(), Valid: true},
					}); err != nil {
						return err
					}
					_, err := tx.PruneNotificationDeliveries(ctx, sqlc.PruneNotificationDeliveriesParams{
						ChannelID: channelKey,
						Limit:     cap,
					})
					return err
				})
				if err != nil {
					errs <- err
					return
				}
			}
		}()
	}
	wg.Wait()
	close(errs)
	if err := <-errs; err != nil {
		t.Fatalf("concurrent history write: %v", err)
	}

	history, err := st.ListNotificationDeliveries(ctx, sqlc.ListNotificationDeliveriesParams{
		ChannelID: channelKey,
		Limit:     1000,
	})
	if err != nil {
		t.Fatalf("ListNotificationDeliveries: %v", err)
	}
	if len(history) != cap {
		t.Errorf("history = %d rows, want exactly the %d cap", len(history), cap)
	}
}

func TestReliableDeliveryQueueQueries(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	st := scratchStore(t)

	channel, err := st.CreateNotificationChannel(ctx, sqlc.CreateNotificationChannelParams{
		ID:         pgtype.UUID{Bytes: uuid.New(), Valid: true},
		Name:       "reliable",
		Kind:       "discord",
		Config:     "sealed",
		Enabled:    true,
		Events:     []string{"deploy_failure"},
		ResourceID: pgtype.UUID{},
	})
	if err != nil {
		t.Fatalf("create channel: %v", err)
	}
	now := time.Now().UTC()
	incidentID := uuid.New()
	deliveryID := uuid.New()
	if _, err := st.DB.Exec(ctx, `INSERT INTO incidents (id, identity_key, detector_key, scope_kind, severity, lifecycle, first_observed_at, last_observed_at, created_at, updated_at) VALUES ($1,$2,$3,'global','critical','firing',$4,$4,$4,$4)`, incidentID, "identity-"+incidentID.String(), "detector", now); err != nil {
		t.Fatalf("seed incident: %v", err)
	}
	params := sqlc.CreatePendingDeliveriesParams{
		Ids:         []pgtype.UUID{{Bytes: deliveryID, Valid: true}, {Bytes: uuid.New(), Valid: true}},
		ChannelIds:  []pgtype.UUID{channel.ID, channel.ID},
		DedupeKeys:  []string{"incident:one", "incident:one"},
		Payloads:    []string{`{"Kind":"incident"}`, `{"Kind":"incident"}`},
		IncidentIds: []pgtype.UUID{{Bytes: incidentID, Valid: true}, {Bytes: incidentID, Valid: true}},
		EventType:   "incident_opened",
		CreatedAt:   pgtype.Timestamptz{Time: now.Add(-time.Minute), Valid: true},
		ExpiresAt:   pgtype.Timestamptz{Time: now.Add(23 * time.Hour), Valid: true},
	}
	inserted, err := st.CreatePendingDeliveries(ctx, params)
	if err != nil {
		t.Fatalf("CreatePendingDeliveries: %v", err)
	}
	if len(inserted) != 1 {
		t.Fatalf("inserted = %d, want one row after the duplicate conflicts", len(inserted))
	}

	claimed, err := st.ClaimNotificationDeliveries(ctx, sqlc.ClaimNotificationDeliveriesParams{
		Now:        pgtype.Timestamptz{Time: now, Valid: true},
		LeaseUntil: pgtype.Timestamptz{Time: now.Add(2 * time.Minute), Valid: true},
		Batch:      10,
	})
	if err != nil {
		t.Fatalf("ClaimNotificationDeliveries: %v", err)
	}
	if len(claimed) != 1 || claimed[0].Status != "sending" || claimed[0].Attempts != 1 || !claimed[0].LeaseUntil.Valid {
		t.Fatalf("claimed = %+v, want one leased sending row", claimed)
	}

	delivered := now
	if err := st.FinishNotificationDelivery(ctx, sqlc.FinishNotificationDeliveryParams{
		Status:        "delivered",
		ErrorSummary:  "",
		NextAttemptAt: pgtype.Timestamptz{Time: now, Valid: true},
		CompletedAt:   pgtype.Timestamptz{Time: delivered, Valid: true},
		UpdatedAt:     pgtype.Timestamptz{Time: now, Valid: true},
		ID:            claimed[0].ID,
	}); err != nil {
		t.Fatalf("FinishNotificationDelivery: %v", err)
	}
	hasOpening, err := st.HasDeliveredIncidentOpening(ctx, sqlc.HasDeliveredIncidentOpeningParams{
		IncidentID: pgtype.UUID{Bytes: incidentID, Valid: true},
		ChannelID:  channel.ID,
	})
	if err != nil || !hasOpening {
		t.Fatalf("HasDeliveredIncidentOpening = %v, %v; want true", hasOpening, err)
	}

	health, err := st.ListNotificationChannelHealth(ctx)
	if err != nil || len(health) != 1 || !health[0].LastDeliveredAt.Valid {
		t.Fatalf("health = %+v, %v; want a delivered channel", health, err)
	}

	// Inbox deduplication: the second insert with the same key returns no row.
	notification := sqlc.CreateNotificationParams{
		ID:        pgtype.UUID{Bytes: uuid.New(), Valid: true},
		EventKey:  "incident_opened",
		Name:      "app",
		CreatedAt: pgtype.Timestamptz{Time: now, Valid: true},
		DedupeKey: "incident:one",
	}
	if _, err := st.CreateNotification(ctx, notification); err != nil {
		t.Fatalf("CreateNotification: %v", err)
	}
	notification.ID = pgtype.UUID{Bytes: uuid.New(), Valid: true}
	if _, err := st.CreateNotification(ctx, notification); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("duplicate CreateNotification err = %v, want pgx.ErrNoRows", err)
	}

	// Lease recovery after a simulated crash.
	staleID := uuid.New()
	if err := st.InTx(ctx, func(tx *store.Store) error {
		_, err := tx.CreateNotificationDelivery(ctx, sqlc.CreateNotificationDeliveryParams{
			ID:          pgtype.UUID{Bytes: staleID, Valid: true},
			ChannelID:   channel.ID,
			EventType:   "deploy_failure",
			Status:      "sending",
			CreatedAt:   pgtype.Timestamptz{Time: now.Add(-time.Hour), Valid: true},
			Column10:    pgtype.Timestamptz{Time: now, Valid: true},
			Column12:    pgtype.Timestamptz{Time: now.Add(24 * time.Hour), Valid: true},
			DedupeKey:   "stale",
			Payload:     `{}`,
			IncidentID:  pgtype.UUID{},
			CompletedAt: pgtype.Timestamptz{},
		})
		return err
	}); err != nil {
		t.Fatalf("seed stale sending row: %v", err)
	}
	if _, err := st.DB.Exec(ctx, `UPDATE notification_deliveries SET lease_until = $1 WHERE id = $2`, now.Add(-time.Minute), staleID); err != nil {
		t.Fatalf("expire lease: %v", err)
	}
	if n, err := st.ReclaimNotificationDeliveries(ctx, sqlc.ReclaimNotificationDeliveriesParams{
		Now:         pgtype.Timestamptz{Time: now, Valid: true},
		MaxAttempts: 8,
	}); err != nil || n < 1 {
		t.Fatalf("ReclaimNotificationDeliveries = %d, %v; want the stale lease", n, err)
	}

	// Terminal failure listing and owner retry.
	if err := st.FinishNotificationDelivery(ctx, sqlc.FinishNotificationDeliveryParams{
		Status:        "terminal_failed",
		ErrorSummary:  "answered status 500",
		NextAttemptAt: pgtype.Timestamptz{Time: now, Valid: true},
		CompletedAt:   pgtype.Timestamptz{Time: now, Valid: true},
		UpdatedAt:     pgtype.Timestamptz{Time: now, Valid: true},
		ID:            pgtype.UUID{Bytes: staleID, Valid: true},
	}); err != nil {
		t.Fatalf("finish terminal: %v", err)
	}
	failures, err := st.ListTerminalDeliveryFailures(ctx, 10)
	if err != nil || len(failures) != 1 || failures[0].ChannelName != "reliable" {
		t.Fatalf("failures = %+v, %v", failures, err)
	}
	if latest, err := st.ListLatestDeliveryFailures(ctx); err != nil || len(latest) != 1 {
		t.Fatalf("latest failures = %+v, %v", latest, err)
	}
	if n, err := st.RetryNotificationDelivery(ctx, sqlc.RetryNotificationDeliveryParams{
		Now:       pgtype.Timestamptz{Time: now, Valid: true},
		ExpiresAt: pgtype.Timestamptz{Time: now.Add(24 * time.Hour), Valid: true},
		ID:        pgtype.UUID{Bytes: staleID, Valid: true},
	}); err != nil || n != 1 {
		t.Fatalf("RetryNotificationDelivery = %d, %v", n, err)
	}
	retried, err := st.GetNotificationDelivery(ctx, pgtype.UUID{Bytes: staleID, Valid: true})
	if err != nil || retried.Status != "pending" || retried.Attempts != 0 {
		t.Fatalf("retried = %+v, %v", retried, err)
	}
	if n, err := st.RetryNotificationDelivery(ctx, sqlc.RetryNotificationDeliveryParams{
		Now:       pgtype.Timestamptz{Time: now, Valid: true},
		ExpiresAt: pgtype.Timestamptz{Time: now.Add(24 * time.Hour), Valid: true},
		ID:        pgtype.UUID{Bytes: staleID, Valid: true},
	}); err != nil || n != 0 {
		t.Fatalf("duplicate retry = %d, %v; want no second transition", n, err)
	}

	// Cancellation and the bounded cleanups.
	if err := st.CancelNotificationDelivery(ctx, sqlc.CancelNotificationDeliveryParams{
		ErrorSummary: "channel disabled or unsubscribed",
		CompletedAt:  pgtype.Timestamptz{Time: now, Valid: true},
		ID:           pgtype.UUID{Bytes: staleID, Valid: true},
	}); err != nil {
		t.Fatalf("CancelNotificationDelivery: %v", err)
	}
	oldPending := uuid.New()
	if _, err := st.DB.Exec(ctx, `INSERT INTO notification_deliveries (id, channel_id, event_type, status, created_at, dedupe_key, next_attempt_at, expires_at) VALUES ($1,$2,'deploy_failure','pending',$3,$4,$3,$3)`, oldPending, channel.ID, now.Add(-25*time.Hour), "old-pending"); err != nil {
		t.Fatalf("seed old pending row: %v", err)
	}
	if n, err := st.ExpireNotificationDeliveries(ctx, pgtype.Timestamptz{Time: now, Valid: true}); err != nil || n < 1 {
		t.Fatalf("ExpireNotificationDeliveries = %d, %v; want the 24-hour-old pending row", n, err)
	}
	oldTerminal := uuid.New()
	if _, err := st.DB.Exec(ctx, `INSERT INTO notification_deliveries (id, channel_id, event_type, status, created_at, completed_at, dedupe_key, next_attempt_at) VALUES ($1,$2,'deploy_failure','terminal_failed',$3,$3,$4,$3)`, oldTerminal, channel.ID, now.Add(-40*24*time.Hour), "old-terminal"); err != nil {
		t.Fatalf("seed old terminal row: %v", err)
	}
	if n, err := st.CleanupNotificationDeliveries(ctx, pgtype.Timestamptz{Time: now.Add(-30 * 24 * time.Hour), Valid: true}); err != nil || n < 1 {
		t.Fatalf("CleanupNotificationDeliveries = %d, %v; want terminal rows removed", n, err)
	}
}

func seedTestChannel(t *testing.T, st *store.Store) sqlc.NotificationChannel {
	t.Helper()
	channel, err := st.CreateNotificationChannel(context.Background(), sqlc.CreateNotificationChannelParams{
		ID:         pgtype.UUID{Bytes: uuid.New(), Valid: true},
		Name:       "retention",
		Kind:       "discord",
		Config:     "sealed",
		Enabled:    true,
		Events:     []string{"deploy_failure"},
		ResourceID: pgtype.UUID{},
	})
	if err != nil {
		t.Fatalf("create channel: %v", err)
	}
	return channel
}

func TestRetryResetsDeliveryExpiry(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	st := scratchStore(t)
	channel := seedTestChannel(t, st)
	now := time.Now().UTC()
	deliveryID := uuid.New()
	if _, err := st.DB.Exec(ctx, `INSERT INTO notification_deliveries (id, channel_id, event_type, status, error_summary, created_at, dedupe_key, next_attempt_at, expires_at, completed_at)
		VALUES ($1,$2,'deploy_failure','terminal_failed','answered status 401',$3,$4,$3,$5,$3)`,
		deliveryID, channel.ID, now.Add(-26*time.Hour), "old-terminal", now.Add(-2*time.Hour)); err != nil {
		t.Fatalf("seed old terminal row: %v", err)
	}

	if n, err := st.RetryNotificationDelivery(ctx, sqlc.RetryNotificationDeliveryParams{
		Now:       pgtype.Timestamptz{Time: now, Valid: true},
		ExpiresAt: pgtype.Timestamptz{Time: now.Add(24 * time.Hour), Valid: true},
		ID:        pgtype.UUID{Bytes: deliveryID, Valid: true},
	}); err != nil || n != 1 {
		t.Fatalf("RetryNotificationDelivery = %d, %v", n, err)
	}
	if n, err := st.ExpireNotificationDeliveries(ctx, pgtype.Timestamptz{Time: now.Add(time.Hour), Valid: true}); err != nil || n != 0 {
		t.Fatalf("Expire after retry = %d, %v; want the retried row to stay eligible", n, err)
	}
	row, err := st.GetNotificationDelivery(ctx, pgtype.UUID{Bytes: deliveryID, Valid: true})
	if err != nil || row.Status != "pending" || !row.ExpiresAt.Valid || !row.ExpiresAt.Time.After(now.Add(23*time.Hour)) {
		t.Fatalf("retried row = %+v, %v; want a fresh 24-hour expiry", row, err)
	}
	if n, err := st.ExpireNotificationDeliveries(ctx, pgtype.Timestamptz{Time: now.Add(25 * time.Hour), Valid: true}); err != nil || n != 1 {
		t.Fatalf("Expire after the retried 24 hours = %d, %v; want the row expired", n, err)
	}
}

func TestPruneKeepsInFlightAndActiveIncidentOpenings(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	st := scratchStore(t)
	channel := seedTestChannel(t, st)
	now := time.Now().UTC()

	incidentID := uuid.New()
	if _, err := st.DB.Exec(ctx, `INSERT INTO incidents (id, identity_key, detector_key, scope_kind, severity, lifecycle, first_observed_at, last_observed_at, created_at, updated_at)
		VALUES ($1,$2,'detector','global','critical','firing',$3,$3,$3,$3)`, incidentID, "identity-"+incidentID.String(), now); err != nil {
		t.Fatalf("seed incident: %v", err)
	}
	openingID := uuid.New()
	if _, err := st.DB.Exec(ctx, `INSERT INTO notification_deliveries (id, channel_id, event_type, status, created_at, dedupe_key, payload, incident_id, next_attempt_at, expires_at, completed_at)
		VALUES ($1,$2,'incident_opened','delivered',$3,'opening','{}',$4,$3,$3,$3)`, openingID, channel.ID, now.Add(-4*time.Hour), incidentID); err != nil {
		t.Fatalf("seed opening: %v", err)
	}
	retryingID := uuid.New()
	if _, err := st.DB.Exec(ctx, `INSERT INTO notification_deliveries (id, channel_id, event_type, status, created_at, dedupe_key, payload, next_attempt_at, expires_at)
		VALUES ($1,$2,'deploy_failure','retrying',$3,'retrying','{}',$3,$4)`, retryingID, channel.ID, now.Add(-3*time.Hour), now.Add(20*time.Hour)); err != nil {
		t.Fatalf("seed retrying: %v", err)
	}
	for i := 0; i < 55; i++ {
		id := uuid.New()
		at := now.Add(-time.Duration(i) * time.Minute)
		if _, err := st.DB.Exec(ctx, `INSERT INTO notification_deliveries (id, channel_id, event_type, status, created_at, dedupe_key, payload, next_attempt_at, expires_at, completed_at)
			VALUES ($1,$2,'deploy_failure','delivered',$3,$4,'{}',$3,$3,$3)`, id, channel.ID, at, "delivered-"+id.String()); err != nil {
			t.Fatalf("seed delivered row: %v", err)
		}
	}

	if _, err := st.PruneNotificationDeliveries(ctx, sqlc.PruneNotificationDeliveriesParams{
		ChannelID: channel.ID,
		Limit:     50,
	}); err != nil {
		t.Fatalf("PruneNotificationDeliveries: %v", err)
	}
	if _, err := st.GetNotificationDelivery(ctx, pgtype.UUID{Bytes: retryingID, Valid: true}); err != nil {
		t.Fatalf("retrying row was deleted by history prune: %v", err)
	}
	if _, err := st.GetNotificationDelivery(ctx, pgtype.UUID{Bytes: openingID, Valid: true}); err != nil {
		t.Fatalf("active incident opening was deleted by history prune: %v", err)
	}
	hasOpening, err := st.HasDeliveredIncidentOpening(ctx, sqlc.HasDeliveredIncidentOpeningParams{
		IncidentID: pgtype.UUID{Bytes: incidentID, Valid: true},
		ChannelID:  channel.ID,
	})
	if err != nil || !hasOpening {
		t.Fatalf("HasDeliveredIncidentOpening after prune = %v, %v; want the active opening kept", hasOpening, err)
	}
}

func TestDeliveredHistoryIsBoundedAndPayloadCleared(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	st := scratchStore(t)
	channel := seedTestChannel(t, st)
	now := time.Now().UTC()

	payloadID := uuid.New()
	if _, err := st.DB.Exec(ctx, `INSERT INTO notification_deliveries (id, channel_id, event_type, status, created_at, dedupe_key, payload, next_attempt_at, expires_at)
		VALUES ($1,$2,'deploy_failure','pending',$3,'payload-check','{"Kind":"deploy"}',$3,$4)`, payloadID, channel.ID, now, now.Add(24*time.Hour)); err != nil {
		t.Fatalf("seed payload row: %v", err)
	}
	if err := st.FinishNotificationDelivery(ctx, sqlc.FinishNotificationDeliveryParams{
		Status:        "delivered",
		ErrorSummary:  "",
		NextAttemptAt: pgtype.Timestamptz{Time: now, Valid: true},
		CompletedAt:   pgtype.Timestamptz{Time: now, Valid: true},
		UpdatedAt:     pgtype.Timestamptz{Time: now, Valid: true},
		ID:            pgtype.UUID{Bytes: payloadID, Valid: true},
	}); err != nil {
		t.Fatalf("FinishNotificationDelivery: %v", err)
	}
	cleared, err := st.GetNotificationDelivery(ctx, pgtype.UUID{Bytes: payloadID, Valid: true})
	if err != nil || cleared.Payload != "" {
		t.Fatalf("payload = %q, %v; want it cleared on completion", cleared.Payload, err)
	}
	terminalID := uuid.New()
	if _, err := st.DB.Exec(ctx, `INSERT INTO notification_deliveries (id, channel_id, event_type, status, created_at, dedupe_key, payload, next_attempt_at, expires_at)
		VALUES ($1,$2,'deploy_failure','pending',$3,'terminal-payload','{"Kind":"deploy"}',$3,$4)`, terminalID, channel.ID, now, now.Add(24*time.Hour)); err != nil {
		t.Fatalf("seed terminal payload row: %v", err)
	}
	if err := st.FinishNotificationDelivery(ctx, sqlc.FinishNotificationDeliveryParams{
		Status:        "terminal_failed",
		ErrorSummary:  "answered status 500",
		NextAttemptAt: pgtype.Timestamptz{Time: now, Valid: true},
		CompletedAt:   pgtype.Timestamptz{Time: now, Valid: true},
		UpdatedAt:     pgtype.Timestamptz{Time: now, Valid: true},
		ID:            pgtype.UUID{Bytes: terminalID, Valid: true},
	}); err != nil {
		t.Fatalf("finish terminal: %v", err)
	}
	retained, err := st.GetNotificationDelivery(ctx, pgtype.UUID{Bytes: terminalID, Valid: true})
	if err != nil || retained.Payload == "" {
		t.Fatalf("terminal payload = %q, %v; want it retained for owner Retry", retained.Payload, err)
	}

	for i := 0; i < 119; i++ {
		id := uuid.New()
		at := now.Add(-time.Duration(i) * time.Second)
		if _, err := st.DB.Exec(ctx, `INSERT INTO notification_deliveries (id, channel_id, event_type, status, created_at, dedupe_key, payload, next_attempt_at, expires_at, completed_at)
			VALUES ($1,$2,'deploy_failure','delivered',$3,$4,'{}',$3,$3,$3)`, id, channel.ID, at, "bound-"+id.String()); err != nil {
			t.Fatalf("seed delivered row: %v", err)
		}
	}
	if _, err := st.PruneNotificationDeliveries(ctx, sqlc.PruneNotificationDeliveriesParams{
		ChannelID: channel.ID,
		Limit:     50,
	}); err != nil {
		t.Fatalf("PruneNotificationDeliveries: %v", err)
	}
	var remaining int
	if err := st.DB.QueryRow(ctx, `SELECT count(*) FROM notification_deliveries WHERE channel_id=$1 AND status='delivered'`, channel.ID).Scan(&remaining); err != nil {
		t.Fatalf("count delivered: %v", err)
	}
	if remaining > 50 {
		t.Fatalf("delivered rows = %d, want the newest 50 bound", remaining)
	}
}

func TestHasLaterIncidentTransitionQuery(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	st := scratchStore(t)
	channel := seedTestChannel(t, st)
	now := time.Now().UTC()
	incidentID := uuid.New()
	if _, err := st.DB.Exec(ctx, `INSERT INTO incidents (id, identity_key, detector_key, scope_kind, severity, lifecycle, first_observed_at, last_observed_at, created_at, updated_at)
		VALUES ($1,$2,'detector','global','warning','resolved',$3,$3,$3,$3)`, incidentID, "identity-"+incidentID.String(), now); err != nil {
		t.Fatalf("seed incident: %v", err)
	}
	openingID, recoveryID := uuid.New(), uuid.New()
	if _, err := st.DB.Exec(ctx, `INSERT INTO notification_deliveries (id, channel_id, event_type, status, created_at, dedupe_key, payload, incident_id, next_attempt_at, expires_at, completed_at)
		VALUES ($1,$2,'incident_opened','terminal_failed',$3,'late-opening','{}',$4,$3,$3,$3)`, openingID, channel.ID, now.Add(-time.Hour), incidentID); err != nil {
		t.Fatalf("seed opening: %v", err)
	}
	if _, err := st.DB.Exec(ctx, `INSERT INTO notification_deliveries (id, channel_id, event_type, status, created_at, dedupe_key, payload, incident_id, next_attempt_at, expires_at, completed_at)
		VALUES ($1,$2,'incident_recovered','delivered',$3,'recovery','{}',$4,$3,$3,$3)`, recoveryID, channel.ID, now.Add(-30*time.Minute), incidentID); err != nil {
		t.Fatalf("seed recovery: %v", err)
	}
	ok, err := st.HasLaterIncidentTransition(ctx, sqlc.HasLaterIncidentTransitionParams{
		IncidentID: pgtype.UUID{Bytes: incidentID, Valid: true},
		ChannelID:  channel.ID,
		Column3:    pgtype.Timestamptz{Time: now.Add(-time.Hour), Valid: true},
		Column4:    pgtype.UUID{Bytes: openingID, Valid: true},
	})
	if err != nil || !ok {
		t.Fatalf("HasLaterIncidentTransition = %v, %v; want the recovery found", ok, err)
	}
}

func TestReliableDeliveryMigrationOnPopulatedSchema(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	base := testDSN()
	if err := store.ProbeOnce(ctx, base); err != nil {
		if testDSNExplicit() {
			t.Fatalf("GOTHAM_TEST_DSN is set but Postgres is unavailable: %v", err)
		}
		t.Skipf("Postgres not available: %v", err)
	}
	parsed, err := url.Parse(base)
	if err != nil {
		t.Fatalf("parse DSN: %v", err)
	}
	name := fmt.Sprintf("mig76%d%d", time.Now().UnixNano(), os.Getpid())
	admin, err := pgx.Connect(ctx, base)
	if err != nil {
		t.Fatalf("admin connect: %v", err)
	}
	if _, err := admin.Exec(ctx, `CREATE DATABASE "`+name+`"`); err != nil {
		_ = admin.Close(ctx)
		t.Fatalf("create scratch database: %v", err)
	}
	_ = admin.Close(ctx)
	parsed.Path = "/" + name
	dsn := parsed.String()
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		conn, err := pgx.Connect(cleanupCtx, base)
		if err == nil {
			_, _ = conn.Exec(cleanupCtx, `DROP DATABASE IF EXISTS "`+name+`" WITH (FORCE)`)
			_ = conn.Close(cleanupCtx)
		}
	})

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open sql: %v", err)
	}
	defer func() { _ = db.Close() }()
	provider, err := goose.NewProvider(goose.DialectPostgres, db, store.Migrations())
	if err != nil {
		t.Fatalf("init migrations: %v", err)
	}
	if _, err := provider.UpTo(ctx, 75); err != nil {
		t.Fatalf("migrate to 00075: %v", err)
	}
	channelID := uuid.New()
	if _, err := db.ExecContext(ctx, `INSERT INTO notification_channels (id, name, kind, config, enabled, events) VALUES ($1,'old','discord','sealed',true,'{deploy_failure}')`, channelID); err != nil {
		t.Fatalf("seed old channel: %v", err)
	}
	oldSuccess, oldFailure := uuid.New(), uuid.New()
	if _, err := db.ExecContext(ctx, `INSERT INTO notification_deliveries (id, channel_id, event_type, status, error_summary) VALUES ($1,$2,'deploy_success','success',''),($3,$2,'deploy_failure','failure','answered status 500')`, oldSuccess, channelID, oldFailure); err != nil {
		t.Fatalf("seed old deliveries: %v", err)
	}
	if _, err := provider.Up(ctx); err != nil {
		t.Fatalf("apply 00076 on a populated database: %v", err)
	}
	rows, err := db.QueryContext(ctx, `SELECT id, status, expires_at, payload FROM notification_deliveries ORDER BY created_at`)
	if err != nil {
		t.Fatalf("read migrated deliveries: %v", err)
	}
	defer func() { _ = rows.Close() }()
	count := 0
	for rows.Next() {
		var id uuid.UUID
		var status, payload string
		var expires sql.NullTime
		if err := rows.Scan(&id, &status, &expires, &payload); err != nil {
			t.Fatalf("scan migrated row: %v", err)
		}
		count++
		if id == oldSuccess && status != "delivered" {
			t.Errorf("old success row status = %q, want delivered", status)
		}
		if id == oldFailure && status != "terminal_failed" {
			t.Errorf("old failure row status = %q, want terminal_failed", status)
		}
		if !expires.Valid || payload != "" {
			t.Errorf("migrated row %s expires=%v payload=%q, want backfilled expiry and empty payload", id, expires, payload)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate migrated rows: %v", err)
	}
	if count != 2 {
		t.Fatalf("migrated rows = %d, want the two pre-existing rows", count)
	}
}
