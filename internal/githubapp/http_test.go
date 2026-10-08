package githubapp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// fakeGitHub serves the GitHub REST shapes the production client consumes, so
// the client is proven against the wire format without network access.
func fakeGitHub(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/app-manifests/test-code/conversions", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("conversions method = %s", r.Method)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":             77,
			"slug":           "gotham-fake",
			"name":           "gotham-fake",
			"client_id":      "cid",
			"webhook_secret": map[string]any{"secret": "shh"},
			"pem":            "fake-pem",
		})
	})
	mux.HandleFunc("/app/installations/999/access_tokens", func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); !strings.HasPrefix(got, "Bearer ") || strings.TrimSpace(strings.TrimPrefix(got, "Bearer ")) == "" {
			t.Errorf("token request has no bearer jwt")
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"token":      "inst-token-1",
			"expires_at": time.Now().Add(time.Hour).UTC().Format(time.RFC3339),
		})
	})
	mux.HandleFunc("/installation/repositories", func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer inst-token-1" {
			t.Errorf("repos auth = %q", got)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"repositories": []map[string]any{{
				"id": 1, "name": "web", "full_name": "acme/web", "private": true,
				"default_branch": "main", "clone_url": "https://example.com/acme/web.git",
			}},
		})
	})
	mux.HandleFunc("/repos/acme/web/branches", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode([]map[string]any{{
			"name": "main", "protected": true, "commit": map[string]any{"sha": "abc"},
		}})
	})
	return httptest.NewServer(mux)
}

func TestHTTPAPIAgainstFake(t *testing.T) {
	server := fakeGitHub(t)
	defer server.Close()

	api := NewHTTPAPI(server.URL, true)
	ctx := context.Background()

	conv, err := api.ExchangeManifest(ctx, "test-code")
	if err != nil {
		t.Fatal(err)
	}
	if conv.ID != 77 || conv.Slug != "gotham-fake" || conv.WebhookSecret != "shh" || conv.PEM != "fake-pem" {
		t.Fatalf("conversion = %+v", conv)
	}

	tok, err := api.CreateInstallationToken(ctx, 999, "test-jwt")
	if err != nil {
		t.Fatal(err)
	}
	if tok.Token != "inst-token-1" || time.Until(tok.ExpiresAt) > time.Hour {
		t.Fatalf("token = %+v", tok)
	}

	repos, err := api.ListInstallationRepos(ctx, tok.Token)
	if err != nil {
		t.Fatal(err)
	}
	if len(repos) != 1 || repos[0].FullName != "acme/web" || !repos[0].Private {
		t.Fatalf("repos = %+v", repos)
	}

	branches, err := api.ListBranches(ctx, tok.Token, "acme/web")
	if err != nil {
		t.Fatal(err)
	}
	if len(branches) != 1 || branches[0].Name != "main" || !branches[0].Protected {
		t.Fatalf("branches = %+v", branches)
	}

	if _, err := api.ListBranches(ctx, tok.Token, "not-a-repo"); err == nil {
		t.Fatal("bad repo name was accepted")
	}
}
