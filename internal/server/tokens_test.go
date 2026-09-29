package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/auth"
	"github.com/justindeelux/gotham/internal/config"
)

// fakeTokenService is an in-memory TokenService for handler tests.
type fakeTokenService struct {
	mu     sync.Mutex
	tokens map[uuid.UUID]fakeTokenRecord
	seq    int
}

// fakeTokenRecord is one stored token and its ownership.
type fakeTokenRecord struct {
	created  auth.CreatedToken
	owner    uuid.UUID
	revoked  bool
	lastUsed *time.Time
}

func newFakeTokenService() *fakeTokenService {
	return &fakeTokenService{tokens: make(map[uuid.UUID]fakeTokenRecord)}
}

func (f *fakeTokenService) Create(_ context.Context, userID uuid.UUID, name string, scopes []string) (*auth.CreatedToken, error) {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" || len(trimmed) > 64 {
		return nil, fmt.Errorf("%w: name must be between 1 and 64 characters", auth.ErrValidation)
	}
	normalized, err := normalizeFakeScopes(scopes)
	if err != nil {
		return nil, err
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	f.seq++
	created := auth.CreatedToken{
		ID:        uuid.New(),
		Name:      trimmed,
		Scopes:    normalized,
		Token:     fmt.Sprintf("%s%043d", auth.APITokenPrefix, f.seq),
		CreatedAt: time.Now().UTC(),
	}
	f.tokens[created.ID] = fakeTokenRecord{created: created, owner: userID}
	return &created, nil
}

func (f *fakeTokenService) List(_ context.Context, userID uuid.UUID) ([]auth.APIToken, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	tokens := make([]auth.APIToken, 0, len(f.tokens))
	for _, record := range f.tokens {
		if record.owner != userID {
			continue
		}
		tokens = append(tokens, auth.APIToken{
			ID:         record.created.ID,
			Name:       record.created.Name,
			Scopes:     record.created.Scopes,
			LastUsedAt: record.lastUsed,
			RevokedAt:  fakeRevokedAt(record),
			CreatedAt:  record.created.CreatedAt,
		})
	}
	return tokens, nil
}

func (f *fakeTokenService) Revoke(_ context.Context, userID, id uuid.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	record, ok := f.tokens[id]
	if !ok || record.owner != userID || record.revoked {
		return auth.ErrNotFound
	}
	record.revoked = true
	f.tokens[id] = record
	return nil
}

func (f *fakeTokenService) Rotate(_ context.Context, userID, id uuid.UUID) (*auth.CreatedToken, error) {
	f.mu.Lock()
	record, ok := f.tokens[id]
	if !ok || record.owner != userID || record.revoked {
		f.mu.Unlock()
		return nil, auth.ErrNotFound
	}
	name, scopes := record.created.Name, record.created.Scopes
	record.revoked = true
	f.tokens[id] = record
	f.mu.Unlock()

	return f.Create(context.Background(), userID, name, scopes)
}

func (f *fakeTokenService) Authenticate(_ context.Context, token string) (*auth.TokenIdentity, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	for id, record := range f.tokens {
		if record.created.Token != token {
			continue
		}
		if record.revoked {
			return nil, auth.ErrUnauthorized
		}
		now := time.Now().UTC()
		record.lastUsed = &now
		f.tokens[id] = record
		return &auth.TokenIdentity{UserID: record.owner, Scopes: record.created.Scopes}, nil
	}
	return nil, auth.ErrUnauthorized
}

// normalizeFakeScopes mirrors the service validation for handler tests.
func normalizeFakeScopes(scopes []string) ([]string, error) {
	if len(scopes) == 0 {
		return []string{auth.ScopeRead}, nil
	}
	valid := map[string]struct{}{auth.ScopeRead: {}, auth.ScopeDeploy: {}, auth.ScopeAdmin: {}}

	seen := make(map[string]struct{}, len(scopes))
	normalized := make([]string, 0, len(scopes))
	for _, scope := range scopes {
		trimmed := strings.TrimSpace(scope)
		if _, ok := valid[trimmed]; !ok {
			return nil, fmt.Errorf("%w: unknown scope %q", auth.ErrValidation, scope)
		}
		if _, dup := seen[trimmed]; dup {
			continue
		}
		seen[trimmed] = struct{}{}
		normalized = append(normalized, trimmed)
	}
	return normalized, nil
}

// fakeRevokedAt returns a non-nil pointer once a token is revoked.
func fakeRevokedAt(record fakeTokenRecord) *time.Time {
	if !record.revoked {
		return nil
	}
	revoked := record.created.CreatedAt
	return &revoked
}

