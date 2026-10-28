package notifications

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/justindeelux/gotham/internal/store"
	"github.com/justindeelux/gotham/internal/store/sqlc"
)

const defaultTestDSN = "postgres://gotham:gotham@localhost:5432/gotham?sslmode=disable"

func scratchNotificationStore(t *testing.T) *store.Store {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	dsn := os.Getenv("GOTHAM_TEST_DSN")
	explicit := dsn != ""
	if dsn == "" {
		dsn = defaultTestDSN
	}
	if err := store.ProbeOnce(ctx, dsn); err != nil {
		if explicit {
			t.Fatalf("GOTHAM_TEST_DSN is set but Postgres is unavailable: %v", err)
		}
		t.Skipf("Postgres not available: %v", err)
	}
	parsed, err := url.Parse(dsn)
	if err != nil {
		t.Fatalf("parse DSN: %v", err)
	}
	name := fmt.Sprintf("ntf%d%d", time.Now().UnixNano(), os.Getpid())
	admin, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("admin connect: %v", err)
	}
	if _, err := admin.Exec(ctx, `CREATE DATABASE "`+name+`"`); err != nil {
		_ = admin.Close(ctx)
		t.Fatalf("create scratch database: %v", err)
	}
	_ = admin.Close(ctx)
	parsed.Path = "/" + name
	scratchDSN := parsed.String()
	if err := store.Migrate(ctx, scratchDSN, store.MigrateUp); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	pool, err := store.Open(ctx, scratchDSN)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() {
		pool.Close()
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		conn, err := pgx.Connect(cleanupCtx, dsn)
		if err != nil {
			t.Logf("reconnect for drop: %v", err)
			return
		}
		defer conn.Close(cleanupCtx)
		_, _ = conn.Exec(cleanupCtx, `DROP DATABASE IF EXISTS "`+name+`" WITH (FORCE)`)
	})
	return store.New(pool)
}

func integrationChannel(t *testing.T, st *store.Store) Channel {
	t.Helper()
	sealed, err := sealConfig(testSecret, ChannelConfig{WebhookURL: "https://discord.com/api/webhooks/1/abc"})
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	created, err := st.CreateNotificationChannel(context.Background(), sqlc.CreateNotificationChannelParams{
		ID:         pgUUID(uuid.New()),
		Name:       "alerts",
		Kind:       "discord",
		Config:     sealed,
		Enabled:    true,
		ResourceID: pgtype.UUID{},
		Events:     stringsFromEvents(AllEvents),
	})
	if err != nil {
		t.Fatalf("create channel: %v", err)
	}
	return channelFromRow(created)
}
