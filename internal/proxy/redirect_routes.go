package proxy

import (
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
)

type redirectResponse struct {
	ID            string    `json:"id"`
	ApplicationID string    `json:"application_id"`
	SourceDomain  string    `json:"source_domain"`
	TargetDomain  string    `json:"target_domain"`
	Code          int       `json:"code"`
	PreservePath  bool      `json:"preserve_path"`
	Enabled       bool      `json:"enabled"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

type redirectEnvelope struct {
	Redirect redirectResponse `json:"redirect"`
}

type redirectListEnvelope struct {
	Redirects []redirectResponse `json:"redirects"`
}

type createRedirectRequest struct {
	ApplicationID string `json:"application_id"`
	SourceDomain  string `json:"source_domain"`
	TargetDomain  string `json:"target_domain"`
	Code          *int   `json:"code,omitempty"`
	PreservePath  *bool  `json:"preserve_path,omitempty"`
	Enabled       *bool  `json:"enabled,omitempty"`
}

type updateRedirectRequest struct {
	SourceDomain *string `json:"source_domain,omitempty"`
	TargetDomain *string `json:"target_domain,omitempty"`
	Code         *int    `json:"code,omitempty"`
	PreservePath *bool   `json:"preserve_path,omitempty"`
	Enabled      *bool   `json:"enabled,omitempty"`
}

func (h *handler) createRedirect(w http.ResponseWriter, r *http.Request) {
	var req createRedirectRequest
	if !decodeRequiredBody(w, r, &req) {
		return
	}
	applicationID, err := uuid.Parse(strings.TrimSpace(req.ApplicationID))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, apiError{Message: "invalid application_id"})
		return
	}
	in := CreateRedirectInput{
		ApplicationID: applicationID,
		SourceDomain:  req.SourceDomain,
		TargetDomain:  req.TargetDomain,
		PreservePath:  req.PreservePath,
		Enabled:       req.Enabled,
	}
	if req.Code != nil {
		in.Code = *req.Code
	}
	redirect, err := h.redirects.CreateRedirect(r.Context(), in)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, redirectEnvelope{Redirect: newRedirectResponse(redirect)})
}

func (h *handler) listRedirects(w http.ResponseWriter, r *http.Request) {
	applicationID, ok := parseOptionalUUID(w, r.URL.Query().Get("application_id"), "invalid application_id")
	if !ok {
		return
	}
	redirects, err := h.redirects.ListRedirects(r.Context(), applicationID)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	response := make([]redirectResponse, 0, len(redirects))
	for _, redirect := range redirects {
		response = append(response, newRedirectResponse(redirect))
	}
	writeJSON(w, http.StatusOK, redirectListEnvelope{Redirects: response})
}

func (h *handler) getRedirect(w http.ResponseWriter, r *http.Request) {
	id, ok := sslPathID(w, r, "redirect")
	if !ok {
		return
	}
	redirect, err := h.redirects.GetRedirect(r.Context(), id)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, redirectEnvelope{Redirect: newRedirectResponse(redirect)})
}

func (h *handler) updateRedirect(w http.ResponseWriter, r *http.Request) {
	id, ok := sslPathID(w, r, "redirect")
	if !ok {
		return
	}
	var req updateRedirectRequest
	if !decodeRequiredBody(w, r, &req) {
		return
	}
	redirect, err := h.redirects.UpdateRedirect(r.Context(), id, UpdateRedirectInput(req))
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, redirectEnvelope{Redirect: newRedirectResponse(redirect)})
}

func (h *handler) deleteRedirect(w http.ResponseWriter, r *http.Request) {
	id, ok := sslPathID(w, r, "redirect")
	if !ok {
		return
	}
	if err := h.redirects.DeleteRedirect(r.Context(), id); err != nil {
		h.writeServiceError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func newRedirectResponse(redirect DomainRedirect) redirectResponse {
	return redirectResponse{
		ID:            redirect.ID.String(),
		ApplicationID: redirect.ApplicationID.String(),
		SourceDomain:  redirect.SourceDomain,
		TargetDomain:  redirect.TargetDomain,
		Code:          redirect.Code,
		PreservePath:  redirect.PreservePath,
		Enabled:       redirect.Enabled,
		CreatedAt:     redirect.CreatedAt,
		UpdatedAt:     redirect.UpdatedAt,
	}
}
