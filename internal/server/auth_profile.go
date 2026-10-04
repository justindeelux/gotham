package server

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/justindeelux/gotham/internal/auth"
)

// updateProfileRequest is the body of PATCH /me. display_name is raw so a
// missing field (no change) stays distinguishable from an explicit null
// (clear the name).
type updateProfileRequest struct {
	DisplayName json.RawMessage `json:"display_name"`
}

// changePasswordRequest is the body of POST /me/password. current_password is
// required only when the account already has a password; the service decides.
type changePasswordRequest struct {
	CurrentPassword string `json:"current_password"`
	NewPassword     string `json:"new_password"`
}

// handleUpdateProfile replaces the account's display name (null or blank
// clears it) and returns the updated account.
func (s *Server) handleUpdateProfile(w http.ResponseWriter, r *http.Request) {
	if !requireInteractiveSession(w, r) {
		return
	}
	userID, ok := UserIDFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, apiError{Message: "unauthorized"})
		return
	}

	var req updateProfileRequest
	if !s.decodeJSON(w, r, &req) {
		return
	}

	var displayName *string
	if req.DisplayName != nil {
		var name *string
		if err := json.Unmarshal(req.DisplayName, &name); err != nil {
			writeJSON(w, http.StatusBadRequest, apiError{Message: auth.ErrDisplayNameInvalid.Error()})
			return
		}
		displayName = name
	} else {
		// No display_name member: nothing to change, return the account.
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
		return
	}

	user, err := s.auth.UpdateProfile(r.Context(), userID, displayName)
	if err != nil {
		switch {
		case errors.Is(err, auth.ErrDisplayNameInvalid):
			writeJSON(w, http.StatusBadRequest, apiError{Message: auth.ErrDisplayNameInvalid.Error()})
		case errors.Is(err, auth.ErrUnauthorized):
			writeJSON(w, http.StatusUnauthorized, apiError{Message: "unauthorized"})
		default:
			s.logger.Error("auth: update profile", "error", err)
			writeJSON(w, http.StatusInternalServerError, apiError{Message: "internal error"})
		}
		return
	}

	writeJSON(w, http.StatusOK, meResponse{User: user})
}

// handleChangePassword replaces the account's password, ends every other
// session, and returns a fresh token pair for the caller.
func (s *Server) handleChangePassword(w http.ResponseWriter, r *http.Request) {
	if !requireInteractiveSession(w, r) {
		return
	}
	userID, ok := UserIDFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, apiError{Message: "unauthorized"})
		return
	}

	var req changePasswordRequest
	if !s.decodeJSON(w, r, &req) {
		return
	}

	result, err := s.auth.ChangePassword(r.Context(), userID, req.CurrentPassword, req.NewPassword)
	if err != nil {
		switch {
		case errors.Is(err, auth.ErrCurrentPasswordIncorrect):
			// A wrong current password is a 400, never a 401: the web layer
			// treats 401 as session expiry.
			writeJSON(w, http.StatusBadRequest, apiError{Message: auth.ErrCurrentPasswordIncorrect.Error()})
		case errors.Is(err, auth.ErrValidation):
			writeJSON(w, http.StatusBadRequest, apiError{Message: err.Error()})
		case errors.Is(err, auth.ErrUnauthorized):
			writeJSON(w, http.StatusUnauthorized, apiError{Message: "unauthorized"})
		default:
			s.logger.Error("auth: change password", "error", err)
			writeJSON(w, http.StatusInternalServerError, apiError{Message: "internal error"})
		}
		return
	}

	writeJSON(w, http.StatusOK, newAuthResponse(result))
}
