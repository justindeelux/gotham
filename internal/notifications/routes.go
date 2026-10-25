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

const maxBodyBytes = 1 << 20

type UserIDFunc func(ctx context.Context) (uuid.UUID, bool)

type channelEnvelope struct {
	Channel ChannelView `json:"channel"`
}

type channelListEnvelope struct {
	Channels []ChannelView `json:"channels"`
}

type testEnvelope struct {
	Check TestResult `json:"check"`
}

type deliveryListEnvelope struct {
	Deliveries []DeliveryView `json:"deliveries"`
}

type errorBody struct {
	Message string `json:"message"`
}

type handler struct {
	svc    NotificationService
	userID UserIDFunc
	logger *slog.Logger
}

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
		protected.Get("/v1/notification-channels/{id}/deliveries", h.deliveries)
	})
}

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

func (h *handler) deliveries(w http.ResponseWriter, r *http.Request) {
	userID, channelID, ok := h.channelParams(w, r)
	if !ok {
		return
	}
	deliveries, err := h.svc.ListDeliveries(r.Context(), userID, channelID)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	if deliveries == nil {
		deliveries = []DeliveryView{}
	}
	writeJSON(w, http.StatusOK, deliveryListEnvelope{Deliveries: deliveries})
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

func (h *handler) writeServiceError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrNotFound):
		writeJSON(w, http.StatusNotFound, errorBody{Message: "not found"})
	case errors.Is(err, ErrForbidden):
		writeJSON(w, http.StatusForbidden, errorBody{Message: "forbidden"})
	case errors.Is(err, ErrValidation):
		writeJSON(w, http.StatusBadRequest, errorBody{Message: err.Error()})
	default:
		h.logger.Error("notifications: request failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, errorBody{Message: "internal error"})
	}
}

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

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
