package projects

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/justindeelux/gotham/internal/store"
)

// scratchDSN mirrors the store integration suites: the dev database unless
// GOTHAM_TEST_DSN overrides it.
func scratchBaseDSN() string {
	if dsn := os.Getenv("GOTHAM_TEST_DSN"); dsn != "" {
		return dsn
	}
	return "postgres://gotham:gotham@localhost:5432/gotham?sslmode=disable"
}

// dsnForDatabase points a DSN at another database on the same server.
func dsnForDatabase(dsn, database string) string {
	parsed, err := url.Parse(dsn)
	if err != nil {
		return dsn
	}
	parsed.Path = "/" + database
	return parsed.String()
}

// newScratchStore creates a disposable database, migrates it to latest and
// returns a store backed by it. The database is dropped on cleanup.
func newScratchStore(t *testing.T) *store.Store {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	base := scratchBaseDSN()
	admin, err := sql.Open("pgx", dsnForDatabase(base, "postgres"))
	if err != nil {
		t.Fatalf("open maintenance connection: %v", err)
	}
	t.Cleanup(func() { _ = admin.Close() })
	if err := admin.PingContext(ctx); err != nil {
		if os.Getenv("GOTHAM_TEST_DSN") != "" {
			t.Fatalf("GOTHAM_TEST_DSN is set but Postgres is unavailable: %v", err)
		}
		t.Skipf("Postgres not available: %v", err)
	}

	scratch := fmt.Sprintf("projects_%d", time.Now().UnixNano())
	if _, err := admin.ExecContext(ctx, "CREATE DATABASE "+scratch); err != nil {
		if os.Getenv("GOTHAM_TEST_DSN") != "" {
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
		t.Fatalf("migrate scratch database: %v", err)
	}
	pool, err := store.Open(ctx, scratchDSN)
	if err != nil {
		t.Fatalf("open scratch pool: %v", err)
	}
	t.Cleanup(pool.Close)
	return store.New(pool)
}

// uuidOf converts a sqlc UUID column to the domain type.
func uuidOf(v pgtype.UUID) uuid.UUID {
	return uuid.UUID(v.Bytes)
}

// seedUser creates an account (with its personal team) and returns the
// team ID (the personal team's ID is the user's ID).
func seedUser(t *testing.T, ctx context.Context, st *store.Store, email string) (teamID uuid.UUID) {
	t.Helper()
	user, err := st.CreateUser(ctx, email, nil)
	if err != nil {
		t.Fatalf("create user %s: %v", email, err)
	}
	return uuidOf(user.ID)
}

// TestStoreRepositoryRoundtrip walks the repository against a scratch
// database: create (project plus production in one transaction), rename,
// second environment, deletes and the cascade.
func TestStoreRepositoryRoundtrip(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	st := newScratchStore(t)
	repo := newStoreRepository(st)
	teamID := seedUser(t, ctx, st, fmt.Sprintf("repo-%d@example.com", time.Now().UnixNano()))

	project, production, err := repo.CreateProject(ctx, teamID, "Shop", "storefront")
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	if production.Name != ProductionEnvironment || production.ProjectID != project.ID {
		t.Fatalf("environment = %+v, want production of the project", production)
	}

	projects, err := repo.ListProjects(ctx, teamID)
	if err != nil || len(projects) != 1 {
		t.Fatalf("ListProjects = %+v, %v; want one", projects, err)
	}
	fetched, err := repo.GetProject(ctx, teamID, project.ID)
	if err != nil || fetched.Name != "Shop" || fetched.Description != "storefront" {
		t.Fatalf("GetProject = %+v, %v", fetched, err)
	}

	staging, err := repo.CreateEnvironment(ctx, teamID, project.ID, "staging")
	if err != nil {
		t.Fatalf("CreateEnvironment: %v", err)
	}
	environments, err := repo.ListEnvironments(ctx, teamID, project.ID)
	if err != nil || len(environments) != 2 {
		t.Fatalf("ListEnvironments = %+v, %v; want two", environments, err)
	}
	if count, err := repo.CountEnvironments(ctx, project.ID); err != nil || count != 2 {
		t.Fatalf("CountEnvironments = %d, %v; want 2", count, err)
	}

	renamed, err := repo.UpdateProject(ctx, teamID, project.ID, "Store", "new blurb")
	if err != nil || renamed.Name != "Store" || renamed.Description != "new blurb" {
		t.Fatalf("UpdateProject = %+v, %v", renamed, err)
	}
	renamedEnv, err := repo.UpdateEnvironment(ctx, teamID, staging.ID, "qa")
	if err != nil || renamedEnv.Name != "qa" {
		t.Fatalf("UpdateEnvironment = %+v, %v", renamedEnv, err)
	}

	if _, err := repo.DeleteEnvironmentIfNotLast(ctx, teamID, staging.ID); err != nil {
		t.Fatalf("DeleteEnvironmentIfNotLast: %v", err)
	}
	if _, err := repo.DeleteProject(ctx, teamID, project.ID); err != nil {
		t.Fatalf("DeleteProject: %v", err)
	}
	if _, err := repo.GetProject(ctx, teamID, project.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetProject after delete = %v, want ErrNotFound", err)
	}
	if _, err := repo.GetEnvironment(ctx, teamID, production.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("production survived its project delete: %v", err)
	}
}

// TestStoreRepositoryUniquenessIsCaseInsensitive asserts the lower(name)
// indexes reject differently-cased duplicates in the same scope but allow
// the same name in another scope.
func TestStoreRepositoryUniquenessIsCaseInsensitive(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	st := newScratchStore(t)
	repo := newStoreRepository(st)
	teamA := seedUser(t, ctx, st, fmt.Sprintf("uniq-a-%d@example.com", time.Now().UnixNano()))
	teamB := seedUser(t, ctx, st, fmt.Sprintf("uniq-b-%d@example.com", time.Now().UnixNano()))

	project, _, err := repo.CreateProject(ctx, teamA, "Shop", "")
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	if _, _, err := repo.CreateProject(ctx, teamA, "SHOP", ""); !errors.Is(err, ErrProjectExists) {
		t.Fatalf("duplicate project = %v, want ErrProjectExists", err)
	}
	if _, err := repo.UpdateProject(ctx, teamA, project.ID, "sHoP", ""); err != nil {
		t.Fatalf("rename onto itself with different case = %v, want success", err)
	}
	if _, _, err := repo.CreateProject(ctx, teamB, "SHOP", ""); err != nil {
		t.Fatalf("same name in another team = %v, want success", err)
	}

	if _, err := repo.CreateEnvironment(ctx, teamA, project.ID, "PRODUCTION"); !errors.Is(err, ErrEnvironmentExists) {
		t.Fatalf("duplicate environment = %v, want ErrEnvironmentExists", err)
	}
}

// TestStoreRepositoryFailedCreateLeavesNoRows asserts the
// project-plus-production transaction is atomic: a duplicate name commits
// neither row.
func TestStoreRepositoryFailedCreateLeavesNoRows(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	st := newScratchStore(t)
	repo := newStoreRepository(st)
	teamID := seedUser(t, ctx, st, fmt.Sprintf("atomic-%d@example.com", time.Now().UnixNano()))

	if _, _, err := repo.CreateProject(ctx, teamID, "Shop", ""); err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	if _, _, err := repo.CreateProject(ctx, teamID, "SHOP", ""); !errors.Is(err, ErrProjectExists) {
		t.Fatalf("duplicate = %v, want ErrProjectExists", err)
	}
	projects, err := repo.ListProjects(ctx, teamID)
	if err != nil || len(projects) != 1 {
		t.Fatalf("ListProjects = %+v, %v; want exactly the first project", projects, err)
	}
	var envCount int
	if err := st.DB.QueryRow(ctx, "SELECT count(*) FROM environments").Scan(&envCount); err != nil {
		t.Fatalf("count environments: %v", err)
	}
	if envCount != 1 {
		t.Fatalf("environments = %d, want 1 (no orphan production)", envCount)
	}
}

// TestServiceConcurrentDeleteKeepsOneEnvironment is the F1 regression:
// two admins deleting a project's last two environments at once must leave
// exactly one behind. The survivor check and the delete run in one
// transaction that locks the parent project row, so the second delete
// observes the first. It fails on the old check-then-delete code, where both
// deletes routinely succeed and the project ends with zero environments.
func TestServiceConcurrentDeleteKeepsOneEnvironment(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	st := newScratchStore(t)
	svc := NewService(Config{Store: st, Counter: newFakeCounter(), Logger: discardLogger()})
	userID := seedUser(t, ctx, st, fmt.Sprintf("race-%d@example.com", time.Now().UnixNano()))

	const iterations = 30
	for i := 0; i < iterations; i++ {
		project, production, err := svc.CreateProject(ctx, userID, fmt.Sprintf("race-%d", i), "")
		if err != nil {
			t.Fatalf("iter %d: CreateProject: %v", i, err)
		}
		_ = project
		staging, err := svc.CreateEnvironment(ctx, userID, production.ProjectID, "staging")
		if err != nil {
			t.Fatalf("iter %d: CreateEnvironment: %v", i, err)
		}

		start := make(chan struct{})
		results := make(chan error, 2)
		for _, envID := range []uuid.UUID{production.ID, staging.ID} {
			go func(id uuid.UUID) {
				<-start
				results <- svc.DeleteEnvironment(ctx, userID, id)
			}(envID)
		}
		close(start)
		first, second := <-results, <-results

		succeeded := 0
		for _, err := range []error{first, second} {
			switch {
			case err == nil:
				succeeded++
			case errors.Is(err, ErrLastEnvironment):
			default:
				t.Fatalf("iter %d: delete = %v, want nil or ErrLastEnvironment", i, err)
			}
		}
		if succeeded != 1 {
			t.Fatalf("iter %d: %d deletes succeeded, want exactly 1", i, succeeded)
		}
		remaining, err := st.CountEnvironmentsByProject(ctx, pgUUID(production.ProjectID))
		if err != nil {
			t.Fatalf("iter %d: count: %v", i, err)
		}
		if remaining != 1 {
			t.Fatalf("iter %d: %d environments remain, want 1", i, remaining)
		}
		if err := svc.DeleteProject(ctx, userID, production.ProjectID); err != nil {
			t.Fatalf("iter %d: cleanup: %v", i, err)
		}
	}
}

// TestStoreRepositoryCreateRacesDeleteProject is the F3 regression: an
// environment created while its project is deleted must answer ErrNotFound
// (or succeed when the create wins), never a raw foreign-key error. It fails
// on the old code, where the 23503 from the lost INSERT races through to
// the caller.
func TestStoreRepositoryCreateRacesDeleteProject(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	st := newScratchStore(t)
	repo := newStoreRepository(st)
	teamID := seedUser(t, ctx, st, fmt.Sprintf("f3race-%d@example.com", time.Now().UnixNano()))

	const iterations = 60
	for i := 0; i < iterations; i++ {
		project, _, err := repo.CreateProject(ctx, teamID, fmt.Sprintf("f3-%d", i), "")
		if err != nil {
			t.Fatalf("iter %d: CreateProject: %v", i, err)
		}
		start := make(chan struct{})
		type outcome struct {
			env Environment
			err error
		}
		created := make(chan outcome, 1)
		deleted := make(chan error, 1)
		go func() {
			<-start
			env, err := repo.CreateEnvironment(ctx, teamID, project.ID, "staging")
			created <- outcome{env, err}
		}()
		go func() {
			<-start
			deleted <- func() error {
				_, err := repo.DeleteProject(ctx, teamID, project.ID)
				return err
			}()
		}()
		close(start)
		got := <-created
		if derr := <-deleted; derr != nil {
			t.Fatalf("iter %d: DeleteProject: %v", i, derr)
		}
		if got.err != nil && !errors.Is(got.err, ErrNotFound) {
			t.Fatalf("iter %d: CreateEnvironment during delete = %v, want nil or ErrNotFound", i, got.err)
		}
	}
}

// TestStoreRepositoryCrossTeamIsNotFound asserts the team filter on every
// read and delete.
func TestStoreRepositoryCrossTeamIsNotFound(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	st := newScratchStore(t)
	repo := newStoreRepository(st)
	teamA := seedUser(t, ctx, st, fmt.Sprintf("xteam-a-%d@example.com", time.Now().UnixNano()))
	teamB := seedUser(t, ctx, st, fmt.Sprintf("xteam-b-%d@example.com", time.Now().UnixNano()))

	project, production, err := repo.CreateProject(ctx, teamA, "Shop", "")
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	for name, err := range map[string]error{
		"get project":    mustErr(repo.GetProject(ctx, teamB, project.ID)),
		"update project": mustErr(repo.UpdateProject(ctx, teamB, project.ID, "x", "")),
		"delete project": mustErr(repo.DeleteProject(ctx, teamB, project.ID)),
		"get env":        mustErr(repo.GetEnvironment(ctx, teamB, production.ID)),
		"update env":     mustErr(repo.UpdateEnvironment(ctx, teamB, production.ID, "x")),
		"delete env":     mustErr(repo.DeleteEnvironmentIfNotLast(ctx, teamB, production.ID)),
		"create env":     mustErr(repo.CreateEnvironment(ctx, teamB, project.ID, "staging")),
		"list envs":      mustErr(repo.ListEnvironments(ctx, teamB, project.ID)),
	} {
		if !errors.Is(err, ErrNotFound) {
			t.Errorf("cross-team %s = %v, want ErrNotFound", name, err)
		}
	}
	if _, err := repo.GetProject(ctx, teamA, project.ID); err != nil {
		t.Errorf("own project unreadable after cross-team attempts: %v", err)
	}
}

// mustErr adapts a (T, error) call to the error for table assertions.
func mustErr[T any](_ T, err error) error { return err }
