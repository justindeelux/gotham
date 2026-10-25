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

const (
	refreshTokenBytes = 32
	refreshTokenTTL   = 30 * 24 * time.Hour
	minPasswordLength = 8
	maxPasswordLength = 128
)

// Account roles. The owner operates the instance; every other account is a
// member.
const (
	RoleOwner  = "owner"
	RoleMember = "member"
)

type User struct {
	ID          string    `json:"id"`
	Email       string    `json:"email"`
	Avatar      *string   `json:"avatar,omitempty"`
	Role        string    `json:"role"`
	CreatedAt   time.Time `json:"created_at"`
	DisplayName *string   `json:"display_name,omitempty"`
	HasPassword bool      `json:"has_password"`
}

type AuthResult struct {
	User         *User  `json:"user"`
	AccessToken  string `json:"access_token"`
	ExpiresIn    int64  `json:"expires_in"`
	RefreshToken string `json:"refresh_token"`
	MFARequired  bool   `json:"mfa_required,omitempty"`
	MFAToken     string `json:"mfa_token,omitempty"`
}

type Service struct {
	store                 *store.Store
	signer                *Signer
	logger                *slog.Logger
	now                   func() time.Time
	secretKey             string
	AllowOpenRegistration bool
	afterPasswordVerified func()
	beforeRotate          func()
	verifyPassword        func(encoded, password string) (bool, error)
}

func New(st *store.Store, signer *Signer, logger *slog.Logger) *Service {
	if logger == nil {
		logger = slog.Default()
	}
	_ = dummyPasswordHash()
	return &Service{store: st, signer: signer, logger: logger, now: time.Now, verifyPassword: VerifyPassword}
}

type InviteAcceptor interface {
	PeekInvite(ctx context.Context, token string) (teamName, email string, err error)
	AcceptInvite(ctx context.Context, userID uuid.UUID, token string) error
}

func (s *Service) Register(ctx context.Context, email, password, inviteToken string, invites InviteAcceptor, meta SessionMeta) (*AuthResult, error) {
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
		user, err = s.store.CreateFirstUser(ctx, normalized, &hash)
		if errors.Is(err, store.ErrInstanceHasAccount) {
			return nil, ErrRegistrationClosed
		}
	default:
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
			cleanup := context.WithoutCancel(ctx)
			if delErr := s.store.DeleteUserAndPersonalTeam(cleanup, user.ID); delErr != nil {
				s.logger.Error("auth: invite rollback failed", "error", delErr, "user_id", uuid.UUID(user.ID.Bytes))
			}
			s.logger.Warn("auth: invite accept failed at registration", "error", err)
			return nil, ErrRegistrationClosed
		}
	}

	return s.issue(ctx, user, meta)
}

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

func (s *Service) Login(ctx context.Context, email, password string, meta SessionMeta) (*AuthResult, error) {
	normalized := strings.ToLower(strings.TrimSpace(email))

	user, err := s.store.GetUserByEmail(ctx, normalized)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			s.spendPasswordCheck(password)
			return nil, ErrInvalidCredentials
		}
		return nil, fmt.Errorf("auth: get user: %w", err)
	}
	if user.PasswordHash == nil {
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

	return s.issueOrChallenge(ctx, current, meta)
}

var dummyPasswordHash = sync.OnceValue(func() string {
	hash, err := HashPassword("gotham-login-timing-placeholder")
	if err != nil {
		return ""
	}
	return hash
})

func (s *Service) spendPasswordCheck(password string) {
	if _, err := s.verifyPassword(dummyPasswordHash(), password); err != nil {
		s.logger.Error("auth: dummy password verification", "error", err)
	}
}

func (s *Service) Refresh(ctx context.Context, refreshToken string, meta SessionMeta) (*AuthResult, error) {
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
		s.maybeRevokeFamily(ctx, session.UserID, hash)
		return nil, ErrUnauthorized
	}
	if !session.ExpiresAt.Valid || s.now().After(session.ExpiresAt.Time) {
		return nil, ErrUnauthorized
	}

	user, err := s.store.GetUserByID(ctx, session.UserID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrUnauthorized
		}
		return nil, fmt.Errorf("auth: get user: %w", err)
	}
	if session.CredentialVersion < user.CredentialVersion {
		if err := s.store.RevokeSession(ctx, hash); err != nil {
			s.logger.Error("auth: revoke stale session", "error", err, "user_id", uuid.UUID(user.ID.Bytes))
		}
		return nil, ErrUnauthorized
	}

	refreshTokenNext, refreshHashNext, err := newRefreshToken()
	if err != nil {
		return nil, err
	}
	if s.beforeRotate != nil {
		s.beforeRotate()
	}
	rotated, err := s.store.RotateSession(ctx, session.UserID, sqlc.RotateSessionParams{
		RevokedRefreshHash: hash,
		NewRefreshHash:     refreshHashNext,
		ExpiresAt:          pgTimestamp(s.now().Add(refreshTokenTTL)),
		CredentialVersion:  user.CredentialVersion,
		UserAgent:          cleanUserAgent(meta.UserAgent),
		Ip:                 parseSessionIP(meta.IP),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			s.maybeRevokeFamily(ctx, session.UserID, hash)
			return nil, ErrUnauthorized
		}
		return nil, fmt.Errorf("auth: rotate session: %w", err)
	}

	accessToken, err := s.signAccessToken(user, uuid.UUID(rotated.ID.Bytes))
	if err != nil {
		return nil, err
	}

	return &AuthResult{
		User:         toUser(user),
		AccessToken:  accessToken,
		ExpiresIn:    int64(accessTokenTTL / time.Second),
		RefreshToken: refreshTokenNext,
	}, nil
}

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

