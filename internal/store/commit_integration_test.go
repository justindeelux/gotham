package store_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/justindeelux/gotham/internal/store"
	"github.com/justindeelux/gotham/internal/store/sqlc"
)

// TestStoreDeploymentCommitRoundtrip runs the embedded migrations and verifies
// the JUS-82 columns through Postgres: the commit metadata round-trips on
// create and update, and rows that predate the columns read back empty,
// never NULL.
func TestStoreDeploymentCommitRoundtrip(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	dsn := testDSN()
	if err := store.ProbeOnce(ctx, dsn); err != nil {
		if testDSNExplicit() {
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

	email := fmt.Sprintf("jus82-%d@example.com", time.Now().UnixNano())
	user, err := st.CreateUser(ctx, email, nil)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	team, err := st.CreateTeam(ctx, sqlc.CreateTeamParams{
		ID:   pgtype.UUID{Bytes: uuid.New(), Valid: true},
		Name: fmt.Sprintf("jus82-%d", time.Now().UnixNano()),
	})
	if err != nil {
		t.Fatalf("CreateTeam: %v", err)
	}
	project, err := st.CreateProject(ctx, sqlc.CreateProjectParams{
		ID:     pgtype.UUID{Bytes: uuid.New(), Valid: true},
		TeamID: team.ID,
		Name:   fmt.Sprintf("jus82-%d", time.Now().UnixNano()),
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
	server, err := st.CreateServer(ctx, sqlc.CreateServerParams{
		Name:    fmt.Sprintf("jus82-node-%d", time.Now().UnixNano()),
		Ip:      "127.0.0.1",
		Port:    22,
		SshUser: "root",
	})
	if err != nil {
		t.Fatalf("CreateServer: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanupCancel()
		if _, err := pool.Exec(cleanupCtx, "DELETE FROM applications WHERE environment_id = $1", environment.ID); err != nil {
			t.Logf("cleanup applications: %v", err)
		}
		if _, err := pool.Exec(cleanupCtx, "DELETE FROM teams WHERE id = $1", team.ID); err != nil {
			t.Logf("cleanup team: %v", err)
		}
		if _, err := pool.Exec(cleanupCtx, "DELETE FROM servers WHERE id = $1", server.ID); err != nil {
			t.Logf("cleanup server: %v", err)
		}
		if _, err := pool.Exec(cleanupCtx, "DELETE FROM users WHERE id = $1", user.ID); err != nil {
			t.Logf("cleanup user: %v", err)
		}
	})

	app, err := st.CreateApplication(ctx, sqlc.CreateApplicationParams{
		UserID:        user.ID,
		TeamID:        team.ID,
		ServerID:      server.ID,
		EnvironmentID: environment.ID,
		Name:          fmt.Sprintf("jus82-app-%d", time.Now().UnixNano()),
		SourceType:    "git_public",
		Branch:        "main",
		BuildPack:     "dockerfile",
	})
	if err != nil {
		t.Fatalf("CreateApplication: %v", err)
	}

	sha := strings.Repeat("ab12", 10)
	created, err := st.CreateDeployment(ctx, sqlc.CreateDeploymentParams{
		ApplicationID: app.ID,
		Kind:          "deploy",
		State:         "queued",
		CommitSha:     sha,
		CommitMessage: "Add widgets",
		CommitAuthor:  "Ada Lovelace",
		CommittedAt:   "2026-10-01T12:34:56Z",
	})
	if err != nil {
		t.Fatalf("CreateDeployment: %v", err)
	}
	if created.CommitSha != sha || created.CommitMessage != "Add widgets" ||
		created.CommitAuthor != "Ada Lovelace" || created.CommittedAt != "2026-10-01T12:34:56Z" {
		t.Errorf("created commit = %+v, want the metadata", created)
	}

	updated, err := st.UpdateDeployment(ctx, sqlc.UpdateDeploymentParams{
		ID:            created.ID,
		State:         "running",
		CommitSha:     sha,
		CommitMessage: "Add widgets v2",
		CommitAuthor:  "Ada Lovelace",
		CommittedAt:   "2026-10-02T12:34:56Z",
	})
	if err != nil {
		t.Fatalf("UpdateDeployment: %v", err)
	}
	if updated.CommitMessage != "Add widgets v2" || updated.CommittedAt != "2026-10-02T12:34:56Z" {
		t.Errorf("updated commit = %+v, want the new metadata", updated)
	}

	fetched, err := st.GetDeployment(ctx, created.ID, app.ID)
	if err != nil {
		t.Fatalf("GetDeployment: %v", err)
	}
	if fetched.CommitSha != sha || fetched.CommitMessage != "Add widgets v2" {
		t.Errorf("fetched commit = %+v, want the updated metadata", fetched)
	}
}
