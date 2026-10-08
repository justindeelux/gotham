package store_test

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/justindeelux/gotham/internal/store"
	"github.com/justindeelux/gotham/internal/store/sqlc"
)

// TestApplicationImageSourceColumnsRoundtrip stores an image-source
// application and asserts the GS-9 columns round-trip: the reference, the
// registry username and the sealed password ciphertext survive create, read
// and update unchanged.
func TestApplicationImageSourceColumnsRoundtrip(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	base := testDSN()
	admin, err := sql.Open("pgx", dsnForDatabase(base, "postgres"))
	if err != nil {
		t.Fatalf("open maintenance connection: %v", err)
	}
	t.Cleanup(func() { _ = admin.Close() })
	if err := admin.PingContext(ctx); err != nil {
		if testDSNExplicit() {
			t.Fatalf("GOTHAM_TEST_DSN is set but Postgres is unavailable: %v", err)
		}
		t.Skipf("Postgres not available: %v", err)
	}

	scratch := fmt.Sprintf("gs9_image_%d", time.Now().UnixNano())
	if _, err := admin.ExecContext(ctx, "CREATE DATABASE "+scratch); err != nil {
		if testDSNExplicit() {
			t.Fatalf("GOTHAM_TEST_DSN is set but a disposable database cannot be created: %v", err)
		}
		t.Skipf("cannot create a disposable database (needs CREATEDB): %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanupCancel()
		if _, err := admin.ExecContext(cleanupCtx, "DROP DATABASE IF EXISTS "+scratch+" WITH (FORCE)"); err != nil {
			t.Logf("drop scratch database: %v", err)
		}
	})

	scratchDSN := dsnForDatabase(base, scratch)
	if err := store.Migrate(ctx, scratchDSN, store.MigrateUp); err != nil {
		t.Fatalf("migration failed: %v", err)
	}
	pool, err := store.Open(ctx, scratchDSN)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(pool.Close)
	st := store.New(pool)

	userID, teamID, envID, serverID := seedImageScope(t, ctx, st)

	created, err := st.CreateApplication(ctx, sqlc.CreateApplicationParams{
		UserID: userID, TeamID: teamID,
		ServerID: serverID, EnvironmentID: envID,
		Name: "image-app", SourceType: "image",
		ImageRef:                   "registry.example.com/team/app:1.2",
		RegistryUsername:           "robot",
		RegistryPasswordCiphertext: "sealed-credential",
		Port:                       3000,
	})
	if err != nil {
		t.Fatalf("CreateApplication: %v", err)
	}
	if created.ImageRef != "registry.example.com/team/app:1.2" ||
		created.RegistryUsername != "robot" ||
		created.RegistryPasswordCiphertext != "sealed-credential" {
		t.Errorf("created row = %+v; want the image columns stored", created)
	}

	read, err := st.GetApplication(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetApplication: %v", err)
	}
	if read.ImageRef != created.ImageRef ||
		read.RegistryUsername != created.RegistryUsername ||
		read.RegistryPasswordCiphertext != created.RegistryPasswordCiphertext {
		t.Errorf("read row = %+v; want the image columns back", read)
	}

	updated, err := st.UpdateApplication(ctx, sqlc.UpdateApplicationParams{
		ID: created.ID, Name: read.Name, Branch: read.Branch,
		BuildPack: read.BuildPack, BaseDomain: read.BaseDomain,
		Port: read.Port, HostPort: read.HostPort, ServerID: read.ServerID,
		BaseDomainDisabled: read.BaseDomainDisabled, EnvironmentID: read.EnvironmentID,
		ImageRef:                   "registry.example.com/team/app:2.0",
		RegistryUsername:           "robot",
		RegistryPasswordCiphertext: "rotated-credential",
		// The merged schema (GS-7) holds build_args NOT NULL: pass the
		// empty object like the repository's marshalBuildArgs does.
		BuildArgs: []byte("{}"),
	})
	if err != nil {
		t.Fatalf("UpdateApplication: %v", err)
	}
	if updated.ImageRef != "registry.example.com/team/app:2.0" ||
		updated.RegistryPasswordCiphertext != "rotated-credential" {
		t.Errorf("updated row = %+v; want the rotated image columns", updated)
	}
}

// seedImageScope creates the user/team/project/environment/server rows an
// application insert requires.
func seedImageScope(t *testing.T, ctx context.Context, st *store.Store) (userID, teamID, envID, serverID pgtype.UUID) {
	t.Helper()
	user, err := st.CreateUser(ctx, fmt.Sprintf("gs9-%d@example.com", time.Now().UnixNano()), nil)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	team, err := st.CreateTeam(ctx, sqlc.CreateTeamParams{
		ID:   pgtype.UUID{Bytes: uuid.New(), Valid: true},
		Name: fmt.Sprintf("gs9-%d", time.Now().UnixNano()),
	})
	if err != nil {
		t.Fatalf("CreateTeam: %v", err)
	}
	project, err := st.CreateProject(ctx, sqlc.CreateProjectParams{
		ID:     pgtype.UUID{Bytes: uuid.New(), Valid: true},
		TeamID: team.ID,
		Name:   fmt.Sprintf("gs9-%d", time.Now().UnixNano()),
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
		Name:    fmt.Sprintf("gs9-node-%d", time.Now().UnixNano()),
		Ip:      "127.0.0.1",
		Port:    22,
		SshUser: "root",
	})
	if err != nil {
		t.Fatalf("CreateServer: %v", err)
	}
	return user.ID, team.ID, environment.ID, server.ID
}
