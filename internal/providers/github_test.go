package providers

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"testing"
)

func TestGitHubSourceListRepos(t *testing.T) {
	var gotAuth string
	srv := serve(t, func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		if r.URL.Path != "/user/repos" {
			http.NotFound(w, r)
			return
		}
		writeJSONTest(t, w, []map[string]any{
			{
				"id": 1, "name": "gotham", "full_name": "justindeelux/gotham",
				"private": true, "default_branch": "main",
				"clone_url": "https://github.com/justindeelux/gotham.git",
				"ssh_url":   "git@github.com:justindeelux/gotham.git",
				"html_url":  "https://github.com/justindeelux/gotham",
			},
			{
				"id": 2, "name": "docs", "full_name": "justindeelux/docs",
				"private": false, "default_branch": "trunk",
			},
		})
	})

	source := newGitHubSource(Provider{BaseURL: srv.URL})
	repos, err := source.ListRepos(context.Background(), staticToken)
	if err != nil {
		t.Fatalf("ListRepos: %v", err)
	}
	if len(repos) != 2 {
		t.Fatalf("len(repos) = %d, want 2", len(repos))
	}
	if repos[0].ExternalID != "1" || repos[0].FullName != "justindeelux/gotham" || !repos[0].Private {
		t.Errorf("repos[0] = %+v", repos[0])
	}
	if repos[1].FullName != "justindeelux/docs" || repos[1].Private {
		t.Errorf("repos[1] = %+v", repos[1])
	}
	if gotAuth != "Bearer test-token" {
		t.Errorf("Authorization = %q, want Bearer test-token", gotAuth)
	}
}

func TestGitHubSourceListReposPagination(t *testing.T) {
	srv := serve(t, func(w http.ResponseWriter, r *http.Request) {
		page := r.URL.Query().Get("page")
		if page == "1" {
			batch := make([]map[string]any, 0, gitHubPageSize)
			for i := 0; i < gitHubPageSize; i++ {
				batch = append(batch, map[string]any{
					"id": i, "name": fmt.Sprintf("repo-%d", i), "full_name": fmt.Sprintf("o/repo-%d", i),
				})
			}
			writeJSONTest(t, w, batch)
			return
		}
		writeJSONTest(t, w, []map[string]any{{"id": 999, "name": "last", "full_name": "o/last"}})
	})

	source := newGitHubSource(Provider{BaseURL: srv.URL})
	repos, err := source.ListRepos(context.Background(), staticToken)
	if err != nil {
		t.Fatalf("ListRepos: %v", err)
	}
	if len(repos) != gitHubPageSize+1 {
		t.Fatalf("len(repos) = %d, want %d", len(repos), gitHubPageSize+1)
	}
	if repos[len(repos)-1].FullName != "o/last" {
		t.Errorf("last repo = %+v", repos[len(repos)-1])
	}
}

func TestGitHubSourceListBranches(t *testing.T) {
	srv := serve(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/justindeelux/gotham/branches" {
			http.NotFound(w, r)
			return
		}
		writeJSONTest(t, w, []map[string]any{
			{"name": "main", "protected": true, "commit": map[string]any{"sha": "abc123"}},
			{"name": "feat", "protected": false, "commit": map[string]any{"sha": "def456"}},
		})
	})

	source := newGitHubSource(Provider{BaseURL: srv.URL})
	branches, err := source.ListBranches(context.Background(), staticToken, "justindeelux/gotham")
	if err != nil {
		t.Fatalf("ListBranches: %v", err)
	}
	if len(branches) != 2 {
		t.Fatalf("len(branches) = %d, want 2", len(branches))
	}
	if branches[0].Name != "main" || branches[0].Commit != "abc123" || !branches[0].Protected {
		t.Errorf("branches[0] = %+v", branches[0])
	}
}

func TestGitHubSourceListBranchesInvalidRepo(t *testing.T) {
	source := newGitHubSource(Provider{BaseURL: "http://irrelevant"})
	if _, err := source.ListBranches(context.Background(), staticToken, "no-slash"); !errors.Is(err, ErrValidation) {
		t.Fatalf("error = %v, want ErrValidation", err)
	}
}

func TestGitHubSourceExchangeToken(t *testing.T) {
	srv := serve(t, func(w http.ResponseWriter, _ *http.Request) {
		writeJSONTest(t, w, map[string]any{
			"access_token": "gho_test", "token_type": "bearer", "scope": "repo",
		})
	})

	source := newGitHubSource(Provider{ClientID: "id", ClientSecret: "secret"})
	source.config.Endpoint.TokenURL = srv.URL + "/login/oauth/access_token"

	tok, err := source.ExchangeToken(context.Background(), "the-code")
	if err != nil {
		t.Fatalf("ExchangeToken: %v", err)
	}
	if tok.AccessToken != "gho_test" {
		t.Errorf("AccessToken = %q, want gho_test", tok.AccessToken)
	}
}

func TestGitHubSourceCreateWebhookNotWired(t *testing.T) {
	source := newGitHubSource(Provider{})
	err := source.CreateWebhook(context.Background(), staticToken, "o/r", Webhook{URL: "https://cp/hook"})
	if !errors.Is(err, ErrNotWired) {
		t.Fatalf("error = %v, want ErrNotWired", err)
	}
	if source.Name() != NameGitHub {
		t.Errorf("Name = %q, want %q", source.Name(), NameGitHub)
	}
}
