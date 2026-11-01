package storages

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/justindeelux/gotham/internal/store"
	"github.com/justindeelux/gotham/internal/store/sqlc"
)

const defaultIntegrationDSN = "postgres://gotham:gotham@localhost:5432/gotham?sslmode=disable"

func storageIntegrationRepo(t *testing.T) (Repository, *store.Store) {
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
	return NewStoreRepository(st), st
}

func storageIntegrationResources(t *testing.T, st *store.Store) (uuid.UUID, uuid.UUID) {
	t.Helper()
	ctx := context.Background()
	ownerID := uuid.New()
	if _, err := st.DB.Exec(ctx,
		`INSERT INTO users(id,email,password_hash) VALUES($1,$2,'x')`,
		ownerID, fmt.Sprintf("storage-%s@example.com", ownerID)); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, _ = st.DB.Exec(cleanupCtx, `DELETE FROM users WHERE id=$1`, ownerID)
	})

	serverID, projectID, envID, databaseID := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	if _, err := st.DB.Exec(ctx,
		`INSERT INTO servers(id,name,ip,ssh_user,status) VALUES($1,$2,'127.0.0.1','root','ready')`,
		serverID, fmt.Sprintf("storage-node-%s", serverID)); err != nil {
		t.Fatalf("insert server: %v", err)
	}
	if _, err := st.DB.Exec(ctx,
		`INSERT INTO projects(id,name) VALUES($1,$2)`, projectID, fmt.Sprintf("storage-project-%s", projectID)); err != nil {
		t.Fatalf("insert project: %v", err)
	}
	if _, err := st.DB.Exec(ctx,
		`INSERT INTO environments(id,project_id,name) VALUES($1,$2,'production')`, envID, projectID); err != nil {
		t.Fatalf("insert environment: %v", err)
	}
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

func seedLocalStorage(t *testing.T, repo Repository, ownerID uuid.UUID) Storage {
	t.Helper()
	now := time.Now().UTC()
	created, err := repo.CreateBackupTargetWithSecrets(context.Background(), Storage{
		ID:        uuid.New(),
		UserID:    ownerID,
		Name:      "Offsite-" + uuid.NewString(),
		Kind:      KindS3,
		Endpoint:  "https://s3.example.test",
		Bucket:    "backups",
		CreatedAt: now,
		UpdatedAt: now,
	}, nil)
	if err != nil {
		t.Fatalf("create storage: %v", err)
	}
	return created
}

func TestDeleteGuardedRefusesAReferencedSchedule(t *testing.T) {
	repo, st := storageIntegrationRepo(t)
	ownerID, databaseID := storageIntegrationResources(t, st)
	ctx := context.Background()
	target := seedLocalStorage(t, repo, ownerID)

	if _, err := st.CreateBackupSchedule(ctx, sqlc.CreateBackupScheduleParams{
		ID:            pgUUID(uuid.New()),
		DatabaseID:    pgUUID(databaseID),
		Cron:          "0 2 * * *",
		TargetID:      pgUUID(target.ID),
		Enabled:       true,
		NextRunAt:     pgtype.Timestamptz{Time: time.Now().UTC(), Valid: true},
		MaxRunSeconds: 7200,
	}); err != nil {
		t.Fatalf("create schedule: %v", err)
	}

	_, schedules, _, deleted, err := repo.DeleteGuarded(ctx, target.ID, ownerID, true)
	if err != nil {
		t.Fatalf("DeleteGuarded: %v", err)
	}
	if deleted {
		t.Fatal("a storage with a schedule was deleted")
	}
	if len(schedules) != 1 {
		t.Fatalf("schedules = %v, want one", schedules)
	}
	if _, err := st.GetBackupTarget(ctx, pgUUID(target.ID)); err != nil {
		t.Fatalf("the storage must survive the refused delete: %v", err)
	}
}

