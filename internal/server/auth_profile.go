package server

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/justindeelux/gotham/internal/auth"
)

type updateProfileRequest struct {
	DisplayName json.RawMessage `json:"display_name"`
}

type changePasswordRequest struct {
	CurrentPassword string `json:"current_password"`
	NewPassword     string `json:"new_password"`
}

func (s *Server) handleUpdateProfile(w http.ResponseWriter, r *http.Request) {
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

func (s *Server) handleChangePassword(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, apiError{Message: "unauthorized"})
		return
	}

	var req changePasswordRequest
	if !s.decodeJSON(w, r, &req) {
		return
	}

	result, err := s.auth.ChangePassword(r.Context(), userID, req.CurrentPassword, req.NewPassword, s.sessionMeta(r))
	if err != nil {
		switch {
		case errors.Is(err, auth.ErrCurrentPasswordIncorrect):
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
