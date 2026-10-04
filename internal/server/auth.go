package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/auth"
)

// maxAuthBodyBytes bounds the size of an authentication request body.
const maxAuthBodyBytes = 1 << 20 // 1 MiB

// tokenTypeBearer is the OAuth2 token type reported to clients.
const tokenTypeBearer = "Bearer"

// AuthService is the subset of auth.Service the HTTP layer depends on. Keeping
// it an interface lets tests substitute a fake without a database.
type AuthService interface {
	Register(ctx context.Context, email, password, inviteToken string, invites auth.InviteAcceptor, meta auth.SessionMeta) (*auth.AuthResult, error)
	Login(ctx context.Context, email, password string, meta auth.SessionMeta) (*auth.AuthResult, error)
	Refresh(ctx context.Context, refreshToken string, meta auth.SessionMeta) (*auth.AuthResult, error)
	Logout(ctx context.Context, refreshToken string) error
	Me(ctx context.Context, userID uuid.UUID) (*auth.User, error)
	UpdateProfile(ctx context.Context, userID uuid.UUID, displayName *string) (*auth.User, error)
	ChangePassword(ctx context.Context, userID uuid.UUID, current, newPassword string, meta auth.SessionMeta) (*auth.AuthResult, error)
	ListSessions(ctx context.Context, userID, currentSessionID uuid.UUID) ([]auth.SessionInfo, error)
	RevokeSession(ctx context.Context, userID, id uuid.UUID) error
	RevokeOtherSessions(ctx context.Context, userID, currentSessionID uuid.UUID) error
	VerifyAccessToken(token string) (*auth.Claims, error)
}

// contextKey is the unexported type for authenticated-request context values.
type contextKey int

// Context keys for values set by RequireAuth.
const (
	userIDKey contextKey = iota
	roleKey
	scopesKey
	apiTokenKey
	sessionIDKey
)

// UserIDFromContext returns the authenticated user ID set by RequireAuth.
func UserIDFromContext(ctx context.Context) (uuid.UUID, bool) {
	userID, ok := ctx.Value(userIDKey).(uuid.UUID)
	return userID, ok
}

// RoleFromContext returns the authenticated role set by RequireAuth.
func RoleFromContext(ctx context.Context) (string, bool) {
	role, ok := ctx.Value(roleKey).(string)
	return role, ok
}

// ScopesFromContext returns the API-token scopes set by RequireAuth. A JWT
// request carries no scopes, so ok is false for it.
func ScopesFromContext(ctx context.Context) ([]string, bool) {
	scopes, ok := ctx.Value(scopesKey).([]string)
	return scopes, ok
}

// IsAPITokenRequest reports whether the request was authenticated with a
// scoped API token rather than an interactive JWT session. The self-service
// profile routes (PATCH /me, POST /me/password, and the PF-2 session routes)
// reject API tokens through the requireInteractiveSession middleware.
func IsAPITokenRequest(ctx context.Context) bool {
	apiToken, _ := ctx.Value(apiTokenKey).(bool)
	return apiToken
}

// SessionIDFromContext returns the refresh-session id carried by the access
// token's "sid" claim (PF-2). It is uuid.Nil when the token predates the
// claim: "current unknown", never an authentication failure.
func SessionIDFromContext(ctx context.Context) uuid.UUID {
	sessionID, _ := ctx.Value(sessionIDKey).(uuid.UUID)
	return sessionID
}

// requireInteractiveSession is chi middleware answering 403 unless the request
// carries an interactive JWT session. A scoped API token (even admin) must
// never change profile material. It lives on the profile route group so every
// route mounted there (including the PF-2 session routes) is guarded without
// per-handler code.
func (s *Server) requireInteractiveSession(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if IsAPITokenRequest(r.Context()) {
			writeJSON(w, http.StatusForbidden, apiError{Message: "this action needs an interactive session"})
			return
		}
		next.ServeHTTP(w, r)
	})
}

