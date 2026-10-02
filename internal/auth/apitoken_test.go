package auth

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"io"
	"log/slog"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/store"
)

// newTestAPITokenService migrates the dev database and returns a token service
// wired to it, alongside the store for fixtures.
func newTestAPITokenService(t *testing.T) (*APITokenService, *store.Store) {
	t.Helper()

	_, st := newTestService(t)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return NewAPITokenService(st, logger), st
}

// createTestUser inserts a passwordless account and returns its ID. The account
// is removed afterwards, cascading to its tokens.
func createTestUser(t *testing.T, st *store.Store) uuid.UUID {
	t.Helper()

	ctx := context.Background()
	email := uniqueEmail("api-token")
	cleanupUser(t, st, email)

	user, err := st.CreateUser(ctx, email, nil)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	return uuid.UUID(user.ID.Bytes)
}

func TestAPITokenLifecycle(t *testing.T) {
	svc, st := newTestAPITokenService(t)
	ctx := context.Background()
	userID := createTestUser(t, st)

	created, err := svc.Create(ctx, userID, "  CI deploy  ", []string{"deploy", "read", "deploy"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.Name != "CI deploy" {
		t.Errorf("Name = %q, want trimmed %q", created.Name, "CI deploy")
	}
	if want := []string{"deploy", "read"}; !reflect.DeepEqual(created.Scopes, want) {
		t.Errorf("Scopes = %v, want %v", created.Scopes, want)
	}
	if created.Token == "" {
		t.Fatal("Create returned an empty token")
	}

	list, err := svc.List(ctx, userID)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("List returned %d tokens, want 1", len(list))
	}
	if list[0].ID != created.ID {
		t.Errorf("List ID = %v, want %v", list[0].ID, created.ID)
	}
	if list[0].LastUsedAt != nil {
		t.Errorf("LastUsedAt = %v, want nil before first use", list[0].LastUsedAt)
	}
	if list[0].RevokedAt != nil {
		t.Errorf("RevokedAt = %v, want nil", list[0].RevokedAt)
	}

	identity, err := svc.Authenticate(ctx, created.Token)
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	if identity.UserID != userID {
		t.Errorf("identity UserID = %v, want %v", identity.UserID, userID)
	}
	if !ScopesContain(identity.Scopes, "deploy", "read") {
		t.Errorf("identity Scopes = %v, want deploy+read", identity.Scopes)
	}

	relisted, err := svc.List(ctx, userID)
	if err != nil {
		t.Fatalf("List after authenticate: %v", err)
	}
	if relisted[0].LastUsedAt == nil {
		t.Fatal("LastUsedAt = nil, want set after Authenticate")
	}
	if relisted[0].LastUsedAt.Before(created.CreatedAt.Add(-time.Minute)) {
		t.Errorf("LastUsedAt = %v, want at or after creation %v", relisted[0].LastUsedAt, created.CreatedAt)
	}

	if err := svc.Revoke(ctx, userID, created.ID); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	if _, err := svc.Authenticate(ctx, created.Token); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("Authenticate(revoked) error = %v, want ErrUnauthorized", err)
	}
	if err := svc.Revoke(ctx, userID, created.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Revoke(already revoked) error = %v, want ErrNotFound", err)
	}

	revoked, err := svc.List(ctx, userID)
	if err != nil {
		t.Fatalf("List after revoke: %v", err)
	}
	if revoked[0].RevokedAt == nil {
		t.Error("RevokedAt = nil, want set after Revoke")
	}
}

