package providers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"

	"github.com/google/uuid"
)

// deployKeyUnderTest is the public key every deploy-key test registers.
func deployKeyUnderTest() DeployKey {
	return DeployKey{Title: "gotham:demo", Key: "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5 gotham:deploy:abc"}
}

func TestGitHubSourceAddDeployKey(t *testing.T) {
	var gotMethod, gotPath, gotAuth string
	var gotBody struct {
		Title    string `json:"title"`
		Key      string `json:"key"`
		ReadOnly bool   `json:"read_only"`
	}
	srv := serve(t, func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath, gotAuth = r.Method, r.URL.EscapedPath(), r.Header.Get("Authorization")
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Errorf("decode request: %v", err)
		}
		w.WriteHeader(http.StatusCreated)
		writeJSONTest(t, w, map[string]any{"id": 77})
	})

	source := newGitHubSource(Provider{BaseURL: srv.URL})
	id, err := source.AddDeployKey(context.Background(), staticToken, "o/r", deployKeyUnderTest())
	if err != nil {
		t.Fatalf("AddDeployKey: %v", err)
	}
	if id != "77" {
		t.Errorf("id = %q, want 77", id)
	}
	if gotMethod != http.MethodPost || gotPath != "/repos/o/r/keys" {
		t.Errorf("request = %s %s, want POST /repos/o/r/keys", gotMethod, gotPath)
	}
	if gotAuth != "Bearer test-token" {
		t.Errorf("Authorization = %q, want Bearer test-token", gotAuth)
	}
	if gotBody.Title != deployKeyUnderTest().Title || gotBody.Key != deployKeyUnderTest().Key {
		t.Errorf("body = %+v, want the title and public key", gotBody)
	}
	if !gotBody.ReadOnly {
		t.Error("read_only = false, deploy keys must not push")
	}
}

func TestGitHubSourceAddDeployKeyFailures(t *testing.T) {
	srv := serve(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	})

	source := newGitHubSource(Provider{BaseURL: srv.URL})
	if _, err := source.AddDeployKey(context.Background(), staticToken, "o/r", deployKeyUnderTest()); err == nil {
		t.Error("AddDeployKey on 403: no error, want failure")
	}
	if _, err := source.AddDeployKey(context.Background(), staticToken, "../keys", deployKeyUnderTest()); !errors.Is(err, ErrValidation) {
		t.Errorf("path-traversing repo: error = %v, want ErrValidation", err)
	}
	if _, err := source.AddDeployKey(context.Background(), staticToken, "o/r", DeployKey{Title: "x"}); !errors.Is(err, ErrValidation) {
		t.Errorf("empty key: error = %v, want ErrValidation", err)
	}
}

func TestGitHubSourceRemoveDeployKey(t *testing.T) {
	var gotMethod, gotPath string
	status := http.StatusNoContent
	srv := serve(t, func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.EscapedPath()
		w.WriteHeader(status)
	})

	source := newGitHubSource(Provider{BaseURL: srv.URL})
	if err := source.RemoveDeployKey(context.Background(), staticToken, "o/r", "77"); err != nil {
		t.Fatalf("RemoveDeployKey: %v", err)
	}
	if gotMethod != http.MethodDelete || gotPath != "/repos/o/r/keys/77" {
		t.Errorf("request = %s %s, want DELETE /repos/o/r/keys/77", gotMethod, gotPath)
	}

	// An already-removed key is a success: removing stays idempotent.
	status = http.StatusNotFound
	if err := source.RemoveDeployKey(context.Background(), staticToken, "o/r", "77"); err != nil {
		t.Errorf("RemoveDeployKey on 404: %v, want nil", err)
	}

	status = http.StatusInternalServerError
	if err := source.RemoveDeployKey(context.Background(), staticToken, "o/r", "77"); err == nil {
		t.Error("RemoveDeployKey on 500: no error, want failure")
	}
	if err := source.RemoveDeployKey(context.Background(), staticToken, "o/r", "7;drop"); !errors.Is(err, ErrValidation) {
		t.Errorf("non-numeric key id: error = %v, want ErrValidation", err)
	}
}

