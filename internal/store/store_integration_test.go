package store_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/justindeelux/gotham/internal/store"
	"github.com/justindeelux/gotham/internal/store/sqlc"
)

// defaultTestDSN points at the dev database from deploy/compose.dev.yml. Override
// with GOTHAM_TEST_DSN, e.g. to force a skip in CI with
// GOTHAM_TEST_DSN=postgres://nope.
const defaultTestDSN = "postgres://gotham:gotham@localhost:5432/gotham?sslmode=disable"

func testDSN() string {
	if dsn := os.Getenv("GOTHAM_TEST_DSN"); dsn != "" {
		return dsn
	}
	return defaultTestDSN
}

// testDSNExplicit reports whether the operator opted in by setting
// GOTHAM_TEST_DSN: an explicit opt-in turns a missing database into a failure
// instead of a skip.
func testDSNExplicit() bool {
	return os.Getenv("GOTHAM_TEST_DSN") != ""
}

// TestStoreUserRoundtrip runs the embedded migrations and verifies an insert and
// fetch roundtrip against a real PostgreSQL server. It skips when no database is
// reachable so CI stays green without one.
func TestStoreUserRoundtrip(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	dsn := testDSN()

	// ProbeOnce fails fast when no database is listening, sparing Open's
	// retry loop; an Open failure past a good probe is a real error.
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
	// Registered before the row cleanup so LIFO order closes the pool last.
	t.Cleanup(pool.Close)
	// Open proved the database is reachable, so a migration error is a real
	// failure, never a skip (D1-12).
	if err := store.Migrate(ctx, dsn, store.MigrateUp); err != nil {
		t.Fatalf("migration failed: %v", err)
	}

	s := store.New(pool)

	email := fmt.Sprintf("be-0.3-%d@example.com", time.Now().UnixNano())
	created, err := s.CreateUser(ctx, email, nil)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		if _, err := pool.Exec(cleanupCtx, "DELETE FROM users WHERE id = $1", created.ID); err != nil {
			t.Logf("cleanup delete: %v", err)
		}
	})

	fetched, err := s.GetUserByEmail(ctx, email)
	if err != nil {
		t.Fatalf("GetUserByEmail: %v", err)
	}
	if fetched.ID != created.ID {
		t.Fatalf("id mismatch: got %v, want %v", fetched.ID, created.ID)
	}
	if fetched.Email != email {
		t.Fatalf("email mismatch: got %q, want %q", fetched.Email, email)
	}
	if !fetched.CreatedAt.Valid || fetched.CreatedAt.Time.IsZero() {
		t.Fatalf("expected created_at to be set, got %+v", fetched.CreatedAt)
	}

	if _, err := s.GetUserByEmail(ctx, "missing-"+email); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("expected pgx.ErrNoRows for missing user, got %v", err)
	}
}

