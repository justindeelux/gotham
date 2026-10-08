package providers

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"golang.org/x/oauth2"
)

// fakeSource is a deterministic SourceProvider for runWithToken and truncation
// tests. Every call records the token it was given and, when mutate is set,
// rotates it to simulate an oauth2 refresh.
type fakeSource struct {
	mutate    bool
	truncated bool
	repos     []Repo
	latest    *oauth2.Token
}

func (f *fakeSource) record(tok *oauth2.Token) {
	if f.mutate {
		tok.AccessToken = "fresh-token"
		tok.RefreshToken = "rotated-refresh"
	}
	f.latest = tok
}

func (f *fakeSource) Name() string { return NameGitHub }

func (f *fakeSource) ExchangeToken(context.Context, string) (*oauth2.Token, error) {
	return &oauth2.Token{AccessToken: "exchanged"}, nil
}

func (f *fakeSource) AuthCodeURL(string) string { return "" }

func (f *fakeSource) ListRepos(_ context.Context, tok *oauth2.Token) ([]Repo, error) {
	f.record(tok)
	return f.repos, nil
}

func (f *fakeSource) ListBranches(context.Context, *oauth2.Token, string) ([]Branch, error) {
	return nil, nil
}

func (f *fakeSource) CreateWebhook(_ context.Context, tok *oauth2.Token, _ string, _ Webhook) (string, error) {
	f.record(tok)
	return "1", nil
}

func (f *fakeSource) DeleteWebhook(_ context.Context, tok *oauth2.Token, _, _ string) error {
	f.record(tok)
	return nil
}

func (f *fakeSource) CreatePullRequestComment(_ context.Context, tok *oauth2.Token, _ string, _ int, _ string) error {
	f.record(tok)
	return nil
}

func (f *fakeSource) AddDeployKey(_ context.Context, tok *oauth2.Token, _ string, _ DeployKey) (string, error) {
	f.record(tok)
	return "1", nil
}

func (f *fakeSource) RemoveDeployKey(_ context.Context, tok *oauth2.Token, _, _ string) error {
	f.record(tok)
	return nil
}

// Token reports the last token the source saw, without refreshing.
func (f *fakeSource) Token() *oauth2.Token { return f.latest }

// Truncated reports whether the last listing hit a bound.
func (f *fakeSource) Truncated() bool { return f.truncated }

// seedProvider stores a connected provider in repo and returns it.
func seedProvider(t *testing.T, repo *fakeRepo, p Provider) Provider {
	t.Helper()
	if p.ID == uuid.Nil {
		p.ID = uuid.New()
	}
	if p.UserID == uuid.Nil {
		p.UserID = uuid.New()
	}
	p.AccessToken = "test-token"
	stored, err := repo.Create(context.Background(), p)
	if err != nil {
		t.Fatalf("seed provider: %v", err)
	}
	return stored
}

func TestServiceListReposCaches(t *testing.T) {
	srv := serve(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSONTest(t, w, []map[string]any{
			{"id": 1, "name": "gotham", "full_name": "o/gotham", "default_branch": "main"},
		})
	})

	repo := newFakeRepo()
	provider := seedProvider(t, repo, Provider{Name: NameGitHub, BaseURL: srv.URL})
	svc := newTestService(repo)

	repos, err := svc.ListRepos(context.Background(), provider.UserID, provider.ID)
	if err != nil {
		t.Fatalf("ListRepos: %v", err)
	}
	if len(repos) != 1 || repos[0].FullName != "o/gotham" {
		t.Fatalf("repos = %+v", repos)
	}
	if len(repo.replaced[provider.ID]) != 1 {
		t.Errorf("cache not refreshed: %+v", repo.replaced[provider.ID])
	}
}

func TestServiceListReposFallsBackToCache(t *testing.T) {
	srv := serve(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})

	repo := newFakeRepo()
	provider := seedProvider(t, repo, Provider{Name: NameGitHub, BaseURL: srv.URL})
	repo.cached[provider.ID] = []Repo{{ExternalID: "9", FullName: "o/cached"}}
	svc := newTestService(repo)

	repos, err := svc.ListRepos(context.Background(), provider.UserID, provider.ID)
	if err != nil {
		t.Fatalf("ListRepos: %v", err)
	}
	if len(repos) != 1 || repos[0].FullName != "o/cached" {
		t.Fatalf("repos = %+v, want cached fallback", repos)
	}
}

