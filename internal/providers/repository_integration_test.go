package providers

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/store"
)

// defaultIntegrationDSN points at the dev database from deploy/compose.dev.yml.
// Override with GOTHAM_TEST_DSN; a value that cannot be reached skips the test
// so CI stays green without a database.
const defaultIntegrationDSN = "postgres://gotham:gotham@localhost:5432/gotham?sslmode=disable"

// integrationDSN returns the DSN the repository integration test should use.
func integrationDSN() string {
	if dsn := os.Getenv("GOTHAM_TEST_DSN"); dsn != "" {
		return dsn
	}
	return defaultIntegrationDSN
}

// TestStoreRepositoryRoundtrip exercises the PostgreSQL adapter, including
// credential encryption at rest, against a real server. It skips when no
// database is reachable.
func TestStoreRepositoryRoundtrip(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	dsn := integrationDSN()
	if err := store.Migrate(ctx, dsn, store.MigrateUp); err != nil {
		t.Skipf("Postgres not available: %v", err)
	}
	pool, err := store.Open(ctx, dsn)
	if err != nil {
		t.Skipf("Postgres not available: %v", err)
	}
	t.Cleanup(pool.Close)

	st := store.New(pool)

	email := fmt.Sprintf("be-4.1-%d@example.com", time.Now().UnixNano())
	user, err := st.CreateUser(ctx, email, nil)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	userID := uuid.UUID(user.ID.Bytes)
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		if _, err := pool.Exec(cleanupCtx, "DELETE FROM users WHERE id = $1", user.ID); err != nil {
			t.Logf("cleanup delete: %v", err)
		}
	})

	repo := newStoreRepository(st, newSecretCipher("integration-secret"))

	created, err := repo.Create(ctx, Provider{
		UserID:       userID,
		Name:         NameGitHub,
		ClientID:     "client-id",
		ClientSecret: "client-secret",
		AccessToken:  "access-token",
		Scopes:       "repo",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.ClientSecret != "client-secret" || created.AccessToken != "access-token" {
		t.Fatalf("created credentials = %q / %q, want plaintext roundtrip", created.ClientSecret, created.AccessToken)
	}

	// The columns must hold ciphertext, not the plaintext credentials.
	var storedSecret, storedToken string
	if err := pool.QueryRow(ctx,
		"SELECT client_secret, access_token FROM providers WHERE id = $1", created.ID,
	).Scan(&storedSecret, &storedToken); err != nil {
		t.Fatalf("read raw row: %v", err)
	}
	if storedSecret == "client-secret" || storedToken == "access-token" {
		t.Fatal("credentials are stored in the clear")
	}

	got, err := repo.Get(ctx, created.ID, userID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.AccessToken != "access-token" {
		t.Errorf("Get AccessToken = %q, want access-token", got.AccessToken)
	}
	if _, err := repo.Get(ctx, created.ID, uuid.New()); err != ErrNotFound {
		t.Errorf("Get other user error = %v, want ErrNotFound", err)
	}

	list, err := repo.List(ctx, userID)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 1 || list[0].ID != created.ID {
		t.Fatalf("List = %+v", list)
	}

	expiry := time.Now().Add(time.Hour).UTC().Truncate(time.Millisecond)
	updated, err := repo.UpdateToken(ctx, created.ID, "new-access", "new-refresh", &expiry)
	if err != nil {
		t.Fatalf("UpdateToken: %v", err)
	}
	if updated.AccessToken != "new-access" || updated.RefreshToken != "new-refresh" {
		t.Errorf("updated tokens = %q / %q", updated.AccessToken, updated.RefreshToken)
	}
	if updated.TokenExpiresAt == nil {
		t.Error("TokenExpiresAt is nil, want set")
	}

	repos := []Repo{
		{ExternalID: "1", Name: "gotham", FullName: "o/gotham", Private: true, DefaultBranch: "main"},
		{ExternalID: "2", Name: "docs", FullName: "o/docs"},
	}
	if err := repo.ReplaceRepos(ctx, created.ID, repos); err != nil {
		t.Fatalf("ReplaceRepos: %v", err)
	}
	if err := repo.ReplaceRepos(ctx, created.ID, repos[:1]); err != nil {
		t.Fatalf("ReplaceRepos (shrink): %v", err)
	}
	cached, err := repo.ListCachedRepos(ctx, created.ID)
	if err != nil {
		t.Fatalf("ListCachedRepos: %v", err)
	}
	if len(cached) != 1 || cached[0].FullName != "o/gotham" || !cached[0].Private {
		t.Fatalf("ListCachedRepos = %+v", cached)
	}
}

// TestStoreRepositoryReplaceReposIsAtomic is the C1-9 regression: a refresh
// that fails after clearing the cache must roll back, leaving the previous
// complete list intact instead of an empty or partial one.
func TestStoreRepositoryReplaceReposIsAtomic(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	dsn := integrationDSN()
	if err := store.Migrate(ctx, dsn, store.MigrateUp); err != nil {
		t.Skipf("Postgres not available: %v", err)
	}
	pool, err := store.Open(ctx, dsn)
	if err != nil {
		t.Skipf("Postgres not available: %v", err)
	}
	t.Cleanup(pool.Close)

	st := store.New(pool)

	email := fmt.Sprintf("fx10b-cache-%d@example.com", time.Now().UnixNano())
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

	repo := newStoreRepository(st, newSecretCipher("integration-secret"))
	provider, err := repo.Create(ctx, Provider{
		UserID: uuid.UUID(user.ID.Bytes), Name: NameGitHub,
		ClientID: "client-id", ClientSecret: "client-secret",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	first := []Repo{{ExternalID: "1", FullName: "o/one"}, {ExternalID: "2", FullName: "o/two"}}
	if err := repo.ReplaceRepos(ctx, provider.ID, first); err != nil {
		t.Fatalf("ReplaceRepos: %v", err)
	}

	// Force a failure after the clear; the transaction must roll back.
	st.BeforeRepoCacheInsert = func() error { return errors.New("boom") }
	if err := repo.ReplaceRepos(ctx, provider.ID, []Repo{{ExternalID: "3", FullName: "o/three"}}); err == nil {
		t.Fatal("ReplaceRepos with a failing insert: no error, want rollback")
	}
	st.BeforeRepoCacheInsert = nil

	cached, err := repo.ListCachedRepos(ctx, provider.ID)
	if err != nil {
		t.Fatalf("ListCachedRepos: %v", err)
	}
	if len(cached) != 2 {
		t.Fatalf("cached = %+v, want the previous complete list of 2", cached)
	}
}