// TestStoreReplaceApplicationEnvIsAtomic is the item-2 store regression: two
// concurrent replacements of the same collection must serialize, so each
// commits its own full replacement rather than both inserting disjoint keys
// (a union where a replace was asked for). It skips when no database is
// reachable.
func TestStoreReplaceApplicationEnvIsAtomic(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	dsn := testDSN()
	// ProbeOnce fails fast when no database is listening, sparing Open's
	// retry loop; an Open failure past a good probe is a real error.
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
	// Open proved the database is reachable, so a migration error is a real
	// failure, never a skip (D1-12).
	if err := store.Migrate(ctx, dsn, store.MigrateUp); err != nil {
		t.Fatalf("migration failed: %v", err)
	}

	st := store.New(pool)
	email := fmt.Sprintf("fx-6b-replace-%d@example.com", time.Now().UnixNano())
	user, err := st.CreateUser(ctx, email, nil)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		if _, err := pool.Exec(cleanupCtx, "DELETE FROM users WHERE id = $1", user.ID); err != nil {
			t.Logf("cleanup delete: %v", err)
		}
	})

	teamID, envID, serverID := seedEnvColumns(t, ctx, st, pool)

	app, err := st.CreateApplication(ctx, sqlc.CreateApplicationParams{
		UserID: user.ID, TeamID: teamID, ServerID: serverID, EnvironmentID: envID,
		Name:     "replace-race-app",
		Provider: "github",
		Repo:     "acme/demo",
		CloneUrl: "https://github.com/acme/demo.git",
		Branch:   "main",
	})
	if err != nil {
		t.Fatalf("CreateApplication: %v", err)
	}

	// Start from empty and race two full replacements. The seam holds the
	// first transaction open inside its clear/insert window so the second must
	// either serialize on the parent row lock (fixed) or also enter and insert
	// (buggy), committing a union of the two disjoint sets.
	release := make(chan struct{})
	entered := make(chan struct{}, 2)
	st.BeforeCollectionClear = func() {
		entered <- struct{}{}
		<-release
	}

	a := []sqlc.InsertEnvVarParams{{Key: "RA", Value: "1"}}
	b := []sqlc.InsertEnvVarParams{{Key: "RB", Value: "1"}}
	errs := make(chan error, 2)
	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		errs <- st.ReplaceApplicationEnv(ctx, app.ID, a, nil)
	}()
	<-entered // the first transaction is inside its window

	wg.Add(1)
	go func() {
		defer wg.Done()
		errs <- st.ReplaceApplicationEnv(ctx, app.ID, b, nil)
	}()

	// With the parent lock the second transaction is blocked before the seam;
	// without it, it enters the seam too. Wait a bounded time for the second
	// entry (the buggy path) and then release whichever way it went.
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
	}
	close(release)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("ReplaceApplicationEnv: %v", err)
		}
	}

	stored, err := st.ListEnvVarsByApp(ctx, app.ID)
	if err != nil {
		t.Fatalf("ListEnvVarsByApp: %v", err)
	}
	if len(stored) != 1 {
		keys := make([]string, 0, len(stored))
		for _, row := range stored {
			keys = append(keys, row.Key)
		}
		t.Fatalf("stored env vars = %v, want exactly one collection (no merged union)", keys)
	}
}

// TestStoreReplaceApplicationStoragesIsAtomic is the storages half of the
// item-2 regression (same pattern, see ReplaceApplicationEnv).
func TestStoreReplaceApplicationStoragesIsAtomic(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	dsn := testDSN()
	// ProbeOnce fails fast when no database is listening, sparing Open's
	// retry loop; an Open failure past a good probe is a real error.
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
	// Open proved the database is reachable, so a migration error is a real
	// failure, never a skip (D1-12).
	if err := store.Migrate(ctx, dsn, store.MigrateUp); err != nil {
		t.Fatalf("migration failed: %v", err)
	}

	st := store.New(pool)
	email := fmt.Sprintf("fx-6b-storage-%d@example.com", time.Now().UnixNano())
	user, err := st.CreateUser(ctx, email, nil)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		if _, err := pool.Exec(cleanupCtx, "DELETE FROM users WHERE id = $1", user.ID); err != nil {
			t.Logf("cleanup delete: %v", err)
		}
	})

	teamID, envID, serverID := seedEnvColumns(t, ctx, st, pool)

	app, err := st.CreateApplication(ctx, sqlc.CreateApplicationParams{
		UserID: user.ID, TeamID: teamID, ServerID: serverID, EnvironmentID: envID,
		Name:     "storage-race-app",
		Provider: "github",
		Repo:     "acme/demo",
		CloneUrl: "https://github.com/acme/demo.git",
		Branch:   "main",
	})
	if err != nil {
		t.Fatalf("CreateApplication: %v", err)
	}

	a := []sqlc.InsertStorageParams{{Name: "vol-a", ContainerPath: "/data/a"}}
	b := []sqlc.InsertStorageParams{{Name: "vol-b", ContainerPath: "/data/b"}}
	release := make(chan struct{})
	entered := make(chan struct{}, 2)
	st.BeforeCollectionClear = func() {
		entered <- struct{}{}
		<-release
	}
	errs := make(chan error, 2)
	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		errs <- st.ReplaceApplicationStorages(ctx, app.ID, a)
	}()
	<-entered

	wg.Add(1)
	go func() {
		defer wg.Done()
		errs <- st.ReplaceApplicationStorages(ctx, app.ID, b)
	}()

	select {
	case <-entered:
	case <-time.After(2 * time.Second):
	}
	close(release)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("ReplaceApplicationStorages: %v", err)
		}
	}

	stored, err := st.ListStoragesByApp(ctx, app.ID)
	if err != nil {
		t.Fatalf("ListStoragesByApp: %v", err)
	}
	if len(stored) != 1 {
		names := make([]string, 0, len(stored))
		for _, row := range stored {
			names = append(names, row.Name)
		}
		t.Fatalf("stored storages = %v, want exactly one collection (no merged union)", names)
	}
}

