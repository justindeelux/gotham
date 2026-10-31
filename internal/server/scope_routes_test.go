package server

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/auth"
	"github.com/justindeelux/gotham/internal/config"
)

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

func mustCreateOwnerToken(t *testing.T, tokens *fakeTokenService, scopes ...string) string {
	t.Helper()
	created, err := tokens.Create(context.Background(), testOwnerID, "owner", scopes)
	if err != nil {
		t.Fatalf("create owner %v token: %v", scopes, err)
	}
	return created.Token
}

func mustCreateAdminToken(t *testing.T, tokens *fakeTokenService, userID uuid.UUID) *auth.CreatedToken {
	t.Helper()
	created, err := tokens.Create(context.Background(), userID, "admin", []string{auth.ScopeAdmin})
	if err != nil {
		t.Fatalf("create admin token: %v", err)
	}
	return created
}

func TestResourceScopeContainment(t *testing.T) {
	s, tokens := newScopeTestServer(t)

	readToken := mustCreateToken(t, tokens, auth.ScopeRead)
	deployToken := mustCreateToken(t, tokens, auth.ScopeDeploy)
	ownerReadToken := mustCreateOwnerToken(t, tokens, auth.ScopeRead)
	ownerDeployToken := mustCreateOwnerToken(t, tokens, auth.ScopeDeploy)
	ownerAdminToken := mustCreateOwnerToken(t, tokens, auth.ScopeAdmin)
	memberAdminToken := mustCreateToken(t, tokens, auth.ScopeAdmin)
	const session = "Bearer valid-token"
	const createBody = `{"name":"web-1","ip":"10.0.0.5","port":22,"ssh_user":"root"}`

	registry := s.servers.(*fakeServerService)
	serverID := seedServer(t, registry, "web-read")
	key, err := registry.AddPrivateKey(context.Background(), "deploy-key",
		"-----BEGIN OPENSSH PRIVATE KEY-----\nfake\n-----END OPENSSH PRIVATE KEY-----\n", "", "")
	if err != nil {
		t.Fatalf("seed private key: %v", err)
	}
	metricsPath := "/api/v1/servers/" + serverID.String() + "/metrics?from=2026-01-01T00:00:00Z&to=2026-01-01T01:00:00Z"

	tests := map[string]struct {
		method  string
		path    string
		body    string
		bearer  string
		want    int
		wantMsg string
	}{
		"read token cannot mint":                   {http.MethodPost, "/api/v1/tokens", `{"name":"x","scopes":["deploy"]}`, "Bearer " + readToken, http.StatusForbidden, tokenScopeGrantDenied},
		"deploy cannot mint admin":                 {http.MethodPost, "/api/v1/tokens", `{"name":"x","scopes":["admin"]}`, "Bearer " + deployToken, http.StatusForbidden, tokenScopeGrantDenied},
		"deploy can mint read":                     {http.MethodPost, "/api/v1/tokens", `{"name":"x","scopes":["read"]}`, "Bearer " + deployToken, http.StatusCreated, ""},
		"read can mint read":                       {http.MethodPost, "/api/v1/tokens", `{"name":"x","scopes":["read"]}`, "Bearer " + readToken, http.StatusCreated, ""},
		"jwt can mint deploy":                      {http.MethodPost, "/api/v1/tokens", `{"name":"x","scopes":["deploy"]}`, session, http.StatusCreated, ""},
		"owner read token reads servers":           {http.MethodGet, "/api/v1/servers", "", "Bearer " + ownerReadToken, http.StatusOK, ""},
		"owner read token reads server":            {http.MethodGet, "/api/v1/servers/" + serverID.String(), "", "Bearer " + ownerReadToken, http.StatusOK, ""},
		"owner read token reads metrics":           {http.MethodGet, metricsPath, "", "Bearer " + ownerReadToken, http.StatusOK, ""},
		"owner deploy token reads servers":         {http.MethodGet, "/api/v1/servers", "", "Bearer " + ownerDeployToken, http.StatusOK, ""},
		"owner deploy token cannot create servers": {http.MethodPost, "/api/v1/servers", createBody, "Bearer " + ownerDeployToken, http.StatusForbidden, ""},
		"owner admin token reads servers":          {http.MethodGet, "/api/v1/servers", "", "Bearer " + ownerAdminToken, http.StatusOK, ""},
		"owner read token cannot mutate":           {http.MethodPost, "/api/v1/servers", createBody, "Bearer " + ownerReadToken, http.StatusForbidden, ""},
		"owner read token cannot list keys":        {http.MethodGet, "/api/v1/private-keys", "", "Bearer " + ownerReadToken, http.StatusForbidden, ""},
		"owner deploy token cannot list keys":      {http.MethodGet, "/api/v1/private-keys", "", "Bearer " + ownerDeployToken, http.StatusForbidden, ""},
		"owner read token cannot get key":          {http.MethodGet, "/api/v1/private-keys/" + key.ID.String(), "", "Bearer " + ownerReadToken, http.StatusForbidden, ""},
		"owner deploy token cannot get key":        {http.MethodGet, "/api/v1/private-keys/" + key.ID.String(), "", "Bearer " + ownerDeployToken, http.StatusForbidden, ""},
		"owner read token cannot reveal key":       {http.MethodPost, "/api/v1/private-keys/" + key.ID.String() + "/reveal", "", "Bearer " + ownerReadToken, http.StatusForbidden, ""},
		"owner deploy token cannot delete server":  {http.MethodDelete, "/api/v1/servers/" + serverID.String(), "", "Bearer " + ownerDeployToken, http.StatusForbidden, ""},
		"owner admin token creates servers":        {http.MethodPost, "/api/v1/servers", createBody, "Bearer " + ownerAdminToken, http.StatusCreated, ""},
		"member read token cannot read":            {http.MethodGet, "/api/v1/servers", "", "Bearer " + readToken, http.StatusNotFound, ""},
		"member deploy token cannot mutate":        {http.MethodPost, "/api/v1/servers", createBody, "Bearer " + deployToken, http.StatusNotFound, ""},
		"member admin token cannot read":           {http.MethodGet, "/api/v1/servers", "", "Bearer " + memberAdminToken, http.StatusNotFound, ""},
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

func TestServerOperatorMutationsRequireAdminScope(t *testing.T) {
	s, tokens := newScopeTestServer(t)
	serverID := uuid.NewString()
	keyID := uuid.NewString()

	routes := []struct {
		name   string
		method string
		path   string
		body   string
	}{
		{"create server", http.MethodPost, "/api/v1/servers", `{"name":"web-1","ip":"10.0.0.5","ssh_user":"root"}`},
		{"update server", http.MethodPatch, "/api/v1/servers/" + serverID, `{"name":"web-2"}`},
		{"delete server", http.MethodDelete, "/api/v1/servers/" + serverID, ""},
		{"validate server", http.MethodPost, "/api/v1/servers/" + serverID + "/validate", ""},
		{"install key", http.MethodPost, "/api/v1/servers/" + serverID + "/install-key", ""},
		{"install docker", http.MethodPost, "/api/v1/servers/" + serverID + "/docker-install", ""},
		{"reset host key", http.MethodDelete, "/api/v1/servers/" + serverID + "/host-key", ""},
		{"create private key", http.MethodPost, "/api/v1/private-keys", `{"name":"k","private_key":"pem"}`},
		{"generate private key", http.MethodPost, "/api/v1/private-keys/generate", `{"name":"k","type":"ed25519"}`},
		{"list private keys", http.MethodGet, "/api/v1/private-keys", ""},
		{"get private key", http.MethodGet, "/api/v1/private-keys/" + keyID, ""},
		{"update private key", http.MethodPatch, "/api/v1/private-keys/" + keyID, `{"name":"k2"}`},
		{"delete private key", http.MethodDelete, "/api/v1/private-keys/" + keyID, ""},
		{"reveal private key", http.MethodPost, "/api/v1/private-keys/" + keyID + "/reveal", ""},
	}

	for _, scope := range []string{auth.ScopeRead, auth.ScopeDeploy} {
		token := mustCreateOwnerToken(t, tokens, scope)
		for _, route := range routes {
			t.Run(scope+" "+route.name, func(t *testing.T) {
				rec := doRequest(t, s, route.method, route.path, route.body, "Bearer "+token)
				if rec.Code != http.StatusForbidden {
					t.Fatalf("%s %s = %d, want 403 (body %s)", route.method, route.path, rec.Code, rec.Body.String())
				}
			})
		}
	}
}

func TestPrivateKeyRevealRequiresAdminScope(t *testing.T) {
	s, tokens := newScopeTestServer(t)
	registry := s.servers.(*fakeServerService)
	key, err := registry.AddPrivateKey(context.Background(), "deploy-key",
		"-----BEGIN OPENSSH PRIVATE KEY-----\nfake\n-----END OPENSSH PRIVATE KEY-----\n", "", "")
	if err != nil {
		t.Fatalf("seed private key: %v", err)
	}

	path := "/api/v1/private-keys/" + key.ID.String() + "/reveal"
	for _, scope := range []string{auth.ScopeRead, auth.ScopeDeploy} {
		token := mustCreateOwnerToken(t, tokens, scope)
		if rec := doRequest(t, s, http.MethodPost, path, "", "Bearer "+token); rec.Code != http.StatusForbidden {
			t.Fatalf("owner %s token reveal = %d, want 403 (body %s)", scope, rec.Code, rec.Body.String())
		}
	}

	if rec := doRequest(t, s, http.MethodPost, path, "", "Bearer "+mustCreateOwnerToken(t, tokens, auth.ScopeAdmin)); rec.Code != http.StatusOK {
		t.Fatalf("owner admin token reveal = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	if rec := doRequest(t, s, http.MethodPost, path, "", "Bearer owner-token"); rec.Code != http.StatusOK {
		t.Fatalf("owner session reveal = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
}

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

	rec := doRequest(t, s, http.MethodPost, "/api/v1/tokens/"+rotateTarget.ID.String()+"/rotate", "", "Bearer "+readToken)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("read token rotating deploy token = %d, want 403 (body %s)", rec.Code, rec.Body.String())
	}

	rec = doRequest(t, s, http.MethodPost, "/api/v1/tokens/"+rotateTarget.ID.String()+"/rotate", "", "Bearer "+rotateTarget.Token)
	if rec.Code != http.StatusOK {
		t.Fatalf("deploy token rotating deploy token = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}

	rec = doRequest(t, s, http.MethodDelete, "/api/v1/tokens/"+revokeTarget.ID.String(), "", "Bearer "+readToken)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("read token revoking deploy token = %d, want 403 (body %s)", rec.Code, rec.Body.String())
	}

	rec = doRequest(t, s, http.MethodDelete, "/api/v1/tokens/"+revokeTarget.ID.String(), "", "Bearer "+revokeTarget.Token)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("deploy token revoking deploy token = %d, want 204 (body %s)", rec.Code, rec.Body.String())
	}

	readTarget, err := tokens.Create(context.Background(), testUserID, "read", []string{auth.ScopeRead})
	if err != nil {
		t.Fatalf("create read target: %v", err)
	}
	if rec := doRequest(t, s, http.MethodDelete, "/api/v1/tokens/"+readTarget.ID.String(), "", "Bearer "+readToken); rec.Code != http.StatusNoContent {
		t.Fatalf("read token revoking read token = %d, want 204 (body %s)", rec.Code, rec.Body.String())
	}
}

func TestRotateAdminTokenRequiresOwner(t *testing.T) {
	s, tokens := newScopeTestServer(t)
	const session = "Bearer valid-token"

	rotateTarget := mustCreateAdminToken(t, tokens, testUserID)
	revokeTarget := mustCreateAdminToken(t, tokens, testUserID)
	ownerTarget := mustCreateAdminToken(t, tokens, testOwnerID)

	rec := doRequest(t, s, http.MethodPost, "/api/v1/tokens/"+rotateTarget.ID.String()+"/rotate", "", session)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("member rotating admin token = %d, want 403 (body %s)", rec.Code, rec.Body.String())
	}
	var body apiError
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode error body: %v", err)
	}
	if body.Message != ownerScopeDenied {
		t.Fatalf("message = %q, want %q", body.Message, ownerScopeDenied)
	}

	rec = doRequest(t, s, http.MethodDelete, "/api/v1/tokens/"+revokeTarget.ID.String(), "", session)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("member revoking admin token = %d, want 204 (body %s)", rec.Code, rec.Body.String())
	}

	if rec := doRequest(t, s, http.MethodPost, "/api/v1/tokens/"+ownerTarget.ID.String()+"/rotate", "", "Bearer owner-token"); rec.Code != http.StatusOK {
		t.Fatalf("owner rotating admin token = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
}

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
		"read reads":                              {http.MethodGet, "/api/v1/servers", []string{auth.ScopeRead}, true, http.StatusOK},
		"deploy reads":                            {http.MethodGet, "/api/v1/servers", []string{auth.ScopeDeploy}, true, http.StatusOK},
		"read cannot mutate":                      {http.MethodPost, "/api/v1/servers", []string{auth.ScopeRead}, true, http.StatusForbidden},
		"deploy mutates":                          {http.MethodPost, "/api/v1/servers", []string{auth.ScopeDeploy}, true, http.StatusOK},
		"read cannot delete":                      {http.MethodDelete, "/api/v1/servers/x", []string{auth.ScopeRead}, true, http.StatusForbidden},
		"read cannot read credentials":            {http.MethodGet, "/api/v1/databases/x/credentials", []string{auth.ScopeRead}, true, http.StatusForbidden},
		"deploy reads credentials":                {http.MethodGet, "/api/v1/databases/x/credentials", []string{auth.ScopeDeploy}, true, http.StatusOK},
		"admin reads credentials":                 {http.MethodGet, "/api/v1/databases/x/credentials", []string{auth.ScopeAdmin}, true, http.StatusOK},
		"jwt reads credentials":                   {http.MethodGet, "/api/v1/databases/x/credentials", nil, false, http.StatusOK},
		"read cannot read files content":          {http.MethodGet, "/api/v1/servers/x/files/content", []string{auth.ScopeRead}, true, http.StatusForbidden},
		"deploy reads files content":              {http.MethodGet, "/api/v1/servers/x/files/content", []string{auth.ScopeDeploy}, true, http.StatusOK},
		"admin reads files content":               {http.MethodGet, "/api/v1/servers/x/files/content", []string{auth.ScopeAdmin}, true, http.StatusOK},
		"jwt reads files content":                 {http.MethodGet, "/api/v1/servers/x/files/content", nil, false, http.StatusOK},
		"read cannot read content trailing slash": {http.MethodGet, "/api/v1/servers/x/files/content/", []string{auth.ScopeRead}, true, http.StatusForbidden},
		"read cannot read content with query":     {http.MethodGet, "/api/v1/servers/x/files/content?root=r&path=p", []string{auth.ScopeRead}, true, http.StatusForbidden},
		"read cannot read history version":        {http.MethodGet, "/api/v1/servers/x/files/history/2f8c1b4e-8a1d-4f45-9c2a-6b1e7d3a5c90", []string{auth.ScopeRead}, true, http.StatusForbidden},
		"deploy reads history version":            {http.MethodGet, "/api/v1/servers/x/files/history/2f8c1b4e-8a1d-4f45-9c2a-6b1e7d3a5c90", []string{auth.ScopeDeploy}, true, http.StatusOK},
		"admin reads history version":             {http.MethodGet, "/api/v1/servers/x/files/history/2f8c1b4e-8a1d-4f45-9c2a-6b1e7d3a5c90", []string{auth.ScopeAdmin}, true, http.StatusOK},
		"jwt reads history version":               {http.MethodGet, "/api/v1/servers/x/files/history/2f8c1b4e-8a1d-4f45-9c2a-6b1e7d3a5c90", nil, false, http.StatusOK},
		"read reads history list":                 {http.MethodGet, "/api/v1/servers/x/files/history", []string{auth.ScopeRead}, true, http.StatusOK},
		"read reads files roots":                  {http.MethodGet, "/api/v1/servers/x/files/roots", []string{auth.ScopeRead}, true, http.StatusOK},
		"read reads files list":                   {http.MethodGet, "/api/v1/servers/x/files/list", []string{auth.ScopeRead}, true, http.StatusOK},
		"read reads files stat":                   {http.MethodGet, "/api/v1/servers/x/files/stat", []string{auth.ScopeRead}, true, http.StatusOK},
		"read cannot mint download ticket":        {http.MethodPost, "/api/v1/servers/x/files/download-ticket", []string{auth.ScopeRead}, true, http.StatusForbidden},
		"deploy mints download ticket":            {http.MethodPost, "/api/v1/servers/x/files/download-ticket", []string{auth.ScopeDeploy}, true, http.StatusOK},
		"read cannot put file content":            {http.MethodPut, "/api/v1/servers/x/files/content", []string{auth.ScopeRead}, true, http.StatusForbidden},
		"deploy puts file content":                {http.MethodPut, "/api/v1/servers/x/files/content", []string{auth.ScopeDeploy}, true, http.StatusOK},
		"jwt puts file content":                   {http.MethodPut, "/api/v1/servers/x/files/content", nil, false, http.StatusOK},
		"read cannot upload":                      {http.MethodPost, "/api/v1/servers/x/files/upload", []string{auth.ScopeRead}, true, http.StatusForbidden},
		"deploy uploads":                          {http.MethodPost, "/api/v1/servers/x/files/upload", []string{auth.ScopeDeploy}, true, http.StatusOK},
		"read cannot mkdir":                       {http.MethodPost, "/api/v1/servers/x/files/mkdir", []string{auth.ScopeRead}, true, http.StatusForbidden},
		"read cannot create":                      {http.MethodPost, "/api/v1/servers/x/files/create", []string{auth.ScopeRead}, true, http.StatusForbidden},
		"read cannot rename":                      {http.MethodPost, "/api/v1/servers/x/files/rename", []string{auth.ScopeRead}, true, http.StatusForbidden},
		"read cannot move":                        {http.MethodPost, "/api/v1/servers/x/files/move", []string{auth.ScopeRead}, true, http.StatusForbidden},
		"read cannot copy":                        {http.MethodPost, "/api/v1/servers/x/files/copy", []string{auth.ScopeRead}, true, http.StatusForbidden},
		"read cannot chmod":                       {http.MethodPost, "/api/v1/servers/x/files/chmod", []string{auth.ScopeRead}, true, http.StatusForbidden},
		"deploy chmods":                           {http.MethodPost, "/api/v1/servers/x/files/chmod", []string{auth.ScopeDeploy}, true, http.StatusOK},
		"read cannot compress":                    {http.MethodPost, "/api/v1/servers/x/files/compress", []string{auth.ScopeRead}, true, http.StatusForbidden},
		"deploy compresses":                       {http.MethodPost, "/api/v1/servers/x/files/compress", []string{auth.ScopeDeploy}, true, http.StatusOK},
		"jwt compresses":                          {http.MethodPost, "/api/v1/servers/x/files/compress", nil, false, http.StatusOK},
		"read cannot extract":                     {http.MethodPost, "/api/v1/servers/x/files/extract", []string{auth.ScopeRead}, true, http.StatusForbidden},
		"deploy extracts":                         {http.MethodPost, "/api/v1/servers/x/files/extract", []string{auth.ScopeDeploy}, true, http.StatusOK},
		"read cannot delete entry":                {http.MethodDelete, "/api/v1/servers/x/files/entry", []string{auth.ScopeRead}, true, http.StatusForbidden},
		"deploy deletes entry":                    {http.MethodDelete, "/api/v1/servers/x/files/entry", []string{auth.ScopeDeploy}, true, http.StatusOK},
		"read cannot mkdir trailing slash":        {http.MethodPost, "/api/v1/servers/x/files/mkdir/", []string{auth.ScopeRead}, true, http.StatusForbidden},
		"read cannot mkdir with query":            {http.MethodPost, "/api/v1/servers/x/files/mkdir?root=r&path=p", []string{auth.ScopeRead}, true, http.StatusForbidden},
		"read cannot upload double slash":         {http.MethodPost, "/api/v1/servers/x/files//upload", []string{auth.ScopeRead}, true, http.StatusForbidden},
		"read cannot plan transfer":               {http.MethodPost, "/api/v1/servers/x/files/transfers/plan", []string{auth.ScopeRead}, true, http.StatusForbidden},
		"deploy plans transfer":                   {http.MethodPost, "/api/v1/servers/x/files/transfers/plan", []string{auth.ScopeDeploy}, true, http.StatusOK},
		"admin plans transfer":                    {http.MethodPost, "/api/v1/servers/x/files/transfers/plan", []string{auth.ScopeAdmin}, true, http.StatusOK},
		"read cannot execute transfer":            {http.MethodPost, "/api/v1/servers/x/files/transfers", []string{auth.ScopeRead}, true, http.StatusForbidden},
		"deploy executes transfer":                {http.MethodPost, "/api/v1/servers/x/files/transfers", []string{auth.ScopeDeploy}, true, http.StatusOK},
		"jwt mutates":                             {http.MethodPost, "/api/v1/servers", nil, false, http.StatusOK},
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

func TestFilesContentScopeGate(t *testing.T) {
	s, tokens := newScopeTestServer(t)
	readToken := mustCreateOwnerToken(t, tokens, auth.ScopeRead)
	deployToken := mustCreateOwnerToken(t, tokens, auth.ScopeDeploy)
	id := uuid.New()
	path := "/api/v1/servers/" + id.String() + "/files/content?root=volumes&path=app.env"

	rec := doRequest(t, s, http.MethodGet, path, "", "Bearer "+readToken)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("read token content = %d, want 403 (body %s)", rec.Code, rec.Body.String())
	}
	var body apiError
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode error body: %v", err)
	}
	if body.Message != "owner access is required" {
		t.Fatalf("message = %q, want %q", body.Message, "owner access is required")
	}

	rec = doRequest(t, s, http.MethodGet, path, "", "Bearer "+deployToken)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("deploy token content = %d (body %s), want the platform-operator refusal", rec.Code, rec.Body.String())
	}

	rec = doRequest(t, s, http.MethodGet, path, "", "Bearer "+mustCreateOwnerToken(t, tokens, auth.ScopeAdmin))
	if rec.Code == http.StatusForbidden || rec.Code == http.StatusUnauthorized {
		t.Fatalf("admin token content = %d, want the gates to pass", rec.Code)
	}
}