// credentialsRequest is the body of register and login. inviteToken carries
// the admin-created invite (P-A2) for registration after the first account.
type credentialsRequest struct {
	Email       string `json:"email"`
	Password    string `json:"password"`
	InviteToken string `json:"inviteToken,omitempty"`
}

// refreshTokenRequest is the body of refresh and logout.
type refreshTokenRequest struct {
	RefreshToken string `json:"refresh_token"`
}

// authResponse is the token-pair body returned by register, login, and refresh.
type authResponse struct {
	User         *auth.User `json:"user"`
	AccessToken  string     `json:"access_token"`
	TokenType    string     `json:"token_type"`
	ExpiresIn    int64      `json:"expires_in"`
	RefreshToken string     `json:"refresh_token"`
}

// meResponse is the body returned by /auth/me.
type meResponse struct {
	User *auth.User `json:"user"`
}

// authConfigResponse is the public instance configuration the SPA needs
// before it renders the register surface.
type authConfigResponse struct {
	RegistrationOpen bool `json:"registrationOpen"`
}

// inviteValidateResponse tells the invitee which team they are joining before
// they create credentials.
type inviteValidateResponse struct {
	Team  string `json:"team"`
	Email string `json:"email"`
}

// mountAuthRoutes registers the authentication endpoints under /api.
func (s *Server) mountAuthRoutes(api chi.Router) {
	api.Route("/v1/auth", func(r chi.Router) {
		// Public surface: the SPA probes /config before rendering the register
		// tab, and invite links validate before credentials. /config is a
		// cheap, secret-free read the SPA calls on every auth render, so it
		// stays off the credential rate limiter; validate keeps it (a token
		// must not be brute-forceable).
		r.Get("/config", s.handleAuthConfig)
		r.With(s.rateLimit).Get("/invites/validate", s.handleInviteValidate)

		r.With(s.rateLimit).Post("/register", s.handleRegister)
		r.With(s.rateLimit).Post("/login", s.handleLogin)
		r.With(s.refreshRateLimit).Post("/refresh", s.handleRefresh)
		r.With(s.refreshRateLimit).Post("/logout", s.handleLogout)

		// Protected group: Phase 2+ can mount further authenticated routes here.
		r.Group(func(protected chi.Router) {
			protected.Use(s.RequireAuth)
			protected.Get("/me", s.handleMe)
			// Self-service profile group (PF-2 mounts its session routes
			// here too): interactive JWT sessions only, behind the
			// credential rate limiter. The API-token guard is middleware so
			// a route mounted in this group cannot forget it.
			protected.Group(func(profile chi.Router) {
				profile.Use(s.rateLimit)
				profile.Use(s.requireInteractiveSession)
				profile.Patch("/me", s.handleUpdateProfile)
				profile.Post("/me/password", s.handleChangePassword)
				profile.Get("/me/sessions", s.handleListSessions)
				profile.Delete("/me/sessions/{id}", s.handleRevokeSession)
				profile.Post("/me/sessions/revoke-others", s.handleRevokeOtherSessions)
			})
		})
	})
}

