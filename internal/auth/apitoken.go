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

const (
	APITokenPrefix = "gt1_"
	apiTokenBytes  = 32
	apiTokenMaxLen = 64
)

const (
	ScopeRead   = "read"
	ScopeDeploy = "deploy"
	ScopeAdmin  = "admin"
)

var validAPITokenScopes = map[string]struct{}{
	ScopeRead:   {},
	ScopeDeploy: {},
	ScopeAdmin:  {},
}

type CreatedToken struct {
	ID        uuid.UUID
	Name      string
	Scopes    []string
	Token     string
	CreatedAt time.Time
}

type APIToken struct {
	ID         uuid.UUID
	Name       string
	Scopes     []string
	LastUsedAt *time.Time
	RevokedAt  *time.Time
	CreatedAt  time.Time
}

type TokenIdentity struct {
	UserID uuid.UUID
	Scopes []string
}

type APITokenService struct {
	store  *store.Store
	logger *slog.Logger
	now    func() time.Time
}

func NewAPITokenService(st *store.Store, logger *slog.Logger) *APITokenService {
	if logger == nil {
		logger = slog.Default()
	}
	return &APITokenService{store: st, logger: logger, now: time.Now}
}

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

func (s *APITokenService) Get(ctx context.Context, userID, id uuid.UUID) (*APIToken, error) {
	row, err := s.store.GetAPITokenByIDAndUser(ctx, sqlc.GetAPITokenByIDAndUserParams{
		ID:     pgUUID(id),
		UserID: pgUUID(userID),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("auth: get api token: %w", err)
	}
	if row.RevokedAt.Valid {
		return nil, ErrNotFound
	}
	token := toAPIToken(row)
	return &token, nil
}

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

var scopeGrantRank = map[string]int{
	ScopeRead:   1,
	ScopeDeploy: 2,
	ScopeAdmin:  3,
}

func ScopesAuthorize(scopes []string, required ...string) bool {
	if len(required) == 0 {
		return true
	}
	maxHeld := 0
	for _, scope := range scopes {
		if rank, ok := scopeGrantRank[scope]; ok && rank > maxHeld {
			maxHeld = rank
		}
	}
	for _, scope := range required {
		rank, ok := scopeGrantRank[scope]
		if !ok || maxHeld < rank {
			return false
		}
	}
	return true
}

func CanGrantScopes(held, requested []string) bool {
	return ScopesAuthorize(held, requested...)
}

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

func newAPIToken() (token string, hash string, err error) {
	buf := make([]byte, apiTokenBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", "", fmt.Errorf("auth: generate api token: %w", err)
	}
	token = APITokenPrefix + base64.RawURLEncoding.EncodeToString(buf)
	return token, hashAPIToken(token), nil
}

func hashAPIToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

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

func optionalTime(ts pgtype.Timestamptz) *time.Time {
	if !ts.Valid {
		return nil
	}
	value := ts.Time
	return &value
}
