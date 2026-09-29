package teams

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// maxBodyBytes bounds a team request body.
const maxBodyBytes = 1 << 20

// UserIDFunc resolves the authenticated user from the request context. The
// server passes its own accessor, so this package never imports the HTTP server
// (it mirrors services.UserIDFunc for the same reason).
type UserIDFunc func(ctx context.Context) (uuid.UUID, bool)

// request bodies.
type (
	createTeamRequest struct {
		Name string `json:"name"`
	}
	renameTeamRequest struct {
		Name string `json:"name"`
	}
	updateRoleRequest struct {
		Role string `json:"role"`
	}
	createInviteRequest struct {
		Email string `json:"email"`
		Role  string `json:"role"`
	}
	acceptInviteRequest struct {
		Token string `json:"token"`
	}
)

// teamResponse is the wire representation of a team. role is the requesting
// user's role in it.
type teamResponse struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	IsPersonal bool      `json:"is_personal"`
	Role       Role      `json:"role"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// memberResponse is the wire representation of one membership.
type memberResponse struct {
	UserID    string    `json:"user_id"`
	Email     string    `json:"email"`
	Role      Role      `json:"role"`
	CreatedAt time.Time `json:"created_at"`
}

// inviteResponse is the wire representation of one invite. The token is never
// part of it.
type inviteResponse struct {
	ID         string     `json:"id"`
	TeamID     string     `json:"team_id"`
	Email      string     `json:"email"`
	Role       Role       `json:"role"`
	ExpiresAt  time.Time  `json:"expires_at"`
	AcceptedAt *time.Time `json:"accepted_at,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
}

// createdInviteResponse carries the raw token exactly once, when the invite is
// created. accept_url names the endpoint the token is posted to (in the request
// body, never a URL path, so access logs never see it); email delivery of the
// link is a separate work package (BE-8.3).
type createdInviteResponse struct {
	inviteResponse
	Token     string `json:"token"`
	AcceptURL string `json:"accept_url"`
}

// envelope types.
type (
	teamEnvelope struct {
		Team teamResponse `json:"team"`
	}
	teamListEnvelope struct {
		Teams []teamResponse `json:"teams"`
	}
	memberListEnvelope struct {
		Members []memberResponse `json:"members"`
	}
	inviteListEnvelope struct {
		Invites []inviteResponse `json:"invites"`
	}
	singleInviteEnvelope struct {
		Invite createdInviteResponse `json:"invite"`
	}
)

// errorBody is the JSON body returned for failures.
type errorBody struct {
	Message string `json:"message"`
}

// handler serves the team routes for one TeamService.
type handler struct {
	svc    TeamService
	userID UserIDFunc
	logger *slog.Logger
}

// Mount registers the authenticated team-management endpoints under /api:
//
//	POST   /v1/teams
//	GET    /v1/teams
//	GET    /v1/teams/{id}
//	PATCH  /v1/teams/{id}
//	DELETE /v1/teams/{id}
//	GET    /v1/teams/{id}/members
//	PATCH  /v1/teams/{id}/members/{userID}
//	DELETE /v1/teams/{id}/members/{userID}
//	GET    /v1/teams/{id}/invites
//	POST   /v1/teams/{id}/invites
//	DELETE /v1/teams/{id}/invites/{inviteID}
//	POST   /v1/invites/accept
//
// auth wraps the group. A nil svc or FEATURE_TEAMS=false mounts nothing, so the
// control plane can call Mount unconditionally. The routes carry no admin scope:
// they are the standard per-team role surface, and every handler's authorization
// comes from the caller's role in the team the path names.
func Mount(r chi.Router, auth func(http.Handler) http.Handler, userID UserIDFunc, svc TeamService) {
	if svc == nil || !Enabled() {
		return
	}
	h := &handler{svc: svc, userID: userID, logger: slog.Default()}
	r.Group(func(protected chi.Router) {
		protected.Use(auth)
		protected.Post("/v1/teams", h.create)
		protected.Get("/v1/teams", h.list)
		protected.Get("/v1/teams/{id}", h.get)
		protected.Patch("/v1/teams/{id}", h.rename)
		protected.Delete("/v1/teams/{id}", h.delete)
		protected.Get("/v1/teams/{id}/members", h.members)
		protected.Patch("/v1/teams/{id}/members/{userID}", h.updateMember)
		protected.Delete("/v1/teams/{id}/members/{userID}", h.removeMember)
		protected.Get("/v1/teams/{id}/invites", h.invites)
		protected.Post("/v1/teams/{id}/invites", h.createInvite)
		protected.Delete("/v1/teams/{id}/invites/{inviteID}", h.revokeInvite)
		protected.Post("/v1/invites/accept", h.acceptInvite)
	})
}

