package deploy

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"golang.org/x/crypto/ssh"

	"github.com/justindeelux/gotham/internal/providers"
	"github.com/justindeelux/gotham/internal/servers"
)

// fakeRegistrar is a scriptable KeyRegistrar: it records every registration
// and removal and can fail either call, which is how the Git host is faked
// without a network.
type fakeRegistrar struct {
	mu sync.Mutex

	added   []providers.DeployKey
	targets []providers.HookTarget
	removed []string

	addErr    error
	removeErr error
	nextID    int
}

// Compile-time guarantee that fakeRegistrar satisfies the seam.
var _ KeyRegistrar = (*fakeRegistrar)(nil)

// AddDeployKey implements KeyRegistrar, handing out a stable provider key ID.
func (f *fakeRegistrar) AddDeployKey(_ context.Context, target providers.HookTarget, key providers.DeployKey) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.addErr != nil {
		return "", f.addErr
	}
	f.added = append(f.added, key)
	f.targets = append(f.targets, target)
	f.nextID++
	return fmt.Sprintf("key-%d", f.nextID), nil
}

// RemoveDeployKey implements KeyRegistrar.
func (f *fakeRegistrar) RemoveDeployKey(_ context.Context, _ providers.HookTarget, keyID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.removeErr != nil {
		return f.removeErr
	}
	f.removed = append(f.removed, keyID)
	return nil
}

// addedKey returns the first registered key (test helper).
func (f *fakeRegistrar) addedKey() providers.DeployKey {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.added) == 0 {
		return providers.DeployKey{}
	}
	return f.added[0]
}

// newKeyService builds a Service whose deploy keys are registered through
// registrar.
func newKeyService(t *testing.T, repo *fakeRepository, registrar KeyRegistrar) *Service {
	t.Helper()
	svc := NewService(Config{
		Repository:   repo,
		Secret:       testSecretKey,
		Logger:       discardLogger(),
		KeyRegistrar: registrar,
	})
	t.Cleanup(func() { _ = svc.Close() })
	return svc
}

// keyFixture seeds a repository holding one GitHub application.
func keyFixture(t *testing.T) (*fakeRepository, *fakeRegistrar, *Service, Application) {
	t.Helper()
	repo := &fakeRepository{}
	app := testApplication(uuid.New())
	repo.app = app
	registrar := &fakeRegistrar{}
	return repo, registrar, newKeyService(t, repo, registrar), app
}

// TestGenerateDeployKeyPair checks the generated material: an OpenSSH private
// PEM `ssh -i` can read, an authorized_keys line carrying the comment, and a
// SHA-256 fingerprint that matches that line.
func TestGenerateDeployKeyPair(t *testing.T) {
	const comment = "gotham:deploy:11111111-2222-3333-4444-555555555555"
	privatePEM, publicKey, fingerprint, err := generateDeployKeyPair(comment)
	if err != nil {
		t.Fatalf("generateDeployKeyPair: %v", err)
	}
	if !strings.Contains(privatePEM, "BEGIN OPENSSH PRIVATE KEY") {
		t.Fatalf("private key = %q, want an OpenSSH PEM", firstLine(privatePEM))
	}
	if !strings.HasPrefix(publicKey, "ssh-ed25519 ") {
		t.Errorf("public key = %q, want an ssh-ed25519 line", publicKey)
	}
	if !strings.HasSuffix(publicKey, " "+comment) {
		t.Errorf("public key = %q, want it to end with the comment", publicKey)
	}
	if !strings.HasPrefix(fingerprint, "SHA256:") {
		t.Errorf("fingerprint = %q, want a SHA256: fingerprint", fingerprint)
	}

	signer, err := ssh.ParsePrivateKey([]byte(privatePEM))
	if err != nil {
		t.Fatalf("parse private key: %v", err)
	}
	parsed, _, _, _, err := ssh.ParseAuthorizedKey([]byte(publicKey))
	if err != nil {
		t.Fatalf("parse public key: %v", err)
	}
	if string(signer.PublicKey().Marshal()) != string(parsed.Marshal()) {
		t.Error("public key does not match the private key")
	}
	if got := ssh.FingerprintSHA256(parsed); got != fingerprint {
		t.Errorf("fingerprint = %q, want %q", got, fingerprint)
	}
}

// TestDeployKeySealingFollowsPrivateKeyContract proves the private half is
// sealed with the shared AES-256-GCM helper and stays readable by the Phase-2
// reader of private_keys.encrypted_key (servers.DecryptKey): both use
// base64(nonce||ciphertext) over the SHA-256 of the same secret.
func TestDeployKeySealingFollowsPrivateKeyContract(t *testing.T) {
	privatePEM, _, _, err := generateDeployKeyPair("gotham:deploy:test")
	if err != nil {
		t.Fatalf("generateDeployKeyPair: %v", err)
	}

	sealed, err := providers.SealSecret(testSecretKey, privatePEM)
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	if strings.Contains(sealed, "BEGIN OPENSSH") {
		t.Fatal("sealed value exposes the private key")
	}
	opened, err := providers.OpenSecret(testSecretKey, sealed)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if opened != privatePEM {
		t.Error("round-trip changed the private key")
	}
	viaServers, err := servers.DecryptKey(sealed, testSecretKey)
	if err != nil {
		t.Fatalf("servers.DecryptKey: %v", err)
	}
	if viaServers != privatePEM {
		t.Error("servers.DecryptKey cannot read the sealed deploy key")
	}
}

