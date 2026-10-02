package server

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
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

func mustCreateAdminToken(t *testing.T, tokens *fakeTokenService) *auth.CreatedToken {
	t.Helper()
	created, err := tokens.Create(context.Background(), testUserID, "admin", []string{auth.ScopeAdmin})
	if err != nil {
		t.Fatalf("create admin token: %v", err)
	}
	return created
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
		method  string
		path    string
		body    string
		bearer  string
		want    int
		wantMsg string
	}{
		"read token reads":         {http.MethodGet, "/api/v1/servers", "", "Bearer " + readToken, http.StatusOK, ""},
		"read token cannot mutate": {http.MethodPost, "/api/v1/servers", createBody, "Bearer " + readToken, http.StatusForbidden, ""},
		"deploy token mutates":     {http.MethodPost, "/api/v1/servers", createBody, "Bearer " + deployToken, http.StatusCreated, ""},
		"jwt session reads":        {http.MethodGet, "/api/v1/servers", "", session, http.StatusOK, ""},
		"jwt session mutates":      {http.MethodPost, "/api/v1/servers", createBody, session, http.StatusCreated, ""},
		"no token is unauthorized": {http.MethodGet, "/api/v1/servers", "", "", http.StatusUnauthorized, ""},
		// The subset gate runs before the platform-operator gate, so the message
		// isolates CanGrantScopes (the deploy token is not a platform operator
		// either, but the subset check rejects it first).
		"read token cannot mint":   {http.MethodPost, "/api/v1/tokens", `{"name":"x","scopes":["deploy"]}`, "Bearer " + readToken, http.StatusForbidden, tokenScopeGrantDenied},
		"deploy cannot mint admin": {http.MethodPost, "/api/v1/tokens", `{"name":"x","scopes":["admin"]}`, "Bearer " + deployToken, http.StatusForbidden, tokenScopeGrantDenied},
		"deploy can mint read":     {http.MethodPost, "/api/v1/tokens", `{"name":"x","scopes":["read"]}`, "Bearer " + deployToken, http.StatusCreated, ""},
		"read can mint read":       {http.MethodPost, "/api/v1/tokens", `{"name":"x","scopes":["read"]}`, "Bearer " + readToken, http.StatusCreated, ""},
		"jwt can mint deploy":      {http.MethodPost, "/api/v1/tokens", `{"name":"x","scopes":["deploy"]}`, session, http.StatusCreated, ""},
	}

	for label, tc := range tests {
		t.Run(label, func(t *testing.T) {
			rec := doRequest(t, s, tc.method, tc.path, tc.body, tc.bearer)
			if rec.Code != tc.want {
				t.Fatalf("status = %d, want %d (body %s)", rec.Code, tc.want, rec.Body.String())
			}
			if tc.wantMsg != "" {
				var body apiError
				if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
					t.Fatalf("decode error body %q: %v", rec.Body.String(), err)
				}
				if body.Message != tc.wantMsg {
					t.Fatalf("message = %q, want %q", body.Message, tc.wantMsg)
				}
			}
		})
	}
}

// TestRotateRevokeRespectScopeGrant proves a token cannot rotate or revoke a
// more privileged token: a read token is refused on a deploy token, while the
// deploy token manages its own.
func TestRotateRevokeRespectScopeGrant(t *testing.T) {
	s, tokens := newScopeTestServer(t)

	readToken := mustCreateToken(t, tokens, auth.ScopeRead)
	rotateTarget, err := tokens.Create(context.Background(), testUserID, "deploy", []string{auth.ScopeDeploy})
	if err != nil {
		t.Fatalf("create rotate target: %v", err)
	}
	revokeTarget, err := tokens.Create(context.Background(), testUserID, "deploy", []string{auth.ScopeDeploy})
	if err != nil {
		t.Fatalf("create revoke target: %v", err)
	}

	// A read token cannot rotate a deploy token.
	rec := doRequest(t, s, http.MethodPost, "/api/v1/tokens/"+rotateTarget.ID.String()+"/rotate", "", "Bearer "+readToken)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("read token rotating deploy token = %d, want 403 (body %s)", rec.Code, rec.Body.String())
	}

	// The deploy token itself may rotate the deploy token.
	rec = doRequest(t, s, http.MethodPost, "/api/v1/tokens/"+rotateTarget.ID.String()+"/rotate", "", "Bearer "+rotateTarget.Token)
	if rec.Code != http.StatusOK {
		t.Fatalf("deploy token rotating deploy token = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}

	// A read token cannot revoke a deploy token.
	rec = doRequest(t, s, http.MethodDelete, "/api/v1/tokens/"+revokeTarget.ID.String(), "", "Bearer "+readToken)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("read token revoking deploy token = %d, want 403 (body %s)", rec.Code, rec.Body.String())
	}

	// The deploy token may revoke its own deploy token.
	rec = doRequest(t, s, http.MethodDelete, "/api/v1/tokens/"+revokeTarget.ID.String(), "", "Bearer "+revokeTarget.Token)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("deploy token revoking deploy token = %d, want 204 (body %s)", rec.Code, rec.Body.String())
	}

	// A read token may revoke a read token it owns.
	readTarget, err := tokens.Create(context.Background(), testUserID, "read", []string{auth.ScopeRead})
	if err != nil {
		t.Fatalf("create read target: %v", err)
	}
	if rec := doRequest(t, s, http.MethodDelete, "/api/v1/tokens/"+readTarget.ID.String(), "", "Bearer "+readToken); rec.Code != http.StatusNoContent {
		t.Fatalf("read token revoking read token = %d, want 204 (body %s)", rec.Code, rec.Body.String())
	}
}

