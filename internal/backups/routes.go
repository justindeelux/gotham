package backups

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
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

// UserIDFunc resolves the caller from the request context.
type UserIDFunc func(ctx context.Context) (uuid.UUID, bool)

type errorBody struct {
	Message string `json:"message"`
}

type backupResponse struct {
	ID           string       `json:"id"`
	ResourceType ResourceType `json:"resource_type"`
	ResourceID   string       `json:"resource_id"`
	ScheduleID   string       `json:"schedule_id,omitempty"`
	ProviderID   string       `json:"provider_id,omitempty"`
	ProviderName string       `json:"provider_name,omitempty"`
	Type         BackupType   `json:"type"`
	Status       BackupStatus `json:"status"`
	Size         int64        `json:"size"`
	Location     string       `json:"location,omitempty"`
	Error        string       `json:"error,omitempty"`
	// ProviderRemoved means the run's storage provider was deleted, so its
	// artifact is unreachable but the run stays visible.
	ProviderRemoved bool `json:"provider_removed,omitempty"`
	// CleanupPending marks a run whose artifact removal is still outstanding.
	CleanupPending bool   `json:"cleanup_pending,omitempty"`
	CleanupError   string `json:"cleanup_error,omitempty"`
	ContainerID    string `json:"container_id,omitempty"`
	// PauseDuringBackup is the choice the run was started with. False is
	// meaningful only for applications and services: it labels the run as
	// possibly inconsistent.
	PauseDuringBackup bool `json:"pause_during_backup"`
	// Verified means the run recorded a checksum, so a restore checks the
	// artifact before touching the resource. Older rows restore unverified.
	Verified   bool       `json:"verified"`
	CreatedAt  time.Time  `json:"created_at"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
}

type backupEnvelope struct {
	Backup backupResponse `json:"backup"`
}

type backupListEnvelope struct {
	Backups []backupResponse `json:"backups"`
}

type restoreEnvelope struct {
	Restore RestoreResult `json:"restore"`
}

type restoreResponse struct {
	ID           string        `json:"id"`
	ResourceType ResourceType  `json:"resource_type"`
	ResourceID   string        `json:"resource_id"`
	BackupID     string        `json:"backup_id"`
	Status       RestoreStatus `json:"status"`
	Error        string        `json:"error,omitempty"`
	CreatedAt    time.Time     `json:"created_at"`
	FinishedAt   *time.Time    `json:"finished_at,omitempty"`
}

type restoreListEnvelope struct {
	Restores []restoreResponse `json:"restores"`
}

type scheduleResponse struct {
	ID                string       `json:"id"`
	ResourceType      ResourceType `json:"resource_type"`
	ResourceID        string       `json:"resource_id"`
	Cron              string       `json:"cron"`
	ProviderID        string       `json:"provider_id,omitempty"`
	Enabled           bool         `json:"enabled"`
	NextRunAt         time.Time    `json:"next_run_at"`
	LastRunAt         *time.Time   `json:"last_run_at,omitempty"`
	LastSlotAt        *time.Time   `json:"last_slot_at,omitempty"`
	MaxRunSeconds     int          `json:"max_run_seconds"`
	PauseDuringBackup bool         `json:"pause_during_backup"`
	KeepLast          *int         `json:"keep_last,omitempty"`
	CreatedAt         time.Time    `json:"created_at"`
	UpdatedAt         time.Time    `json:"updated_at"`
}

type scheduleEnvelope struct {
	Schedule scheduleResponse `json:"schedule"`
}

type scheduleListEnvelope struct {
	Schedules []scheduleResponse `json:"schedules"`
}

type providerResponse struct {
	ID             string     `json:"id"`
	Name           string     `json:"name"`
	Kind           TargetKind `json:"kind"`
	Endpoint       string     `json:"endpoint,omitempty"`
	Region         string     `json:"region,omitempty"`
	Bucket         string     `json:"bucket,omitempty"`
	Prefix         string     `json:"prefix,omitempty"`
	Builtin        bool       `json:"builtin"`
	HasCredentials bool       `json:"has_credentials"`
	BackupCount    int64      `json:"backup_count"`
	TotalSize      int64      `json:"total_size"`
	FreeSpace      int64      `json:"free_space,omitempty"`
	TotalSpace     int64      `json:"total_space,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

type providerEnvelope struct {
	Provider providerResponse `json:"provider"`
}

type providerListEnvelope struct {
	Providers []providerResponse `json:"providers"`
}

type providerCheckEnvelope struct {
	Check TargetCheck `json:"check"`
}

type createBackupRequest struct {
	ResourceType      string `json:"resource_type"`
	ResourceID        string `json:"resource_id"`
	ProviderID        string `json:"provider_id,omitempty"`
	PauseDuringBackup *bool  `json:"pause_during_backup,omitempty"`
}

type scheduleBody struct {
	ResourceType      string  `json:"resource_type,omitempty"`
	ResourceID        string  `json:"resource_id,omitempty"`
	Cron              string  `json:"cron"`
	ProviderID        *string `json:"provider_id,omitempty"`
	Enabled           *bool   `json:"enabled,omitempty"`
	MaxRunSeconds     *int    `json:"max_run_seconds,omitempty"`
	PauseDuringBackup *bool   `json:"pause_during_backup,omitempty"`
	KeepLast          *int    `json:"keep_last,omitempty"`
}

type contentResponse struct {
	Name   string `json:"name"`
	Kind   string `json:"kind"`
	Target string `json:"target,omitempty"`
}

type contentListEnvelope struct {
	Contents []contentResponse `json:"contents"`
}

type providerBody = ProviderRequest

type backupHandler struct {
	svc    BackupService
	userID UserIDFunc
	logger *slog.Logger
}

// Mount registers the resource-agnostic backup surface. Providers stay
// owner-only through the central project policy; runs, schedules and restores
// are reachable by a member with access to the resource.
func Mount(r chi.Router, auth func(http.Handler) http.Handler, userID UserIDFunc, svc BackupService) {
	if svc == nil || !Enabled() {
		return
	}
	h := &backupHandler{svc: svc, userID: userID, logger: slog.Default()}
	r.Group(func(protected chi.Router) {
		protected.Use(auth)
		protected.Get("/v1/applications/{id}/backup-contents", h.contents(ResourceApplication))
		protected.Get("/v1/services/{id}/backup-contents", h.contents(ResourceService))
		protected.Get("/v1/databases/{id}/backup-contents", h.contents(ResourceDatabase))
		protected.Get("/v1/backups", h.listBackups)
		protected.Post("/v1/backups", h.createBackup)
		protected.Get("/v1/backups/restores", h.listRestores)
		protected.Get("/v1/backups/schedules", h.listSchedules)
		protected.Post("/v1/backups/schedules", h.createSchedule)
		protected.Patch("/v1/backups/schedules/{id}", h.updateSchedule)
		protected.Delete("/v1/backups/schedules/{id}", h.deleteSchedule)
		protected.Get("/v1/backups/providers", h.listProviders)
		protected.Post("/v1/backups/providers", h.createProvider)
		protected.Patch("/v1/backups/providers/{id}", h.updateProvider)
		protected.Delete("/v1/backups/providers/{id}", h.deleteProvider)
		protected.Post("/v1/backups/providers/{id}/test", h.testProvider)
		protected.Get("/v1/backups/{id}", h.getBackup)
		protected.Delete("/v1/backups/{id}", h.deleteBackup)
		protected.Post("/v1/backups/{id}/cleanup", h.retryCleanup)
		protected.Post("/v1/backups/{id}/restore", h.restore)
	})
}

func (h *backupHandler) createBackup(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.currentUser(w, r)
	if !ok {
		return
	}
	var req createBackupRequest
	if !decodeOptionalBody(w, r, &req) {
		return
	}
	ref, ok := h.resourceRef(w, req.ResourceType, req.ResourceID)
	if !ok {
		return
	}
	providerID, ok := h.optionalUUID(w, req.ProviderID, "provider id")
	if !ok {
		return
	}
	backup, err := h.svc.CreateBackup(r.Context(), userID, CreateBackupRequest{
		Resource:          ref,
		TargetID:          providerID,
		PauseDuringBackup: req.PauseDuringBackup,
	})
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, backupEnvelope{Backup: newBackupResponse(backup)})
}