func TestAPITokenRotate(t *testing.T) {
	svc, st := newTestAPITokenService(t)
	ctx := context.Background()
	userID := createTestUser(t, st)

	first, err := svc.Create(ctx, userID, "rotate-me", []string{"read"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	rotated, err := svc.Rotate(ctx, userID, first.ID)
	if err != nil {
		t.Fatalf("Rotate: %v", err)
	}
	if rotated.ID == first.ID {
		t.Error("Rotate reused the old token ID, want a new token")
	}
	if rotated.Token == first.Token {
		t.Error("Rotate reused the old plaintext, want a new token")
	}
	if rotated.Name != "rotate-me" {
		t.Errorf("rotated Name = %q, want %q", rotated.Name, "rotate-me")
	}
	if want := []string{"read"}; !reflect.DeepEqual(rotated.Scopes, want) {
		t.Errorf("rotated Scopes = %v, want %v", rotated.Scopes, want)
	}

	if _, err := svc.Authenticate(ctx, first.Token); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("Authenticate(old) error = %v, want ErrUnauthorized", err)
	}
	if _, err := svc.Authenticate(ctx, rotated.Token); err != nil {
		t.Fatalf("Authenticate(new): %v", err)
	}

	if _, err := svc.Rotate(ctx, userID, first.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Rotate(revoked) error = %v, want ErrNotFound", err)
	}
}

func TestAPITokenValidation(t *testing.T) {
	svc, st := newTestAPITokenService(t)
	ctx := context.Background()
	userID := createTestUser(t, st)

	tests := map[string]struct {
		name   string
		scopes []string
		want   error
	}{
		"empty name":     {name: "", want: ErrValidation},
		"blank name":     {name: "   ", want: ErrValidation},
		"name too long":  {name: strings.Repeat("a", apiTokenMaxLen+1), want: ErrValidation},
		"unknown scope":  {name: "ci", scopes: []string{"bogus"}, want: ErrValidation},
		"mixed scopes":   {name: "ci", scopes: []string{"read", "bogus"}, want: ErrValidation},
		"default scopes": {name: "ci", scopes: nil, want: nil},
		"all scopes":     {name: "ci", scopes: []string{"read", "deploy", "admin"}, want: nil},
	}

	for label, tc := range tests {
		t.Run(label, func(t *testing.T) {
			created, err := svc.Create(ctx, userID, tc.name, tc.scopes)
			if tc.want != nil {
				if !errors.Is(err, tc.want) {
					t.Fatalf("Create error = %v, want %v", err, tc.want)
				}
				return
			}
			if err != nil {
				t.Fatalf("Create: %v", err)
			}
			if len(created.Scopes) == 0 {
				t.Fatal("Create returned no scopes")
			}
		})
	}

	// A nil scope list defaults to read-only.
	created, err := svc.Create(ctx, userID, "default", nil)
	if err != nil {
		t.Fatalf("Create(no scopes): %v", err)
	}
	if want := []string{ScopeRead}; !reflect.DeepEqual(created.Scopes, want) {
		t.Errorf("default Scopes = %v, want %v", created.Scopes, want)
	}
}

func TestAPITokenRevokeNotOwned(t *testing.T) {
	svc, st := newTestAPITokenService(t)
	ctx := context.Background()
	owner := createTestUser(t, st)
	other := createTestUser(t, st)

	created, err := svc.Create(ctx, owner, "owner-only", nil)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err := svc.Revoke(ctx, other, created.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Revoke(other user) error = %v, want ErrNotFound", err)
	}
	if _, err := svc.Authenticate(ctx, created.Token); err != nil {
		t.Fatalf("Authenticate after foreign revoke: %v", err)
	}

	otherList, err := svc.List(ctx, other)
	if err != nil {
		t.Fatalf("List(other): %v", err)
	}
	if len(otherList) != 0 {
		t.Errorf("other user sees %d tokens, want 0", len(otherList))
	}

	if err := svc.Revoke(ctx, owner, created.ID); err != nil {
		t.Fatalf("Revoke(owner): %v", err)
	}
}

func TestAPITokenFormat(t *testing.T) {
	svc, st := newTestAPITokenService(t)
	ctx := context.Background()
	userID := createTestUser(t, st)

	created, err := svc.Create(ctx, userID, "format", nil)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if !strings.HasPrefix(created.Token, APITokenPrefix) {
		t.Fatalf("Token %q missing prefix %q", created.Token, APITokenPrefix)
	}

	encoded := strings.TrimPrefix(created.Token, APITokenPrefix)
	raw, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatalf("decode token payload: %v", err)
	}
	if len(raw) != apiTokenBytes {
		t.Errorf("token payload = %d bytes, want %d", len(raw), apiTokenBytes)
	}

	// Only the hash is stored, and it is the SHA-256 hex of the plaintext.
	wantHash := hex.EncodeToString(func() []byte {
		sum := sha256.Sum256([]byte(created.Token))
		return sum[:]
	}())
	if wantHash == created.Token {
		t.Fatal("stored hash equals plaintext token")
	}

	row, err := st.GetAPITokenByHash(ctx, wantHash)
	if err != nil {
		t.Fatalf("GetAPITokenByHash: %v", err)
	}
	if row.Hash != wantHash {
		t.Errorf("stored hash = %q, want %q", row.Hash, wantHash)
	}
	if row.Hash == created.Token {
		t.Error("stored hash equals plaintext token")
	}
}