// TestRotateAdminTokenRequiresPlatformOperator is the fix-round-1 R1 regression:
// a non-operator session owning an admin token must not be able to rotate it
// into a fresh admin secret. Fix round 2 S1: revoke is de-escalating, so the
// owner may always kill a leaked admin token even after losing operator status.
func TestRotateAdminTokenRequiresPlatformOperator(t *testing.T) {
	s, tokens := newScopeTestServer(t)
	const session = "Bearer valid-token"

	rotateTarget := mustCreateAdminToken(t, tokens)
	revokeTarget := mustCreateAdminToken(t, tokens)

	rec := doRequest(t, s, http.MethodPost, "/api/v1/tokens/"+rotateTarget.ID.String()+"/rotate", "", session)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("non-operator rotating admin token = %d, want 403 (body %s)", rec.Code, rec.Body.String())
	}
	var body apiError
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode error body: %v", err)
	}
	if body.Message != platformAdminScopeDenied {
		t.Fatalf("message = %q, want %q", body.Message, platformAdminScopeDenied)
	}

	// Revoke stays available to the owner: a non-operator may kill their own
	// admin token.
	rec = doRequest(t, s, http.MethodDelete, "/api/v1/tokens/"+revokeTarget.ID.String(), "", session)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("non-operator revoking admin token = %d, want 204 (body %s)", rec.Code, rec.Body.String())
	}

	// An operator-listed session may rotate the surviving token.
	t.Setenv(PlatformAdminsEnv, "user@example.com")
	if rec := doRequest(t, s, http.MethodPost, "/api/v1/tokens/"+rotateTarget.ID.String()+"/rotate", "", session); rec.Code != http.StatusOK {
		t.Fatalf("operator rotating admin token = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
}

// TestRequireResourceScopesClassifier exercises the method/path classification
// directly, including the deploy-gated secret read.
func TestRequireResourceScopesClassifier(t *testing.T) {
	stub := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	handler := requireResourceScopes(stub)

	tests := map[string]struct {
		method string
		path   string
		scopes []string
		api    bool
		want   int
	}{
		"read reads":                   {http.MethodGet, "/api/v1/servers", []string{auth.ScopeRead}, true, http.StatusOK},
		"deploy reads":                 {http.MethodGet, "/api/v1/servers", []string{auth.ScopeDeploy}, true, http.StatusOK},
		"read cannot mutate":           {http.MethodPost, "/api/v1/servers", []string{auth.ScopeRead}, true, http.StatusForbidden},
		"deploy mutates":               {http.MethodPost, "/api/v1/servers", []string{auth.ScopeDeploy}, true, http.StatusOK},
		"read cannot delete":           {http.MethodDelete, "/api/v1/servers/x", []string{auth.ScopeRead}, true, http.StatusForbidden},
		"read cannot read credentials": {http.MethodGet, "/api/v1/databases/x/credentials", []string{auth.ScopeRead}, true, http.StatusForbidden},
		"deploy reads credentials":     {http.MethodGet, "/api/v1/databases/x/credentials", []string{auth.ScopeDeploy}, true, http.StatusOK},
		"admin reads credentials":      {http.MethodGet, "/api/v1/databases/x/credentials", []string{auth.ScopeAdmin}, true, http.StatusOK},
		"jwt reads credentials":        {http.MethodGet, "/api/v1/databases/x/credentials", nil, false, http.StatusOK},
		"jwt mutates":                  {http.MethodPost, "/api/v1/servers", nil, false, http.StatusOK},
	}

	for label, tc := range tests {
		t.Run(label, func(t *testing.T) {
			ctx := context.Background()
			if tc.api {
				ctx = context.WithValue(ctx, scopesKey, tc.scopes)
			}
			req := httptest.NewRequest(tc.method, tc.path, nil).WithContext(ctx)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code != tc.want {
				t.Fatalf("status = %d, want %d (body %s)", rec.Code, tc.want, rec.Body.String())
			}
		})
	}
}

// TestAuthScopeWrappers covers the four chains the server mounts. It pins the
// presence and ordering of RequireAuth plus the scope gate on each: admin
// rejects read/deploy and accepts admin/JWT, the resource and team chains split
// reads from mutations, and every chain rejects a missing token. Note that
// readScopeAuth is not distinguishable from bare RequireAuth for a valid token,
// because read is the baseline scope every token carries (deploy/admin imply
// it); its row pins the authentication leg, and the intent is documented on the
// helper itself.
func TestAuthScopeWrappers(t *testing.T) {
	s, tokens := newScopeTestServer(t)
	readToken := mustCreateToken(t, tokens, auth.ScopeRead)
	deployToken := mustCreateToken(t, tokens, auth.ScopeDeploy)
	adminToken := mustCreateToken(t, tokens, auth.ScopeAdmin)
	const session = "Bearer valid-token"

	wrappers := map[string]func(http.Handler) http.Handler{
		"readScopeAuth":     s.readScopeAuth,
		"adminScopeAuth":    s.adminScopeAuth,
		"resourceScopeAuth": s.resourceScopeAuth,
		"withTeam":          s.withTeam(),
	}

	tests := map[string]struct {
		wrapper string
		method  string
		bearer  string
		want    int
	}{
		"read accepts read":           {"readScopeAuth", http.MethodGet, "Bearer " + readToken, http.StatusOK},
		"read accepts deploy":         {"readScopeAuth", http.MethodGet, "Bearer " + deployToken, http.StatusOK},
		"read accepts admin":          {"readScopeAuth", http.MethodGet, "Bearer " + adminToken, http.StatusOK},
		"read accepts jwt":            {"readScopeAuth", http.MethodGet, session, http.StatusOK},
		"read rejects none":           {"readScopeAuth", http.MethodGet, "", http.StatusUnauthorized},
		"admin rejects read":          {"adminScopeAuth", http.MethodGet, "Bearer " + readToken, http.StatusForbidden},
		"admin rejects deploy":        {"adminScopeAuth", http.MethodGet, "Bearer " + deployToken, http.StatusForbidden},
		"admin accepts admin":         {"adminScopeAuth", http.MethodGet, "Bearer " + adminToken, http.StatusOK},
		"admin accepts jwt":           {"adminScopeAuth", http.MethodGet, session, http.StatusOK},
		"admin rejects none":          {"adminScopeAuth", http.MethodGet, "", http.StatusUnauthorized},
		"resource read reads":         {"resourceScopeAuth", http.MethodGet, "Bearer " + readToken, http.StatusOK},
		"resource read cannot mutate": {"resourceScopeAuth", http.MethodPost, "Bearer " + readToken, http.StatusForbidden},
		"resource deploy mutates":     {"resourceScopeAuth", http.MethodPost, "Bearer " + deployToken, http.StatusOK},
		"resource jwt mutates":        {"resourceScopeAuth", http.MethodPost, session, http.StatusOK},
		"resource rejects none":       {"resourceScopeAuth", http.MethodPost, "", http.StatusUnauthorized},
		"team read reads":             {"withTeam", http.MethodGet, "Bearer " + readToken, http.StatusOK},
		"team read cannot mutate":     {"withTeam", http.MethodPost, "Bearer " + readToken, http.StatusForbidden},
		"team deploy mutates":         {"withTeam", http.MethodPost, "Bearer " + deployToken, http.StatusOK},
		"team rejects none":           {"withTeam", http.MethodPost, "", http.StatusUnauthorized},
	}

	for label, tc := range tests {
		t.Run(label, func(t *testing.T) {
			stub := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
			handler := wrappers[tc.wrapper](stub)

			req := httptest.NewRequest(tc.method, "/api/v1/servers", nil)
			if tc.bearer != "" {
				req.Header.Set("Authorization", tc.bearer)
			}
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code != tc.want {
				t.Fatalf("status = %d, want %d (body %s)", rec.Code, tc.want, rec.Body.String())
			}
		})
	}
}

// TestTeamsMountRequiresAdminScope pins the production teams mount wiring
// (server.go passes s.adminScopeAuth): a read token is refused before the team
// service, so reverting that call to RequireAuth fails here. teams.Mount is a
// no-op without a team service, so the router is rebuilt with one injected.
func TestTeamsMountRequiresAdminScope(t *testing.T) {
	s, tokens := newScopeTestServer(t)
	readToken := mustCreateToken(t, tokens, auth.ScopeRead)
	adminToken := mustCreateToken(t, tokens, auth.ScopeAdmin)

	s.teamService = newFakeTeamService()
	router, err := s.routes()
	if err != nil {
		t.Fatalf("routes: %v", err)
	}
	s.router = router

	rec := doRequest(t, s, http.MethodGet, "/api/v1/teams", "", "Bearer "+readToken)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("read token GET /api/v1/teams = %d, want 403 (body %s)", rec.Code, rec.Body.String())
	}

	// An admin token passes the scope gate; the fake team service then answers
	// an unimplemented error, so the route is reached rather than refused.
	if rec := doRequest(t, s, http.MethodGet, "/api/v1/teams", "", "Bearer "+adminToken); rec.Code == http.StatusForbidden {
		t.Fatalf("admin token GET /api/v1/teams = 403, want the scope gate to pass")
	}
}
