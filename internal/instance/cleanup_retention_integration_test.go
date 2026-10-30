package instance

import (
	"bytes"
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/store"
)

func TestCleanupRetentionPersistenceAndAudit(t *testing.T) {
	dsn := os.Getenv("GOTHAM_TEST_DSN")
	if dsn == "" {
		t.Skip("GOTHAM_TEST_DSN is required for singleton settings persistence")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool, err := store.Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if err := store.Migrate(ctx, dsn, store.MigrateUp); err != nil {
		t.Fatal(err)
	}
	repo := NewStoreRepository(store.New(pool))
	original, err := repo.Load(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := repo.SaveCleanup(context.Background(), uuid.Nil, original, map[string]any{"event": "test cleanup"}); err != nil {
			t.Error(err)
		}
	}()

	want := Retention{HistoryDays: 30, BackupFileMinAgeDays: 14}
	rec := original
	rec.Retention = want
	if err := repo.SaveCleanup(ctx, uuid.Nil, rec, map[string]any{
		"history_days":             want.HistoryDays,
		"backup_file_min_age_days": want.BackupFileMinAgeDays,
	}); err != nil {
		t.Fatal(err)
	}
	loaded, err := NewStoreRepository(store.New(pool)).Load(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Retention != want {
		t.Fatalf("loaded retention = %+v; want %+v", loaded.Retention, want)
	}

	var section string
	var changes []byte
	if err := pool.QueryRow(ctx,
		`SELECT section, changes FROM instance_settings_audit ORDER BY id DESC LIMIT 1`).
		Scan(&section, &changes); err != nil {
		t.Fatal(err)
	}
	if section != SectionCleanup || !bytes.Contains(changes, []byte(`"history_days": 30`)) {
		t.Fatalf("audit = %s %s; want a cleanup row with the new window", section, changes)
	}
}