func (h *backupHandler) listBackups(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.currentUser(w, r)
	if !ok {
		return
	}
	filter := BackupFilter{Status: BackupStatus(strings.TrimSpace(r.URL.Query().Get("status")))}
	if ref, set, ok := h.queryResourceRef(w, r); !ok {
		return
	} else if set {
		filter.Resource = &ref
	}
	limit, offset, ok := optionalPage(w, r)
	if !ok {
		return
	}
	filter.Limit, filter.Offset = limit, offset
	backups, err := h.svc.ListBackups(r.Context(), userID, filter)
	if err != nil {
		h.writeError(w, err)
		return
	}
	response := make([]backupResponse, 0, len(backups))
	for _, backup := range backups {
		response = append(response, newBackupResponse(backup))
	}
	writeJSON(w, http.StatusOK, backupListEnvelope{Backups: response})
}

func (h *backupHandler) getBackup(w http.ResponseWriter, r *http.Request) {
	userID, backupID, ok := h.backupParams(w, r)
	if !ok {
		return
	}
	backup, err := h.svc.GetBackup(r.Context(), userID, backupID)
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, backupEnvelope{Backup: newBackupResponse(backup)})
}

func (h *backupHandler) deleteBackup(w http.ResponseWriter, r *http.Request) {
	userID, backupID, ok := h.backupParams(w, r)
	if !ok {
		return
	}
	var err error
	if r.URL.Query().Get("discard") == "true" {
		err = h.svc.DiscardBackup(r.Context(), userID, backupID)
	} else {
		err = h.svc.DeleteBackup(r.Context(), userID, backupID)
	}
	if err != nil {
		h.writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *backupHandler) retryCleanup(w http.ResponseWriter, r *http.Request) {
	userID, backupID, ok := h.backupParams(w, r)
	if !ok {
		return
	}
	if err := h.svc.RetryCleanup(r.Context(), userID, backupID); err != nil {
		h.writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *backupHandler) restore(w http.ResponseWriter, r *http.Request) {
	userID, backupID, ok := h.backupParams(w, r)
	if !ok {
		return
	}
	result, err := h.svc.RestoreBackup(r.Context(), userID, backupID)
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, restoreEnvelope{Restore: result})
}

func (h *backupHandler) listRestores(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.currentUser(w, r)
	if !ok {
		return
	}
	filter := RestoreFilter{}
	if ref, set, ok := h.queryResourceRef(w, r); !ok {
		return
	} else if set {
		filter.Resource = &ref
	}
	limit, offset, ok := optionalPage(w, r)
	if !ok {
		return
	}
	filter.Limit, filter.Offset = limit, offset
	restores, err := h.svc.ListRestores(r.Context(), userID, filter)
	if err != nil {
		h.writeError(w, err)
		return
	}
	response := make([]restoreResponse, 0, len(restores))
	for _, restore := range restores {
		response = append(response, newRestoreResponse(restore))
	}
	writeJSON(w, http.StatusOK, restoreListEnvelope{Restores: response})
}

func (h *backupHandler) listSchedules(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.currentUser(w, r)
	if !ok {
		return
	}
	ref, _, ok := h.queryResourceRef(w, r)
	if !ok {
		return
	}
	schedules, err := h.svc.ListSchedules(r.Context(), userID, ref)
	if err != nil {
		h.writeError(w, err)
		return
	}
	response := make([]scheduleResponse, 0, len(schedules))
	for _, schedule := range schedules {
		response = append(response, newScheduleResponse(schedule))
	}
	writeJSON(w, http.StatusOK, scheduleListEnvelope{Schedules: response})
}

func (h *backupHandler) createSchedule(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.currentUser(w, r)
	if !ok {
		return
	}
	var body scheduleBody
	if !decodeBody(w, r, &body) {
		return
	}
	ref, ok := h.resourceRef(w, body.ResourceType, body.ResourceID)
	if !ok {
		return
	}
	schedule, err := h.svc.CreateSchedule(r.Context(), userID, ScheduleRequest{
		Resource:          ref,
		Cron:              body.Cron,
		TargetID:          body.ProviderID,
		Enabled:           body.Enabled,
		MaxRunSeconds:     body.MaxRunSeconds,
		PauseDuringBackup: body.PauseDuringBackup,
		KeepLast:          body.KeepLast,
	})
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, scheduleEnvelope{Schedule: newScheduleResponse(schedule)})
}

