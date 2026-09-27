package providers

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
)

func TestGitLabSourceListRepos(t *testing.T) {
	var gotAuth string
	srv := serve(t, func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		if r.URL.Path != "/api/v4/projects" {
			http.NotFound(w, r)
			return
		}
		writeJSONTest(t, w, []map[string]any{
			{
				"id": 10, "name": "gotham", "path_with_namespace": "team/sub/gotham",
				"visibility": "private", "default_branch": "main",
				"http_url_to_repo": "https://gitlab.com/team/sub/gotham.git",
				"ssh_url_to_repo":  "git@gitlab.com:team/sub/gotham.git",
				"web_url":          "https://gitlab.com/team/sub/gotham",
			},
			{
				"id": 11, "name": "site", "path_with_namespace": "team/site",
				"visibility": "public", "default_branch": nil,
			},
		})
	})

	source := newGitLabSource(Provider{BaseURL: srv.URL})
	repos, err := source.ListRepos(context.Background(), staticToken)
	if err != nil {
		t.Fatalf("ListRepos: %v", err)
	}
	if len(repos) != 2 {
		t.Fatalf("len(repos) = %d, want 2", len(repos))
	}
	if repos[0].ExternalID != "10" || repos[0].FullName != "team/sub/gotham" || !repos[0].Private {
		t.Errorf("repos[0] = %+v", repos[0])
	}
	if repos[1].Private || repos[1].DefaultBranch != "" {
		t.Errorf("repos[1] = %+v, want public with empty default branch", repos[1])
	}
	if gotAuth != "Bearer test-token" {
		t.Errorf("Authorization = %q, want Bearer test-token", gotAuth)
	}
}

func TestGitLabSourceListBranches(t *testing.T) {
	srv := serve(t, func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/repository/branches") {
			http.NotFound(w, r)
			return
		}
		writeJSONTest(t, w, []map[string]any{
			{"name": "main", "protected": true, "commit": map[string]any{"id": "abc"}},
		})
	})

	source := newGitLabSource(Provider{BaseURL: srv.URL})
	branches, err := source.ListBranches(context.Background(), staticToken, "team/sub/gotham")
	if err != nil {
		t.Fatalf("ListBranches: %v", err)
	}
	if len(branches) != 1 || branches[0].Name != "main" || branches[0].Commit != "abc" {
		t.Fatalf("branches = %+v", branches)
	}
}

func TestGitLabSourceListBranchesInvalidRepo(t *testing.T) {
	source := newGitLabSource(Provider{BaseURL: "http://irrelevant"})
	for _, repo := range []string{"bad repo", "noslash"} {
		if _, err := source.ListBranches(context.Background(), staticToken, repo); !errors.Is(err, ErrValidation) {
			t.Fatalf("repo %q: error = %v, want ErrValidation", repo, err)
		}
	}
}

func TestGitLabSourceExchangeToken(t *testing.T) {
	srv := serve(t, func(w http.ResponseWriter, _ *http.Request) {
		writeJSONTest(t, w, map[string]any{"access_token": "glpat_test", "token_type": "bearer"})
	})

	source := newGitLabSource(Provider{BaseURL: srv.URL, ClientID: "id", ClientSecret: "secret"})
	source.config.Endpoint.TokenURL = srv.URL + "/oauth/token"

	tok, err := source.ExchangeToken(context.Background(), "code")
	if err != nil {
		t.Fatalf("ExchangeToken: %v", err)
	}
	if tok.AccessToken != "glpat_test" {
		t.Errorf("AccessToken = %q, want glpat_test", tok.AccessToken)
	}
}

func TestGitLabSourceCreateWebhookNotWired(t *testing.T) {
	source := newGitLabSource(Provider{BaseURL: "https://gitlab.com"})
	if err := source.CreateWebhook(context.Background(), staticToken, "g/p", Webhook{}); !errors.Is(err, ErrNotWired) {
		t.Fatalf("error = %v, want ErrNotWired", err)
	}
	if source.Name() != NameGitLab {
		t.Errorf("Name = %q, want %q", source.Name(), NameGitLab)
	}
}
