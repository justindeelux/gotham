package providers

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// TestRoutesDeleteProvider forgets the connection and answers idempotently.
func TestRoutesDeleteProvider(t *testing.T) {
	userID := uuid.New()
	providerID := uuid.New()
	svc := &fakeService{}
	srv := newRouteServer(svc, alwaysUser(userID))

	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, "/v1/providers/"+providerID.String(), nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	if svc.deletedProviderID != providerID {
		t.Fatalf("deleted = %v, want %v", svc.deletedProviderID, providerID)
	}
	var body deleteEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !body.Deleted {
		t.Fatalf("body = %+v, want deleted", body)
	}
}

// TestRoutesListBranches returns the branches of the requested repo.
func TestRoutesListBranches(t *testing.T) {
	userID := uuid.New()
	svc := &fakeService{branches: []Branch{{Name: "main", Commit: "abc", Protected: true}}}
	srv := newRouteServer(svc, alwaysUser(userID))

	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet,
		"/v1/providers/"+uuid.New().String()+"/branches?repo=acme%2Fdemo", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	if svc.branchRepo != "acme/demo" {
		t.Fatalf("repo = %q, want acme/demo", svc.branchRepo)
	}
	var body branchListEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Branches) != 1 || body.Branches[0].Name != "main" {
		t.Fatalf("body = %+v", body)
	}
}

// TestRoutesListBranchesRequiresRepo answers 400 without the repo query.
func TestRoutesListBranchesRequiresRepo(t *testing.T) {
	srv := newRouteServer(&fakeService{}, alwaysUser(uuid.New()))

	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet,
		"/v1/providers/"+uuid.New().String()+"/branches", nil))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

// TestRoutesGitLabAutoProvision stores the provisioned connection and never
// echoes credentials.
func TestRoutesGitLabAutoProvision(t *testing.T) {
	userID := uuid.New()
	svc := &fakeService{provisioned: Provider{
		ID: uuid.New(), UserID: userID, Name: NameGitLab,
		ClientID: "app-id", ClientSecret: "app-secret",
		AccessToken: "access", RefreshToken: "refresh",
	}}
	srv := newRouteServer(svc, alwaysUser(userID))

	body := `{"base_url":"https://git.example","admin_token":"one-time","redirect_url":"https://cp.example/api/v1/providers/gitlab/callback"}`
	req := httptest.NewRequest(http.MethodPost,
		"/v1/providers/gitlab/auto-provision", strings.NewReader(body))
	req.Host = "cp.example"
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201 (body %s)", rec.Code, rec.Body.String())
	}
	if svc.provisionSeen.AdminToken != "one-time" || svc.provisionSeen.RedirectURL != "https://cp.example/api/v1/providers/gitlab/callback" {
		t.Fatalf("provision input = %+v", svc.provisionSeen)
	}
	for _, leaked := range []string{"one-time", "app-secret", "access", "refresh"} {
		if strings.Contains(rec.Body.String(), leaked) {
			t.Errorf("response leaked %q: %s", leaked, rec.Body.String())
		}
	}
}

// TestRoutesGitLabSetupInfo returns the manual-application details.
func TestRoutesGitLabSetupInfo(t *testing.T) {
	svc := &fakeService{setupInfo: GitLabSetupInfo{
		BaseURL: "https://git.example", RedirectURI: "https://cp.example/api/v1/providers/gitlab/callback",
		Scopes: gitLabDefaultProvisionScopes,
	}}
	srv := newRouteServer(svc, alwaysUser(uuid.New()))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet,
		"/v1/providers/gitlab/setup-info?base_url=https%3A%2F%2Fgit.example&redirect_url=https%3A%2F%2Fcp.example%2Fapi%2Fv1%2Fproviders%2Fgitlab%2Fcallback", nil)
	req.Host = "cp.example"
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	var body gitLabSetupInfoResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.RedirectURI != "https://cp.example/api/v1/providers/gitlab/callback" || body.Scopes != gitLabDefaultProvisionScopes {
		t.Fatalf("body = %+v", body)
	}
	if svc.setupBase != "https://git.example" || svc.setupReturn != "https://cp.example/api/v1/providers/gitlab/callback" {
		t.Fatalf("setup args = %q/%q", svc.setupBase, svc.setupReturn)
	}
}

