package instance

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/store"
)

// A row written by a release that still ran the confirmation trial must load
// as applied and lose the retired column on the next save.
func TestLeftoverPendingControlPlaneURLIsIgnoredAndCleared(t *testing.T) {
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
		if err := repo.Save(context.Background(), uuid.Nil, SectionGeneral, original, map[string]any{"event": "test cleanup"}); err != nil {
			t.Error(err)
		}
	}()
	trial := `{"previous":"https://old.example.com","proposed":"https://new.example.com","deadline":"2026-01-01T00:00:00Z","synced":true}`
	if _, err := pool.Exec(ctx, `UPDATE instance_settings SET control_plane_url = $1, pending_control_plane_url = $2 WHERE id = 1`, "https://new.example.com", trial); err != nil {
		t.Fatal(err)
	}
	loaded, err := repo.Load(ctx)
	if err != nil || loaded.ControlPlaneURL == nil || *loaded.ControlPlaneURL != "https://new.example.com" {
		t.Fatalf("leftover trial blocked the upgrade: %+v err=%v", loaded, err)
	}
	if err := repo.Save(ctx, uuid.Nil, SectionGeneral, loaded, map[string]any{"event": "upgrade"}); err != nil {
		t.Fatal(err)
	}
	var pending *string
	if err := pool.QueryRow(ctx, `SELECT pending_control_plane_url::text FROM instance_settings WHERE id = 1`).Scan(&pending); err != nil {
		t.Fatal(err)
	}
	if pending != nil {
		t.Fatalf("retired trial column kept: %s", *pending)
	}
}