func (h *backupHandler) updateSchedule(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.currentUser(w, r)
	if !ok {
		return
	}
	scheduleID, ok := h.paramUUID(w, r, "id", "schedule id")
	if !ok {
		return
	}
	var body scheduleBody
	if !decodeBody(w, r, &body) {
		return
	}
	req := ScheduleRequest{
		Cron:              body.Cron,
		TargetID:          body.ProviderID,
		Enabled:           body.Enabled,
		MaxRunSeconds:     body.MaxRunSeconds,
		PauseDuringBackup: body.PauseDuringBackup,
		KeepLast:          body.KeepLast,
	}
	if strings.TrimSpace(body.ResourceType) != "" || strings.TrimSpace(body.ResourceID) != "" {
		ref, ok := h.resourceRef(w, body.ResourceType, body.ResourceID)
		if !ok {
			return
		}
		req.Resource = ref
	}
	schedule, err := h.svc.UpdateSchedule(r.Context(), userID, scheduleID, req)
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, scheduleEnvelope{Schedule: newScheduleResponse(schedule)})
}

func (h *backupHandler) deleteSchedule(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.currentUser(w, r)
	if !ok {
		return
	}
	scheduleID, ok := h.paramUUID(w, r, "id", "schedule id")
	if !ok {
		return
	}
	if err := h.svc.DeleteSchedule(r.Context(), userID, scheduleID); err != nil {
		h.writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *backupHandler) listProviders(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.currentUser(w, r)
	if !ok {
		return
	}
	providers, err := h.svc.ListProviders(r.Context(), userID)
	if err != nil {
		h.writeError(w, err)
		return
	}
	response := make([]providerResponse, 0, len(providers))
	for _, provider := range providers {
		response = append(response, newProviderResponse(provider))
	}
	writeJSON(w, http.StatusOK, providerListEnvelope{Providers: response})
}

func (h *backupHandler) createProvider(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.currentUser(w, r)
	if !ok {
		return
	}
	var req providerBody
	if !decodeBody(w, r, &req) {
		return
	}
	provider, err := h.svc.CreateProvider(r.Context(), userID, req)
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, providerEnvelope{Provider: newProviderResponse(provider)})
}

