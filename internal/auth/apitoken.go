package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/justindeelux/gotham/internal/store"
	"github.com/justindeelux/gotham/internal/store/sqlc"
)

// API token generation and validation policy.
const (
	// APITokenPrefix marks an opaque API token so the auth middleware can tell
	// it apart from a JWT without attempting verification.
	APITokenPrefix = "gt1_"
	apiTokenBytes  = 32
	apiTokenMaxLen = 64
)

// API token scopes. They map to roles in a later phase.
const (
	ScopeRead   = "read"
	ScopeDeploy = "deploy"
	ScopeAdmin  = "admin"
)

// validAPITokenScopes is the closed set of scope names a token may carry.
var validAPITokenScopes = map[string]struct{}{
	ScopeRead:   {},
	ScopeDeploy: {},
	ScopeAdmin:  {},
}

// CreatedToken is the result of creating or rotating an API token. Token holds
// the plaintext secret and is only ever returned once, at creation time.
type CreatedToken struct {
	ID        uuid.UUID
	Name      string
	Scopes    []string
	Token     string
	CreatedAt time.Time
}

// APIToken is API token metadata with the secret hash deliberately omitted.
type APIToken struct {
	ID         uuid.UUID
	Name       string
	Scopes     []string
	LastUsedAt *time.Time
	RevokedAt  *time.Time
	CreatedAt  time.Time
}

// TokenIdentity is the authenticated principal behind a valid API token.
type TokenIdentity struct {
	UserID uuid.UUID
	Scopes []string
}

// APITokenService manages scoped API tokens on top of the store. The clock is
// injectable so tests can exercise last-used timestamps without sleeping.
type APITokenService struct {
	store  *store.Store
	logger *slog.Logger
	now    func() time.Time
}

// NewAPITokenService builds an APITokenService.
func NewAPITokenService(st *store.Store, logger *slog.Logger) *APITokenService {
	if logger == nil {
		logger = slog.Default()
	}
	return &APITokenService{store: st, logger: logger, now: time.Now}
}

// Create validates name and scopes, generates a token, and stores only its
// hash. The plaintext is returned in CreatedToken exactly once.
func (s *APITokenService) Create(ctx context.Context, userID uuid.UUID, name string, scopes []string) (*CreatedToken, error) {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" || len(trimmed) > apiTokenMaxLen {
		return nil, fmt.Errorf("%w: name must be between 1 and %d characters", ErrValidation, apiTokenMaxLen)
	}

	normalized, err := NormalizeScopes(scopes)
	if err != nil {
		return nil, err
	}

	plaintext, hash, err := newAPIToken()
	if err != nil {
		return nil, err
	}

	row, err := s.store.CreateAPIToken(ctx, sqlc.CreateAPITokenParams{
		UserID: pgUUID(userID),
		Name:   trimmed,
		Hash:   hash,
		Scopes: normalized,
	})
	if err != nil {
		return nil, fmt.Errorf("auth: create api token: %w", err)
	}

	return &CreatedToken{
		ID:        uuid.UUID(row.ID.Bytes),
		Name:      row.Name,
		Scopes:    row.Scopes,
		Token:     plaintext,
		CreatedAt: row.CreatedAt.Time,
	}, nil
}

// List returns the metadata for every token owned by userID. The stored hash is
// never exposed.
func (s *APITokenService) List(ctx context.Context, userID uuid.UUID) ([]APIToken, error) {
	rows, err := s.store.ListAPITokensByUser(ctx, pgUUID(userID))
	if err != nil {
		return nil, fmt.Errorf("auth: list api tokens: %w", err)
	}

	tokens := make([]APIToken, 0, len(rows))
	for _, row := range rows {
		tokens = append(tokens, toAPIToken(row))
	}
	return tokens, nil
}

// Revoke marks an owned, active token as revoked. A token that does not exist,
// is not owned by userID, or is already revoked returns ErrNotFound.
func (s *APITokenService) Revoke(ctx context.Context, userID, id uuid.UUID) error {
	affected, err := s.store.RevokeAPIToken(ctx, sqlc.RevokeAPITokenParams{
		ID:     pgUUID(id),
		UserID: pgUUID(userID),
	})
	if err != nil {
		return fmt.Errorf("auth: revoke api token: %w", err)
	}
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}

