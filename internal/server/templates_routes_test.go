package server

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/justindeelux/gotham/internal/auth"
	"github.com/justindeelux/gotham/internal/services"
)

// TestTemplatesRoutesWiring proves the BE-7.2 surface is mounted on the real
// router behind the same auth and admin-scope chain as the services routes:
// a missing token gets 401, a read-scoped token gets 403 without reaching the
// catalog, and a session token serves the embedded catalog and a render.
func TestTemplatesRoutesWiring(t *testing.T) {
	s, tokens := newTestTokenServer(t)
	const session = "Bearer valid-token"

	if recorder := doRequest(t, s, http.MethodGet, "/api/v1/templates", "", ""); recorder.Code != http.StatusUnauthorized {
		t.Fatalf("missing token status = %d, want 401", recorder.Code)
	}
	readToken, err := tokens.Create(context.Background(), testUserID, "read-only", []string{auth.ScopeRead})
	if err != nil {
		t.Fatalf("create read token: %v", err)
	}
	if recorder := doRequest(t, s, http.MethodGet, "/api/v1/templates", "", "Bearer "+readToken.Token); recorder.Code != http.StatusForbidden {
		t.Fatalf("read token status = %d, want 403", recorder.Code)
	}

	recorder := doRequest(t, s, http.MethodGet, "/api/v1/templates", "", session)
	if recorder.Code != http.StatusOK {
		t.Fatalf("GET /api/v1/templates = %d: %s", recorder.Code, recorder.Body)
	}
	var catalog struct {
		Templates []struct {
			Slug string `json:"slug"`
		} `json:"templates"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &catalog); err != nil {
		t.Fatalf("decode catalog: %v", err)
	}
	if len(catalog.Templates) != 4 {
		t.Fatalf("catalog = %+v, want the four built-in templates", catalog.Templates)
	}

	recorder = doRequest(t, s, http.MethodPost, "/api/v1/templates/wordpress/render",
		`{"values":{"domain":"blog.example.test","db_password":"wp-secret","db_root_password":"root-secret"}}`, session)
	if recorder.Code != http.StatusOK {
		t.Fatalf("render = %d: %s", recorder.Code, recorder.Body)
	}
	var rendered struct {
		ComposeYAML string            `json:"compose_yaml"`
		Env         map[string]string `json:"env"`
		Spec        struct {
			Domains []struct {
				Domain string `json:"domain"`
			} `json:"domains"`
		} `json:"spec"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &rendered); err != nil {
		t.Fatalf("decode render: %v", err)
	}
	if len(rendered.Spec.Domains) != 1 || rendered.Spec.Domains[0].Domain != "blog.example.test" {
		t.Fatalf("rendered domains = %+v", rendered.Spec.Domains)
	}
	// The secret boundary the gallery depends on: the value travels in env,
	// the document only references it.
	if got := rendered.Env["db_password"]; got != "wp-secret" {
		t.Errorf("env[db_password] = %q", got)
	}
	if strings.Contains(rendered.ComposeYAML, "wp-secret") {
		t.Fatalf("the rendered document contains the secret:\n%s", rendered.ComposeYAML)
	}
}

// TestTemplatesRoutesKillSwitch proves FEATURE_SERVICES=false unmounts the
// template surface together with the service routes.
func TestTemplatesRoutesKillSwitch(t *testing.T) {
	t.Setenv(services.FeatureEnv, "false")
	s, _ := newTestTokenServer(t)
	if recorder := doRequest(t, s, http.MethodGet, "/api/v1/templates", "", "Bearer valid-token"); recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 (nothing mounted)", recorder.Code)
	}
}
