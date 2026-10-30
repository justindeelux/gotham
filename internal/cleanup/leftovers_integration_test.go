package cleanup

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/justindeelux/gotham/internal/store"
)

func TestHistoryRowsKeepNewestAndRecentAndDeleteInBatches(t *testing.T) {
	ctx, dsn := cleanupScratch(t)
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("open pool: %v", err)
	}
	t.Cleanup(pool.Close)
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close(context.Background()) })
	st := store.New(pool)

	userID, serverID := uuid.New(), uuid.New()
	projectID, envID := uuid.New(), uuid.New()
	app1, app2 := uuid.New(), uuid.New()
	now := time.Now().UTC()
	old := func(days int) time.Time { return now.Add(-time.Duration(days) * 24 * time.Hour) }

	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := conn.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("%s: %v", sql, err)
		}
	}
	exec(`INSERT INTO users(id,email,password_hash,role) VALUES($1,$2,'x','owner')`,
		userID, "cleanup-history@example.com")
	exec(`INSERT INTO servers(id,name,ip,ssh_user,status) VALUES($1,'node','127.0.0.1','root','ready')`, serverID)
	exec(`INSERT INTO projects(id,name) VALUES($1,'project')`, projectID)
	exec(`INSERT INTO environments(id,project_id,name) VALUES($1,$2,'production')`, envID, projectID)
	for _, app := range []uuid.UUID{app1, app2} {
		exec(`INSERT INTO applications(id,user_id,server_id,environment_id,name) VALUES($1,$2,$3,$4,$5)`,
			app, userID, serverID, envID, "app-"+app.String()[:8])
	}
	deployment := func(app uuid.UUID, state, log string, at time.Time) {
		exec(`INSERT INTO deployments(application_id,state,build_log,created_at) VALUES($1,$2,$3,$4)`,
			app, state, log, at)
	}
	// app1: three old failures (targets), then the newest three rows and the
	// current running deployment keep their logs.
	deployment(app1, "failed", "old-a", old(100))
	deployment(app1, "failed", "old-b", old(90))
	deployment(app1, "failed", "old-c", old(80))
	deployment(app1, "running", "current", old(70))
	deployment(app1, "failed", "kept-4", old(60))
	deployment(app1, "failed", "kept-5", old(50))
	deployment(app1, "failed", "recent", old(1))
	// app2 has only one deployment, so it is the newest three for that app.
	deployment(app2, "failed", "only", old(100))

	runningJob, succeededJob, recentJob := uuid.New(), uuid.New(), uuid.New()
	exec(`INSERT INTO toolchain_jobs(id,server_id,tool_id,action,status,created_at)
	      VALUES($1,$2,'node','install','running',$3)`, runningJob, serverID, old(100))
	exec(`INSERT INTO toolchain_jobs(id,server_id,tool_id,action,status,created_at)
	      VALUES($1,$2,'go','install','succeeded',$3)`, succeededJob, serverID, old(100))
	exec(`INSERT INTO toolchain_jobs(id,server_id,tool_id,action,status,created_at)
	      VALUES($1,$2,'git','install','succeeded',$3)`, recentJob, serverID, old(1))
	exec(`INSERT INTO toolchain_job_logs(job_id,seq,line,created_at) VALUES($1,1,'live',$2)`, runningJob, old(100))
	exec(`INSERT INTO toolchain_job_logs(job_id,seq,line,created_at) VALUES($1,1,'one',$2)`, succeededJob, old(100))
	exec(`INSERT INTO toolchain_job_logs(job_id,seq,line,created_at) VALUES($1,2,'two',$2)`, succeededJob, old(100))
	exec(`INSERT INTO toolchain_job_logs(job_id,seq,line,created_at) VALUES($1,1,'recent',$2)`, recentJob, old(1))

	receipt := func(key string, at time.Time) {
		exec(`INSERT INTO request_receipts(principal_id,operation,target_id,request_key,fingerprint,resource_id,related_id,accepted_at)
		      VALUES($1,'deploy',$2,$3,'fp',$4,$5,$6)`,
			uuid.New(), uuid.New(), key, uuid.New(), uuid.New(), at)
	}
	receipt("old-1", old(100))
	receipt("old-2", old(90))
	receipt("recent", old(1))

	webhook := func(delivery string, at time.Time) {
		exec(`INSERT INTO webhook_events(application_id,provider,delivery_id,received_at)
		      VALUES($1,'github',$2,$3)`, app1, delivery, at)
	}
	webhook("old", old(100))
	webhook("recent", old(1))

	cutoff := old(30)
	stats, err := st.HistoryStats(ctx, cutoff)
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]store.HistoryStat{}
	for _, stat := range stats {
		byName[stat.Name] = stat
	}
	wantRows := map[string]int64{
		store.HistoryDeploymentLogs:   3,
		store.HistoryToolchainJobLogs: 2,
		store.HistoryRequestReceipts:  2,
		store.HistoryWebhookEvents:    1,
	}
	for name, want := range wantRows {
		got := byName[name]
		if got.Rows != want {
			t.Errorf("%s rows = %d; want %d", name, got.Rows, want)
		}
		if got.Bytes <= 0 {
			t.Errorf("%s bytes = %d; want a positive estimate", name, got.Bytes)
		}
	}

	// Two batches of two rows, then the final empty read.
	for batch, want := range []int64{2, 1, 0} {
		deleted, err := st.DeleteHistoryBatch(ctx, store.HistoryDeploymentLogs, cutoff, 2)
		if err != nil {
			t.Fatal(err)
		}
		if deleted != want {
			t.Fatalf("batch %d deleted %d rows; want %d", batch+1, deleted, want)
		}
	}
	var remaining int
	if err := conn.QueryRow(ctx,
		`SELECT count(*) FROM deployments WHERE build_log <> ''`).Scan(&remaining); err != nil {
		t.Fatal(err)
	}
	if remaining != 5 {
		t.Fatalf("deployments with logs = %d; want the newest three and the running one", remaining)
	}

	for _, name := range []string{
		store.HistoryToolchainJobLogs, store.HistoryRequestReceipts, store.HistoryWebhookEvents,
	} {
		for {
			deleted, err := st.DeleteHistoryBatch(ctx, name, cutoff, store.HistoryDeleteBatch)
			if err != nil {
				t.Fatalf("%s: %v", name, err)
			}
			if deleted == 0 {
				break
			}
		}
	}
	stats, err = st.HistoryStats(ctx, cutoff)
	if err != nil {
		t.Fatal(err)
	}
	for _, stat := range stats {
		if stat.Rows != 0 {
			t.Fatalf("%s still has %d rows; want only the kept rows", stat.Name, stat.Rows)
		}
	}
}