// newTestTokenServer builds a Server backed by the fake auth and token services.
func newTestTokenServer(t *testing.T) (*Server, *fakeTokenService) {
	t.Helper()

	cfg := &config.Config{
		Values: config.Values{Server: config.Server{Addr: "127.0.0.1", Port: 0}},
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	tokens := newFakeTokenService()

	s, err := New(cfg, logger, newFakeAuthService(), nil, tokens, nil, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(s.closer)

	s.db = stubPinger{}
	s.redis = stubPinger{}
	return s, tokens
}

// decodeCreatedToken decodes a created/rotated token body.
func decodeCreatedToken(t *testing.T, rec *httptest.ResponseRecorder) tokenResponse {
	t.Helper()

	var body tokenResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode token body %q: %v", rec.Body.String(), err)
	}
	return body
}

func TestTokensCRUDFlow(t *testing.T) {
	s, _ := newTestTokenServer(t)
	const bearer = "Bearer valid-token"

	created := doRequest(t, s, http.MethodPost, "/api/v1/tokens",
		`{"name":"ci","scopes":["read","deploy"]}`, bearer)
	if created.Code != http.StatusCreated {
		t.Fatalf("create status = %d, want 201 (body %s)", created.Code, created.Body.String())
	}
	body := decodeCreatedToken(t, created)
	if body.Name != "ci" {
		t.Errorf("created name = %q, want %q", body.Name, "ci")
	}
	if len(body.Scopes) != 2 {
		t.Errorf("created scopes = %v, want [read deploy]", body.Scopes)
	}
	if !strings.HasPrefix(body.Token, auth.APITokenPrefix) {
		t.Fatalf("created token %q missing prefix", body.Token)
	}
	if body.ID == "" || body.CreatedAt.IsZero() {
		t.Errorf("created body incomplete: %+v", body)
	}

	listed := doRequest(t, s, http.MethodGet, "/api/v1/tokens", "", bearer)
	if listed.Code != http.StatusOK {
		t.Fatalf("list status = %d, want 200 (body %s)", listed.Code, listed.Body.String())
	}
	var list tokenListResponse
	if err := json.Unmarshal(listed.Body.Bytes(), &list); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(list.Tokens) != 1 || list.Tokens[0].ID != body.ID {
		t.Fatalf("list = %+v, want the created token", list.Tokens)
	}
	if list.Tokens[0].LastUsedAt != nil {
		t.Errorf("list last_used_at = %v, want nil", list.Tokens[0].LastUsedAt)
	}

	// The API token authenticates the same protected route as a JWT.
	me := doRequest(t, s, http.MethodGet, "/api/v1/auth/me", "", "Bearer "+body.Token)
	if me.Code != http.StatusOK {
		t.Fatalf("me with api token status = %d, want 200 (body %s)", me.Code, me.Body.String())
	}
	var meBody meResponse
	if err := json.Unmarshal(me.Body.Bytes(), &meBody); err != nil {
		t.Fatalf("decode me: %v", err)
	}
	if meBody.User == nil || meBody.User.ID != testUserID.String() {
		t.Errorf("me user = %+v, want %s", meBody.User, testUserID)
	}

	rotated := doRequest(t, s, http.MethodPost, "/api/v1/tokens/"+body.ID+"/rotate", "", bearer)
	if rotated.Code != http.StatusOK {
		t.Fatalf("rotate status = %d, want 200 (body %s)", rotated.Code, rotated.Body.String())
	}
	rotatedBody := decodeCreatedToken(t, rotated)
	if rotatedBody.Token == body.Token {
		t.Error("rotate returned the same plaintext token")
	}
	if rotatedBody.Name != body.Name {
		t.Errorf("rotate name = %q, want %q", rotatedBody.Name, body.Name)
	}

	if old := doRequest(t, s, http.MethodGet, "/api/v1/auth/me", "", "Bearer "+body.Token); old.Code != http.StatusUnauthorized {
		t.Errorf("me with old token status = %d, want 401", old.Code)
	}
	if fresh := doRequest(t, s, http.MethodGet, "/api/v1/auth/me", "", "Bearer "+rotatedBody.Token); fresh.Code != http.StatusOK {
		t.Errorf("me with rotated token status = %d, want 200", fresh.Code)
	}

	revoked := doRequest(t, s, http.MethodDelete, "/api/v1/tokens/"+rotatedBody.ID, "", bearer)
	if revoked.Code != http.StatusNoContent {
		t.Fatalf("delete status = %d, want 204 (body %s)", revoked.Code, revoked.Body.String())
	}
	if again := doRequest(t, s, http.MethodDelete, "/api/v1/tokens/"+rotatedBody.ID, "", bearer); again.Code != http.StatusNotFound {
		t.Errorf("delete again status = %d, want 404", again.Code)
	}
	if gone := doRequest(t, s, http.MethodGet, "/api/v1/auth/me", "", "Bearer "+rotatedBody.Token); gone.Code != http.StatusUnauthorized {
		t.Errorf("me with revoked token status = %d, want 401", gone.Code)
	}
}

func TestTokensValidation(t *testing.T) {
	s, _ := newTestTokenServer(t)
	const bearer = "Bearer valid-token"

	tests := map[string]struct {
		body string
		want int
	}{
		"unknown scope": {body: `{"name":"ci","scopes":["bogus"]}`, want: http.StatusBadRequest},
		"empty name":    {body: `{"name":"","scopes":["read"]}`, want: http.StatusBadRequest},
		"blank name":    {body: `{"name":"   "}`, want: http.StatusBadRequest},
		"malformed":     {body: `{`, want: http.StatusBadRequest},
	}

	for label, tc := range tests {
		t.Run(label, func(t *testing.T) {
			rec := doRequest(t, s, http.MethodPost, "/api/v1/tokens", tc.body, bearer)
			if rec.Code != tc.want {
				t.Fatalf("status = %d, want %d (body %s)", rec.Code, tc.want, rec.Body.String())
			}
		})
	}

	unauthorized := doRequest(t, s, http.MethodPost, "/api/v1/tokens",
		`{"name":"ci"}`, "")
	if unauthorized.Code != http.StatusUnauthorized {
		t.Errorf("create without token status = %d, want 401", unauthorized.Code)
	}
}

func TestTokensRevokeNotOwned(t *testing.T) {
	s, tokens := newTestTokenServer(t)
	const bearer = "Bearer valid-token"

	foreign, err := tokens.Create(context.Background(), uuid.New(), "foreign", nil)
	if err != nil {
		t.Fatalf("seed foreign token: %v", err)
	}

	deleted := doRequest(t, s, http.MethodDelete, "/api/v1/tokens/"+foreign.ID.String(), "", bearer)
	if deleted.Code != http.StatusNotFound {
		t.Fatalf("delete foreign status = %d, want 404", deleted.Code)
	}

	rotated := doRequest(t, s, http.MethodPost, "/api/v1/tokens/"+foreign.ID.String()+"/rotate", "", bearer)
	if rotated.Code != http.StatusNotFound {
		t.Fatalf("rotate foreign status = %d, want 404", rotated.Code)
	}

	unknown := doRequest(t, s, http.MethodDelete, "/api/v1/tokens/"+uuid.New().String(), "", bearer)
	if unknown.Code != http.StatusNotFound {
		t.Errorf("delete unknown status = %d, want 404", unknown.Code)
	}

	malformed := doRequest(t, s, http.MethodDelete, "/api/v1/tokens/not-a-uuid", "", bearer)
	if malformed.Code != http.StatusBadRequest {
		t.Errorf("delete malformed id status = %d, want 400", malformed.Code)
	}
}

func TestRequireScopes(t *testing.T) {
	tests := map[string]struct {
		scopes   []string
		present  bool
		required []string
		want     int
	}{
		"api token has scope":          {scopes: []string{"read"}, present: true, required: []string{"read"}, want: http.StatusOK},
		"api token missing scope":      {scopes: []string{"read"}, present: true, required: []string{"deploy"}, want: http.StatusForbidden},
		"api token all scopes":         {scopes: []string{"read", "deploy"}, present: true, required: []string{"read", "deploy"}, want: http.StatusOK},
		"api token partial scopes":     {scopes: []string{"read", "deploy"}, present: true, required: []string{"read", "admin"}, want: http.StatusForbidden},
		"jwt has no scopes in phase 1": {present: false, required: []string{"deploy"}, want: http.StatusOK},
		"no requirement":               {present: true, required: nil, want: http.StatusOK},
	}

	for label, tc := range tests {
		t.Run(label, func(t *testing.T) {
			next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusOK)
			})
			handler := RequireScopes(tc.required...)(next)

			ctx := context.Background()
			if tc.present {
				ctx = context.WithValue(ctx, scopesKey, tc.scopes)
			}

			req := httptest.NewRequest(http.MethodGet, "/api/v1/tokens", nil).WithContext(ctx)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if rec.Code != tc.want {
				t.Fatalf("status = %d, want %d (body %s)", rec.Code, tc.want, rec.Body.String())
			}
		})
	}
}