func (h *backupHandler) updateProvider(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.currentUser(w, r)
	if !ok {
		return
	}
	providerID, ok := h.paramUUID(w, r, "id", "provider id")
	if !ok {
		return
	}
	var req providerBody
	if !decodeBody(w, r, &req) {
		return
	}
	provider, err := h.svc.UpdateProvider(r.Context(), userID, providerID, req)
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, providerEnvelope{Provider: newProviderResponse(provider)})
}

func (h *backupHandler) deleteProvider(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.currentUser(w, r)
	if !ok {
		return
	}
	providerID, ok := h.paramUUID(w, r, "id", "provider id")
	if !ok {
		return
	}
	confirm := r.URL.Query().Get("confirm") == "true"
	if err := h.svc.DeleteProvider(r.Context(), userID, providerID, confirm); err != nil {
		h.writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *backupHandler) testProvider(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.currentUser(w, r)
	if !ok {
		return
	}
	providerID, ok := h.paramUUID(w, r, "id", "provider id")
	if !ok {
		return
	}
	check, err := h.svc.TestProvider(r.Context(), userID, providerID)
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, providerCheckEnvelope{Check: check})
}

// contents lists what a resource's next run captures. The route stays under
// the resource path so the central project policy grants a member access the
// same way it does for the resource itself.
func (h *backupHandler) contents(kind ResourceType) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID, ok := h.currentUser(w, r)
		if !ok {
			return
		}
		resourceID, ok := h.paramUUID(w, r, "id", "resource id")
		if !ok {
			return
		}
		contents, err := h.svc.Contents(r.Context(), userID, ResourceRef{Type: kind, ID: resourceID})
		if err != nil {
			h.writeError(w, err)
			return
		}
		response := make([]contentResponse, 0, len(contents))
		for _, item := range contents {
			response = append(response, contentResponse(item))
		}
		writeJSON(w, http.StatusOK, contentListEnvelope{Contents: response})
	}
}

func (h *backupHandler) currentUser(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
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

func (h *backupHandler) backupParams(w http.ResponseWriter, r *http.Request) (uuid.UUID, uuid.UUID, bool) {
	userID, ok := h.currentUser(w, r)
	if !ok {
		return uuid.Nil, uuid.Nil, false
	}
	backupID, ok := h.paramUUID(w, r, "id", "backup id")
	if !ok {
		return uuid.Nil, uuid.Nil, false
	}
	return userID, backupID, true
}

func (h *backupHandler) resourceRef(w http.ResponseWriter, rawType, rawID string) (ResourceRef, bool) {
	kind, err := ParseResourceType(rawType)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody{Message: err.Error()})
		return ResourceRef{}, false
	}
	id, err := uuid.Parse(strings.TrimSpace(rawID))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody{Message: "invalid resource id"})
		return ResourceRef{}, false
	}
	return ResourceRef{Type: kind, ID: id}, true
}

