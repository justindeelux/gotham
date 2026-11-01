package storages

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

const maxBodyBytes = 1 << 20

// UserIDFunc resolves the caller from the request context.
type UserIDFunc func(ctx context.Context) (uuid.UUID, bool)

type errorBody struct {
	Message string `json:"message"`
}

type storageResponse struct {
	ID             string    `json:"id"`
	Name           string    `json:"name"`
	Kind           Kind      `json:"kind"`
	Endpoint       string    `json:"endpoint,omitempty"`
	Region         string    `json:"region,omitempty"`
	Bucket         string    `json:"bucket,omitempty"`
	Prefix         string    `json:"prefix,omitempty"`
	Builtin        bool      `json:"builtin"`
	HasCredentials bool      `json:"has_credentials"`
	BackupCount    int64     `json:"backup_count"`
	TotalSize      int64     `json:"total_size"`
	FreeSpace      int64     `json:"free_space,omitempty"`
	TotalSpace     int64     `json:"total_space,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

type storageEnvelope struct {
	Storage storageResponse `json:"storage"`
}

type storageListEnvelope struct {
	Storages []storageResponse `json:"storages"`
}

type checkEnvelope struct {
	Check Check `json:"check"`
}

type storageHandler struct {
	svc    *Manager
	userID UserIDFunc
	logger *slog.Logger
}

// Mount registers the standalone storage surface. It is an operator surface:
// the caller passes the owner + admin-scope middleware so an API token needs
// the admin scope, matching the other platform routes.
func Mount(r chi.Router, auth func(http.Handler) http.Handler, userID UserIDFunc, svc *Manager) {
	if svc == nil {
		return
	}
	h := &storageHandler{svc: svc, userID: userID, logger: slog.Default()}
	r.Group(func(protected chi.Router) {
		protected.Use(auth)
		protected.Get("/v1/storages", h.list)
		protected.Post("/v1/storages", h.create)
		protected.Patch("/v1/storages/{id}", h.update)
		protected.Delete("/v1/storages/{id}", h.delete)
		protected.Post("/v1/storages/{id}/test", h.test)
	})
}

func (h *storageHandler) list(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.currentUser(w, r)
	if !ok {
		return
	}
	items, err := h.svc.List(r.Context(), userID)
	if err != nil {
		h.writeError(w, err)
		return
	}
	response := make([]storageResponse, 0, len(items))
	for _, item := range items {
		response = append(response, newStorageResponse(item))
	}
	writeJSON(w, http.StatusOK, storageListEnvelope{Storages: response})
}

func (h *storageHandler) create(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.currentUser(w, r)
	if !ok {
		return
	}
	var req Request
	if !decodeBody(w, r, &req) {
		return
	}
	item, err := h.svc.Create(r.Context(), userID, req)
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, storageEnvelope{Storage: newStorageResponse(item)})
}

func (h *storageHandler) update(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.currentUser(w, r)
	if !ok {
		return
	}
	storageID, ok := h.paramUUID(w, r, "id", "storage id")
	if !ok {
		return
	}
	var req Request
	if !decodeBody(w, r, &req) {
		return
	}
	item, err := h.svc.Update(r.Context(), userID, storageID, req)
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, storageEnvelope{Storage: newStorageResponse(item)})
}

func (h *storageHandler) delete(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.currentUser(w, r)
	if !ok {
		return
	}
	storageID, ok := h.paramUUID(w, r, "id", "storage id")
	if !ok {
		return
	}
	confirm := r.URL.Query().Get("confirm") == "true"
	if err := h.svc.Delete(r.Context(), userID, storageID, confirm); err != nil {
		h.writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *storageHandler) test(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.currentUser(w, r)
	if !ok {
		return
	}
	storageID, ok := h.paramUUID(w, r, "id", "storage id")
	if !ok {
		return
	}
	check, err := h.svc.Test(r.Context(), userID, storageID)
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, checkEnvelope{Check: check})
}

func (h *storageHandler) currentUser(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
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

func (h *storageHandler) paramUUID(w http.ResponseWriter, r *http.Request, name, label string) (uuid.UUID, bool) {
	id, err := uuid.Parse(chi.URLParam(r, name))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody{Message: "invalid " + label})
		return uuid.Nil, false
	}
	return id, true
}

func (h *storageHandler) writeError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrNotFound):
		writeJSON(w, http.StatusNotFound, errorBody{Message: "storage not found"})
	case errors.Is(err, ErrValidation), errors.Is(err, ErrUnsupported):
		writeJSON(w, http.StatusBadRequest, errorBody{Message: err.Error()})
	case errors.Is(err, ErrBuiltin), errors.Is(err, ErrInUse),
		errors.Is(err, ErrHasBackups), errors.Is(err, ErrStranded), errors.Is(err, ErrConflict):
		writeJSON(w, http.StatusConflict, errorBody{Message: err.Error()})
	case errors.Is(err, ErrNotReady):
		writeJSON(w, http.StatusServiceUnavailable, errorBody{Message: "storage management is unavailable"})
	default:
		h.logger.Error("storages: request failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, errorBody{Message: "internal error"})
	}
}

func newStorageResponse(item Storage) storageResponse {
	return storageResponse{
		ID:             item.ID.String(),
		Name:           item.Name,
		Kind:           item.Kind,
		Endpoint:       item.Endpoint,
		Region:         item.Region,
		Bucket:         item.Bucket,
		Prefix:         item.Prefix,
		Builtin:        item.Builtin,
		HasCredentials: item.Kind == KindS3,
		BackupCount:    item.Usage.BackupCount,
		TotalSize:      item.Usage.TotalSize,
		FreeSpace:      item.Usage.FreeSpace,
		TotalSpace:     item.Usage.TotalSpace,
		CreatedAt:      item.CreatedAt,
		UpdatedAt:      item.UpdatedAt,
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