func TestCreateDeployKeyRegistersAndStores(t *testing.T) {
	repo, registrar, svc, app := keyFixture(t)

	key, err := svc.CreateDeployKey(context.Background(), app.UserID, app.ID)
	if err != nil {
		t.Fatalf("CreateDeployKey: %v", err)
	}
	if key.ProviderKeyID == "" {
		t.Error("provider key id is empty")
	}
	if !strings.HasPrefix(key.Fingerprint, "SHA256:") {
		t.Errorf("fingerprint = %q", key.Fingerprint)
	}
	if !strings.HasPrefix(key.PublicKey, "ssh-ed25519 ") {
		t.Errorf("public key = %q", key.PublicKey)
	}
	if len(registrar.added) != 1 {
		t.Fatalf("provider registrations = %d, want 1", len(registrar.added))
	}
	target := registrar.targets[0]
	if target.Repo != app.Repo || target.Provider != app.Provider || target.UserID != app.UserID {
		t.Errorf("target = %+v, want the application's repository", target)
	}
	if !strings.HasPrefix(registrar.addedKey().Title, "gotham:") {
		t.Errorf("title = %q, want a gotham: prefix", registrar.addedKey().Title)
	}
	if registrar.addedKey().Key != key.PublicKey {
		t.Error("registered a different key than the stored one")
	}

	// The cloner must be able to open the stored private half.
	privatePEM, err := repo.DeployKeyPrivatePEM(context.Background(), app.ID)
	if err != nil {
		t.Fatalf("DeployKeyPrivatePEM: %v", err)
	}
	if _, err := ssh.ParsePrivateKey([]byte(privatePEM)); err != nil {
		t.Errorf("stored private key is not a usable OpenSSH key: %v", err)
	}

	// Repeating the call returns the same key instead of registering another.
	again, err := svc.CreateDeployKey(context.Background(), app.UserID, app.ID)
	if err != nil {
		t.Fatalf("CreateDeployKey again: %v", err)
	}
	if again.ID != key.ID {
		t.Errorf("second key = %s, want the first one (%s)", again.ID, key.ID)
	}
	if len(registrar.added) != 1 {
		t.Errorf("provider registrations = %d, want 1 (idempotent)", len(registrar.added))
	}
}

func TestCreateDeployKeyProviderFailureStoresNothing(t *testing.T) {
	repo, registrar, svc, app := keyFixture(t)
	registrar.addErr = errors.New("host refused")

	_, err := svc.CreateDeployKey(context.Background(), app.UserID, app.ID)
	if !errors.Is(err, ErrProvider) {
		t.Fatalf("error = %v, want ErrProvider", err)
	}
	if repo.hasDeployKey(app.ID) {
		t.Error("stored a deploy key the Git host refused")
	}
}

func TestCreateDeployKeyStoreFailureRollsBackHostKey(t *testing.T) {
	repo, registrar, svc, app := keyFixture(t)
	repo.deployKeyErr = errors.New("database down")

	_, err := svc.CreateDeployKey(context.Background(), app.UserID, app.ID)
	if err == nil {
		t.Fatal("error = nil, want the store failure to surface")
	}
	if len(registrar.added) != 1 {
		t.Fatalf("provider registrations = %d, want 1", len(registrar.added))
	}
	if len(registrar.removed) != 1 || registrar.removed[0] != "key-1" {
		t.Errorf("removed = %v, want the registered key rolled back", registrar.removed)
	}
}

func TestCreateDeployKeyRejectsUnsupportedProvider(t *testing.T) {
	repo, registrar, svc, app := keyFixture(t)
	app.Provider = ""
	repo.app = app

	_, err := svc.CreateDeployKey(context.Background(), app.UserID, app.ID)
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("error = %v, want ErrValidation", err)
	}
	if len(registrar.added) != 0 {
		t.Errorf("provider registrations = %d, want 0", len(registrar.added))
	}
}

func TestCreateDeployKeyForeignApplicationIsNotFound(t *testing.T) {
	_, _, svc, app := keyFixture(t)

	if _, err := svc.CreateDeployKey(context.Background(), uuid.New(), app.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("error = %v, want ErrNotFound", err)
	}
}

