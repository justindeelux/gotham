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

// fakeGitHub serves the documented GitHub REST shapes the production client
// consumes, so the client is proven against the wire format without network
// access. Payloads mirror the real schemas: the manifest conversion returns
// webhook_secret as a plain string with owner/permissions/events siblings,
// the installation object carries account and app_id, and the token response
// carries permissions.
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
			"client_secret":  "csecret",
			"webhook_secret": "shh",
			"pem":            "fake-pem",
			"html_url":       "https://github.com/apps/gotham-fake",
			"owner":          map[string]any{"login": "acme", "id": 1},
			"permissions":    map[string]any{"contents": "read", "metadata": "read", "pull_requests": "write"},
			"events":         []string{"push", "pull_request"},
		})
	})
	mux.HandleFunc("/app/installations/999", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("installation method = %s", r.Method)
		}
		if got := r.Header.Get("Authorization"); !strings.HasPrefix(got, "Bearer ") {
			t.Errorf("installation request has no bearer jwt")
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": 999, "app_id": 77, "account": map[string]any{"login": "acme"},
		})
	})
	mux.HandleFunc("/app/installations/999/access_tokens", func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); !strings.HasPrefix(got, "Bearer ") || strings.TrimSpace(strings.TrimPrefix(got, "Bearer ")) == "" {
			t.Errorf("token request has no bearer jwt")
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"token":       "inst-token-1",
			"expires_at":  time.Now().Add(time.Hour).UTC().Format(time.RFC3339),
			"permissions": map[string]any{"contents": "read"},
			"repositories": []map[string]any{
				{"full_name": "acme/web"},
			},
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

// TestConversionAcceptsBothSecretShapes proves the decoder reads the
// documented plain-string webhook_secret and still accepts a nested object,
// so an Enterprise variant never breaks Connect.
func TestConversionAcceptsBothSecretShapes(t *testing.T) {
	for _, body := range []string{
		`{"id":1,"pem":"p","webhook_secret":"plain"}`,
		`{"id":1,"pem":"p","webhook_secret":{"secret":"nested"}}`,
	} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(body))
		}))
		api := NewHTTPAPI(server.URL, true)
		conv, err := api.ExchangeManifest(context.Background(), "code")
		server.Close()
		if err != nil {
			t.Fatalf("body %s: %v", body, err)
		}
		want := "plain"
		if strings.Contains(body, "nested") {
			want = "nested"
		}
		if conv.WebhookSecret != want {
			t.Fatalf("body %s: secret = %q, want %q", body, conv.WebhookSecret, want)
		}
	}
}

// TestRepoPagination proves multi-page installations list fully and a host
// that always answers full pages stops at the bound.
func TestRepoPagination(t *testing.T) {
	paged := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		page := r.URL.Query().Get("page")
		var repos []map[string]any
		switch page {
		case "", "1":
			// A full first page forces the walk on.
			for i := 0; i < repoListPageSize; i++ {
				repos = append(repos, map[string]any{
					"id": i, "name": "r", "full_name": "acme/r",
				})
			}
		case "2":
			repos = []map[string]any{{"id": 1000, "name": "last", "full_name": "acme/last"}}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"repositories": repos})
	}))
	defer paged.Close()

	api := NewHTTPAPI(paged.URL, true)
	repos, truncated, err := api.ListInstallationRepos(context.Background(), "tok")
	if err != nil {
		t.Fatal(err)
	}
	if len(repos) != repoListPageSize+1 || repos[repoListPageSize].FullName != "acme/last" {
		t.Fatalf("repos = %d, want %d", len(repos), repoListPageSize+1)
	}
	if truncated {
		t.Fatal("partial walk reports truncated")
	}

	endless := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		repos := make([]map[string]any, 0, repoListPageSize)
		for i := 0; i < repoListPageSize; i++ {
			repos = append(repos, map[string]any{"id": i, "full_name": "acme/r"})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"repositories": repos})
	}))
	defer endless.Close()

	bounded := NewHTTPAPI(endless.URL, true)
	repos, truncated, err = bounded.ListInstallationRepos(context.Background(), "tok")
	if err != nil {
		t.Fatal(err)
	}
	if len(repos) != repoListPageSize*maxRepoListPages {
		t.Fatalf("repos = %d, want the bounded %d", len(repos), repoListPageSize*maxRepoListPages)
	}
	if !truncated {
		t.Fatal("bound-hit listing does not report truncated")
	}
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

	info, err := api.GetInstallation(ctx, 999, "test-jwt")
	if err != nil {
		t.Fatal(err)
	}
	if info.ID != 999 || info.Account != "acme" || info.AppID != 77 {
		t.Fatalf("installation = %+v", info)
	}

	repos, truncated, err := api.ListInstallationRepos(ctx, tok.Token)
	if err != nil {
		t.Fatal(err)
	}
	if len(repos) != 1 || repos[0].FullName != "acme/web" || !repos[0].Private {
		t.Fatalf("repos = %+v", repos)
	}
	if truncated {
		t.Fatal("single-page listing reports truncated")
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
