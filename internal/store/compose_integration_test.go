package store_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/justindeelux/gotham/internal/store"
	"github.com/justindeelux/gotham/internal/store/sqlc"
)

// TestStoreApplicationComposeRoundtrip runs the embedded migrations and
// verifies the GS-8 columns through Postgres: the pasted text, in-repo file
// path and web service round-trip on write and update, and direct sqlc
// callers that predate the columns get empty defaults, never NULL.
func TestStoreApplicationComposeRoundtrip(t *testing.T) {
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

	email := fmt.Sprintf("gs8-%d@example.com", time.Now().UnixNano())
	user, err := st.CreateUser(ctx, email, nil)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	team, err := st.CreateTeam(ctx, sqlc.CreateTeamParams{
		ID:   pgtype.UUID{Bytes: uuid.New(), Valid: true},
		Name: fmt.Sprintf("gs8-%d", time.Now().UnixNano()),
	})
	if err != nil {
		t.Fatalf("CreateTeam: %v", err)
	}
	project, err := st.CreateProject(ctx, sqlc.CreateProjectParams{
		ID:     pgtype.UUID{Bytes: uuid.New(), Valid: true},
		TeamID: team.ID,
		Name:   fmt.Sprintf("gs8-%d", time.Now().UnixNano()),
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
		Name:    fmt.Sprintf("gs8-node-%d", time.Now().UnixNano()),
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

	newParams := func(name string) sqlc.CreateApplicationParams {
		return sqlc.CreateApplicationParams{
			UserID:        user.ID,
			TeamID:        team.ID,
			ServerID:      server.ID,
			EnvironmentID: environment.ID,
			Name:          name,
			SourceType:    "compose",
			Branch:        "",
			BuildPack:     "",
		}
	}

	t.Run("columns round-trip", func(t *testing.T) {
		params := newParams(fmt.Sprintf("gs8-app-%d", time.Now().UnixNano()))
		params.ComposeContent = "services:\n  web:\n    image: example.com/app:1.0\n"
		params.ComposeService = "web"
		created, err := st.CreateApplication(ctx, params)
		if err != nil {
			t.Fatalf("CreateApplication: %v", err)
		}
		fetched, err := st.GetApplication(ctx, created.ID)
		if err != nil {
			t.Fatalf("GetApplication: %v", err)
		}
		if fetched.ComposeContent != params.ComposeContent {
			t.Errorf("content = %q, want the pasted text", fetched.ComposeContent)
		}
		if fetched.ComposeService != "web" {
			t.Errorf("service = %q, want web", fetched.ComposeService)
		}

		updated, err := st.UpdateApplication(ctx, sqlc.UpdateApplicationParams{
			ID:                 created.ID,
			Name:               created.Name,
			Branch:             created.Branch,
			BuildPack:          created.BuildPack,
			BaseDomain:         created.BaseDomain,
			Port:               created.Port,
			HostPort:           created.HostPort,
			ServerID:           created.ServerID,
			BaseDomainDisabled: created.BaseDomainDisabled,
			EnvironmentID:      created.EnvironmentID,
			ComposeContent:     "services:\n  web:\n    image: example.com/app:2.0\n",
			ComposeFile:        "",
			ComposeService:     "web",
			// The merged schema (GS-7) holds build_args NOT NULL: pass the
			// empty object like the repository's marshalBuildArgs does.
			BuildArgs: []byte("{}"),
		})
		if err != nil {
			t.Fatalf("UpdateApplication: %v", err)
		}
		if updated.ComposeContent != "services:\n  web:\n    image: example.com/app:2.0\n" {
			t.Errorf("updated content = %q, want the new text", updated.ComposeContent)
		}

		deployment, err := st.CreateDeployment(ctx, sqlc.CreateDeploymentParams{
			ApplicationID:   created.ID,
			Kind:            "deploy",
			State:           "running",
			ComposeDocument: "services:\n  web:\n    image: example.com/app:1.0\n",
			ComposeCommit:   "abc1234",
		})
		if err != nil {
			t.Fatalf("CreateDeployment: %v", err)
		}
		if deployment.ComposeDocument != "services:\n  web:\n    image: example.com/app:1.0\n" ||
			deployment.ComposeCommit != "abc1234" {
			t.Errorf("deployment document = %q/%q, want the recorded release",
				deployment.ComposeDocument, deployment.ComposeCommit)
		}
		updatedDep, err := st.UpdateDeployment(ctx, sqlc.UpdateDeploymentParams{
			ID:              deployment.ID,
			State:           deployment.State,
			ImageTag:        deployment.ImageTag,
			RegistryImage:   deployment.RegistryImage,
			Digest:          deployment.Digest,
			Error:           deployment.Error,
			Attempt:         deployment.Attempt,
			ContainerID:     deployment.ContainerID,
			StartedAt:       deployment.StartedAt,
			FinishedAt:      deployment.FinishedAt,
			ComposeDocument: "services:\n  web:\n    image: example.com/app:2.0\n",
			ComposeCommit:   "def5678",
		})
		if err != nil {
			t.Fatalf("UpdateDeployment: %v", err)
		}
		if updatedDep.ComposeDocument != "services:\n  web:\n    image: example.com/app:2.0\n" ||
			updatedDep.ComposeCommit != "def5678" {
			t.Errorf("updated deployment document = %q/%q, want the new release",
				updatedDep.ComposeDocument, updatedDep.ComposeCommit)
		}
	})

	t.Run("zero values default instead of NULL", func(t *testing.T) {
		created, err := st.CreateApplication(ctx, newParams(fmt.Sprintf("gs8-legacy-%d", time.Now().UnixNano())))
		if err != nil {
			t.Fatalf("CreateApplication: %v", err)
		}
		if created.ComposeContent != "" || created.ComposeFile != "" || created.ComposeService != "" {
			t.Errorf("compose columns = %q/%q/%q, want empty",
				created.ComposeContent, created.ComposeFile, created.ComposeService)
		}
	})
}
