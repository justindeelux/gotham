package backups

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/store"
)

const defaultIntegrationDSN = "postgres://gotham:gotham@localhost:5432/gotham?sslmode=disable"

func backupIntegrationRepo(t *testing.T) (*storeBackupRepository, *store.Store) {
	t.Helper()

	dsn := os.Getenv("GOTHAM_TEST_DSN")
	explicit := dsn != ""
	if !explicit {
		dsn = defaultIntegrationDSN
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := store.ProbeOnce(ctx, dsn); err != nil {
		if explicit {
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
	return newStoreBackupRepository(st), st
}

// backupIntegrationResources seeds an owner plus one database in a fresh
// project and removes them when the test ends.
func backupIntegrationResources(t *testing.T, st *store.Store) (uuid.UUID, uuid.UUID) {
	t.Helper()
	ctx := context.Background()
	ownerID := uuid.New()
	if _, err := st.DB.Exec(ctx,
		`INSERT INTO users(id,email,password_hash) VALUES($1,$2,'x')`,
		ownerID, fmt.Sprintf("backup-%s@example.com", ownerID)); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, err := st.DB.Exec(cleanupCtx, `DELETE FROM users WHERE id=$1`, ownerID); err != nil {
			t.Logf("cleanup user: %v", err)
		}
	})

	serverID, projectID, envID := uuid.New(), uuid.New(), uuid.New()
	if _, err := st.DB.Exec(ctx,
		`INSERT INTO servers(id,name,ip,ssh_user,status) VALUES($1,$2,'127.0.0.1','root','ready')`,
		serverID, fmt.Sprintf("backup-node-%s", serverID)); err != nil {
		t.Fatalf("insert server: %v", err)
	}
	if _, err := st.DB.Exec(ctx,
		`INSERT INTO projects(id,name) VALUES($1,$2)`, projectID, fmt.Sprintf("backup-project-%s", projectID)); err != nil {
		t.Fatalf("insert project: %v", err)
	}
	if _, err := st.DB.Exec(ctx,
		`INSERT INTO environments(id,project_id,name) VALUES($1,$2,'production')`, envID, projectID); err != nil {
		t.Fatalf("insert environment: %v", err)
	}
	databaseID := uuid.New()
	if _, err := st.DB.Exec(ctx,
		`INSERT INTO databases(id,user_id,server_id,environment_id,name,engine,status,storage_path)
		 VALUES($1,$2,$3,$4,$5,'postgres','running',$6)`,
		databaseID, ownerID, serverID, envID, fmt.Sprintf("db-%s", databaseID), "gotham-db-"+databaseID.String()); err != nil {
		t.Fatalf("insert database: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, _ = st.DB.Exec(cleanupCtx, `DELETE FROM projects WHERE id=$1`, projectID)
		_, _ = st.DB.Exec(cleanupCtx, `DELETE FROM databases WHERE id=$1`, databaseID)
		_, _ = st.DB.Exec(cleanupCtx, `DELETE FROM servers WHERE id=$1`, serverID)
	})
	return ownerID, databaseID
}

func TestBackupRepositoryRoundTrip(t *testing.T) {
	repo, st := backupIntegrationRepo(t)
	ownerID, databaseID := backupIntegrationResources(t, st)
	ctx := context.Background()
	ref := databaseRef(databaseID)
	now := time.Now().UTC().Truncate(time.Microsecond)

	backup, err := repo.CreateBackup(ctx, Backup{
		ID:          uuid.New(),
		ResourceRef: ref,
		Type:        BackupManual,
		Status:      BackupRunning,
		CreatedAt:   now,
	})
	if err != nil {
		t.Fatalf("CreateBackup: %v", err)
	}
	if backup.ResourceRef != ref || backup.Status != BackupRunning {
		t.Errorf("backup = %+v", backup)
	}

	listed, err := repo.ListBackupsByResource(ctx, ref, "", maxBackupListLimit)
	if err != nil || len(listed) != 1 {
		t.Fatalf("ListBackupsByResource = %d rows (%v), want 1", len(listed), err)
	}
	other, err := repo.ListBackupsByResource(ctx, databaseRef(uuid.New()), "", maxBackupListLimit)
	if err != nil || len(other) != 0 {
		t.Fatalf("another resource listed %d rows (%v), want 0", len(other), err)
	}

	finished, err := repo.FinishBackup(ctx, Backup{
		ID:          backup.ID,
		Status:      BackupCompleted,
		Size:        1024,
		Location:    locationFilePrefix + "/tmp/backup.gz",
		ContainerID: "gotham-backup-abcd1234",
		FinishedAt:  time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("FinishBackup: %v", err)
	}
	if finished.Status != BackupCompleted || finished.Size != 1024 || finished.FinishedAt.IsZero() {
		t.Errorf("finished = %+v", finished)
	}

	if _, err := repo.GetBackup(ctx, uuid.New()); err != ErrNotFound {
		t.Errorf("GetBackup(missing) err = %v, want ErrNotFound", err)
	}

	restore, err := repo.CreateRestore(ctx, Restore{
		ID:          uuid.New(),
		ResourceRef: ref,
		BackupID:    backup.ID,
		Status:      RestoreRunning,
		CreatedAt:   now,
	})
	if err != nil {
		t.Fatalf("CreateRestore: %v", err)
	}
	running, err := repo.ListRunningRestores(ctx)
	if err != nil {
		t.Fatalf("ListRunningRestores: %v", err)
	}
	found := false
	for _, item := range running {
		if item.ID == restore.ID {
			found = true
		}
	}
	if !found {
		t.Errorf("the running restore is not listed: %+v", running)
	}
	finishedRestore, err := repo.FinishRestore(ctx, Restore{
		ID:         restore.ID,
		Status:     RestoreFailed,
		Error:      "interrupted",
		FinishedAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("FinishRestore: %v", err)
	}
	if finishedRestore.Status != RestoreFailed || finishedRestore.Error != "interrupted" {
		t.Errorf("finished restore = %+v", finishedRestore)
	}

	schedule, err := repo.CreateBackupSchedule(ctx, BackupSchedule{
		ID:            uuid.New(),
		ResourceRef:   ref,
		Cron:          "0 2 * * *",
		Enabled:       true,
		NextRunAt:     time.Now().UTC().Add(-time.Minute),
		MaxRunSeconds: DefaultMaxBackupRunSeconds,
		CreatedAt:     now,
	})
	if err != nil {
		t.Fatalf("CreateBackupSchedule: %v", err)
	}
	due, err := repo.ListDueBackupSchedules(ctx, time.Now().UTC())
	if err != nil {
		t.Fatalf("ListDueBackupSchedules: %v", err)
	}
	foundDue := false
	for _, item := range due {
		if item.ID == schedule.ID {
			foundDue = true
		}
	}
	if !foundDue {
		t.Errorf("due schedule is not listed: %+v", due)
	}
	schedules, err := repo.ListBackupSchedulesByResource(ctx, ref)
	if err != nil || len(schedules) != 1 {
		t.Fatalf("schedules = %d (%v), want 1", len(schedules), err)
	}

	target, err := repo.EnsureBuiltinTarget(ctx, ownerID)
	if err != nil {
		t.Fatalf("EnsureBuiltinTarget: %v", err)
	}
	if !target.Builtin || target.Kind != TargetLocal {
		t.Errorf("builtin = %+v", target)
	}
	if again, err := repo.GetBuiltinTarget(ctx, ownerID); err != nil || again.ID != target.ID {
		t.Errorf("GetBuiltinTarget = %+v, %v", again, err)
	}

	// A deleted resource cascades: no orphaned run rows stay behind.
	if _, err := st.DB.Exec(ctx, `DELETE FROM databases WHERE id=$1`, databaseID); err != nil {
		t.Fatalf("delete database: %v", err)
	}
	if _, err := repo.GetBackup(ctx, backup.ID); err != ErrNotFound {
		t.Errorf("backup survived its resource: %v", err)
	}
}
