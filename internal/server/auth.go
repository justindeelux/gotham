package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/auth"
)

const maxAuthBodyBytes = 1 << 20

const tokenTypeBearer = "Bearer"

type AuthService interface {
	Register(ctx context.Context, email, password string, meta auth.SessionMeta) (*auth.AuthResult, error)
	Login(ctx context.Context, email, password string, meta auth.SessionMeta) (*auth.AuthResult, error)
	Refresh(ctx context.Context, refreshToken string, meta auth.SessionMeta) (*auth.AuthResult, error)
	Logout(ctx context.Context, refreshToken string) error
	Me(ctx context.Context, userID uuid.UUID) (*auth.User, error)
	UpdateProfile(ctx context.Context, userID uuid.UUID, displayName *string) (*auth.User, error)
	ChangePassword(ctx context.Context, userID uuid.UUID, current, newPassword string, meta auth.SessionMeta) (*auth.AuthResult, error)
	ListSessions(ctx context.Context, userID, currentSessionID uuid.UUID) ([]auth.SessionInfo, error)
	SessionSignInAt(ctx context.Context, userID, sessionID uuid.UUID) (time.Time, bool, error)
	RevokeSession(ctx context.Context, userID, id uuid.UUID) error
	RevokeOtherSessions(ctx context.Context, userID, currentSessionID uuid.UUID) error
	VerifyAccessToken(token string) (*auth.Claims, error)
	TOTPStatus(ctx context.Context, userID uuid.UUID) (*auth.TOTPStatus, error)
	BeginTOTPEnrollment(ctx context.Context, userID uuid.UUID) (*auth.TOTPEnrollment, error)
	EnableTOTP(ctx context.Context, userID uuid.UUID, password, code string, currentSessionID uuid.UUID) ([]string, error)
	DisableTOTP(ctx context.Context, userID uuid.UUID, password, code string, currentSessionID uuid.UUID) error
	RegenerateTOTPRecoveryCodes(ctx context.Context, userID uuid.UUID, password, code string, currentSessionID uuid.UUID) ([]string, error)
	CompleteMFALogin(ctx context.Context, mfaToken, code string, meta auth.SessionMeta) (*auth.AuthResult, error)
}

type contextKey int

const (
	userIDKey contextKey = iota
	scopesKey
	apiTokenKey
	sessionIDKey
)

func UserIDFromContext(ctx context.Context) (uuid.UUID, bool) {
	userID, ok := ctx.Value(userIDKey).(uuid.UUID)
	return userID, ok
}

func ScopesFromContext(ctx context.Context) ([]string, bool) {
	scopes, ok := ctx.Value(scopesKey).([]string)
	return scopes, ok
}

func IsAPITokenRequest(ctx context.Context) bool {
	apiToken, _ := ctx.Value(apiTokenKey).(bool)
	return apiToken
}

func SessionIDFromContext(ctx context.Context) uuid.UUID {
	sessionID, _ := ctx.Value(sessionIDKey).(uuid.UUID)
	return sessionID
}

func (s *Server) requireInteractiveSession(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if IsAPITokenRequest(r.Context()) {
			writeJSON(w, http.StatusForbidden, apiError{Message: "this action needs an interactive session"})
			return
		}
		next.ServeHTTP(w, r)
	})
}

type credentialsRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type refreshTokenRequest struct {
	RefreshToken string `json:"refresh_token"`
}

type authResponse struct {
	User         *auth.User `json:"user"`
	AccessToken  string     `json:"access_token"`
	TokenType    string     `json:"token_type"`
	ExpiresIn    int64      `json:"expires_in"`
	RefreshToken string     `json:"refresh_token"`
	MFARequired  bool       `json:"mfa_required,omitempty"`
	MFAToken     string     `json:"mfa_token,omitempty"`
}

type meResponse struct {
	User *auth.User `json:"user"`
}

type authConfigResponse struct {
	RegistrationOpen bool `json:"registrationOpen"`
}

