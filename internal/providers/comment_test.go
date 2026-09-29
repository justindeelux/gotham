package providers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"

	"github.com/google/uuid"
)

// TestGitHubSourceCreatePullRequestComment asserts the GitHub issue-comment
// endpoint and payload.
func TestGitHubSourceCreatePullRequestComment(t *testing.T) {
	var gotMethod, gotPath, gotAccept string
	var gotBody struct {
		Body string `json:"body"`
	}
	srv := serve(t, func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath, gotAccept = r.Method, r.URL.EscapedPath(), r.Header.Get("Accept")
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Errorf("decode request: %v", err)
		}
		w.WriteHeader(http.StatusCreated)
		writeJSONTest(t, w, map[string]any{"id": 1})
	})

	source := newGitHubSource(Provider{BaseURL: srv.URL})
	if err := source.CreatePullRequestComment(context.Background(), staticToken, "o/r", 7, "preview started"); err != nil {
		t.Fatalf("CreatePullRequestComment: %v", err)
	}
	if gotMethod != http.MethodPost || gotPath != "/repos/o/r/issues/7/comments" {
		t.Errorf("request = %s %s, want POST /repos/o/r/issues/7/comments", gotMethod, gotPath)
	}
	if gotAccept != gitHubAccept {
		t.Errorf("Accept = %q, want %q", gotAccept, gitHubAccept)
	}
	if gotBody.Body != "preview started" {
		t.Errorf("body = %+v", gotBody)
	}

	if err := source.CreatePullRequestComment(context.Background(), staticToken, "o/r", 0, "x"); !errors.Is(err, ErrValidation) {
		t.Errorf("number 0 = %v, want ErrValidation", err)
	}
	if err := source.CreatePullRequestComment(context.Background(), staticToken, "o/r", 7, "  "); !errors.Is(err, ErrValidation) {
		t.Errorf("blank body = %v, want ErrValidation", err)
	}
	if err := source.CreatePullRequestComment(context.Background(), staticToken, "../issues", 7, "x"); !errors.Is(err, ErrValidation) {
		t.Errorf("traversing repo = %v, want ErrValidation", err)
	}
}

// TestGitLabSourceCreatePullRequestComment asserts the GitLab merge-request
// note endpoint and payload.
func TestGitLabSourceCreatePullRequestComment(t *testing.T) {
	var gotMethod, gotPath string
	var gotBody struct {
		Body string `json:"body"`
	}
	srv := serve(t, func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.EscapedPath()
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Errorf("decode request: %v", err)
		}
		w.WriteHeader(http.StatusCreated)
		writeJSONTest(t, w, map[string]any{"id": 2})
	})

	source := newGitLabSource(Provider{BaseURL: "https://gitlab.com"})
	source.apiBase = srv.URL
	if err := source.CreatePullRequestComment(context.Background(), staticToken, "group/project", 9, "preview started"); err != nil {
		t.Fatalf("CreatePullRequestComment: %v", err)
	}
	if gotMethod != http.MethodPost || gotPath != "/projects/group%2Fproject/merge_requests/9/notes" {
		t.Errorf("request = %s %s, want POST /projects/group%%2Fproject/merge_requests/9/notes", gotMethod, gotPath)
	}
	if gotBody.Body != "preview started" {
		t.Errorf("body = %+v", gotBody)
	}

	if err := source.CreatePullRequestComment(context.Background(), staticToken, "group/project", -1, "x"); !errors.Is(err, ErrValidation) {
		t.Errorf("negative number = %v, want ErrValidation", err)
	}
}

// TestGiteaSourceCreatePullRequestComment asserts the Gitea issue-comment
// endpoint and payload.
func TestGiteaSourceCreatePullRequestComment(t *testing.T) {
	var gotMethod, gotPath string
	var gotBody struct {
		Body string `json:"body"`
	}
	srv := serve(t, func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.EscapedPath()
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Errorf("decode request: %v", err)
		}
		w.WriteHeader(http.StatusCreated)
		writeJSONTest(t, w, map[string]any{"id": 3})
	})

	source := newGiteaSource(Provider{BaseURL: srv.URL})
	if err := source.CreatePullRequestComment(context.Background(), staticToken, "t/r", 4, "preview started"); err != nil {
		t.Fatalf("CreatePullRequestComment: %v", err)
	}
	if gotMethod != http.MethodPost || gotPath != "/api/v1/repos/t/r/issues/4/comments" {
		t.Errorf("request = %s %s, want POST /api/v1/repos/t/r/issues/4/comments", gotMethod, gotPath)
	}
	if gotBody.Body != "preview started" {
		t.Errorf("body = %+v", gotBody)
	}
}

// TestServiceCreatePullRequestCommentUsesStoredConnection drives the service
// seam: the stored connection authenticates the call and the provider's
// endpoint receives it.
func TestServiceCreatePullRequestCommentUsesStoredConnection(t *testing.T) {
	var gotPath string
	srv := serve(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.EscapedPath()
		w.WriteHeader(http.StatusCreated)
		writeJSONTest(t, w, map[string]any{"id": 4})
	})

	repo := newFakeRepo()
	provider := seedProvider(t, repo, Provider{Name: NameGitHub, BaseURL: srv.URL})
	svc := NewService(Config{Repository: repo, Logger: discardLogger()})

	target := HookTarget{
		UserID: provider.UserID, Provider: NameGitHub,
		CloneURL: "https://github.com/o/r.git", Repo: "o/r",
	}
	if err := svc.CreatePullRequestComment(context.Background(), target, 7, "hello"); err != nil {
		t.Fatalf("CreatePullRequestComment: %v", err)
	}
	if gotPath != "/repos/o/r/issues/7/comments" {
		t.Errorf("path = %q", gotPath)
	}

	// A user without a connection cannot comment; the delivery then stays
	// comment-free (the caller treats this as best effort).
	if err := svc.CreatePullRequestComment(context.Background(), HookTarget{
		UserID: uuid.New(), Provider: NameGitHub, Repo: "o/r",
	}, 7, "hello"); !errors.Is(err, ErrNotFound) {
		t.Errorf("unconnected user = %v, want ErrNotFound", err)
	}
}
