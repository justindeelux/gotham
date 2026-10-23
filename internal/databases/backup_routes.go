package databases

import (
	"bytes"
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

type backupResponse struct {
	ID          string       `json:"id"`
	DatabaseID  string       `json:"database_id"`
	ScheduleID  string       `json:"schedule_id,omitempty"`
	Type        BackupType   `json:"type"`
	Status      BackupStatus `json:"status"`
	Size        int64        `json:"size"`
	Location    string       `json:"location,omitempty"`
	Error       string       `json:"error,omitempty"`
	ContainerID string       `json:"container_id,omitempty"`
	CreatedAt   time.Time    `json:"created_at"`
	FinishedAt  *time.Time   `json:"finished_at,omitempty"`
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
	ID         string        `json:"id"`
	DatabaseID string        `json:"database_id"`
	BackupID   string        `json:"backup_id"`
	Status     RestoreStatus `json:"status"`
	Error      string        `json:"error,omitempty"`
	CreatedAt  time.Time     `json:"created_at"`
	FinishedAt *time.Time    `json:"finished_at,omitempty"`
}

type restoreListEnvelope struct {
	Restores []restoreResponse `json:"restores"`
}

type scheduleResponse struct {
	ID         string     `json:"id"`
	DatabaseID string     `json:"database_id"`
	Cron       string     `json:"cron"`
	TargetID   string     `json:"target_id,omitempty"`
	Enabled    bool       `json:"enabled"`
	NextRunAt  time.Time  `json:"next_run_at"`
	LastRunAt  *time.Time `json:"last_run_at,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
}

type scheduleEnvelope struct {
	Schedule scheduleResponse `json:"schedule"`
}

type scheduleListEnvelope struct {
	Schedules []scheduleResponse `json:"schedules"`
}

type targetResponse struct {
	ID             string     `json:"id"`
	Name           string     `json:"name"`
	Kind           TargetKind `json:"kind"`
	Endpoint       string     `json:"endpoint,omitempty"`
	Region         string     `json:"region,omitempty"`
	Bucket         string     `json:"bucket,omitempty"`
	Prefix         string     `json:"prefix,omitempty"`
	HasCredentials bool       `json:"has_credentials"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

type targetEnvelope struct {
	Target targetResponse `json:"target"`
}

type targetListEnvelope struct {
	Targets []targetResponse `json:"targets"`
}

type targetCheckEnvelope struct {
	Check TargetCheck `json:"check"`
}

type backupCreateRequest struct {
	TargetID string `json:"target_id,omitempty"`
}

type restoreRequest struct {
	BackupID string `json:"backup_id"`
}

type scheduleRequest = ScheduleRequest

type targetRequest = TargetRequest

type backupHandler struct {
	handler
	svc BackupService
}

func MountBackups(r chi.Router, dbAuth func(http.Handler) http.Handler, targetAuth func(http.Handler) http.Handler, userID UserIDFunc, svc BackupService) {
	if svc == nil || !Enabled() {
		return
	}
	h := &backupHandler{
		handler: handler{userID: userID, logger: slog.Default()},
		svc:     svc,
	}
	r.Group(func(protected chi.Router) {
		protected.Use(dbAuth)
		protected.Post("/v1/databases/{id}/backup", h.createBackup)
		protected.Post("/v1/databases/{id}/restore", h.restore)
		protected.Get("/v1/databases/{id}/backups", h.listBackups)
		protected.Get("/v1/databases/{id}/backups/{backupId}", h.getBackup)
		protected.Delete("/v1/databases/{id}/backups/{backupId}", h.deleteBackup)
		protected.Get("/v1/databases/{id}/restores", h.listRestores)
		protected.Get("/v1/databases/{id}/schedules", h.listSchedules)
		protected.Post("/v1/databases/{id}/schedules", h.createSchedule)
		protected.Patch("/v1/databases/{id}/schedules/{scheduleId}", h.updateSchedule)
		protected.Delete("/v1/databases/{id}/schedules/{scheduleId}", h.deleteSchedule)
	})
	r.Group(func(protected chi.Router) {
		protected.Use(targetAuth)
		protected.Get("/v1/databases/backup-targets", h.listTargets)
		protected.Post("/v1/databases/backup-targets", h.createTarget)
		protected.Patch("/v1/databases/backup-targets/{targetId}", h.updateTarget)
		protected.Delete("/v1/databases/backup-targets/{targetId}", h.deleteTarget)
		protected.Post("/v1/databases/backup-targets/{targetId}/test", h.testTarget)
	})
}

func (h *backupHandler) createBackup(w http.ResponseWriter, r *http.Request) {
	userID, databaseID, ok := h.databaseParams(w, r)
	if !ok {
		return
	}
	var req backupCreateRequest
	if !decodeOptionalBody(w, r, &req) {
		return
	}
	targetID, ok := h.optionalUUID(w, req.TargetID, "target id")
	if !ok {
		return
	}
	backup, err := h.svc.CreateBackup(r.Context(), userID, databaseID, CreateBackupRequest{TargetID: targetID})
	if err != nil {
		h.writeBackupError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, backupEnvelope{Backup: newBackupResponse(backup)})
}

func (h *backupHandler) restore(w http.ResponseWriter, r *http.Request) {
	userID, databaseID, ok := h.databaseParams(w, r)
	if !ok {
		return
	}
	var req restoreRequest
	if !decodeBody(w, r, &req) {
		return
	}
	backupID, ok := h.requiredUUID(w, req.BackupID, "backup id")
	if !ok {
		return
	}
	result, err := h.svc.RestoreBackup(r.Context(), userID, databaseID, RestoreRequest{BackupID: backupID})
	if err != nil {
		h.writeBackupError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, restoreEnvelope{Restore: result})
}

func (h *backupHandler) listBackups(w http.ResponseWriter, r *http.Request) {
	userID, databaseID, ok := h.databaseParams(w, r)
	if !ok {
		return
	}
	limit, ok := h.optionalLimit(w, r)
	if !ok {
		return
	}
	backups, err := h.svc.ListBackups(r.Context(), userID, databaseID, limit)
	if err != nil {
		h.writeBackupError(w, err)
		return
	}
	response := make([]backupResponse, 0, len(backups))
	for _, backup := range backups {
		response = append(response, newBackupResponse(backup))
	}
	writeJSON(w, http.StatusOK, backupListEnvelope{Backups: response})
}

func (h *backupHandler) getBackup(w http.ResponseWriter, r *http.Request) {
	userID, databaseID, backupID, ok := h.backupParams(w, r)
	if !ok {
		return
	}
	backup, err := h.svc.GetBackup(r.Context(), userID, databaseID, backupID)
	if err != nil {
		h.writeBackupError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, backupEnvelope{Backup: newBackupResponse(backup)})
}

func (h *backupHandler) listRestores(w http.ResponseWriter, r *http.Request) {
	userID, databaseID, ok := h.databaseParams(w, r)
	if !ok {
		return
	}
	limit, ok := h.optionalLimit(w, r)
	if !ok {
		return
	}
	restores, err := h.svc.ListRestores(r.Context(), userID, databaseID, limit)
	if err != nil {
		h.writeBackupError(w, err)
		return
	}
	response := make([]restoreResponse, 0, len(restores))
	for _, restore := range restores {
		response = append(response, newRestoreResponse(restore))
	}
	writeJSON(w, http.StatusOK, restoreListEnvelope{Restores: response})
}

func (h *backupHandler) deleteBackup(w http.ResponseWriter, r *http.Request) {
	userID, databaseID, backupID, ok := h.backupParams(w, r)
	if !ok {
		return
	}
	if err := h.svc.DeleteBackup(r.Context(), userID, databaseID, backupID); err != nil {
		h.writeBackupError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *backupHandler) listSchedules(w http.ResponseWriter, r *http.Request) {
	userID, databaseID, ok := h.databaseParams(w, r)
	if !ok {
		return
	}
	schedules, err := h.svc.ListSchedules(r.Context(), userID, databaseID)
	if err != nil {
		h.writeBackupError(w, err)
		return
	}
	response := make([]scheduleResponse, 0, len(schedules))
	for _, schedule := range schedules {
		response = append(response, newScheduleResponse(schedule))
	}
	writeJSON(w, http.StatusOK, scheduleListEnvelope{Schedules: response})
}

func (h *backupHandler) createSchedule(w http.ResponseWriter, r *http.Request) {
	userID, databaseID, ok := h.databaseParams(w, r)
	if !ok {
		return
	}
	var req scheduleRequest
	if !decodeBody(w, r, &req) {
		return
	}
	schedule, err := h.svc.CreateSchedule(r.Context(), userID, databaseID, req)
	if err != nil {
		h.writeBackupError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, scheduleEnvelope{Schedule: newScheduleResponse(schedule)})
}

func (h *backupHandler) updateSchedule(w http.ResponseWriter, r *http.Request) {
	userID, databaseID, scheduleID, ok := h.scheduleParams(w, r)
	if !ok {
		return
	}
	var req scheduleRequest
	if !decodeBody(w, r, &req) {
		return
	}
	schedule, err := h.svc.UpdateSchedule(r.Context(), userID, databaseID, scheduleID, req)
	if err != nil {
		h.writeBackupError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, scheduleEnvelope{Schedule: newScheduleResponse(schedule)})
}

func (h *backupHandler) deleteSchedule(w http.ResponseWriter, r *http.Request) {
	userID, databaseID, scheduleID, ok := h.scheduleParams(w, r)
	if !ok {
		return
	}
	if err := h.svc.DeleteSchedule(r.Context(), userID, databaseID, scheduleID); err != nil {
		h.writeBackupError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *backupHandler) listTargets(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.currentUser(w, r)
	if !ok {
		return
	}
	targets, err := h.svc.ListTargets(r.Context(), userID)
	if err != nil {
		h.writeBackupError(w, err)
		return
	}
	response := make([]targetResponse, 0, len(targets))
	for _, target := range targets {
		response = append(response, newTargetResponse(target))
	}
	writeJSON(w, http.StatusOK, targetListEnvelope{Targets: response})
}

func (h *backupHandler) createTarget(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.currentUser(w, r)
	if !ok {
		return
	}
	var req targetRequest
	if !decodeBody(w, r, &req) {
		return
	}
	target, err := h.svc.CreateTarget(r.Context(), userID, req)
	if err != nil {
		h.writeBackupError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, targetEnvelope{Target: newTargetResponse(target)})
}

func (h *backupHandler) updateTarget(w http.ResponseWriter, r *http.Request) {
	userID, targetID, ok := h.targetParams(w, r)
	if !ok {
		return
	}
	var req targetRequest
	if !decodeBody(w, r, &req) {
		return
	}
	target, err := h.svc.UpdateTarget(r.Context(), userID, targetID, req)
	if err != nil {
		h.writeBackupError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, targetEnvelope{Target: newTargetResponse(target)})
}

func (h *backupHandler) deleteTarget(w http.ResponseWriter, r *http.Request) {
	userID, targetID, ok := h.targetParams(w, r)
	if !ok {
		return
	}
	if err := h.svc.DeleteTarget(r.Context(), userID, targetID); err != nil {
		h.writeBackupError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *backupHandler) testTarget(w http.ResponseWriter, r *http.Request) {
	userID, targetID, ok := h.targetParams(w, r)
	if !ok {
		return
	}
	check, err := h.svc.TestTarget(r.Context(), userID, targetID)
	if err != nil {
		h.writeBackupError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, targetCheckEnvelope{Check: check})
}

func (h *backupHandler) backupParams(w http.ResponseWriter, r *http.Request) (uuid.UUID, uuid.UUID, uuid.UUID, bool) {
	userID, databaseID, ok := h.databaseParams(w, r)
	if !ok {
		return uuid.Nil, uuid.Nil, uuid.Nil, false
	}
	backupID, ok := h.paramUUID(w, r, "backupId", "backup id")
	if !ok {
		return uuid.Nil, uuid.Nil, uuid.Nil, false
	}
	return userID, databaseID, backupID, true
}

func (h *backupHandler) scheduleParams(w http.ResponseWriter, r *http.Request) (uuid.UUID, uuid.UUID, uuid.UUID, bool) {
	userID, databaseID, ok := h.databaseParams(w, r)
	if !ok {
		return uuid.Nil, uuid.Nil, uuid.Nil, false
	}
	scheduleID, ok := h.paramUUID(w, r, "scheduleId", "schedule id")
	if !ok {
		return uuid.Nil, uuid.Nil, uuid.Nil, false
	}
	return userID, databaseID, scheduleID, true
}

func (h *backupHandler) targetParams(w http.ResponseWriter, r *http.Request) (uuid.UUID, uuid.UUID, bool) {
	userID, ok := h.currentUser(w, r)
	if !ok {
		return uuid.Nil, uuid.Nil, false
	}
	targetID, ok := h.paramUUID(w, r, "targetId", "target id")
	if !ok {
		return uuid.Nil, uuid.Nil, false
	}
	return userID, targetID, true
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

func (h *backupHandler) writeBackupError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrBackupInFlight):
		writeJSON(w, http.StatusConflict, errorBody{Message: "a backup or restore is already running for this database"})
	case errors.Is(err, ErrBackupNotCompleted):
		writeJSON(w, http.StatusConflict, errorBody{Message: err.Error()})
	case errors.Is(err, ErrTargetStranded):
		writeJSON(w, http.StatusConflict, errorBody{Message: err.Error()})
	case errors.Is(err, ErrConflict):
		writeJSON(w, http.StatusConflict, errorBody{Message: "a storage target with that name already exists"})
	default:
		h.writeServiceError(w, err)
	}
}

func (h *backupHandler) optionalLimit(w http.ResponseWriter, r *http.Request) (int, bool) {
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
		ID:          backup.ID.String(),
		DatabaseID:  backup.DatabaseID.String(),
		Type:        backup.Type,
		Status:      backup.Status,
		Size:        backup.Size,
		Location:    backup.Location,
		Error:       backup.Error,
		ContainerID: backup.ContainerID,
		CreatedAt:   backup.CreatedAt,
	}
	if backup.ScheduleID != uuid.Nil {
		response.ScheduleID = backup.ScheduleID.String()
	}
	if !backup.FinishedAt.IsZero() {
		finished := backup.FinishedAt
		response.FinishedAt = &finished
	}
	return response
}

func newRestoreResponse(restore Restore) restoreResponse {
	response := restoreResponse{
		ID:         restore.ID.String(),
		DatabaseID: restore.DatabaseID.String(),
		BackupID:   restore.BackupID.String(),
		Status:     restore.Status,
		Error:      restore.Error,
		CreatedAt:  restore.CreatedAt,
	}
	if !restore.FinishedAt.IsZero() {
		finished := restore.FinishedAt
		response.FinishedAt = &finished
	}
	return response
}

func newScheduleResponse(schedule BackupSchedule) scheduleResponse {
	response := scheduleResponse{
		ID:         schedule.ID.String(),
		DatabaseID: schedule.DatabaseID.String(),
		Cron:       schedule.Cron,
		Enabled:    schedule.Enabled,
		NextRunAt:  schedule.NextRunAt,
		CreatedAt:  schedule.CreatedAt,
		UpdatedAt:  schedule.UpdatedAt,
	}
	if schedule.TargetID != uuid.Nil {
		response.TargetID = schedule.TargetID.String()
	}
	if !schedule.LastRunAt.IsZero() {
		lastRun := schedule.LastRunAt
		response.LastRunAt = &lastRun
	}
	return response
}

func newTargetResponse(target BackupTarget) targetResponse {
	return targetResponse{
		ID:             target.ID.String(),
		Name:           target.Name,
		Kind:           target.Kind,
		Endpoint:       target.Endpoint,
		Region:         target.Region,
		Bucket:         target.Bucket,
		Prefix:         target.Prefix,
		HasCredentials: target.Kind == TargetS3,
		CreatedAt:      target.CreatedAt,
		UpdatedAt:      target.UpdatedAt,
	}
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
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dst); err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody{Message: "invalid request body"})
		return false
	}
	return true
}
