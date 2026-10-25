package deploy

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/justindeelux/gotham/internal/providers"
	"github.com/justindeelux/gotham/internal/servers"
	"github.com/justindeelux/gotham/internal/store"
	"github.com/justindeelux/gotham/internal/store/sqlc"
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

func TestStoreRepositoryDeployKeyRoundtrip(t *testing.T) {
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

	email := fmt.Sprintf("be-4.4b-%d@example.com", time.Now().UnixNano())
	user, err := st.CreateUser(ctx, email, nil)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	userID := uuid.UUID(user.ID.Bytes)
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		if _, err := pool.Exec(cleanupCtx, "DELETE FROM users WHERE id = $1", user.ID); err != nil {
			t.Logf("cleanup delete user: %v", err)
		}
	})

	envID, serverID := seedProjectEnvironment(t, ctx, st)

	app, err := st.CreateApplication(ctx, sqlc.CreateApplicationParams{
		UserID:        pgUUID(userID),
		ServerID:      pgUUID(serverID),
		EnvironmentID: pgUUID(envID),
		Name:          "deploy-key-app",
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

	if pem, err := repo.DeployKeyPrivatePEM(ctx, appID); err != nil || pem != "" {
		t.Fatalf("DeployKeyPrivatePEM = %q, %v; want empty, nil", pem, err)
	}
	if _, err := repo.GetDeployKey(ctx, appID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetDeployKey error = %v, want ErrNotFound", err)
	}

	privatePEM, publicKey, fingerprint, err := generateDeployKeyPair("gotham:deploy:integration")
	if err != nil {
		t.Fatalf("generateDeployKeyPair: %v", err)
	}
	created, err := repo.CreateDeployKey(ctx, DeployKey{
		ApplicationID: appID,
		Provider:      "github",
		Repo:          "acme/demo",
		ProviderKeyID: "77",
		Fingerprint:   fingerprint,
		PublicKey:     publicKey,
	}, privatePEM)
	if err != nil {
		t.Fatalf("CreateDeployKey: %v", err)
	}
	if created.ID == uuid.Nil || created.PrivateKeyID == uuid.Nil {
		t.Fatalf("created key = %+v, want both ids set", created)
	}
	if created.Fingerprint != fingerprint || created.PublicKey != publicKey {
		t.Errorf("created key = %+v, want the fingerprint and public key stored", created)
	}

	mapping, err := st.GetApplicationDeployKey(ctx, pgUUID(appID))
	if err != nil {
		t.Fatalf("GetApplicationDeployKey: %v", err)
	}
	sealed, err := st.GetPrivateKeyByID(ctx, mapping.PrivateKeyID)
	if err != nil {
		t.Fatalf("GetPrivateKeyByID: %v", err)
	}
	if strings.Contains(sealed.EncryptedKey, "BEGIN OPENSSH") {
		t.Fatal("the private key is stored in the clear")
	}
	opened, err := servers.DecryptKey(sealed.EncryptedKey, secret)
	if err != nil {
		t.Fatalf("servers.DecryptKey: %v", err)
	}
	if opened != privatePEM {
		t.Error("servers.DecryptKey returned a different private key")
	}
	if pem, err := repo.DeployKeyPrivatePEM(ctx, appID); err != nil || pem != privatePEM {
		t.Fatalf("DeployKeyPrivatePEM = %q, %v; want the stored key", pem, err)
	}

	got, err := repo.GetDeployKey(ctx, appID)
	if err != nil {
		t.Fatalf("GetDeployKey: %v", err)
	}
	if got.ID != created.ID || got.ProviderKeyID != "77" || got.ApplicationID != appID {
		t.Errorf("key = %+v, want the stored mapping", got)
	}

	if _, err := repo.CreateDeployKey(ctx, DeployKey{
		ApplicationID: appID,
		Provider:      "github",
		Repo:          "acme/demo",
		Fingerprint:   "SHA256:second",
		PublicKey:     "ssh-ed25519 AAAA second",
	}, privatePEM); err == nil {
		t.Fatal("second CreateDeployKey: no error, want the unique index to refuse it")
	}
	var orphanCheck int
	if err := pool.QueryRow(ctx,
		"SELECT COUNT(*) FROM private_keys WHERE name = $1", deployKeyRowName(appID),
	).Scan(&orphanCheck); err != nil {
		t.Fatalf("count private keys: %v", err)
	}
	if orphanCheck != 1 {
		t.Errorf("private_keys rows = %d, want 1 (the failed insert must roll back)", orphanCheck)
	}

	removed, err := repo.DeleteDeployKey(ctx, got)
	if err != nil {
		t.Fatalf("DeleteDeployKey: %v", err)
	}
	if removed.ID != created.ID {
		t.Errorf("removed = %+v, want the stored mapping", removed)
	}
	if _, err := repo.DeleteDeployKey(ctx, got); !errors.Is(err, ErrNotFound) {
		t.Errorf("second delete error = %v, want ErrNotFound", err)
	}
	if pem, err := repo.DeployKeyPrivatePEM(ctx, appID); err != nil || pem != "" {
		t.Errorf("DeployKeyPrivatePEM after delete = %q, %v; want empty", pem, err)
	}
	var remaining int
	if err := pool.QueryRow(ctx,
		"SELECT COUNT(*) FROM private_keys WHERE name = $1", deployKeyRowName(appID),
	).Scan(&remaining); err != nil {
		t.Fatalf("count private keys: %v", err)
	}
	if remaining != 0 {
		t.Errorf("private_keys rows after delete = %d, want 0", remaining)
	}
}

func TestStoreRepositoryDeleteDeployKeyFence(t *testing.T) {
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

	email := fmt.Sprintf("c3-6-fence-%d@example.com", time.Now().UnixNano())
	user, err := st.CreateUser(ctx, email, nil)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	userID := uuid.UUID(user.ID.Bytes)
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		if _, err := pool.Exec(cleanupCtx, "DELETE FROM users WHERE id = $1", user.ID); err != nil {
			t.Logf("cleanup delete user: %v", err)
		}
	})

	envID, serverID := seedProjectEnvironment(t, ctx, st)

	app, err := st.CreateApplication(ctx, sqlc.CreateApplicationParams{
		UserID:        pgUUID(userID),
		ServerID:      pgUUID(serverID),
		EnvironmentID: pgUUID(envID),
		Name:          "fence-app",
		Provider:      "github",
		Repo:          "acme/fence",
		CloneUrl:      "https://github.com/acme/fence.git",
		Branch:        "main",
		BuildPack:     "dockerfile",
	})
	if err != nil {
		t.Fatalf("CreateApplication: %v", err)
	}
	appID := uuid.UUID(app.ID.Bytes)
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		if _, err := pool.Exec(cleanupCtx, "DELETE FROM private_keys WHERE name = $1", deployKeyRowName(appID)); err != nil {
			t.Logf("cleanup delete private keys: %v", err)
		}
	})

	privatePEM, publicKey, fingerprint, err := generateDeployKeyPair("gotham:deploy:fence")
	if err != nil {
		t.Fatalf("generateDeployKeyPair: %v", err)
	}
	key1, err := repo.CreateDeployKey(ctx, DeployKey{
		ApplicationID: appID, Provider: "github", Repo: "acme/fence",
		ProviderKeyID: "1", Fingerprint: fingerprint, PublicKey: publicKey,
	}, privatePEM)
	if err != nil {
		t.Fatalf("CreateDeployKey(key1): %v", err)
	}

	read1, err := repo.GetDeployKey(ctx, appID)
	if err != nil || read1.ID != key1.ID {
		t.Fatalf("GetDeployKey = %+v / %v, want key1", read1, err)
	}
	if _, err := repo.DeleteDeployKey(ctx, read1); err != nil {
		t.Fatalf("DeleteDeployKey(key1): %v", err)
	}
	key2, err := repo.CreateDeployKey(ctx, DeployKey{
		ApplicationID: appID, Provider: "github", Repo: "acme/fence",
		ProviderKeyID: "2", Fingerprint: fingerprint, PublicKey: publicKey,
	}, privatePEM)
	if err != nil {
		t.Fatalf("CreateDeployKey(key2): %v", err)
	}

	if _, err := repo.DeleteDeployKey(ctx, read1); !errors.Is(err, ErrNotFound) {
		t.Fatalf("stale delete error = %v, want ErrNotFound", err)
	}
	stored, err := repo.GetDeployKey(ctx, appID)
	if err != nil {
		t.Fatalf("replacement vanished: %v", err)
	}
	if stored.ID != key2.ID {
		t.Errorf("stored key = %s, want the replacement %s", stored.ID, key2.ID)
	}
	if pem, err := repo.DeployKeyPrivatePEM(ctx, appID); err != nil || pem != privatePEM {
		t.Errorf("replacement private key = %q / %v, want it intact", pem, err)
	}
}

