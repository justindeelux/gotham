package server

import (
	"errors"
	"net/http"

	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/auth"
)

// Two-factor authentication over HTTP (JUS-196).
//
// The profile routes are mounted in the interactive-session group, so an API
// token can never manage the second factor. Every mutating route requires the
// account password (when one is set) plus a current code, so a stolen access
// token alone cannot turn the second factor off or repoint it.

// totpCodeRequest is the body of the profile TOTP mutations. password is
// required only when the account has one; the service decides.
type totpCodeRequest struct {
	Password string `json:"password"`
	Code     string `json:"code"`
}

// mfaLoginRequest is the body of the second login step: the half-authenticated
// token from /login (or the OAuth exchange) and the submitted code.
type mfaLoginRequest struct {
	MFAToken string `json:"mfa_token"`
	Code     string `json:"code"`
}

// recoveryCodesResponse carries the freshly minted recovery codes, shown once.
type recoveryCodesResponse struct {
	RecoveryCodes []string `json:"recovery_codes"`
}

// handleTOTPStatus returns whether two-factor is enabled and how many recovery
// codes are left.
func (s *Server) handleTOTPStatus(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, apiError{Message: "unauthorized"})
		return
	}

	status, err := s.auth.TOTPStatus(r.Context(), userID)
	if err != nil {
		s.writeTOTPError(w, "status", err, http.StatusUnauthorized)
		return
	}
	writeJSON(w, http.StatusOK, status)
}

// handleTOTPEnroll mints a pending secret and returns the setup material (the
// base32 secret and the otpauth URI the QR encodes). Nothing is live until
// enable confirms a code.
func (s *Server) handleTOTPEnroll(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, apiError{Message: "unauthorized"})
		return
	}

	enrollment, err := s.auth.BeginTOTPEnrollment(r.Context(), userID)
	if err != nil {
		s.writeTOTPError(w, "enroll", err, http.StatusConflict)
		return
	}
	writeJSON(w, http.StatusOK, enrollment)
}

// handleTOTPEnable confirms the pending secret with a code and returns the
// recovery codes once.
func (s *Server) handleTOTPEnable(w http.ResponseWriter, r *http.Request) {
	userID, code, ok := s.decodeTOTPRequest(w, r)
	if !ok {
		return
	}

	codes, err := s.auth.EnableTOTP(r.Context(), userID, code.Password, code.Code)
	if err != nil {
		s.writeTOTPError(w, "enable", err, http.StatusConflict)
		return
	}
	writeJSON(w, http.StatusOK, recoveryCodesResponse{RecoveryCodes: codes})
}

// handleTOTPDisable turns two-factor off after proving the password and a code.
func (s *Server) handleTOTPDisable(w http.ResponseWriter, r *http.Request) {
	userID, code, ok := s.decodeTOTPRequest(w, r)
	if !ok {
		return
	}

	if err := s.auth.DisableTOTP(r.Context(), userID, code.Password, code.Code); err != nil {
		s.writeTOTPError(w, "disable", err, http.StatusConflict)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleTOTPRegenerate replaces the recovery codes, invalidating every previous
// code.
func (s *Server) handleTOTPRegenerate(w http.ResponseWriter, r *http.Request) {
	userID, code, ok := s.decodeTOTPRequest(w, r)
	if !ok {
		return
	}

	codes, err := s.auth.RegenerateTOTPRecoveryCodes(r.Context(), userID, code.Password, code.Code)
	if err != nil {
		s.writeTOTPError(w, "regenerate recovery codes", err, http.StatusConflict)
		return
	}
	writeJSON(w, http.StatusOK, recoveryCodesResponse{RecoveryCodes: codes})
}

// handleLoginTOTP completes a login whose account has TOTP enabled: it redeems
// the half-authenticated token with a code and only then returns the token pair.
func (s *Server) handleLoginTOTP(w http.ResponseWriter, r *http.Request) {
	var req mfaLoginRequest
	if !s.decodeJSON(w, r, &req) {
		return
	}

	result, err := s.auth.CompleteMFALogin(r.Context(), req.MFAToken, req.Code, s.sessionMeta(r))
	if err != nil {
		switch {
		case errors.Is(err, auth.ErrInvalidCode), errors.Is(err, auth.ErrInvalidCredentials),
			errors.Is(err, auth.ErrTOTPNotConfigured):
			// One message for every refusal: a caller cannot learn from the
			// response whether the token or the code was the problem.
			writeJSON(w, http.StatusUnauthorized, apiError{Message: "invalid code"})
		default:
			s.logger.Error("auth: complete mfa login", "error", err)
			writeJSON(w, http.StatusInternalServerError, apiError{Message: "internal error"})
		}
		return
	}

	s.clearOAuthCookies(w, r)
	writeJSON(w, http.StatusOK, newAuthResponse(result))
}

// decodeTOTPRequest reads the caller and the code body shared by the profile
// TOTP mutations.
func (s *Server) decodeTOTPRequest(w http.ResponseWriter, r *http.Request) (uuid.UUID, totpCodeRequest, bool) {
	userID, ok := UserIDFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, apiError{Message: "unauthorized"})
		return uuid.Nil, totpCodeRequest{}, false
	}

	var req totpCodeRequest
	if !s.decodeJSON(w, r, &req) {
		return uuid.Nil, totpCodeRequest{}, false
	}
	return userID, req, true
}

// writeTOTPError maps a TOTP service error to its HTTP response. The messages
// that are API contract bodies (an invalid code, a wrong current password, a
// conflicting state) render verbatim with a 400/409; an unexpected failure is a
// logged 500. A session-level ErrUnauthorized is always a 401.
func (s *Server) writeTOTPError(w http.ResponseWriter, action string, err error, conflictStatus int) {
	switch {
	case errors.Is(err, auth.ErrUnauthorized):
		writeJSON(w, http.StatusUnauthorized, apiError{Message: "unauthorized"})
	case errors.Is(err, auth.ErrCurrentPasswordIncorrect):
		writeJSON(w, http.StatusBadRequest, apiError{Message: auth.ErrCurrentPasswordIncorrect.Error()})
	case errors.Is(err, auth.ErrInvalidCode):
		writeJSON(w, http.StatusBadRequest, apiError{Message: auth.ErrInvalidCode.Error()})
	case errors.Is(err, auth.ErrTOTPAlreadyEnabled),
		errors.Is(err, auth.ErrTOTPNotEnrolled),
		errors.Is(err, auth.ErrTOTPNotConfigured):
		writeJSON(w, conflictStatus, apiError{Message: err.Error()})
	default:
		s.logger.Error("auth: totp "+action, "error", err)
		writeJSON(w, http.StatusInternalServerError, apiError{Message: "internal error"})
	}
}
