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
	// AllowOpenRegistration reopens self-registration after the first account.
	// Test/dev only (GOTHAM_AUTH_ALLOW_REGISTRATION): production relies on the
	// closed default, where members join through admin invites (P-A2).
	AllowOpenRegistration bool
}

// New builds a Service. The clock is injectable so tests can exercise expiry
// without sleeping.
func New(st *store.Store, signer *Signer, logger *slog.Logger) *Service {
	if logger == nil {
		logger = slog.Default()
	}
	return &Service{store: st, signer: signer, logger: logger, now: time.Now}
}

// InviteAcceptor is the teams-domain slice the invite-registration path
// (P-A2) needs from the caller: peek a pending invite before the account
// exists, then consume it for the fresh account. auth never imports the teams
// package; the HTTP wiring passes the teams service at call time.
type InviteAcceptor interface {
	// PeekInvite returns the invited team name and target email behind a
	// pending invite token, or an error when the token is unknown, expired,
	// or already consumed.
	PeekInvite(ctx context.Context, token string) (teamName, email string, err error)
	// AcceptInvite consumes a pending invite for the freshly created account.
	AcceptInvite(ctx context.Context, userID uuid.UUID, token string) error
}

// Register creates a new account and returns an authenticated token pair.
//
// Registration is open only while the instance has no account (the bootstrap
// of exactly one admin account, P-A2). Afterwards a valid, pending, unused
// invite token issued to the same email is required; any token problem
// answers ErrRegistrationClosed so token validity is never publicly
// distinguishable from a closed instance.
func (s *Service) Register(ctx context.Context, email, password, inviteToken string, invites InviteAcceptor) (*AuthResult, error) {
	normalized, err := NormalizeEmail(email)
	if err != nil {
		return nil, err
	}
	if err := ValidatePassword(password); err != nil {
		return nil, err
	}

	hash, err := HashPassword(password)
	if err != nil {
		return nil, err
	}

	count, err := s.store.CountUsers(ctx)
	if err != nil {
		return nil, fmt.Errorf("auth: count users: %w", err)
	}
	needInvite := count > 0 && !s.AllowOpenRegistration
	if needInvite {
		if err := checkInvite(ctx, normalized, inviteToken, invites); err != nil {
			return nil, err
		}
	}

	user, err := s.store.CreateUser(ctx, normalized, &hash)
	if err != nil {
		if isUniqueViolation(err) {
			return nil, ErrEmailTaken
		}
		return nil, fmt.Errorf("auth: create user: %w", err)
	}

	if needInvite {
		if err := invites.AcceptInvite(ctx, uuid.UUID(user.ID.Bytes), inviteToken); err != nil {
			// The invite stopped being valid between the peek and the
			// accept; roll the fresh account back so a retry starts clean.
			_, _ = s.store.DB.Exec(ctx, "DELETE FROM users WHERE id = $1", user.ID)
			s.logger.Warn("auth: invite accept failed at registration", "error", err)
			return nil, ErrRegistrationClosed
		}
	}

	return s.issue(ctx, user)
}

// checkInvite reports whether the pending invite token admits email to
// register. Every token problem maps to ErrRegistrationClosed.
func checkInvite(ctx context.Context, email, token string, invites InviteAcceptor) error {
	if token == "" || invites == nil {
		return ErrRegistrationClosed
	}
	_, inviteEmail, err := invites.PeekInvite(ctx, token)
	if err != nil {
		return ErrRegistrationClosed
	}
	if !strings.EqualFold(inviteEmail, email) {
		return ErrRegistrationClosed
	}
	return nil
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

// IssueSession creates a refresh session and signs an access token for user. It
// exposes the internal issue path so alternative flows (for example OAuth2
// logins) mint sessions exactly like a password login, without duplicating the
// token logic.
func (s *Service) IssueSession(ctx context.Context, user sqlc.User) (*AuthResult, error) {
	return s.issue(ctx, user)
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

// NormalizeEmail trims, lowercases, and syntactically validates an email. It
// is shared by registration and the admin CLI.
func NormalizeEmail(email string) (string, error) {
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

// ValidatePassword enforces the password length bounds. It is shared by
// registration and the admin CLI (P-A3).
func ValidatePassword(password string) error {
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
