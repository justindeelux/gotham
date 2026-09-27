package providers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// fakeService is a deterministic ProviderService for route tests.
type fakeService struct {
	providers []Provider
	repos     []Repo
	listErr   error
	reposErr  error
}

func (f *fakeService) List(context.Context, uuid.UUID) ([]Provider, error) {
	return f.providers, f.listErr
}

func (f *fakeService) ListRepos(context.Context, uuid.UUID, uuid.UUID) ([]Repo, error) {
	return f.repos, f.reposErr
}

// newRouteServer mounts the provider routes with a no-op auth middleware and
// the given user accessor.
func newRouteServer(svc ProviderService, userID UserIDFunc) http.Handler {
	r := chi.NewRouter()
	auth := func(next http.Handler) http.Handler { return next }
	Mount(r, auth, userID, svc)
	return r
}

func alwaysUser(id uuid.UUID) UserIDFunc {
	return func(context.Context) (uuid.UUID, bool) { return id, true }
}

func TestRoutesListProviders(t *testing.T) {
	userID := uuid.New()
	providerID := uuid.New()
	svc := &fakeService{providers: []Provider{{
		ID: providerID, UserID: userID, Name: NameGitHub,
		AccessToken: "secret-token", ClientSecret: "secret-client",
	}}}
	srv := newRouteServer(svc, alwaysUser(userID))

	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/providers", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	var body providerListEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Providers) != 1 || body.Providers[0].ID != providerID.String() {
		t.Fatalf("body = %+v", body)
	}
	if body.Providers[0].Connected != true {
		t.Errorf("Connected = %v, want true", body.Providers[0].Connected)
	}
	if raw := rec.Body.String(); strings.Contains(raw, "secret-token") || strings.Contains(raw, "secret-client") {
		t.Errorf("response leaked credentials: %s", raw)
	}
}

func TestRoutesListRepos(t *testing.T) {
	userID := uuid.New()
	svc := &fakeService{repos: []Repo{{ExternalID: "1", FullName: "o/r", DefaultBranch: "main"}}}
	srv := newRouteServer(svc, alwaysUser(userID))

	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/providers/"+uuid.New().String()+"/repos", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	var body repoListEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Repos) != 1 || body.Repos[0].FullName != "o/r" {
		t.Fatalf("body = %+v", body)
	}
}

func TestRoutesInvalidProviderID(t *testing.T) {
	svc := &fakeService{}
	srv := newRouteServer(svc, alwaysUser(uuid.New()))

	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/providers/not-a-uuid/repos", nil))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestRoutesUnauthorized(t *testing.T) {
	svc := &fakeService{}
	srv := newRouteServer(svc, func(context.Context) (uuid.UUID, bool) { return uuid.Nil, false })

	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/providers", nil))

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestRoutesServiceErrors(t *testing.T) {
	userID := uuid.New()
	cases := []struct {
		name string
		err  error
		want int
	}{
		{"not found", ErrNotFound, http.StatusNotFound},
		{"not connected", ErrNotConnected, http.StatusConflict},
		{"unsupported", ErrUnsupported, http.StatusBadRequest},
		{"provider api", &httpError{provider: NameGitHub, url: "https://api.github.com", status: 500}, http.StatusBadGateway},
		{"internal", errors.New("boom"), http.StatusInternalServerError},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc := &fakeService{listErr: tc.err, reposErr: tc.err}
			srv := newRouteServer(svc, alwaysUser(userID))

			rec := httptest.NewRecorder()
			srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/providers", nil))
			if rec.Code != tc.want {
				t.Fatalf("status = %d, want %d (body %s)", rec.Code, tc.want, rec.Body.String())
			}
		})
	}
}

func TestMountNilService(t *testing.T) {
	srv := newRouteServer(nil, alwaysUser(uuid.New()))
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/providers", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}