// queryResourceRef reads the optional resource_type/resource_id pair. Both or
// neither must be present.
func (h *backupHandler) queryResourceRef(w http.ResponseWriter, r *http.Request) (ResourceRef, bool, bool) {
	rawType := strings.TrimSpace(r.URL.Query().Get("resource_type"))
	rawID := strings.TrimSpace(r.URL.Query().Get("resource_id"))
	if rawType == "" && rawID == "" {
		return ResourceRef{}, false, true
	}
	if rawType == "" || rawID == "" {
		writeJSON(w, http.StatusBadRequest, errorBody{Message: "resource_type and resource_id are required together"})
		return ResourceRef{}, false, false
	}
	ref, ok := h.resourceRef(w, rawType, rawID)
	return ref, ok, ok
}

func (h *backupHandler) paramUUID(w http.ResponseWriter, r *http.Request, name, label string) (uuid.UUID, bool) {
	id, err := uuid.Parse(chi.URLParam(r, name))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody{Message: "invalid " + label})
		return uuid.Nil, false
	}
	return id, true
}

func (h *backupHandler) requiredUUID(w http.ResponseWriter, raw, label string) (uuid.UUID, bool) {
	id, err := uuid.Parse(strings.TrimSpace(raw))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody{Message: "invalid " + label})
		return uuid.Nil, false
	}
	return id, true
}

func (h *backupHandler) optionalUUID(w http.ResponseWriter, raw, label string) (uuid.UUID, bool) {
	if strings.TrimSpace(raw) == "" {
		return uuid.Nil, true
	}
	return h.requiredUUID(w, raw, label)
}

func (h *backupHandler) writeError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrNotFound):
		writeJSON(w, http.StatusNotFound, errorBody{Message: "not found"})
	case errors.Is(err, ErrValidation), errors.Is(err, ErrUnsupportedResource):
		writeJSON(w, http.StatusBadRequest, errorBody{Message: err.Error()})
	case errors.Is(err, ErrBackupInFlight):
		writeJSON(w, http.StatusConflict, errorBody{Message: "a backup or restore is already running for this resource"})
	case errors.Is(err, ErrResourceBusy):
		writeJSON(w, http.StatusConflict, errorBody{Message: err.Error()})
	case errors.Is(err, ErrBackupNotCompleted):
		writeJSON(w, http.StatusConflict, errorBody{Message: err.Error()})
	case errors.Is(err, ErrBackupDamaged):
		writeJSON(w, http.StatusConflict, errorBody{Message: err.Error()})
	case errors.Is(err, ErrTargetStranded):
		writeJSON(w, http.StatusConflict, errorBody{Message: err.Error()})
	case errors.Is(err, ErrBuiltinProvider):
		writeJSON(w, http.StatusConflict, errorBody{Message: err.Error()})
	case errors.Is(err, ErrProviderInUse), errors.Is(err, ErrProviderHasBackups):
		writeJSON(w, http.StatusConflict, errorBody{Message: err.Error()})
	case errors.Is(err, ErrCleanupFailed):
		writeJSON(w, http.StatusBadGateway, errorBody{Message: err.Error()})
	case errors.Is(err, ErrInsufficientSpace):
		writeJSON(w, http.StatusInsufficientStorage, errorBody{Message: err.Error()})
	case errors.Is(err, ErrConflict):
		writeJSON(w, http.StatusConflict, errorBody{Message: "a storage provider with that name already exists"})
	case errors.Is(err, ErrDisabled):
		writeJSON(w, http.StatusServiceUnavailable, errorBody{Message: "backups are disabled"})
	default:
		h.logger.Error("backups: request failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, errorBody{Message: "internal error"})
	}
}

// optionalPage reads limit/offset. Ordering is deterministic (created_at DESC,
// id DESC) so a page is stable and a member's page is filled from the runs
// they can actually reach, not from whatever happens to be newest.
func optionalPage(w http.ResponseWriter, r *http.Request) (int, int, bool) {
	limit, ok := optionalLimit(w, r)
	if !ok {
		return 0, 0, false
	}
	raw := strings.TrimSpace(r.URL.Query().Get("offset"))
	if raw == "" {
		return limit, 0, true
	}
	offset, err := strconv.Atoi(raw)
	if err != nil || offset < 0 {
		writeJSON(w, http.StatusBadRequest, errorBody{Message: "invalid offset"})
		return 0, 0, false
	}
	return limit, offset, true
}

