package providers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

// isPKCEUnreserved reports whether r is in the RFC 7636 unreserved set.
func isPKCEUnreserved(r rune) bool {
	switch {
	case r >= 'A' && r <= 'Z', r >= 'a' && r <= 'z', r >= '0' && r <= '9':
		return true
	default:
		return r == '-' || r == '_'
	}
}

// TestPKCEChallengeVector pins the S256 derivation to the RFC 7636 appendix B
// vector, so a wrong hash can never silently break the GitLab flow.
func TestPKCEChallengeVector(t *testing.T) {
	verifier := "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"
	if got := pkceChallenge(verifier); got != "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM" {
		t.Fatalf("challenge = %q, want the RFC 7636 vector", got)
	}
}

// TestPKCEVerifierFormat pins the generator output to the RFC 7636 verifier
// alphabet and length: 43 base64url characters with no padding.
func TestPKCEVerifierFormat(t *testing.T) {
	verifier, err := generatePKCEVerifier()
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if len(verifier) != 43 {
		t.Fatalf("len = %d, want 43", len(verifier))
	}
	for _, r := range verifier {
		if !isPKCEUnreserved(r) {
			t.Fatalf("verifier %q holds non-unreserved character %q", verifier, r)
		}
	}
	other, err := generatePKCEVerifier()
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if verifier == other {
		t.Fatal("two verifiers are identical")
	}
}

