package backups

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
)

// TestDeletingAResourceQueuesItsArtifacts proves the database trigger captures
// artifacts before the resource (and its backup rows) cascade away, and that the
// sweeper removes both the artifact and the queue entry.
func TestDeletingAResourceQueuesItsArtifacts(t *testing.T) {
	repo, st := backupIntegrationRepo(t)
	_, databaseID := backupIntegrationResources(t, st)
	ctx := context.Background()
	dir := t.TempDir()
	path := filepath.Join(dir, "artifact.gz")
	if err := os.WriteFile(path, []byte("dump"), 0o600); err != nil {
		t.Fatalf("write artifact: %v", err)
	}
	backupID := uuid.New()
	if _, err := st.DB.Exec(ctx,
		`INSERT INTO backups(id,database_id,type,status,size,location)
		 VALUES($1,$2,'manual','completed',4,$3)`,
		backupID, databaseID, locationFilePrefix+path); err != nil {
		t.Fatalf("insert backup: %v", err)
	}

	if _, err := st.DB.Exec(ctx, `DELETE FROM databases WHERE id=$1`, databaseID); err != nil {
		t.Fatalf("delete database: %v", err)
	}

	var rows int
	if err := st.DB.QueryRow(ctx,
		`SELECT count(*) FROM backup_artifact_cleanup WHERE location=$1`,
		locationFilePrefix+path).Scan(&rows); err != nil {
		t.Fatalf("count queued artifacts: %v", err)
	}
	if rows != 1 {
		t.Fatalf("queued artifacts = %d, want 1", rows)
	}

	manager := NewBackupService(BackupConfig{Repository: repo, LocalDir: dir})
	if _, err := manager.SweepCleanup(ctx); err != nil {
		t.Fatalf("SweepCleanup: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("artifact still on disk: %v", err)
	}
	if err := st.DB.QueryRow(ctx,
		`SELECT count(*) FROM backup_artifact_cleanup WHERE location=$1`,
		locationFilePrefix+path).Scan(&rows); err != nil {
		t.Fatalf("count queue: %v", err)
	}
	if rows != 0 {
		t.Fatalf("queue rows = %d, want none for this artifact", rows)
	}
}
