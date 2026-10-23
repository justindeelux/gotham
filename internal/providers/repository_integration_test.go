package providers

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/store"
)

const defaultIntegrationDSN = "postgres://gotham:gotham@localhost:5432/gotham?sslmode=disable"

func integrationDSN() string {
	if dsn := os.Getenv("GOTHAM_TEST_DSN"); dsn != "" {
		return dsn
	}
	return defaultIntegrationDSN
}

func integrationDSNExplicit() bool {
	return os.Getenv("GOTHAM_TEST_DSN") != ""
}

func TestStoreRepositoryRoundtrip(t *testing.T) {
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

func TestStoreRepositoryReplaceReposIsAtomic(t *testing.T) {
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

func TestStoreRepositoryReplaceReposSerializes(t *testing.T) {
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

	st := store.New(pool)

	email := fmt.Sprintf("fx10b-serialize-%d@example.com", time.Now().UnixNano())
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

	aInSeam := make(chan struct{}, 1)
	release := make(chan struct{})
	st.BeforeRepoCacheInsert = func() error {
		aInSeam <- struct{}{}
		<-release
		return nil
	}
	t.Cleanup(func() { st.BeforeRepoCacheInsert = nil })

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := repo.ReplaceRepos(ctx, provider.ID, []Repo{{ExternalID: "a", FullName: "o/a"}}); err != nil {
			t.Errorf("A ReplaceRepos: %v", err)
		}
	}()

	<-aInSeam

	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := repo.ReplaceRepos(ctx, provider.ID, []Repo{{ExternalID: "b", FullName: "o/b"}}); err != nil {
			t.Errorf("B ReplaceRepos: %v", err)
		}
	}()

	time.Sleep(100 * time.Millisecond)
	close(release)
	wg.Wait()

	cached, err := repo.ListCachedRepos(ctx, provider.ID)
	if err != nil {
		t.Fatalf("ListCachedRepos: %v", err)
	}
	if len(cached) != 1 || cached[0].FullName != "o/b" {
		t.Fatalf("cached = %+v, want only B's replacement", cached)
	}
}

func TestStoreRepositoryDelete(t *testing.T) {
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

	st := store.New(pool)

	newUser := func(tag string) uuid.UUID {
		email := fmt.Sprintf("gs6-delete-%s-%d@example.com", tag, time.Now().UnixNano())
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
		return uuid.UUID(user.ID.Bytes)
	}

	repo := newStoreRepository(st, newSecretCipher("integration-secret"))
	userID, foreignID := newUser("owner"), newUser("foreign")
	provider, err := repo.Create(ctx, Provider{
		UserID: userID, Name: NameGitLab, BaseURL: "https://git.example",
		ClientID: "id", ClientSecret: "secret",
		AccessToken: "access", RefreshToken: "refresh",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := repo.ReplaceRepos(ctx, provider.ID, []Repo{{ExternalID: "1", FullName: "acme/demo"}}); err != nil {
		t.Fatalf("ReplaceRepos: %v", err)
	}
	foreign, err := repo.Create(ctx, Provider{
		UserID: foreignID, Name: NameGitLab, ClientID: "id", ClientSecret: "secret",
	})
	if err != nil {
		t.Fatalf("Create foreign: %v", err)
	}

	if err := repo.Delete(ctx, provider.ID, foreignID); err != nil {
		t.Fatalf("foreign Delete: %v", err)
	}
	if _, err := repo.Get(ctx, provider.ID, userID); err != nil {
		t.Fatalf("foreign delete removed the row: %v", err)
	}

	if err := repo.Delete(ctx, provider.ID, userID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := repo.Get(ctx, provider.ID, userID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get after delete = %v, want ErrNotFound", err)
	}
	if cached, err := repo.ListCachedRepos(ctx, provider.ID); err != nil || len(cached) != 0 {
		t.Fatalf("cached = %+v/%v, want the cascade to clear it", cached, err)
	}
	if err := repo.Delete(ctx, provider.ID, userID); err != nil {
		t.Fatalf("second Delete: %v", err)
	}
	if _, err := repo.Get(ctx, foreign.ID, foreignID); err != nil {
		t.Fatalf("foreign row did not survive: %v", err)
	}
}