// create serves POST /v1/teams: 201 with the stored team; the caller is owner.
func (h *handler) create(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.currentUser(w, r)
	if !ok {
		return
	}
	var req createTeamRequest
	if !decodeBody(w, r, &req) {
		return
	}
	team, err := h.svc.Create(r.Context(), userID, req.Name)
	if err != nil {
		h.writeError(w, err)
		return
	}
	team.Role = RoleOwner
	writeJSON(w, http.StatusCreated, teamEnvelope{Team: newTeamResponse(team)})
}

// list serves GET /v1/teams.
func (h *handler) list(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.currentUser(w, r)
	if !ok {
		return
	}
	all, err := h.svc.List(r.Context(), userID)
	if err != nil {
		h.writeError(w, err)
		return
	}
	response := teamListEnvelope{Teams: make([]teamResponse, 0, len(all))}
	for _, team := range all {
		response.Teams = append(response.Teams, newTeamResponse(team))
	}
	writeJSON(w, http.StatusOK, response)
}

// get serves GET /v1/teams/{id}: any member.
func (h *handler) get(w http.ResponseWriter, r *http.Request) {
	userID, teamID, ok := h.teamParams(w, r)
	if !ok {
		return
	}
	team, err := h.svc.Get(r.Context(), userID, teamID)
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, teamEnvelope{Team: newTeamResponse(team)})
}

// rename serves PATCH /v1/teams/{id}: owner/admin.
func (h *handler) rename(w http.ResponseWriter, r *http.Request) {
	userID, teamID, ok := h.teamParams(w, r)
	if !ok {
		return
	}
	var req renameTeamRequest
	if !decodeBody(w, r, &req) {
		return
	}
	team, err := h.svc.Rename(r.Context(), userID, teamID, req.Name)
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, teamEnvelope{Team: newTeamResponse(team)})
}