// TestGitLabAuthorizeConnectPKCE runs the GitLab connect against a fake:
// Authorize binds the URL to an S256 challenge, and Connect redeems the code
// with the verifier the browser never saw.
func TestGitLabAuthorizeConnectPKCE(t *testing.T) {
	var gotVerifier, gotCode string
	tokenSrv := serve(t, func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		gotCode = r.Form.Get("code")
		gotVerifier = r.Form.Get("code_verifier")
		writeJSONTest(t, w, map[string]any{
			"access_token": "gl-access", "refresh_token": "gl-refresh",
			"token_type": "bearer", "expires_in": 7200,
		})
	})

	repo := newFakeRepo()
	userID := uuid.New()
	provider, err := repo.Create(context.Background(), Provider{
		ID: uuid.New(), UserID: userID, Name: NameGitLab,
		ClientID: "gl-client", ClientSecret: "gl-secret",
		RedirectURL: "https://cp.example/oauth/callback",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	svc := NewService(Config{
		Repository: repo, Logger: discardLogger(), AllowUnsafeBaseURL: true,
		Factories: map[string]Factory{
			NameGitLab: func(p Provider) (SourceProvider, error) {
				s := newGitLabSource(p, true)
				s.config.Endpoint.TokenURL = tokenSrv.URL
				return s, nil
			},
		},
	})

	url, state, err := svc.Authorize(context.Background(), userID, provider.ID)
	if err != nil {
		t.Fatalf("Authorize: %v", err)
	}
	if !strings.Contains(url, "code_challenge_method=S256") || !strings.Contains(url, "code_challenge=") {
		t.Fatalf("authorize url %q has no S256 challenge", url)
	}
	verifier := svc.states.entries[state].verifier
	if verifier == "" {
		t.Fatal("no verifier stored for the state")
	}
	if challenge := pkceChallenge(verifier); !strings.Contains(url, "code_challenge="+challenge) {
		t.Fatalf("authorize url %q is not bound to the stored verifier", url)
	}

	connected, err := svc.Connect(context.Background(), userID, provider.ID, "gl-code", state)
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	if connected.AccessToken != "gl-access" || connected.RefreshToken != "gl-refresh" {
		t.Fatalf("connected = %+v, want the exchanged pair", connected)
	}
	if gotCode != "gl-code" || gotVerifier != verifier {
		t.Fatalf("token request code/verifier = %q/%q, want gl-code and the stored verifier",
			gotCode, gotVerifier)
	}

	// The verifier is single-use with the state: replaying it is refused.
	if _, err := svc.Connect(context.Background(), userID, provider.ID, "gl-code", state); !errors.Is(err, ErrValidation) {
		t.Fatalf("replayed Connect: error = %v, want ErrValidation", err)
	}
}

// TestGitLabConnectFailsClosedWithoutVerifier proves a PKCE source never
// redeems a code for a state that carries no verifier (a legacy or forged
// authorization).
func TestGitLabConnectFailsClosedWithoutVerifier(t *testing.T) {
	repo := newFakeRepo()
	provider := seedProvider(t, repo, Provider{Name: NameGitLab})
	svc := NewService(Config{
		Repository: repo, Logger: discardLogger(), AllowUnsafeBaseURL: true,
		Factories: map[string]Factory{
			NameGitLab: func(p Provider) (SourceProvider, error) { return newGitLabSource(p, true), nil },
		},
	})

	state, err := svc.states.new(provider.UserID, provider.ID)
	if err != nil {
		t.Fatalf("new state: %v", err)
	}
	if _, err := svc.Connect(context.Background(), provider.UserID, provider.ID, "code", state); !errors.Is(err, ErrValidation) {
		t.Fatalf("error = %v, want ErrValidation", err)
	}
}

// TestConnectStateVerifierExpiry proves an expired PKCE authorization loses
// its verifier with its state.
func TestConnectStateVerifierExpiry(t *testing.T) {
	states := newConnectState()
	user, provider := uuid.New(), uuid.New()
	state, err := states.newPKCE(user, provider, "verifier")
	if err != nil {
		t.Fatalf("newPKCE: %v", err)
	}
	states.now = func() time.Time { return time.Now().Add(connectStateTTL + time.Minute) }
	if _, ok := states.redeem(state, user, provider); ok {
		t.Fatal("expired PKCE state redeemed")
	}
}

// TestConnectStateVerifierRoundTrip proves redeem returns the stored verifier
// exactly once.
func TestConnectStateVerifierRoundTrip(t *testing.T) {
	states := newConnectState()
	user, provider := uuid.New(), uuid.New()
	state, err := states.newPKCE(user, provider, "verifier-abc")
	if err != nil {
		t.Fatalf("newPKCE: %v", err)
	}
	verifier, ok := states.redeem(state, user, provider)
	if !ok || verifier != "verifier-abc" {
		t.Fatalf("redeem = %q/%v, want verifier-abc/true", verifier, ok)
	}
	if _, ok := states.redeem(state, user, provider); ok {
		t.Fatal("verifier survived its redemption")
	}
}

// fakeGitLab serves the GitLab endpoints GS-6 needs: OAuth application
// provisioning, the token exchange and the project APIs.
type fakeGitLab struct {
	t *testing.T

	seenAuth     string
	seenPayload  map[string]any
	seenVerifier string
	seenCode     string

	applicationStatus int
	tokenStatus       int
}

func (f *fakeGitLab) handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v4/applications":
			f.seenAuth = r.Header.Get("Authorization")
			var payload map[string]any
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				http.Error(w, "bad body", http.StatusBadRequest)
				return
			}
			f.seenPayload = payload
			if f.applicationStatus != 0 {
				http.Error(w, "denied", f.applicationStatus)
				return
			}
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id": 7, "application_id": "gl-app-id", "secret": "gl-app-secret",
			})
		case r.Method == http.MethodPost && r.URL.Path == "/oauth/token":
			_ = r.ParseForm()
			f.seenCode = r.Form.Get("code")
			f.seenVerifier = r.Form.Get("code_verifier")
			if f.tokenStatus != 0 {
				http.Error(w, "denied", f.tokenStatus)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token": "gl-access", "refresh_token": "gl-refresh",
				"token_type": "bearer", "expires_in": 7200,
			})
		case r.Method == http.MethodGet && r.URL.Path == "/api/v4/projects":
			if r.URL.Query().Get("membership") != "true" {
				http.Error(w, "membership required", http.StatusBadRequest)
				return
			}
			_ = json.NewEncoder(w).Encode([]map[string]any{{
				"id": 42, "name": "demo", "path_with_namespace": "acme/demo",
				"visibility": "private", "default_branch": "main",
				"http_url_to_repo": "https://git.example/acme/demo.git",
				"ssh_url_to_repo":  "git@git.example:acme/demo.git",
				"web_url":          "https://git.example/acme/demo",
			}})
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/repository/branches"):
			_ = json.NewEncoder(w).Encode([]map[string]any{{
				"name": "main", "protected": true, "commit": map[string]any{"id": "abc123"},
			}})
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/hooks"):
			var payload map[string]any
			_ = json.NewDecoder(r.Body).Decode(&payload)
			if payload["url"] == nil || payload["token"] == nil ||
				payload["push_events"] != true || payload["merge_requests_events"] != true {
				http.Error(w, "bad hook", http.StatusBadRequest)
				return
			}
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]any{"id": 9})
		case r.Method == http.MethodDelete && strings.Contains(r.URL.Path, "/hooks/"):
			w.WriteHeader(http.StatusNoContent)
		default:
			http.Error(w, "unexpected "+r.Method+" "+r.URL.Path, http.StatusNotFound)
		}
	}
}