func TestFilesHistoryVersionScopeGate(t *testing.T) {
	s, tokens := newScopeTestServer(t)
	readToken := mustCreateToken(t, tokens, auth.ScopeRead)
	deployToken := mustCreateToken(t, tokens, auth.ScopeDeploy)
	id := uuid.New()
	path := "/api/v1/servers/" + id.String() + "/files/history/" + uuid.NewString() + "?root=volumes&path=a.txt"

	rec := doRequest(t, s, http.MethodGet, path, "", "Bearer "+readToken)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("read token history version = %d, want 403 (body %s)", rec.Code, rec.Body.String())
	}
	var body apiError
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode error body: %v", err)
	}
	if body.Message != "insufficient scope" {
		t.Fatalf("message = %q, want %q", body.Message, "insufficient scope")
	}

	rec = doRequest(t, s, http.MethodGet, path, "", "Bearer "+deployToken)
	if rec.Code == http.StatusForbidden || strings.Contains(rec.Body.String(), "insufficient scope") {
		t.Fatalf("deploy token history version = %d (body %s), want the platform-operator refusal", rec.Code, rec.Body.String())
	}

	rec = doRequest(t, s, http.MethodGet, path, "", "Bearer "+mustCreateOwnerToken(t, tokens, auth.ScopeAdmin))
	if rec.Code == http.StatusForbidden || rec.Code == http.StatusUnauthorized {
		t.Fatalf("admin token history version = %d, want the gates to pass", rec.Code)
	}

	listPath := "/api/v1/servers/" + id.String() + "/files/history?root=volumes&path=a.txt"
	rec = doRequest(t, s, http.MethodGet, listPath, "", "Bearer "+readToken)
	if rec.Code == http.StatusForbidden || rec.Code == http.StatusUnauthorized {
		t.Fatalf("read token history list = %d, want the gates to pass (body %s)", rec.Code, rec.Body.String())
	}
}