// Rotate revokes an owned, active token and issues a replacement carrying the
// same name and scopes.
func (s *APITokenService) Rotate(ctx context.Context, userID, id uuid.UUID) (*CreatedToken, error) {
	current, err := s.store.GetAPITokenByIDAndUser(ctx, sqlc.GetAPITokenByIDAndUserParams{
		ID:     pgUUID(id),
		UserID: pgUUID(userID),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("auth: get api token: %w", err)
	}
	if current.RevokedAt.Valid {
		return nil, ErrNotFound
	}

	if err := s.Revoke(ctx, userID, id); err != nil {
		return nil, err
	}

	return s.Create(ctx, userID, current.Name, current.Scopes)
}

// Authenticate resolves a plaintext API token to its owner and scopes. An
// unknown or revoked token returns ErrUnauthorized.
func (s *APITokenService) Authenticate(ctx context.Context, token string) (*TokenIdentity, error) {
	if !strings.HasPrefix(token, APITokenPrefix) {
		return nil, ErrUnauthorized
	}

	row, err := s.store.GetAPITokenByHash(ctx, hashAPIToken(token))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrUnauthorized
		}
		return nil, fmt.Errorf("auth: get api token: %w", err)
	}
	if row.RevokedAt.Valid {
		return nil, ErrUnauthorized
	}

	// Recording the last use is best-effort: a failure must not reject a valid
	// request.
	if err := s.store.TouchAPITokenLastUsed(ctx, sqlc.TouchAPITokenLastUsedParams{
		ID:         row.ID,
		LastUsedAt: pgTimestamp(s.now()),
	}); err != nil {
		s.logger.Warn("auth: touch api token last_used_at",
			"error", err,
			"token_id", uuid.UUID(row.ID.Bytes).String(),
		)
	}

	return &TokenIdentity{
		UserID: uuid.UUID(row.UserID.Bytes),
		Scopes: row.Scopes,
	}, nil
}

// ScopesContain reports whether scopes holds every name in required. When no
// scope is required the check trivially passes.
func ScopesContain(scopes []string, required ...string) bool {
	if len(required) == 0 {
		return true
	}

	held := make(map[string]struct{}, len(scopes))
	for _, scope := range scopes {
		held[scope] = struct{}{}
	}
	for _, scope := range required {
		if _, ok := held[scope]; !ok {
			return false
		}
	}
	return true
}

// NormalizeScopes validates, trims and de-duplicates requested scopes,
// defaulting to read-only when none are supplied. The HTTP layer calls it
// before its admin-scope gate, so the list the gate authorizes and the list
// that is persisted are the same canonical value (a padded or duplicated
// "admin" can never slip past the gate and still be stored as admin).
func NormalizeScopes(scopes []string) ([]string, error) {
	if len(scopes) == 0 {
		return []string{ScopeRead}, nil
	}

	seen := make(map[string]struct{}, len(scopes))
	normalized := make([]string, 0, len(scopes))
	for _, scope := range scopes {
		trimmed := strings.TrimSpace(scope)
		if _, ok := validAPITokenScopes[trimmed]; !ok {
			return nil, fmt.Errorf("%w: unknown scope %q", ErrValidation, scope)
		}
		if _, dup := seen[trimmed]; dup {
			continue
		}
		seen[trimmed] = struct{}{}
		normalized = append(normalized, trimmed)
	}
	return normalized, nil
}

// newAPIToken generates an opaque API token and the SHA-256 hash persisted in
// api_tokens.hash.
func newAPIToken() (token string, hash string, err error) {
	buf := make([]byte, apiTokenBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", "", fmt.Errorf("auth: generate api token: %w", err)
	}
	token = APITokenPrefix + base64.RawURLEncoding.EncodeToString(buf)
	return token, hashAPIToken(token), nil
}

// hashAPIToken returns the hex-encoded SHA-256 of an API token.
func hashAPIToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// toAPIToken maps a stored row to the metadata representation.
func toAPIToken(row sqlc.ApiToken) APIToken {
	return APIToken{
		ID:         uuid.UUID(row.ID.Bytes),
		Name:       row.Name,
		Scopes:     row.Scopes,
		LastUsedAt: optionalTime(row.LastUsedAt),
		RevokedAt:  optionalTime(row.RevokedAt),
		CreatedAt:  row.CreatedAt.Time,
	}
}

// optionalTime converts a nullable timestamptz into a pointer, nil when unset.
func optionalTime(ts pgtype.Timestamptz) *time.Time {
	if !ts.Valid {
		return nil
	}
	value := ts.Time
	return &value
}
