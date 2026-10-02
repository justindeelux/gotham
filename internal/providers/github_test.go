package providers

import (
	"context"
	"encoding/json"
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

	source := newGitHubSource(Provider{BaseURL: srv.URL}, true)
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

	source := newGitHubSource(Provider{BaseURL: srv.URL}, true)
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

// TestGitHubSourceListReposBounded is the C1-8 regression: a provider that
// never returns a short page must not make the listing loop forever.
func TestGitHubSourceListReposBounded(t *testing.T) {
	var requests int
	srv := serve(t, func(w http.ResponseWriter, _ *http.Request) {
		requests++
		batch := make([]map[string]any, 0, gitHubPageSize)
		for i := 0; i < gitHubPageSize; i++ {
			batch = append(batch, map[string]any{"id": i, "name": "r", "full_name": "o/r"})
		}
		writeJSONTest(t, w, batch)
	})

	source := newGitHubSource(Provider{BaseURL: srv.URL}, true)
	repos, err := source.ListRepos(context.Background(), staticToken)
	if err != nil {
		t.Fatalf("ListRepos: %v", err)
	}
	if requests != maxRepoPages {
		t.Fatalf("requests = %d, want the %d-page cap", requests, maxRepoPages)
	}
	if len(repos) != maxRepoPages*gitHubPageSize {
		t.Fatalf("len(repos) = %d, want %d", len(repos), maxRepoPages*gitHubPageSize)
	}
	if !source.Truncated() {
		t.Error("listing at the page cap is not marked truncated")
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

	source := newGitHubSource(Provider{BaseURL: srv.URL}, true)
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
	source := newGitHubSource(Provider{BaseURL: "http://irrelevant"}, true)
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

	source := newGitHubSource(Provider{ClientID: "id", ClientSecret: "secret"}, true)
	source.config.Endpoint.TokenURL = srv.URL + "/login/oauth/access_token"

	tok, err := source.ExchangeToken(context.Background(), "the-code")
	if err != nil {
		t.Fatalf("ExchangeToken: %v", err)
	}
	if tok.AccessToken != "gho_test" {
		t.Errorf("AccessToken = %q, want gho_test", tok.AccessToken)
	}
}

// newWebhookHook returns the hook every provider webhook test installs.
func webhookUnderTest() Webhook {
	return Webhook{
		URL:    "https://cp.gotham.dev/api/v1/webhooks/github",
		Secret: "s3cr3t",
		Events: []string{"push"},
	}
}

func TestGitHubSourceCreateWebhook(t *testing.T) {
	var gotMethod, gotPath, gotAuth string
	var gotBody struct {
		Name   string            `json:"name"`
		Active bool              `json:"active"`
		Events []string          `json:"events"`
		Config map[string]string `json:"config"`
	}
	srv := serve(t, func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath, gotAuth = r.Method, r.URL.EscapedPath(), r.Header.Get("Authorization")
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Errorf("decode request: %v", err)
		}
		w.WriteHeader(http.StatusCreated)
		writeJSONTest(t, w, map[string]any{"id": 4242})
	})

	source := newGitHubSource(Provider{BaseURL: srv.URL}, true)
	id, err := source.CreateWebhook(context.Background(), staticToken, "o/r", webhookUnderTest())
	if err != nil {
		t.Fatalf("CreateWebhook: %v", err)
	}
	if id != "4242" {
		t.Errorf("id = %q, want 4242", id)
	}
	if gotMethod != http.MethodPost || gotPath != "/repos/o/r/hooks" {
		t.Errorf("request = %s %s, want POST /repos/o/r/hooks", gotMethod, gotPath)
	}
	if gotAuth != "Bearer test-token" {
		t.Errorf("Authorization = %q, want Bearer test-token", gotAuth)
	}
	if !gotBody.Active || gotBody.Name != "web" {
		t.Errorf("body = %+v, want an active \"web\" hook", gotBody)
	}
	if len(gotBody.Events) != 1 || gotBody.Events[0] != "push" {
		t.Errorf("events = %v, want [push]", gotBody.Events)
	}
	if gotBody.Config["url"] != webhookUnderTest().URL || gotBody.Config["secret"] != "s3cr3t" {
		t.Errorf("config = %v, want the control-plane url and secret", gotBody.Config)
	}
	if gotBody.Config["content_type"] != "json" {
		t.Errorf("content_type = %q, want json", gotBody.Config["content_type"])
	}
}

func TestGitHubSourceCreateWebhookFailure(t *testing.T) {
	srv := serve(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	})

	source := newGitHubSource(Provider{BaseURL: srv.URL}, true)
	if _, err := source.CreateWebhook(context.Background(), staticToken, "o/r", webhookUnderTest()); err == nil {
		t.Fatal("CreateWebhook on 403: no error, want failure")
	}
	if _, err := source.CreateWebhook(context.Background(), staticToken, "../hooks", webhookUnderTest()); err == nil {
		t.Fatal("CreateWebhook with a path-traversing repo: no error, want ErrValidation")
	}
}

func TestGitHubSourceDeleteWebhook(t *testing.T) {
	var gotMethod, gotPath string
	status := http.StatusNoContent
	srv := serve(t, func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.EscapedPath()
		w.WriteHeader(status)
	})

	source := newGitHubSource(Provider{BaseURL: srv.URL}, true)
	if err := source.DeleteWebhook(context.Background(), staticToken, "o/r", "4242"); err != nil {
		t.Fatalf("DeleteWebhook: %v", err)
	}
	if gotMethod != http.MethodDelete || gotPath != "/repos/o/r/hooks/4242" {
		t.Errorf("request = %s %s, want DELETE /repos/o/r/hooks/4242", gotMethod, gotPath)
	}

	// An already-removed hook is a success: deleting stays idempotent.
	status = http.StatusNotFound
	if err := source.DeleteWebhook(context.Background(), staticToken, "o/r", "4242"); err != nil {
		t.Errorf("DeleteWebhook on 404: %v, want nil", err)
	}

	status = http.StatusInternalServerError
	if err := source.DeleteWebhook(context.Background(), staticToken, "o/r", "4242"); err == nil {
		t.Error("DeleteWebhook on 500: no error, want failure")
	}
	if err := source.DeleteWebhook(context.Background(), staticToken, "o/r", "../../hooks"); err == nil {
		t.Error("DeleteWebhook with a path-traversing hook id: no error, want ErrValidation")
	}
	if source.Name() != NameGitHub {
		t.Errorf("Name = %q, want %q", source.Name(), NameGitHub)
	}
}