// TestRoutesGitLabCallbackRedirects completes the browser flow with a fixed
// result flag instead of a session.
func TestRoutesGitLabCallbackRedirects(t *testing.T) {
	svc := &fakeService{connected: Provider{Name: NameGitLab}}
	srv := newRouteServer(svc, alwaysUser(uuid.New()))

	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet,
		"/v1/providers/gitlab/callback?code=abc&state=xyz", nil))
	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302 (body %s)", rec.Code, rec.Body.String())
	}
	location := rec.Header().Get("Location")
	if location != "/providers/callback?status=ok&provider=gitlab" {
		t.Fatalf("location = %q", location)
	}
	if svc.connectCode != "abc" || svc.connectState != "xyz" {
		t.Fatalf("callback args = %q/%q", svc.connectCode, svc.connectState)
	}
}

// TestRoutesGitLabCallbackFailure carries fixed reason codes, never detail.
func TestRoutesGitLabCallbackFailure(t *testing.T) {
	cases := []struct {
		name   string
		err    error
		reason string
	}{
		{"invalid state", ErrValidation, "invalid"},
		{"exchange down", errors.New("boom"), "failed"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc := &fakeService{connectErr: tc.err}
			srv := newRouteServer(svc, alwaysUser(uuid.New()))

			rec := httptest.NewRecorder()
			srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet,
				"/v1/providers/gitlab/callback?code=abc&state=xyz", nil))
			if rec.Code != http.StatusFound {
				t.Fatalf("status = %d, want 302", rec.Code)
			}
			want := "/providers/callback?status=error&reason=" + tc.reason
			if location := rec.Header().Get("Location"); location != want {
				t.Fatalf("location = %q, want %q", location, want)
			}
		})
	}
}

// TestRoutesDeleteConflict answers 409 when applications still use the
// connection, with a curated body naming them and no internal prefix.
func TestRoutesDeleteConflict(t *testing.T) {
	svc := &fakeService{deleteErr: fmt.Errorf("%w: shop, blog", ErrInUse)}
	srv := newRouteServer(svc, alwaysUser(uuid.New()))

	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, "/v1/providers/"+uuid.New().String(), nil))
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409 (body %s)", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if strings.Contains(body, "providers:") {
		t.Errorf("body leaks the internal prefix: %s", body)
	}
	if !strings.Contains(body, "shop, blog") {
		t.Errorf("body names no applications: %s", body)
	}
}

// TestRoutesProvisionRejectsForeignRedirect refuses a redirect URL that does
// not name this control plane.
func TestRoutesProvisionRejectsForeignRedirect(t *testing.T) {
	srv := newRouteServer(&fakeService{}, alwaysUser(uuid.New()))

	body := `{"base_url":"https://git.example","admin_token":"one-time","redirect_url":"https://evil.example/cb"}`
	req := httptest.NewRequest(http.MethodPost,
		"/v1/providers/gitlab/auto-provision", strings.NewReader(body))
	req.Host = "cp.example"
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

// TestRoutesSetupInfoRejectsForeignRedirect refuses a redirect URL that does
// not name this control plane.
func TestRoutesSetupInfoRejectsForeignRedirect(t *testing.T) {
	srv := newRouteServer(&fakeService{}, alwaysUser(uuid.New()))

	req := httptest.NewRequest(http.MethodGet,
		"/v1/providers/gitlab/setup-info?redirect_url=https%3A%2F%2Fevil.example%2Fcb", nil)
	req.Host = "cp.example"
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}
