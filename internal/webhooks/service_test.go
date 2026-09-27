package webhooks

import (
	"context"
	"errors"
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
