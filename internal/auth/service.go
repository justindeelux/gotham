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
	"net/mail"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/justindeelux/gotham/internal/store"
	"github.com/justindeelux/gotham/internal/store/sqlc"
)

// Session and password policy.
const (
	refreshTokenBytes = 32
	refreshTokenTTL   = 30 * 24 * time.Hour
	minPasswordLength = 8
	maxPasswordLength = 128
	defaultRole       = "user"
)

// User is the public representation of an account, safe to serialise to JSON.
type User struct {
	ID        string    `json:"id"`
	Email     string    `json:"email"`
	Avatar    *string   `json:"avatar,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

// AuthResult bundles the account and the freshly issued token pair.
type AuthResult struct {
	User         *User  `json:"user"`
	AccessToken  string `json:"access_token"`
	ExpiresIn    int64  `json:"expires_in"`
	RefreshToken string `json:"refresh_token"`
}

// Service implements the core authentication flows on top of the store and the
// JWT signer.
type Service struct {
	store  *store.Store
	signer *Signer
	logger *slog.Logger
	now    func() time.Time
}

// New builds a Service. The clock is injectable so tests can exercise expiry
// without sleeping.
func New(st *store.Store, signer *Signer, logger *slog.Logger) *Service {
	if logger == nil {
		logger = slog.Default()
	}
	return &Service{store: st, signer: signer, logger: logger, now: time.Now}
}

// Register creates a new account and returns an authenticated token pair.
func (s *Service) Register(ctx context.Context, email, password string) (*AuthResult, error) {
	normalized, err := normalizeEmail(email)
	if err != nil {
		return nil, err
	}
	if err := validatePassword(password); err != nil {
		return nil, err
	}

	hash, err := HashPassword(password)
	if err != nil {
		return nil, err
	}

	user, err := s.store.CreateUser(ctx, normalized, &hash)
	if err != nil {
		if isUniqueViolation(err) {
			return nil, ErrEmailTaken
		}
		return nil, fmt.Errorf("auth: create user: %w", err)
	}

	return s.issue(ctx, user)
}

// Login verifies credentials and returns an authenticated token pair. Unknown
// emails and wrong passwords return the same ErrInvalidCredentials.
func (s *Service) Login(ctx context.Context, email, password string) (*AuthResult, error) {
	normalized := strings.ToLower(strings.TrimSpace(email))

	user, err := s.store.GetUserByEmail(ctx, normalized)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrInvalidCredentials
		}
		return nil, fmt.Errorf("auth: get user: %w", err)
	}
	if user.PasswordHash == nil {
		return nil, ErrInvalidCredentials
	}

	ok, err := VerifyPassword(*user.PasswordHash, password)
	if err != nil {
		s.logger.Error("auth: verify password", "error", err)
		return nil, ErrInvalidCredentials
	}
	if !ok {
		return nil, ErrInvalidCredentials
	}

	return s.issue(ctx, user)
}

// Refresh rotates a refresh token: the presented session is revoked and a new
// session plus token pair is issued. Any unusable token returns ErrUnauthorized.
func (s *Service) Refresh(ctx context.Context, refreshToken string) (*AuthResult, error) {
	if refreshToken == "" {
		return nil, ErrUnauthorized
	}

	hash := hashRefreshToken(refreshToken)
	session, err := s.store.GetSessionByRefreshHash(ctx, hash)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrUnauthorized
		}
		return nil, fmt.Errorf("auth: get session: %w", err)
	}
	if session.RevokedAt.Valid || !session.ExpiresAt.Valid || s.now().After(session.ExpiresAt.Time) {
		return nil, ErrUnauthorized
	}

	if err := s.store.RevokeSession(ctx, hash); err != nil {
		return nil, fmt.Errorf("auth: revoke session: %w", err)
	}

	user, err := s.store.GetUserByID(ctx, session.UserID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrUnauthorized
		}
		return nil, fmt.Errorf("auth: get user: %w", err)
	}

	return s.issue(ctx, user)
}

// Logout revokes the session behind refreshToken. It is idempotent: an unknown
// or already-revoked token is not an error.
func (s *Service) Logout(ctx context.Context, refreshToken string) error {
	if refreshToken == "" {
		return nil
	}
	return s.store.RevokeSession(ctx, hashRefreshToken(refreshToken))
}

// Me returns the public account for userID.
func (s *Service) Me(ctx context.Context, userID uuid.UUID) (*User, error) {
	user, err := s.store.GetUserByID(ctx, pgUUID(userID))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrUnauthorized
		}
		return nil, fmt.Errorf("auth: get user: %w", err)
	}
	return toUser(user), nil
}

// VerifyAccessToken delegates to the signer so the HTTP layer can validate
// bearer tokens without depending on the signer directly.
func (s *Service) VerifyAccessToken(token string) (*Claims, error) {
	return s.signer.VerifyAccessToken(token)
}

// issue creates a refresh session and signs an access token for user.
func (s *Service) issue(ctx context.Context, user sqlc.User) (*AuthResult, error) {
	refreshToken, refreshHash, err := newRefreshToken()
	if err != nil {
		return nil, err
	}

	_, err = s.store.CreateSession(ctx, sqlc.CreateSessionParams{
		UserID:      user.ID,
		RefreshHash: refreshHash,
		ExpiresAt:   pgTimestamp(s.now().Add(refreshTokenTTL)),
	})
	if err != nil {
		return nil, fmt.Errorf("auth: create session: %w", err)
	}

	accessToken, _, err := s.signer.IssueAccessToken(uuid.UUID(user.ID.Bytes), defaultRole)
	if err != nil {
		return nil, err
	}

	return &AuthResult{
		User:         toUser(user),
		AccessToken:  accessToken,
		ExpiresIn:    int64(accessTokenTTL / time.Second),
		RefreshToken: refreshToken,
	}, nil
}

// newRefreshToken generates an opaque refresh token and the SHA-256 hash that is
// persisted in sessions.refresh_hash.
func newRefreshToken() (token string, hash string, err error) {
	buf := make([]byte, refreshTokenBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", "", fmt.Errorf("auth: generate refresh token: %w", err)
	}
	token = base64.RawURLEncoding.EncodeToString(buf)
	return token, hashRefreshToken(token), nil
}

// hashRefreshToken returns the hex-encoded SHA-256 of a refresh token.
func hashRefreshToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// normalizeEmail trims, lowercases, and syntactically validates an email.
func normalizeEmail(email string) (string, error) {
	normalized := strings.ToLower(strings.TrimSpace(email))
	if normalized == "" {
		return "", fmt.Errorf("%w: email is required", ErrValidation)
	}

	addr, err := mail.ParseAddress(normalized)
	if err != nil || addr.Address != normalized {
		return "", fmt.Errorf("%w: invalid email address", ErrValidation)
	}
	return normalized, nil
}

// validatePassword enforces the password length bounds.
func validatePassword(password string) error {
	if len(password) < minPasswordLength || len(password) > maxPasswordLength {
		return fmt.Errorf("%w: password must be between %d and %d characters",
			ErrValidation, minPasswordLength, maxPasswordLength)
	}
	return nil
}

// isUniqueViolation reports whether err is a PostgreSQL unique-constraint
// violation (SQLSTATE 23505).
func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

// toUser converts a stored row into the public representation.
func toUser(user sqlc.User) *User {
	return &User{
		ID:        uuid.UUID(user.ID.Bytes).String(),
		Email:     user.Email,
		Avatar:    user.Avatar,
		CreatedAt: user.CreatedAt.Time,
	}
}

// pgUUID converts a uuid.UUID into its pgtype form.
func pgUUID(id uuid.UUID) pgtype.UUID {
	return pgtype.UUID{Bytes: id, Valid: true}
}

// pgTimestamp converts a time.Time into its pgtype form.
func pgTimestamp(t time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: t, Valid: true}
}
