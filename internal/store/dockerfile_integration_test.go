package store_test

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/justindeelux/gotham/internal/store"
	"github.com/justindeelux/gotham/internal/store/sqlc"
)

// TestStoreApplicationDockerfileRoundtrip runs the embedded migrations and
// verifies the GS-7 columns through Postgres: pasted text and --build-arg
// pairs round-trip on write and update, and direct sqlc callers that predate
// the columns get the COALESCE defaults (” and '{}', never NULL).
func TestStoreApplicationDockerfileRoundtrip(t *testing.T) {
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

	email := fmt.Sprintf("gs7-%d@example.com", time.Now().UnixNano())
	user, err := st.CreateUser(ctx, email, nil)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	team, err := st.CreateTeam(ctx, sqlc.CreateTeamParams{
		ID:   pgtype.UUID{Bytes: uuid.New(), Valid: true},
		Name: fmt.Sprintf("gs7-%d", time.Now().UnixNano()),
	})
	if err != nil {
		t.Fatalf("CreateTeam: %v", err)
	}
	project, err := st.CreateProject(ctx, sqlc.CreateProjectParams{
		ID:     pgtype.UUID{Bytes: uuid.New(), Valid: true},
		TeamID: team.ID,
		Name:   fmt.Sprintf("gs7-%d", time.Now().UnixNano()),
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
		Name:    fmt.Sprintf("gs7-node-%d", time.Now().UnixNano()),
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
			SourceType:    "dockerfile",
			Branch:        "main",
			BuildPack:     "dockerfile",
		}
	}

	t.Run("columns round-trip", func(t *testing.T) {
		params := newParams(fmt.Sprintf("gs7-app-%d", time.Now().UnixNano()))
		params.DockerfileContent = "FROM alpine:3.20\n"
		params.BuildArgs = []byte(`{"APP_ENV":"production"}`)
		created, err := st.CreateApplication(ctx, params)
		if err != nil {
			t.Fatalf("CreateApplication: %v", err)
		}
		fetched, err := st.GetApplication(ctx, created.ID)
		if err != nil {
			t.Fatalf("GetApplication: %v", err)
		}
		if fetched.DockerfileContent != "FROM alpine:3.20\n" {
			t.Errorf("content = %q, want the pasted text", fetched.DockerfileContent)
		}
		if args := decodeBuildArgs(t, fetched.BuildArgs); args["APP_ENV"] != "production" {
			t.Errorf("build args = %s, want APP_ENV=production", fetched.BuildArgs)
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
			DockerfileContent:  "FROM alpine:3.21\n",
			BuildArgs:          []byte(`{"APP_ENV":"staging"}`),
		})
		if err != nil {
			t.Fatalf("UpdateApplication: %v", err)
		}
		if updated.DockerfileContent != "FROM alpine:3.21\n" {
			t.Errorf("updated content = %q, want the new text", updated.DockerfileContent)
		}
		if args := decodeBuildArgs(t, updated.BuildArgs); args["APP_ENV"] != "staging" {
			t.Errorf("updated build args = %s, want APP_ENV=staging", updated.BuildArgs)
		}
	})

	t.Run("zero values default instead of NULL", func(t *testing.T) {
		created, err := st.CreateApplication(ctx, newParams(fmt.Sprintf("gs7-legacy-%d", time.Now().UnixNano())))
		if err != nil {
			t.Fatalf("CreateApplication: %v", err)
		}
		if created.DockerfileContent != "" {
			t.Errorf("content = %q, want empty", created.DockerfileContent)
		}
		if args := decodeBuildArgs(t, created.BuildArgs); len(args) != 0 {
			t.Errorf("build args = %s, want {}", created.BuildArgs)
		}
	})
}

// decodeBuildArgs parses a build_args jsonb value for comparison (Postgres
// normalizes whitespace, so string equality is too strict).
func decodeBuildArgs(t *testing.T, data []byte) map[string]string {
	t.Helper()
	var args map[string]string
	if err := json.Unmarshal(data, &args); err != nil {
		t.Fatalf("decode build args %q: %v", data, err)
	}
	return args
}