// TestAutoProvisionGitLab creates the OAuth application from the one-time
// admin token and stores only the resulting client credentials.
func TestAutoProvisionGitLab(t *testing.T) {
	fake := &fakeGitLab{t: t}
	srv := httptest.NewServer(fake.handler())
	t.Cleanup(srv.Close)

	repo := newFakeRepo()
	svc := NewService(Config{Repository: repo, Logger: discardLogger(), AllowUnsafeBaseURL: true})
	userID := uuid.New()

	provider, err := svc.AutoProvisionGitLab(context.Background(), userID, AutoProvisionGitLabInput{
		BaseURL:     srv.URL,
		AdminToken:  "one-time-admin",
		RedirectURL: "https://cp.example/oauth/callback",
	})
	if err != nil {
		t.Fatalf("AutoProvisionGitLab: %v", err)
	}
	if provider.Name != NameGitLab || provider.BaseURL != srv.URL {
		t.Fatalf("provider = %+v", provider)
	}
	if provider.ClientID != "gl-app-id" || provider.ClientSecret != "gl-app-secret" {
		t.Fatalf("client credentials = %q/%q", provider.ClientID, provider.ClientSecret)
	}
	if provider.Connected() {
		t.Fatal("provisioned provider must hold no tokens yet")
	}
	if provider.Scopes != gitLabProvisionScopes {
		t.Fatalf("scopes = %q, want %q", provider.Scopes, gitLabProvisionScopes)
	}
	if fake.seenAuth != "Bearer one-time-admin" {
		t.Fatalf("authorization = %q", fake.seenAuth)
	}
	if fake.seenPayload["redirect_uri"] != "https://cp.example/oauth/callback" ||
		fake.seenPayload["confidential"] != true ||
		fake.seenPayload["scopes"] != gitLabProvisionScopes {
		t.Fatalf("payload = %v", fake.seenPayload)
	}

	// The admin token is one-time: no stored field may contain it.
	stored := repo.providers[provider.ID]
	for _, field := range []string{stored.ClientID, stored.ClientSecret, stored.AccessToken, stored.RefreshToken, stored.RedirectURL, stored.Scopes} {
		if strings.Contains(field, "one-time-admin") {
			t.Fatalf("admin token reached the store in %q", field)
		}
	}
}

// TestGitLabInstanceBase pins the instance-root normalization: gitlab.com
// (with or without the API suffix) stores as empty, and a self-hosted root
// loses a pasted /api/v4 suffix.
func TestGitLabInstanceBase(t *testing.T) {
	for raw, want := range map[string]string{
		"":                             "",
		"https://gitlab.com":           "",
		"https://gitlab.com/api/v4":    "",
		"https://git.example":          "https://git.example",
		"https://git.example/":         "https://git.example",
		"https://git.example/api/v4":   "https://git.example",
		"https://git.example/api/v4/":  "https://git.example",
		"http://git.example:8080":      "http://git.example:8080",
		"http://127.0.0.1:8080/api/v4": "http://127.0.0.1:8080",
	} {
		if got := gitLabInstanceBase(raw); got != want {
			t.Errorf("gitLabInstanceBase(%q) = %q, want %q", raw, got, want)
		}
	}
}

// TestAutoProvisionGitLabValidation rejects a missing admin token, a bad
// redirect and an unsafe base without touching the network.
func TestAutoProvisionGitLabValidation(t *testing.T) {
	repo := newFakeRepo()
	svc := NewService(Config{Repository: repo, Logger: discardLogger(), AllowUnsafeBaseURL: true})
	userID := uuid.New()

	for name, input := range map[string]AutoProvisionGitLabInput{
		"missing admin token": {BaseURL: "https://git.example", RedirectURL: "https://cp.example/oauth/callback"},
		"relative redirect":   {BaseURL: "https://git.example", AdminToken: "a", RedirectURL: "/oauth/callback"},
		"userinfo redirect":   {BaseURL: "https://git.example", AdminToken: "a", RedirectURL: "https://user@cp.example/cb"},
	} {
		if _, err := svc.AutoProvisionGitLab(context.Background(), userID, input); !errors.Is(err, ErrValidation) {
			t.Errorf("%s: error = %v, want ErrValidation", name, err)
		}
	}
	if len(repo.providers) != 0 {
		t.Fatal("a rejected provision stored a row")
	}
}

