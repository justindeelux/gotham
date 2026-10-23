package providers

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"golang.org/x/oauth2"
)

var staticToken = &oauth2.Token{AccessToken: "test-token", TokenType: "Bearer"}

func writeJSONTest(t *testing.T, w http.ResponseWriter, payload any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		t.Fatalf("encode response: %v", err)
	}
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

type fakeRepo struct {
	providers map[uuid.UUID]Provider
	cached    map[uuid.UUID][]Repo
	replaced  map[uuid.UUID][]Repo
	replaceFn func(providerID uuid.UUID, repos []Repo) error
	createErr error

	updateTokenCalls int
}

func newFakeRepo() *fakeRepo {
	return &fakeRepo{
		providers: make(map[uuid.UUID]Provider),
		cached:    make(map[uuid.UUID][]Repo),
		replaced:  make(map[uuid.UUID][]Repo),
	}
}

func (f *fakeRepo) Create(_ context.Context, p Provider) (Provider, error) {
	if f.createErr != nil {
		return Provider{}, f.createErr
	}
	if p.ID == uuid.Nil {
		p.ID = uuid.New()
	}
	f.providers[p.ID] = p
	return p, nil
}

func (f *fakeRepo) Get(_ context.Context, id, userID uuid.UUID) (Provider, error) {
	p, ok := f.providers[id]
	if !ok || p.UserID != userID {
		return Provider{}, ErrNotFound
	}
	return p, nil
}

func (f *fakeRepo) List(_ context.Context, userID uuid.UUID) ([]Provider, error) {
	out := make([]Provider, 0)
	for _, p := range f.providers {
		if p.UserID == userID {
			out = append(out, p)
		}
	}
	return out, nil
}

func (f *fakeRepo) UpdateToken(ctx context.Context, id uuid.UUID, accessToken, refreshToken string, expiresAt *time.Time) (Provider, error) {
	if err := ctx.Err(); err != nil {
		return Provider{}, err
	}
	p, ok := f.providers[id]
	if !ok {
		return Provider{}, ErrNotFound
	}
	f.updateTokenCalls++
	p.AccessToken = accessToken
	p.RefreshToken = refreshToken
	p.TokenExpiresAt = expiresAt
	f.providers[id] = p
	return p, nil
}

func (f *fakeRepo) ReplaceRepos(_ context.Context, providerID uuid.UUID, repos []Repo) error {
	if f.replaceFn != nil {
		if err := f.replaceFn(providerID, repos); err != nil {
			return err
		}
	}
	f.replaced[providerID] = repos
	f.cached[providerID] = repos
	return nil
}

func (f *fakeRepo) ListCachedRepos(_ context.Context, providerID uuid.UUID) ([]Repo, error) {
	return f.cached[providerID], nil
}

func (f *fakeRepo) Delete(_ context.Context, id, userID uuid.UUID) error {
	p, ok := f.providers[id]
	if !ok || p.UserID != userID {
		return nil
	}
	delete(f.providers, id)
	delete(f.cached, id)
	return nil
}

func serve(t *testing.T, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return srv
}

func newTestService(repo Repository) *Service {
	return NewService(Config{Repository: repo, Logger: discardLogger(), AllowUnsafeBaseURL: true})
}