// TestDeleteGuardedWaitsForAConcurrentScheduleWrite closes the check-then-act
// window: the schedule insert holds FOR SHARE on the target, so the delete
// blocks until it commits and then sees the schedule instead of deleting the
// row and nulling the schedule's destination.
func TestDeleteGuardedWaitsForAConcurrentScheduleWrite(t *testing.T) {
	repo, st := storageIntegrationRepo(t)
	ownerID, databaseID := storageIntegrationResources(t, st)
	ctx := context.Background()
	target := seedLocalStorage(t, repo, ownerID)

	tx, err := st.DB.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var locked uuid.UUID
	if err := tx.QueryRow(ctx,
		`SELECT id FROM backup_targets WHERE id=$1 FOR SHARE`, target.ID).Scan(&locked); err != nil {
		t.Fatalf("lock target: %v", err)
	}
	scheduleID := uuid.New()
	if _, err := tx.Exec(ctx,
		`INSERT INTO backup_schedules(id,database_id,cron,target_id,next_run_at)
		 VALUES($1,$2,'0 2 * * *',$3,now())`,
		scheduleID, databaseID, target.ID); err != nil {
		t.Fatalf("insert schedule in tx: %v", err)
	}

	type result struct {
		schedules []uuid.UUID
		deleted   bool
		err       error
	}
	done := make(chan result, 1)
	go func() {
		_, schedules, _, deleted, err := repo.DeleteGuarded(ctx, target.ID, ownerID, true)
		done <- result{schedules: schedules, deleted: deleted, err: err}
	}()

	select {
	case got := <-done:
		t.Fatalf("delete did not wait for the schedule transaction: %+v", got)
	case <-time.After(150 * time.Millisecond):
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit schedule: %v", err)
	}
	got := <-done
	if got.err != nil {
		t.Fatalf("DeleteGuarded: %v", got.err)
	}
	if got.deleted {
		t.Fatal("the storage was deleted although the committed schedule references it")
	}
	if len(got.schedules) != 1 || got.schedules[0] != scheduleID {
		t.Fatalf("schedules = %v, want [%s]", got.schedules, scheduleID)
	}
	if _, err := st.GetBackupTarget(ctx, pgUUID(target.ID)); err != nil {
		t.Fatalf("the storage must survive: %v", err)
	}
}

func TestDeleteGuardedRequiresConfirmationForBackups(t *testing.T) {
	repo, st := storageIntegrationRepo(t)
	ownerID, databaseID := storageIntegrationResources(t, st)
	ctx := context.Background()
	target := seedLocalStorage(t, repo, ownerID)
	if _, err := st.CreateBackup(ctx, sqlc.CreateBackupParams{
		ID:         pgUUID(uuid.New()),
		DatabaseID: pgUUID(databaseID),
		Type:       "manual",
		Status:     "completed",
		TargetID:   pgUUID(target.ID),
	}); err != nil {
		t.Fatalf("create backup: %v", err)
	}

	if _, _, backups, deleted, err := repo.DeleteGuarded(ctx, target.ID, ownerID, false); err != nil || deleted || backups != 1 {
		t.Fatalf("unconfirmed delete = (%d backups, deleted=%v, err=%v), want refusal", backups, deleted, err)
	}
	if _, _, _, deleted, err := repo.DeleteGuarded(ctx, target.ID, ownerID, true); err != nil || !deleted {
		t.Fatalf("confirmed delete = (deleted=%v, err=%v), want success", deleted, err)
	}
	var providerRemoved bool
	if err := st.DB.QueryRow(ctx,
		`SELECT provider_removed FROM backups WHERE target_id IS NULL ORDER BY created_at DESC LIMIT 1`).Scan(&providerRemoved); err != nil {
		t.Fatalf("read run: %v", err)
	}
	if !providerRemoved {
		t.Error("the confirmed delete did not mark the run provider-removed")
	}
}

func TestDeleteGuardedRefusesAnotherOwner(t *testing.T) {
	repo, st := storageIntegrationRepo(t)
	ownerID, _ := storageIntegrationResources(t, st)
	target := seedLocalStorage(t, repo, ownerID)

	if _, _, _, _, err := repo.DeleteGuarded(context.Background(), target.ID, uuid.New(), true); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-owner delete = %v, want ErrNotFound", err)
	}
}
