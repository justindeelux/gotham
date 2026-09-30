package updates

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
)

// fakeService is a deterministic Service for route tests.
type fakeService struct {
	release    *Release
	checkErr   error
	applied    []Channel
	applyErr   error
	lastStatus *Status
	resets     int
}

func (f *fakeService) Current() string { return "v1.0.0" }

func (f *fakeService) Check(context.Context) (*Release, error) { return f.release, f.checkErr }

func (f *fakeService) Apply(_ context.Context, channel Channel) (*ApplyResult, error) {
	f.applied = append(f.applied, channel)
	if f.applyErr != nil {
		return nil, f.applyErr
	}
	if f.release == nil {
		return &ApplyResult{Applied: false, Version: "v1.0.0", Message: "already up to date"}, nil
	}
	return &ApplyResult{Applied: true, Version: f.release.Version, Staged: true, Restart: true}, nil
}

func (f *fakeService) Rollback() error { return nil }

func (f *fakeService) Reset() error { f.resets++; return nil }

func (f *fakeService) Resume(context.Context) error { return nil }

func (f *fakeService) LastStatus() (*Status, error) { return f.lastStatus, nil }

func (f *fakeService) StartAuto(context.Context) {}

// identityAuth and deniedAuthMiddleware stand in for the server's RequireAuth
// and RequireAuth+RequirePlatformAdmin chains.
func identityAuth(next http.Handler) http.Handler { return next }

func deniedAuthMiddleware(_ http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusForbidden, errorBody{Message: "denied"})
	})
}

func allowAdmin(*http.Request) bool { return true }
func denyAdmin(*http.Request) bool  { return false }