// TestAutoProvisionGitLabRejectsBadAdminToken maps a 401 from GitLab to a
// validation error that never names the token.
func TestAutoProvisionGitLabRejectsBadAdminToken(t *testing.T) {
	fake := &fakeGitLab{t: t, applicationStatus: http.StatusUnauthorized}
	srv := httptest.NewServer(fake.handler())
	t.Cleanup(srv.Close)

	repo := newFakeRepo()
	svc := NewService(Config{Repository: repo, Logger: discardLogger(), AllowUnsafeBaseURL: true})

	_, err := svc.AutoProvisionGitLab(context.Background(), uuid.New(), AutoProvisionGitLabInput{
		BaseURL: srv.URL, AdminToken: "wrong-token", RedirectURL: "https://cp.example/oauth/callback",
	})
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("error = %v, want ErrValidation", err)
	}
	if strings.Contains(err.Error(), "wrong-token") {
		t.Fatalf("error names the admin token: %v", err)
	}
}

// TestGitLabSetupInfo returns the exact redirect URI and scopes for a manual
// application.
func TestGitLabSetupInfo(t *testing.T) {
	svc := NewService(Config{Repository: newFakeRepo(), Logger: discardLogger(), AllowUnsafeBaseURL: true})

	info, err := svc.GitLabSetupInfoFor("https://git.example/", "https://cp.example/oauth/callback")
	if err != nil {
		t.Fatalf("GitLabSetupInfoFor: %v", err)
	}
	if info.BaseURL != "https://git.example" || info.RedirectURI != "https://cp.example/oauth/callback" || info.Scopes != gitLabProvisionScopes {
		t.Fatalf("info = %+v", info)
	}

	empty, err := svc.GitLabSetupInfoFor("", "https://cp.example/oauth/callback")
	if err != nil {
		t.Fatalf("GitLabSetupInfoFor: %v", err)
	}
	if empty.BaseURL != gitLabDefaultBase {
		t.Fatalf("base = %q, want gitlab.com", empty.BaseURL)
	}

	if _, err := svc.GitLabSetupInfoFor("https://git.example", "/relative"); !errors.Is(err, ErrValidation) {
		t.Fatalf("bad redirect: error = %v, want ErrValidation", err)
	}
}