func TestAPITokenAuthenticateRejectsBadTokens(t *testing.T) {
	svc, _ := newTestAPITokenService(t)
	ctx := context.Background()

	// Wrong prefix is not an API token.
	if _, err := svc.Authenticate(ctx, "not-an-api-token"); !errors.Is(err, ErrUnauthorized) {
		t.Errorf("Authenticate(wrong prefix) error = %v, want ErrUnauthorized", err)
	}
	// Correct prefix but unknown hash.
	unknown := APITokenPrefix + strings.Repeat("x", 43)
	if _, err := svc.Authenticate(ctx, unknown); !errors.Is(err, ErrUnauthorized) {
		t.Errorf("Authenticate(unknown) error = %v, want ErrUnauthorized", err)
	}
	// Empty token.
	if _, err := svc.Authenticate(ctx, ""); !errors.Is(err, ErrUnauthorized) {
		t.Errorf("Authenticate(empty) error = %v, want ErrUnauthorized", err)
	}
}

func TestScopesContain(t *testing.T) {
	tests := map[string]struct {
		scopes   []string
		required []string
		want     bool
	}{
		"no requirement":     {scopes: nil, required: nil, want: true},
		"empty scopes bare":  {scopes: nil, required: []string{}, want: true},
		"single present":     {scopes: []string{"read"}, required: []string{"read"}, want: true},
		"single missing":     {scopes: []string{"read"}, required: []string{"deploy"}, want: false},
		"all present":        {scopes: []string{"read", "deploy"}, required: []string{"deploy", "read"}, want: true},
		"one of two missing": {scopes: []string{"read", "deploy"}, required: []string{"read", "admin"}, want: false},
		"empty held":         {scopes: nil, required: []string{"read"}, want: false},
		"admin implies not":  {scopes: []string{"admin"}, required: []string{"read"}, want: false},
	}

	for label, tc := range tests {
		t.Run(label, func(t *testing.T) {
			if got := ScopesContain(tc.scopes, tc.required...); got != tc.want {
				t.Errorf("ScopesContain(%v, %v) = %v, want %v", tc.scopes, tc.required, got, tc.want)
			}
		})
	}
}

// TestCanGrantScopes pins the token-minting subset rule: a token may only grant
// scopes it holds, with a more privileged scope covering the ones below it
// (deploy grants read; only admin grants admin).
func TestCanGrantScopes(t *testing.T) {
	tests := map[string]struct {
		held      []string
		requested []string
		want      bool
	}{
		"read grants read":          {held: []string{"read"}, requested: []string{"read"}, want: true},
		"read cannot grant deploy":  {held: []string{"read"}, requested: []string{"deploy"}, want: false},
		"read cannot grant admin":   {held: []string{"read"}, requested: []string{"admin"}, want: false},
		"deploy grants deploy":      {held: []string{"deploy"}, requested: []string{"deploy"}, want: true},
		"deploy grants read":        {held: []string{"deploy"}, requested: []string{"read"}, want: true},
		"deploy cannot grant admin": {held: []string{"deploy"}, requested: []string{"admin"}, want: false},
		"admin grants admin":        {held: []string{"admin"}, requested: []string{"admin"}, want: true},
		"admin grants read":         {held: []string{"admin"}, requested: []string{"read"}, want: true},
		"empty request always ok":   {held: []string{"read"}, requested: nil, want: true},
		"mixed partial":             {held: []string{"read", "deploy"}, requested: []string{"read", "admin"}, want: false},
	}

	for label, tc := range tests {
		t.Run(label, func(t *testing.T) {
			if got := CanGrantScopes(tc.held, tc.requested); got != tc.want {
				t.Errorf("CanGrantScopes(%v, %v) = %v, want %v", tc.held, tc.requested, got, tc.want)
			}
		})
	}
}