func optionalLimit(w http.ResponseWriter, r *http.Request) (int, bool) {
	raw := strings.TrimSpace(r.URL.Query().Get("limit"))
	if raw == "" {
		return 0, true
	}
	limit, err := strconv.Atoi(raw)
	if err != nil || limit < 0 {
		writeJSON(w, http.StatusBadRequest, errorBody{Message: "invalid limit"})
		return 0, false
	}
	return limit, true
}

func newBackupResponse(backup Backup) backupResponse {
	response := backupResponse{
		ID:                backup.ID.String(),
		ResourceType:      backup.ResourceRef.Type,
		ResourceID:        backup.ResourceRef.ID.String(),
		Type:              backup.Type,
		Status:            backup.Status,
		Size:              backup.Size,
		Location:          backup.Location,
		Error:             backup.Error,
		ProviderRemoved:   backup.ProviderRemoved,
		CleanupPending:    backup.CleanupPending,
		CleanupError:      backup.CleanupError,
		ContainerID:       backup.ContainerID,
		PauseDuringBackup: backup.PauseDuringBackup,
		Verified:          backup.ChecksumSHA256 != "",
		CreatedAt:         backup.CreatedAt,
	}
	if backup.ScheduleID != uuid.Nil {
		response.ScheduleID = backup.ScheduleID.String()
	}
	if backup.TargetID != uuid.Nil {
		response.ProviderID = backup.TargetID.String()
	}
	response.ProviderName = backup.ProviderName
	if !backup.FinishedAt.IsZero() {
		finished := backup.FinishedAt
		response.FinishedAt = &finished
	}
	return response
}

func newRestoreResponse(restore Restore) restoreResponse {
	response := restoreResponse{
		ID:           restore.ID.String(),
		ResourceType: restore.ResourceRef.Type,
		ResourceID:   restore.ResourceRef.ID.String(),
		BackupID:     restore.BackupID.String(),
		Status:       restore.Status,
		Error:        restore.Error,
		CreatedAt:    restore.CreatedAt,
	}
	if !restore.FinishedAt.IsZero() {
		finished := restore.FinishedAt
		response.FinishedAt = &finished
	}
	return response
}

func newScheduleResponse(schedule BackupSchedule) scheduleResponse {
	response := scheduleResponse{
		ID:                schedule.ID.String(),
		ResourceType:      schedule.ResourceRef.Type,
		ResourceID:        schedule.ResourceRef.ID.String(),
		Cron:              schedule.Cron,
		Enabled:           schedule.Enabled,
		NextRunAt:         schedule.NextRunAt,
		MaxRunSeconds:     schedule.MaxRunSeconds,
		PauseDuringBackup: schedule.PauseDuringBackup,
		KeepLast:          schedule.KeepLast,
		CreatedAt:         schedule.CreatedAt,
		UpdatedAt:         schedule.UpdatedAt,
	}
	if schedule.TargetID != uuid.Nil {
		response.ProviderID = schedule.TargetID.String()
	}
	if !schedule.LastRunAt.IsZero() {
		lastRun := schedule.LastRunAt
		response.LastRunAt = &lastRun
	}
	if !schedule.LastSlotAt.IsZero() {
		lastSlot := schedule.LastSlotAt
		response.LastSlotAt = &lastSlot
	}
	return response
}

func newProviderResponse(target BackupTarget) providerResponse {
	return providerResponse{
		ID:             target.ID.String(),
		Name:           target.Name,
		Kind:           target.Kind,
		Endpoint:       target.Endpoint,
		Region:         target.Region,
		Bucket:         target.Bucket,
		Prefix:         target.Prefix,
		Builtin:        target.Builtin,
		HasCredentials: target.Kind == TargetS3,
		BackupCount:    target.Usage.BackupCount,
		TotalSize:      target.Usage.TotalSize,
		FreeSpace:      target.Usage.FreeSpace,
		TotalSpace:     target.Usage.TotalSpace,
		CreatedAt:      target.CreatedAt,
		UpdatedAt:      target.UpdatedAt,
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
	return decodeInto(w, body, dst)
}

func decodeOptionalBody(w http.ResponseWriter, r *http.Request, dst any) bool {
	body, err := io.ReadAll(io.LimitReader(r.Body, maxBodyBytes))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody{Message: "invalid request body"})
		return false
	}
	if len(bytes.TrimSpace(body)) == 0 {
		return true
	}
	return decodeInto(w, body, dst)
}

func decodeInto(w http.ResponseWriter, body []byte, dst any) bool {
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