func (s *Service) Logout(ctx context.Context, refreshToken string) error {
	if refreshToken == "" {
		return nil
	}
	return s.store.DeleteSession(ctx, hashRefreshToken(refreshToken))
}

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

func (s *Service) VerifyAccessToken(token string) (*Claims, error) {
	return s.signer.VerifyAccessToken(token)
}

func (s *Service) IssueSession(ctx context.Context, user sqlc.User, meta SessionMeta) (*AuthResult, error) {
	return s.issueOrChallenge(ctx, user, meta)
}

func (s *Service) issueOrChallenge(ctx context.Context, user sqlc.User, meta SessionMeta) (*AuthResult, error) {
	row, err := s.store.GetUserTOTP(ctx, user.ID)
	switch {
	case err == nil && row.Enabled:
		token, err := s.signer.IssueMFAToken(uuid.UUID(user.ID.Bytes), user.CredentialVersion)
		if err != nil {
			return nil, err
		}
		return &AuthResult{User: toUser(user), MFARequired: true, MFAToken: token}, nil
	case err != nil && !errors.Is(err, pgx.ErrNoRows):
		return nil, fmt.Errorf("auth: get totp: %w", err)
	}
	return s.issue(ctx, user, meta)
}

func (s *Service) issue(ctx context.Context, user sqlc.User, meta SessionMeta) (*AuthResult, error) {
	return s.issueWith(ctx, s.store, user, meta)
}

func (s *Service) issueWith(ctx context.Context, st *store.Store, user sqlc.User, meta SessionMeta) (*AuthResult, error) {
	refreshToken, refreshHash, err := newRefreshToken()
	if err != nil {
		return nil, err
	}

	created, err := st.CreateSession(ctx, sqlc.CreateSessionParams{
		UserID:            user.ID,
		RefreshHash:       refreshHash,
		ExpiresAt:         pgTimestamp(s.now().Add(refreshTokenTTL)),
		CredentialVersion: user.CredentialVersion,
		UserAgent:         cleanUserAgent(meta.UserAgent),
		Ip:                parseSessionIP(meta.IP),
	})
	if err != nil {
		return nil, fmt.Errorf("auth: create session: %w", err)
	}

	accessToken, err := s.signAccessToken(user, uuid.UUID(created.ID.Bytes))
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

func (s *Service) signAccessToken(user sqlc.User, sessionID uuid.UUID) (string, error) {
	accessToken, _, err := s.signer.IssueAccessToken(uuid.UUID(user.ID.Bytes), user.Role, sessionID)
	return accessToken, err
}

func newRefreshToken() (token string, hash string, err error) {
	buf := make([]byte, refreshTokenBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", "", fmt.Errorf("auth: generate refresh token: %w", err)
	}
	token = base64.RawURLEncoding.EncodeToString(buf)
	return token, hashRefreshToken(token), nil
}

func hashRefreshToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

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

func ValidatePassword(password string) error {
	if len(password) < minPasswordLength || len(password) > maxPasswordLength {
		return fmt.Errorf("%w: password must be between %d and %d characters",
			ErrValidation, minPasswordLength, maxPasswordLength)
	}
	return nil
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

func toUser(user sqlc.User) *User {
	return &User{
		ID:          uuid.UUID(user.ID.Bytes).String(),
		Email:       user.Email,
		Avatar:      user.Avatar,
		Role:        user.Role,
		CreatedAt:   user.CreatedAt.Time,
		DisplayName: user.DisplayName,
		HasPassword: user.PasswordHash != nil,
	}
}

func pgUUID(id uuid.UUID) pgtype.UUID {
	return pgtype.UUID{Bytes: id, Valid: true}
}

func pgTimestamp(t time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: t, Valid: true}
}
