package server

import (
	"errors"
	"net/http"

	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/auth"
)

type totpCodeRequest struct {
	Password string `json:"password"`
	Code     string `json:"code"`
}

type mfaLoginRequest struct {
	MFAToken string `json:"mfa_token"`
	Code     string `json:"code"`
}

type recoveryCodesResponse struct {
	RecoveryCodes []string `json:"recovery_codes"`
}

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

func (s *Server) handleTOTPEnable(w http.ResponseWriter, r *http.Request) {
	userID, code, ok := s.decodeTOTPRequest(w, r)
	if !ok {
		return
	}

	codes, err := s.auth.EnableTOTP(r.Context(), userID, code.Password, code.Code, SessionIDFromContext(r.Context()))
	if err != nil {
		s.writeTOTPError(w, "enable", err, http.StatusConflict)
		return
	}
	writeJSON(w, http.StatusOK, recoveryCodesResponse{RecoveryCodes: codes})
}

func (s *Server) handleTOTPDisable(w http.ResponseWriter, r *http.Request) {
	userID, code, ok := s.decodeTOTPRequest(w, r)
	if !ok {
		return
	}

	if err := s.auth.DisableTOTP(r.Context(), userID, code.Password, code.Code, SessionIDFromContext(r.Context())); err != nil {
		s.writeTOTPError(w, "disable", err, http.StatusConflict)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleTOTPRegenerate(w http.ResponseWriter, r *http.Request) {
	userID, code, ok := s.decodeTOTPRequest(w, r)
	if !ok {
		return
	}

	codes, err := s.auth.RegenerateTOTPRecoveryCodes(r.Context(), userID, code.Password, code.Code, SessionIDFromContext(r.Context()))
	if err != nil {
		s.writeTOTPError(w, "regenerate recovery codes", err, http.StatusConflict)
		return
	}
	writeJSON(w, http.StatusOK, recoveryCodesResponse{RecoveryCodes: codes})
}

func (s *Server) handleLoginTOTP(w http.ResponseWriter, r *http.Request) {
	var req mfaLoginRequest
	if !s.decodeJSON(w, r, &req) {
		return
	}

	result, err := s.auth.CompleteMFALogin(r.Context(), req.MFAToken, req.Code, s.sessionMeta(r))
	if err != nil {
		switch {
		case errors.Is(err, auth.ErrTOTPLocked):
			writeJSON(w, http.StatusTooManyRequests, apiError{Message: auth.ErrTOTPLocked.Error()})
		case errors.Is(err, auth.ErrInvalidCode), errors.Is(err, auth.ErrInvalidCredentials),
			errors.Is(err, auth.ErrTOTPNotConfigured):
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

func (s *Server) writeTOTPError(w http.ResponseWriter, action string, err error, conflictStatus int) {
	switch {
	case errors.Is(err, auth.ErrUnauthorized):
		writeJSON(w, http.StatusUnauthorized, apiError{Message: "unauthorized"})
	case errors.Is(err, auth.ErrTOTPLocked):
		writeJSON(w, http.StatusTooManyRequests, apiError{Message: auth.ErrTOTPLocked.Error()})
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