func TestListApplicationsByEnvironmentPreviewsFlag(t *testing.T) {
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
	repo := newStoreRepository(st, "integration-secret")
	envID, serverID := seedProjectEnvironment(t, ctx, st)

	user, err := st.CreateUser(ctx, fmt.Sprintf("preview-flag-%d@example.com", time.Now().UnixNano()), nil)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	userID := uuid.UUID(user.ID.Bytes)
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		if _, err := pool.Exec(cleanupCtx, "DELETE FROM users WHERE id = $1", user.ID); err != nil {
			t.Logf("cleanup delete user: %v", err)
		}
	})

	mkApp := func(name string, preview bool) {
		t.Helper()
		if _, err := st.CreateApplication(ctx, sqlc.CreateApplicationParams{
			UserID: pgUUID(userID), ServerID: pgUUID(serverID), EnvironmentID: pgUUID(envID),
			Name: name, CloneUrl: "https://github.com/acme/demo.git",
			Branch: "main", BuildPack: "dockerfile", IsPreview: preview,
		}); err != nil {
			t.Fatalf("CreateApplication(%s): %v", name, err)
		}
	}
	mkApp("base", false)
	mkApp("base-pr-7", true)

	byDefault, err := repo.ListApplicationsByEnvironment(ctx, envID, false)
	if err != nil {
		t.Fatalf("list default: %v", err)
	}
	if len(byDefault) != 1 || byDefault[0].Name != "base" {
		t.Fatalf("default listing = %+v, want only the base application", byDefault)
	}
	withPreviews, err := repo.ListApplicationsByEnvironment(ctx, envID, true)
	if err != nil {
		t.Fatalf("list with previews: %v", err)
	}
	if len(withPreviews) != 2 {
		t.Fatalf("previews listing = %d rows, want base plus sibling", len(withPreviews))
	}
}

