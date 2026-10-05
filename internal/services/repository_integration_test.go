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
// Override with GOTHAM_TEST_DSN; an explicit value turns a missing database or
// a failed migration into a test failure instead of a skip, matching
// internal/databases' integration test.
const defaultIntegrationDSN = "postgres://gotham:gotham@localhost:5432/gotham?sslmode=disable"

// integrationEnv runs the embedded migrations and returns a repository backed
// by a real PostgreSQL. It skips only when GOTHAM_TEST_DSN is unset; an
// explicit DSN makes an unreachable database or a failed migration fatal.
func integrationEnv(t *testing.T) (*storeRepository, *store.Store) {
	t.Helper()

	dsn := os.Getenv("GOTHAM_TEST_DSN")
	explicit := dsn != ""
	if !explicit {
		dsn = defaultIntegrationDSN
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// ProbeOnce fails fast when no database is listening, sparing Open's
	// retry loop; an Open failure past a good probe is a real error.
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
	// Registered first so LIFO order closes the pool after the row cleanups.
	t.Cleanup(pool.Close)
	// Open proved the database is reachable, so a migration error is a real
	// failure, never a skip (D1-12).
	if err := store.Migrate(ctx, dsn, store.MigrateUp); err != nil {
		t.Fatalf("migration failed: %v", err)
	}

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

// seedProjectEnvironment creates a team, a project and an environment on the
// shared test database and returns their IDs, cleaning them up at test end.
func seedProjectEnvironment(t *testing.T, st *store.Store) (teamID, projectID, envID uuid.UUID) {
	t.Helper()
	ctx := context.Background()

	team, err := st.CreateTeam(ctx, sqlc.CreateTeamParams{
		ID:   pgtype.UUID{Bytes: uuid.New(), Valid: true},
		Name: fmt.Sprintf("p13-%d", time.Now().UnixNano()),
	})
	if err != nil {
		t.Fatalf("CreateTeam: %v", err)
	}
	project, err := st.CreateProject(ctx, sqlc.CreateProjectParams{
		ID:     pgtype.UUID{Bytes: uuid.New(), Valid: true},
		TeamID: team.ID,
		Name:   fmt.Sprintf("shop-%d", time.Now().UnixNano()),
	})
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	environment, err := st.CreateEnvironment(ctx, sqlc.CreateEnvironmentParams{
		ID:        pgtype.UUID{Bytes: uuid.New(), Valid: true},
		ProjectID: project.ID,
		Name:      "production",
	})
	if err != nil {
		t.Fatalf("CreateEnvironment: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		for _, query := range []string{
			"DELETE FROM applications WHERE environment_id = $1",
			"DELETE FROM services WHERE environment_id = $1",
			"DELETE FROM databases WHERE environment_id = $1",
		} {
			if _, err := st.DB.Exec(cleanupCtx, query, environment.ID); err != nil {
				t.Logf("cleanup resources: %v", err)
			}
		}
		if _, err := st.DB.Exec(cleanupCtx, "DELETE FROM teams WHERE id = $1", team.ID); err != nil {
			t.Logf("cleanup team: %v", err)
		}
	})
	return uuidFromPG(team.ID), uuidFromPG(project.ID), uuidFromPG(environment.ID)
}

// TestProxySourceRendersDomainMap proves the proxy adapter derives the routing
// input from the stored document and environment (no stored copy), reports an
// unrenderable routing document as unroutable, and skips services without
// domains.
func TestProxySourceRendersDomainMap(t *testing.T) {
	repo, st := integrationEnv(t)
	ctx := context.Background()
	ownerID, serverID := seedUserAndServer(t, st)
	teamID, _, envID := seedProjectEnvironment(t, st)

	routed, err := repo.CreateService(ctx, Service{
		ID: uuid.New(), UserID: ownerID, TeamID: teamID, ServerID: serverID, EnvironmentID: envID,
		Name: "routed", Status: StatusRunning,
		ComposeYAML: testDocument, Env: testEnv,
	})
	if err != nil {
		t.Fatalf("CreateService(routed): %v", err)
	}
	unrouted, err := repo.CreateService(ctx, Service{
		ID: uuid.New(), UserID: ownerID, TeamID: teamID, ServerID: serverID, EnvironmentID: envID,
		Name: "plain", Status: StatusRunning,
		ComposeYAML: "services:\n  web:\n    image: nginx:1.23\n", Env: map[string]string{},
	})
	if err != nil {
		t.Fatalf("CreateService(plain): %v", err)
	}
	broken, err := repo.CreateService(ctx, Service{
		ID: uuid.New(), UserID: ownerID, TeamID: teamID, ServerID: serverID, EnvironmentID: envID,
		Name: "broken", Status: StatusRunning,
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

// TestUpdateServiceConfigKeepsUnsetPlacement pins F3 at the SQL level: a
// Nil environment or server (the service zeroes placement the request
// leaves alone) keeps the stored values instead of writing NULL, while a
// set value moves the row.
func TestUpdateServiceConfigKeepsUnsetPlacement(t *testing.T) {
	repo, st := integrationEnv(t)
	ctx := context.Background()
	ownerID, serverID := seedUserAndServer(t, st)
	teamID, projectID, envID := seedProjectEnvironment(t, st)

	created, err := repo.CreateService(ctx, Service{
		ID: uuid.New(), UserID: ownerID, TeamID: teamID, ServerID: serverID, EnvironmentID: envID,
		Name: "wordpress", Status: StatusCreating, ComposeYAML: testDocument, Env: testEnv,
	})
	if err != nil {
		t.Fatalf("CreateService: %v", err)
	}

	renamed, err := repo.UpdateServiceConfig(ctx, Service{
		ID: created.ID, Name: "renamed", ComposeYAML: testDocument, Env: testEnv,
	})
	if err != nil {
		t.Fatalf("UpdateServiceConfig (rename only): %v", err)
	}
	if renamed.Name != "renamed" {
		t.Fatalf("name = %q, want renamed", renamed.Name)
	}
	if renamed.EnvironmentID != envID || renamed.ServerID != serverID {
		t.Fatalf("placement = %s/%s, want the stored %s/%s",
			renamed.EnvironmentID, renamed.ServerID, envID, serverID)
	}

	_, otherProjectID, _ := seedProjectEnvironment(t, st)
	_ = otherProjectID
	otherEnv, err := st.CreateEnvironment(ctx, sqlc.CreateEnvironmentParams{
		ID:        pgtype.UUID{Bytes: uuid.New(), Valid: true},
		ProjectID: pgtype.UUID{Bytes: projectID, Valid: true},
		Name:      "staging",
	})
	if err != nil {
		t.Fatalf("CreateEnvironment (staging): %v", err)
	}
	otherEnvID := uuidFromPG(otherEnv.ID)
	moved, err := repo.UpdateServiceConfig(ctx, Service{
		ID: created.ID, Name: "renamed", ComposeYAML: testDocument, Env: testEnv,
		EnvironmentID: otherEnvID, ServerID: serverID,
	})
	if err != nil {
		t.Fatalf("UpdateServiceConfig (explicit move): %v", err)
	}
	if moved.EnvironmentID != otherEnvID {
		t.Fatalf("environment = %s, want %s", moved.EnvironmentID, otherEnvID)
	}
}
func TestRepositoryRoundTrip(t *testing.T) {
	repo, st := integrationEnv(t)
	ctx := context.Background()
	ownerID, serverID := seedUserAndServer(t, st)
	otherUserID, _ := seedUserAndServer(t, st)
	teamID, _, envID := seedProjectEnvironment(t, st)

	now := time.Now().UTC()
	created, err := repo.CreateService(ctx, Service{
		ID:            uuid.New(),
		UserID:        ownerID,
		TeamID:        teamID,
		ServerID:      serverID,
		EnvironmentID: envID,
		Name:          "wordpress",
		Status:        StatusCreating,
		ComposeYAML:   testDocument,
		Env:           testEnv,
		CreatedAt:     now,
		UpdatedAt:     now,
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

	// Same name, same environment → conflict (per-environment uniqueness);
	// same name, other user, same environment → still conflict; a different
	// environment allows the name.
	duplicate := created
	duplicate.ID = uuid.New()
	if _, err := repo.CreateService(ctx, duplicate); !errors.Is(err, ErrConflict) {
		t.Errorf("duplicate CreateService error = %v, want ErrConflict", err)
	}
	duplicate.UserID = otherUserID
	if _, err := repo.CreateService(ctx, duplicate); !errors.Is(err, ErrConflict) {
		t.Errorf("same-environment duplicate for another user error = %v, want ErrConflict", err)
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
