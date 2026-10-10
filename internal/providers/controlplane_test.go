package providers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// controlPlaneSource returns a ControlPlaneURLSource yielding base.
func controlPlaneSource(base string) ControlPlaneURLSource {
	return func(context.Context) string { return base }
}

// routeServerWithControlPlane mounts with the given source for route-level
// redirect-host tests.
func routeServerWithControlPlane(t *testing.T, svc ProviderService, userID UserIDFunc, src ControlPlaneURLSource) http.Handler {
	t.Helper()

	r := chi.NewRouter()
	auth := func(next http.Handler) http.Handler { return next }
	MountWithControlPlaneURL(r, auth, userID, svc, src)
	return r
}

// TestRedirectMatchesControlPlane pins the host check: the control-plane URL
// host wins when set (even when the request host differs), empty keeps the
// request-host behavior, and an unusable base falls back to the request host.
func TestRedirectMatchesControlPlane(t *testing.T) {
	withCP := &handler{controlPlaneURL: controlPlaneSource("https://cp.example:8443")}
	without := &handler{}
	invalidBase := &handler{controlPlaneURL: controlPlaneSource("://bad")}

	newRequest := func(host string) *http.Request {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Host = host
		return req
	}

	for _, tc := range []struct {
		name    string
		handler *handler
		host    string
		target  string
		want    bool
	}{
		{"cp match", withCP, "internal.local:8000", "https://cp.example/api/v1/providers/gitlab/callback", true},
		{"cp mismatch", withCP, "internal.local:8000", "https://evil.example/api/v1/providers/gitlab/callback", false},
		{"cp ignores request host", withCP, "cp.example", "https://other.example/x", false},
		{"cp relative rejected", withCP, "cp.example", "/api/v1/providers/gitlab/callback", false},
		{"unset falls back to host", without, "cp.example:8000", "https://cp.example/api/v1/providers/gitlab/callback", true},
		{"unset host mismatch", without, "cp.example", "https://other.example/x", false},
		{"invalid base falls back", invalidBase, "cp.example", "https://cp.example/x", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.handler.redirectMatches(newRequest(tc.host), tc.target); got != tc.want {
				t.Errorf("redirectMatches(host %q, %q) = %v, want %v", tc.host, tc.target, got, tc.want)
			}
		})
	}
}

// TestRoutesAutoProvisionUsesControlPlaneHost pins the route behavior behind
// a proxy: with the control-plane URL set, a redirect naming its host passes
// even though the request arrived on the internal host.
func TestRoutesAutoProvisionUsesControlPlaneHost(t *testing.T) {
	userID := uuid.New()
	svc := &fakeService{provisioned: Provider{
		ID: uuid.New(), UserID: userID, Name: NameGitLab,
	}}
	srv := routeServerWithControlPlane(t, svc, alwaysUser(userID), controlPlaneSource("https://cp.example"))

	body := `{"base_url":"https://git.example","admin_token":"one-time","redirect_url":"https://cp.example/api/v1/providers/gitlab/callback"}`
	req := httptest.NewRequest(http.MethodPost, "/v1/providers/gitlab/auto-provision", strings.NewReader(body))
	req.Host = "internal.local:8000"
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201 (body %s)", rec.Code, rec.Body.String())
	}

	foreign := `{"base_url":"https://git.example","admin_token":"one-time","redirect_url":"https://evil.example/api/v1/providers/gitlab/callback"}`
	req = httptest.NewRequest(http.MethodPost, "/v1/providers/gitlab/auto-provision", strings.NewReader(foreign))
	req.Host = "internal.local:8000"
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (body %s)", rec.Code, rec.Body.String())
	}
}