func TestStoreRepositoryActiveDeploymentIndex(t *testing.T) {
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

	email := fmt.Sprintf("fx-6a-active-%d@example.com", time.Now().UnixNano())
	user, err := st.CreateUser(ctx, email, nil)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	userID := uuid.UUID(user.ID.Bytes)
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		if _, err := pool.Exec(cleanupCtx, "DELETE FROM users WHERE id = $1", user.ID); err != nil {
			t.Logf("cleanup delete user: %v", err)
		}
	})

	envID, serverID := seedProjectEnvironment(t, ctx, st)

	app, err := st.CreateApplication(ctx, sqlc.CreateApplicationParams{
		UserID:        pgUUID(userID),
		ServerID:      pgUUID(serverID),
		EnvironmentID: pgUUID(envID),
		Name:          "active-index-app",
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

	first, err := repo.CreateDeployment(ctx, Deployment{
		ApplicationID: appID, Kind: KindDeploy, State: StateQueued,
	})
	if err != nil {
		t.Fatalf("first active deployment: %v", err)
	}
	if _, err := repo.CreateDeployment(ctx, Deployment{
		ApplicationID: appID, Kind: KindDeploy, State: StateQueued,
	}); !errors.Is(err, ErrConflict) {
		t.Fatalf("second active deployment error = %v, want ErrConflict", err)
	}

	first.State = StateRunning
	if _, err := repo.UpdateDeployment(ctx, first); err != nil {
		t.Fatalf("mark first running: %v", err)
	}
	second, err := repo.CreateDeployment(ctx, Deployment{
		ApplicationID: appID, Kind: KindDeploy, State: StateQueued,
	})
	if err != nil {
		t.Fatalf("deployment after running release: %v", err)
	}

	second.State = StateFailed
	if _, err := repo.UpdateDeployment(ctx, second); err != nil {
		t.Fatalf("mark second failed: %v", err)
	}
	if _, err := repo.CreateDeployment(ctx, Deployment{
		ApplicationID: appID, Kind: KindRollback, State: StateQueued,
	}); err != nil {
		t.Fatalf("deployment after failed release: %v", err)
	}
}

func TestStoreRepositoryListDeploymentsByAppLimit(t *testing.T) {
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

	email := fmt.Sprintf("limit-%d@example.com", time.Now().UnixNano())
	user, err := st.CreateUser(ctx, email, nil)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	userID := uuid.UUID(user.ID.Bytes)
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		if _, err := pool.Exec(cleanupCtx, "DELETE FROM users WHERE id = $1", user.ID); err != nil {
			t.Logf("cleanup delete user: %v", err)
		}
	})

	envID, serverID := seedProjectEnvironment(t, ctx, st)

	newApp := func(name string) uuid.UUID {
		t.Helper()
		app, err := st.CreateApplication(ctx, sqlc.CreateApplicationParams{
			UserID:        pgUUID(userID),
			ServerID:      pgUUID(serverID),
			EnvironmentID: pgUUID(envID),
			Name:          name,
			Provider:      "github",
			Repo:          "acme/demo",
			CloneUrl:      "https://github.com/acme/demo.git",
			Branch:        "main",
			BuildPack:     "dockerfile",
		})
		if err != nil {
			t.Fatalf("CreateApplication(%s): %v", name, err)
		}
		return uuid.UUID(app.ID.Bytes)
	}
	appA := newApp("limit-app-a")
	appB := newApp("limit-app-b")
	appEmpty := newApp("limit-app-empty")

	seed := []Deployment{
		{ApplicationID: appA, Kind: KindDeploy, State: StateFailed},
		{ApplicationID: appA, Kind: KindDeploy, State: StateFailed},
		{ApplicationID: appA, Kind: KindDeploy, State: StateRunning},
	}
	created := make([]Deployment, 0, len(seed))
	for i, dep := range seed {
		row, err := repo.CreateDeployment(ctx, dep)
		if err != nil {
			t.Fatalf("CreateDeployment(%d): %v", i, err)
		}
		created = append(created, row)
	}
	base := time.Now().UTC().Truncate(time.Second)
	for i, row := range created {
		ts := base.Add(time.Duration(i) * time.Second)
		if _, err := pool.Exec(ctx,
			"UPDATE deployments SET created_at = $1, updated_at = $1 WHERE id = $2",
			ts, pgUUID(row.ID)); err != nil {
			t.Fatalf("stamp deployment %d: %v", i, err)
		}
	}
	ids := func(rows []Deployment) []string {
		out := make([]string, 0, len(rows))
		for _, row := range rows {
			out = append(out, row.ID.String())
		}
		return out
	}
	newestFirst := []string{created[2].ID.String(), created[1].ID.String(), created[0].ID.String()}

	t.Run("oversized limit returns every row newest first", func(t *testing.T) {
		rows, err := repo.ListDeploymentsLimit(ctx, appA, 10)
		if err != nil {
			t.Fatalf("ListDeploymentsLimit: %v", err)
		}
		got := ids(rows)
		if len(got) != 3 || got[0] != newestFirst[0] || got[1] != newestFirst[1] || got[2] != newestFirst[2] {
			t.Errorf("order = %v, want %v", got, newestFirst)
		}
	})

	t.Run("limit 1 returns exactly the newest", func(t *testing.T) {
		rows, err := repo.ListDeploymentsLimit(ctx, appA, 1)
		if err != nil {
			t.Fatalf("ListDeploymentsLimit: %v", err)
		}
		if len(rows) != 1 || rows[0].ID != created[2].ID {
			t.Errorf("rows = %v, want exactly [%s]", ids(rows), created[2].ID)
		}
	})

	t.Run("limit 2 returns the two newest", func(t *testing.T) {
		rows, err := repo.ListDeploymentsLimit(ctx, appA, 2)
		if err != nil {
			t.Fatalf("ListDeploymentsLimit: %v", err)
		}
		got := ids(rows)
		if len(got) != 2 || got[0] != newestFirst[0] || got[1] != newestFirst[1] {
			t.Errorf("rows = %v, want %v", got, newestFirst[:2])
		}
	})

	t.Run("non-positive limit selects one row", func(t *testing.T) {
		rows, err := repo.ListDeploymentsLimit(ctx, appA, 0)
		if err != nil {
			t.Fatalf("ListDeploymentsLimit: %v", err)
		}
		if len(rows) != 1 || rows[0].ID != created[2].ID {
			t.Errorf("rows = %v, want exactly [%s]", ids(rows), created[2].ID)
		}
	})

	t.Run("equal timestamps fall back to id DESC", func(t *testing.T) {
		fixed := []string{
			"11111111-1111-1111-1111-111111111111",
			"22222222-2222-2222-2222-222222222222",
			"33333333-3333-3333-3333-333333333333",
		}
		for i, row := range created {
			id, err := uuid.Parse(fixed[i])
			if err != nil {
				t.Fatalf("parse fixed id %d: %v", i, err)
			}
			if _, err := pool.Exec(ctx,
				"UPDATE deployments SET id = $1, created_at = $2, updated_at = $2 WHERE id = $3",
				pgUUID(id), base, pgUUID(row.ID)); err != nil {
				t.Fatalf("assign fixed id %d: %v", i, err)
			}
			created[i].ID = id
		}
		want := []string{created[2].ID.String(), created[1].ID.String(), created[0].ID.String()}
		rows, err := repo.ListDeploymentsLimit(ctx, appA, 10)
		if err != nil {
			t.Fatalf("ListDeploymentsLimit: %v", err)
		}
		got := ids(rows)
		if len(got) != 3 || got[0] != want[0] || got[1] != want[1] || got[2] != want[2] {
			t.Errorf("order = %v, want %v", got, want)
		}
	})

	t.Run("other applications never leak", func(t *testing.T) {
		foreign, err := repo.CreateDeployment(ctx, Deployment{
			ApplicationID: appB, Kind: KindDeploy, State: StateFailed,
		})
		if err != nil {
			t.Fatalf("CreateDeployment(appB): %v", err)
		}
		rows, err := repo.ListDeploymentsLimit(ctx, appA, 10)
		if err != nil {
			t.Fatalf("ListDeploymentsLimit(appA): %v", err)
		}
		for _, row := range rows {
			if row.ID == foreign.ID || row.ApplicationID != appA {
				t.Errorf("appA page contains foreign row %s (app %s)", row.ID, row.ApplicationID)
			}
		}
		own, err := repo.ListDeploymentsLimit(ctx, appB, 10)
		if err != nil {
			t.Fatalf("ListDeploymentsLimit(appB): %v", err)
		}
		if len(own) != 1 || own[0].ID != foreign.ID {
			t.Errorf("appB rows = %v, want exactly [%s]", ids(own), foreign.ID)
		}
	})

	t.Run("empty application returns none", func(t *testing.T) {
		rows, err := repo.ListDeploymentsLimit(ctx, appEmpty, 10)
		if err != nil {
			t.Fatalf("ListDeploymentsLimit: %v", err)
		}
		if len(rows) != 0 {
			t.Errorf("rows = %v, want none", ids(rows))
		}
	})
}

