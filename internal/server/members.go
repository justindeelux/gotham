package server

import (
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/justindeelux/gotham/internal/auth"
	"github.com/justindeelux/gotham/internal/store"
)

func (s *Server) mountMemberRoutes(api chi.Router) {
	if s.persistence == nil {
		return
	}
	api.Group(func(r chi.Router) {
		r.Use(s.RequireAuth, s.RequireOwner)
		r.Get("/v1/users", s.handleListUsers)
		r.Delete("/v1/users/{id}", s.handleRemoveMember)
		r.Post("/v1/users/{id}/reset", s.handleResetMember)
		r.Put("/v1/users/{id}/projects", s.handleSetMemberProjects)
		r.Get("/v1/invites", s.handleListInvites)
		r.Post("/v1/invites", s.handleCreateInvite)
		r.Delete("/v1/invites/{id}", s.handleRevokeInvite)
	})
}

func (s *Server) handleListUsers(w http.ResponseWriter, r *http.Request) {
	users, err := s.auth.ListUsers(r.Context())
	if err != nil {
		s.logger.Error("members: list users", "error", err)
		writeJSON(w, http.StatusInternalServerError, apiError{Message: "internal error"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"users": users})
}

func (s *Server) handleRemoveMember(w http.ResponseWriter, r *http.Request) {
	userID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		s.auditMember(r, "member.remove", uuid.Nil, "refused")
		hideResource(w)
		return
	}
	ownerID, _ := UserIDFromContext(r.Context())
	if err := s.auth.RemoveMember(r.Context(), ownerID, userID); err != nil {
		s.writeMemberError(w, r, "member.remove", userID, err)
		return
	}
	s.auditMember(r, "member.remove", userID, "ok")
	if s.realtime != nil {
		s.realtime.RevokeUserSubscriptions(userID)
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleResetMember(w http.ResponseWriter, r *http.Request) {
	userID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		s.auditMember(r, "member.reset", uuid.Nil, "refused")
		hideResource(w)
		return
	}
	if err := s.auth.ResetMember(r.Context(), userID); err != nil {
		s.writeMemberError(w, r, "member.reset", userID, err)
		return
	}
	s.auditMember(r, "member.reset", userID, "ok")
	if s.realtime != nil {
		s.realtime.RevokeUserSubscriptions(userID)
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleSetMemberProjects(w http.ResponseWriter, r *http.Request) {
	userID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		s.auditMember(r, "member.projects", uuid.Nil, "refused")
		hideResource(w)
		return
	}
	var req struct {
		ProjectIDs *[]uuid.UUID `json:"project_ids"`
	}
	if !s.decodeJSON(w, r, &req) {
		s.auditMember(r, "member.projects", userID, "refused")
		return
	}
	if req.ProjectIDs == nil {
		s.auditMember(r, "member.projects", userID, "refused")
		writeJSON(w, http.StatusBadRequest, apiError{Message: "project_ids is required"})
		return
	}
	ownerID, _ := UserIDFromContext(r.Context())
	account, err := s.auth.SetMemberProjects(r.Context(), ownerID, userID, *req.ProjectIDs)
	if err != nil {
		s.writeMemberError(w, r, "member.projects", userID, err)
		return
	}
	s.auditMember(r, "member.projects", userID, "ok", "projects", len(*req.ProjectIDs))
	if s.realtime != nil {
		s.realtime.InvalidateAuthorization()
	}
	writeJSON(w, http.StatusOK, map[string]any{"user": account})
}

func (s *Server) handleListInvites(w http.ResponseWriter, r *http.Request) {
	invites, err := s.auth.ListInvites(r.Context())
	if err != nil {
		s.logger.Error("members: list invites", "error", err)
		writeJSON(w, http.StatusInternalServerError, apiError{Message: "internal error"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"invites": invites})
}

func (s *Server) handleCreateInvite(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email      string      `json:"email"`
		ProjectIDs []uuid.UUID `json:"project_ids"`
	}
	if !s.decodeJSON(w, r, &req) {
		s.auditMember(r, "invite.create", uuid.Nil, "refused")
		return
	}
	ownerID, _ := UserIDFromContext(r.Context())
	invite, token, err := s.auth.CreateInvite(r.Context(), ownerID, req.Email, req.ProjectIDs)
	if err != nil {
		s.writeMemberError(w, r, "invite.create", uuid.Nil, err)
		return
	}
	s.auditMember(r, "invite.create", invite.ID, "ok", "projects", len(invite.Projects))
	writeJSON(w, http.StatusCreated, map[string]any{"invite": invite, "token": token})
}

func (s *Server) handleRevokeInvite(w http.ResponseWriter, r *http.Request) {
	inviteID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		s.auditMember(r, "invite.revoke", uuid.Nil, "refused")
		hideResource(w)
		return
	}
	if err := s.auth.RevokeInvite(r.Context(), inviteID); err != nil {
		s.writeMemberError(w, r, "invite.revoke", inviteID, err)
		return
	}
	s.auditMember(r, "invite.revoke", inviteID, "ok")
	w.WriteHeader(http.StatusNoContent)
}

// auditMember records one owner mutation attempt: who tried what on which
// account or invite, and whether it succeeded. Tokens, credentials and invitee
// addresses never reach this line.
func (s *Server) auditMember(r *http.Request, action string, target uuid.UUID, outcome string, extra ...any) {
	actorID, _ := UserIDFromContext(r.Context())
	fields := []any{
		"actor_id", actorID.String(),
		"action", action,
		"target_id", target.String(),
		"outcome", outcome,
	}
	s.logger.Info("members: audit", append(fields, extra...)...)
}

func memberOutcome(err error) string {
	switch {
	case errors.Is(err, pgx.ErrNoRows),
		errors.Is(err, auth.ErrNotFound),
		errors.Is(err, auth.ErrValidation),
		errors.Is(err, auth.ErrEmailTaken),
		errors.Is(err, store.ErrOwnerProtected),
		errors.Is(err, store.ErrUnknownProject):
		return "refused"
	default:
		return "error"
	}
}

func (s *Server) writeMemberError(w http.ResponseWriter, r *http.Request, action string, target uuid.UUID, err error) {
	s.auditMember(r, action, target, memberOutcome(err))
	switch {
	case errors.Is(err, pgx.ErrNoRows), errors.Is(err, auth.ErrNotFound):
		hideResource(w)
	case errors.Is(err, store.ErrOwnerProtected):
		writeJSON(w, http.StatusForbidden, apiError{Message: "the owner account cannot be changed here"})
	case errors.Is(err, auth.ErrValidation):
		writeJSON(w, http.StatusBadRequest, apiError{Message: err.Error()})
	case errors.Is(err, auth.ErrEmailTaken):
		writeJSON(w, http.StatusConflict, apiError{Message: "email already registered"})
	case errors.Is(err, store.ErrUnknownProject):
		writeJSON(w, http.StatusBadRequest, apiError{Message: "unknown project"})
	default:
		s.logger.Error("members: mutation", "action", action, "target_id", target.String(), "error", err)
		writeJSON(w, http.StatusInternalServerError, apiError{Message: "internal error"})
	}
}
