package store_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/justindeelux/gotham/internal/store"
	"github.com/justindeelux/gotham/internal/store/sqlc"
)

// TestProxyConfigHistorySequencing proves the R2 persistence contract against
// a real database: unchanged content records nothing, a replaced predecessor
// is retained for 24 hours measured from the replacement (not the original
// push), pruning only removes predecessors whose retention elapsed, and a
// pending record keeps the active version as the revert target.
func TestProxyConfigHistorySequencing(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	dsn := testDSN()
	explicit := testDSNExplicit()
	if err := store.Migrate(ctx, dsn, store.MigrateUp); err != nil {
		if explicit {
			t.Fatalf("GOTHAM_TEST_DSN is set but Postgres/migrations are unavailable: %v", err)
		}
		t.Skipf("Postgres not available: %v", err)
	}
	pool, err := store.Open(ctx, dsn)
	if err != nil {
		if explicit {
			t.Fatalf("GOTHAM_TEST_DSN is set but Postgres is unavailable: %v", err)
		}
		t.Skipf("Postgres not available: %v", err)
	}
	t.Cleanup(pool.Close)
	st := store.New(pool)

	suffix := time.Now().UnixNano()
	server, err := st.CreateServer(ctx, sqlc.CreateServerParams{
		Name:    fmt.Sprintf("p6-history-%d", suffix),
		Ip:      "127.0.0.1",
		Port:    22,
		SshUser: "root",
	})
	if err != nil {
		t.Fatalf("create server: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		if _, err := pool.Exec(cleanupCtx, "DELETE FROM servers WHERE id = $1", server.ID); err != nil {
			t.Logf("cleanup server: %v", err)
		}
	})

	content := func(marker string) []byte {
		return []byte(fmt.Sprintf(`[{"name":"dynamic/gotham.yml","content":"%s"}]`, marker))
	}
	hashOf := func(marker string) string { return "hash-" + marker }

	// A: first push, promoted and active.
	versionA, changed, err := st.PrepareProxyConfigVersion(ctx, server.ID, content("A"), hashOf("A"))
	if err != nil || !changed || !versionA.Pending {
		t.Fatalf("prepare A: changed=%v pending=%v err=%v", changed, versionA.Pending, err)
	}
	if err := st.PromoteProxyConfigVersion(ctx, server.ID, versionA.ID, pruneBefore()); err != nil {
		t.Fatalf("promote A: %v", err)
	}

	// Unchanged content records nothing and keeps A active.
	if _, changed, err = st.PrepareProxyConfigVersion(ctx, server.ID, content("A"), hashOf("A")); err != nil || changed {
		t.Fatalf("unchanged prepare: changed=%v err=%v, want false/nil", changed, err)
	}

	// Backdate A's original push: retention must be measured from replacement.
	if _, err := pool.Exec(ctx, "UPDATE proxy_config_versions SET created_at = now() - interval '3 days' WHERE id = $1", versionA.ID); err != nil {
		t.Fatalf("backdate A: %v", err)
	}

	// B replaces A; A must survive even though its push is older than 24h.
	versionB, _, err := st.PrepareProxyConfigVersion(ctx, server.ID, content("B"), hashOf("B"))
	if err != nil {
		t.Fatalf("prepare B: %v", err)
	}
	if err := st.PromoteProxyConfigVersion(ctx, server.ID, versionB.ID, pruneBefore()); err != nil {
		t.Fatalf("promote B: %v", err)
	}
	previous, err := st.PreviousProxyConfigVersion(ctx, server.ID)
	if err != nil || previous.ContentHash != hashOf("A") {
		t.Fatalf("previous after B = %q (err %v), want A", previous.ContentHash, err)
	}
	var supersededAt pgtype.Timestamptz
	if err := pool.QueryRow(ctx, "SELECT superseded_at FROM proxy_config_versions WHERE id = $1", versionA.ID).Scan(&supersededAt); err != nil {
		t.Fatalf("read A superseded_at: %v", err)
	}
	if !supersededAt.Valid || time.Since(supersededAt.Time) > time.Hour {
		t.Fatalf("A superseded_at = %v, want set at replacement time", supersededAt)
	}

	// Expire A's retention window, then C prunes A and B becomes previous.
	if _, err := pool.Exec(ctx, "UPDATE proxy_config_versions SET superseded_at = now() - interval '25 hours' WHERE id = $1", versionA.ID); err != nil {
		t.Fatalf("expire A: %v", err)
	}
	versionC, _, err := st.PrepareProxyConfigVersion(ctx, server.ID, content("C"), hashOf("C"))
	if err != nil {
		t.Fatalf("prepare C: %v", err)
	}
	if err := st.PromoteProxyConfigVersion(ctx, server.ID, versionC.ID, pruneBefore()); err != nil {
		t.Fatalf("promote C: %v", err)
	}
	var countA int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM proxy_config_versions WHERE id = $1", versionA.ID).Scan(&countA); err != nil {
		t.Fatalf("count A: %v", err)
	}
	if countA != 0 {
		t.Fatal("expired predecessor A was not pruned")
	}
	previous, err = st.PreviousProxyConfigVersion(ctx, server.ID)
	if err != nil || previous.ContentHash != hashOf("B") {
		t.Fatalf("previous after C = %q (err %v), want B", previous.ContentHash, err)
	}

	// A pending D records the intent but never becomes the revert target: the
	// active C stays the actual prior while the node may serve D.
	if _, _, err := st.PrepareProxyConfigVersion(ctx, server.ID, content("D"), hashOf("D")); err != nil {
		t.Fatalf("prepare D: %v", err)
	}
	pending, err := st.PendingProxyConfigVersion(ctx, server.ID)
	if err != nil || pending.ContentHash != hashOf("D") {
		t.Fatalf("pending = %q (err %v), want D", pending.ContentHash, err)
	}
	active, err := st.NewestActiveProxyConfigVersion(ctx, server.ID)
	if err != nil || active.ContentHash != hashOf("C") {
		t.Fatalf("active = %q (err %v), want C", active.ContentHash, err)
	}

	// Abort drops the pending intent; B is the predecessor again.
	if err := st.AbortProxyConfigVersion(ctx, server.ID, pending.ID); err != nil {
		t.Fatalf("abort D: %v", err)
	}
	if _, err := st.PendingProxyConfigVersion(ctx, server.ID); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("pending after abort err = %v, want no rows", err)
	}
	previous, err = st.PreviousProxyConfigVersion(ctx, server.ID)
	if err != nil || previous.ContentHash != hashOf("B") {
		t.Fatalf("previous after abort = %q (err %v), want B", previous.ContentHash, err)
	}
}

// pruneBefore is the retention cutoff used by the tests (24h before now).
func pruneBefore() pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: time.Now().Add(-24 * time.Hour), Valid: true}
}
