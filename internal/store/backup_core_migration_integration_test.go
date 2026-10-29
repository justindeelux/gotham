package store

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/pressly/goose/v3"
)

func backupCoreScratch(t *testing.T) (context.Context, string, *goose.Provider) {
	t.Helper()

	base := os.Getenv("GOTHAM_TEST_DSN")
	explicit := base != ""
	if base == "" {
		base = "postgres://gotham:gotham@localhost:5432/gotham?sslmode=disable"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	t.Cleanup(cancel)

	admin, err := pgx.Connect(ctx, base)
	if err != nil {
		if explicit {
			t.Fatalf("GOTHAM_TEST_DSN is set but Postgres is unavailable: %v", err)
		}
		t.Skipf("Postgres not available: %v", err)
	}
	name := fmt.Sprintf("bkp%d", time.Now().UnixNano())
	if _, err := admin.Exec(ctx, `CREATE DATABASE "`+name+`"`); err != nil {
		_ = admin.Close(ctx)
		t.Skipf("cannot create a disposable database (needs CREATEDB): %v", err)
	}
	_ = admin.Close(ctx)
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		conn, err := pgx.Connect(cleanupCtx, base)
		if err != nil {
			t.Logf("reconnect for scratch drop: %v", err)
			return
		}
		defer conn.Close(cleanupCtx)
		if _, err := conn.Exec(cleanupCtx, `DROP DATABASE IF EXISTS "`+name+`" WITH (FORCE)`); err != nil {
			t.Logf("drop scratch database: %v", err)
		}
	})

	parsed, err := url.Parse(base)
	if err != nil {
		t.Fatalf("parse test DSN: %v", err)
	}
	parsed.Path = "/" + name
	dsn := parsed.String()

	sqldb, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(func() { _ = sqldb.Close() })
	provider, err := goose.NewProvider(goose.DialectPostgres, sqldb, Migrations())
	if err != nil {
		t.Fatalf("init migrations: %v", err)
	}
	return ctx, dsn, provider
}

