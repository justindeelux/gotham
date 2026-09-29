package deploy

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
)

// seedBaseForPreview stores a base application with environment, storage and
// deploy key, returning the repository holding it.
func seedBaseForPreview(t *testing.T, app Application) *fakeRepository {
	t.Helper()
	repo := &fakeRepository{app: app}
	if _, err := repo.CreateDeployKey(context.Background(), DeployKey{
		ApplicationID: app.ID,
		Provider:      "github",
		Repo:          app.Repo,
		PublicKey:     "ssh-ed25519 AAAA",
		Fingerprint:   "SHA256:test",
	}, "PRIVATE KEY PEM"); err != nil {
		t.Fatalf("seed deploy key: %v", err)
	}
	return repo
}

func TestCreatePreviewApplicationClonesConfig(t *testing.T) {
	userID := uuid.New()
	base := testApplication(userID)
	base.TeamID = uuid.New()
	base.BaseDomain = "app.example.com"
	repo := seedBaseForPreview(t, base)
	repo.envVars = []EnvVar{{ApplicationID: base.ID, Key: "FOO", Value: "bar"}}
	repo.secrets = []Secret{{ID: uuid.New(), ApplicationID: base.ID, Key: "TOKEN", Ciphertext: "sealed"}}
	repo.storages = []Storage{{ApplicationID: base.ID, Name: "data", HostPath: "/srv/data", ContainerPath: "/data"}}

	svc := newTestService(t, repo)
	created, err := svc.CreatePreviewApplication(context.Background(), base.ID, PreviewApplicationInput{
		Name:       "demo app-pr-7",
		Branch:     "feat/x",
		BaseDomain: "PR-7-Demo.app.example.com", // normalized by the clone
	})
	if err != nil {
		t.Fatalf("CreatePreviewApplication: %v", err)
	}

	if created.UserID != base.UserID || created.TeamID != base.TeamID ||
		created.ServerID != base.ServerID || created.Provider != base.Provider ||
		created.Repo != base.Repo || created.CloneURL != base.CloneURL ||
		created.BuildPack != base.BuildPack || created.Port != base.Port {
		t.Errorf("clone config = %+v, want everything copied from %+v", created, base)
	}
	if created.Name != "demo app-pr-7" || created.Branch != "feat/x" ||
		created.BaseDomain != "pr-7-demo.app.example.com" {
		t.Errorf("clone identity = %+v", created)
	}
	if created.HostPort != 0 {
		t.Errorf("HostPort = %d, want 0 so the preview never fights the base port binding", created.HostPort)
	}
	if !created.IsPreview {
		t.Error("the clone is not marked is_preview")
	}
	if base.IsPreview {
		t.Error("the base application was marked is_preview")
	}

	// Plain environment variables travel with the preview; sealed secrets and
	// storages do not (M2: a PR branch must not read production secrets or
	// write production volumes).
	envVars, _ := repo.ListEnvVars(context.Background(), created.ID)
	secrets, _ := repo.ListSecrets(context.Background(), created.ID)
	storages, _ := repo.ListStorages(context.Background(), created.ID)
	if len(envVars) != 1 || envVars[0].Key != "FOO" {
		t.Errorf("cloned env vars = %v, want the base's plain variables", envVars)
	}
	if len(secrets) != 0 {
		t.Errorf("cloned secrets = %v, want none", secrets)
	}
	if len(storages) != 0 {
		t.Errorf("cloned storages = %v, want none", storages)
	}

	key, err := repo.GetDeployKey(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("the preview must reuse the base deploy key: %v", err)
	}
	if key.Fingerprint != "SHA256:test" {
		t.Errorf("cloned deploy key = %+v", key)
	}
	if pem, _ := repo.DeployKeyPrivatePEM(context.Background(), created.ID); pem != "PRIVATE KEY PEM" {
		t.Errorf("cloned private key = %q", pem)
	}
	if !repo.hasDeployKey(base.ID) {
		t.Error("the base deploy key was removed by the clone")
	}

	// A second preview for another PR gets its own name and does not collide.
	second, err := svc.CreatePreviewApplication(context.Background(), base.ID, PreviewApplicationInput{
		Name: "demo app-pr-8", Branch: "feat/y", BaseDomain: "pr-8-demo.app.example.com",
	})
	if err != nil {
		t.Fatalf("second CreatePreviewApplication: %v", err)
	}
	if second.ID == created.ID {
		t.Error("two previews share one application row")
	}
}

