package webhooks

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/providers"
)

func TestCreateWebhookRejectsUnusableApplication(t *testing.T) {
	cases := []struct {
		name     string
		mutate   func(*fakeRepository)
		callback string
		want     error
	}{
		{
			name:     "unsupported provider",
			mutate:   func(r *fakeRepository) { r.app.Provider = "bitbucket" },
			callback: "https://cp.example/api/v1/webhooks",
			want:     ErrValidation,
		},
		{
			name:     "application without a repository",
			mutate:   func(r *fakeRepository) { r.app.Repo = "" },
			callback: "https://cp.example/api/v1/webhooks",
			want:     ErrValidation,
		},
		{
			name:     "callback without a host",
			mutate:   func(r *fakeRepository) {},
			callback: "/api/v1/webhooks",
			want:     ErrValidation,
		},
		{
			name:     "callback with a non-http scheme",
			mutate:   func(r *fakeRepository) {},
			callback: "ftp://cp.example/api/v1/webhooks",
			want:     ErrValidation,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := newFakeRepository()
			tc.mutate(repo)
			installer := &fakeInstaller{}
			svc := newTestService(repo, installer, &fakeDeployer{})

			_, err := svc.CreateWebhook(context.Background(), repo.app.UserID, repo.app.ID, tc.callback)
			if !errors.Is(err, tc.want) {
				t.Fatalf("error = %v, want %v", err, tc.want)
			}
			if len(installer.created) != 0 {
				t.Errorf("provider installs = %d, want 0", len(installer.created))
			}
			if repo.hook != nil {
				t.Errorf("stored hook = %+v, want none", repo.hook)
			}
		})
	}
}

func TestCreateWebhookRollsBackWhenStoreFails(t *testing.T) {
	repo := newFakeRepository()
	repo.createErr = errors.New("database down")
	installer := &fakeInstaller{}
	svc := newTestService(repo, installer, &fakeDeployer{})

	_, err := svc.CreateWebhook(context.Background(), repo.app.UserID, repo.app.ID,
		"https://cp.example/api/v1/webhooks")
	if err == nil {
		t.Fatal("CreateWebhook: no error, want the store failure to surface")
	}
	if len(installer.created) != 1 {
		t.Fatalf("provider installs = %d, want 1", len(installer.created))
	}
	if len(installer.deleted) != 1 {
		t.Errorf("provider rollbacks = %d, want 1 (the host must not keep an unverified hook)",
			len(installer.deleted))
	}
}

func TestCreateWebhookWrapsProviderFailure(t *testing.T) {
	repo := newFakeRepository()
	installer := &fakeInstaller{createErr: errors.New("502 bad gateway")}
	svc := newTestService(repo, installer, &fakeDeployer{})

	_, err := svc.CreateWebhook(context.Background(), repo.app.UserID, repo.app.ID,
		"https://cp.example/api/v1/webhooks")
	if !errors.Is(err, ErrProvider) {
		t.Fatalf("error = %v, want ErrProvider", err)
	}
	if !strings.Contains(err.Error(), providers.NameGitHub) {
		t.Errorf("error = %v, want it to name the provider", err)
	}
}

func TestDeleteWebhookWithoutHookIsNoop(t *testing.T) {
	repo := newFakeRepository()
	installer := &fakeInstaller{}
	svc := newTestService(repo, installer, &fakeDeployer{})

	deleted, err := svc.DeleteWebhook(context.Background(), repo.app.UserID, repo.app.ID)
	if err != nil {
		t.Fatalf("DeleteWebhook: %v", err)
	}
	if deleted {
		t.Error("deleted = true, want false")
	}
	if len(installer.deleted) != 0 {
		t.Errorf("provider deletions = %d, want 0", len(installer.deleted))
	}
}

func TestDeleteWebhookForeignApplicationIsNotFound(t *testing.T) {
	repo := newFakeRepository()
	svc := newTestService(repo, &fakeInstaller{}, &fakeDeployer{})

	_, err := svc.DeleteWebhook(context.Background(), uuid.New(), repo.app.ID)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("error = %v, want ErrNotFound", err)
	}
}