func TestSystemTeardownRemovesLocalKey(t *testing.T) {
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

	email := fmt.Sprintf("be-8.1-key-%d@example.com", time.Now().UnixNano())
	user, err := st.CreateUser(ctx, email, nil)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	userID := uuid.UUID(user.ID.Bytes)
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		if _, err := pool.Exec(cleanupCtx, "DELETE FROM users WHERE id = $1", user.ID); err != nil {
			t.Logf("cleanup delete user: %v", err)
		}
	})

	envID, serverID := seedProjectEnvironment(t, ctx, st)

	base, err := st.CreateApplication(ctx, sqlc.CreateApplicationParams{
		UserID: pgUUID(userID), EnvironmentID: pgUUID(envID), ServerID: pgUUID(serverID),
		Name: "preview-base", Provider: "github",
		Repo: "acme/demo", CloneUrl: "https://github.com/acme/demo.git",
		Branch: "main", BuildPack: "dockerfile",
	})
	if err != nil {
		t.Fatalf("CreateApplication(base): %v", err)
	}
	baseID := uuid.UUID(base.ID.Bytes)
	preview, err := st.CreateApplication(ctx, sqlc.CreateApplicationParams{
		UserID: pgUUID(userID), EnvironmentID: pgUUID(envID), ServerID: pgUUID(serverID),
		Name: "preview-base-pr-7", Provider: "github",
		Repo: "acme/demo", CloneUrl: "https://github.com/acme/demo.git",
		Branch: "feat/x", BuildPack: "dockerfile",
		IsPreview: true,
	})
	if err != nil {
		t.Fatalf("CreateApplication(preview): %v", err)
	}
	previewID := uuid.UUID(preview.ID.Bytes)
	if !preview.IsPreview {
		t.Fatal("the preview application is not marked is_preview")
	}

	privatePEM, publicKey, fingerprint, err := generateDeployKeyPair("gotham:deploy:integration")
	if err != nil {
		t.Fatalf("generateDeployKeyPair: %v", err)
	}
	for _, appID := range []uuid.UUID{baseID, previewID} {
		if _, err := repo.CreateDeployKey(ctx, DeployKey{
			ApplicationID: appID, Provider: "github", Repo: "acme/demo",
			ProviderKeyID: "77", Fingerprint: fingerprint, PublicKey: publicKey,
		}, privatePEM); err != nil {
			t.Fatalf("CreateDeployKey(%s): %v", appID, err)
		}
	}

	svc := NewService(Config{Repository: repo, Secret: secret, Logger: discardLogger()})
	t.Cleanup(func() { _ = svc.Close() })
	if err := svc.DeleteSystemApplication(ctx, previewID); err != nil {
		t.Fatalf("DeleteSystemApplication: %v", err)
	}
	if _, err := st.GetApplication(ctx, pgUUID(previewID)); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("preview application still exists: %v", err)
	}

	var previewKeys int
	if err := pool.QueryRow(ctx,
		"SELECT COUNT(*) FROM private_keys WHERE name = $1", deployKeyRowName(previewID),
	).Scan(&previewKeys); err != nil {
		t.Fatalf("count preview private keys: %v", err)
	}
	if previewKeys != 0 {
		t.Errorf("preview private_keys rows = %d, want 0", previewKeys)
	}
	if _, err := repo.GetDeployKey(ctx, baseID); err != nil {
		t.Errorf("base deploy key after the preview teardown: %v", err)
	}
	if pem, err := repo.DeployKeyPrivatePEM(ctx, baseID); err != nil || pem != privatePEM {
		t.Errorf("base private key after the preview teardown = %q, %v", pem, err)
	}
}

