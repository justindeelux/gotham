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

	"github.com/justindeelux/gotham/internal/servers"
	"github.com/justindeelux/gotham/internal/store"
	"github.com/justindeelux/gotham/internal/store/sqlc"
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

// integrationDSNExplicit reports whether the operator opted in by setting
// GOTHAM_TEST_DSN. An explicit opt-in turns a missing database into a failure
// instead of a skip, so the acceptance lane can never pass green by skipping.
func integrationDSNExplicit() bool {
	return os.Getenv("GOTHAM_TEST_DSN") != ""
}

// TestStoreRepositoryDeployKeyRoundtrip exercises the deploy-key rows against a
// real server: sealing the private half in private_keys, reading it back
// through the Phase-2 contract, the unique one-key-per-application index and
// the delete that takes the mapping and the private key together. It skips
// when no database is reachable.
func TestStoreRepositoryDeployKeyRoundtrip(t *testing.T) {
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

	app, err := st.CreateApplication(ctx, sqlc.CreateApplicationParams{
		UserID:    pgUUID(userID),
		Name:      "deploy-key-app",
		Provider:  "github",
		Repo:      "acme/demo",
		CloneUrl:  "https://github.com/acme/demo.git",
		Branch:    "main",
		BuildPack: "dockerfile",
	})
	if err != nil {
		t.Fatalf("CreateApplication: %v", err)
	}
	appID := uuid.UUID(app.ID.Bytes)

	// An application without a key: the cloner's anonymous default.
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

	// The private half is sealed at rest and stays readable by the Phase-2
	// reader of private_keys.encrypted_key (servers.DecryptKey).
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

	// One key per application: a second write fails inside the transaction, so
	// it must not leave an orphan private_keys row behind.
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

	// Deleting takes the mapping and the private key with it, once. The delete
	// is fenced on the mapping ID that was read: replaying it is a no-op.
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

// TestStoreRepositoryDeleteDeployKeyFence pins U2 / C3-6 at the real store:
// the delete is fenced on the mapping ID that was read, so a stale delete
// (whose row was already removed and replaced) matches nothing and leaves the
// replacement's mapping and sealed private key intact.
func TestStoreRepositoryDeleteDeployKeyFence(t *testing.T) {
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

	app, err := st.CreateApplication(ctx, sqlc.CreateApplicationParams{
		UserID:    pgUUID(userID),
		Name:      "fence-app",
		Provider:  "github",
		Repo:      "acme/fence",
		CloneUrl:  "https://github.com/acme/fence.git",
		Branch:    "main",
		BuildPack: "dockerfile",
	})
	if err != nil {
		t.Fatalf("CreateApplication: %v", err)
	}
	appID := uuid.UUID(app.ID.Bytes)
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		// private_keys has no application FK; clear the deploy-key rows so the
		// test does not leak sealed keys across runs.
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

	// The first delete reads key1 and removes it; a replacement is then
	// installed behind it.
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

	// The stale delete (still holding key1) must not touch key2.
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

// TestStoreRepositoryActiveDeploymentIndex pins the partial unique index that
// allows at most one active deployment per application (deployments_active_app_idx):
// a second active submit must surface as ErrConflict, and a row that becomes
// terminal — running or failed — releases the index for the next deploy.
func TestStoreRepositoryActiveDeploymentIndex(t *testing.T) {
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

	app, err := st.CreateApplication(ctx, sqlc.CreateApplicationParams{
		UserID:    pgUUID(userID),
		Name:      "active-index-app",
		Provider:  "github",
		Repo:      "acme/demo",
		CloneUrl:  "https://github.com/acme/demo.git",
		Branch:    "main",
		BuildPack: "dockerfile",
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

	// A running row is terminal: the index is released.
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

	// A failed row releases it too.
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

// TestSystemTeardownRemovesLocalKey is the F7 regression: deleting a preview
// sibling through the system path removes its local deploy-key rows — the
// mapping and the sealed private key it points at — while the base
// application's key stays untouched (the remote key is shared).
func TestSystemTeardownRemovesLocalKey(t *testing.T) {
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

	base, err := st.CreateApplication(ctx, sqlc.CreateApplicationParams{
		UserID: pgUUID(userID), Name: "preview-base", Provider: "github",
		Repo: "acme/demo", CloneUrl: "https://github.com/acme/demo.git",
		Branch: "main", BuildPack: "dockerfile", BaseDomain: "app.example.com",
	})
	if err != nil {
		t.Fatalf("CreateApplication(base): %v", err)
	}
	baseID := uuid.UUID(base.ID.Bytes)
	preview, err := st.CreateApplication(ctx, sqlc.CreateApplicationParams{
		UserID: pgUUID(userID), Name: "preview-base-pr-7", Provider: "github",
		Repo: "acme/demo", CloneUrl: "https://github.com/acme/demo.git",
		Branch: "feat/x", BuildPack: "dockerfile", BaseDomain: "pr-7-app.example.com",
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

	// The preview's local rows are gone...
	var previewKeys int
	if err := pool.QueryRow(ctx,
		"SELECT COUNT(*) FROM private_keys WHERE name = $1", deployKeyRowName(previewID),
	).Scan(&previewKeys); err != nil {
		t.Fatalf("count preview private keys: %v", err)
	}
	if previewKeys != 0 {
		t.Errorf("preview private_keys rows = %d, want 0", previewKeys)
	}
	// ...and the base's are untouched.
	if _, err := repo.GetDeployKey(ctx, baseID); err != nil {
		t.Errorf("base deploy key after the preview teardown: %v", err)
	}
	if pem, err := repo.DeployKeyPrivatePEM(ctx, baseID); err != nil || pem != privatePEM {
		t.Errorf("base private key after the preview teardown = %q, %v", pem, err)
	}
}