func TestServiceListReposNotConnected(t *testing.T) {
	repo := newFakeRepo()
	provider := seedProvider(t, repo, Provider{Name: NameGitHub, BaseURL: "http://irrelevant"})
	provider.AccessToken = ""
	repo.providers[provider.ID] = provider
	svc := newTestService(repo)

	if _, err := svc.ListRepos(context.Background(), provider.UserID, provider.ID); !errors.Is(err, ErrNotConnected) {
		t.Fatalf("error = %v, want ErrNotConnected", err)
	}
}

func TestServiceListReposUnsupportedProvider(t *testing.T) {
	repo := newFakeRepo()
	provider := seedProvider(t, repo, Provider{Name: "bitbucket"})
	svc := newTestService(repo)

	if _, err := svc.ListRepos(context.Background(), provider.UserID, provider.ID); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("error = %v, want ErrUnsupported", err)
	}
}

func TestServiceListReposGiteaRequiresBaseURL(t *testing.T) {
	repo := newFakeRepo()
	provider := seedProvider(t, repo, Provider{Name: NameGitea})
	svc := newTestService(repo)

	if _, err := svc.ListRepos(context.Background(), provider.UserID, provider.ID); !errors.Is(err, ErrValidation) {
		t.Fatalf("error = %v, want ErrValidation", err)
	}
}

func TestServiceListReposNotFound(t *testing.T) {
	repo := newFakeRepo()
	provider := seedProvider(t, repo, Provider{Name: NameGitHub})
	svc := newTestService(repo)

	if _, err := svc.ListRepos(context.Background(), uuid.New(), provider.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("error = %v, want ErrNotFound", err)
	}
}

func TestServiceList(t *testing.T) {
	repo := newFakeRepo()
	provider := seedProvider(t, repo, Provider{Name: NameGitHub})
	svc := newTestService(repo)

	list, err := svc.List(context.Background(), provider.UserID)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 1 || list[0].ID != provider.ID {
		t.Fatalf("list = %+v", list)
	}
}

func TestNewDefaultServiceWithoutStore(t *testing.T) {
	if svc := NewDefaultService(nil, "secret", discardLogger()); svc != nil {
		t.Fatalf("NewDefaultService(nil) = %v, want nil", svc)
	}
}

func TestServiceCreateWebhookInstallsOnStoredConnection(t *testing.T) {
	var gotMethod, gotPath string
	srv := serve(t, func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.EscapedPath()
		w.WriteHeader(http.StatusCreated)
		writeJSONTest(t, w, map[string]any{"id": 5150})
	})

	repo := newFakeRepo()
	provider := seedProvider(t, repo, Provider{Name: NameGitHub, BaseURL: srv.URL})
	svc := newTestService(repo)

	hookID, err := svc.CreateWebhook(context.Background(), HookTarget{
		UserID:   provider.UserID,
		Provider: NameGitHub,
		CloneURL: "https://github.com/o/r.git",
		Repo:     "o/r",
	}, Webhook{URL: "https://cp.example/api/v1/webhooks/github", Secret: "s3cr3t"})
	if err != nil {
		t.Fatalf("CreateWebhook: %v", err)
	}
	if hookID != "5150" {
		t.Errorf("hook id = %q, want 5150", hookID)
	}
	if gotMethod != http.MethodPost || gotPath != "/repos/o/r/hooks" {
		t.Errorf("request = %s %s, want POST /repos/o/r/hooks", gotMethod, gotPath)
	}
}

