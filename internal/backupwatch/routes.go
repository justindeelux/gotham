package backupwatch

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

type handler struct {
	svc *Service
}

// Mount registers the per-resource protection summary. Authorization is the
// shared resource/project gate, so a caller only sees resources in granted
// projects.
func Mount(r chi.Router, auth func(http.Handler) http.Handler, svc *Service) {
	if svc == nil {
		return
	}
	h := &handler{svc: svc}
	r.Group(func(protected chi.Router) {
		protected.Use(auth)
		protected.Get("/v1/databases/{id}/backup-protection", h.database)
		protected.Get("/v1/applications/{id}/backup-protection", h.application)
		protected.Get("/v1/services/{id}/backup-protection", h.service)
	})
}

type errorBody struct {
	Message string `json:"message"`
}

type summaryEnvelope struct {
	Protection Summary `json:"protection"`
}

func (h *handler) database(w http.ResponseWriter, r *http.Request) {
	h.get(w, r, ResourceDatabase)
}

func (h *handler) application(w http.ResponseWriter, r *http.Request) {
	h.get(w, r, ResourceApplication)
}

func (h *handler) service(w http.ResponseWriter, r *http.Request) {
	h.get(w, r, ResourceService)
}

func (h *handler) get(w http.ResponseWriter, r *http.Request, resourceType string) {
	resourceID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeJSON(w, http.StatusNotFound, errorBody{Message: "not found"})
		return
	}
	summary, err := h.svc.Summary(r.Context(), resourceType, resourceID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			writeJSON(w, http.StatusNotFound, errorBody{Message: "not found"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, errorBody{Message: "backup protection is unavailable"})
		return
	}
	writeJSON(w, http.StatusOK, summaryEnvelope{Protection: summary})
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
