package instance

import (
	"context"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/store"
)

func TestPendingControlPlaneURLPersistence(t *testing.T) {
	// Use an explicit test database: the settings row is instance-wide.
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
	old, next := "https://old.example.com", "https://new.example.com"
	rec := original
	rec.ControlPlaneURL = &next
	rec.PendingControlPlaneURL = &PendingControlPlaneURL{Previous: &old, Proposed: next, Deadline: time.Now().UTC().Add(time.Minute).Truncate(time.Microsecond), Synced: true}
	if err := repo.Save(ctx, uuid.Nil, SectionGeneral, rec, map[string]any{"event": "trial"}); err != nil {
		t.Fatal(err)
	}
	restarted := NewStoreRepository(store.New(pool))
	loaded, err := restarted.Load(ctx)
	if err != nil || !reflect.DeepEqual(loaded.PendingControlPlaneURL, rec.PendingControlPlaneURL) {
		t.Fatalf("loaded=%+v err=%v", loaded, err)
	}
	loaded.ControlPlaneURL, loaded.PendingControlPlaneURL = loaded.PendingControlPlaneURL.Previous, nil
	if err := restarted.Save(ctx, uuid.Nil, SectionGeneral, loaded, map[string]any{"event": "revert"}); err != nil {
		t.Fatal(err)
	}
	loaded, err = repo.Load(ctx)
	if err != nil || loaded.PendingControlPlaneURL != nil || loaded.ControlPlaneURL == nil || *loaded.ControlPlaneURL != old {
		t.Fatalf("reverted=%+v err=%v", loaded, err)
	}
}