func TestCreatePreviewApplicationRejectsUnusableInput(t *testing.T) {
	base := testApplication(uuid.New())
	base.BaseDomain = "app.example.com"

	cases := []struct {
		name  string
		input PreviewApplicationInput
		want  error
	}{
		{"missing name", PreviewApplicationInput{Branch: "feat/x", BaseDomain: "pr-1.app.example.com"}, ErrValidation},
		{"missing branch falls back to the base branch", PreviewApplicationInput{Name: "p-pr-1", BaseDomain: "pr-1.app.example.com"}, nil},
		{"invalid domain", PreviewApplicationInput{Name: "p-pr-2", Branch: "feat/x", BaseDomain: "not a domain"}, ErrValidation},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := &fakeRepository{app: base}
			svc := newTestService(t, repo)
			created, err := svc.CreatePreviewApplication(context.Background(), base.ID, tc.input)
			if tc.want == nil {
				if err != nil {
					t.Fatalf("CreatePreviewApplication: %v", err)
				}
				if created.Branch != base.Branch {
					t.Errorf("branch = %q, want the base branch %q", created.Branch, base.Branch)
				}
				return
			}
			if !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
			if len(repo.apps) != 0 {
				t.Errorf("stored %d applications, want none", len(repo.apps))
			}
		})
	}

	// The feature flag disables the system path too.
	t.Setenv(FeatureEnv, "false")
	repo := &fakeRepository{app: base}
	svc := newTestService(t, repo)
	if _, err := svc.CreatePreviewApplication(context.Background(), base.ID, PreviewApplicationInput{
		Name: "p-pr-1", Branch: "feat/x", BaseDomain: "pr-1.app.example.com",
	}); !errors.Is(err, ErrDisabled) {
		t.Fatalf("disabled CreatePreviewApplication = %v, want ErrDisabled", err)
	}
}

// TestDeleteSystemApplicationRemovesLocalKeyOnly is the F7 regression: the
// system teardown deletes the sibling's local deploy-key rows (the mapping and
// the sealed private key) but never touches the shared remote key.
func TestDeleteSystemApplicationRemovesLocalKeyOnly(t *testing.T) {
	base := testApplication(uuid.New())
	repo := seedBaseForPreview(t, base)
	registrar := &fakeRegistrar{}
	svc := newKeyService(t, repo, registrar)

	preview, err := svc.CreatePreviewApplication(context.Background(), base.ID, PreviewApplicationInput{
		Name: "demo app-pr-7", Branch: "feat/x", BaseDomain: "pr-7.app.example.com",
	})
	if err != nil {
		t.Fatalf("CreatePreviewApplication: %v", err)
	}
	if !repo.hasDeployKey(preview.ID) {
		t.Fatal("the preview has no local deploy key to clean up")
	}

	if err := svc.DeleteSystemApplication(context.Background(), preview.ID); err != nil {
		t.Fatalf("DeleteSystemApplication: %v", err)
	}
	if _, err := repo.GetApplication(context.Background(), preview.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("preview still exists: %v", err)
	}
	if repo.hasDeployKey(preview.ID) {
		t.Error("the preview's local deploy key survived the teardown")
	}
	if !repo.hasDeployKey(base.ID) {
		t.Error("the base deploy key was removed by the preview teardown")
	}
	registrar.mu.Lock()
	removed := len(registrar.removed)
	registrar.mu.Unlock()
	if removed != 0 {
		t.Errorf("remote removals = %d, want 0 (the key is shared with the base)", removed)
	}
	// Deleting again (a redelivered close, a sweep racing the close) is a
	// success.
	if err := svc.DeleteSystemApplication(context.Background(), preview.ID); err != nil {
		t.Fatalf("second DeleteSystemApplication: %v", err)
	}

	t.Setenv(FeatureEnv, "false")
	if err := svc.DeleteSystemApplication(context.Background(), base.ID); !errors.Is(err, ErrDisabled) {
		t.Fatalf("disabled DeleteSystemApplication = %v, want ErrDisabled", err)
	}
}

