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
		hideResource(w)
		return
	}
	ownerID, _ := UserIDFromContext(r.Context())
	if err := s.auth.RemoveMember(r.Context(), ownerID, userID); err != nil {
		s.writeMemberError(w, err)
		return
	}
	s.auditMember(r, "member.remove", userID)
	if s.realtime != nil {
		s.realtime.RevokeUserSubscriptions(userID)
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleResetMember(w http.ResponseWriter, r *http.Request) {
	userID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		hideResource(w)
		return
	}
	if err := s.auth.ResetMember(r.Context(), userID); err != nil {
		s.writeMemberError(w, err)
		return
	}
	s.auditMember(r, "member.reset", userID)
	if s.realtime != nil {
		s.realtime.RevokeUserSubscriptions(userID)
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleSetMemberProjects(w http.ResponseWriter, r *http.Request) {
	userID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		hideResource(w)
		return
	}
	var req struct {
		ProjectIDs *[]uuid.UUID `json:"project_ids"`
	}
	if !s.decodeJSON(w, r, &req) {
		return
	}
	if req.ProjectIDs == nil {
		writeJSON(w, http.StatusBadRequest, apiError{Message: "project_ids is required"})
		return
	}
	ownerID, _ := UserIDFromContext(r.Context())
	account, err := s.auth.SetMemberProjects(r.Context(), ownerID, userID, *req.ProjectIDs)
	if err != nil {
		s.writeMemberError(w, err)
		return
	}
	s.auditMember(r, "member.projects", userID, "projects", len(*req.ProjectIDs))
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
		return
	}
	ownerID, _ := UserIDFromContext(r.Context())
	invite, token, err := s.auth.CreateInvite(r.Context(), ownerID, req.Email, req.ProjectIDs)
	if err != nil {
		switch {
		case errors.Is(err, auth.ErrValidation):
			writeJSON(w, http.StatusBadRequest, apiError{Message: err.Error()})
		case errors.Is(err, auth.ErrEmailTaken):
			writeJSON(w, http.StatusConflict, apiError{Message: "email already registered"})
		case errors.Is(err, store.ErrUnknownProject):
			writeJSON(w, http.StatusBadRequest, apiError{Message: "unknown project"})
		default:
			s.logger.Error("members: create invite", "error", err)
			writeJSON(w, http.StatusInternalServerError, apiError{Message: "internal error"})
		}
		return
	}
	s.auditMember(r, "invite.create", invite.ID, "email", invite.Email, "projects", len(invite.Projects))
	writeJSON(w, http.StatusCreated, map[string]any{"invite": invite, "token": token})
}

func (s *Server) handleRevokeInvite(w http.ResponseWriter, r *http.Request) {
	inviteID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		hideResource(w)
		return
	}
	if err := s.auth.RevokeInvite(r.Context(), inviteID); err != nil {
		s.writeMemberError(w, err)
		return
	}
	s.auditMember(r, "invite.revoke", inviteID)
	w.WriteHeader(http.StatusNoContent)
}

// auditMember records one successful owner mutation: who did what to which
// account or invite. Tokens and credentials never reach this line.
func (s *Server) auditMember(r *http.Request, action string, target uuid.UUID, extra ...any) {
	actorID, _ := UserIDFromContext(r.Context())
	fields := []any{"actor_id", actorID.String(), "action", action, "target_id", target.String()}
	s.logger.Info("members: audit", append(fields, extra...)...)
}

func (s *Server) writeMemberError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, pgx.ErrNoRows), errors.Is(err, auth.ErrNotFound):
		hideResource(w)
	case errors.Is(err, store.ErrOwnerProtected):
		writeJSON(w, http.StatusForbidden, apiError{Message: "the owner account cannot be changed here"})
	case errors.Is(err, store.ErrUnknownProject):
		writeJSON(w, http.StatusBadRequest, apiError{Message: "unknown project"})
	default:
		s.logger.Error("members: mutation", "error", err)
		writeJSON(w, http.StatusInternalServerError, apiError{Message: "internal error"})
	}
}
