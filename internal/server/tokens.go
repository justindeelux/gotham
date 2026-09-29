package server

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/auth"
)

// apiTokenRole is the role assigned to API-token requests. Scopes carry the
// finer-grained authorization; roles are unified in a later phase.
const apiTokenRole = "user"

// TokenService is the subset of auth.APITokenService the HTTP layer depends on.
// Keeping it an interface lets tests substitute a fake without a database.
type TokenService interface {
	Create(ctx context.Context, userID uuid.UUID, name string, scopes []string) (*auth.CreatedToken, error)
	List(ctx context.Context, userID uuid.UUID) ([]auth.APIToken, error)
	Revoke(ctx context.Context, userID, id uuid.UUID) error
	Rotate(ctx context.Context, userID, id uuid.UUID) (*auth.CreatedToken, error)
	Authenticate(ctx context.Context, token string) (*auth.TokenIdentity, error)
}

// createTokenRequest is the body of POST /api/v1/tokens.
type createTokenRequest struct {
	Name   string   `json:"name"`
	Scopes []string `json:"scopes"`
}

// tokenResponse is the body returned once when a token is created or rotated.
type tokenResponse struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Scopes    []string  `json:"scopes"`
	Token     string    `json:"token"`
	CreatedAt time.Time `json:"created_at"`
}

// tokenMetadata is a token descriptor returned by the list endpoint. It never
// carries the secret.
type tokenMetadata struct {
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	Scopes     []string   `json:"scopes"`
	LastUsedAt *time.Time `json:"last_used_at"`
	RevokedAt  *time.Time `json:"revoked_at"`
	CreatedAt  time.Time  `json:"created_at"`
}

// tokenListResponse is the body of GET /api/v1/tokens.
type tokenListResponse struct {
	Tokens []tokenMetadata `json:"tokens"`
}

// mountTokenRoutes registers the authenticated API-token endpoints under /api.
func (s *Server) mountTokenRoutes(api chi.Router) {
	api.Group(func(protected chi.Router) {
		protected.Use(s.RequireAuth)
		protected.Get("/v1/tokens", s.handleListTokens)
		protected.Post("/v1/tokens", s.handleCreateToken)
		protected.Post("/v1/tokens/{id}/rotate", s.handleRotateToken)
		protected.Delete("/v1/tokens/{id}", s.handleRevokeToken)
	})
}

// handleListTokens returns the caller's token metadata.
func (s *Server) handleListTokens(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, apiError{Message: "unauthorized"})
		return
	}

	tokens, err := s.tokens.List(r.Context(), userID)
	if err != nil {
		s.logger.Error("tokens: list", "error", err)
		writeJSON(w, http.StatusInternalServerError, apiError{Message: "internal error"})
		return
	}

	response := tokenListResponse{Tokens: make([]tokenMetadata, 0, len(tokens))}
	for _, token := range tokens {
		response.Tokens = append(response.Tokens, newTokenMetadata(token))
	}
	writeJSON(w, http.StatusOK, response)
}

// handleCreateToken creates a scoped token and returns its plaintext once.
func (s *Server) handleCreateToken(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, apiError{Message: "unauthorized"})
		return
	}

	var req createTokenRequest
	if !s.decodeJSON(w, r, &req) {
		return
	}

	// The requested scopes are canonicalized first, so the operator gate below
	// and the values the token service persists cannot diverge: a padded or
	// duplicated "admin" is normalized here and then checked, never stored
	// past the gate (fix-round-3 A).
	scopes, err := auth.NormalizeScopes(req.Scopes)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, apiError{Message: err.Error()})
		return
	}

	// Minting the admin scope is the platform-operator boundary: an
	// admin-scoped token unlocks the platform-global proxy surface
	// (RequirePlatformAdmin), so any authenticated user could otherwise
	// self-service that boundary. Plain read/deploy tokens stay available to
	// every account.
	if slices.Contains(scopes, auth.ScopeAdmin) && !s.isPlatformOperator(r) {
		writeJSON(w, http.StatusForbidden, apiError{Message: platformAdminScopeDenied})
		return
	}

	created, err := s.tokens.Create(r.Context(), userID, req.Name, scopes)
	if err != nil {
		if errors.Is(err, auth.ErrValidation) {
			writeJSON(w, http.StatusBadRequest, apiError{Message: err.Error()})
			return
		}
		s.logger.Error("tokens: create", "error", err)
		writeJSON(w, http.StatusInternalServerError, apiError{Message: "internal error"})
		return
	}

	// The plaintext secret is never logged at Info level.
	s.logger.Debug("tokens: created", "token_id", created.ID.String(), "user_id", userID.String())
	writeJSON(w, http.StatusCreated, newTokenResponse(created))
}

