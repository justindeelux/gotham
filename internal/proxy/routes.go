package proxy

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

type syncRequest struct {
	ServerID string `json:"server_id"`
	Revert   bool   `json:"revert"`
}

type syncResponse struct {
	Diagnostics []Diagnostic `json:"diagnostics,omitempty"`
	Results     []syncResult `json:"results"`
}

type syncResult struct {
	ServerID    string       `json:"server_id"`
	Synced      bool         `json:"synced"`
	Error       string       `json:"error,omitempty"`
	Diagnostics []Diagnostic `json:"diagnostics,omitempty"`
}

func Mount(r chi.Router, teamAuth func(http.Handler) http.Handler, platformAuth func(http.Handler) http.Handler, svc ProxyService, dns DNSProviderService, certs CertificateService, redirects RedirectService, status CertificateStatusService) {
	if svc == nil || !Enabled() {
		return
	}
	h := &handler{svc: svc, dns: dns, certs: certs, redirects: redirects, status: status, logger: slog.Default()}
	r.Group(func(global chi.Router) {
		global.Use(platformAuth)
		global.Post("/v1/proxy/sync", h.sync)
		if certs != nil {
			global.Group(func(panel chi.Router) {
				panel.Use(panelCertificateAuth)
				panel.Get("/v1/proxy/panel-certificates", h.listCertificates)
				panel.Get("/v1/proxy/panel-certificates/{id}", h.getCertificate)
				panel.Patch("/v1/proxy/panel-certificates/{id}", h.updateCertificate)
			})
		}
		if dns != nil {
			global.Post("/v1/proxy/dns-providers", h.createDNSProvider)
			global.Get("/v1/proxy/dns-providers", h.listDNSProviders)
			global.Get("/v1/proxy/dns-providers/{id}", h.getDNSProvider)
			global.Patch("/v1/proxy/dns-providers/{id}", h.updateDNSProvider)
			global.Delete("/v1/proxy/dns-providers/{id}", h.deleteDNSProvider)
		}
	})
	r.Group(func(protected chi.Router) {
		protected.Use(teamAuth)
		if certs != nil {
			protected.Post("/v1/proxy/certificates", h.createCertificate)
			protected.Get("/v1/proxy/certificates", h.listCertificates)
			protected.Get("/v1/proxy/certificates/{id}", h.getCertificate)
			protected.Patch("/v1/proxy/certificates/{id}", h.updateCertificate)
			protected.Delete("/v1/proxy/certificates/{id}", h.deleteCertificate)
		}
		if redirects != nil {
			protected.Post("/v1/proxy/redirects", h.createRedirect)
			protected.Get("/v1/proxy/redirects", h.listRedirects)
			protected.Get("/v1/proxy/redirects/{id}", h.getRedirect)
			protected.Patch("/v1/proxy/redirects/{id}", h.updateRedirect)
			protected.Delete("/v1/proxy/redirects/{id}", h.deleteRedirect)
		}
	})
}

type handler struct {
	svc       ProxyService
	dns       DNSProviderService
	certs     CertificateService
	redirects RedirectService
	status    CertificateStatusService
	logger    *slog.Logger
}