// TestMountRoutes covers routing, the apply admin boundary and the disabled
// feature.
func TestMountRoutes(t *testing.T) {
	t.Run("check and apply", func(t *testing.T) {
		svc := &fakeService{release: &Release{
			Version:     "v1.2.0",
			Channel:     "stable",
			AssetName:   "gotham-linux-amd64",
			PublishedAt: time.Unix(0, 0),
		}}
		router := chi.NewRouter()
		Mount(router, identityAuth, identityAuth, allowAdmin, svc)

		check := httptest.NewRecorder()
		router.ServeHTTP(check, httptest.NewRequest(http.MethodGet, "/v1/updates/check", nil))
		if check.Code != http.StatusOK {
			t.Fatalf("check status = %d, want 200", check.Code)
		}
		var body checkResponse
		if err := json.Unmarshal(check.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode check: %v", err)
		}
		if !body.Available || body.Version != "v1.2.0" || body.Current != "v1.0.0" {
			t.Fatalf("check body = %+v", body)
		}

		apply := httptest.NewRecorder()
		router.ServeHTTP(apply, httptest.NewRequest(http.MethodPost, "/v1/updates/apply", nil))
		if apply.Code != http.StatusOK {
			t.Fatalf("apply status = %d, want 200", apply.Code)
		}
		var applied applyResponse
		if err := json.Unmarshal(apply.Body.Bytes(), &applied); err != nil {
			t.Fatalf("decode apply: %v", err)
		}
		if !applied.Applied || applied.Version != "v1.2.0" {
			t.Fatalf("apply body = %+v", applied)
		}
		if len(svc.applied) != 1 {
			t.Fatalf("apply calls = %d, want 1", len(svc.applied))
		}
	})

	t.Run("apply uses the admin chain", func(t *testing.T) {
		svc := &fakeService{}
		router := chi.NewRouter()
		Mount(router, identityAuth, deniedAuthMiddleware, denyAdmin, svc)

		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/updates/apply", nil))
		if rec.Code != http.StatusForbidden {
			t.Fatalf("apply status = %d, want 403", rec.Code)
		}
		if len(svc.applied) != 0 {
			t.Fatalf("service reached %d times through a denied admin chain", len(svc.applied))
		}
	})

	t.Run("check hides notes and detail from non-admins", func(t *testing.T) {
		svc := &fakeService{
			release:    &Release{Version: "v1.2.0", Channel: "stable", AssetName: "gotham-linux-amd64", Notes: "release notes"},
			lastStatus: &Status{Result: StatusRolledBack, Version: "v1.2.0", Detail: "/var/lib/gotham/bin/gotham unhealthy"},
		}
		router := chi.NewRouter()
		Mount(router, identityAuth, identityAuth, denyAdmin, svc)

		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/updates/check", nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("check status = %d, want 200", rec.Code)
		}
		var body checkResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode check: %v", err)
		}
		if body.Notes != "" {
			t.Errorf("non-admin saw notes %q", body.Notes)
		}
		if body.LastUpdate == nil || body.LastUpdate.Result != StatusRolledBack {
			t.Fatalf("last_update = %+v, want rolled_back", body.LastUpdate)
		}
		if body.LastUpdate.Detail != "" {
			t.Errorf("non-admin saw detail %q", body.LastUpdate.Detail)
		}
	})

	t.Run("check shows notes and detail to admins", func(t *testing.T) {
		svc := &fakeService{
			release:    &Release{Version: "v1.2.0", Channel: "stable", AssetName: "gotham-linux-amd64", Notes: "release notes"},
			lastStatus: &Status{Result: StatusRolledBack, Version: "v1.2.0", Detail: "health failed"},
		}
		router := chi.NewRouter()
		Mount(router, identityAuth, identityAuth, allowAdmin, svc)

		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/updates/check", nil))
		var body checkResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode check: %v", err)
		}
		if body.Notes != "release notes" || body.LastUpdate == nil || body.LastUpdate.Detail != "health failed" {
			t.Fatalf("admin body = %+v", body)
		}
	})

	t.Run("apply accepts a channel body", func(t *testing.T) {
		svc := &fakeService{}
		router := chi.NewRouter()
		Mount(router, identityAuth, identityAuth, allowAdmin, svc)

		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/updates/apply", strings.NewReader(`{"channel":"beta"}`)))
		if rec.Code != http.StatusOK {
			t.Fatalf("apply status = %d, want 200", rec.Code)
		}
		if len(svc.applied) != 1 || svc.applied[0] != ChannelBeta {
			t.Fatalf("applied channels = %v, want [beta]", svc.applied)
		}
	})

	t.Run("apply rejects a malformed body", func(t *testing.T) {
		router := chi.NewRouter()
		Mount(router, identityAuth, identityAuth, allowAdmin, &fakeService{})
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/updates/apply", strings.NewReader(`{"unknown":true}`)))
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("apply status = %d, want 400", rec.Code)
		}
	})

	t.Run("feature disabled unmounts", func(t *testing.T) {
		t.Setenv(FeatureEnv, "false")
		router := chi.NewRouter()
		Mount(router, identityAuth, identityAuth, allowAdmin, &fakeService{})

		for _, req := range []*http.Request{
			httptest.NewRequest(http.MethodGet, "/v1/updates/check", nil),
			httptest.NewRequest(http.MethodPost, "/v1/updates/apply", nil),
		} {
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)
			if rec.Code != http.StatusNotFound {
				t.Fatalf("%s status = %d, want 404", req.URL.Path, rec.Code)
			}
		}
	})

	t.Run("nil service unmounts", func(t *testing.T) {
		router := chi.NewRouter()
		Mount(router, identityAuth, identityAuth, allowAdmin, nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/updates/check", nil))
		if rec.Code != http.StatusNotFound {
			t.Fatalf("check status = %d, want 404", rec.Code)
		}
	})
}

// TestCheckErrorMapping proves upstream failures surface as bad gateway, a
// missing key as unavailable and a pending update as a conflict.
func TestCheckErrorMapping(t *testing.T) {
	cases := []struct {
		err  error
		want int
	}{
		{ErrHTTP, http.StatusBadGateway},
		{ErrAssetNotFound, http.StatusBadGateway},
		{ErrManifest, http.StatusBadGateway},
		{ErrChecksumMismatch, http.StatusBadGateway},
		{ErrNoPublicKey, http.StatusServiceUnavailable},
		{ErrUpdatePending, http.StatusConflict},
		{context.DeadlineExceeded, http.StatusGatewayTimeout},
	}
	for _, tc := range cases {
		router := chi.NewRouter()
		Mount(router, identityAuth, identityAuth, allowAdmin, &fakeService{checkErr: tc.err})
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/updates/check", nil))
		if rec.Code != tc.want {
			t.Errorf("err %v status = %d, want %d", tc.err, rec.Code, tc.want)
		}
	}
}