// handleRotateToken revokes an owned token and returns its replacement.
func (s *Server) handleRotateToken(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, apiError{Message: "unauthorized"})
		return
	}

	id, ok := tokenIDParam(w, r)
	if !ok {
		return
	}

	rotated, err := s.tokens.Rotate(r.Context(), userID, id)
	if err != nil {
		s.writeTokenError(w, "rotate", err)
		return
	}

	s.logger.Debug("tokens: rotated", "token_id", rotated.ID.String(), "user_id", userID.String())
	writeJSON(w, http.StatusOK, newTokenResponse(rotated))
}

// handleRevokeToken revokes an owned token.
func (s *Server) handleRevokeToken(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, apiError{Message: "unauthorized"})
		return
	}

	id, ok := tokenIDParam(w, r)
	if !ok {
		return
	}

	if err := s.tokens.Revoke(r.Context(), userID, id); err != nil {
		s.writeTokenError(w, "revoke", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// writeTokenError maps a token service error to its HTTP response.
func (s *Server) writeTokenError(w http.ResponseWriter, op string, err error) {
	switch {
	case errors.Is(err, auth.ErrNotFound):
		writeJSON(w, http.StatusNotFound, apiError{Message: "not found"})
	case errors.Is(err, auth.ErrValidation):
		writeJSON(w, http.StatusBadRequest, apiError{Message: err.Error()})
	default:
		s.logger.Error("tokens: "+op, "error", err)
		writeJSON(w, http.StatusInternalServerError, apiError{Message: "internal error"})
	}
}

// tokenIDParam parses the {id} path parameter, answering 400 when it is not a
// UUID.
func tokenIDParam(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, apiError{Message: "invalid token id"})
		return uuid.Nil, false
	}
	return id, true
}

// newTokenResponse maps a created token to its wire representation.
func newTokenResponse(created *auth.CreatedToken) tokenResponse {
	return tokenResponse{
		ID:        created.ID.String(),
		Name:      created.Name,
		Scopes:    created.Scopes,
		Token:     created.Token,
		CreatedAt: created.CreatedAt,
	}
}

// newTokenMetadata maps token metadata to its wire representation.
func newTokenMetadata(token auth.APIToken) tokenMetadata {
	return tokenMetadata{
		ID:         token.ID.String(),
		Name:       token.Name,
		Scopes:     token.Scopes,
		LastUsedAt: token.LastUsedAt,
		RevokedAt:  token.RevokedAt,
		CreatedAt:  token.CreatedAt,
	}
}

// RequireScopes enforces API-token scopes on a route. JWT-authenticated
// requests carry no scopes and hold every scope in Phase 1; an API token must
// hold each required scope or the request is rejected with 403.
func RequireScopes(required ...string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			scopes, isAPIToken := ScopesFromContext(r.Context())
			if !isAPIToken || auth.ScopesContain(scopes, required...) {
				next.ServeHTTP(w, r)
				return
			}
			writeJSON(w, http.StatusForbidden, apiError{Message: "insufficient scope"})
		})
	}
}
