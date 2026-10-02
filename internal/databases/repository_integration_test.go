package databases

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
	"github.com/justindeelux/gotham/internal/teams"
)

// defaultIntegrationDSN points at the dev database from deploy/compose.dev.yml.
// Override with GOTHAM_TEST_DSN (or GOTHAM_TEST_DSN=postgres://nope to force a
// skip), matching internal/store's integration test.
const defaultIntegrationDSN = "postgres://gotham:gotham@localhost:5432/gotham?sslmode=disable"

// integrationEnv runs the embedded migrations and returns a repository backed
// by a real PostgreSQL, skipping the test when no database is reachable so CI
// stays green without one. The returned cleanup deletes everything the test
// seeded.
func integrationEnv(t *testing.T) (*storeRepository, *store.Store) {
	t.Helper()

	dsn := os.Getenv("GOTHAM_TEST_DSN")
	if dsn == "" {
		dsn = defaultIntegrationDSN
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := store.Migrate(ctx, dsn, store.MigrateUp); err != nil {
		t.Skipf("Postgres not available: %v", err)
	}
	pool, err := store.Open(ctx, dsn)
	if err != nil {
		t.Skipf("Postgres not available: %v", err)
	}
	// Registered first so LIFO order closes the pool after the row cleanups.
	t.Cleanup(pool.Close)

	st := store.New(pool)
	return newStoreRepository(st), st
}

// seedUserAndServer registers the owner and the node a test database needs.
func seedUserAndServer(t *testing.T, st *store.Store) (uuid.UUID, uuid.UUID) {
	t.Helper()
	ctx := context.Background()

	user, err := st.CreateUser(ctx, fmt.Sprintf("p5-%d@example.com", time.Now().UnixNano()), nil)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, err := poolExec(st, cleanupCtx, "DELETE FROM users WHERE id = $1", user.ID); err != nil {
			t.Logf("cleanup user: %v", err)
		}
	})

	server, err := st.CreateServer(ctx, sqlc.CreateServerParams{
		Name:     fmt.Sprintf("p5-node-%d", time.Now().UnixNano()),
		Ip:       "127.0.0.1",
		Port:     22,
		SshUser:  "root",
		SshKeyID: pgtype.UUID{},
	})
	if err != nil {
		t.Fatalf("CreateServer: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := st.DeleteServer(cleanupCtx, server.ID); err != nil {
			t.Logf("cleanup server: %v", err)
		}
	})

	return uuidFromPG(user.ID), uuidFromPG(server.ID)
}

