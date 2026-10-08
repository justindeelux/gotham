package webhooks

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/providers"
)

// memProviderRepo is an in-memory providers.Repository, so the push test can
// run the real provider service without a database.
type memProviderRepo struct {
	mu        sync.Mutex
	providers map[uuid.UUID]providers.Provider
}

func newMemProviderRepo() *memProviderRepo {
	return &memProviderRepo{providers: make(map[uuid.UUID]providers.Provider)}
}

func (f *memProviderRepo) Create(_ context.Context, p providers.Provider) (providers.Provider, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if p.ID == uuid.Nil {
		p.ID = uuid.New()
	}
	f.providers[p.ID] = p
	return p, nil
}

func (f *memProviderRepo) Get(_ context.Context, id, userID uuid.UUID) (providers.Provider, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	p, ok := f.providers[id]
	if !ok || p.UserID != userID {
		return providers.Provider{}, providers.ErrNotFound
	}
	return p, nil
}

func (f *memProviderRepo) List(_ context.Context, userID uuid.UUID) ([]providers.Provider, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []providers.Provider
	for _, p := range f.providers {
		if p.UserID == userID {
			out = append(out, p)
		}
	}
	return out, nil
}

func (f *memProviderRepo) UpdateToken(_ context.Context, id uuid.UUID, accessToken, refreshToken string, expiresAt *time.Time) (providers.Provider, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	p, ok := f.providers[id]
	if !ok {
		return providers.Provider{}, providers.ErrNotFound
	}
	p.AccessToken = accessToken
	p.RefreshToken = refreshToken
	p.TokenExpiresAt = expiresAt
	f.providers[id] = p
	return p, nil
}

func (f *memProviderRepo) Delete(_ context.Context, id, userID uuid.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if p, ok := f.providers[id]; !ok || p.UserID != userID {
		return nil
	}
	delete(f.providers, id)
	return nil
}

func (f *memProviderRepo) ReplaceRepos(_ context.Context, _ uuid.UUID, _ []providers.Repo) error {
	return nil
}

func (f *memProviderRepo) ListCachedRepos(_ context.Context, _ uuid.UUID) ([]providers.Repo, error) {
	return nil, nil
}

// TestGitLabPushViaFakeGitLabQueuesDeploy is the GS-6 acceptance link: the
// hook is installed through the real provider service against a fake GitLab
// API, then a push signed with the installed secret queues a deployment.
func TestGitLabPushViaFakeGitLabQueuesDeploy(t *testing.T) {
	var installed map[string]any
	gitlab := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || !strings.HasSuffix(r.URL.Path, "/hooks") {
			http.Error(w, "unexpected "+r.Method+" "+r.URL.Path, http.StatusNotFound)
			return
		}
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			http.Error(w, "bad body", http.StatusBadRequest)
			return
		}
		installed = payload
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{"id": 9})
	}))
	t.Cleanup(gitlab.Close)

	providerRepo := newMemProviderRepo()
	// No factory override: the default GitLab source talks to the fake.
	providerSvc := providers.NewService(providers.Config{
		Repository: providerRepo, Logger: discardLogger(), AllowUnsafeBaseURL: true,
	})

	repo := newFakeRepositoryFor(providers.NameGitLab)
	if _, err := providerRepo.Create(context.Background(), providers.Provider{
		ID: uuid.New(), UserID: repo.app.UserID, Name: providers.NameGitLab,
		BaseURL: gitlab.URL, AccessToken: "token",
	}); err != nil {
		t.Fatalf("seed provider: %v", err)
	}

	deployer := &fakeDeployer{}
	svc := newTestServiceWith(Config{
		Repository: repo, Installer: providerSvc, Deployer: deployer,
		Provisioner: deployer, Logger: discardLogger(),
	})

	hook, err := svc.CreateWebhook(context.Background(), repo.app.UserID, repo.app.ID,
		"https://cp.example/api/v1/webhooks")
	if err != nil {
		t.Fatalf("CreateWebhook: %v", err)
	}
	if hook.HookID != "9" {
		t.Fatalf("hook id = %q, want the fake's 9", hook.HookID)
	}
	if installed["url"] != "https://cp.example/api/v1/webhooks/gitlab" ||
		installed["push_events"] != true || installed["token"] != repo.secret {
		t.Fatalf("installed hook = %v, want the callback URL, push events and the stored secret", installed)
	}

	body := gitLabPushBody("abc123")
	srv := newRouteServer(svc, repo.app.UserID)
	req := deliveryRequest(providers.NameGitLab, body, map[string]string{
		headerGitLabToken:    repo.secret,
		headerGitLabEvent:    "Push Hook",
		headerGitLabDelivery: "delivery-1",
	}, "10.0.0.1:4242")
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202 (body %s)", rec.Code, rec.Body.String())
	}
	if deployer.deployCount() != 1 {
		t.Errorf("deployments = %d, want 1", deployer.deployCount())
	}
}