// TestStoreListEnvConfigIsOneSnapshot is the item-1 read regression: the plain
// vars and secrets must come from one transaction snapshot. A replacement
// commits between the two reads; with the single-snapshot read both queries
// still observe the pre-replacement set (no key dropped), where two separate
// autocommit reads would pair the old plain vars with the new secrets.
func TestStoreListEnvConfigIsOneSnapshot(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	dsn := testDSN()
	// ProbeOnce fails fast when no database is listening, sparing Open's
	// retry loop; an Open failure past a good probe is a real error.
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
	// Open proved the database is reachable, so a migration error is a real
	// failure, never a skip (D1-12).
	if err := store.Migrate(ctx, dsn, store.MigrateUp); err != nil {
		t.Fatalf("migration failed: %v", err)
	}

	st := store.New(pool)
	email := fmt.Sprintf("fx-6b-snapshot-%d@example.com", time.Now().UnixNano())
	user, err := st.CreateUser(ctx, email, nil)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		if _, err := pool.Exec(cleanupCtx, "DELETE FROM users WHERE id = $1", user.ID); err != nil {
			t.Logf("cleanup delete: %v", err)
		}
	})

	teamID, envID, serverID := seedEnvColumns(t, ctx, st, pool)

	app, err := st.CreateApplication(ctx, sqlc.CreateApplicationParams{
		UserID: user.ID, TeamID: teamID, ServerID: serverID, EnvironmentID: envID,
		Name: "snapshot-app", Provider: "github",
		Repo: "acme/demo", CloneUrl: "https://github.com/acme/demo.git", Branch: "main",
	})
	if err != nil {
		t.Fatalf("CreateApplication: %v", err)
	}
	appID := app.ID

	// Initial: plain OLD_PLAIN, secret OLD_SECRET.
	if err := st.ReplaceApplicationEnv(ctx, appID,
		[]sqlc.InsertEnvVarParams{{Key: "OLD_PLAIN", Value: "1"}},
		[]sqlc.InsertSecretParams{{ID: pgUUID(uuid.New()), Key: "OLD_SECRET", Ciphertext: "old"}},
	); err != nil {
		t.Fatalf("seed env: %v", err)
	}

	// The seam commits a full replacement (new plain + new secret) between the
	// two reads of ListEnvConfigByApp.
	var seamFired int
	st.AfterEnvReadBeforeSecrets = func() {
		seamFired++
		if err := st.ReplaceApplicationEnv(ctx, appID,
			[]sqlc.InsertEnvVarParams{{Key: "NEW_PLAIN", Value: "1"}},
			[]sqlc.InsertSecretParams{{ID: pgUUID(uuid.New()), Key: "NEW_SECRET", Ciphertext: "new"}},
		); err != nil {
			t.Errorf("mid-read replacement: %v", err)
		}
	}

	envVars, secrets, err := st.ListEnvConfigByApp(ctx, appID)
	if err != nil {
		t.Fatalf("ListEnvConfigByApp: %v", err)
	}
	if seamFired == 0 {
		t.Fatalf("seam never fired; the race was not exercised")
	}

	envKeys := keysOf(envVars)
	secretKeys := secretKeysOf(secrets)
	// Both reads must return their collection: a vacuous comparison below would
	// pass on an empty read, which is exactly the failure mode under test.
	if len(envKeys) != 1 || len(secretKeys) != 1 {
		t.Fatalf("read env %v secrets %v, want exactly one of each", envKeys, secretKeys)
	}
	// Both reads must be the same snapshot: either the old pair or the new
	// pair, never old plain with new secret.
	if len(envKeys) == 1 && envKeys[0] == "OLD_PLAIN" {
		if len(secretKeys) != 1 || secretKeys[0] != "OLD_SECRET" {
			t.Errorf("mixed snapshot: plain %v paired with secrets %v, want the pre-replacement pair", envKeys, secretKeys)
		}
	}
	if len(envKeys) == 1 && envKeys[0] == "NEW_PLAIN" {
		if len(secretKeys) != 1 || secretKeys[0] != "NEW_SECRET" {
			t.Errorf("mixed snapshot: plain %v paired with secrets %v, want the post-replacement pair", envKeys, secretKeys)
		}
	}
}