// TestBackupCoreMigrationKeepsExistingRuns applies the resource-agnostic
// migration to a populated schema: every database backup, schedule, restore and
// target keeps working, and runs that never chose a destination are attached to
// the new built-in Local disk provider.
func TestBackupCoreMigrationKeepsExistingRuns(t *testing.T) {
	ctx, dsn, provider := backupCoreScratch(t)

	if _, err := provider.UpTo(ctx, 78); err != nil {
		t.Fatalf("migrate to 78: %v", err)
	}
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close(context.Background()) })

	ownerID := uuid.New()
	if _, err := conn.Exec(ctx,
		`INSERT INTO users(id,email,password_hash,role) VALUES($1,$2,'x','owner')`,
		ownerID, fmt.Sprintf("bkp-%s@example.com", ownerID)); err != nil {
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
	if _, err := conn.Exec(ctx, `INSERT INTO databases(id,user_id,server_id,environment_id,name,engine,status,storage_path)
		VALUES($1,$2,$3,$4,'orders','postgres','running','gotham-db-orders')`,
		databaseID, ownerID, serverID, envID); err != nil {
		t.Fatalf("insert database: %v", err)
	}

	s3TargetID := uuid.New()
	if _, err := conn.Exec(ctx, `INSERT INTO backup_targets(id,user_id,name,kind,endpoint,bucket)
		VALUES($1,$2,'offsite','s3','https://s3.example.com','b')`, s3TargetID, ownerID); err != nil {
		t.Fatalf("insert target: %v", err)
	}
	s3BackupID, localBackupID := uuid.New(), uuid.New()
	if _, err := conn.Exec(ctx, `INSERT INTO backups(id,database_id,type,status,size,location,target_id)
		VALUES($1,$2,'manual','completed',10,'s3://b/key.gz',$3)`, s3BackupID, databaseID, s3TargetID); err != nil {
		t.Fatalf("insert s3 backup: %v", err)
	}
	if _, err := conn.Exec(ctx, `INSERT INTO backups(id,database_id,type,status,size,location)
		VALUES($1,$2,'manual','completed',20,'file:///var/lib/gotham/backups/key.gz')`, localBackupID, databaseID); err != nil {
		t.Fatalf("insert local backup: %v", err)
	}
	scheduleID := uuid.New()
	if _, err := conn.Exec(ctx, `INSERT INTO backup_schedules(id,database_id,cron,enabled,next_run_at)
		VALUES($1,$2,'0 2 * * *',true,now())`, scheduleID, databaseID); err != nil {
		t.Fatalf("insert schedule: %v", err)
	}
	restoreID := uuid.New()
	if _, err := conn.Exec(ctx, `INSERT INTO restores(id,database_id,backup_id,status)
		VALUES($1,$2,$3,'completed')`, restoreID, databaseID, s3BackupID); err != nil {
		t.Fatalf("insert restore: %v", err)
	}

	if _, err := provider.Up(ctx); err != nil {
		t.Fatalf("migrate to 80: %v", err)
	}

	var databaseRef, applicationRef, serviceRef *uuid.UUID
	if err := conn.QueryRow(ctx, `SELECT database_id, application_id, service_id FROM backups WHERE id=$1`, s3BackupID).
		Scan(&databaseRef, &applicationRef, &serviceRef); err != nil {
		t.Fatalf("read migrated backup: %v", err)
	}
	if databaseRef == nil || *databaseRef != databaseID || applicationRef != nil || serviceRef != nil {
		t.Fatalf("migrated backup = %v/%v/%v, want the original database", databaseRef, applicationRef, serviceRef)
	}

	var builtinID uuid.UUID
	var builtin bool
	if err := conn.QueryRow(ctx,
		`SELECT id, builtin FROM backup_targets WHERE user_id=$1 AND kind='local'`, ownerID).
		Scan(&builtinID, &builtin); err != nil {
		t.Fatalf("built-in provider: %v", err)
	}
	if !builtin {
		t.Error("the migrated provider is not marked builtin")
	}

	var s3Target *uuid.UUID
	if err := conn.QueryRow(ctx, `SELECT target_id FROM backups WHERE id=$1`, s3BackupID).Scan(&s3Target); err != nil {
		t.Fatalf("read s3 target: %v", err)
	}
	if s3Target == nil || *s3Target != s3TargetID {
		t.Errorf("s3 backup target = %v, want the untouched %s", s3Target, s3TargetID)
	}

	var localTarget uuid.UUID
	if err := conn.QueryRow(ctx, `SELECT target_id FROM backups WHERE id=$1`, localBackupID).Scan(&localTarget); err != nil {
		t.Fatalf("read local target: %v", err)
	}
	if localTarget != builtinID {
		t.Errorf("local backup target = %s, want the built-in provider %s", localTarget, builtinID)
	}

	var scheduleTarget uuid.UUID
	if err := conn.QueryRow(ctx, `SELECT target_id FROM backup_schedules WHERE id=$1`, scheduleID).Scan(&scheduleTarget); err != nil {
		t.Fatalf("read schedule target: %v", err)
	}
	if scheduleTarget != builtinID {
		t.Errorf("schedule target = %s, want the built-in provider %s", scheduleTarget, builtinID)
	}

	var restoredBackup uuid.UUID
	if err := conn.QueryRow(ctx, `SELECT backup_id FROM restores WHERE id=$1`, restoreID).Scan(&restoredBackup); err != nil {
		t.Fatalf("read restore: %v", err)
	}
	if restoredBackup != s3BackupID {
		t.Errorf("restore backup = %s, want %s", restoredBackup, s3BackupID)
	}

	// The CHECK keeps a row from belonging to no resource or to two.
	if _, err := conn.Exec(ctx,
		`INSERT INTO backups(id,type,status) VALUES($1,'manual','completed')`, uuid.New()); err == nil {
		t.Error("a backup without a resource was accepted")
	}
	appID := uuid.New()
	if _, err := conn.Exec(ctx,
		`INSERT INTO applications(id,user_id,server_id,environment_id,name) VALUES($1,$2,$3,$4,'app')`,
		appID, ownerID, serverID, envID); err != nil {
		t.Fatalf("insert application: %v", err)
	}
	if _, err := conn.Exec(ctx,
		`INSERT INTO backups(id,database_id,application_id,type,status) VALUES($1,$2,$3,'manual','completed')`,
		uuid.New(), databaseID, appID); err == nil {
		t.Error("a backup with two resources was accepted")
	}
	if _, err := conn.Exec(ctx,
		`INSERT INTO backups(id,application_id,type,status) VALUES($1,$2,'manual','completed')`,
		uuid.New(), appID); err != nil {
		t.Errorf("a backup of an application was rejected: %v", err)
	}
}
