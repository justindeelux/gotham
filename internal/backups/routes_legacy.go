package backups

import (
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// The database-scoped routes predate the resource-agnostic surface. They are
// kept as thin aliases with unchanged request and response shapes so existing
// API tokens, MCP clients and the database Backups tab keep working. Docs mark
// them deprecated.

type legacyBackupResponse struct {
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

type legacyBackupEnvelope struct {
	Backup legacyBackupResponse `json:"backup"`
}

type legacyBackupListEnvelope struct {
	Backups []legacyBackupResponse `json:"backups"`
}

type legacyRestoreEnvelope struct {
	Restore RestoreResult `json:"restore"`
}

type legacyRestoreResponse struct {
	ID         string        `json:"id"`
	DatabaseID string        `json:"database_id"`
	BackupID   string        `json:"backup_id"`
	Status     RestoreStatus `json:"status"`
	Error      string        `json:"error,omitempty"`
	CreatedAt  time.Time     `json:"created_at"`
	FinishedAt *time.Time    `json:"finished_at,omitempty"`
}

type legacyRestoreListEnvelope struct {
	Restores []legacyRestoreResponse `json:"restores"`
}

type legacyScheduleResponse struct {
	ID            string     `json:"id"`
	DatabaseID    string     `json:"database_id"`
	Cron          string     `json:"cron"`
	TargetID      string     `json:"target_id,omitempty"`
	Enabled       bool       `json:"enabled"`
	NextRunAt     time.Time  `json:"next_run_at"`
	LastRunAt     *time.Time `json:"last_run_at,omitempty"`
	LastSlotAt    *time.Time `json:"last_slot_at,omitempty"`
	MaxRunSeconds int        `json:"max_run_seconds"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
}

type legacyScheduleEnvelope struct {
	Schedule legacyScheduleResponse `json:"schedule"`
}

type legacyScheduleListEnvelope struct {
	Schedules []legacyScheduleResponse `json:"schedules"`
}

type legacyTargetResponse struct {
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

type legacyTargetEnvelope struct {
	Target legacyTargetResponse `json:"target"`
}

type legacyTargetListEnvelope struct {
	Targets []legacyTargetResponse `json:"targets"`
}

type legacyTargetCheckEnvelope struct {
	Check TargetCheck `json:"check"`
}

type legacyBackupCreateRequest struct {
	TargetID string `json:"target_id,omitempty"`
}

type legacyRestoreRequest struct {
	BackupID string `json:"backup_id"`
}

type legacyScheduleRequest struct {
	Cron          string  `json:"cron"`
	TargetID      *string `json:"target_id,omitempty"`
	Enabled       *bool   `json:"enabled,omitempty"`
	MaxRunSeconds *int    `json:"max_run_seconds,omitempty"`
}

type legacyHandler struct {
	backupHandler
}

// MountLegacy registers the deprecated database-scoped aliases.
func MountLegacy(r chi.Router, databaseAuth func(http.Handler) http.Handler, targetAuth func(http.Handler) http.Handler, userID UserIDFunc, svc BackupService) {
	if svc == nil || !Enabled() {
		return
	}
	h := &legacyHandler{backupHandler{svc: svc, userID: userID, logger: slog.Default()}}
	r.Group(func(protected chi.Router) {
		protected.Use(databaseAuth)
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

func (h *legacyHandler) createBackup(w http.ResponseWriter, r *http.Request) {
	userID, databaseID, ok := h.databaseParams(w, r)
	if !ok {
		return
	}
	var req legacyBackupCreateRequest
	if !decodeOptionalBody(w, r, &req) {
		return
	}
	targetID, ok := h.optionalUUID(w, req.TargetID, "target id")
	if !ok {
		return
	}
	backup, err := h.svc.CreateBackup(r.Context(), userID, CreateBackupRequest{
		Resource: ResourceRef{Type: ResourceDatabase, ID: databaseID},
		TargetID: targetID,
	})
	if err != nil {
		h.writeLegacyError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, legacyBackupEnvelope{Backup: newLegacyBackupResponse(backup)})
}

func (h *legacyHandler) restore(w http.ResponseWriter, r *http.Request) {
	userID, databaseID, ok := h.databaseParams(w, r)
	if !ok {
		return
	}
	var req legacyRestoreRequest
	if !decodeBody(w, r, &req) {
		return
	}
	backupID, ok := h.requiredUUID(w, req.BackupID, "backup id")
	if !ok {
		return
	}
	backup, err := h.svc.GetBackup(r.Context(), userID, backupID)
	if err != nil {
		h.writeLegacyError(w, err)
		return
	}
	if backup.ResourceRef != (ResourceRef{Type: ResourceDatabase, ID: databaseID}) {
		h.writeError(w, ErrNotFound)
		return
	}
	result, err := h.svc.RestoreBackup(r.Context(), userID, backupID)
	if err != nil {
		h.writeLegacyError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, legacyRestoreEnvelope{Restore: result})
}

func (h *legacyHandler) listBackups(w http.ResponseWriter, r *http.Request) {
	userID, databaseID, ok := h.databaseParams(w, r)
	if !ok {
		return
	}
	limit, offset, ok := optionalPage(w, r)
	if !ok {
		return
	}
	ref := ResourceRef{Type: ResourceDatabase, ID: databaseID}
	backups, err := h.svc.ListBackups(r.Context(), userID, BackupFilter{Resource: &ref, Limit: limit, Offset: offset})
	if err != nil {
		h.writeLegacyError(w, err)
		return
	}
	response := make([]legacyBackupResponse, 0, len(backups))
	for _, backup := range backups {
		response = append(response, newLegacyBackupResponse(backup))
	}
	writeJSON(w, http.StatusOK, legacyBackupListEnvelope{Backups: response})
}

func (h *legacyHandler) getBackup(w http.ResponseWriter, r *http.Request) {
	userID, databaseID, backupID, ok := h.backupParams(w, r)
	if !ok {
		return
	}
	backup, err := h.ownedDatabaseBackup(r, userID, databaseID, backupID)
	if err != nil {
		h.writeLegacyError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, legacyBackupEnvelope{Backup: newLegacyBackupResponse(backup)})
}

func (h *legacyHandler) deleteBackup(w http.ResponseWriter, r *http.Request) {
	userID, databaseID, backupID, ok := h.backupParams(w, r)
	if !ok {
		return
	}
	if _, err := h.ownedDatabaseBackup(r, userID, databaseID, backupID); err != nil {
		h.writeLegacyError(w, err)
		return
	}
	if err := h.svc.DeleteBackup(r.Context(), userID, backupID); err != nil {
		h.writeLegacyError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *legacyHandler) listRestores(w http.ResponseWriter, r *http.Request) {
	userID, databaseID, ok := h.databaseParams(w, r)
	if !ok {
		return
	}
	limit, offset, ok := optionalPage(w, r)
	if !ok {
		return
	}
	ref := ResourceRef{Type: ResourceDatabase, ID: databaseID}
	restores, err := h.svc.ListRestores(r.Context(), userID, RestoreFilter{Resource: &ref, Limit: limit, Offset: offset})
	if err != nil {
		h.writeLegacyError(w, err)
		return
	}
	response := make([]legacyRestoreResponse, 0, len(restores))
	for _, restore := range restores {
		response = append(response, newLegacyRestoreResponse(restore))
	}
	writeJSON(w, http.StatusOK, legacyRestoreListEnvelope{Restores: response})
}

func (h *legacyHandler) listSchedules(w http.ResponseWriter, r *http.Request) {
	userID, databaseID, ok := h.databaseParams(w, r)
	if !ok {
		return
	}
	ref := ResourceRef{Type: ResourceDatabase, ID: databaseID}
	schedules, err := h.svc.ListSchedules(r.Context(), userID, ref)
	if err != nil {
		h.writeLegacyError(w, err)
		return
	}
	response := make([]legacyScheduleResponse, 0, len(schedules))
	for _, schedule := range schedules {
		response = append(response, newLegacyScheduleResponse(schedule))
	}
	writeJSON(w, http.StatusOK, legacyScheduleListEnvelope{Schedules: response})
}

func (h *legacyHandler) createSchedule(w http.ResponseWriter, r *http.Request) {
	userID, databaseID, ok := h.databaseParams(w, r)
	if !ok {
		return
	}
	var req legacyScheduleRequest
	if !decodeBody(w, r, &req) {
		return
	}
	schedule, err := h.svc.CreateSchedule(r.Context(), userID, ScheduleRequest{
		Resource:      ResourceRef{Type: ResourceDatabase, ID: databaseID},
		Cron:          req.Cron,
		TargetID:      req.TargetID,
		Enabled:       req.Enabled,
		MaxRunSeconds: req.MaxRunSeconds,
	})
	if err != nil {
		h.writeLegacyError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, legacyScheduleEnvelope{Schedule: newLegacyScheduleResponse(schedule)})
}

func (h *legacyHandler) updateSchedule(w http.ResponseWriter, r *http.Request) {
	userID, databaseID, scheduleID, ok := h.scheduleParams(w, r)
	if !ok {
		return
	}
	if err := h.assertDatabaseSchedule(r, userID, databaseID, scheduleID); err != nil {
		h.writeLegacyError(w, err)
		return
	}
	var req legacyScheduleRequest
	if !decodeBody(w, r, &req) {
		return
	}
	schedule, err := h.svc.UpdateSchedule(r.Context(), userID, scheduleID, ScheduleRequest{
		Cron:          req.Cron,
		TargetID:      req.TargetID,
		Enabled:       req.Enabled,
		MaxRunSeconds: req.MaxRunSeconds,
	})
	if err != nil {
		h.writeLegacyError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, legacyScheduleEnvelope{Schedule: newLegacyScheduleResponse(schedule)})
}

func (h *legacyHandler) deleteSchedule(w http.ResponseWriter, r *http.Request) {
	userID, databaseID, scheduleID, ok := h.scheduleParams(w, r)
	if !ok {
		return
	}
	if err := h.assertDatabaseSchedule(r, userID, databaseID, scheduleID); err != nil {
		h.writeLegacyError(w, err)
		return
	}
	if err := h.svc.DeleteSchedule(r.Context(), userID, scheduleID); err != nil {
		h.writeLegacyError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *legacyHandler) listTargets(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.currentUser(w, r)
	if !ok {
		return
	}
	targets, err := h.svc.ListProviders(r.Context(), userID)
	if err != nil {
		h.writeLegacyError(w, err)
		return
	}
	response := make([]legacyTargetResponse, 0, len(targets))
	for _, target := range targets {
		response = append(response, newLegacyTargetResponse(target))
	}
	writeJSON(w, http.StatusOK, legacyTargetListEnvelope{Targets: response})
}

func (h *legacyHandler) createTarget(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.currentUser(w, r)
	if !ok {
		return
	}
	var req providerBody
	if !decodeBody(w, r, &req) {
		return
	}
	target, err := h.svc.CreateProvider(r.Context(), userID, req)
	if err != nil {
		h.writeLegacyError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, legacyTargetEnvelope{Target: newLegacyTargetResponse(target)})
}

func (h *legacyHandler) updateTarget(w http.ResponseWriter, r *http.Request) {
	userID, targetID, ok := h.targetParams(w, r)
	if !ok {
		return
	}
	var req providerBody
	if !decodeBody(w, r, &req) {
		return
	}
	target, err := h.svc.UpdateProvider(r.Context(), userID, targetID, req)
	if err != nil {
		h.writeLegacyError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, legacyTargetEnvelope{Target: newLegacyTargetResponse(target)})
}

func (h *legacyHandler) deleteTarget(w http.ResponseWriter, r *http.Request) {
	userID, targetID, ok := h.targetParams(w, r)
	if !ok {
		return
	}
	if err := h.svc.DeleteProvider(r.Context(), userID, targetID, r.URL.Query().Get("confirm") == "true"); err != nil {
		h.writeLegacyError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *legacyHandler) testTarget(w http.ResponseWriter, r *http.Request) {
	userID, targetID, ok := h.targetParams(w, r)
	if !ok {
		return
	}
	check, err := h.svc.TestProvider(r.Context(), userID, targetID)
	if err != nil {
		h.writeLegacyError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, legacyTargetCheckEnvelope{Check: check})
}

func (h *legacyHandler) ownedDatabaseBackup(r *http.Request, userID, databaseID, backupID uuid.UUID) (Backup, error) {
	backup, err := h.svc.GetBackup(r.Context(), userID, backupID)
	if err != nil {
		return Backup{}, err
	}
	if backup.ResourceRef != (ResourceRef{Type: ResourceDatabase, ID: databaseID}) {
		return Backup{}, ErrNotFound
	}
	return backup, nil
}

func (h *legacyHandler) assertDatabaseSchedule(r *http.Request, userID, databaseID, scheduleID uuid.UUID) error {
	schedules, err := h.svc.ListSchedules(r.Context(), userID, ResourceRef{Type: ResourceDatabase, ID: databaseID})
	if err != nil {
		return err
	}
	for _, schedule := range schedules {
		if schedule.ID == scheduleID {
			return nil
		}
	}
	return ErrNotFound
}

func (h *legacyHandler) databaseParams(w http.ResponseWriter, r *http.Request) (uuid.UUID, uuid.UUID, bool) {
	userID, ok := h.currentUser(w, r)
	if !ok {
		return uuid.Nil, uuid.Nil, false
	}
	databaseID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody{Message: "invalid database id"})
		return uuid.Nil, uuid.Nil, false
	}
	return userID, databaseID, true
}

func (h *legacyHandler) backupParams(w http.ResponseWriter, r *http.Request) (uuid.UUID, uuid.UUID, uuid.UUID, bool) {
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

func (h *legacyHandler) scheduleParams(w http.ResponseWriter, r *http.Request) (uuid.UUID, uuid.UUID, uuid.UUID, bool) {
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

func (h *legacyHandler) targetParams(w http.ResponseWriter, r *http.Request) (uuid.UUID, uuid.UUID, bool) {
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

func newLegacyBackupResponse(backup Backup) legacyBackupResponse {
	response := legacyBackupResponse{
		ID:          backup.ID.String(),
		DatabaseID:  backup.ResourceRef.ID.String(),
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

func newLegacyRestoreResponse(restore Restore) legacyRestoreResponse {
	response := legacyRestoreResponse{
		ID:         restore.ID.String(),
		DatabaseID: restore.ResourceRef.ID.String(),
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

func newLegacyScheduleResponse(schedule BackupSchedule) legacyScheduleResponse {
	response := legacyScheduleResponse{
		ID:            schedule.ID.String(),
		DatabaseID:    schedule.ResourceRef.ID.String(),
		Cron:          schedule.Cron,
		Enabled:       schedule.Enabled,
		NextRunAt:     schedule.NextRunAt,
		MaxRunSeconds: schedule.MaxRunSeconds,
		CreatedAt:     schedule.CreatedAt,
		UpdatedAt:     schedule.UpdatedAt,
	}
	if schedule.TargetID != uuid.Nil {
		response.TargetID = schedule.TargetID.String()
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

func newLegacyTargetResponse(target BackupTarget) legacyTargetResponse {
	return legacyTargetResponse{
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

// legacyError keeps the old conflict wording for the aliases.
func (h *legacyHandler) writeLegacyError(w http.ResponseWriter, err error) {
	if errors.Is(err, ErrConflict) {
		writeJSON(w, http.StatusConflict, errorBody{Message: "a storage target with that name already exists"})
		return
	}
	h.writeError(w, err)
}
