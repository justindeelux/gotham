package providers

import (
	"context"
	"errors"
	"net/http"
	"testing"
)

func TestGiteaSourceListRepos(t *testing.T) {
	var gotAuth string
	srv := serve(t, func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		if r.URL.Path != "/api/v1/user/repos" {
			http.NotFound(w, r)
			return
		}
		writeJSONTest(t, w, []map[string]any{
			{
				"id": 7, "name": "gotham", "full_name": "team/gotham",
				"private": true, "default_branch": "main",
				"clone_url": "https://gitea.example/team/gotham.git",
				"ssh_url":   "git@gitea.example:team/gotham.git",
				"html_url":  "https://gitea.example/team/gotham",
			},
		})
	})

	source := newGiteaSource(Provider{BaseURL: srv.URL})
	repos, err := source.ListRepos(context.Background(), staticToken)
	if err != nil {
		t.Fatalf("ListRepos: %v", err)
	}
	if len(repos) != 1 {
		t.Fatalf("len(repos) = %d, want 1", len(repos))
	}
	if repos[0].ExternalID != "7" || repos[0].FullName != "team/gotham" || !repos[0].Private {
		t.Errorf("repos[0] = %+v", repos[0])
	}
	if gotAuth != "Bearer test-token" {
		t.Errorf("Authorization = %q, want Bearer test-token", gotAuth)
	}
}

func TestGiteaSourceListBranches(t *testing.T) {
	srv := serve(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/repos/team/gotham/branches" {
			http.NotFound(w, r)
			return
		}
		writeJSONTest(t, w, []map[string]any{
			{"name": "main", "protected": false, "commit": map[string]any{"id": "sha1"}},
		})
	})

	source := newGiteaSource(Provider{BaseURL: srv.URL})
	branches, err := source.ListBranches(context.Background(), staticToken, "team/gotham")
	if err != nil {
		t.Fatalf("ListBranches: %v", err)
	}
	if len(branches) != 1 || branches[0].Commit != "sha1" {
		t.Fatalf("branches = %+v", branches)
	}
}

func TestGiteaSourceExchangeToken(t *testing.T) {
	srv := serve(t, func(w http.ResponseWriter, _ *http.Request) {
		writeJSONTest(t, w, map[string]any{"access_token": "gitea_test", "token_type": "bearer"})
	})

	source := newGiteaSource(Provider{BaseURL: srv.URL, ClientID: "id", ClientSecret: "secret"})
	source.config.Endpoint.TokenURL = srv.URL + "/login/oauth/access_token"

	tok, err := source.ExchangeToken(context.Background(), "code")
	if err != nil {
		t.Fatalf("ExchangeToken: %v", err)
	}
	if tok.AccessToken != "gitea_test" {
		t.Errorf("AccessToken = %q, want gitea_test", tok.AccessToken)
	}
}

func TestGiteaSourceCreateWebhookNotWired(t *testing.T) {
	source := newGiteaSource(Provider{BaseURL: "https://gitea.example"})
	if err := source.CreateWebhook(context.Background(), staticToken, "t/r", Webhook{}); !errors.Is(err, ErrNotWired) {
		t.Fatalf("error = %v, want ErrNotWired", err)
	}
	if source.Name() != NameGitea {
		t.Errorf("Name = %q, want %q", source.Name(), NameGitea)
	}
}