func TestFilesWriteScopeGate(t *testing.T) {
	s, tokens := newScopeTestServer(t)
	readToken := mustCreateOwnerToken(t, tokens, auth.ScopeRead)
	deployToken := mustCreateOwnerToken(t, tokens, auth.ScopeDeploy)
	adminToken := mustCreateOwnerToken(t, tokens, auth.ScopeAdmin)
	id := uuid.New()
	base := "/api/v1/servers/" + id.String() + "/files/"

	cases := []struct {
		name   string
		method string
		path   string
		body   string
	}{
		{"put content", http.MethodPut, base + "content?root=volumes&path=a", ""},
		{"upload", http.MethodPost, base + "upload?root=volumes&path=", ""},
		{"mkdir", http.MethodPost, base + "mkdir", `{"root":"volumes","path":"d"}`},
		{"create", http.MethodPost, base + "create", `{"root":"volumes","path":"a"}`},
		{"rename", http.MethodPost, base + "rename", `{"root":"volumes","source":"a","destination":"b"}`},
		{"move", http.MethodPost, base + "move", `{"root":"volumes","source":"a","destination":"b"}`},
		{"copy", http.MethodPost, base + "copy", `{"root":"volumes","source":"a","destination":"b"}`},
		{"chmod", http.MethodPost, base + "chmod", `{"root":"volumes","path":"a","mode":384}`},
		{"delete", http.MethodDelete, base + "entry?root=volumes&path=a", ""},
		{"plan transfer", http.MethodPost, base + "transfers/plan", `{"source_root":"volumes","source_directory":"","sources":["a"],"destination":{"server_id":"` + uuid.NewString() + `","root":"volumes","path":""}}`},
		{"execute transfer", http.MethodPost, base + "transfers", `{"source_root":"volumes","source_directory":"","sources":["a"],"destination":{"server_id":"` + uuid.NewString() + `","root":"volumes","path":""}}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := doRequest(t, s, tc.method, tc.path, tc.body, "Bearer "+readToken)
			if rec.Code != http.StatusForbidden {
				t.Fatalf("read token = %d, want 403 (body %s)", rec.Code, rec.Body.String())
			}
			var body apiError
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatalf("decode error body: %v", err)
			}
			if body.Message != "owner access is required" {
				t.Fatalf("message = %q, want %q", body.Message, "owner access is required")
			}
			rec = doRequest(t, s, tc.method, tc.path, tc.body, "Bearer "+deployToken)
			if rec.Code != http.StatusForbidden {
				t.Fatalf("deploy token = %d (body %s), want the platform-operator refusal", rec.Code, rec.Body.String())
			}
			rec = doRequest(t, s, tc.method, tc.path, tc.body, "Bearer "+adminToken)
			if rec.Code == http.StatusForbidden || rec.Code == http.StatusUnauthorized {
				t.Fatalf("admin token = %d, want the gates to pass (body %s)", rec.Code, rec.Body.String())
			}
		})
	}
}

func TestAuthScopeWrappers(t *testing.T) {
	s, tokens := newScopeTestServer(t)
	readToken := mustCreateToken(t, tokens, auth.ScopeRead)
	deployToken := mustCreateToken(t, tokens, auth.ScopeDeploy)
	adminToken := mustCreateToken(t, tokens, auth.ScopeAdmin)
	const session = "Bearer valid-token"

	wrappers := map[string]func(http.Handler) http.Handler{
		"readScopeAuth":           s.readScopeAuth,
		"adminScopeAuth":          s.adminScopeAuth,
		"resourceScopeAuth":       s.resourceScopeAuth,
		"applicationTerminalAuth": s.applicationTerminalAuth(),
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
