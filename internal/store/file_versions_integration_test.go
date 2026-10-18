package store_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/justindeelux/gotham/internal/store"
	"github.com/justindeelux/gotham/internal/store/sqlc"
)

// TestFileVersionRetentionAndConfinement exercises JUS-224 history storage:
// the count and byte retention bounds are applied when a snapshot is inserted,
// and every read/delete is scoped to its (server, root, path) file key.
func TestFileVersionRetentionAndConfinement(t *testing.T) {
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

	var serverID uuid.UUID
	err = pool.QueryRow(ctx,
		`INSERT INTO servers (name, ip, port, ssh_user) VALUES ($1, '127.0.0.1', 22, 'root') RETURNING id`,
		fmt.Sprintf("jus224-%d", time.Now().UnixNano()),
	).Scan(&serverID)
	if err != nil {
		t.Fatalf("insert server: %v", err)
	}
	t.Cleanup(func() {
		if _, err := pool.Exec(context.Background(), `DELETE FROM servers WHERE id = $1`, serverID); err != nil {
			t.Logf("cleanup server: %v", err)
		}
	})

	snapshot := func(path, body string, maxVersions, maxBytes int64) {
		t.Helper()
		_, err := st.SaveFileVersion(ctx, sqlc.InsertFileVersionParams{
			ServerID: pgUUID(serverID),
			Root:     "compose",
			Path:     path,
			Etag:     fmt.Sprintf("%q", body),
			Size:     int64(len(body)),
			Content:  []byte(body),
		}, maxVersions, maxBytes)
		if err != nil {
			t.Fatalf("SaveFileVersion: %v", err)
		}
	}

	// Count retention: five saves, three kept, newest first.
	for i := 0; i < 5; i++ {
		snapshot("app/compose.yml", fmt.Sprintf("body-%d", i), 3, 1<<20)
	}
	list, err := st.ListFileVersions(ctx, sqlc.ListFileVersionsParams{
		ServerID: pgUUID(serverID), Root: "compose", Path: "app/compose.yml",
	})
	if err != nil {
		t.Fatalf("ListFileVersions: %v", err)
	}
	if len(list) != 3 {
		t.Fatalf("retained %d versions, want 3", len(list))
	}
	newest, err := st.GetFileVersion(ctx, sqlc.GetFileVersionParams{
		ID: list[0].ID, ServerID: pgUUID(serverID), Root: "compose", Path: "app/compose.yml",
	})
	if err != nil {
		t.Fatalf("GetFileVersion newest: %v", err)
	}
	if string(newest.Content) != "body-4" {
		t.Errorf("newest body = %q, want body-4", newest.Content)
	}
	oldest, err := st.GetFileVersion(ctx, sqlc.GetFileVersionParams{
		ID: list[2].ID, ServerID: pgUUID(serverID), Root: "compose", Path: "app/compose.yml",
	})
	if err != nil {
		t.Fatalf("GetFileVersion oldest: %v", err)
	}
	if string(oldest.Content) != "body-2" {
		t.Errorf("oldest retained body = %q, want body-2", oldest.Content)
	}

	// Byte retention: 10-byte bodies with a 25-byte cap keep the newest two.
	for i := 0; i < 4; i++ {
		snapshot("app/size.yml", "0123456789", 100, 25)
	}
	sizeList, err := st.ListFileVersions(ctx, sqlc.ListFileVersionsParams{
		ServerID: pgUUID(serverID), Root: "compose", Path: "app/size.yml",
	})
	if err != nil {
		t.Fatalf("ListFileVersions size: %v", err)
	}
	if len(sizeList) != 2 {
		t.Fatalf("retained %d byte-bound versions, want 2", len(sizeList))
	}

	// Confinement: a version id only resolves under its own file key.
	first := list[0]
	if _, err := st.GetFileVersion(ctx, sqlc.GetFileVersionParams{
		ID: first.ID, ServerID: pgUUID(serverID), Root: "compose", Path: "app/other.yml",
	}); !errors.Is(err, pgx.ErrNoRows) {
		t.Errorf("read under another path: err = %v, want ErrNoRows", err)
	}
	if removed, err := st.DeleteFileVersion(ctx, sqlc.DeleteFileVersionParams{
		ID: first.ID, ServerID: pgUUID(serverID), Root: "volumes", Path: "app/compose.yml",
	}); err != nil || removed {
		t.Errorf("delete under another root = (%v, %v), want (false, nil)", removed, err)
	}
	if removed, err := st.DeleteFileVersion(ctx, sqlc.DeleteFileVersionParams{
		ID: first.ID, ServerID: pgUUID(uuid.New()), Root: "compose", Path: "app/compose.yml",
	}); err != nil || removed {
		t.Errorf("delete under another server = (%v, %v), want (false, nil)", removed, err)
	}
	if removed, err := st.DeleteFileVersion(ctx, sqlc.DeleteFileVersionParams{
		ID: first.ID, ServerID: pgUUID(serverID), Root: "compose", Path: "app/compose.yml",
	}); err != nil || !removed {
		t.Errorf("delete own version = (%v, %v), want (true, nil)", removed, err)
	}
}
