package server

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"testing"

	"github.com/justindeelux/gotham/internal/auth"
	"github.com/justindeelux/gotham/internal/config"
)

// newScopeTestServer builds a server with the fake auth, token and server
// services, so the token routes and the resource routes (servers) are mounted
// on the real router for the containment matrix.
func newScopeTestServer(t *testing.T) (*Server, *fakeTokenService) {
	t.Helper()

	cfg := &config.Config{
		Values: config.Values{Server: config.Server{Addr: "127.0.0.1", Port: 0}},
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	tokens := newFakeTokenService()

	s, err := New(cfg, logger, newFakeAuthService(), nil, tokens, newFakeServerService(), nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(s.closer)
	s.db = stubPinger{}
	s.redis = stubPinger{}
	return s, tokens
}

func mustCreateToken(t *testing.T, tokens *fakeTokenService, scopes ...string) string {
	t.Helper()
	created, err := tokens.Create(context.Background(), testUserID, "test", scopes)
	if err != nil {
		t.Fatalf("create %v token: %v", scopes, err)
	}
	return created.Token
}

// TestResourceScopeContainment is the FX-2c matrix: a read token may read a
// resource route but not mutate one, a deploy token may mutate, and a JWT
// session passes everything.
func TestResourceScopeContainment(t *testing.T) {
	s, tokens := newScopeTestServer(t)

	readToken := mustCreateToken(t, tokens, auth.ScopeRead)
	deployToken := mustCreateToken(t, tokens, auth.ScopeDeploy)
	const session = "Bearer valid-token"

	createBody := `{"name":"web-1","ip":"10.0.0.5","port":22,"ssh_user":"root"}`

	tests := map[string]struct {
		method string
		path   string
		body   string
		bearer string
		want   int
	}{
		"read token reads":         {http.MethodGet, "/api/v1/servers", "", "Bearer " + readToken, http.StatusOK},
		"read token cannot mutate": {http.MethodPost, "/api/v1/servers", createBody, "Bearer " + readToken, http.StatusForbidden},
		"deploy token mutates":     {http.MethodPost, "/api/v1/servers", createBody, "Bearer " + deployToken, http.StatusCreated},
		"jwt session reads":        {http.MethodGet, "/api/v1/servers", "", session, http.StatusOK},
		"jwt session mutates":      {http.MethodPost, "/api/v1/servers", createBody, session, http.StatusCreated},
		"no token is unauthorized": {http.MethodGet, "/api/v1/servers", "", "", http.StatusUnauthorized},
		"read token cannot mint":   {http.MethodPost, "/api/v1/tokens", `{"name":"x","scopes":["deploy"]}`, "Bearer " + readToken, http.StatusForbidden},
		"deploy cannot mint admin": {http.MethodPost, "/api/v1/tokens", `{"name":"x","scopes":["admin"]}`, "Bearer " + deployToken, http.StatusForbidden},
		"deploy can mint read":     {http.MethodPost, "/api/v1/tokens", `{"name":"x","scopes":["read"]}`, "Bearer " + deployToken, http.StatusCreated},
		"read can mint read":       {http.MethodPost, "/api/v1/tokens", `{"name":"x","scopes":["read"]}`, "Bearer " + readToken, http.StatusCreated},
		"jwt can mint deploy":      {http.MethodPost, "/api/v1/tokens", `{"name":"x","scopes":["deploy"]}`, session, http.StatusCreated},
	}

	for label, tc := range tests {
		t.Run(label, func(t *testing.T) {
			rec := doRequest(t, s, tc.method, tc.path, tc.body, tc.bearer)
			if rec.Code != tc.want {
				t.Fatalf("status = %d, want %d (body %s)", rec.Code, tc.want, rec.Body.String())
			}
		})
	}
}

// TestRotateRespectsScopeGrant proves a token cannot rotate a more privileged
// token: a read token rotating a deploy token is refused, while a deploy token
// rotating its own deploy token succeeds.
func TestRotateRespectsScopeGrant(t *testing.T) {
	s, tokens := newScopeTestServer(t)

	readToken := mustCreateToken(t, tokens, auth.ScopeRead)
	deployCreated, err := tokens.Create(context.Background(), testUserID, "deploy", []string{auth.ScopeDeploy})
	if err != nil {
		t.Fatalf("create deploy token: %v", err)
	}

	rec := doRequest(t, s, http.MethodPost, "/api/v1/tokens/"+deployCreated.ID.String()+"/rotate", "", "Bearer "+readToken)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("read token rotating deploy token = %d, want 403 (body %s)", rec.Code, rec.Body.String())
	}

	// The deploy token itself may rotate the deploy token.
	deployToken := deployCreated.Token
	rec = doRequest(t, s, http.MethodPost, "/api/v1/tokens/"+deployCreated.ID.String()+"/rotate", "", "Bearer "+deployToken)
	if rec.Code != http.StatusOK {
		t.Fatalf("deploy token rotating deploy token = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
}