// TestInstallHookDerivesCallbackFromRequest pins the deploy.HookLifecycle
// adapter: the callback origin comes from the create request exactly like the
// explicit route, so an application created through the API installs a hook
// the Git host can reach. Repeating the install is idempotent.
func TestInstallHookDerivesCallbackFromRequest(t *testing.T) {
	repo := newFakeRepository()
	installer := &fakeInstaller{}
	svc := newTestService(repo, installer, &fakeDeployer{})

	req := httptest.NewRequest(http.MethodPost, "http://internal:8000/api/v1/applications", nil)
	req.Host = "cp.example.com"
	req.Header.Set("X-Forwarded-Proto", "https")

	if err := svc.InstallHook(context.Background(), repo.app.UserID, repo.app.ID, req); err != nil {
		t.Fatalf("InstallHook: %v", err)
	}
	if len(installer.created) != 1 {
		t.Fatalf("provider installs = %d, want 1", len(installer.created))
	}
	if got, want := installer.created[0].URL, "https://cp.example.com/api/v1/webhooks/github"; got != want {
		t.Errorf("hook url = %q, want %q", got, want)
	}
	// The application already has a hook: the second install returns it.
	if err := svc.InstallHook(context.Background(), repo.app.UserID, repo.app.ID, req); err != nil {
		t.Fatalf("second InstallHook: %v", err)
	}
	if len(installer.created) != 1 {
		t.Errorf("provider installs = %d after a repeat, want 1", len(installer.created))
	}
}

// TestInstallHookSurfacesProviderFailure pins that the adapter does not
// swallow a Git-host failure: the deploy create path logs it (the application
// row is already committed) and the explicit route retries.
func TestInstallHookSurfacesProviderFailure(t *testing.T) {
	repo := newFakeRepository()
	installer := &fakeInstaller{createErr: errors.New("502 bad gateway")}
	svc := newTestService(repo, installer, &fakeDeployer{})
	req := httptest.NewRequest(http.MethodPost, "http://cp.example.com/api/v1/applications", nil)

	err := svc.InstallHook(context.Background(), repo.app.UserID, repo.app.ID, req)
	if !errors.Is(err, ErrProvider) {
		t.Fatalf("error = %v, want ErrProvider", err)
	}
}

// TestRemoveHookIsIdempotent pins the delete-side adapter: the provider hook
// and the stored row go together, and a second removal is a success.
func TestRemoveHookIsIdempotent(t *testing.T) {
	repo := newFakeRepository().withTarget()
	installer := &fakeInstaller{}
	svc := newTestService(repo, installer, &fakeDeployer{})
	hookID := repo.hook.HookID

	if err := svc.RemoveHook(context.Background(), repo.app.UserID, repo.app.ID); err != nil {
		t.Fatalf("RemoveHook: %v", err)
	}
	if len(installer.deleted) != 1 || installer.deleted[0] != hookID {
		t.Fatalf("provider deletions = %v, want one for %s", installer.deleted, hookID)
	}
	if err := svc.RemoveHook(context.Background(), repo.app.UserID, repo.app.ID); err != nil {
		t.Fatalf("second RemoveHook: %v", err)
	}
	if len(installer.deleted) != 1 {
		t.Errorf("provider deletions = %d after a repeat, want 1", len(installer.deleted))
	}
}

func TestReceiveIsNotConfiguredWithoutSeams(t *testing.T) {
	svc := NewService(Config{Logger: discardLogger()})
	req := deliveryRequest(providers.NameGitHub, "{}", nil, "10.0.0.1:1")
	if _, err := svc.Receive(context.Background(), providers.NameGitHub, req); err == nil {
		t.Fatal("Receive: no error, want a configuration failure")
	}
}

func TestNewDefaultServiceNeedsEveryDependency(t *testing.T) {
	repo := newFakeRepository()
	if NewDefaultService(Config{Repository: repo, Logger: discardLogger()}) != nil {
		t.Error("service without an installer or deployer, want nil")
	}
	if NewDefaultService(Config{Installer: &fakeInstaller{}, Deployer: &fakeDeployer{}}) != nil {
		t.Error("service without a repository, want nil")
	}
	if NewDefaultService(Config{
		Repository: repo,
		Installer:  &fakeInstaller{},
		Deployer:   &fakeDeployer{},
	}) == nil {
		t.Error("fully configured service, want non-nil")
	}
}