func TestDeleteDeployKeyRemovesHostKeyThenRow(t *testing.T) {
	repo, registrar, svc, app := keyFixture(t)
	created, err := svc.CreateDeployKey(context.Background(), app.UserID, app.ID)
	if err != nil {
		t.Fatalf("CreateDeployKey: %v", err)
	}

	deleted, err := svc.DeleteDeployKey(context.Background(), app.UserID, app.ID)
	if err != nil {
		t.Fatalf("DeleteDeployKey: %v", err)
	}
	if !deleted {
		t.Error("deleted = false, want true")
	}
	if len(registrar.removed) != 1 || registrar.removed[0] != created.ProviderKeyID {
		t.Errorf("removed = %v, want [%s]", registrar.removed, created.ProviderKeyID)
	}
	if repo.hasDeployKey(app.ID) {
		t.Error("deploy key row survived the delete")
	}
	if pem, err := repo.DeployKeyPrivatePEM(context.Background(), app.ID); err != nil || pem != "" {
		t.Errorf("DeployKeyPrivatePEM = %q, %v; want empty", pem, err)
	}

	// Deleting twice is a success with nothing left to do.
	deleted, err = svc.DeleteDeployKey(context.Background(), app.UserID, app.ID)
	if err != nil || deleted {
		t.Errorf("second delete = %v, %v; want false, nil", deleted, err)
	}
}

func TestDeleteDeployKeyProviderFailureKeepsKey(t *testing.T) {
	repo, registrar, svc, app := keyFixture(t)
	if _, err := svc.CreateDeployKey(context.Background(), app.UserID, app.ID); err != nil {
		t.Fatalf("CreateDeployKey: %v", err)
	}
	registrar.removeErr = errors.New("host down")

	deleted, err := svc.DeleteDeployKey(context.Background(), app.UserID, app.ID)
	if !errors.Is(err, ErrProvider) {
		t.Fatalf("error = %v, want ErrProvider", err)
	}
	if deleted {
		t.Error("deleted = true while the host still holds the key")
	}
	if !repo.hasDeployKey(app.ID) {
		t.Error("deploy key row was removed although the host call failed")
	}
}

// TestDeleteApplicationProviderFailureStillDeletes pins the delete-path
// trade-off: a Git host that cannot drop the deploy key must not leave a live
// application behind (the provider hook, if any, was already removed first, so
// aborting would silently disable automatic deploys). The local key row
// cascades with the application and the remote key orphan is logged for manual
// cleanup.
func TestDeleteApplicationProviderFailureStillDeletes(t *testing.T) {
	repo, registrar, svc, app := keyFixture(t)
	if _, err := svc.CreateDeployKey(context.Background(), app.UserID, app.ID); err != nil {
		t.Fatalf("CreateDeployKey: %v", err)
	}
	registrar.removeErr = errors.New("host down")

	if err := svc.DeleteApplication(context.Background(), app.UserID, app.ID); err != nil {
		t.Fatalf("DeleteApplication = %v, want the delete to proceed despite the host failure", err)
	}
	if _, err := svc.GetApplication(context.Background(), app.UserID, app.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("application survived the delete: %v", err)
	}
	if repo.hasDeployKey(app.ID) {
		t.Error("the local deploy key row must cascade with the application")
	}
	if len(registrar.removed) != 0 {
		t.Errorf("removed = %v, want none (the host call failed)", registrar.removed)
	}
}

func TestDeleteApplicationRemovesDeployKey(t *testing.T) {
	repo, registrar, svc, app := keyFixture(t)
	created, err := svc.CreateDeployKey(context.Background(), app.UserID, app.ID)
	if err != nil {
		t.Fatalf("CreateDeployKey: %v", err)
	}

	if err := svc.DeleteApplication(context.Background(), app.UserID, app.ID); err != nil {
		t.Fatalf("DeleteApplication: %v", err)
	}
	if len(registrar.removed) != 1 || registrar.removed[0] != created.ProviderKeyID {
		t.Errorf("removed = %v, want [%s]", registrar.removed, created.ProviderKeyID)
	}
	if repo.hasDeployKey(app.ID) {
		t.Error("deploy key row outlived the application")
	}
	if _, err := svc.GetApplication(context.Background(), app.UserID, app.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetApplication error = %v, want ErrNotFound", err)
	}
}

// TestDeleteApplicationWithoutKeySkipsProvider keeps the plain delete path
// (an application that never registered a key) free of Git-host calls.
func TestDeleteApplicationWithoutKeySkipsProvider(t *testing.T) {
	repo, registrar, svc, app := keyFixture(t)

	if err := svc.DeleteApplication(context.Background(), app.UserID, app.ID); err != nil {
		t.Fatalf("DeleteApplication: %v", err)
	}
	if len(registrar.removed) != 0 {
		t.Errorf("provider removals = %d, want 0", len(registrar.removed))
	}
	if _, err := repo.GetApplication(context.Background(), app.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetApplication error = %v, want ErrNotFound", err)
	}
}

// firstLine returns the head of a PEM for failure messages without printing a
// whole key.
func firstLine(value string) string {
	line, _, _ := strings.Cut(value, "\n")
	return line
}
