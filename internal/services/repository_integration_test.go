package services

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/justindeelux/gotham/internal/proxy"
	"github.com/justindeelux/gotham/internal/store"
	"github.com/justindeelux/gotham/internal/store/sqlc"
	"github.com/justindeelux/gotham/internal/teams"
)

// defaultIntegrationDSN points at the dev database from deploy/compose.dev.yml.
// Override with GOTHAM_TEST_DSN (or GOTHAM_TEST_DSN=postgres://nope to force a
// skip), matching internal/databases' integration test.
const defaultIntegrationDSN = "postgres://gotham:gotham@localhost:5432/gotham?sslmode=disable"

// integrationEnv runs the embedded migrations and returns a repository backed
// by a real PostgreSQL, skipping the test when no database is reachable so CI
// stays green without one.
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

// seedUserAndServer registers the owner and the node a test service needs.
func seedUserAndServer(t *testing.T, st *store.Store) (uuid.UUID, uuid.UUID) {
	t.Helper()
	ctx := context.Background()

	user, err := st.CreateUser(ctx, fmt.Sprintf("p7-%d@example.com", time.Now().UnixNano()), nil)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, err := st.DB.Exec(cleanupCtx, "DELETE FROM users WHERE id = $1", user.ID); err != nil {
			t.Logf("cleanup user: %v", err)
		}
	})

	server, err := st.CreateServer(ctx, sqlc.CreateServerParams{
		Name:     fmt.Sprintf("p7-node-%d", time.Now().UnixNano()),
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

// TestProxySourceRendersDomainMap proves the proxy adapter derives the routing
// input from the stored document and environment (no stored copy), reports an
// unrenderable routing document as unroutable, and skips services without
// domains.
func TestProxySourceRendersDomainMap(t *testing.T) {
	repo, st := integrationEnv(t)
	ctx := context.Background()
	ownerID, serverID := seedUserAndServer(t, st)

	routed, err := repo.CreateService(ctx, Service{
		ID: uuid.New(), UserID: ownerID, ServerID: serverID, Name: "routed", Status: StatusRunning,
		ComposeYAML: testDocument, Env: testEnv,
	})
	if err != nil {
		t.Fatalf("CreateService(routed): %v", err)
	}
	unrouted, err := repo.CreateService(ctx, Service{
		ID: uuid.New(), UserID: ownerID, ServerID: serverID, Name: "plain", Status: StatusRunning,
		ComposeYAML: "services:\n  web:\n    image: nginx:1.23\n", Env: map[string]string{},
	})
	if err != nil {
		t.Fatalf("CreateService(plain): %v", err)
	}
	broken, err := repo.CreateService(ctx, Service{
		ID: uuid.New(), UserID: ownerID, ServerID: serverID, Name: "broken", Status: StatusRunning,
		ComposeYAML: testDocument, Env: map[string]string{}, // DOMAIN/PASSWORD unresolved
	})
	if err != nil {
		t.Fatalf("CreateService(broken): %v", err)
	}

	proxied, err := NewProxySource(st).ListProxiedServices(ctx)
	if err != nil {
		t.Fatalf("ListProxiedServices: %v", err)
	}
	byID := map[uuid.UUID]proxy.ProxiedService{}
	for _, entry := range proxied {
		byID[entry.ID] = entry
	}
	if _, ok := byID[unrouted.ID]; ok {
		t.Errorf("a service without routing labels must not be listed: %+v", byID[unrouted.ID])
	}
	routedEntry, ok := byID[routed.ID]
	if !ok {
		t.Fatalf("the routed service is missing from %+v", proxied)
	}
	if routedEntry.Project != ProjectName(routed.ID) || routedEntry.ServerID != serverID {
		t.Errorf("entry = %+v", routedEntry)
	}
	if len(routedEntry.Domains) != 1 || routedEntry.Domains[0].Host != "app.example.com" ||
		routedEntry.Domains[0].Service != "web" || routedEntry.Domains[0].Port != 80 {
		t.Errorf("domains = %+v", routedEntry.Domains)
	}
	brokenEntry, ok := byID[broken.ID]
	if !ok || brokenEntry.Unroutable == "" {
		t.Fatalf("an unrenderable routing document must be reported unroutable: %+v", byID[broken.ID])
	}
	if strings.Contains(brokenEntry.Unroutable, "hunter2-secret") {
		t.Errorf("the unroutable reason leaked an environment value: %q", brokenEntry.Unroutable)
	}
}

// TestRepositoryRoundTrip exercises the SQL behind the repository: the
// migrations, ownership reads, the unique-name index, the jsonb environment,
// the deploy history and the soft delete that keeps the volumes.
func TestRepositoryRoundTrip(t *testing.T) {
	repo, st := integrationEnv(t)
	ctx := context.Background()
	ownerID, serverID := seedUserAndServer(t, st)
	otherUserID, _ := seedUserAndServer(t, st)

	now := time.Now().UTC()
	created, err := repo.CreateService(ctx, Service{
		ID:          uuid.New(),
		UserID:      ownerID,
		ServerID:    serverID,
		Name:        "wordpress",
		Status:      StatusCreating,
		ComposeYAML: testDocument,
		Env:         testEnv,
		CreatedAt:   now,
		UpdatedAt:   now,
	})
	if err != nil {
		t.Fatalf("CreateService: %v", err)
	}
	if created.Name != "wordpress" || created.Status != StatusCreating {
		t.Fatalf("created = %+v", created)
	}

	fetched, err := repo.GetService(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetService: %v", err)
	}
	if fetched.UserID != ownerID || fetched.ServerID != serverID {
		t.Errorf("ownership = %s/%s, want %s/%s", fetched.UserID, fetched.ServerID, ownerID, serverID)
	}
	if fetched.Env["PASSWORD"] != "hunter2-secret" || len(fetched.Env) != 2 {
		t.Errorf("env = %+v", fetched.Env)
	}
	if fetched.ComposeYAML != testDocument {
		t.Errorf("compose_yaml = %q", fetched.ComposeYAML)
	}

	// Same name, same user → conflict; same name, other user → allowed.
	duplicate := created
	duplicate.ID = uuid.New()
	if _, err := repo.CreateService(ctx, duplicate); !errors.Is(err, ErrConflict) {
		t.Errorf("duplicate CreateService error = %v, want ErrConflict", err)
	}
	duplicate.UserID = otherUserID
	if _, err := repo.CreateService(ctx, duplicate); err != nil {
		t.Errorf("another user's service with the same name: %v", err)
	}

	// Status vocabulary is enforced by the CHECK constraint.
	created.Status = "not-a-status"
	if _, err := repo.UpdateServiceStatus(ctx, created.ID, created.Status); err == nil {
		t.Error("an unknown status must be rejected by the CHECK constraint")
	}
	created.Status = StatusRunning
	created.Name = "renamed"
	updated, err := repo.UpdateServiceConfig(ctx, created)
	if err != nil {
		t.Fatalf("UpdateServiceConfig: %v", err)
	}
	if updated.Status != StatusCreating {
		t.Errorf("a configuration write must not change the status, got %q", updated.Status)
	}
	if _, err := repo.UpdateServiceStatus(ctx, created.ID, StatusRunning); err != nil {
		t.Fatalf("UpdateServiceStatus: %v", err)
	}
	if _, err := repo.UpdateServiceStatus(ctx, created.ID, StatusRunning); err != nil {
		t.Fatalf("UpdateServiceStatus (again): %v", err)
	}

	// Deploy history: newest first, snapshot preserved, error recorded.
	first, err := repo.CreateServiceDeploy(ctx, Deploy{
		ID: uuid.New(), ServiceID: created.ID, State: DeployDeploying, ComposeYAML: "services: {}\n",
		CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		t.Fatalf("CreateServiceDeploy: %v", err)
	}
	first.State = DeployFailed
	first.Error = "services: deploy failed"
	first.FinishedAt = time.Now().UTC()
	if _, err := repo.UpdateServiceDeploy(ctx, first); err != nil {
		t.Fatalf("UpdateServiceDeploy: %v", err)
	}
	if _, err := repo.CreateServiceDeploy(ctx, Deploy{
		ID: uuid.New(), ServiceID: created.ID, State: DeployRunning, ComposeYAML: "services: {}\n# second\n",
		CreatedAt: now.Add(time.Second), UpdatedAt: now.Add(time.Second), FinishedAt: now.Add(time.Second),
	}); err != nil {
		t.Fatalf("CreateServiceDeploy (second): %v", err)
	}
	deploys, err := repo.ListServiceDeploys(ctx, created.ID, 10)
	if err != nil {
		t.Fatalf("ListServiceDeploys: %v", err)
	}
	if len(deploys) != 2 || deploys[0].State != DeployRunning || deploys[1].Error == "" {
		t.Fatalf("deploys = %+v", deploys)
	}
	if deploys[0].ComposeYAML == deploys[1].ComposeYAML {
		t.Errorf("deploy snapshots must be per-attempt")
	}
	if deploys[1].FinishedAt.IsZero() {
		t.Errorf("finished_at = zero, want the recorded time")
	}

	// Soft delete hides the row and frees the name for reuse.
	deleted, err := repo.SoftDeleteService(ctx, created.ID)
	if err != nil {
		t.Fatalf("SoftDeleteService: %v", err)
	}
	if deleted.DeletedAt.IsZero() || deleted.Status != StatusDeleting {
		t.Errorf("deleted = %+v", deleted)
	}
	if _, err := repo.GetService(ctx, created.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetService after delete = %v, want ErrNotFound", err)
	}
	if _, err := repo.SoftDeleteService(ctx, created.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("second SoftDeleteService = %v, want ErrNotFound", err)
	}
	recreated := created
	recreated.ID = uuid.New()
	recreated.Status = StatusCreating
	recreated.DeletedAt = time.Time{}
	if _, err := repo.CreateService(ctx, recreated); err != nil {
		t.Errorf("a soft-deleted name must be reusable: %v", err)
	}
	if _, err := repo.GetService(ctx, uuid.Nil); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetService(nil) = %v, want ErrNotFound", err)
	}

	// The owner sees only their live rows.
	list, err := repo.ListServices(ctx, teams.Scope{UserID: ownerID})
	if err != nil {
		t.Fatalf("ListServicesByUser: %v", err)
	}
	if len(list) != 1 || list[0].ID != recreated.ID {
		t.Errorf("owner list = %+v, want only the recreated row", list)
	}

	// ServerExists resolves registered and unknown nodes.
	if exists, err := repo.ServerExists(ctx, serverID); err != nil || !exists {
		t.Errorf("ServerExists(seeded) = %v, %v, want true", exists, err)
	}
	if exists, err := repo.ServerExists(ctx, uuid.New()); err != nil || exists {
		t.Errorf("ServerExists(unknown) = %v, %v, want false", exists, err)
	}
	if exists, err := repo.ServerExists(ctx, uuid.Nil); err != nil || exists {
		t.Errorf("ServerExists(nil) = %v, %v, want false", exists, err)
	}
}