// delete serves DELETE /v1/teams/{id}: owner; personal teams are refused.
func (h *handler) delete(w http.ResponseWriter, r *http.Request) {
	userID, teamID, ok := h.teamParams(w, r)
	if !ok {
		return
	}
	if err := h.svc.Delete(r.Context(), userID, teamID); err != nil {
		h.writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// members serves GET /v1/teams/{id}/members: any member.
func (h *handler) members(w http.ResponseWriter, r *http.Request) {
	userID, teamID, ok := h.teamParams(w, r)
	if !ok {
		return
	}
	members, err := h.svc.Members(r.Context(), userID, teamID)
	if err != nil {
		h.writeError(w, err)
		return
	}
	response := memberListEnvelope{Members: make([]memberResponse, 0, len(members))}
	for _, member := range members {
		response.Members = append(response.Members, newMemberResponse(member))
	}
	writeJSON(w, http.StatusOK, response)
}

// updateMember serves PATCH /v1/teams/{id}/members/{userID}: owner/admin.
func (h *handler) updateMember(w http.ResponseWriter, r *http.Request) {
	userID, teamID, ok := h.teamParams(w, r)
	if !ok {
		return
	}
	memberID, ok := pathUUID(w, r, "userID", "user id")
	if !ok {
		return
	}
	var req updateRoleRequest
	if !decodeBody(w, r, &req) {
		return
	}
	role, err := ParseRole(strings.TrimSpace(req.Role))
	if err != nil {
		h.writeError(w, err)
		return
	}
	member, err := h.svc.SetMemberRole(r.Context(), userID, teamID, memberID, role)
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, struct {
		Member memberResponse `json:"member"`
	}{Member: newMemberResponse(member)})
}

// removeMember serves DELETE /v1/teams/{id}/members/{userID}: owner/admin, or
// the member themselves (leaving the team).
func (h *handler) removeMember(w http.ResponseWriter, r *http.Request) {
	userID, teamID, ok := h.teamParams(w, r)
	if !ok {
		return
	}
	memberID, ok := pathUUID(w, r, "userID", "user id")
	if !ok {
		return
	}
	if err := h.svc.RemoveMember(r.Context(), userID, teamID, memberID); err != nil {
		h.writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// invites serves GET /v1/teams/{id}/invites: owner/admin.
func (h *handler) invites(w http.ResponseWriter, r *http.Request) {
	userID, teamID, ok := h.teamParams(w, r)
	if !ok {
		return
	}
	invites, err := h.svc.Invites(r.Context(), userID, teamID)
	if err != nil {
		h.writeError(w, err)
		return
	}
	response := inviteListEnvelope{Invites: make([]inviteResponse, 0, len(invites))}
	for _, invite := range invites {
		response.Invites = append(response.Invites, newInviteResponse(invite))
	}
	writeJSON(w, http.StatusOK, response)
}

// createInvite serves POST /v1/teams/{id}/invites: owner/admin. The response
// carries the raw token once; email delivery is BE-8.3.
func (h *handler) createInvite(w http.ResponseWriter, r *http.Request) {
	userID, teamID, ok := h.teamParams(w, r)
	if !ok {
		return
	}
	var req createInviteRequest
	if !decodeBody(w, r, &req) {
		return
	}
	role, err := ParseRole(strings.TrimSpace(req.Role))
	if err != nil {
		h.writeError(w, err)
		return
	}
	invite, token, err := h.svc.Invite(r.Context(), userID, teamID, req.Email, role)
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, singleInviteEnvelope{Invite: createdInviteResponse{
		inviteResponse: newInviteResponse(invite),
		Token:          token,
		AcceptURL:      acceptURL(),
	}})
}

// revokeInvite serves DELETE /v1/teams/{id}/invites/{inviteID}: owner/admin.
func (h *handler) revokeInvite(w http.ResponseWriter, r *http.Request) {
	userID, teamID, ok := h.teamParams(w, r)
	if !ok {
		return
	}
	inviteID, ok := pathUUID(w, r, "inviteID", "invite id")
	if !ok {
		return
	}
	if err := h.svc.RevokeInvite(r.Context(), userID, teamID, inviteID); err != nil {
		h.writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// acceptInvite serves POST /v1/invites/accept: the authenticated account whose
// email the invite names posts the raw token in the body. The token is never a
// URL segment, so it cannot leak through access logs or referrers.
func (h *handler) acceptInvite(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.currentUser(w, r)
	if !ok {
		return
	}
	var req acceptInviteRequest
	if !decodeBody(w, r, &req) {
		return
	}
	team, err := h.svc.Accept(r.Context(), userID, req.Token)
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, teamEnvelope{Team: newTeamResponse(team)})
}

// currentUser resolves the authenticated user, answering 401 when absent.
func (h *handler) currentUser(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	if h.userID == nil {
		writeJSON(w, http.StatusUnauthorized, errorBody{Message: "unauthorized"})
		return uuid.Nil, false
	}
	userID, ok := h.userID(r.Context())
	if !ok || userID == uuid.Nil {
		writeJSON(w, http.StatusUnauthorized, errorBody{Message: "unauthorized"})
		return uuid.Nil, false
	}
	return userID, true
}

// teamParams resolves the authenticated user and the {id} path parameter.
func (h *handler) teamParams(w http.ResponseWriter, r *http.Request) (uuid.UUID, uuid.UUID, bool) {
	userID, ok := h.currentUser(w, r)
	if !ok {
		return uuid.Nil, uuid.Nil, false
	}
	teamID, ok := pathUUID(w, r, "id", "team id")
	if !ok {
		return uuid.Nil, uuid.Nil, false
	}
	return userID, teamID, true
}

// pathUUID parses one UUID path parameter, answering 400 on a bad value.
func pathUUID(w http.ResponseWriter, r *http.Request, name, label string) (uuid.UUID, bool) {
	id, err := uuid.Parse(chi.URLParam(r, name))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody{Message: "invalid " + label})
		return uuid.Nil, false
	}
	return id, true
}

// writeError maps a teams sentinel to its HTTP response.
func (h *handler) writeError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrNotFound):
		writeJSON(w, http.StatusNotFound, errorBody{Message: "not found"})
	case errors.Is(err, ErrForbidden):
		writeJSON(w, http.StatusForbidden, errorBody{Message: err.Error()})
	case errors.Is(err, ErrInviteExpired):
		writeJSON(w, http.StatusGone, errorBody{Message: err.Error()})
	case errors.Is(err, ErrInviteUsed), errors.Is(err, ErrConflict):
		writeJSON(w, http.StatusConflict, errorBody{Message: err.Error()})
	case errors.Is(err, ErrLastOwner), errors.Is(err, ErrPersonalTeam),
		errors.Is(err, ErrPersonalOwner), errors.Is(err, ErrTeamNotEmpty):
		writeJSON(w, http.StatusConflict, errorBody{Message: err.Error()})
	case errors.Is(err, ErrValidation):
		writeJSON(w, http.StatusBadRequest, errorBody{Message: err.Error()})
	default:
		h.logger.Error("teams: request failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, errorBody{Message: "internal error"})
	}
}