func TestStoreRepositoryGitCredentialRoundtrip(t *testing.T) {
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

	email := fmt.Sprintf("gs4-cred-%d@example.com", time.Now().UnixNano())
	user, err := st.CreateUser(ctx, email, nil)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	userID := uuid.UUID(user.ID.Bytes)
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		if _, err := pool.Exec(cleanupCtx, "DELETE FROM users WHERE id = $1", user.ID); err != nil {
			t.Logf("cleanup delete user: %v", err)
		}
	})

	envID, serverID := seedProjectEnvironment(t, ctx, st)

	app, err := st.CreateApplication(ctx, sqlc.CreateApplicationParams{
		UserID:        pgUUID(userID),
		ServerID:      pgUUID(serverID),
		EnvironmentID: pgUUID(envID),
		Name:          "credential-app",
		Provider:      "",
		Repo:          "https://git.internal/acme/demo.git",
		CloneUrl:      "https://git.internal/acme/demo.git",
		Branch:        "main",
		BuildPack:     "dockerfile",
		SourceType:    "git_private",
	})
	if err != nil {
		t.Fatalf("CreateApplication: %v", err)
	}
	appID := uuid.UUID(app.ID.Bytes)

	if _, err := repo.GetGitCredential(ctx, appID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetGitCredential error = %v, want ErrNotFound", err)
	}
	if username, token, err := repo.GitCredential(ctx, appID); err != nil || username != "" || token != "" {
		t.Fatalf("GitCredential = %q/%q, %v; want empty, nil", username, token, err)
	}

	sealed, err := providers.SealSecret(secret, "s3cr3t-token")
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	if err := repo.UpsertGitCredential(ctx, appID, "bob", sealed); err != nil {
		t.Fatalf("UpsertGitCredential: %v", err)
	}
	meta, err := repo.GetGitCredential(ctx, appID)
	if err != nil {
		t.Fatalf("GetGitCredential: %v", err)
	}
	if meta.Username != "bob" {
		t.Errorf("username = %q, want bob", meta.Username)
	}
	var stored string
	if err := pool.QueryRow(ctx,
		"SELECT ciphertext FROM application_git_credentials WHERE application_id = $1", pgUUID(appID)).Scan(&stored); err != nil {
		t.Fatalf("read ciphertext: %v", err)
	}
	if stored != sealed || strings.Contains(stored, "s3cr3t-token") {
		t.Error("the token is not stored sealed")
	}
	if username, token, err := repo.GitCredential(ctx, appID); err != nil || username != "bob" || token != "s3cr3t-token" {
		t.Errorf("GitCredential = %q/%q, %v; want the opened token", username, token, err)
	}

	sibling, err := st.CreateApplication(ctx, sqlc.CreateApplicationParams{
		UserID:        pgUUID(userID),
		ServerID:      pgUUID(serverID),
		EnvironmentID: pgUUID(envID),
		Name:          "credential-sibling",
		Provider:      "",
		Repo:          "https://git.internal/acme/demo.git",
		CloneUrl:      "https://git.internal/acme/demo.git",
		Branch:        "feat/x",
		BuildPack:     "dockerfile",
		SourceType:    "git_private",
	})
	if err != nil {
		t.Fatalf("CreateApplication sibling: %v", err)
	}
	siblingID := uuid.UUID(sibling.ID.Bytes)
	if err := repo.CopyGitCredential(ctx, appID, siblingID); err != nil {
		t.Fatalf("CopyGitCredential: %v", err)
	}
	if _, token, err := repo.GitCredential(ctx, siblingID); err != nil || token != "s3cr3t-token" {
		t.Errorf("sibling credential opens to %q, %v; want the token", token, err)
	}
	if err := repo.CopyGitCredential(ctx, uuid.New(), uuid.New()); err != nil {
		t.Errorf("CopyGitCredential without a row: %v", err)
	}

	if err := repo.DeleteGitCredential(ctx, appID); err != nil {
		t.Fatalf("DeleteGitCredential: %v", err)
	}
	if err := repo.DeleteGitCredential(ctx, appID); !errors.Is(err, ErrNotFound) {
		t.Errorf("second delete error = %v, want ErrNotFound", err)
	}
}

