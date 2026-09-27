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
