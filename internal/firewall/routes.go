package firewall

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// UserIDFunc returns the authenticated actor of a request.
type UserIDFunc func(context.Context) (uuid.UUID, bool)

type handler struct {
	svc    *Service
	userID UserIDFunc
	logger *slog.Logger
}

type statusEnvelope struct {
	Status Status `json:"status"`
}

type stateEnvelope struct {
	Status Status `json:"status"`
	Rules  []Rule `json:"rules"`
}

type rulesEnvelope struct {
	Rules []Rule `json:"rules"`
}

type ruleEnvelope struct {
	Rule RuleInput `json:"rule"`
}

type toggleRequest struct {
	Enabled bool `json:"enabled"`
}

// errorBody carries a stable code the web translates and the English message
// it falls back to when the code is unknown (an older agent sends none).
type errorBody struct {
	Message string            `json:"message"`
	Code    string            `json:"code,omitempty"`
	Params  map[string]string `json:"params,omitempty"`
}

// Mount registers the per-server firewall routes behind the owner platform
// gate.
func Mount(r chi.Router, admin func(http.Handler) http.Handler, userID UserIDFunc, svc *Service) {
	if svc == nil {
		return
	}
	h := &handler{svc: svc, userID: userID, logger: slog.Default()}
	r.Group(func(g chi.Router) {
		g.Use(admin)
		g.Get("/v1/servers/{id}/firewall", h.getStatus)
		g.Put("/v1/servers/{id}/firewall", h.putEnabled)
		g.Get("/v1/servers/{id}/firewall/rules", h.listRules)
		g.Post("/v1/servers/{id}/firewall/rules", h.addRule)
		g.Put("/v1/servers/{id}/firewall/rules/{ruleID}", h.updateRule)
		g.Delete("/v1/servers/{id}/firewall/rules/{ruleID}", h.deleteRule)
	})
}

func (h *handler) actor(r *http.Request) uuid.UUID {
	if h.userID == nil {
		return uuid.Nil
	}
	id, _ := h.userID(r.Context())
	return id
}

func (h *handler) getStatus(w http.ResponseWriter, r *http.Request) {
	serverID, ok := serverIDParam(w, r)
	if !ok {
		return
	}
	st, err := h.svc.Status(r.Context(), serverID)
	if err != nil {
		h.replyError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, statusEnvelope{Status: st})
}

func (h *handler) putEnabled(w http.ResponseWriter, r *http.Request) {
	serverID, ok := serverIDParam(w, r)
	if !ok {
		return
	}
	var req toggleRequest
	if !decode(w, r, &req) {
		return
	}
	state, err := h.svc.SetEnabled(r.Context(), h.actor(r), serverID, req.Enabled)
	if err != nil {
		h.replyError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, stateEnvelope(state))
}

func (h *handler) listRules(w http.ResponseWriter, r *http.Request) {
	serverID, ok := serverIDParam(w, r)
	if !ok {
		return
	}
	rules, err := h.svc.ListRules(r.Context(), serverID)
	if err != nil {
		h.replyError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, rulesEnvelope{Rules: rules})
}

func (h *handler) addRule(w http.ResponseWriter, r *http.Request) {
	serverID, ok := serverIDParam(w, r)
	if !ok {
		return
	}
	var req ruleEnvelope
	if !decode(w, r, &req) {
		return
	}
	rules, err := h.svc.AddRule(r.Context(), h.actor(r), serverID, req.Rule)
	if err != nil {
		h.replyError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, rulesEnvelope{Rules: rules})
}

func (h *handler) updateRule(w http.ResponseWriter, r *http.Request) {
	serverID, ok := serverIDParam(w, r)
	if !ok {
		return
	}
	ruleID := chi.URLParam(r, "ruleID")
	var req ruleEnvelope
	if !decode(w, r, &req) {
		return
	}
	rules, err := h.svc.UpdateRule(r.Context(), h.actor(r), serverID, ruleID, req.Rule)
	if err != nil {
		h.replyError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, rulesEnvelope{Rules: rules})
}

func (h *handler) deleteRule(w http.ResponseWriter, r *http.Request) {
	serverID, ok := serverIDParam(w, r)
	if !ok {
		return
	}
	rules, err := h.svc.DeleteRule(r.Context(), h.actor(r), serverID, chi.URLParam(r, "ruleID"))
	if err != nil {
		h.replyError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, rulesEnvelope{Rules: rules})
}

func (h *handler) replyError(w http.ResponseWriter, err error) {
	d := detailOf(err)
	switch {
	case errors.Is(err, ErrNotFound):
		d = d.withDefault(codeRuleNotFound)
		writeJSON(w, http.StatusNotFound, errorBody{Message: "not found", Code: d.code, Params: d.params})
	case errors.Is(err, ErrValidation):
		writeJSON(w, http.StatusBadRequest, errorBody{Message: trimSentinel(err, ErrValidation), Code: d.code, Params: d.params})
	case errors.Is(err, ErrConflict):
		writeJSON(w, http.StatusConflict, errorBody{Message: trimSentinel(err, ErrConflict), Code: d.code, Params: d.params})
	case errors.Is(err, ErrUnsupported):
		d = d.withDefault(codeAgentOutdated)
		writeJSON(w, http.StatusBadGateway, errorBody{Message: err.Error(), Code: d.code, Params: d.params})
	case errors.Is(err, ErrUnavailable):
		d = d.withDefault(codeAgentUnavailable)
		writeJSON(w, http.StatusBadGateway, errorBody{Message: err.Error(), Code: d.code, Params: d.params})
	default:
		h.logger.Error("firewall: request failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, errorBody{Message: "internal error"})
	}
}

// trimSentinel drops the module prefix so the operator sees the node's own
// lockout explanation, not an internal sentinel name.
func trimSentinel(err, sentinel error) string {
	return strings.TrimPrefix(err.Error(), sentinel.Error()+": ")
}

func serverIDParam(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody{Message: "invalid server id", Code: codeInvalidRequest})
		return uuid.Nil, false
	}
	return id, true
}

func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody{Message: "invalid body", Code: codeInvalidRequest})
		return false
	}
	if len(body) == 0 {
		writeJSON(w, http.StatusBadRequest, errorBody{Message: "invalid body", Code: codeInvalidRequest})
		return false
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(v); err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody{Message: "invalid body", Code: codeInvalidRequest})
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}