func (s *Server) mountAuthRoutes(api chi.Router) {
	api.Route("/v1/auth", func(r chi.Router) {
		r.Get("/config", s.handleAuthConfig)
		r.With(s.rateLimit).Post("/register", s.handleRegister)
		r.With(s.rateLimit).Post("/login", s.handleLogin)
		r.With(s.rateLimit).Post("/login/totp", s.handleLoginTOTP)
		r.With(s.refreshRateLimit).Post("/refresh", s.handleRefresh)
		r.With(s.refreshRateLimit).Post("/logout", s.handleLogout)

		r.Group(func(protected chi.Router) {
			protected.Use(s.RequireAuth)
			protected.Get("/me", s.handleMe)
			protected.Group(func(profile chi.Router) {
				profile.Use(s.rateLimit)
				profile.Use(s.requireInteractiveSession)
				profile.Patch("/me", s.handleUpdateProfile)
				profile.Post("/me/password", s.handleChangePassword)
				profile.Get("/me/sessions", s.handleListSessions)
				profile.Delete("/me/sessions/{id}", s.handleRevokeSession)
				profile.Post("/me/sessions/revoke-others", s.handleRevokeOtherSessions)
				profile.Get("/me/totp", s.handleTOTPStatus)
				profile.Post("/me/totp/enroll", s.handleTOTPEnroll)
				profile.Post("/me/totp/enable", s.handleTOTPEnable)
				profile.Post("/me/totp/disable", s.handleTOTPDisable)
				profile.Post("/me/totp/recovery-codes", s.handleTOTPRegenerate)
			})
		})
	})
}

func (s *Server) handleAuthConfig(w http.ResponseWriter, r *http.Request) {
	open := s.allowRegistration
	if s.persistence != nil {
		count, err := s.persistence.CountUsers(r.Context())
		if err != nil {
			s.logger.Error("auth: config", "error", err)
			writeJSON(w, http.StatusInternalServerError, apiError{Message: "internal error"})
			return
		}
		open = count == 0 || s.allowRegistration
	}
	writeJSON(w, http.StatusOK, authConfigResponse{RegistrationOpen: open})
}

func (s *Server) handleRegister(w http.ResponseWriter, r *http.Request) {
	var req credentialsRequest
	if !s.decodeJSON(w, r, &req) {
		return
	}

	result, err := s.auth.Register(r.Context(), req.Email, req.Password, s.sessionMeta(r))
	if err != nil {
		switch {
		case errors.Is(err, auth.ErrEmailTaken):
			writeJSON(w, http.StatusConflict, apiError{Message: "email already registered"})
		case errors.Is(err, auth.ErrRegistrationClosed):
			writeJSON(w, http.StatusForbidden, apiError{Message: "registration is closed"})
		case errors.Is(err, auth.ErrValidation):
			writeJSON(w, http.StatusBadRequest, apiError{Message: err.Error()})
		default:
			s.logger.Error("auth: register", "error", err)
			writeJSON(w, http.StatusInternalServerError, apiError{Message: "internal error"})
		}
		return
	}

	s.clearOAuthCookies(w, r)
	writeJSON(w, http.StatusOK, newAuthResponse(result))
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req credentialsRequest
	if !s.decodeJSON(w, r, &req) {
		return
	}

	result, err := s.auth.Login(r.Context(), req.Email, req.Password, s.sessionMeta(r))
	if err != nil {
		switch {
		case errors.Is(err, auth.ErrInvalidCredentials):
			writeJSON(w, http.StatusUnauthorized, apiError{Message: "invalid credentials"})
		case errors.Is(err, auth.ErrValidation):
			writeJSON(w, http.StatusBadRequest, apiError{Message: err.Error()})
		default:
			s.logger.Error("auth: login", "error", err)
			writeJSON(w, http.StatusInternalServerError, apiError{Message: "internal error"})
		}
		return
	}

	s.clearOAuthCookies(w, r)
	writeJSON(w, http.StatusOK, newAuthResponse(result))
}

