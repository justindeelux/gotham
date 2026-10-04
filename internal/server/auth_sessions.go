package server

import (
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/auth"
	"github.com/justindeelux/gotham/internal/clientip"
)

// sessionResponse is one row of GET /me/sessions.
type sessionResponse struct {
	ID         uuid.UUID `json:"id"`
	UserAgent  string    `json:"user_agent"`
	IP         string    `json:"ip"`
	CreatedAt  time.Time `json:"created_at"`
	LastUsedAt time.Time `json:"last_used_at"`
	Current    bool      `json:"current"`
}

// sessionsResponse is the body of GET /me/sessions. Sessions is never null:
// an account with no live session gets [].
type sessionsResponse struct {
	Sessions []sessionResponse `json:"sessions"`
}

// parseSessionClaim parses the access token's "sid" claim. A missing or
// malformed value means "current unknown" (a token minted before PF-2), never
// an authentication failure: callers get uuid.Nil.
func parseSessionClaim(raw string) uuid.UUID {
	if raw == "" {
		return uuid.Nil
	}
	sessionID, err := uuid.Parse(raw)
	if err != nil {
		return uuid.Nil
	}
	return sessionID
}

// sessionMeta resolves the device metadata recorded with a new or rotated
// refresh session: the request User-Agent and the client IP behind the
// trusted proxies (the same handling the rate limiter uses).
func (s *Server) sessionMeta(r *http.Request) auth.SessionMeta {
	return auth.SessionMeta{
		UserAgent: r.UserAgent(),
		IP:        clientip.ClientIP(r, s.trustedProxies),
	}
}

// handleListSessions returns the caller's live sessions, newest last use
// first. A token without a "sid" claim marks no row as current.
func (s *Server) handleListSessions(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, apiError{Message: "unauthorized"})
		return
	}

	sessions, err := s.auth.ListSessions(r.Context(), userID, SessionIDFromContext(r.Context()))
	if err != nil {
		if errors.Is(err, auth.ErrUnauthorized) {
			writeJSON(w, http.StatusUnauthorized, apiError{Message: "unauthorized"})
			return
		}
		s.logger.Error("auth: list sessions", "error", err)
		writeJSON(w, http.StatusInternalServerError, apiError{Message: "internal error"})
		return
	}

	out := make([]sessionResponse, 0, len(sessions))
	for _, session := range sessions {
		out = append(out, sessionResponse{
			ID:         session.ID,
			UserAgent:  session.UserAgent,
			IP:         session.IP,
			CreatedAt:  session.CreatedAt,
			LastUsedAt: session.LastUsedAt,
			Current:    session.Current,
		})
	}
	writeJSON(w, http.StatusOK, sessionsResponse{Sessions: out})
}

// handleRevokeSession ends one of the caller's sessions. An unknown, foreign,
// or already-dead id answers 404 (never 403, so a probe cannot distinguish
// them); ending the caller's own session is allowed and the web signs out
// afterwards.
//
// Revoking a session makes its refresh token unusable immediately; access
// tokens already issued live out their 15-minute life (the same documented
// window as reset-password and the PF-1 password change).
func (s *Server) handleRevokeSession(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, apiError{Message: "unauthorized"})
		return
	}

	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeJSON(w, http.StatusNotFound, apiError{Message: "session not found"})
		return
	}

	if err := s.auth.RevokeSession(r.Context(), userID, id); err != nil {
		switch {
		case errors.Is(err, auth.ErrNotFound):
			writeJSON(w, http.StatusNotFound, apiError{Message: "session not found"})
		case errors.Is(err, auth.ErrUnauthorized):
			writeJSON(w, http.StatusUnauthorized, apiError{Message: "unauthorized"})
		default:
			s.logger.Error("auth: revoke session", "error", err)
			writeJSON(w, http.StatusInternalServerError, apiError{Message: "internal error"})
		}
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// handleRevokeOtherSessions ends every live session of the caller except the
// current one. A token without a "sid" claim (minted before PF-2) answers
// 409: without a trustworthy current row the server refuses to guess.
//
// Revoking a session makes its refresh token unusable immediately; access
// tokens already issued live out their 15-minute life (the same documented
// window as reset-password and the PF-1 password change).
func (s *Server) handleRevokeOtherSessions(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, apiError{Message: "unauthorized"})
		return
	}

	if err := s.auth.RevokeOtherSessions(r.Context(), userID, SessionIDFromContext(r.Context())); err != nil {
		switch {
		case errors.Is(err, auth.ErrSessionUnknown):
			writeJSON(w, http.StatusConflict, apiError{Message: err.Error()})
		case errors.Is(err, auth.ErrUnauthorized):
			writeJSON(w, http.StatusUnauthorized, apiError{Message: "unauthorized"})
		default:
			s.logger.Error("auth: revoke other sessions", "error", err)
			writeJSON(w, http.StatusInternalServerError, apiError{Message: "internal error"})
		}
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
