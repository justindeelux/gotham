package store

import (
	"context"
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// TestBackupRetentionMigrationKeepsExistingSchedules applies the retention
// migration to a populated schema: existing schedules keep an unlimited policy,
// and the new limits are enforced by the database.
func TestBackupRetentionMigrationKeepsExistingSchedules(t *testing.T) {
	ctx, dsn, provider := backupCoreScratch(t)

	if _, err := provider.UpTo(ctx, 80); err != nil {
		t.Fatalf("migrate to 80: %v", err)
	}
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close(context.Background()) })

	ownerID := uuid.New()
	if _, err := conn.Exec(ctx,
		`INSERT INTO users(id,email,password_hash,role) VALUES($1,$2,'x','owner')`,
		ownerID, fmt.Sprintf("retention-%s@example.com", ownerID)); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	serverID, projectID, envID := uuid.New(), uuid.New(), uuid.New()
	if _, err := conn.Exec(ctx,
		`INSERT INTO servers(id,name,ip,ssh_user,status) VALUES($1,'node','127.0.0.1','root','ready')`,
		serverID); err != nil {
		t.Fatalf("insert server: %v", err)
	}
	if _, err := conn.Exec(ctx, `INSERT INTO projects(id,name) VALUES($1,'project')`, projectID); err != nil {
		t.Fatalf("insert project: %v", err)
	}
	if _, err := conn.Exec(ctx,
		`INSERT INTO environments(id,project_id,name) VALUES($1,$2,'production')`,
		envID, projectID); err != nil {
		t.Fatalf("insert environment: %v", err)
	}
	databaseID := uuid.New()
	if _, err := conn.Exec(ctx,
		`INSERT INTO databases(id,user_id,server_id,environment_id,name,engine,status,storage_path)
		 VALUES($1,$2,$3,$4,'orders','postgres','running','gotham-db-orders')`,
		databaseID, ownerID, serverID, envID); err != nil {
		t.Fatalf("insert database: %v", err)
	}
	scheduleID := uuid.New()
	if _, err := conn.Exec(ctx,
		`INSERT INTO backup_schedules(id,database_id,cron,enabled,next_run_at)
		 VALUES($1,$2,'0 2 * * *',true,now())`, scheduleID, databaseID); err != nil {
		t.Fatalf("insert schedule: %v", err)
	}

	if _, err := provider.Up(ctx); err != nil {
		t.Fatalf("migrate to 81: %v", err)
	}

	var keepLast *int
	if err := conn.QueryRow(ctx,
		`SELECT keep_last FROM backup_schedules WHERE id=$1`, scheduleID).Scan(&keepLast); err != nil {
		t.Fatalf("read keep_last: %v", err)
	}
	if keepLast != nil {
		t.Fatalf("keep_last = %d, want NULL for an existing schedule", *keepLast)
	}

	if _, err := conn.Exec(ctx,
		`UPDATE backup_schedules SET keep_last=7 WHERE id=$1`, scheduleID); err != nil {
		t.Fatalf("set keep_last: %v", err)
	}
	for _, invalid := range []int{0, 366} {
		if _, err := conn.Exec(ctx,
			`UPDATE backup_schedules SET keep_last=$2 WHERE id=$1`, scheduleID, invalid); err == nil {
			t.Errorf("keep_last=%d was accepted", invalid)
		}
	}
}