func TestStoreRepositoryListRecentDeploymentsScope(t *testing.T) {
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

	newUser := func(tag string) uuid.UUID {
		t.Helper()
		email := fmt.Sprintf("feed-%s-%d@example.com", tag, time.Now().UnixNano())
		user, err := st.CreateUser(ctx, email, nil)
		if err != nil {
			t.Fatalf("CreateUser: %v", err)
		}
		id := uuid.UUID(user.ID.Bytes)
		t.Cleanup(func() {
			cctx, ccancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer ccancel()
			if _, err := pool.Exec(cctx, "DELETE FROM users WHERE id = $1", user.ID); err != nil {
				t.Logf("cleanup delete user: %v", err)
			}
		})
		return id
	}
	userA, userB := newUser("a"), newUser("b")

	envA, serverA := seedProjectEnvironment(t, ctx, st)
	envB, serverB := seedProjectEnvironment(t, ctx, st)
	for _, grant := range []struct{ user, env uuid.UUID }{{userA, envA}, {userB, envB}} {
		project, err := st.ResourceProject(ctx, "environment", grant.env)
		if err != nil {
			t.Fatal(err)
		}
		if err := st.GrantProject(ctx, project, grant.user, grant.user); err != nil {
			t.Fatal(err)
		}
	}

	newApp := func(user, env, server uuid.UUID, name string, preview bool) uuid.UUID {
		t.Helper()
		app, err := st.CreateApplication(ctx, sqlc.CreateApplicationParams{
			UserID:        pgUUID(user),
			ServerID:      pgUUID(server),
			EnvironmentID: pgUUID(env),
			Name:          name,
			Provider:      "github",
			Repo:          "acme/demo",
			CloneUrl:      "https://github.com/acme/demo.git",
			Branch:        "main",
			BuildPack:     "dockerfile",
			IsPreview:     preview,
		})
		if err != nil {
			t.Fatalf("CreateApplication(%s): %v", name, err)
		}
		return uuid.UUID(app.ID.Bytes)
	}
	appA := newApp(userA, envA, serverA, "feed-a", false)
	appB := newApp(userB, envB, serverB, "feed-b", false)
	previewA := newApp(userA, envA, serverA, "feed-a-preview", true)

	seedRows := []Deployment{
		{ApplicationID: appA, Kind: KindDeploy, State: StateFailed},
		{ApplicationID: appB, Kind: KindDeploy, State: StateFailed},
		{ApplicationID: previewA, Kind: KindDeploy, State: StateFailed},
	}
	created := make([]Deployment, 0, len(seedRows))
	for i, dep := range seedRows {
		row, err := repo.CreateDeployment(ctx, dep)
		if err != nil {
			t.Fatalf("CreateDeployment(%d): %v", i, err)
		}
		created = append(created, row)
	}
	base := time.Now().UTC().Truncate(time.Second)
	for i, row := range created {
		ts := base.Add(time.Duration(i) * time.Second)
		if _, err := pool.Exec(ctx,
			"UPDATE deployments SET created_at = $1, updated_at = $1 WHERE id = $2",
			ts, pgUUID(row.ID)); err != nil {
			t.Fatalf("stamp deployment %d: %v", i, err)
		}
	}

	rows, err := repo.ListRecentDeployments(ctx, userA, 10)
	if err != nil {
		t.Fatalf("ListRecentDeployments: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("user A rows = %d, want 1 (its granted non-preview app only)", len(rows))
	}
	if rows[0].ApplicationID != appA || rows[0].ApplicationName != "feed-a" {
		t.Errorf("user A row = %s/%q, want feed-a only (no cross-project leak)",
			rows[0].ApplicationID, rows[0].ApplicationName)
	}

	userFeed, err := repo.ListRecentDeployments(ctx, userA, 10)
	if err != nil {
		t.Fatalf("granted feed: %v", err)
	}
	if len(userFeed) != 1 || userFeed[0].ApplicationID != appA {
		t.Fatalf("granted rows = %d, want user A's granted non-preview app only", len(userFeed))
	}

	limited, err := repo.ListRecentDeployments(ctx, userB, 0)
	if err != nil {
		t.Fatalf("user B feed: %v", err)
	}
	if len(limited) != 1 || limited[0].ApplicationID != appB {
		t.Errorf("user B rows = %d, want exactly its own app", len(limited))
	}
}
