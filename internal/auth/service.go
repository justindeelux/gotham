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
	"sync"
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
	// afterPasswordVerified is a test seam: Login runs it after verifying the
	// password and before re-reading the credential version, so a test can
	// commit a password reset into that window deterministically.
	afterPasswordVerified func()
	// beforeRotate is a test seam: Refresh runs it after the credential
	// checks and before the atomic rotation, so a test can commit a reset
	// plus a fresh login into that window deterministically.
	beforeRotate func()
	// verifyPassword is a seam over VerifyPassword so a test can observe that
	// the unknown-email and passwordless paths still run a real verification.
	verifyPassword func(encoded, password string) (bool, error)
}

// New builds a Service. The clock is injectable so tests can exercise expiry
// without sleeping.
func New(st *store.Store, signer *Signer, logger *slog.Logger) *Service {
	if logger == nil {
		logger = slog.Default()
	}
	// Warm the dummy hash once here so the first login miss does not pay an
	// extra argon2 derivation and reveal itself through timing.
	_ = dummyPasswordHash()
	return &Service{store: st, signer: signer, logger: logger, now: time.Now, verifyPassword: VerifyPassword}
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

	// Bootstrap vs invite. On an empty instance the account is created through
	// CreateFirstUser, whose emptiness check and insert share one transaction,
	// so two concurrent first registrations cannot both win (P-A2).
	count, err := s.store.CountUsers(ctx)
	if err != nil {
		return nil, fmt.Errorf("auth: count users: %w", err)
	}
	// An invite token is honoured whenever one is supplied, even when ordinary
	// registration is also open (test/dev override) or the instance is empty:
	// the invitee must still join the team, not just get a personal account.
	consumeInvite := inviteToken != ""
	if !consumeInvite && count > 0 && !s.AllowOpenRegistration {
		return nil, ErrRegistrationClosed
	}
	if consumeInvite {
		if err := checkInvite(ctx, normalized, inviteToken, invites); err != nil {
			return nil, err
		}
	}

	var user sqlc.User
	switch {
	case consumeInvite:
		user, err = s.store.CreateUser(ctx, normalized, &hash)
	case count == 0:
		// Bootstrap: the emptiness check and the insert share one transaction,
		// so only one of two concurrent first registrations can win.
		user, err = s.store.CreateFirstUser(ctx, normalized, &hash)
		if errors.Is(err, store.ErrInstanceHasAccount) {
			// Lost the race (or the table filled between the count and the
			// insert): the instance now has an account and an invite is needed.
			return nil, ErrRegistrationClosed
		}
	default:
		// Registration is forced open (test/dev override) on a populated
		// instance: a plain insert.
		user, err = s.store.CreateUser(ctx, normalized, &hash)
	}
	if err != nil {
		if isUniqueViolation(err) {
			return nil, ErrEmailTaken
		}
		return nil, fmt.Errorf("auth: create user: %w", err)
	}

	if consumeInvite {
		if err := invites.AcceptInvite(ctx, uuid.UUID(user.ID.Bytes), inviteToken); err != nil {
			// AcceptInvite reports failure only when nothing was committed
			// (teams.Accept never fails after a successful membership write),
			// so the fresh account and its personal team are rolled back with a
			// context that survives request cancellation.
			cleanup := context.WithoutCancel(ctx)
			if delErr := s.store.DeleteUserAndPersonalTeam(cleanup, user.ID); delErr != nil {
				s.logger.Error("auth: invite rollback failed", "error", delErr, "user_id", uuid.UUID(user.ID.Bytes))
			}
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
			// Burn a real argon2id verification so an unknown email costs the
			// same as a known one; otherwise response latency is an account
			// oracle (A2-5).
			s.spendPasswordCheck(password)
			return nil, ErrInvalidCredentials
		}
		return nil, fmt.Errorf("auth: get user: %w", err)
	}
	if user.PasswordHash == nil {
		// A passwordless (OAuth-only) account must be indistinguishable from a
		// wrong password, including its timing.
		s.spendPasswordCheck(password)
		return nil, ErrInvalidCredentials
	}

	ok, err := s.verifyPassword(*user.PasswordHash, password)
	if err != nil {
		s.logger.Error("auth: verify password", "error", err)
		return nil, ErrInvalidCredentials
	}
	if !ok {
		return nil, ErrInvalidCredentials
	}
	if s.afterPasswordVerified != nil {
		s.afterPasswordVerified()
	}

	// A password reset may have committed while the (deliberately slow) hash
	// verification ran: re-read the credential version and refuse when it
	// moved, so a login that verified the old password cannot mint a session
	// under the new credential (P-A5). Issue from the fresh read.
	current, err := s.store.GetUserByID(ctx, user.ID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrInvalidCredentials
		}
		return nil, fmt.Errorf("auth: re-read user: %w", err)
	}
	if current.CredentialVersion != user.CredentialVersion {
		return nil, ErrInvalidCredentials
	}

	return s.issue(ctx, current)
}