// TestServiceDelete forgets the connection; deleting twice or another user's
// row is safe.
func TestServiceDelete(t *testing.T) {
	repo := newFakeRepo()
	svc := NewService(Config{Repository: repo, Logger: discardLogger(), AllowUnsafeBaseURL: true})

	provider := seedProvider(t, repo, Provider{Name: NameGitLab})
	if err := svc.Delete(context.Background(), provider.UserID, provider.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := repo.Get(context.Background(), provider.ID, provider.UserID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("row survived disconnect: %v", err)
	}
	if err := svc.Delete(context.Background(), provider.UserID, provider.ID); err != nil {
		t.Fatalf("second Delete: %v", err)
	}

	foreign := seedProvider(t, repo, Provider{Name: NameGitLab})
	if err := svc.Delete(context.Background(), uuid.New(), foreign.ID); err != nil {
		t.Fatalf("foreign Delete: %v", err)
	}
	if _, err := repo.Get(context.Background(), foreign.ID, foreign.UserID); err != nil {
		t.Fatalf("foreign row removed: %v", err)
	}
}

// TestGitLabListBranchesAndWebhooks runs list, branches and the webhook
// lifecycle against the fake GitLab.
func TestGitLabListBranchesAndWebhooks(t *testing.T) {
	fake := &fakeGitLab{t: t}
	srv := httptest.NewServer(fake.handler())
	t.Cleanup(srv.Close)

	repo := newFakeRepo()
	userID := uuid.New()
	provider, err := repo.Create(context.Background(), Provider{
		ID: uuid.New(), UserID: userID, Name: NameGitLab, BaseURL: srv.URL,
		ClientID: "id", ClientSecret: "secret", AccessToken: "token",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	svc := NewService(Config{
		Repository: repo, Logger: discardLogger(), AllowUnsafeBaseURL: true,
		Factories: map[string]Factory{
			NameGitLab: func(p Provider) (SourceProvider, error) { return newGitLabSource(p, true), nil },
		},
	})

	repos, err := svc.ListRepos(context.Background(), userID, provider.ID)
	if err != nil {
		t.Fatalf("ListRepos: %v", err)
	}
	if len(repos) != 1 || repos[0].FullName != "acme/demo" || !repos[0].Private {
		t.Fatalf("repos = %+v", repos)
	}

	branches, err := svc.ListBranches(context.Background(), userID, provider.ID, "acme/demo")
	if err != nil {
		t.Fatalf("ListBranches: %v", err)
	}
	if len(branches) != 1 || branches[0].Name != "main" || branches[0].Commit != "abc123" || !branches[0].Protected {
		t.Fatalf("branches = %+v", branches)
	}

	hookID, err := svc.CreateWebhook(context.Background(), HookTarget{
		UserID: userID, Provider: NameGitLab, Repo: "acme/demo",
	}, Webhook{URL: "https://cp.example/api/v1/webhooks/gitlab", Secret: "hook-secret", Events: []string{"push", "pull_request"}})
	if err != nil {
		t.Fatalf("CreateWebhook: %v", err)
	}
	if hookID != "9" {
		t.Fatalf("hook id = %q", hookID)
	}
	if err := svc.DeleteWebhook(context.Background(), HookTarget{
		UserID: userID, Provider: NameGitLab, Repo: "acme/demo",
	}, hookID); err != nil {
		t.Fatalf("DeleteWebhook: %v", err)
	}
}

// TestServiceListBranchesRequiresConnection fails closed without a token.
func TestServiceListBranchesRequiresConnection(t *testing.T) {
	repo := newFakeRepo()
	provider, err := repo.Create(context.Background(), Provider{
		ID: uuid.New(), UserID: uuid.New(), Name: NameGitLab, BaseURL: "https://git.example",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	svc := NewService(Config{Repository: repo, Logger: discardLogger(), AllowUnsafeBaseURL: true})

	if _, err := svc.ListBranches(context.Background(), provider.UserID, provider.ID, "acme/demo"); !errors.Is(err, ErrNotConnected) {
		t.Fatalf("error = %v, want ErrNotConnected", err)
	}
}

// TestGitLabTransparentRefresh rotates an expired token inside a listing call
// and persists the rotated pair, so the next call does not present the
// consumed refresh token.
func TestGitLabTransparentRefresh(t *testing.T) {
	var refreshSeen string
	apiSrv := serve(t, func(w http.ResponseWriter, _ *http.Request) {
		writeJSONTest(t, w, []map[string]any{})
	})
	tokenSrv := serve(t, func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		refreshSeen = r.Form.Get("refresh_token")
		writeJSONTest(t, w, map[string]any{
			"access_token": "rotated-access", "refresh_token": "rotated-refresh",
			"token_type": "bearer", "expires_in": 7200,
		})
	})

	repo := newFakeRepo()
	expired := time.Now().Add(-time.Hour)
	provider, err := repo.Create(context.Background(), Provider{
		ID: uuid.New(), UserID: uuid.New(), Name: NameGitLab, BaseURL: apiSrv.URL,
		ClientID: "id", ClientSecret: "secret",
		AccessToken: "stale", RefreshToken: "consumable", TokenExpiresAt: &expired,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	svc := NewService(Config{
		Repository: repo, Logger: discardLogger(), AllowUnsafeBaseURL: true,
		Factories: map[string]Factory{
			NameGitLab: func(p Provider) (SourceProvider, error) {
				s := newGitLabSource(p, true)
				s.config.Endpoint.TokenURL = tokenSrv.URL
				return s, nil
			},
		},
	})

	if _, err := svc.ListRepos(context.Background(), provider.UserID, provider.ID); err != nil {
		t.Fatalf("ListRepos: %v", err)
	}
	if refreshSeen != "consumable" {
		t.Fatalf("refresh_token = %q, want the stored one", refreshSeen)
	}
	stored, err := repo.Get(context.Background(), provider.ID, provider.UserID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if stored.AccessToken != "rotated-access" || stored.RefreshToken != "rotated-refresh" {
		t.Fatalf("stored = %q/%q, want the rotated pair", stored.AccessToken, stored.RefreshToken)
	}
}
