package providers

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/google/uuid"
)

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
	svc := NewService(Config{Repository: repo, Logger: discardLogger()})

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
	svc := NewService(Config{Repository: repo, Logger: discardLogger()})

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
	svc := NewService(Config{Repository: repo, Logger: discardLogger()})

	if _, err := svc.ListRepos(context.Background(), provider.UserID, provider.ID); !errors.Is(err, ErrNotConnected) {
		t.Fatalf("error = %v, want ErrNotConnected", err)
	}
}

func TestServiceListReposUnsupportedProvider(t *testing.T) {
	repo := newFakeRepo()
	provider := seedProvider(t, repo, Provider{Name: "bitbucket"})
	svc := NewService(Config{Repository: repo, Logger: discardLogger()})

	if _, err := svc.ListRepos(context.Background(), provider.UserID, provider.ID); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("error = %v, want ErrUnsupported", err)
	}
}

func TestServiceListReposGiteaRequiresBaseURL(t *testing.T) {
	repo := newFakeRepo()
	provider := seedProvider(t, repo, Provider{Name: NameGitea})
	svc := NewService(Config{Repository: repo, Logger: discardLogger()})

	if _, err := svc.ListRepos(context.Background(), provider.UserID, provider.ID); !errors.Is(err, ErrValidation) {
		t.Fatalf("error = %v, want ErrValidation", err)
	}
}

func TestServiceListReposNotFound(t *testing.T) {
	repo := newFakeRepo()
	provider := seedProvider(t, repo, Provider{Name: NameGitHub})
	svc := NewService(Config{Repository: repo, Logger: discardLogger()})

	if _, err := svc.ListRepos(context.Background(), uuid.New(), provider.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("error = %v, want ErrNotFound", err)
	}
}

func TestServiceList(t *testing.T) {
	repo := newFakeRepo()
	provider := seedProvider(t, repo, Provider{Name: NameGitHub})
	svc := NewService(Config{Repository: repo, Logger: discardLogger()})

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
	svc := NewService(Config{Repository: repo, Logger: discardLogger()})

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
			svc := NewService(Config{Repository: tc.repo, Logger: discardLogger()})
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
	svc := NewService(Config{Repository: repo, Logger: discardLogger()})

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
	svc := NewService(Config{Repository: repo, Logger: discardLogger()})
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
