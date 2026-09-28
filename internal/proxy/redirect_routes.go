package proxy

import (
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
)

// redirectResponse is the wire representation of one domain→domain redirect
// rule. Code is the operator's intent (301 permanent, 302 temporary); Traefik
// special-cases only GET, so GET answers 301/302 while HEAD and every other
// method answer 308/307.
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

// redirectEnvelope wraps a single redirect rule.
type redirectEnvelope struct {
	Redirect redirectResponse `json:"redirect"`
}

// redirectListEnvelope wraps a redirect list.
type redirectListEnvelope struct {
	Redirects []redirectResponse `json:"redirects"`
}

// createRedirectRequest is the body of POST /v1/proxy/redirects.
type createRedirectRequest struct {
	ApplicationID string `json:"application_id"`
	SourceDomain  string `json:"source_domain"`
	TargetDomain  string `json:"target_domain"`
	// Code defaults to 301 when omitted.
	Code *int `json:"code,omitempty"`
	// PreservePath defaults to true when omitted.
	PreservePath *bool `json:"preserve_path,omitempty"`
	// Enabled defaults to true when omitted.
	Enabled *bool `json:"enabled,omitempty"`
}

// updateRedirectRequest is the body of PATCH /v1/proxy/redirects/{id}.
type updateRedirectRequest struct {
	SourceDomain *string `json:"source_domain,omitempty"`
	TargetDomain *string `json:"target_domain,omitempty"`
	Code         *int    `json:"code,omitempty"`
	PreservePath *bool   `json:"preserve_path,omitempty"`
	Enabled      *bool   `json:"enabled,omitempty"`
}

// createRedirect serves POST /v1/proxy/redirects.
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

// listRedirects serves GET /v1/proxy/redirects, optionally filtered by
// ?application_id=.
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

// getRedirect serves GET /v1/proxy/redirects/{id}.
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

// updateRedirect serves PATCH /v1/proxy/redirects/{id}.
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

// deleteRedirect serves DELETE /v1/proxy/redirects/{id}.
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

// newRedirectResponse maps a redirect rule onto its wire shape.
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
