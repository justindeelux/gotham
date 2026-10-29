package store

import (
	"context"
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// TestBackupChecksumMigrationKeepsExistingRuns applies the integrity migration
// to a populated schema: a run written before checksums existed keeps NULL
// columns (so it still restores as "not verified"), and the new columns hold a
// value for a new run.
func TestBackupChecksumMigrationKeepsExistingRuns(t *testing.T) {
	ctx, dsn, provider := backupCoreScratch(t)

	if _, err := provider.UpTo(ctx, 82); err != nil {
		t.Fatalf("migrate to 82: %v", err)
	}
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close(context.Background()) })

	ownerID := uuid.New()
	if _, err := conn.Exec(ctx,
		`INSERT INTO users(id,email,password_hash,role) VALUES($1,$2,'x','owner')`,
		ownerID, fmt.Sprintf("checksum-%s@example.com", ownerID)); err != nil {
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
	backupID := uuid.New()
	if _, err := conn.Exec(ctx,
		`INSERT INTO backups(id,database_id,type,status,size,location)
		 VALUES($1,$2,'manual','completed',42,'file:///tmp/old.dump.gz')`,
		backupID, databaseID); err != nil {
		t.Fatalf("insert backup: %v", err)
	}

	if _, err := provider.Up(ctx); err != nil {
		t.Fatalf("migrate to 83: %v", err)
	}

	var checksum, algo *string
	if err := conn.QueryRow(ctx,
		`SELECT checksum_sha256, checksum_algo FROM backups WHERE id=$1`, backupID).Scan(&checksum, &algo); err != nil {
		t.Fatalf("read checksum: %v", err)
	}
	if checksum != nil || algo != nil {
		t.Fatalf("columns = %v/%v, want NULL for an existing run", checksum, algo)
	}

	if _, err := conn.Exec(ctx,
		`UPDATE backups SET checksum_sha256=$2, checksum_algo='sha256' WHERE id=$1`,
		backupID, "abc123"); err != nil {
		t.Fatalf("set checksum: %v", err)
	}
	if err := conn.QueryRow(ctx,
		`SELECT checksum_sha256, checksum_algo FROM backups WHERE id=$1`, backupID).Scan(&checksum, &algo); err != nil {
		t.Fatalf("read checksum: %v", err)
	}
	if checksum == nil || *checksum != "abc123" || algo == nil || *algo != "sha256" {
		t.Fatalf("columns = %v/%v, want the stored checksum", checksum, algo)
	}
}