func (s *Server) handleRefresh(w http.ResponseWriter, r *http.Request) {
	var req refreshTokenRequest
	if !s.decodeJSON(w, r, &req) {
		return
	}

	result, err := s.auth.Refresh(r.Context(), req.RefreshToken, s.sessionMeta(r))
	if err != nil {
		if errors.Is(err, auth.ErrUnauthorized) {
			writeJSON(w, http.StatusUnauthorized, apiError{Message: "unauthorized"})
			return
		}
		s.logger.Error("auth: refresh", "error", err)
		writeJSON(w, http.StatusInternalServerError, apiError{Message: "internal error"})
		return
	}

	writeJSON(w, http.StatusOK, newAuthResponse(result))
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	s.clearOAuthCookies(w, r)

	var req refreshTokenRequest
	if err := decodeJSONBody(w, r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, apiError{Message: "invalid request body"})
		return
	}

	if err := s.auth.Logout(r.Context(), req.RefreshToken); err != nil {
		s.logger.Error("auth: logout", "error", err)
		writeJSON(w, http.StatusInternalServerError, apiError{Message: "internal error"})
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, apiError{Message: "unauthorized"})
		return
	}

	user, err := s.auth.Me(r.Context(), userID)
	if err != nil {
		if errors.Is(err, auth.ErrUnauthorized) {
			writeJSON(w, http.StatusUnauthorized, apiError{Message: "unauthorized"})
			return
		}
		s.logger.Error("auth: me", "error", err)
		writeJSON(w, http.StatusInternalServerError, apiError{Message: "internal error"})
		return
	}

	writeJSON(w, http.StatusOK, meResponse{User: user})
}

func (s *Server) RequireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token, ok := bearerToken(r)
		if !ok {
			writeJSON(w, http.StatusUnauthorized, apiError{Message: "unauthorized"})
			return
		}

		if strings.HasPrefix(token, auth.APITokenPrefix) {
			s.authenticateAPIToken(w, r, next, token)
			return
		}

		claims, err := s.auth.VerifyAccessToken(token)
		if err != nil {
			writeJSON(w, http.StatusUnauthorized, apiError{Message: "unauthorized"})
			return
		}

		userID, err := uuid.Parse(claims.Subject)
		if err != nil {
			writeJSON(w, http.StatusUnauthorized, apiError{Message: "unauthorized"})
			return
		}

		ctx := context.WithValue(r.Context(), userIDKey, userID)
		ctx = context.WithValue(ctx, sessionIDKey, parseSessionClaim(claims.SessionID))
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (s *Server) authenticateAPIToken(w http.ResponseWriter, r *http.Request, next http.Handler, token string) {
	if s.tokens == nil {
		writeJSON(w, http.StatusUnauthorized, apiError{Message: "unauthorized"})
		return
	}

	identity, err := s.tokens.Authenticate(r.Context(), token)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, apiError{Message: "unauthorized"})
		return
	}

	ctx := context.WithValue(r.Context(), userIDKey, identity.UserID)
	ctx = context.WithValue(ctx, scopesKey, identity.Scopes)
	ctx = context.WithValue(ctx, apiTokenKey, true)
	next.ServeHTTP(w, r.WithContext(ctx))
}

func bearerToken(r *http.Request) (string, bool) {
	const prefix = "Bearer "

	header := r.Header.Get("Authorization")
	if len(header) <= len(prefix) || !strings.EqualFold(header[:len(prefix)], prefix) {
		return "", false
	}

	token := strings.TrimSpace(header[len(prefix):])
	if token == "" {
		return "", false
	}
	return token, true
}

func (s *Server) decodeJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	if err := decodeJSONBody(w, r, dst); err != nil {
		writeJSON(w, http.StatusBadRequest, apiError{Message: "invalid request body"})
		return false
	}
	return true
}

func decodeJSONBody(w http.ResponseWriter, r *http.Request, dst any) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxAuthBodyBytes)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dst); err != nil {
		return err
	}

	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return errors.New("auth: unexpected trailing JSON")
	}
	return nil
}

func newAuthResponse(result *auth.AuthResult) authResponse {
	return authResponse{
		User:         result.User,
		AccessToken:  result.AccessToken,
		TokenType:    tokenTypeBearer,
		ExpiresIn:    result.ExpiresIn,
		RefreshToken: result.RefreshToken,
		MFARequired:  result.MFARequired,
		MFAToken:     result.MFAToken,
	}
}