// TestAdminScopeIssuanceIsPlatformGated is the fix-round-2 A regression: an
// admin-scoped API token unlocks the platform-global surface, so minting one
// must itself require platform-operator access. A plain session can still mint
// read/deploy tokens.
func TestAdminScopeIssuanceIsPlatformGated(t *testing.T) {
	s, _ := newTestTokenServer(t)
	const bearer = "Bearer valid-token"

	// A plain session cannot mint the admin scope...
	rec := doRequest(t, s, http.MethodPost, "/api/v1/tokens",
		`{"name":"sneaky","scopes":["admin"]}`, bearer)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("plain session admin token = %d, want 403 (body %s)", rec.Code, rec.Body.String())
	}
	// ...but may still mint a read/deploy token, including a mixed request
	// that asks for admin alongside them.
	if rec := doRequest(t, s, http.MethodPost, "/api/v1/tokens",
		`{"name":"ci","scopes":["read","deploy"]}`, bearer); rec.Code != http.StatusCreated {
		t.Fatalf("plain session read/deploy token = %d, want 201 (body %s)", rec.Code, rec.Body.String())
	}
	if rec := doRequest(t, s, http.MethodPost, "/api/v1/tokens",
		`{"name":"mixed","scopes":["read","admin"]}`, bearer); rec.Code != http.StatusForbidden {
		t.Fatalf("plain session mixed token = %d, want 403 (body %s)", rec.Code, rec.Body.String())
	}

	// An operator-listed session may mint it.
	t.Setenv(PlatformAdminsEnv, "user@example.com")
	operatorToken := doRequest(t, s, http.MethodPost, "/api/v1/tokens",
		`{"name":"operator","scopes":["admin"]}`, bearer)
	if operatorToken.Code != http.StatusCreated {
		t.Fatalf("operator session admin token = %d, want 201 (body %s)", operatorToken.Code, operatorToken.Body.String())
	}
}