// TestDeleteSystemApplicationRefusesNonPreview pins the hardening check: the
// system teardown only accepts preview siblings (it skips the remote key
// detach), so an ordinary application can never be deleted through it.
func TestDeleteSystemApplicationRefusesNonPreview(t *testing.T) {
	base := testApplication(uuid.New())
	repo := &fakeRepository{app: base}
	svc := newTestService(t, repo)

	err := svc.DeleteSystemApplication(context.Background(), base.ID)
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("DeleteSystemApplication(base) = %v, want ErrValidation", err)
	}
	if _, err := repo.GetApplication(context.Background(), base.ID); err != nil {
		t.Fatalf("the base application was deleted: %v", err)
	}
}

// TestDeleteApplicationOfAPreviewKeepsTheRemoteKey is the H1 regression: the
// user-facing delete of a preview sibling removes the local rows but leaves
// the base application's remote deploy key registered.
func TestDeleteApplicationOfAPreviewKeepsTheRemoteKey(t *testing.T) {
	base := testApplication(uuid.New())
	repo := seedBaseForPreview(t, base)
	registrar := &fakeRegistrar{}
	svc := newKeyService(t, repo, registrar)

	preview, err := svc.CreatePreviewApplication(context.Background(), base.ID, PreviewApplicationInput{
		Name: "demo app-pr-7", Branch: "feat/x", BaseDomain: "pr-7.app.example.com",
	})
	if err != nil {
		t.Fatalf("CreatePreviewApplication: %v", err)
	}
	if err := svc.DeleteApplication(context.Background(), preview.UserID, preview.ID); err != nil {
		t.Fatalf("DeleteApplication(preview): %v", err)
	}
	registrar.mu.Lock()
	removed := len(registrar.removed)
	registrar.mu.Unlock()
	if removed != 0 {
		t.Errorf("remote removals = %d, want 0 (the preview shares the base's key)", removed)
	}
	if repo.hasDeployKey(preview.ID) {
		t.Error("the preview's local deploy key survived the user delete")
	}
	if !repo.hasDeployKey(base.ID) {
		t.Error("the base deploy key was removed")
	}
	if _, err := repo.GetApplication(context.Background(), base.ID); err != nil {
		t.Fatalf("base application after the preview delete: %v", err)
	}
}

// TestDeleteApplicationTearsDownPreviewsFirst is the F5 regression: the base
// delete runs the preview cleanup first and aborts when it fails, so the
// bindings are never cascaded away while a sibling survives.
func TestDeleteApplicationTearsDownPreviewsFirst(t *testing.T) {
	app := testApplication(uuid.New())
	repo := &fakeRepository{app: app}
	cleanupErr := error(nil)
	cleaned := 0
	svc := NewService(Config{
		Repository: repo,
		Secret:     testSecretKey,
		Logger:     discardLogger(),
		PreviewCleanup: func(_ context.Context, appID uuid.UUID) error {
			if appID != app.ID {
				t.Errorf("cleanup app = %s, want %s", appID, app.ID)
			}
			cleaned++
			return cleanupErr
		},
	})
	t.Cleanup(func() { _ = svc.Close() })

	// A failing cleanup aborts the delete and keeps the base application.
	cleanupErr = errors.New("preview teardown failed")
	if err := svc.DeleteApplication(context.Background(), app.UserID, app.ID); err == nil {
		t.Fatal("DeleteApplication with a failing preview cleanup: no error, want one")
	}
	if _, err := repo.GetApplication(context.Background(), app.ID); err != nil {
		t.Fatalf("base application was deleted despite the failed cleanup: %v", err)
	}
	if cleaned != 1 {
		t.Fatalf("cleanup calls = %d, want 1", cleaned)
	}

	// Retrying with a working cleanup deletes the base.
	cleanupErr = nil
	if err := svc.DeleteApplication(context.Background(), app.UserID, app.ID); err != nil {
		t.Fatalf("DeleteApplication(retry): %v", err)
	}
	if cleaned != 2 {
		t.Fatalf("cleanup calls = %d, want 2", cleaned)
	}
	if _, err := repo.GetApplication(context.Background(), app.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("application still exists: %v", err)
	}
}
