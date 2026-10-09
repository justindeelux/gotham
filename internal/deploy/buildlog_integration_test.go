package deploy

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/store"
	"github.com/justindeelux/gotham/internal/store/sqlc"
)

// TestStoreRepositoryBuildLogRoundtrip persists a capped build log through
// the repository and reads it back with the deployment: the stream lines a
// finished run teed, the UTF-8 sanitized tail the API serves.
func TestStoreRepositoryBuildLogRoundtrip(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	dsn := integrationDSN()
	if err := store.ProbeOnce(ctx, dsn); err != nil {
		if integrationDSNExplicit() {
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

	const secret = "integration-secret"
	st := store.New(pool)
	repo := newStoreRepository(st, secret)

	email := "buildlog-roundtrip@example.com"
	if _, err := pool.Exec(ctx, "DELETE FROM users WHERE email = $1", email); err != nil {
		t.Fatalf("cleanup user: %v", err)
	}
	user, err := st.CreateUser(ctx, email, nil)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	userID := uuid.UUID(user.ID.Bytes)
	teamID, envID, serverID := seedProjectEnvironment(t, ctx, st)

	app, err := st.CreateApplication(ctx, sqlc.CreateApplicationParams{
		UserID:        pgUUID(userID),
		TeamID:        pgUUID(teamID),
		ServerID:      pgUUID(serverID),
		EnvironmentID: pgUUID(envID),
		Name:          "buildlog-app",
		Provider:      "github",
		Repo:          "acme/demo",
		CloneUrl:      "https://github.com/acme/demo.git",
		Branch:        "main",
		BuildPack:     "dockerfile",
	})
	if err != nil {
		t.Fatalf("CreateApplication: %v", err)
	}
	appID := uuid.UUID(app.ID.Bytes)

	dep, err := repo.CreateDeployment(ctx, Deployment{
		ApplicationID: appID,
		Kind:          KindDeploy,
		State:         StateQueued,
	})
	if err != nil {
		t.Fatalf("CreateDeployment: %v", err)
	}

	// A fresh deployment carries no log: the run is still in flight.
	if got, err := repo.GetDeployment(ctx, appID, dep.ID); err != nil || got.BuildLog != "" {
		t.Fatalf("GetDeployment = %q, %v; want empty, nil", got.BuildLog, err)
	}

	// An oversized log with invalid UTF-8 stores as the sanitized tail.
	raw := strings.Repeat("H\n", maxBuildLogBytes) + "\xff\xfe broken\ntail line\n"
	if err := repo.UpdateDeploymentBuildLog(ctx, dep.ID, sanitizeBuildLog(raw)); err != nil {
		t.Fatalf("UpdateDeploymentBuildLog: %v", err)
	}
	got, err := repo.GetDeployment(ctx, appID, dep.ID)
	if err != nil {
		t.Fatalf("GetDeployment: %v", err)
	}
	if len(got.BuildLog) > maxBuildLogBytes {
		t.Fatalf("len = %d, want at most %d", len(got.BuildLog), maxBuildLogBytes)
	}
	if !strings.HasSuffix(got.BuildLog, "tail line\n") {
		t.Error("stored log does not keep the tail")
	}

	// State-machine writes never clobber the stored log.
	dep.State = StateFailed
	dep.Error = "boom"
	if _, err := repo.UpdateDeployment(ctx, dep); err != nil {
		t.Fatalf("UpdateDeployment: %v", err)
	}
	if got, err := repo.GetDeployment(ctx, appID, dep.ID); err != nil || !strings.HasSuffix(got.BuildLog, "tail line\n") {
		t.Fatalf("GetDeployment after transition = %q, %v; want the log kept", got.BuildLog, err)
	}
}
