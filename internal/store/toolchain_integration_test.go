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

// TestToolchainQueueContract exercises the generated toolchain-job queries
// against Postgres: the active-job de-dupe, oldest-first per-node claiming,
// cancel scoped to queued jobs, lease-scoped recovery, and the append-only log.
// These are the SQL semantics the worker relies on, pinned separately from the
// worker's own unit tests.
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

	newServer := func() sqlc.Server {
		t.Helper()
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
		return server
	}

	enqueue := func(serverID pgtype.UUID, tool, action string, at time.Time) sqlc.ToolchainJob {
		t.Helper()
		job, err := st.CreateToolchainJob(ctx, sqlc.CreateToolchainJobParams{
			ServerID: serverID,
			ToolID:   tool,
			Action:   action,
		})
		if err != nil {
			t.Fatalf("CreateToolchainJob(%s/%s): %v", tool, action, err)
		}
		if _, err := pool.Exec(ctx, "UPDATE toolchain_jobs SET created_at = $2 WHERE id = $1", job.ID, at); err != nil {
			t.Fatalf("pin created_at: %v", err)
		}
		job.CreatedAt = pgtype.Timestamptz{Time: at, Valid: true}
		return job
	}

	claim := func(instance string) sqlc.ToolchainJob {
		t.Helper()
		job, err := st.ClaimNextQueuedToolchainJob(ctx, sqlc.ClaimNextQueuedToolchainJobParams{
			ClaimedBy:    instance,
			LeaseSeconds: 90,
		})
		if err != nil {
			t.Fatalf("ClaimNextQueuedToolchainJob: %v", err)
		}
		return job
	}

	base := time.Now().UTC().Truncate(time.Second)
	serverA, serverB := newServer(), newServer()
	serverC, serverD := newServer(), newServer()

	// Active de-dupe: a second active job for the same node+tool+action is a
	// no-op; another action is a distinct job; re-enqueue is allowed once the
	// first is terminal.
	superseded := enqueue(serverA.ID, "docker", "install", base)
	if _, err := st.CreateToolchainJob(ctx, sqlc.CreateToolchainJobParams{
		ServerID: serverA.ID, ToolID: "docker", Action: "install",
	}); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("duplicate active job err = %v, want pgx.ErrNoRows", err)
	}
	if _, err := st.CancelToolchainJob(ctx, superseded.ID); err != nil {
		t.Fatalf("cancel superseded: %v", err)
	}
	a1 := enqueue(serverA.ID, "docker", "install", base.Add(time.Second))
	a2 := enqueue(serverA.ID, "git", "install", base.Add(2*time.Second))
	b1 := enqueue(serverB.ID, "railpack", "install", base.Add(3*time.Second))

	// Oldest-first, and per-node sequential: after claiming A's first job, A is
	// skipped (it has a running job) so B's job is claimed next; A's second job
	// waits until A's first finishes.
	first := claim("inst-1")
	if uuid.UUID(first.ID.Bytes) != uuid.UUID(a1.ID.Bytes) {
		t.Fatalf("first claim = %s, want a1", uuid.UUID(first.ID.Bytes))
	}
	second := claim("inst-1")
	if uuid.UUID(second.ID.Bytes) != uuid.UUID(b1.ID.Bytes) {
		t.Fatalf("second claim = %s, want b1 (A is busy, so B runs in parallel)", uuid.UUID(second.ID.Bytes))
	}
	if _, err := st.ClaimNextQueuedToolchainJob(ctx, sqlc.ClaimNextQueuedToolchainJobParams{
		ClaimedBy: "inst-1", LeaseSeconds: 90,
	}); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("claim while A busy err = %v, want pgx.ErrNoRows (A's second job blocked)", err)
	}
	if _, err := st.FinishToolchainJob(ctx, sqlc.FinishToolchainJobParams{ID: first.ID, Status: "succeeded"}); err != nil {
		t.Fatalf("finish a1: %v", err)
	}
	third := claim("inst-1")
	if uuid.UUID(third.ID.Bytes) != uuid.UUID(a2.ID.Bytes) {
		t.Fatalf("third claim = %s, want a2 after a1 finished", uuid.UUID(third.ID.Bytes))
	}
	if third.ClaimedBy != "inst-1" || !third.LeaseExpiresAt.Valid {
		t.Fatalf("claim did not record instance/lease: %+v", third)
	}

	// Cancel only touches queued jobs.
	if _, err := st.CancelToolchainJob(ctx, third.ID); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("cancel running err = %v, want pgx.ErrNoRows", err)
	}
	queued := enqueue(serverA.ID, "caddy", "install", base.Add(4*time.Second))
	canceled, err := st.CancelToolchainJob(ctx, queued.ID)
	if err != nil {
		t.Fatalf("cancel queued: %v", err)
	}
	if canceled.Status != "canceled" || !canceled.FinishedAt.Valid {
		t.Fatalf("canceled job = %+v, want canceled with finished_at", canceled)
	}

	// Recovery fails only this instance's jobs or jobs whose lease lapsed:
	// b1 and a2 are ours, lapsed is another instance's stale lease, and fresh is
	// another instance's live lease.
	lapsedJob := enqueue(serverC.ID, "lapsed", "install", base.Add(5*time.Second))
	freshJob := enqueue(serverD.ID, "fresh", "install", base.Add(6*time.Second))
	lapsed := claim("inst-2")
	if uuid.UUID(lapsed.ID.Bytes) != uuid.UUID(lapsedJob.ID.Bytes) {
		t.Fatalf("claim = %s, want lapsedJob on the idle node", uuid.UUID(lapsed.ID.Bytes))
	}
	if _, err := pool.Exec(ctx, "UPDATE toolchain_jobs SET lease_expires_at = now() - interval '1 minute' WHERE id = $1", lapsed.ID); err != nil {
		t.Fatalf("expire lease: %v", err)
	}
	fresh := claim("inst-2")
	if uuid.UUID(fresh.ID.Bytes) != uuid.UUID(freshJob.ID.Bytes) {
		t.Fatalf("claim = %s, want freshJob on the idle node", uuid.UUID(fresh.ID.Bytes))
	}
	ownedIDs, err := st.FailOwnedToolchainJobs(ctx, "inst-1")
	if err != nil {
		t.Fatalf("FailOwnedToolchainJobs: %v", err)
	}
	staleIDs, err := st.FailStaleToolchainJobs(ctx)
	if err != nil {
		t.Fatalf("FailStaleToolchainJobs: %v", err)
	}
	// Our two running jobs (b1 and a2) plus the lapsed one are failed; the
	// fresh peer job is untouched.
	if len(ownedIDs) != 2 || len(staleIDs) != 1 {
		t.Fatalf("recovered owned=%d stale=%d, want owned=2 stale=1 (not the live peer)", len(ownedIDs), len(staleIDs))
	}
	live, err := st.GetToolchainJobForServer(ctx, sqlc.GetToolchainJobForServerParams{ID: fresh.ID, ServerID: serverD.ID})
	if err != nil {
		t.Fatalf("GetToolchainJobForServer(fresh): %v", err)
	}
	if live.Status != "running" {
		t.Fatalf("fresh peer job status = %s, want running (untouched)", live.Status)
	}

	// Append-only log: line rows with a monotonic seq, ordered reads, an after
	// cursor, and idempotent appends.
	logJob := enqueue(serverB.ID, "logtool", "install", base.Add(5*time.Second))
	appendLine := func(seq int64, line string) {
		t.Helper()
		if err := st.AppendToolchainJobLog(ctx, sqlc.AppendToolchainJobLogParams{
			JobID: logJob.ID, Seq: seq, Stream: "stdout", Line: line + "\n",
		}); err != nil {
			t.Fatalf("AppendToolchainJobLog: %v", err)
		}
	}
	appendLine(1, "one")
	appendLine(2, "two")
	appendLine(1, "ignored") // ON CONFLICT DO NOTHING
	if err := st.AppendToolchainJobLogTail(ctx, sqlc.AppendToolchainJobLogTailParams{
		JobID: logJob.ID, Stream: "stderr", Line: "three\n",
	}); err != nil {
		t.Fatalf("AppendToolchainJobLogTail: %v", err)
	}
	rows, err := st.ListToolchainJobLogs(ctx, sqlc.ListToolchainJobLogsParams{JobID: logJob.ID, Seq: 0, Limit: 100})
	if err != nil {
		t.Fatalf("ListToolchainJobLogs: %v", err)
	}
	if len(rows) != 3 || rows[0].Line != "one\n" || rows[1].Line != "two\n" || rows[2].Line != "three\n" {
		t.Fatalf("log rows = %+v, want 3 ordered lines (idempotent append, tail seq 3)", rows)
	}
	after, err := st.ListToolchainJobLogs(ctx, sqlc.ListToolchainJobLogsParams{JobID: logJob.ID, Seq: 1, Limit: 100})
	if err != nil {
		t.Fatalf("ListToolchainJobLogs(after): %v", err)
	}
	if len(after) != 2 || after[0].Seq != 2 {
		t.Fatalf("after=1 rows = %+v, want seq 2 and 3", after)
	}
	// Deleting the job cascades its log rows away.
	if _, err := pool.Exec(ctx, "DELETE FROM toolchain_jobs WHERE id = $1", logJob.ID); err != nil {
		t.Fatalf("delete job: %v", err)
	}
	remaining, err := st.ListToolchainJobLogs(ctx, sqlc.ListToolchainJobLogsParams{JobID: logJob.ID, Seq: 0, Limit: 100})
	if err != nil {
		t.Fatalf("ListToolchainJobLogs(after delete): %v", err)
	}
	if len(remaining) != 0 {
		t.Fatalf("log rows survived the job delete: %+v", remaining)
	}
}