// TestAdminScopeIssuanceCannotBePadded is the fix-round-3 regression: the
// token service trims scopes before persisting them, so the operator gate must
// compare the canonical (normalized) list. A padded or duplicated "admin" used
// to slip past the raw-string check and still be stored as admin.
func TestAdminScopeIssuanceCannotBePadded(t *testing.T) {
	s, tokens := newTestTokenServer(t)
	const bearer = "Bearer valid-token"

	padded := []string{
		`[" admin "]`,
		`["admin "]`,
		`["\tadmin"]`,
		`["admin\n"]`,
		`["admin","admin"]`,
		`["read"," admin "]`,
	}
	for _, scopes := range padded {
		rec := doRequest(t, s, http.MethodPost, "/api/v1/tokens",
			`{"name":"sneaky","scopes":`+scopes+`}`, bearer)
		if rec.Code != http.StatusForbidden {
			t.Errorf("plain session scopes %s = %d, want 403 (body %s)", scopes, rec.Code, rec.Body.String())
		}
	}

	// A read/deploy-scoped API token cannot mint an admin scope either.
	scoped, err := tokens.Create(context.Background(), testUserID, "ci", []string{auth.ScopeRead, auth.ScopeDeploy})
	if err != nil {
		t.Fatalf("seed scoped token: %v", err)
	}
	rec := doRequest(t, s, http.MethodPost, "/api/v1/tokens",
		`{"name":"sneaky","scopes":[" admin "]}`,
		"Bearer "+scoped.Token)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("scoped token padded admin request = %d, want 403 (body %s)", rec.Code, rec.Body.String())
	}

	// A read/deploy API token stays refused even when its owner's email is
	// allowlisted: an API token is a platform operator only by holding the
	// admin scope itself (the documented boundary).
	t.Setenv(PlatformAdminsEnv, "user@example.com")
	if rec := doRequest(t, s, http.MethodPost, "/api/v1/tokens",
		`{"name":"sneaky","scopes":[" admin "]}`,
		"Bearer "+scoped.Token); rec.Code != http.StatusForbidden {
		t.Fatalf("allowlisted owner's read token padded admin request = %d, want 403 (body %s)", rec.Code, rec.Body.String())
	}

	// An operator session passes, and the stored scopes are the canonical
	// ones: the response echoes the normalized list the token service
	// persisted.
	rec = doRequest(t, s, http.MethodPost, "/api/v1/tokens",
		`{"name":"operator","scopes":[" admin "]}`,
		bearer)
	if rec.Code != http.StatusCreated {
		t.Fatalf("scoped token padded admin request as operator = %d, want 201 (body %s)", rec.Code, rec.Body.String())
	}
	body := decodeCreatedToken(t, rec)
	if len(body.Scopes) != 1 || body.Scopes[0] != auth.ScopeAdmin {
		t.Fatalf("stored scopes = %v, want the normalized [admin]", body.Scopes)
	}
}