func TestServiceCreateWebhookConnectionProblems(t *testing.T) {
	disconnectedUser := uuid.New()
	cases := []struct {
		name   string
		repo   *fakeRepo
		target HookTarget
		want   error
	}{
		{
			name:   "no connection for the provider",
			repo:   newFakeRepo(),
			target: HookTarget{Provider: NameGitHub, Repo: "o/r"},
			want:   ErrNotFound,
		},
		{
			name: "connection without an access token",
			repo: func() *fakeRepo {
				r := newFakeRepo()
				_, _ = r.Create(context.Background(), Provider{
					ID: uuid.New(), UserID: disconnectedUser, Name: NameGitHub,
					BaseURL: "https://api.github.com",
				})
				return r
			}(),
			target: HookTarget{UserID: disconnectedUser, Provider: NameGitHub, Repo: "o/r"},
			want:   ErrNotConnected,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc := newTestService(tc.repo)
			_, err := svc.CreateWebhook(context.Background(), tc.target,
				Webhook{URL: "https://cp.example/api/v1/webhooks/github", Secret: "s"})
			if !errors.Is(err, tc.want) {
				t.Fatalf("error = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestServiceCreateWebhookValidatesCallbackURL(t *testing.T) {
	repo := newFakeRepo()
	provider := seedProvider(t, repo, Provider{Name: NameGitHub})
	svc := newTestService(repo)

	_, err := svc.CreateWebhook(context.Background(), HookTarget{
		UserID: provider.UserID, Provider: NameGitHub, Repo: "o/r",
	}, Webhook{URL: "", Secret: "s"})
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("error = %v, want ErrValidation", err)
	}
}

func TestServiceDeleteWebhookRemovesHook(t *testing.T) {
	var gotMethod, gotPath string
	status := http.StatusNoContent
	srv := serve(t, func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.EscapedPath()
		w.WriteHeader(status)
	})

	repo := newFakeRepo()
	provider := seedProvider(t, repo, Provider{Name: NameGitHub, BaseURL: srv.URL})
	svc := newTestService(repo)
	target := HookTarget{
		UserID: provider.UserID, Provider: NameGitHub,
		CloneURL: "https://github.com/o/r.git", Repo: "o/r",
	}

	if err := svc.DeleteWebhook(context.Background(), target, "5150"); err != nil {
		t.Fatalf("DeleteWebhook: %v", err)
	}
	if gotMethod != http.MethodDelete || gotPath != "/repos/o/r/hooks/5150" {
		t.Errorf("request = %s %s, want DELETE /repos/o/r/hooks/5150", gotMethod, gotPath)
	}

	// The host having already forgotten the hook stays a success.
	status = http.StatusNotFound
	if err := svc.DeleteWebhook(context.Background(), target, "5150"); err != nil {
		t.Errorf("DeleteWebhook on 404: %v, want nil", err)
	}
}

func TestChooseConnection(t *testing.T) {
	githubAPI := Provider{Name: NameGitHub, BaseURL: "https://api.github.com"}
	githubPublic := Provider{Name: NameGitHub}
	githubGHE := Provider{Name: NameGitHub, BaseURL: "https://ghe.example/api/v3"}
	giteaA := Provider{Name: NameGitea, BaseURL: "https://gitea-a.example"}
	giteaB := Provider{Name: NameGitea, BaseURL: "https://gitea-b.example:3000"}
	gitlabSelf := Provider{Name: NameGitLab, BaseURL: "https://gitlab.corp.example"}

	cases := []struct {
		name       string
		candidates []Provider
		cloneURL   string
		want       Provider
		wantErr    bool
	}{
		{
			name:       "single connection is always unambiguous",
			candidates: []Provider{giteaA},
			cloneURL:   "",
			want:       giteaA,
		},
		{
			name:       "self-hosted instance picked by clone host",
			candidates: []Provider{giteaA, giteaB},
			cloneURL:   "https://gitea-b.example:3000/octo/gotham.git",
			want:       giteaB,
		},
		{
			name:       "scp-like clone url picked by host",
			candidates: []Provider{giteaA, giteaB},
			cloneURL:   "git@gitea-a.example:octo/gotham.git",
			want:       giteaA,
		},
		{
			name:       "github enterprise instance picked by clone host",
			candidates: []Provider{githubPublic, githubGHE},
			cloneURL:   "https://ghe.example/octo/gotham.git",
			want:       githubGHE,
		},
		{
			name:       "public github clone url picks the public connection",
			candidates: []Provider{githubPublic, githubGHE},
			cloneURL:   "git@github.com:octo/gotham.git",
			want:       githubPublic,
		},
		{
			name:       "rest api host still matches a github.com clone url",
			candidates: []Provider{githubAPI, githubGHE},
			cloneURL:   "https://github.com/octo/gotham.git",
			want:       githubAPI,
		},
		{
			name:       "no connection serves that host",
			candidates: []Provider{giteaA, giteaB},
			cloneURL:   "https://elsewhere.example/octo/gotham.git",
			wantErr:    true,
		},
		{
			name:       "several connections and no clone url to tell them apart",
			candidates: []Provider{gitlabSelf, gitlabSelf},
			cloneURL:   "",
			wantErr:    true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := chooseConnection(tc.candidates, tc.cloneURL)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("chooseConnection = %+v, want an error", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("chooseConnection: %v", err)
			}
			if got.BaseURL != tc.want.BaseURL {
				t.Errorf("picked %q, want %q", got.BaseURL, tc.want.BaseURL)
			}
		})
	}
}

// TestServicePersistsRefreshedToken is the C1-10 regression: oauth2 refreshes
// an expired token in memory during a call, and the service must write the
// rotated pair back so the next call does not present a consumed refresh token.
func TestServicePersistsRefreshedToken(t *testing.T) {
	tokenSrv := serve(t, func(w http.ResponseWriter, _ *http.Request) {
		writeJSONTest(t, w, map[string]any{
			"access_token": "fresh-token", "refresh_token": "rotated-refresh",
			"token_type": "bearer", "expires_in": 3600,
		})
	})
	apiSrv := serve(t, func(w http.ResponseWriter, _ *http.Request) {
		writeJSONTest(t, w, []map[string]any{{"id": 1, "name": "gotham", "full_name": "o/gotham"}})
	})

	repo := newFakeRepo()
	past := time.Now().Add(-time.Hour)
	provider := seedProvider(t, repo, Provider{
		Name: NameGitHub, BaseURL: apiSrv.URL,
		RefreshToken: "old-refresh", TokenExpiresAt: &past,
	})
	svc := NewService(Config{
		Repository: repo, Logger: discardLogger(), AllowUnsafeBaseURL: true,
		Factories: map[string]Factory{
			NameGitHub: func(p Provider) (SourceProvider, error) {
				s := newGitHubSource(p, true)
				s.config.Endpoint.TokenURL = tokenSrv.URL
				return s, nil
			},
		},
	})

	if _, err := svc.ListRepos(context.Background(), provider.UserID, provider.ID); err != nil {
		t.Fatalf("ListRepos: %v", err)
	}
	stored := repo.providers[provider.ID]
	if stored.AccessToken != "fresh-token" || stored.RefreshToken != "rotated-refresh" {
		t.Fatalf("stored token = %q / %q, want the refreshed pair", stored.AccessToken, stored.RefreshToken)
	}
	if stored.TokenExpiresAt == nil || !stored.TokenExpiresAt.After(time.Now()) {
		t.Fatalf("stored expiry = %v, want a future time", stored.TokenExpiresAt)
	}
}

// TestServiceCreateProvider is the C1-11 regression: a connection can be
// created through the production service path.
func TestServiceCreateProvider(t *testing.T) {
	repo := newFakeRepo()
	userID := uuid.New()
	svc := newTestService(repo)

	created, err := svc.Create(context.Background(), userID, CreateProviderInput{
		Name: NameGitHub, BaseURL: "https://api.github.com",
		ClientID: "client-id", ClientSecret: "client-secret",
		RedirectURL: "https://cp.example/oauth/callback", Scopes: "repo",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.ID == uuid.Nil || created.UserID != userID || created.Name != NameGitHub {
		t.Fatalf("created = %+v", created)
	}
	stored := repo.providers[created.ID]
	if stored.ClientSecret != "client-secret" || stored.AccessToken != "" {
		t.Fatalf("stored = %+v, want the app config and no token", stored)
	}
}

// TestServiceRejectsUnsafeBaseURL covers C1-15 at the create and use boundary.
func TestServiceRejectsUnsafeBaseURL(t *testing.T) {
	repo := newFakeRepo()
	svc := NewService(Config{Repository: repo, Logger: discardLogger()})

	_, err := svc.Create(context.Background(), uuid.New(), CreateProviderInput{
		Name: NameGitea, BaseURL: "http://169.254.169.254",
		ClientID: "id", ClientSecret: "s", RedirectURL: "https://cp.example/cb",
	})
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("Create with a metadata base_url: error = %v, want ErrValidation", err)
	}

	// A row written out of band with a loopback base_url is refused at use.
	provider := seedProvider(t, repo, Provider{Name: NameGitHub, BaseURL: "http://127.0.0.1:1"})
	if _, err := svc.ListRepos(context.Background(), provider.UserID, provider.ID); !errors.Is(err, ErrValidation) {
		t.Fatalf("ListRepos with a loopback base_url: error = %v, want ErrValidation", err)
	}
}

// TestServiceAuthorizeAndConnect is the C1-11 regression: the OAuth start and
// completion path exchanges the code and stores the tokens.
func TestServiceAuthorizeAndConnect(t *testing.T) {
	var gotCode string
	tokenSrv := serve(t, func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		gotCode = r.Form.Get("code")
		writeJSONTest(t, w, map[string]any{
			"access_token": "connected-token", "refresh_token": "connected-refresh",
			"token_type": "bearer", "expires_in": 3600,
		})
	})
	apiSrv := serve(t, func(w http.ResponseWriter, _ *http.Request) {
		writeJSONTest(t, w, []map[string]any{})
	})

	repo := newFakeRepo()
	userID := uuid.New()
	provider, err := repo.Create(context.Background(), Provider{
		ID: uuid.New(), UserID: userID, Name: NameGitHub, BaseURL: apiSrv.URL,
		ClientID: "client-id", ClientSecret: "client-secret",
		RedirectURL: "https://cp.example/oauth/callback",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	svc := NewService(Config{
		Repository: repo, Logger: discardLogger(), AllowUnsafeBaseURL: true,
		Factories: map[string]Factory{
			NameGitHub: func(p Provider) (SourceProvider, error) {
				s := newGitHubSource(p, true)
				s.config.Endpoint.TokenURL = tokenSrv.URL
				return s, nil
			},
		},
	})

	url, state, err := svc.Authorize(context.Background(), userID, provider.ID)
	if err != nil {
		t.Fatalf("Authorize: %v", err)
	}
	if state == "" || !strings.Contains(url, "state="+state) || !strings.Contains(url, "client_id=client-id") {
		t.Fatalf("authorize url = %q, state = %q", url, state)
	}

	connected, err := svc.Connect(context.Background(), userID, provider.ID, "the-code", state)
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	if !connected.Connected() || connected.AccessToken != "connected-token" {
		t.Fatalf("connected = %+v, want the exchanged token", connected)
	}
	if gotCode != "the-code" {
		t.Fatalf("provider saw code %q, want the-code", gotCode)
	}

	// The state is single-use: replaying it is refused.
	if _, err := svc.Connect(context.Background(), userID, provider.ID, "the-code", state); !errors.Is(err, ErrValidation) {
		t.Fatalf("replayed Connect: error = %v, want ErrValidation", err)
	}
}

// TestServiceConnectRejectsForgedState proves a state that was never issued
// cannot complete a connection.
func TestServiceConnectRejectsForgedState(t *testing.T) {
	repo := newFakeRepo()
	provider := seedProvider(t, repo, Provider{Name: NameGitHub, BaseURL: "https://api.github.com"})
	svc := newTestService(repo)

	if _, err := svc.Connect(context.Background(), provider.UserID, provider.ID, "code", "forged"); !errors.Is(err, ErrValidation) {
		t.Fatalf("error = %v, want ErrValidation", err)
	}
}

// TestServicePersistsRefreshedTokenDespiteCancelledContext is the U4
// regression: the persist must survive a request whose context was cancelled
// (client disconnect / HTTP cap), or GitLab's rotated refresh token is lost.
func TestServicePersistsRefreshedTokenDespiteCancelledContext(t *testing.T) {
	repo := newFakeRepo()
	provider := seedProvider(t, repo, Provider{Name: NameGitHub, BaseURL: "https://api.github.com"})

	ctx, cancel := context.WithCancel(context.Background())
	source := &fakeSource{mutate: true, repos: []Repo{{ExternalID: "1", FullName: "o/r"}}}
	svc := NewService(Config{
		Repository: repo, Logger: discardLogger(), AllowUnsafeBaseURL: true,
		Factories: map[string]Factory{NameGitHub: func(Provider) (SourceProvider, error) {
			// Simulate the client disconnecting during the call.
			cancel()
			return source, nil
		}},
	})

	// The factory cancels the context while the source is built; the persist
	// after the call must still run on a detached context.
	if _, err := svc.ListRepos(ctx, provider.UserID, provider.ID); err != nil {
		t.Fatalf("ListRepos: %v", err)
	}
	stored := repo.providers[provider.ID]
	if stored.AccessToken != "fresh-token" || stored.RefreshToken != "rotated-refresh" {
		t.Fatalf("stored token = %q / %q, want the rotated pair persisted on a cancelled request",
			stored.AccessToken, stored.RefreshToken)
	}
}

// TestServiceTruncatedListingSkipsCache is the U5 regression: a truncated
// listing must not overwrite the previous complete cache.
func TestServiceTruncatedListingSkipsCache(t *testing.T) {
	repo := newFakeRepo()
	provider := seedProvider(t, repo, Provider{Name: NameGitHub, BaseURL: "https://api.github.com"})
	repo.cached[provider.ID] = []Repo{{ExternalID: "old", FullName: "o/old"}}

	source := &fakeSource{truncated: true, repos: []Repo{{ExternalID: "partial", FullName: "o/partial"}}}
	svc := NewService(Config{
		Repository: repo, Logger: discardLogger(), AllowUnsafeBaseURL: true,
		Factories: map[string]Factory{NameGitHub: func(Provider) (SourceProvider, error) { return source, nil }},
	})

	repos, err := svc.ListRepos(context.Background(), provider.UserID, provider.ID)
	if err != nil {
		t.Fatalf("ListRepos: %v", err)
	}
	if len(repos) != 1 || repos[0].FullName != "o/partial" {
		t.Fatalf("repos = %+v, want the live partial list", repos)
	}
	if _, ok := repo.replaced[provider.ID]; ok {
		t.Fatalf("truncated listing replaced the cache: %+v", repo.replaced[provider.ID])
	}
	if cached := repo.cached[provider.ID]; len(cached) != 1 || cached[0].FullName != "o/old" {
		t.Fatalf("cache = %+v, want the previous complete list", cached)
	}
}

// TestRunWithTokenPersistsAcrossPaths is the U8 table test: every provider call
// path persists a rotated token, and an unchanged token is not written.
func TestRunWithTokenPersistsAcrossPaths(t *testing.T) {
	cases := []struct {
		name    string
		mutate  bool
		invoke  func(svc *Service, userID, providerID uuid.UUID) error
		wantPut int
	}{
		{
			name: "list repos", mutate: true,
			invoke: func(svc *Service, userID, providerID uuid.UUID) error {
				_, err := svc.ListRepos(context.Background(), userID, providerID)
				return err
			},
			wantPut: 1,
		},
		{
			name: "create webhook", mutate: true,
			invoke: func(svc *Service, userID, _ uuid.UUID) error {
				_, err := svc.CreateWebhook(context.Background(),
					HookTarget{UserID: userID, Provider: NameGitHub, Repo: "o/r", CloneURL: "https://github.com/o/r.git"},
					Webhook{URL: "https://cp.example/api/v1/webhooks/github", Secret: "s"})
				return err
			},
			wantPut: 1,
		},
		{
			name: "delete webhook", mutate: true,
			invoke: func(svc *Service, userID, _ uuid.UUID) error {
				return svc.DeleteWebhook(context.Background(),
					HookTarget{UserID: userID, Provider: NameGitHub, Repo: "o/r", CloneURL: "https://github.com/o/r.git"}, "1")
			},
			wantPut: 1,
		},
		{
			name: "comment", mutate: true,
			invoke: func(svc *Service, userID, _ uuid.UUID) error {
				return svc.CreatePullRequestComment(context.Background(),
					HookTarget{UserID: userID, Provider: NameGitHub, Repo: "o/r", CloneURL: "https://github.com/o/r.git"}, 1, "hi")
			},
			wantPut: 1,
		},
		{
			name: "add deploy key", mutate: true,
			invoke: func(svc *Service, userID, _ uuid.UUID) error {
				_, err := svc.AddDeployKey(context.Background(),
					HookTarget{UserID: userID, Provider: NameGitHub, Repo: "o/r", CloneURL: "https://github.com/o/r.git"},
					DeployKey{Title: "t", Key: "k"})
				return err
			},
			wantPut: 1,
		},
		{
			name: "remove deploy key", mutate: true,
			invoke: func(svc *Service, userID, _ uuid.UUID) error {
				return svc.RemoveDeployKey(context.Background(),
					HookTarget{UserID: userID, Provider: NameGitHub, Repo: "o/r", CloneURL: "https://github.com/o/r.git"}, "1")
			},
			wantPut: 1,
		},
		{
			name: "unchanged token is not written", mutate: false,
			invoke: func(svc *Service, userID, providerID uuid.UUID) error {
				_, err := svc.ListRepos(context.Background(), userID, providerID)
				return err
			},
			wantPut: 0,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := newFakeRepo()
			provider := seedProvider(t, repo, Provider{Name: NameGitHub, BaseURL: "https://api.github.com"})
			source := &fakeSource{mutate: tc.mutate}
			svc := NewService(Config{
				Repository: repo, Logger: discardLogger(), AllowUnsafeBaseURL: true,
				Factories: map[string]Factory{NameGitHub: func(Provider) (SourceProvider, error) { return source, nil }},
			})

			if err := tc.invoke(svc, provider.UserID, provider.ID); err != nil {
				t.Fatalf("invoke: %v", err)
			}
			if repo.updateTokenCalls != tc.wantPut {
				t.Fatalf("UpdateToken calls = %d, want %d", repo.updateTokenCalls, tc.wantPut)
			}
			if tc.wantPut == 1 {
				stored := repo.providers[provider.ID]
				if stored.AccessToken != "fresh-token" || stored.RefreshToken != "rotated-refresh" {
					t.Fatalf("stored token = %q / %q", stored.AccessToken, stored.RefreshToken)
				}
			}
		})
	}
}

// TestConnectStatePerUserCapAndRedeem covers U7/U8: one account cannot exhaust
// the store, redeem is bound to the exact user and provider, and redeeming
// frees a slot.
func TestConnectStatePerUserCapAndRedeem(t *testing.T) {
	states := newConnectState()
	user := uuid.New()
	provider := uuid.New()

	// A foreign user or provider cannot redeem its own guess.
	foreignUser, err := states.new(user, provider, "")
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	if _, ok := states.redeem(foreignUser, uuid.New(), provider); ok {
		t.Error("redeem with a foreign user returned true")
	}
	foreignProvider, err := states.new(user, provider, "")
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	if _, ok := states.redeem(foreignProvider, user, uuid.New()); ok {
		t.Error("redeem with a foreign provider returned true")
	}

	// Fill to the per-user cap.
	var last string
	for i := 0; i < connectStatePerUser; i++ {
		state, err := states.new(user, provider, "")
		if err != nil {
			t.Fatalf("new #%d: %v", i, err)
		}
		last = state
	}
	if _, err := states.new(user, provider, ""); !errors.Is(err, ErrTooManyRequests) {
		t.Fatalf("over-cap new: error = %v, want ErrTooManyRequests", err)
	}

	// The correct pair redeems once and frees a slot.
	if _, ok := states.redeem(last, user, provider); !ok {
		t.Fatal("redeem with the bound pair returned false")
	}
	if _, ok := states.redeem(last, user, provider); ok {
		t.Error("replaying a redeemed state returned true")
	}
	if _, err := states.new(user, provider, ""); err != nil {
		t.Fatalf("new after redeem: %v", err)
	}

	// Another user is unaffected by the first user's cap.
	if _, err := states.new(uuid.New(), uuid.New(), ""); err != nil {
		t.Fatalf("new for another user: %v", err)
	}
}