func TestGitLabSourceAddDeployKey(t *testing.T) {
	var gotMethod, gotPath string
	var gotBody struct {
		Title string `json:"title"`
		Key   string `json:"key"`
	}
	srv := serve(t, func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.EscapedPath()
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Errorf("decode request: %v", err)
		}
		w.WriteHeader(http.StatusCreated)
		writeJSONTest(t, w, map[string]any{"id": 91})
	})

	source := newGitLabSource(Provider{BaseURL: "https://gitlab.com"})
	source.apiBase = srv.URL
	id, err := source.AddDeployKey(context.Background(), staticToken, "group/project", deployKeyUnderTest())
	if err != nil {
		t.Fatalf("AddDeployKey: %v", err)
	}
	if id != "91" {
		t.Errorf("id = %q, want 91", id)
	}
	if gotMethod != http.MethodPost || gotPath != "/projects/group%2Fproject/deploy_keys" {
		t.Errorf("request = %s %s, want POST /projects/group%%2Fproject/deploy_keys", gotMethod, gotPath)
	}
	if gotBody.Title != deployKeyUnderTest().Title || gotBody.Key != deployKeyUnderTest().Key {
		t.Errorf("body = %+v, want the title and public key", gotBody)
	}
}

func TestGitLabSourceRemoveDeployKey(t *testing.T) {
	var gotMethod, gotPath string
	status := http.StatusNoContent
	srv := serve(t, func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.EscapedPath()
		w.WriteHeader(status)
	})

	source := newGitLabSource(Provider{BaseURL: "https://gitlab.com"})
	source.apiBase = srv.URL
	if err := source.RemoveDeployKey(context.Background(), staticToken, "group/project", "91"); err != nil {
		t.Fatalf("RemoveDeployKey: %v", err)
	}
	if gotMethod != http.MethodDelete || gotPath != "/projects/group%2Fproject/deploy_keys/91" {
		t.Errorf("request = %s %s, want DELETE /projects/group%%2Fproject/deploy_keys/91", gotMethod, gotPath)
	}

	status = http.StatusNotFound
	if err := source.RemoveDeployKey(context.Background(), staticToken, "group/project", "91"); err != nil {
		t.Errorf("RemoveDeployKey on 404: %v, want nil", err)
	}
	if err := source.RemoveDeployKey(context.Background(), staticToken, "group/project", "9;1"); !errors.Is(err, ErrValidation) {
		t.Errorf("non-numeric key id: error = %v, want ErrValidation", err)
	}
}

func TestGiteaSourceAddDeployKey(t *testing.T) {
	var gotMethod, gotPath string
	var gotBody struct {
		Title    string `json:"title"`
		Key      string `json:"key"`
		ReadOnly bool   `json:"read_only"`
	}
	srv := serve(t, func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.EscapedPath()
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Errorf("decode request: %v", err)
		}
		w.WriteHeader(http.StatusCreated)
		writeJSONTest(t, w, map[string]any{"id": 13})
	})

	source := newGiteaSource(Provider{BaseURL: srv.URL})
	id, err := source.AddDeployKey(context.Background(), staticToken, "t/r", deployKeyUnderTest())
	if err != nil {
		t.Fatalf("AddDeployKey: %v", err)
	}
	if id != "13" {
		t.Errorf("id = %q, want 13", id)
	}
	if gotMethod != http.MethodPost || gotPath != "/api/v1/repos/t/r/keys" {
		t.Errorf("request = %s %s, want POST /api/v1/repos/t/r/keys", gotMethod, gotPath)
	}
	if !gotBody.ReadOnly || gotBody.Key != deployKeyUnderTest().Key {
		t.Errorf("body = %+v, want a read-only deploy key", gotBody)
	}
}

