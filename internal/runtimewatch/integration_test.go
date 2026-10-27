package runtimewatch

import (
	"context"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/justindeelux/gotham/internal/incidents"
	"github.com/justindeelux/gotham/internal/servers"
	"github.com/justindeelux/gotham/internal/services"
	"github.com/justindeelux/gotham/internal/store"
	"github.com/justindeelux/gotham/internal/store/sqlc"
)

type recordingReporter struct {
	mu           sync.Mutex
	observations []incidents.Observation
}

func (r *recordingReporter) Report(_ context.Context, obs incidents.Observation) (*incidents.Incident, incidents.Transition, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.observations = append(r.observations, obs)
	return nil, incidents.Transition{}, nil
}

func (r *recordingReporter) Close(context.Context, string, string) error { return nil }

func (r *recordingReporter) last(identity string) (incidents.Observation, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i := len(r.observations) - 1; i >= 0; i-- {
		if r.observations[i].Identity == identity {
			return r.observations[i], true
		}
	}
	return incidents.Observation{}, false
}

func integrationStore(t *testing.T) *store.Store {
	t.Helper()
	dsn := os.Getenv("GOTHAM_TEST_DSN")
	explicit := dsn != ""
	if !explicit {
		dsn = "postgres://gotham:gotham@localhost:5432/gotham?sslmode=disable"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
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
	return store.New(pool)
}

func TestRuntimeMonitorDetectsComposeReplicaFailureAndRecovery(t *testing.T) {
	st := integrationStore(t)
	ctx := context.Background()
	user, err := st.CreateUser(ctx, "runtime-watch-"+time.Now().Format("150405.000000")+"@example.com", nil)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	userID := uuid.UUID(user.ID.Bytes)
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, _ = st.DB.Exec(cleanupCtx, "DELETE FROM users WHERE id = $1", user.ID)
	})
	project, err := st.CreateProject(ctx, sqlc.CreateProjectParams{
		ID:   pgtype.UUID{Bytes: uuid.New(), Valid: true},
		Name: fmt.Sprintf("runtime-watch-%d", time.Now().UnixNano()),
	})
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, _ = st.DB.Exec(cleanupCtx, "DELETE FROM projects WHERE id = $1", project.ID)
	})
	environment, err := st.CreateEnvironment(ctx, sqlc.CreateEnvironmentParams{
		ID:        pgtype.UUID{Bytes: uuid.New(), Valid: true},
		ProjectID: project.ID,
		Name:      "production",
	})
	if err != nil {
		t.Fatalf("CreateEnvironment: %v", err)
	}
	serverRow, err := st.CreateServer(ctx, sqlc.CreateServerParams{
		Name: "runtime-watch-node", Ip: "127.0.0.1", Port: 22, SshUser: "root",
	})
	if err != nil {
		t.Fatalf("CreateServer: %v", err)
	}
	serverID := uuid.UUID(serverRow.ID.Bytes)
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, _ = st.DB.Exec(cleanupCtx, "DELETE FROM servers WHERE id = $1", serverRow.ID)
	})
	appRow, err := st.CreateApplication(ctx, sqlc.CreateApplicationParams{
		ID:            pgtype.UUID{Bytes: uuid.New(), Valid: true},
		UserID:        pgtype.UUID{Bytes: userID, Valid: true},
		ServerID:      pgtype.UUID{Bytes: serverID, Valid: true},
		EnvironmentID: environment.ID,
		Name:          "runtime-watch-app",
		SourceType:    "compose",
		Provider:      "compose",
		Branch:        "main",
		BuildPack:     "compose",
	})
	if err != nil {
		t.Fatalf("CreateApplication: %v", err)
	}
	appID := uuid.UUID(appRow.ID.Bytes)
	databaseRow, err := st.CreateDatabase(ctx, sqlc.CreateDatabaseParams{
		ID:            pgtype.UUID{Bytes: uuid.New(), Valid: true},
		UserID:        pgtype.UUID{Bytes: userID, Valid: true},
		ServerID:      pgtype.UUID{Bytes: serverID, Valid: true},
		EnvironmentID: environment.ID,
		Name:          "runtime-watch-db-" + time.Now().Format("150405"),
		Engine:        "postgres",
		Version:       "16",
		Status:        "stopped",
		StoragePath:   "/tmp/runtime-watch-db",
	})
	if err != nil {
		t.Fatalf("CreateDatabase: %v", err)
	}
	dbID := uuid.UUID(databaseRow.ID.Bytes)

	reporter := &recordingReporter{}
	svc := NewService(Config{Store: st, Incidents: reporter, Now: time.Now})
	projectName := services.ProjectName(appID)
	now := time.Now().UTC()

	replica := func(number, state, health string) servers.ContainerObservation {
		c := servers.ContainerObservation{
			ID:             appID.String()[:8] + number,
			State:          state,
			Health:         health,
			ComposeProject: projectName,
			ComposeService: "web",
			Replica:        number,
			RestartPolicy:  "always",
		}
		return c
	}

	observe := func(at time.Time, complete bool, containers []servers.ContainerObservation) {
		svc.Observe(ctx, servers.NodeObservation{
			ServerID:   serverID,
			ObservedAt: at,
			Inventory:  servers.ContainerInventory{Complete: complete, Containers: containers},
		})
	}

	observe(now, true, []servers.ContainerObservation{
		replica("1", "running", "healthy"),
		replica("2", "running", "unhealthy"),
	})
	identity2 := incidents.Identity(DetectorHealth, ResourceApplication, appID, "web", "2")
	obs, ok := reporter.last(identity2)
	if !ok || !obs.Valid || !obs.Unhealthy {
		t.Fatalf("unhealthy replica not reported: %+v (found=%v)", obs, ok)
	}

	// A healthy sibling must not mask the failed expected replica.
	observe(now.Add(15*time.Second), true, []servers.ContainerObservation{
		replica("1", "running", "healthy"),
		replica("2", "exited", ""),
	})
	obs, ok = reporter.last(identity2)
	if !ok || !obs.Unhealthy || obs.Severity != incidents.SeverityCritical {
		t.Fatalf("stopped replica not critical: %+v (found=%v)", obs, ok)
	}

	// A complete inventory that no longer contains replica 2 proves absence.
	observe(now.Add(30*time.Second), true, []servers.ContainerObservation{
		replica("1", "running", "healthy"),
	})
	obs, ok = reporter.last(identity2)
	if !ok || !obs.Unhealthy || obs.Evidence == "" {
		t.Fatalf("missing replica not reported: %+v (found=%v)", obs, ok)
	}

	// Recovery returns a valid healthy report for the same identity.
	observe(now.Add(45*time.Second), true, []servers.ContainerObservation{
		replica("1", "running", "healthy"),
		replica("2", "running", "healthy"),
	})
	obs, ok = reporter.last(identity2)
	if !ok || !obs.Valid || obs.Unhealthy {
		t.Fatalf("recovered replica not reported healthy: %+v (found=%v)", obs, ok)
	}

	// A partial inventory cannot prove absence and interrupts recovery.
	observe(now.Add(60*time.Second), false, nil)
	obs, ok = reporter.last(identity2)
	if !ok || obs.Valid {
		t.Fatalf("partial inventory did not interrupt evidence: %+v (found=%v)", obs, ok)
	}

	// A stopped database is intentional, not a failure.
	dbContainer := servers.ContainerObservation{ID: "db1234567890", State: "exited", DatabaseID: dbID.String(), Managed: true}
	observe(now.Add(90*time.Second), true, []servers.ContainerObservation{dbContainer})
	dbIdentity := incidents.Identity(DetectorHealth, ResourceDatabase, dbID, "", "")
	obs, ok = reporter.last(dbIdentity)
	if !ok || !obs.Expected {
		t.Fatalf("stopped database not treated as expected: %+v (found=%v)", obs, ok)
	}
	if obs.Valid && obs.Unhealthy {
		t.Fatal("intentionally stopped database raised a failure")
	}
}