// handleAuthConfig reports whether registration is open: it is open only on a
// fresh instance with zero accounts (P-A2). Without a store the instance is
// treated as closed, matching the closed-by-default registration rule.
func (s *Server) handleAuthConfig(w http.ResponseWriter, r *http.Request) {
	// allowRegistration (GOTHAM_AUTH_ALLOW_REGISTRATION) is a test/dev escape
	// hatch; production relies on the closed default.
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

// handleInviteValidate looks up a pending invite token so the invitee sees
// which team they are joining before they create credentials. Unknown,
// expired and consumed tokens all answer 404, never a distinction.
func (s *Server) handleInviteValidate(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("token")
	if s.invites == nil || strings.TrimSpace(token) == "" {
		writeJSON(w, http.StatusNotFound, apiError{Message: "invite not found"})
		return
	}

	team, email, err := s.invites.PeekInvite(r.Context(), token)
	if err != nil {
		writeJSON(w, http.StatusNotFound, apiError{Message: "invite not found"})
		return
	}
	writeJSON(w, http.StatusOK, inviteValidateResponse{Team: team, Email: email})
}

// handleRegister creates an account and returns a token pair. On an instance
// that already has accounts (P-A2) a valid admin-created invite token is
// required; any registration refusal answers 403 without leaking whether the
// token itself was the problem.
func (s *Server) handleRegister(w http.ResponseWriter, r *http.Request) {
	var req credentialsRequest
	if !s.decodeJSON(w, r, &req) {
		return
	}

	result, err := s.auth.Register(r.Context(), req.Email, req.Password, req.InviteToken, s.invites, s.sessionMeta(r))
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

	// A registration starts a fresh session: drop any pending OAuth exchange
	// cookies so a stalled callback cannot overwrite it.
	s.clearOAuthCookies(w, r)
	writeJSON(w, http.StatusOK, newAuthResponse(result))
}

// handleLogin verifies credentials and returns a token pair.
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

	// A password login replaces any pending OAuth exchange: drop the protocol
	// cookies so a paused callback link cannot redeem after the fact.
	s.clearOAuthCookies(w, r)
	writeJSON(w, http.StatusOK, newAuthResponse(result))
}

// handleRefresh rotates a refresh token and returns a fresh token pair.
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

// handleLogout deletes the presented session and clears the OAuth protocol
// cookies on the 204, 400 and 500 responses it writes. The shared
// refresh/logout rate limiter answers 429 before this handler runs, so that
// response does not clear the cookies. A malformed body answers 400; a
// well-formed unknown or already-deleted token answers 204, so clients cannot
// probe which refresh tokens exist. A persistence failure answers 500:
// reporting success would leave the refresh token usable while the client
// believes it is signed out.
func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	// Clear the OAuth protocol cookies on the responses this handler writes
	// (204, 400, 500): a browser that attempted to log out must never be able
	// to redeem a pending OAuth exchange, whatever the outcome.
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

// handleMe returns the authenticated account.
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

// RequireAuth validates the bearer token and stores the user ID and role in the
// request context. It accepts both a JWT and a scoped API token: a token with
// the API-token prefix is resolved through the token service, everything else is
// verified as a JWT. It answers 401 for a missing or invalid token.
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
		ctx = context.WithValue(ctx, roleKey, claims.Role)
		ctx = context.WithValue(ctx, sessionIDKey, parseSessionClaim(claims.SessionID))
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// authenticateAPIToken resolves an API token and stores its owner and scopes in
// the request context. API tokens act as the default "user" role.
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
	ctx = context.WithValue(ctx, roleKey, apiTokenRole)
	ctx = context.WithValue(ctx, scopesKey, identity.Scopes)
	ctx = context.WithValue(ctx, apiTokenKey, true)
	next.ServeHTTP(w, r.WithContext(ctx))
}

// bearerToken extracts a bearer token from the Authorization header.
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

// decodeJSON decodes an authentication request body, answering 400 on failure.
func (s *Server) decodeJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	if err := decodeJSONBody(w, r, dst); err != nil {
		writeJSON(w, http.StatusBadRequest, apiError{Message: "invalid request body"})
		return false
	}
	return true
}

// decodeJSONBody decodes a size-limited JSON body, rejecting unknown fields.
// It requires exactly one JSON value: trailing input (a second document or
// garbage after the first value) is an error, so a handler cannot succeed on a
// body that is only partially valid.
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

// newAuthResponse maps an auth result to its wire representation.
func newAuthResponse(result *auth.AuthResult) authResponse {
	return authResponse{
		User:         result.User,
		AccessToken:  result.AccessToken,
		TokenType:    tokenTypeBearer,
		ExpiresIn:    result.ExpiresIn,
		RefreshToken: result.RefreshToken,
	}
}