func TestGiteaSourceRemoveDeployKey(t *testing.T) {
	var gotMethod, gotPath string
	status := http.StatusNoContent
	srv := serve(t, func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.EscapedPath()
		w.WriteHeader(status)
	})

	source := newGiteaSource(Provider{BaseURL: srv.URL})
	if err := source.RemoveDeployKey(context.Background(), staticToken, "t/r", "13"); err != nil {
		t.Fatalf("RemoveDeployKey: %v", err)
	}
	if gotMethod != http.MethodDelete || gotPath != "/api/v1/repos/t/r/keys/13" {
		t.Errorf("request = %s %s, want DELETE /api/v1/repos/t/r/keys/13", gotMethod, gotPath)
	}

	status = http.StatusNotFound
	if err := source.RemoveDeployKey(context.Background(), staticToken, "t/r", "13"); err != nil {
		t.Errorf("RemoveDeployKey on 404: %v, want nil", err)
	}
	if err := source.RemoveDeployKey(context.Background(), staticToken, "t/r", "13/../keys"); !errors.Is(err, ErrValidation) {
		t.Errorf("path-traversing key id: error = %v, want ErrValidation", err)
	}
}

// TestServiceAddDeployKey proves the service layer resolves the caller's stored
// connection and hands the key to the provider implementation.
func TestServiceAddDeployKey(t *testing.T) {
	var gotPath string
	srv := serve(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.EscapedPath()
		w.WriteHeader(http.StatusCreated)
		writeJSONTest(t, w, map[string]any{"id": 5})
	})

	repo := newFakeRepo()
	provider := seedProvider(t, repo, Provider{Name: NameGitHub, BaseURL: srv.URL})
	svc := NewService(Config{Repository: repo, Logger: discardLogger()})

	id, err := svc.AddDeployKey(context.Background(), HookTarget{
		UserID:   provider.UserID,
		Provider: NameGitHub,
		Repo:     "o/r",
	}, deployKeyUnderTest())
	if err != nil {
		t.Fatalf("AddDeployKey: %v", err)
	}
	if id != "5" || gotPath != "/repos/o/r/keys" {
		t.Errorf("id = %q, path = %q; want 5 and /repos/o/r/keys", id, gotPath)
	}
}

// TestServiceRemoveDeployKey mirrors the webhook delete: the stored connection
// authenticates the call and a 404 stays a success.
func TestServiceRemoveDeployKey(t *testing.T) {
	status := http.StatusNoContent
	srv := serve(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(status)
	})

	repo := newFakeRepo()
	provider := seedProvider(t, repo, Provider{Name: NameGitHub, BaseURL: srv.URL})
	svc := NewService(Config{Repository: repo, Logger: discardLogger()})
	target := HookTarget{UserID: provider.UserID, Provider: NameGitHub, Repo: "o/r"}

	if err := svc.RemoveDeployKey(context.Background(), target, "5"); err != nil {
		t.Fatalf("RemoveDeployKey: %v", err)
	}
	status = http.StatusNotFound
	if err := svc.RemoveDeployKey(context.Background(), target, "5"); err != nil {
		t.Errorf("RemoveDeployKey on 404: %v, want nil", err)
	}
	if err := svc.RemoveDeployKey(context.Background(), target, "5;drop"); !errors.Is(err, ErrValidation) {
		t.Errorf("non-numeric key id: error = %v, want ErrValidation", err)
	}
}

// TestServiceAddDeployKeyConnectionErrors covers the two connection failures
// the deploy routes translate into statuses.
func TestServiceAddDeployKeyConnectionErrors(t *testing.T) {
	repo := newFakeRepo()
	svc := NewService(Config{Repository: repo, Logger: discardLogger()})

	_, err := svc.AddDeployKey(context.Background(), HookTarget{
		UserID:   uuid.New(),
		Provider: NameGitHub,
		Repo:     "o/r",
	}, deployKeyUnderTest())
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("no connection: error = %v, want ErrNotFound", err)
	}

	provider := seedProvider(t, repo, Provider{Name: NameGitHub, BaseURL: "http://irrelevant"})
	provider.AccessToken = ""
	repo.providers[provider.ID] = provider
	_, err = svc.AddDeployKey(context.Background(), HookTarget{
		UserID:   provider.UserID,
		Provider: NameGitHub,
		Repo:     "o/r",
	}, deployKeyUnderTest())
	if !errors.Is(err, ErrNotConnected) {
		t.Errorf("disconnected: error = %v, want ErrNotConnected", err)
	}
}
