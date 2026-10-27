package incidents

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

const maxBodyBytes = 1 << 20

type UserIDFunc func(ctx context.Context) (uuid.UUID, bool)

type handler struct {
	svc    *Service
	userID UserIDFunc
	logger *slog.Logger
}

type errorBody struct {
	Message string `json:"message"`
}

func Mount(r chi.Router, auth func(http.Handler) http.Handler, userID UserIDFunc, svc *Service) {
	if svc == nil {
		return
	}
	h := &handler{svc: svc, userID: userID, logger: slog.Default()}
	r.Group(func(protected chi.Router) {
		protected.Use(auth)
		protected.Get("/v1/incidents", h.list)
		protected.Get("/v1/incidents/{id}", h.get)
		protected.Get("/v1/incidents/{id}/history", h.history)
		protected.Get("/v1/incidents/{id}/deliveries", h.deliveries)
		protected.Post("/v1/incidents/{id}/acknowledge", h.acknowledge)
		protected.Post("/v1/incidents/{id}/snooze", h.snooze)
	})
}

type listEnvelope struct {
	Incidents  []IncidentView `json:"incidents"`
	NextCursor string         `json:"next_cursor,omitempty"`
}

type incidentEnvelope struct {
	Incident IncidentView `json:"incident"`
}

type historyEnvelope struct {
	Events     []EventView `json:"events"`
	NextCursor string      `json:"next_cursor,omitempty"`
}

type deliveryEnvelope struct {
	Deliveries []DeliveryView `json:"deliveries"`
}

func (h *handler) list(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.currentUser(w, r)
	if !ok {
		return
	}
	filter, err := parseListParams(r)
	if err != nil {
		h.writeError(w, err)
		return
	}
	page, err := h.svc.List(r.Context(), userID, filter)
	if err != nil {
		h.writeError(w, err)
		return
	}
	if page.Incidents == nil {
		page.Incidents = []IncidentView{}
	}
	writeJSON(w, http.StatusOK, listEnvelope(page))
}

func (h *handler) get(w http.ResponseWriter, r *http.Request) {
	userID, incidentID, ok := h.params(w, r)
	if !ok {
		return
	}
	incident, err := h.svc.Get(r.Context(), userID, incidentID)
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, incidentEnvelope{Incident: incident})
}

func (h *handler) history(w http.ResponseWriter, r *http.Request) {
	userID, incidentID, ok := h.params(w, r)
	if !ok {
		return
	}
	limit, err := parseLimit(r.URL.Query().Get("limit"))
	if err != nil {
		h.writeError(w, err)
		return
	}
	cursor, err := decodeCursor(r.URL.Query().Get("cursor"))
	if err != nil {
		h.writeError(w, err)
		return
	}
	events, err := h.svc.History(r.Context(), userID, incidentID, HistoryParams{Cursor: cursor, Limit: limit + 1})
	if err != nil {
		h.writeError(w, err)
		return
	}
	next := ""
	if len(events) > limit {
		events = events[:limit]
		last := events[len(events)-1]
		next = encodeCursor(Cursor{At: last.CreatedAt, ID: last.ID})
	}
	views := make([]EventView, 0, len(events))
	for _, event := range events {
		views = append(views, eventView(event))
	}
	writeJSON(w, http.StatusOK, historyEnvelope{Events: views, NextCursor: next})
}

func (h *handler) deliveries(w http.ResponseWriter, r *http.Request) {
	userID, incidentID, ok := h.params(w, r)
	if !ok {
		return
	}
	intents, err := h.svc.Deliveries(r.Context(), userID, incidentID)
	if err != nil {
		h.writeError(w, err)
		return
	}
	views := make([]DeliveryView, 0, len(intents))
	for _, intent := range intents {
		views = append(views, deliveryView(intent))
	}
	writeJSON(w, http.StatusOK, deliveryEnvelope{Deliveries: views})
}

func (h *handler) acknowledge(w http.ResponseWriter, r *http.Request) {
	userID, incidentID, ok := h.params(w, r)
	if !ok {
		return
	}
	incident, err := h.svc.Acknowledge(r.Context(), userID, incidentID)
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, incidentEnvelope{Incident: view(incident, nil, true)})
}

func (h *handler) snooze(w http.ResponseWriter, r *http.Request) {
	userID, incidentID, ok := h.params(w, r)
	if !ok {
		return
	}
	var req struct {
		Until string `json:"until"`
	}
	if !decodeBody(w, r, &req) {
		return
	}
	until, err := time.Parse(time.RFC3339, strings.TrimSpace(req.Until))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody{Message: "invalid until"})
		return
	}
	incident, err := h.svc.Snooze(r.Context(), userID, incidentID, until)
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, incidentEnvelope{Incident: view(incident, nil, true)})
}

func parseListParams(r *http.Request) (ListParams, error) {
	query := r.URL.Query()
	limit, err := parseLimit(query.Get("limit"))
	if err != nil {
		return ListParams{}, err
	}
	cursor, err := decodeCursor(query.Get("cursor"))
	if err != nil {
		return ListParams{}, err
	}
	filter := ListParams{Limit: limit, Cursor: cursor}
	if raw := strings.TrimSpace(query.Get("severity")); raw != "" {
		severity := Severity(raw)
		if !severity.Valid() {
			return ListParams{}, fmt.Errorf("%w: severity must be warning or critical", ErrValidation)
		}
		filter.Severity = &severity
	}
	if raw := strings.TrimSpace(query.Get("lifecycle")); raw != "" {
		lifecycle := Lifecycle(raw)
		if !lifecycle.Valid() {
			return ListParams{}, fmt.Errorf("%w: lifecycle must be pending, firing or resolved", ErrValidation)
		}
		filter.Lifecycle = &lifecycle
		filter.IncludeResolved = lifecycle == LifecycleResolved
		return filter, nil
	}
	switch strings.TrimSpace(query.Get("status")) {
	case "", "active":
	case "resolved":
		filter.IncludeResolved = true
		lifecycle := LifecycleResolved
		filter.Lifecycle = &lifecycle
	case "all":
		filter.IncludeResolved = true
	default:
		return ListParams{}, fmt.Errorf("%w: status must be active, resolved or all", ErrValidation)
	}
	return filter, nil
}

func parseLimit(raw string) (int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return DefaultPageSize, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < 1 || value > MaxPageSize {
		return 0, fmt.Errorf("%w: limit must be between 1 and 100", ErrValidation)
	}
	return value, nil
}

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

func (h *handler) params(w http.ResponseWriter, r *http.Request) (uuid.UUID, uuid.UUID, bool) {
	userID, ok := h.currentUser(w, r)
	if !ok {
		return uuid.Nil, uuid.Nil, false
	}
	incidentID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody{Message: "invalid incident id"})
		return uuid.Nil, uuid.Nil, false
	}
	return userID, incidentID, true
}

func (h *handler) writeError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrNotFound):
		writeJSON(w, http.StatusNotFound, errorBody{Message: "not found"})
	case errors.Is(err, ErrForbidden):
		writeJSON(w, http.StatusForbidden, errorBody{Message: "forbidden"})
	case errors.Is(err, ErrValidation):
		writeJSON(w, http.StatusBadRequest, errorBody{Message: err.Error()})
	default:
		h.logger.Error("incidents: request failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, errorBody{Message: "internal error"})
	}
}

func decodeBody(w http.ResponseWriter, r *http.Request, dst any) bool {
	body, err := io.ReadAll(io.LimitReader(r.Body, maxBodyBytes))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody{Message: "invalid request body"})
		return false
	}
	if len(bytes.TrimSpace(body)) == 0 {
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

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
