package notifications

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

// maxBodyBytes bounds channel request bodies (they are tiny).
const maxBodyBytes = 1 << 20 // 1 MiB

// UserIDFunc resolves the authenticated user from the request context. The
// server passes its own accessor, so this package never imports the HTTP
// server (it mirrors databases.UserIDFunc for the same reason).
type UserIDFunc func(ctx context.Context) (uuid.UUID, bool)

// channelEnvelope wraps a single channel.
type channelEnvelope struct {
	Channel ChannelView `json:"channel"`
}

// channelListEnvelope wraps a channel list.
type channelListEnvelope struct {
	Channels []ChannelView `json:"channels"`
}

// testEnvelope wraps a send-test result.
type testEnvelope struct {
	Check TestResult `json:"check"`
}

// errorBody is the JSON body returned for failures.
type errorBody struct {
	Message string `json:"message"`
}

// handler serves the notification-channel routes for one
// NotificationService.
type handler struct {
	svc    NotificationService
	userID UserIDFunc
	logger *slog.Logger
}

// Mount registers the authenticated notification-channel endpoints on r:
//
//	GET    /v1/notification-channels
//	POST   /v1/notification-channels
//	GET    /v1/notification-channels/{id}
//	PATCH  /v1/notification-channels/{id}
//	DELETE /v1/notification-channels/{id}
//	POST   /v1/notification-channels/{id}/test
//
// auth wraps the group (the server passes its team chain: RequireAuth,
// RequireTeam and the owner/admin gate for mutating methods, so a read_only
// member reads and every mutation is refused before the handler). A nil svc
// or FEATURE_NOTIFICATIONS=false mounts nothing, so the control plane can
// call Mount unconditionally.
func Mount(r chi.Router, auth func(http.Handler) http.Handler, userID UserIDFunc, svc NotificationService) {
	if svc == nil || !Enabled() {
		return
	}
	h := &handler{svc: svc, userID: userID, logger: slog.Default()}
	r.Group(func(protected chi.Router) {
		protected.Use(auth)
		protected.Get("/v1/notification-channels", h.list)
		protected.Post("/v1/notification-channels", h.create)
		protected.Get("/v1/notification-channels/{id}", h.get)
		protected.Patch("/v1/notification-channels/{id}", h.update)
		protected.Delete("/v1/notification-channels/{id}", h.delete)
		protected.Post("/v1/notification-channels/{id}/test", h.test)
	})
}

// list serves GET /v1/notification-channels.
func (h *handler) list(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.currentUser(w, r)
	if !ok {
		return
	}
	channels, err := h.svc.ListChannels(r.Context(), userID)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	if channels == nil {
		channels = []ChannelView{}
	}
	writeJSON(w, http.StatusOK, channelListEnvelope{Channels: channels})
}

// create serves POST /v1/notification-channels: 201 with the redacted row.
func (h *handler) create(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.currentUser(w, r)
	if !ok {
		return
	}
	var req ChannelRequest
	if !decodeBody(w, r, &req) {
		return
	}
	channel, err := h.svc.CreateChannel(r.Context(), userID, req)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, channelEnvelope{Channel: channel})
}

// get serves GET /v1/notification-channels/{id}.
func (h *handler) get(w http.ResponseWriter, r *http.Request) {
	userID, channelID, ok := h.channelParams(w, r)
	if !ok {
		return
	}
	channel, err := h.svc.GetChannel(r.Context(), userID, channelID)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, channelEnvelope{Channel: channel})
}

// update serves PATCH /v1/notification-channels/{id}.
func (h *handler) update(w http.ResponseWriter, r *http.Request) {
	userID, channelID, ok := h.channelParams(w, r)
	if !ok {
		return
	}
	var req ChannelRequest
	if !decodeBody(w, r, &req) {
		return
	}
	channel, err := h.svc.UpdateChannel(r.Context(), userID, channelID, req)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, channelEnvelope{Channel: channel})
}

// delete serves DELETE /v1/notification-channels/{id}: 204 on success.
func (h *handler) delete(w http.ResponseWriter, r *http.Request) {
	userID, channelID, ok := h.channelParams(w, r)
	if !ok {
		return
	}
	if err := h.svc.DeleteChannel(r.Context(), userID, channelID); err != nil {
		h.writeServiceError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// test serves POST /v1/notification-channels/{id}/test: it delivers a
// synthetic event and reports the outcome without exposing any secret.
func (h *handler) test(w http.ResponseWriter, r *http.Request) {
	userID, channelID, ok := h.channelParams(w, r)
	if !ok {
		return
	}
	result, err := h.svc.TestChannel(r.Context(), userID, channelID)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, testEnvelope{Check: result})
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

// channelParams resolves the authenticated user and the {id} path parameter,
// answering 401/400 as needed.
func (h *handler) channelParams(w http.ResponseWriter, r *http.Request) (uuid.UUID, uuid.UUID, bool) {
	userID, ok := h.currentUser(w, r)
	if !ok {
		return uuid.Nil, uuid.Nil, false
	}
	channelID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody{Message: "invalid channel id"})
		return uuid.Nil, uuid.Nil, false
	}
	return userID, channelID, true
}

// writeServiceError maps service sentinels to HTTP responses.
func (h *handler) writeServiceError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrNotFound):
		writeJSON(w, http.StatusNotFound, errorBody{Message: "not found"})
	case errors.Is(err, ErrForbidden):
		writeJSON(w, http.StatusForbidden, errorBody{Message: "insufficient team role"})
	case errors.Is(err, ErrValidation):
		writeJSON(w, http.StatusBadRequest, errorBody{Message: err.Error()})
	default:
		h.logger.Error("notifications: request failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, errorBody{Message: "internal error"})
	}
}

// decodeBody decodes a required JSON body into dst. An empty or malformed
// body answers 400.
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