// dummyPasswordHash is a fixed argon2id hash with the production parameters,
// computed once (warmed by New). The login miss paths verify against it so they
// pay the same argon2id cost as a genuine check and cannot be timed to reveal
// whether an account exists (A2-5). A failure to hash is effectively impossible
// (crypto/rand); an empty value simply makes the dummy check return early
// rather than weakening the real paths.
var dummyPasswordHash = sync.OnceValue(func() string {
	hash, err := HashPassword("gotham-login-timing-placeholder")
	if err != nil {
		return ""
	}
	return hash
})

// spendPasswordCheck runs one argon2id verification against the dummy hash.
// The result is always false; only the work matters.
func (s *Service) spendPasswordCheck(password string) {
	if _, err := s.verifyPassword(dummyPasswordHash(), password); err != nil {
		s.logger.Error("auth: dummy password verification", "error", err)
	}
}

// Refresh rotates a refresh token: the presented session is revoked and a new
// session plus token pair is issued in one atomic store operation, so a failed
// replacement cannot consume the user's only token. Any unusable token returns
// ErrUnauthorized. Presenting a session that is still on record but already
// revoked (a rotated-away token) is treated as refresh-token theft: every live
// session for the account is revoked and the event is logged without token
// material. A merely expired token is not proof of theft and gets a plain 401.
// A session whose credential version is older than the account's current
// version (a password reset committed after it was minted) is refused as well,
// so the old chain cannot mint a replacement under the new credential.
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
	if session.RevokedAt.Valid {
		// A replayed revoked token. Classify and revoke atomically: only
		// genuine reuse ends the family; a stale post-reset chain is a plain
		// 401 so freshly authenticated sessions survive.
		s.maybeRevokeFamily(ctx, session.UserID, hash)
		return nil, ErrUnauthorized
	}
	if !session.ExpiresAt.Valid || s.now().After(session.ExpiresAt.Time) {
		// A merely expired token is not proof of theft: no family action.
		return nil, ErrUnauthorized
	}

	user, err := s.store.GetUserByID(ctx, session.UserID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrUnauthorized
		}
		return nil, fmt.Errorf("auth: get user: %w", err)
	}
	// A password reset bumps the account's credential version and deletes its
	// sessions in one transaction. When the reset commits between the session
	// read above and this user read, the delete finds the session already
	// revoked (or gone), so only the version comparison stops the old chain
	// from minting a replacement under the new credential (P-A5). Revoke the
	// raced row so it cannot sit live until the sweep; a store failure is not
	// fatal, as the version check refuses it on every retry anyway.
	if session.CredentialVersion < user.CredentialVersion {
		if err := s.store.RevokeSession(ctx, hash); err != nil {
			s.logger.Error("auth: revoke stale session", "error", err, "user_id", uuid.UUID(user.ID.Bytes))
		}
		return nil, ErrUnauthorized
	}

	// Sign before rotating: a signer failure must not consume the session.
	accessToken, err := s.signAccessToken(user)
	if err != nil {
		return nil, err
	}

	refreshTokenNext, refreshHashNext, err := newRefreshToken()
	if err != nil {
		return nil, err
	}
	if s.beforeRotate != nil {
		s.beforeRotate()
	}
	if _, err := s.store.RotateSession(ctx, session.UserID, sqlc.RotateSessionParams{
		RevokedRefreshHash: hash,
		NewRefreshHash:     refreshHashNext,
		ExpiresAt:          pgTimestamp(s.now().Add(refreshTokenTTL)),
		CredentialVersion:  user.CredentialVersion,
	}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// The row stopped being live between the read and the rotation.
			// Classify it atomically: a rotation race (row revoked, current
			// version) is reuse, while a reset purge (row deleted) or a stale
			// post-reset chain is a plain 401 that leaves fresh sessions be.
			s.maybeRevokeFamily(ctx, session.UserID, hash)
			return nil, ErrUnauthorized
		}
		return nil, fmt.Errorf("auth: rotate session: %w", err)
	}

	return &AuthResult{
		User:         toUser(user),
		AccessToken:  accessToken,
		ExpiresIn:    int64(accessTokenTTL / time.Second),
		RefreshToken: refreshTokenNext,
	}, nil
}

