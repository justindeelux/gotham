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

	created     Provider
	createInput CreateProviderInput
	createErr   error

	authURL   string
	authState string
	authErr   error

	connected    Provider
	connectCode  string
	connectState string
	connectErr   error

	webhookID     string
	webhookErr    error
	webhook       Webhook
	webhookTarget HookTarget
	deletedHookID string
	commentNumber int
	commentBody   string

	branches    []Branch
	branchesErr error
	branchRepo  string

	deletedProviderID uuid.UUID
	deleteErr         error

	provisioned   Provider
	provisionErr  error
	provisionSeen AutoProvisionGitLabInput

	setupInfo   GitLabSetupInfo
	setupErr    error
	setupBase   string
	setupReturn string
}

func (f *fakeService) List(context.Context, uuid.UUID) ([]Provider, error) {
	return f.providers, f.listErr
}

// Create implements ProviderService.
func (f *fakeService) Create(_ context.Context, _ uuid.UUID, input CreateProviderInput) (Provider, error) {
	f.createInput = input
	if f.createErr != nil {
		return Provider{}, f.createErr
	}
	return f.created, nil
}

// Authorize implements ProviderService.
func (f *fakeService) Authorize(context.Context, uuid.UUID, uuid.UUID) (string, string, error) {
	return f.authURL, f.authState, f.authErr
}

// Connect implements ProviderService.
func (f *fakeService) Connect(_ context.Context, _, _ uuid.UUID, code, state string) (Provider, error) {
	f.connectCode, f.connectState = code, state
	if f.connectErr != nil {
		return Provider{}, f.connectErr
	}
	return f.connected, nil
}

// ConnectCallback implements ProviderService.
func (f *fakeService) ConnectCallback(_ context.Context, code, state string) (string, error) {
	f.connectCode, f.connectState = code, state
	if f.connectErr != nil {
		return "", f.connectErr
	}
	return f.connected.Name, nil
}

func (f *fakeService) ListRepos(context.Context, uuid.UUID, uuid.UUID) ([]Repo, error) {
	return f.repos, f.reposErr
}

// Delete implements ProviderService.
func (f *fakeService) Delete(_ context.Context, _ uuid.UUID, providerID uuid.UUID) error {
	f.deletedProviderID = providerID
	return f.deleteErr
}

// ListBranches implements ProviderService.
func (f *fakeService) ListBranches(_ context.Context, _ uuid.UUID, _ uuid.UUID, repo string) ([]Branch, error) {
	f.branchRepo = repo
	return f.branches, f.branchesErr
}

// AutoProvisionGitLab implements ProviderService.
func (f *fakeService) AutoProvisionGitLab(_ context.Context, _ uuid.UUID, input AutoProvisionGitLabInput) (Provider, error) {
	f.provisionSeen = input
	if f.provisionErr != nil {
		return Provider{}, f.provisionErr
	}
	return f.provisioned, nil
}

// GitLabSetupInfoFor implements ProviderService.
func (f *fakeService) GitLabSetupInfoFor(baseURL, redirectURL string) (GitLabSetupInfo, error) {
	f.setupBase, f.setupReturn = baseURL, redirectURL
	if f.setupErr != nil {
		return GitLabSetupInfo{}, f.setupErr
	}
	return f.setupInfo, nil
}

// CreateWebhook implements ProviderService. The webhook tests drive the real
// Service, so the route fake only reports the call.
func (f *fakeService) CreateWebhook(_ context.Context, target HookTarget, hook Webhook) (string, error) {
	f.webhookTarget, f.webhook = target, hook
	if f.webhookErr != nil {
		return "", f.webhookErr
	}
	return f.webhookID, nil
}

// DeleteWebhook implements ProviderService.
func (f *fakeService) DeleteWebhook(_ context.Context, target HookTarget, hookID string) error {
	f.webhookTarget, f.deletedHookID = target, hookID
	return f.webhookErr
}

// CreatePullRequestComment implements ProviderService.
func (f *fakeService) CreatePullRequestComment(_ context.Context, target HookTarget, number int, body string) error {
	f.webhookTarget, f.commentNumber, f.commentBody = target, number, body
	return f.webhookErr
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
		{"rate limited", ErrTooManyRequests, http.StatusTooManyRequests},
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

// TestRoutesCreateProvider is the C1-11 regression: the create route reaches
// the service and never echoes the client secret.
func TestRoutesCreateProvider(t *testing.T) {
	userID := uuid.New()
	providerID := uuid.New()
	svc := &fakeService{created: Provider{ID: providerID, UserID: userID, Name: NameGitHub}}
	srv := newRouteServer(svc, alwaysUser(userID))

	body := `{"provider":"github","client_id":"client-id","client_secret":"client-secret","redirect_url":"https://cp.example/oauth/callback","scopes":"repo"}`
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/providers", strings.NewReader(body)))

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201 (body %s)", rec.Code, rec.Body.String())
	}
	if svc.createInput.Name != NameGitHub || svc.createInput.ClientID != "client-id" || svc.createInput.Scopes != "repo" {
		t.Fatalf("create input = %+v", svc.createInput)
	}
	if strings.Contains(rec.Body.String(), "client-secret") {
		t.Errorf("response leaked the client secret: %s", rec.Body.String())
	}
}

// TestRoutesCreateProviderInvalidBody rejects an unknown field and trailing
// input.
func TestRoutesCreateProviderInvalidBody(t *testing.T) {
	svc := &fakeService{}
	srv := newRouteServer(svc, alwaysUser(uuid.New()))

	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/providers", strings.NewReader(`{"unknown":true}`)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

// TestRoutesAuthorize returns the provider URL and the binding state.
func TestRoutesAuthorize(t *testing.T) {
	userID := uuid.New()
	svc := &fakeService{authURL: "https://github.com/login/oauth/authorize?state=abc", authState: "abc"}
	srv := newRouteServer(svc, alwaysUser(userID))

	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/providers/"+uuid.New().String()+"/authorize", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	var body authorizeResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.State != "abc" || body.URL == "" {
		t.Fatalf("body = %+v", body)
	}
}

// TestRoutesConnect forwards the code and state and returns the connection.
func TestRoutesConnect(t *testing.T) {
	userID := uuid.New()
	providerID := uuid.New()
	svc := &fakeService{connected: Provider{
		ID: providerID, UserID: userID, Name: NameGitHub, AccessToken: "secret-token",
	}}
	srv := newRouteServer(svc, alwaysUser(userID))

	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/providers/"+providerID.String()+"/connect",
		strings.NewReader(`{"code":"the-code","state":"the-state"}`)))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	if svc.connectCode != "the-code" || svc.connectState != "the-state" {
		t.Fatalf("connect saw code=%q state=%q", svc.connectCode, svc.connectState)
	}
	if strings.Contains(rec.Body.String(), "secret-token") {
		t.Errorf("response leaked the access token: %s", rec.Body.String())
	}
}
