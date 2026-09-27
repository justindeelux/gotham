package providers

import (
	"context"
	"encoding/json"
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

func TestGiteaSourceCreateWebhook(t *testing.T) {
	var gotMethod, gotPath string
	var gotBody struct {
		Type   string            `json:"type"`
		Events []string          `json:"events"`
		Config map[string]string `json:"config"`
	}
	srv := serve(t, func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.EscapedPath()
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Errorf("decode request: %v", err)
		}
		writeJSONTest(t, w, map[string]any{"id": 13})
	})

	source := newGiteaSource(Provider{BaseURL: srv.URL})
	id, err := source.CreateWebhook(context.Background(), staticToken, "t/r", Webhook{
		URL:    "https://cp.gotham.dev/api/v1/webhooks/gitea",
		Secret: "s3cr3t",
		Events: []string{"push"},
	})
	if err != nil {
		t.Fatalf("CreateWebhook: %v", err)
	}
	if id != "13" {
		t.Errorf("id = %q, want 13", id)
	}
	if gotMethod != http.MethodPost || gotPath != "/api/v1/repos/t/r/hooks" {
		t.Errorf("request = %s %s, want POST /api/v1/repos/t/r/hooks", gotMethod, gotPath)
	}
	if gotBody.Type != "gitea" || len(gotBody.Events) != 1 || gotBody.Events[0] != "push" {
		t.Errorf("body = %+v, want a gitea push hook", gotBody)
	}
	if gotBody.Config["url"] != "https://cp.gotham.dev/api/v1/webhooks/gitea" || gotBody.Config["secret"] != "s3cr3t" {
		t.Errorf("config = %v, want the control-plane url and secret", gotBody.Config)
	}
	if source.Name() != NameGitea {
		t.Errorf("Name = %q, want %q", source.Name(), NameGitea)
	}
}

func TestGiteaSourceDeleteWebhook(t *testing.T) {
	var gotMethod, gotPath string
	status := http.StatusNoContent
	srv := serve(t, func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.EscapedPath()
		w.WriteHeader(status)
	})

	source := newGiteaSource(Provider{BaseURL: srv.URL})
	if err := source.DeleteWebhook(context.Background(), staticToken, "t/r", "13"); err != nil {
		t.Fatalf("DeleteWebhook: %v", err)
	}
	if gotMethod != http.MethodDelete || gotPath != "/api/v1/repos/t/r/hooks/13" {
		t.Errorf("request = %s %s, want DELETE /api/v1/repos/t/r/hooks/13", gotMethod, gotPath)
	}

	status = http.StatusNotFound
	if err := source.DeleteWebhook(context.Background(), staticToken, "t/r", "13"); err != nil {
		t.Errorf("DeleteWebhook on 404: %v, want nil", err)
	}
	if err := source.DeleteWebhook(context.Background(), staticToken, "t/r", "13/../hooks"); err == nil {
		t.Error("DeleteWebhook with a path-traversing hook id: no error, want ErrValidation")
	}
}