// maybeRevokeFamily classifies a replayed revoked token under the per-user
// session lock and revokes the family only for genuine reuse (the row still
// exists, is revoked, and carries the account's current credential version).
// The classification and the revocation are one store transaction, so a
// password reset cannot interleave. A store failure is logged and swallowed
// because the caller is answering 401 either way.
func (s *Service) maybeRevokeFamily(ctx context.Context, userID pgtype.UUID, refreshHash string) {
	stolen, err := s.store.RevokeFamilyIfStolen(ctx, userID, refreshHash)
	if err != nil {
		s.logger.Error("auth: reuse revocation failed", "error", err, "user_id", uuid.UUID(userID.Bytes))
		return
	}
	if stolen {
		s.logger.Warn("auth: refresh token reuse detected; revoked all live sessions", "user_id", uuid.UUID(userID.Bytes))
	}
}

// Logout deletes the live session behind refreshToken. Deleting rather than
// revoking keeps a replayed logged-out token a plain 401: the reuse classifier
// only attributes theft to a still-existing revoked row, so an old token from a
// logged-out browser cannot revoke the account's other live sessions. A token
// that was already rotated away is left revoked, preserving its reuse evidence.
// It is idempotent: an unknown or already-revoked token deletes nothing and is
// not an error.
func (s *Service) Logout(ctx context.Context, refreshToken string) error {
	if refreshToken == "" {
		return nil
	}
	return s.store.DeleteSession(ctx, hashRefreshToken(refreshToken))
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

// issue creates a refresh session and signs an access token for user. The
// session records the account's credential version, so a later password reset
// invalidates it.
func (s *Service) issue(ctx context.Context, user sqlc.User) (*AuthResult, error) {
	refreshToken, refreshHash, err := newRefreshToken()
	if err != nil {
		return nil, err
	}

	_, err = s.store.CreateSession(ctx, sqlc.CreateSessionParams{
		UserID:            user.ID,
		RefreshHash:       refreshHash,
		ExpiresAt:         pgTimestamp(s.now().Add(refreshTokenTTL)),
		CredentialVersion: user.CredentialVersion,
	})
	if err != nil {
		return nil, fmt.Errorf("auth: create session: %w", err)
	}

	accessToken, err := s.signAccessToken(user)
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

// signAccessToken mints the short-lived JWT for user.
func (s *Service) signAccessToken(user sqlc.User) (string, error) {
	accessToken, _, err := s.signer.IssueAccessToken(uuid.UUID(user.ID.Bytes), defaultRole)
	return accessToken, err
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