// pgUUID converts a domain UUID into the pgtype form the sqlc params take.
func pgUUID(id uuid.UUID) pgtype.UUID {
	return pgtype.UUID{Bytes: id, Valid: true}
}

// seedEnvColumns creates a team, project, environment and server for tests
// that insert resource rows directly (environment_id and server_id are NOT
// NULL since PE-2). Cleanup removes the resources first so the RESTRICT
// references never block the team and server deletes.
func seedEnvColumns(t *testing.T, ctx context.Context, st *store.Store, pool *pgxpool.Pool) (teamID, envID, serverID pgtype.UUID) {
	t.Helper()
	suffix := time.Now().UnixNano()
	team, err := st.CreateTeam(ctx, sqlc.CreateTeamParams{
		ID:   pgUUID(uuid.New()),
		Name: fmt.Sprintf("store-seed-%d", suffix),
	})
	if err != nil {
		t.Fatalf("CreateTeam: %v", err)
	}
	project, err := st.CreateProject(ctx, sqlc.CreateProjectParams{
		ID:     pgUUID(uuid.New()),
		TeamID: team.ID,
		Name:   fmt.Sprintf("store-seed-%d", suffix),
	})
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	environment, err := st.CreateEnvironment(ctx, sqlc.CreateEnvironmentParams{
		ID:        pgUUID(uuid.New()),
		ProjectID: project.ID,
		Name:      "production",
	})
	if err != nil {
		t.Fatalf("CreateEnvironment: %v", err)
	}
	server, err := st.CreateServer(ctx, sqlc.CreateServerParams{
		Name:    fmt.Sprintf("store-seed-node-%d", suffix),
		Ip:      "127.0.0.1",
		Port:    22,
		SshUser: "root",
	})
	if err != nil {
		t.Fatalf("CreateServer: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		for _, query := range []string{
			"DELETE FROM applications WHERE environment_id = $1",
			"DELETE FROM services WHERE environment_id = $1",
			"DELETE FROM databases WHERE environment_id = $1",
		} {
			if _, err := pool.Exec(cleanupCtx, query, environment.ID); err != nil {
				t.Logf("cleanup resources: %v", err)
			}
		}
		if _, err := pool.Exec(cleanupCtx, "DELETE FROM teams WHERE id = $1", team.ID); err != nil {
			t.Logf("cleanup team: %v", err)
		}
		if _, err := pool.Exec(cleanupCtx, "DELETE FROM servers WHERE id = $1", server.ID); err != nil {
			t.Logf("cleanup server: %v", err)
		}
	})
	return team.ID, environment.ID, server.ID
}

// keysOf returns the sorted env-var keys.
func keysOf(rows []sqlc.EnvVar) []string {
	keys := make([]string, 0, len(rows))
	for _, row := range rows {
		keys = append(keys, row.Key)
	}
	sort.Strings(keys)
	return keys
}

