package store_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/justindeelux/gotham/internal/store"
	"github.com/justindeelux/gotham/internal/store/sqlc"
)

// TestToolchainQueueContract exercises the generated toolchain-job queries
// against Postgres: the active-job de-dupe, ordered claiming, cancel scoped to
// queued jobs, and boot recovery scoped to running jobs. These are the SQL
// semantics the worker relies on, pinned separately from the worker's own unit
// tests.
func TestToolchainQueueContract(t *testing.T) {
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

	server, err := st.CreateServer(ctx, sqlc.CreateServerParams{
		Name:    fmt.Sprintf("jus176-node-%d", time.Now().UnixNano()),
		Ip:      "127.0.0.1",
		Port:    22,
		SshUser: "root",
	})
	if err != nil {
		t.Fatalf("CreateServer: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		if _, err := pool.Exec(cleanupCtx, "DELETE FROM servers WHERE id = $1", server.ID); err != nil {
			t.Logf("cleanup server: %v", err)
		}
	})

	enqueue := func(tool, action string, at time.Time) sqlc.ToolchainJob {
		t.Helper()
		job, err := st.CreateToolchainJob(ctx, sqlc.CreateToolchainJobParams{
			ServerID: server.ID,
			ToolID:   tool,
			Action:   action,
		})
		if err != nil {
			t.Fatalf("CreateToolchainJob(%s/%s): %v", tool, action, err)
		}
		// Pin created_at so the ordering assertion does not depend on clock
		// resolution between two inserts.
		if _, err := pool.Exec(ctx, "UPDATE toolchain_jobs SET created_at = $2 WHERE id = $1", job.ID, at); err != nil {
			t.Fatalf("pin created_at: %v", err)
		}
		job.CreatedAt = pgtype.Timestamptz{Time: at, Valid: true}
		return job
	}

	base := time.Now().UTC().Truncate(time.Second)

	// Active de-dupe: a second active job for the same node+tool+action is a
	// no-op (no rows). A different action is a distinct job, and re-enqueuing is
	// allowed once the first job is terminal (here, cancelled).
	superseded := enqueue("docker", "install", base)
	if _, err := st.CreateToolchainJob(ctx, sqlc.CreateToolchainJobParams{
		ServerID: server.ID, ToolID: "docker", Action: "install",
	}); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("duplicate active job err = %v, want pgx.ErrNoRows", err)
	}
	if _, err := st.CancelToolchainJob(ctx, superseded.ID); err != nil {
		t.Fatalf("cancel superseded: %v", err)
	}
	reinstall := enqueue("docker", "install", base.Add(time.Second))
	uninstall := enqueue("docker", "uninstall", base.Add(2*time.Second))
	railpack := enqueue("railpack", "install", base.Add(3*time.Second))
	git := enqueue("git", "install", base.Add(4*time.Second))

	// Claiming is oldest-first.
	wantOrder := []uuid.UUID{
		uuid.UUID(reinstall.ID.Bytes),
		uuid.UUID(uninstall.ID.Bytes),
		uuid.UUID(railpack.ID.Bytes),
		uuid.UUID(git.ID.Bytes),
	}
	for _, want := range wantOrder {
		claimed, err := st.ClaimNextQueuedToolchainJob(ctx)
		if err != nil {
			t.Fatalf("ClaimNextQueuedToolchainJob: %v", err)
		}
		if got := uuid.UUID(claimed.ID.Bytes); got != want {
			t.Fatalf("claimed %s, want %s (queue not oldest-first)", got, want)
		}
		if claimed.Status != "running" || !claimed.StartedAt.Valid {
			t.Fatalf("claimed job not running/started: %+v", claimed)
		}
	}
	if _, err := st.ClaimNextQueuedToolchainJob(ctx); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("empty claim err = %v, want pgx.ErrNoRows", err)
	}

	// Cancel only touches queued jobs: a running job is a no-op (no rows).
	if _, err := st.CancelToolchainJob(ctx, reinstall.ID); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("cancel running err = %v, want pgx.ErrNoRows", err)
	}
	// A queued job can be cancelled.
	queued := enqueue("caddy", "install", base.Add(5*time.Second))
	canceled, err := st.CancelToolchainJob(ctx, queued.ID)
	if err != nil {
		t.Fatalf("cancel queued: %v", err)
	}
	if canceled.Status != "canceled" || !canceled.FinishedAt.Valid {
		t.Fatalf("canceled job = %+v, want canceled with finished_at", canceled)
	}

	// Boot recovery fails only running jobs and appends the reason.
	ids, err := st.FailInterruptedToolchainJobs(ctx, "\ninterrupted\n")
	if err != nil {
		t.Fatalf("FailInterruptedToolchainJobs: %v", err)
	}
	if len(ids) != len(wantOrder) {
		t.Fatalf("interrupted count = %d, want %d (only running jobs)", len(ids), len(wantOrder))
	}
	recovered, err := st.GetToolchainJobForServer(ctx, sqlc.GetToolchainJobForServerParams{
		ID: reinstall.ID, ServerID: server.ID,
	})
	if err != nil {
		t.Fatalf("GetToolchainJobForServer: %v", err)
	}
	if recovered.Status != "failed" || !strings.Contains(recovered.Log, "interrupted") {
		t.Fatalf("recovered job = %+v, want failed with the reason appended", recovered)
	}
	if _, err := st.ClaimNextQueuedToolchainJob(ctx); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("recovery claim err = %v, want pgx.ErrNoRows", err)
	}
}
