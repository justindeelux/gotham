package providers

import (
	"context"
	"encoding/json"
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

// TestGitLabSourceListReposMembershipAndOffsetLimit is the C1-8 regression:
// the listing must ask only for the user's member projects and must stop
// gracefully at GitLab's 50k offset ceiling instead of failing.
func TestGitLabSourceListReposMembershipAndOffsetLimit(t *testing.T) {
	var sawMembership bool
	var requests int
	srv := serve(t, func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.URL.Query().Get("membership") != "true" {
			t.Errorf("membership = %q, want true", r.URL.Query().Get("membership"))
		}
		sawMembership = true
		if r.URL.Query().Get("page") == "1" {
			batch := make([]map[string]any, 0, gitLabPageSize)
			for i := 0; i < gitLabPageSize; i++ {
				batch = append(batch, map[string]any{
					"id": i, "name": "r", "path_with_namespace": "o/r", "visibility": "private",
				})
			}
			writeJSONTest(t, w, batch)
			return
		}
		// Past the offset ceiling GitLab answers 400.
		w.WriteHeader(http.StatusBadRequest)
	})

	source := newGitLabSource(Provider{BaseURL: srv.URL})
	repos, err := source.ListRepos(context.Background(), staticToken)
	if err != nil {
		t.Fatalf("ListRepos: %v", err)
	}
	if !sawMembership {
		t.Fatal("listing never asked for membership")
	}
	if requests != 2 {
		t.Fatalf("requests = %d, want 2 (page 1 then the offset-limit 400)", requests)
	}
	if len(repos) != gitLabPageSize {
		t.Fatalf("len(repos) = %d, want the first page", len(repos))
	}
}

// TestGitLabSourceListReposBounded proves the loop is bounded even when the
// instance keeps returning full pages.
func TestGitLabSourceListReposBounded(t *testing.T) {
	var requests int
	srv := serve(t, func(w http.ResponseWriter, _ *http.Request) {
		requests++
		batch := make([]map[string]any, 0, gitLabPageSize)
		for i := 0; i < gitLabPageSize; i++ {
			batch = append(batch, map[string]any{"id": i, "name": "r", "path_with_namespace": "o/r"})
		}
		writeJSONTest(t, w, batch)
	})

	source := newGitLabSource(Provider{BaseURL: srv.URL})
	repos, err := source.ListRepos(context.Background(), staticToken)
	if err != nil {
		t.Fatalf("ListRepos: %v", err)
	}
	if requests != maxRepoPages || len(repos) != maxRepoPages*gitLabPageSize {
		t.Fatalf("requests = %d, repos = %d, want the %d-page cap", requests, len(repos), maxRepoPages)
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

func TestGitLabSourceCreateWebhook(t *testing.T) {
	// F1 regression: the events list must translate "pull_request" into
	// merge_requests_events, or a previews-enabled hook never delivers MR
	// notifications.
	cases := []struct {
		name         string
		events       []string
		wantPush     bool
		wantRequests bool
	}{
		{"previews enabled", []string{"push", "pull_request"}, true, true},
		{"previews disabled", []string{"push"}, true, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var gotMethod, gotPath string
			var gotBody struct {
				URL                string `json:"url"`
				Token              string `json:"token"`
				PushEvents         bool   `json:"push_events"`
				MergeRequestsEvent bool   `json:"merge_requests_events"`
				SSL                bool   `json:"enable_ssl_verification"`
			}
			srv := serve(t, func(w http.ResponseWriter, r *http.Request) {
				gotMethod, gotPath = r.Method, r.URL.EscapedPath()
				if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
					t.Errorf("decode request: %v", err)
				}
				w.WriteHeader(http.StatusCreated)
				writeJSONTest(t, w, map[string]any{"id": 77})
			})

			source := newGitLabSource(Provider{BaseURL: "https://gitlab.com"})
			source.apiBase = srv.URL
			id, err := source.CreateWebhook(context.Background(), staticToken, "group/project", Webhook{
				URL:    "https://cp.gotham.dev/api/v1/webhooks/gitlab",
				Secret: "s3cr3t",
				Events: tc.events,
			})
			if err != nil {
				t.Fatalf("CreateWebhook: %v", err)
			}
			if id != "77" {
				t.Errorf("id = %q, want 77", id)
			}
			if gotMethod != http.MethodPost || gotPath != "/projects/group%2Fproject/hooks" {
				t.Errorf("request = %s %s, want POST /projects/group%%2Fproject/hooks", gotMethod, gotPath)
			}
			if gotBody.URL != "https://cp.gotham.dev/api/v1/webhooks/gitlab" || gotBody.Token != "s3cr3t" {
				t.Errorf("body = %+v, want the control-plane url and token", gotBody)
			}
			if gotBody.PushEvents != tc.wantPush || gotBody.MergeRequestsEvent != tc.wantRequests || !gotBody.SSL {
				t.Errorf("body = %+v, want push=%v merge_requests=%v with TLS verification",
					gotBody, tc.wantPush, tc.wantRequests)
			}
		})
	}
}

func TestGitLabSourceDeleteWebhook(t *testing.T) {
	var gotMethod, gotPath string
	status := http.StatusNoContent
	srv := serve(t, func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.EscapedPath()
		w.WriteHeader(status)
	})

	source := newGitLabSource(Provider{BaseURL: "https://gitlab.com"})
	source.apiBase = srv.URL
	if err := source.DeleteWebhook(context.Background(), staticToken, "group/project", "77"); err != nil {
		t.Fatalf("DeleteWebhook: %v", err)
	}
	if gotMethod != http.MethodDelete || gotPath != "/projects/group%2Fproject/hooks/77" {
		t.Errorf("request = %s %s, want DELETE /projects/group%%2Fproject/hooks/77", gotMethod, gotPath)
	}

	status = http.StatusNotFound
	if err := source.DeleteWebhook(context.Background(), staticToken, "group/project", "77"); err != nil {
		t.Errorf("DeleteWebhook on 404: %v, want nil", err)
	}
	if err := source.DeleteWebhook(context.Background(), staticToken, "group/project", "7;drop"); err == nil {
		t.Error("DeleteWebhook with a non-numeric hook id: no error, want ErrValidation")
	}
}
