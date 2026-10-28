package backupwatch

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

func scratchStore(t *testing.T) *store.Store {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	dsn := os.Getenv("GOTHAM_TEST_DSN")
	if dsn == "" {
		t.Skip("GOTHAM_TEST_DSN is required for backup protection integration")
	}
	parsed, err := url.Parse(dsn)
	if err != nil {
		t.Fatalf("parse DSN: %v", err)
	}
	name := fmt.Sprintf("bw%d%d", time.Now().UnixNano(), os.Getpid())
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
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		admin, err := pgx.Connect(cleanupCtx, dsn)
		if err != nil {
			t.Logf("reconnect for drop: %v", err)
			return
		}
		defer admin.Close(cleanupCtx)
		if _, err := admin.Exec(cleanupCtx, `DROP DATABASE IF EXISTS "`+name+`"`); err != nil {
			t.Logf("drop scratch database: %v", err)
		}
	})
	return store.New(pool)
}

func TestProtectionSchedulesMatchDatabaseAndTarget(t *testing.T) {
	ctx := context.Background()
	st := scratchStore(t)
	user, err := st.CreateUser(ctx, fmt.Sprintf("bw-%d@example.com", time.Now().UnixNano()), nil)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	project, err := st.CreateProject(ctx, sqlc.CreateProjectParams{
		ID: pgtype.UUID{Bytes: uuid.New(), Valid: true}, Name: fmt.Sprintf("bw-%d", time.Now().UnixNano()),
	})
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	environment, err := st.CreateEnvironment(ctx, sqlc.CreateEnvironmentParams{
		ID: pgtype.UUID{Bytes: uuid.New(), Valid: true}, ProjectID: project.ID, Name: "production",
	})
	if err != nil {
		t.Fatalf("CreateEnvironment: %v", err)
	}
	databaseID, targetA, targetB := uuid.New(), uuid.New(), uuid.New()
	server, err := st.CreateServer(ctx, sqlc.CreateServerParams{
		Name: fmt.Sprintf("bw-node-%d", time.Now().UnixNano()), Ip: "127.0.0.1", Port: 22, SshUser: "root",
	})
	if err != nil {
		t.Fatalf("CreateServer: %v", err)
	}
	slot := time.Now().UTC().Add(-2 * time.Hour).Truncate(time.Second)
	for _, target := range []struct {
		id   uuid.UUID
		name string
	}{{targetA, "offsite-a"}, {targetB, "offsite-b"}} {
		if _, err := st.DB.Exec(ctx, `INSERT INTO backup_targets (id, user_id, name, kind, bucket)
			VALUES ($1, $2, $3, 's3', 'bucket')`, target.id, user.ID, target.name); err != nil {
			t.Fatalf("insert target: %v", err)
		}
	}
	if _, err := st.DB.Exec(ctx, `INSERT INTO databases (id, user_id, server_id, environment_id, name, engine, status)
		VALUES ($1, $2, $3, $4, 'orders', 'postgres', 'running')`, databaseID, user.ID, server.ID, environment.ID); err != nil {
		t.Fatalf("insert database: %v", err)
	}
	scheduleID := uuid.New()
	if _, err := st.DB.Exec(ctx, `INSERT INTO backup_schedules
		(id, database_id, cron, target_id, enabled, next_run_at, last_slot_at, max_run_seconds)
		VALUES ($1, $2, '0 2 * * *', $3, true, $4, $5, 7200)`,
		scheduleID, databaseID, targetA, slot.Add(24*time.Hour), slot); err != nil {
		t.Fatalf("insert schedule: %v", err)
	}
	// A completed backup to another target must not satisfy the slot, and a
	// manual backup to the schedule target must.
	if _, err := st.DB.Exec(ctx, `INSERT INTO backups
		(id, database_id, type, status, target_id, created_at, finished_at)
		VALUES ($1, $2, 'manual', 'completed', $3, $4, $4)`,
		uuid.New(), databaseID, targetB, slot.Add(5*time.Minute)); err != nil {
		t.Fatalf("insert other-target backup: %v", err)
	}
	if _, err := st.DB.Exec(ctx, `INSERT INTO backups
		(id, database_id, type, status, target_id, created_at, finished_at)
		VALUES ($1, $2, 'manual', 'completed', $3, $4, $4)`,
		uuid.New(), databaseID, targetA, slot.Add(10*time.Minute)); err != nil {
		t.Fatalf("insert qualifying backup: %v", err)
	}
	runningID := uuid.New()
	if _, err := st.DB.Exec(ctx, `INSERT INTO backups
		(id, database_id, type, status, target_id, created_at)
		VALUES ($1, $2, 'scheduled', 'running', $3, $4)`,
		runningID, databaseID, targetA, slot.Add(20*time.Minute)); err != nil {
		t.Fatalf("insert running backup: %v", err)
	}

	source := NewStoreSource(st)
	rows, err := source.ProtectionSchedules(ctx)
	if err != nil {
		t.Fatalf("ProtectionSchedules: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("rows = %d", len(rows))
	}
	row := rows[0]
	if row.LastSuccessAt == nil || !row.LastSuccessAt.Equal(slot.Add(10*time.Minute)) {
		t.Fatalf("last success = %v, want the same-target success", row.LastSuccessAt)
	}
	if row.RunningID != runningID || row.RunningSince == nil || !row.RunningSince.Equal(slot.Add(20*time.Minute)) {
		t.Fatalf("running = %s %v", row.RunningID, row.RunningSince)
	}
	if row.TargetID != targetA || row.ProjectID != uuid.UUID(project.ID.Bytes) {
		t.Fatalf("target/project = %s %s", row.TargetID, row.ProjectID)
	}
	if row.MaxRunSeconds != 7200 {
		t.Fatalf("max run = %d", row.MaxRunSeconds)
	}

	// A deleted database is not evidence: its schedule disappears and the
	// observer closes the incident as a resource removal instead of recovering.
	if _, err := st.DB.Exec(ctx, `UPDATE databases SET deleted_at = now() WHERE id = $1`, databaseID); err != nil {
		t.Fatalf("soft delete database: %v", err)
	}
	rows, err = source.ProtectionSchedules(ctx)
	if err != nil {
		t.Fatalf("ProtectionSchedules after delete: %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("deleted database schedules = %d", len(rows))
	}
}
