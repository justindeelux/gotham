package domainwatch

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const maxBodyBytes = 1 << 20

type handler struct {
	svc *Service
}

// Mount registers the resource-scoped website check routes. Authorization is
// the shared resource/project gate; the owner path is the only target
// selector.
func Mount(r chi.Router, auth func(http.Handler) http.Handler, svc *Service) {
	if svc == nil {
		return
	}
	h := &handler{svc: svc}
	r.Group(func(protected chi.Router) {
		protected.Use(auth)
		protected.Get("/v1/applications/{id}/domains/{domainId}/monitoring", h.get(OwnerApplication))
		protected.Put("/v1/applications/{id}/domains/{domainId}/monitoring", h.put(OwnerApplication))
		protected.Post("/v1/applications/{id}/domains/{domainId}/monitoring/test", h.test(OwnerApplication))
		protected.Get("/v1/services/{id}/domains/{domainId}/monitoring", h.get(OwnerService))
		protected.Put("/v1/services/{id}/domains/{domainId}/monitoring", h.put(OwnerService))
		protected.Post("/v1/services/{id}/domains/{domainId}/monitoring/test", h.test(OwnerService))
	})
}

type errorBody struct {
	Message string `json:"message"`
}

type viewEnvelope struct {
	WebsiteCheck View `json:"website_check"`
}

type testEnvelope struct {
	Result testResult `json:"result"`
}

type testResult struct {
	Outcome    string `json:"outcome"`
	Reason     string `json:"reason,omitempty"`
	StatusCode int    `json:"status_code,omitempty"`
	LatencyMS  int    `json:"latency_ms,omitempty"`
	Detail     string `json:"detail,omitempty"`
}

type settingsRequest struct {
	Enabled        bool   `json:"enabled"`
	Path           string `json:"path"`
	ExpectedMin    int    `json:"expected_min"`
	ExpectedMax    int    `json:"expected_max"`
	TimeoutSeconds int    `json:"timeout_seconds"`
}

func (h *handler) get(kind OwnerKind) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ownerID, domainID, ok := params(w, r)
		if !ok {
			return
		}
		view, err := h.svc.Get(r.Context(), kind, ownerID, domainID)
		if err != nil {
			writeServiceError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, viewEnvelope{WebsiteCheck: view})
	}
}

func (h *handler) put(kind OwnerKind) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ownerID, domainID, ok := params(w, r)
		if !ok {
			return
		}
		var req settingsRequest
		decoder := json.NewDecoder(io.LimitReader(r.Body, maxBodyBytes))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, errorBody{Message: "invalid request body"})
			return
		}
		view, err := h.svc.SaveSettings(r.Context(), kind, ownerID, domainID, SettingsInput{
			Enabled:     req.Enabled,
			Path:        req.Path,
			ExpectedMin: req.ExpectedMin,
			ExpectedMax: req.ExpectedMax,
			Timeout:     req.TimeoutSeconds,
		})
		if err != nil {
			writeServiceError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, viewEnvelope{WebsiteCheck: view})
	}
}

func (h *handler) test(kind OwnerKind) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ownerID, domainID, ok := params(w, r)
		if !ok {
			return
		}
		result, err := h.svc.Test(r.Context(), kind, ownerID, domainID)
		if err != nil {
			writeServiceError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, testEnvelope{Result: testResult{
			Outcome:    result.Outcome,
			Reason:     result.Reason,
			StatusCode: result.StatusCode,
			LatencyMS:  int(result.Latency / time.Millisecond),
			Detail:     result.Detail,
		}})
	}
}

func params(w http.ResponseWriter, r *http.Request) (uuid.UUID, uuid.UUID, bool) {
	appID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeJSON(w, http.StatusNotFound, errorBody{Message: "not found"})
		return uuid.Nil, uuid.Nil, false
	}
	domainID, err := uuid.Parse(chi.URLParam(r, "domainId"))
	if err != nil {
		writeJSON(w, http.StatusNotFound, errorBody{Message: "not found"})
		return uuid.Nil, uuid.Nil, false
	}
	return appID, domainID, true
}

func writeServiceError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		writeJSON(w, http.StatusNotFound, errorBody{Message: "not found"})
	case errors.Is(err, ErrInvalidPath), errors.Is(err, ErrInvalidStatusRange), errors.Is(err, ErrInvalidTimeout):
		writeJSON(w, http.StatusBadRequest, errorBody{Message: err.Error()})
	default:
		writeJSON(w, http.StatusInternalServerError, errorBody{Message: "website check is unavailable"})
	}
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