// poolExec hides the pool type behind the Store so the test cleanup does not
// need to thread the pool around.
func poolExec(st *store.Store, ctx context.Context, query string, args ...any) (int64, error) {
	tag, err := st.DB.Exec(ctx, query, args...)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// TestRepositoryRoundTrip exercises the SQL behind the repository: the
// migration, the ownership reads, the unique-name index, the sealed secrets
// and the soft delete that keeps the row and its volume.
func TestRepositoryRoundTrip(t *testing.T) {
	repo, st := integrationEnv(t)
	ctx := context.Background()
	ownerID, serverID := seedUserAndServer(t, st)
	otherUserID, _ := seedUserAndServer(t, st)

	now := time.Now().UTC()
	created, err := repo.CreateDatabase(ctx, Database{
		ID:          uuid.New(),
		UserID:      ownerID,
		ServerID:    serverID,
		Name:        "orders",
		Engine:      EnginePostgres,
		Version:     "16-alpine",
		Status:      StatusCreating,
		PublicPort:  5433,
		StoragePath: "gotham-db-" + uuid.New().String(),
		CreatedAt:   now,
		UpdatedAt:   now,
	})
	if err != nil {
		t.Fatalf("CreateDatabase: %v", err)
	}

	fetched, err := repo.GetDatabase(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetDatabase: %v", err)
	}
	if fetched.Name != "orders" || fetched.Engine != EnginePostgres ||
		fetched.Status != StatusCreating || fetched.PublicPort != 5433 {
		t.Errorf("fetched = %+v", fetched)
	}
	if fetched.StoragePath != created.StoragePath {
		t.Errorf("storage_path = %q, want %q", fetched.StoragePath, created.StoragePath)
	}
	if fetched.UserID != ownerID || fetched.ServerID != serverID {
		t.Errorf("ownership = %s/%s, want %s/%s", fetched.UserID, fetched.ServerID, ownerID, serverID)
	}

	// Same name, same user → conflict; same name, other user → allowed.
	duplicate := created
	duplicate.ID = uuid.New()
	if _, err := repo.CreateDatabase(ctx, duplicate); !errors.Is(err, ErrConflict) {
		t.Errorf("duplicate CreateDatabase error = %v, want ErrConflict", err)
	}
	duplicate.UserID = otherUserID
	if _, err := repo.CreateDatabase(ctx, duplicate); err != nil {
		t.Errorf("another user's database with the same name: %v", err)
	}

	// Sealed credentials round-trip through the SQL table.
	secrets, err := sealCredentials(testSecret, created.ID, Credentials{
		Username: "orders", Password: "pw-value", Database: "orders",
	})
	if err != nil {
		t.Fatalf("sealCredentials: %v", err)
	}
	for _, secret := range secrets {
		if _, err := repo.CreateSecret(ctx, secret); err != nil {
			t.Fatalf("CreateSecret: %v", err)
		}
	}
	stored, err := repo.ListSecrets(ctx, created.ID)
	if err != nil {
		t.Fatalf("ListSecrets: %v", err)
	}
	if len(stored) != len(secrets) {
		t.Fatalf("stored %d secrets, want %d", len(stored), len(secrets))
	}
	opened, err := openCredentials(testSecret, stored)
	if err != nil {
		t.Fatalf("openCredentials: %v", err)
	}
	if opened.Password != "pw-value" || opened.Username != "orders" {
		t.Errorf("opened = %+v", opened)
	}

	// Rename keeps every other column; a colliding rename inside one user
	// conflicts, while another user may take the same name (the index is
	// partial on user_id).
	renamed, err := repo.UpdateDatabaseName(ctx, created.ID, "warehouse")
	if err != nil {
		t.Fatalf("UpdateDatabaseName: %v", err)
	}
	if renamed.Name != "warehouse" || renamed.Status != StatusCreating ||
		renamed.StoragePath != created.StoragePath {
		t.Errorf("renamed = %+v", renamed)
	}

	second := renamed
	second.ID = uuid.New()
	second.Name = "billing"
	if _, err := repo.CreateDatabase(ctx, second); err != nil {
		t.Fatalf("CreateDatabase (second row): %v", err)
	}
	if _, err := repo.UpdateDatabaseName(ctx, second.ID, "warehouse"); !errors.Is(err, ErrConflict) {
		t.Errorf("colliding rename error = %v, want ErrConflict", err)
	}

	if _, err := repo.UpdateDatabaseName(ctx, duplicate.ID, "warehouse"); err != nil {
		t.Errorf("another user may reuse a name: %v", err)
	}

	// Status vocabulary is enforced by the CHECK constraint.
	if _, err := repo.UpdateDatabaseStatus(ctx, renamed.ID, "not-a-status"); err == nil {
		t.Error("an unknown status must be rejected by the CHECK constraint")
	}

	// The column-scoped writes do not clobber each other: a status change
	// keeps the name and container id, and a rename keeps the status.
	tagged, err := repo.UpdateDatabaseContainer(ctx, second.ID, "container-42")
	if err != nil {
		t.Fatalf("UpdateDatabaseContainer: %v", err)
	}
	if tagged.ContainerID != "container-42" || tagged.Name != "billing" || tagged.Status != StatusCreating {
		t.Errorf("container write changed more than container_id: %+v", tagged)
	}
	stopped, err := repo.UpdateDatabaseStatus(ctx, second.ID, StatusStopped)
	if err != nil {
		t.Fatalf("UpdateDatabaseStatus: %v", err)
	}
	if stopped.Status != StatusStopped || stopped.Name != "billing" || stopped.ContainerID != "container-42" {
		t.Errorf("status write changed more than status: %+v", stopped)
	}
	renamedAgain, err := repo.UpdateDatabaseName(ctx, second.ID, "billing-2")
	if err != nil {
		t.Fatalf("UpdateDatabaseName: %v", err)
	}
	if renamedAgain.Name != "billing-2" || renamedAgain.Status != StatusStopped ||
		renamedAgain.ContainerID != "container-42" {
		t.Errorf("rename changed more than name: %+v", renamedAgain)
	}

	// The port pre-check sees the live row and ignores soft-deleted ones.
	if inUse, err := repo.PublicPortInUse(ctx, serverID, 5433); err != nil || !inUse {
		t.Errorf("PublicPortInUse(live) = %v, %v, want true", inUse, err)
	}
	if inUse, err := repo.PublicPortInUse(ctx, serverID, 5499); err != nil || inUse {
		t.Errorf("PublicPortInUse(free) = %v, %v, want false", inUse, err)
	}

	// The owner sees both live databases; a stranger sees only their own.
	list, err := repo.ListDatabases(ctx, teams.Scope{UserID: ownerID})
	if err != nil {
		t.Fatalf("ListDatabasesByUser: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("owner list = %+v, want the two live rows", list)
	}
	otherList, err := repo.ListDatabases(ctx, teams.Scope{UserID: otherUserID})
	if err != nil {
		t.Fatalf("ListDatabasesByUser (other): %v", err)
	}
	if len(otherList) != 1 {
		t.Errorf("other user's list = %+v, want their one row", otherList)
	}

	// A soft-deleted row no longer reserves its port: the SQL fence must
	// ignore it in PublicPortInUse (adding this row after the list check
	// keeps the earlier counts stable).
	portRow, err := repo.CreateDatabase(ctx, Database{
		ID:          uuid.New(),
		UserID:      ownerID,
		ServerID:    serverID,
		Name:        "port-check",
		Engine:      EnginePostgres,
		Status:      StatusCreating,
		PublicPort:  5555,
		StoragePath: "gotham-db-" + uuid.New().String(),
		CreatedAt:   now,
		UpdatedAt:   now,
	})
	if err != nil {
		t.Fatalf("CreateDatabase (port-check): %v", err)
	}
	if inUse, err := repo.PublicPortInUse(ctx, serverID, 5555); err != nil || !inUse {
		t.Errorf("PublicPortInUse(port-check live) = %v, %v, want true", inUse, err)
	}
	if _, err := repo.SoftDeleteDatabase(ctx, portRow.ID); err != nil {
		t.Fatalf("SoftDeleteDatabase (port-check): %v", err)
	}
	if inUse, err := repo.PublicPortInUse(ctx, serverID, 5555); err != nil || inUse {
		t.Errorf("PublicPortInUse(port-check deleted) = %v, %v, want false", inUse, err)
	}

	// Soft delete hides the row and frees the name for reuse.
	deleted, err := repo.SoftDeleteDatabase(ctx, renamed.ID)
	if err != nil {
		t.Fatalf("SoftDeleteDatabase: %v", err)
	}
	if deleted.DeletedAt.IsZero() || deleted.Status != StatusDeleting {
		t.Errorf("deleted row = %+v, want deleted_at and the deleting status", deleted)
	}
	if _, err := repo.GetDatabase(ctx, renamed.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetDatabase after delete = %v, want ErrNotFound", err)
	}
	if _, err := repo.SoftDeleteDatabase(ctx, renamed.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("second SoftDeleteDatabase = %v, want ErrNotFound", err)
	}
	// The fence is the SQL WHERE deleted_at IS NULL, not the fake: every
	// scoped provisioning write must report no row on a soft-deleted row.
	if _, err := repo.UpdateDatabaseName(ctx, renamed.ID, "zombie"); !errors.Is(err, ErrNotFound) {
		t.Errorf("UpdateDatabaseName after delete = %v, want ErrNotFound", err)
	}
	if _, err := repo.UpdateDatabaseContainer(ctx, renamed.ID, "zombie-container"); !errors.Is(err, ErrNotFound) {
		t.Errorf("UpdateDatabaseContainer after delete = %v, want ErrNotFound", err)
	}
	if _, err := repo.UpdateDatabaseStatus(ctx, renamed.ID, StatusRunning); !errors.Is(err, ErrNotFound) {
		t.Errorf("UpdateDatabaseStatus after delete = %v, want ErrNotFound", err)
	}
	if _, err := repo.GetDatabase(ctx, renamed.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("row resurrected after fenced writes: %v", err)
	}
	recreated := renamed
	recreated.ID = uuid.New()
	recreated.Status = StatusCreating
	if _, err := repo.CreateDatabase(ctx, recreated); err != nil {
		t.Errorf("a soft-deleted name must be reusable: %v", err)
	}
	if _, err := repo.GetDatabase(ctx, uuid.Nil); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetDatabase(nil) = %v, want ErrNotFound", err)
	}

	// ServerExists resolves registered and unknown nodes.
	if exists, err := repo.ServerExists(ctx, serverID, teams.Scope{UserID: ownerID}); err != nil || !exists {
		t.Errorf("ServerExists(seeded) = %v, %v, want true", exists, err)
	}
	if exists, err := repo.ServerExists(ctx, uuid.New(), teams.Scope{UserID: ownerID}); err != nil || exists {
		t.Errorf("ServerExists(unknown) = %v, %v, want false", exists, err)
	}
	if exists, err := repo.ServerExists(ctx, uuid.Nil, teams.Scope{UserID: ownerID}); err != nil || exists {
		t.Errorf("ServerExists(nil) = %v, %v, want false", exists, err)
	}
}