func (h *handler) sync(w http.ResponseWriter, r *http.Request) {
	var req syncRequest
	if !decodeOptionalBody(w, r, &req) {
		return
	}
	serverID, ok := parseOptionalServerID(w, req.ServerID)
	if !ok {
		return
	}

	if req.Revert {
		if serverID == uuid.Nil {
			writeJSON(w, http.StatusBadRequest, apiError{Message: "server_id is required to revert"})
			return
		}
		h.revert(w, r, serverID)
		return
	}

	if serverID != uuid.Nil {
		err := h.svc.SyncServer(r.Context(), serverID)
		var partial *PartialError
		if errors.As(err, &partial) {
			writeJSON(w, http.StatusOK, syncResponse{Results: []syncResult{{
				ServerID:    serverID.String(),
				Synced:      true,
				Diagnostics: partial.Diagnostics,
			}}})
			return
		}
		if err != nil {
			h.writeError(w, "sync", serverID, err)
			return
		}
		writeJSON(w, http.StatusOK, syncResponse{Results: []syncResult{{ServerID: serverID.String(), Synced: true}}})
		return
	}

	results, err := h.svc.SyncAll(r.Context())
	var partial *PartialError
	if err != nil && !errors.As(err, &partial) {
		h.writeServerError(w, "sync", err)
		return
	}
	response := syncResponse{Results: make([]syncResult, 0, len(results))}
	if partial != nil {
		response.Diagnostics = partial.Diagnostics
	}
	failed := false
	for _, result := range results {
		entry := syncResult{
			ServerID:    result.ServerID.String(),
			Synced:      result.Error == "",
			Error:       result.Error,
			Diagnostics: result.Diagnostics,
		}
		if result.Error != "" && !hasDiagnostics(result.Diagnostics) {
			failed = true
		}
		response.Results = append(response.Results, entry)
	}
	status := http.StatusOK
	if failed {
		status = http.StatusBadGateway
	}
	writeJSON(w, status, response)
}

func (h *handler) revert(w http.ResponseWriter, r *http.Request, serverID uuid.UUID) {
	if err := h.svc.RevertServer(r.Context(), serverID); err != nil {
		h.writeError(w, "revert", serverID, err)
		return
	}
	writeJSON(w, http.StatusOK, syncResponse{Results: []syncResult{{ServerID: serverID.String(), Synced: true}}})
}

func hasDiagnostics(diagnostics []Diagnostic) bool {
	return len(diagnostics) > 0
}

func (h *handler) writeError(w http.ResponseWriter, op string, serverID uuid.UUID, err error) {
	response := syncResponse{Results: []syncResult{{ServerID: serverID.String(), Error: err.Error()}}}
	switch {
	case errors.Is(err, ErrValidation):
		writeJSON(w, http.StatusBadRequest, response)
	case errors.Is(err, ErrServerNotFound):
		writeJSON(w, http.StatusNotFound, response)
	case errors.Is(err, ErrVersionNotFound):
		writeJSON(w, http.StatusNotFound, response)
	case errors.Is(err, ErrConflict):
		writeJSON(w, http.StatusConflict, response)
	case errors.Is(err, ErrAgentUnavailable), errors.Is(err, ErrReload), errors.Is(err, ErrHistory):
		writeJSON(w, http.StatusBadGateway, response)
	default:
		h.logger.Error("proxy: "+op, "server_id", serverID.String(), "error", err)
		writeJSON(w, http.StatusInternalServerError, response)
	}
}

func (h *handler) writeServerError(w http.ResponseWriter, op string, err error) {
	switch {
	case errors.Is(err, ErrValidation):
		writeJSON(w, http.StatusBadRequest, apiError{Message: err.Error()})
	case errors.Is(err, ErrServerNotFound):
		writeJSON(w, http.StatusNotFound, apiError{Message: "not found"})
	default:
		h.logger.Error("proxy: "+op, "error", err)
		writeJSON(w, http.StatusInternalServerError, apiError{Message: "internal error"})
	}
}

type apiError struct {
	Message string `json:"message"`
}

func decodeOptionalBody(w http.ResponseWriter, r *http.Request, dst any) bool {
	if r.Body == nil {
		return true
	}
	err := json.NewDecoder(r.Body).Decode(dst)
	if err == nil || errors.Is(err, io.EOF) {
		return true
	}
	writeJSON(w, http.StatusBadRequest, apiError{Message: "invalid request body"})
	return false
}

func parseOptionalServerID(w http.ResponseWriter, raw string) (uuid.UUID, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return uuid.Nil, true
	}
	id, err := uuid.Parse(raw)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, apiError{Message: "invalid server_id"})
		return uuid.Nil, false
	}
	return id, true
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