// acceptURL is the relative API path a client posts the token to (in the body).
// The frontend builds the absolute link from it; email delivery is BE-8.3.
func acceptURL() string {
	return "/v1/invites/accept"
}

// newTeamResponse maps a team to its wire representation.
func newTeamResponse(team Team) teamResponse {
	return teamResponse{
		ID:         team.ID.String(),
		Name:       team.Name,
		IsPersonal: team.IsPersonal,
		Role:       team.Role,
		CreatedAt:  team.CreatedAt,
		UpdatedAt:  team.UpdatedAt,
	}
}

// newMemberResponse maps a membership to its wire representation.
func newMemberResponse(member Member) memberResponse {
	return memberResponse{
		UserID:    member.UserID.String(),
		Email:     member.Email,
		Role:      member.Role,
		CreatedAt: member.CreatedAt,
	}
}

// newInviteResponse maps an invite to its wire representation (token-free).
func newInviteResponse(invite Invite) inviteResponse {
	return inviteResponse{
		ID:         invite.ID.String(),
		TeamID:     invite.TeamID.String(),
		Email:      invite.Email,
		Role:       invite.Role,
		ExpiresAt:  invite.ExpiresAt,
		AcceptedAt: invite.AcceptedAt,
		CreatedAt:  invite.CreatedAt,
	}
}

// decodeBody decodes a required JSON body into dst. An empty or malformed body
// answers 400.
func decodeBody(w http.ResponseWriter, r *http.Request, dst any) bool {
	body, err := io.ReadAll(io.LimitReader(r.Body, maxBodyBytes))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody{Message: "invalid request body"})
		return false
	}
	if len(strings.TrimSpace(string(body))) == 0 {
		writeJSON(w, http.StatusBadRequest, errorBody{Message: "request body is required"})
		return false
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dst); err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody{Message: "invalid request body"})
		return false
	}
	return true
}

// writeJSON serialises payload with the given HTTP status.
func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
