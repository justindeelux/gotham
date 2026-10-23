package webhooks

import (
	"context"
	"crypto/tls"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/clientip"
	"github.com/justindeelux/gotham/internal/deploy"
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

func TestInstallHookDerivesCallbackFromRequest(t *testing.T) {
	repo := newFakeRepository()
	installer := &fakeInstaller{}
	svc := newTestService(repo, installer, &fakeDeployer{})

	req := httptest.NewRequest(http.MethodPost, "http://internal:8000/api/v1/applications", nil)
	req.Host = "cp.example.com"
	req.TLS = &tls.ConnectionState{}

	if err := svc.InstallHook(context.Background(), repo.app.UserID, repo.app.ID, req); err != nil {
		t.Fatalf("InstallHook: %v", err)
	}
	if len(installer.created) != 1 {
		t.Fatalf("provider installs = %d, want 1", len(installer.created))
	}
	if got, want := installer.created[0].URL, "https://cp.example.com/api/v1/webhooks/github"; got != want {
		t.Errorf("hook url = %q, want %q", got, want)
	}
	if err := svc.InstallHook(context.Background(), repo.app.UserID, repo.app.ID, req); err != nil {
		t.Fatalf("second InstallHook: %v", err)
	}
	if len(installer.created) != 1 {
		t.Errorf("provider installs = %d after a repeat, want 1", len(installer.created))
	}
}

func TestInstallHookIgnoresUntrustedForwardedScheme(t *testing.T) {
	repo := newFakeRepository()
	installer := &fakeInstaller{}
	svc := newTestService(repo, installer, &fakeDeployer{})

	req := httptest.NewRequest(http.MethodPost, "http://internal:8000/api/v1/applications", nil)
	req.Host = "cp.example.com"
	req.RemoteAddr = "203.0.113.7:1234"
	req.Header.Set("X-Forwarded-Proto", "https")

	if err := svc.InstallHook(context.Background(), repo.app.UserID, repo.app.ID, req); err != nil {
		t.Fatalf("InstallHook: %v", err)
	}
	if len(installer.created) != 1 {
		t.Fatalf("provider installs = %d, want 1", len(installer.created))
	}
	if got, want := installer.created[0].URL, "http://cp.example.com/api/v1/webhooks/github"; got != want {
		t.Errorf("hook url = %q, want %q", got, want)
	}
}

func TestInstallHookTrustedProxyScheme(t *testing.T) {
	repo := newFakeRepository()
	installer := &fakeInstaller{}
	trusted, err := clientip.Parse([]string{"127.0.0.1"})
	if err != nil {
		t.Fatalf("clientip.Parse: %v", err)
	}
	svc := newTestServiceWith(Config{
		Repository:     repo,
		Installer:      installer,
		Deployer:       &fakeDeployer{},
		Logger:         discardLogger(),
		TrustedProxies: trusted,
	})

	req := httptest.NewRequest(http.MethodPost, "http://internal:8000/api/v1/applications", nil)
	req.Host = "cp.example.com"
	req.RemoteAddr = "127.0.0.1:5000"
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
}

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

func TestForgetWebhookDropsRowWithoutContactingProvider(t *testing.T) {
	repo := newFakeRepository().withTarget()
	installer := &fakeInstaller{}
	svc := newTestService(repo, installer, &fakeDeployer{})

	deleted, err := svc.ForgetWebhook(context.Background(), repo.app.UserID, repo.app.ID)
	if err != nil {
		t.Fatalf("ForgetWebhook: %v", err)
	}
	if !deleted {
		t.Error("deleted = false, want the stored row removed")
	}
	if repo.hook != nil {
		t.Errorf("stored hook = %+v, want it forgotten", repo.hook)
	}
	if len(installer.deleted) != 0 {
		t.Errorf("provider deletions = %v, want none (force must not contact the host)", installer.deleted)
	}

	deleted, err = svc.ForgetWebhook(context.Background(), repo.app.UserID, repo.app.ID)
	if err != nil {
		t.Fatalf("second ForgetWebhook: %v", err)
	}
	if deleted {
		t.Error("deleted = true on the second call, want false")
	}
}

type blockingDeleteInstaller struct {
	calls atomic.Int32
}

func (i *blockingDeleteInstaller) CreateWebhook(context.Context, providers.HookTarget, providers.Webhook) (string, error) {
	return "hook-1", nil
}

func (i *blockingDeleteInstaller) DeleteWebhook(ctx context.Context, _ providers.HookTarget, _ string) error {
	i.calls.Add(1)
	<-ctx.Done()
	return ctx.Err()
}

func TestForgetWebhookDoesNotWaitOnAStalledProvider(t *testing.T) {
	repo := newFakeRepository().withTarget()
	installer := &blockingDeleteInstaller{}
	svc := newTestServiceWith(Config{
		Repository: repo,
		Installer:  installer,
		Deployer:   &fakeDeployer{},
	})

	done := make(chan error, 1)
	go func() {
		_, err := svc.ForgetWebhook(context.Background(), repo.app.UserID, repo.app.ID)
		done <- err
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("ForgetWebhook: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("ForgetWebhook blocked on the stalled provider")
	}
	if calls := installer.calls.Load(); calls != 0 {
		t.Errorf("provider calls = %d, want 0 (force must not contact the host)", calls)
	}
	if repo.hook != nil {
		t.Error("the stored hook was not forgotten")
	}
}

type cancelOnCreateInstaller struct {
	cancel  context.CancelFunc
	deleted []string
}

func (i *cancelOnCreateInstaller) CreateWebhook(context.Context, providers.HookTarget, providers.Webhook) (string, error) {
	i.cancel()
	return "hook-1", nil
}

func (i *cancelOnCreateInstaller) DeleteWebhook(ctx context.Context, _ providers.HookTarget, hookID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	i.deleted = append(i.deleted, hookID)
	return nil
}

func TestCreateWebhookRollbackSurvivesExpiredInstallContext(t *testing.T) {
	repo := newFakeRepository()
	repo.createErr = errors.New("database down")
	ctx, cancel := context.WithCancel(context.Background())
	installer := &cancelOnCreateInstaller{cancel: cancel}
	svc := newTestServiceWith(Config{
		Repository: repo,
		Installer:  installer,
		Deployer:   &fakeDeployer{},
	})

	if _, err := svc.CreateWebhook(ctx, repo.app.UserID, repo.app.ID,
		"https://cp.example/api/v1/webhooks"); err == nil {
		t.Fatal("CreateWebhook: no error, want the store failure to surface")
	}
	if len(installer.deleted) != 1 {
		t.Fatalf("rollback deletions = %v, want the remote hook removed on a detached context",
			installer.deleted)
	}
}

func TestRemoveHookTranslatesProviderFailures(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want error
	}{
		{"not connected", providers.ErrNotConnected, deploy.ErrNotConnected},
		{"provider call failure", io.ErrUnexpectedEOF, deploy.ErrProvider},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := newFakeRepository().withTarget()
			svc := newTestService(repo, &fakeInstaller{deleteErr: tc.err}, &fakeDeployer{})

			err := svc.RemoveHook(context.Background(), repo.app.UserID, repo.app.ID)
			if !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
			if repo.hook == nil {
				t.Error("the stored hook was removed although the host still has it")
			}
		})
	}
}

func TestHookBudgetsStayUnderSPARequestTimeout(t *testing.T) {
	const spaRequestTimeout = 15 * time.Second
	worst := deploy.DefaultHookTimeout + hookRollbackTimeout
	if worst >= spaRequestTimeout {
		t.Fatalf("worst-case hook latency %s reaches the SPA request timeout %s", worst, spaRequestTimeout)
	}
	if worst > 12*time.Second {
		t.Errorf("worst-case hook latency %s leaves under 3s of headroom", worst)
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