// secretKeysOf returns the sorted secret keys.
func secretKeysOf(rows []sqlc.Secret) []string {
	keys := make([]string, 0, len(rows))
	for _, row := range rows {
		keys = append(keys, row.Key)
	}
	sort.Strings(keys)
	return keys
}

// TestStoreCreateFirstUserSerializes proves the first-account guard is atomic:
// concurrent bootstraps with different emails must yield exactly one account.
// It runs on a private scratch database (the bootstrap precondition), so it
// neither skips nor touches the shared database.
func TestStoreCreateFirstUserSerializes(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	st := scratchStore(t)

	const attempts = 8
	type result struct {
		user sqlc.User
		err  error
	}
	var wg sync.WaitGroup
	results := make(chan result, attempts)
	for i := 0; i < attempts; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			email := fmt.Sprintf("first-user-race-%d-%d@example.com", time.Now().UnixNano(), i)
			user, err := st.CreateFirstUser(ctx, email, nil)
			results <- result{user: user, err: err}
		}(i)
	}
	wg.Wait()
	close(results)

	wins := 0
	var winner sqlc.User
	for res := range results {
		switch {
		case res.err == nil:
			wins++
			winner = res.user
		case errors.Is(res.err, store.ErrInstanceHasAccount):
		default:
			t.Fatalf("unexpected error: %v", res.err)
		}
	}
	if wins != 1 {
		t.Fatalf("concurrent bootstraps succeeded %d times, want exactly 1", wins)
	}
	if !winner.IsPlatformAdmin {
		t.Fatal("bootstrap race winner IsPlatformAdmin = false, want true (JUS-21)")
	}

	// Delete exactly the row this test created.
	if err := st.DeleteUserAndPersonalTeam(ctx, winner.ID); err != nil {
		t.Fatalf("cleanup winner: %v", err)
	}
}

// TestStoreRevokeSessionIfLive proves the rotation guard: a second revoke of
// the same refresh hash reports false, so a refresh racing a password reset
// cannot mint a replacement session.
func TestStoreRevokeSessionIfLive(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Reachability first: CI has no database, so an unreachable DSN must skip
	// (the opt-in GOTHAM_TEST_DSN turns a missing database into a failure).
	// ProbeOnce fails fast when no database is listening, sparing Open's
	// retry loop; an Open failure past a good probe is a real error.
	if err := store.ProbeOnce(ctx, testDSN()); err != nil {
		if testDSNExplicit() {
			t.Fatalf("open store: %v", err)
		}
		t.Skipf("no database: %v", err)
	}
	pool, err := store.Open(ctx, testDSN())
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer pool.Close()

	if err := store.Migrate(ctx, testDSN(), store.MigrateUp); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	st := store.New(pool)
	user, err := st.CreateUser(ctx, fmt.Sprintf("revoke-live-%d@example.com", time.Now().UnixNano()), nil)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	// Delete before the deferred pool.Close: a t.Cleanup callback would run
	// after it and the delete would hit a closed pool.
	defer func() { _ = st.DeleteUserAndPersonalTeam(context.WithoutCancel(ctx), user.ID) }()

	session, err := st.CreateSession(ctx, sqlc.CreateSessionParams{
		UserID:            user.ID,
		RefreshHash:       fmt.Sprintf("hash-%d", time.Now().UnixNano()),
		ExpiresAt:         pgtype.Timestamptz{Time: time.Now().Add(time.Hour), Valid: true},
		CredentialVersion: user.CredentialVersion,
	})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	live, err := st.RevokeSessionIfLive(ctx, session.RefreshHash)
	if err != nil || !live {
		t.Fatalf("first revoke = (%v, %v), want (true, nil)", live, err)
	}
	live, err = st.RevokeSessionIfLive(ctx, session.RefreshHash)
	if err != nil {
		t.Fatalf("second revoke error: %v", err)
	}
	if live {
		t.Fatal("second revoke reported a live session; the rotation race is open")
	}
}
