package databases

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// backupResponse is the wire representation of a backup run. It never carries
// storage credentials — only where the bytes are.
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

// backupEnvelope wraps a single backup.
type backupEnvelope struct {
	Backup backupResponse `json:"backup"`
}

// backupListEnvelope wraps a backup list.
type backupListEnvelope struct {
	Backups []backupResponse `json:"backups"`
}

// restoreEnvelope wraps a queued restore.
type restoreEnvelope struct {
	Restore RestoreResult `json:"restore"`
}

// scheduleResponse is the wire representation of a schedule.
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

// scheduleEnvelope wraps a single schedule.
type scheduleEnvelope struct {
	Schedule scheduleResponse `json:"schedule"`
}

// scheduleListEnvelope wraps a schedule list.
type scheduleListEnvelope struct {
	Schedules []scheduleResponse `json:"schedules"`
}

// targetResponse is the wire representation of a storage target. The
// credentials are reported only as a flag — an access key is a credential
// too and never leaves the server. An s3 target always carries them (create
// requires both halves), a local one never does.
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

// targetEnvelope wraps a single target.
type targetEnvelope struct {
	Target targetResponse `json:"target"`
}

// targetListEnvelope wraps a target list.
type targetListEnvelope struct {
	Targets []targetResponse `json:"targets"`
}

// targetCheckEnvelope wraps a connection test.
type targetCheckEnvelope struct {
	Check TargetCheck `json:"check"`
}

// backupCreateRequest is the optional body of POST .../backup.
type backupCreateRequest struct {
	TargetID string `json:"target_id,omitempty"`
}

// restoreRequest is the body of POST .../restore.
type restoreRequest struct {
	BackupID string `json:"backup_id"`
}

// scheduleRequest is the body of the schedule create and update endpoints.
// On update, target_id is tri-state: absent or null keeps the stored target,
// an empty string clears it and an id replaces it (see ScheduleRequest).
type scheduleRequest = ScheduleRequest

// targetRequest is the body of the target create and update endpoints.
type targetRequest = TargetRequest

// backupHandler serves the backup endpoints. It embeds the databases handler
// to reuse its authenticated-user resolution, path-parameter parsing and
// error mapping; the embedded DatabaseService is nil on purpose and nothing
// on this path calls it.
type backupHandler struct {
	handler
	svc BackupService
}

// MountBackups registers the authenticated backup endpoints on r:
//
//	POST   /v1/databases/{id}/backup
//	POST   /v1/databases/{id}/restore
//	GET    /v1/databases/{id}/backups
//	GET    /v1/databases/{id}/backups/{backupId}
//	DELETE /v1/databases/{id}/backups/{backupId}
//	GET    /v1/databases/{id}/schedules
//	POST   /v1/databases/{id}/schedules
//	PATCH  /v1/databases/{id}/schedules/{scheduleId}
//	DELETE /v1/databases/{id}/schedules/{scheduleId}
//	GET    /v1/databases/backup-targets
//	POST   /v1/databases/backup-targets
//	PATCH  /v1/databases/backup-targets/{targetId}
//	DELETE /v1/databases/backup-targets/{targetId}
//	POST   /v1/databases/backup-targets/{targetId}/test
//
// auth wraps the group (the server passes its RequireAuth); a nil svc or
// FEATURE_DATABASES=false mounts nothing, exactly like Mount.
func MountBackups(r chi.Router, auth func(http.Handler) http.Handler, userID UserIDFunc, svc BackupService) {
	if svc == nil || !Enabled() {
		return
	}
	h := &backupHandler{
		handler: handler{userID: userID, logger: slog.Default()},
		svc:     svc,
	}
	r.Group(func(protected chi.Router) {
		protected.Use(auth)
		protected.Post("/v1/databases/{id}/backup", h.createBackup)
		protected.Post("/v1/databases/{id}/restore", h.restore)
		protected.Get("/v1/databases/{id}/backups", h.listBackups)
		protected.Get("/v1/databases/{id}/backups/{backupId}", h.getBackup)
		protected.Delete("/v1/databases/{id}/backups/{backupId}", h.deleteBackup)
		protected.Get("/v1/databases/{id}/schedules", h.listSchedules)
		protected.Post("/v1/databases/{id}/schedules", h.createSchedule)
		protected.Patch("/v1/databases/{id}/schedules/{scheduleId}", h.updateSchedule)
		protected.Delete("/v1/databases/{id}/schedules/{scheduleId}", h.deleteSchedule)
		protected.Get("/v1/databases/backup-targets", h.listTargets)
		protected.Post("/v1/databases/backup-targets", h.createTarget)
		protected.Patch("/v1/databases/backup-targets/{targetId}", h.updateTarget)
		protected.Delete("/v1/databases/backup-targets/{targetId}", h.deleteTarget)
		protected.Post("/v1/databases/backup-targets/{targetId}/test", h.testTarget)
	})
}

// createBackup serves POST .../databases/{id}/backup: 202 with the recorded
// row while the job runs behind it. The body is optional — without one the
// artifact goes to the local backup directory.
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

// restore serves POST .../databases/{id}/restore: 202 with the queued job.
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

// listBackups serves GET .../databases/{id}/backups.
func (h *backupHandler) listBackups(w http.ResponseWriter, r *http.Request) {
	userID, databaseID, ok := h.databaseParams(w, r)
	if !ok {
		return
	}
	backups, err := h.svc.ListBackups(r.Context(), userID, databaseID)
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

// getBackup serves GET .../databases/{id}/backups/{backupId}.
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

// deleteBackup serves DELETE .../databases/{id}/backups/{backupId}: the
// stored artifact goes, then the row. 204 on success.
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

// listSchedules serves GET .../databases/{id}/schedules.
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

// createSchedule serves POST .../databases/{id}/schedules: 201 with the row
// and its computed next run.
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

// updateSchedule serves PATCH .../databases/{id}/schedules/{scheduleId}.
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

// deleteSchedule serves DELETE .../databases/{id}/schedules/{scheduleId}.
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

// listTargets serves GET .../databases/backup-targets for the caller.
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

// createTarget serves POST .../databases/backup-targets: 201 with the row,
// credentials sealed on the way in and never echoed back.
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

// updateTarget serves PATCH .../databases/backup-targets/{targetId}.
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

// deleteTarget serves DELETE .../databases/backup-targets/{targetId}.
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

// testTarget serves POST .../databases/backup-targets/{targetId}/test. A
// failed check is a 200 with ok=false: the request itself worked, the answer
// is just negative.
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

// backupParams resolves the authenticated user, the database id and the
// backup id of the request.
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

// scheduleParams resolves the authenticated user, the database id and the
// schedule id of the request.
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

// targetParams resolves the authenticated user and the target id.
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

// paramUUID parses a path parameter, answering 400 when it is not a UUID.
func (h *backupHandler) paramUUID(w http.ResponseWriter, r *http.Request, name, label string) (uuid.UUID, bool) {
	id, err := uuid.Parse(chi.URLParam(r, name))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody{Message: "invalid " + label})
		return uuid.Nil, false
	}
	return id, true
}

// requiredUUID parses a mandatory body id.
func (h *backupHandler) requiredUUID(w http.ResponseWriter, raw, label string) (uuid.UUID, bool) {
	id, err := uuid.Parse(strings.TrimSpace(raw))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody{Message: "invalid " + label})
		return uuid.Nil, false
	}
	return id, true
}

// optionalUUID parses an optional body id: empty means "not set".
func (h *backupHandler) optionalUUID(w http.ResponseWriter, raw, label string) (uuid.UUID, bool) {
	if strings.TrimSpace(raw) == "" {
		return uuid.Nil, true
	}
	return h.requiredUUID(w, raw, label)
}

// writeBackupError maps the backup sentinels to HTTP responses and defers
// everything else to the shared databases mapping.
func (h *backupHandler) writeBackupError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrBackupInFlight):
		writeJSON(w, http.StatusConflict, errorBody{Message: "a backup or restore is already running for this database"})
	case errors.Is(err, ErrBackupNotCompleted):
		writeJSON(w, http.StatusConflict, errorBody{Message: err.Error()})
	default:
		h.writeServiceError(w, err)
	}
}

// newBackupResponse maps a domain backup to its wire representation.
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

// newScheduleResponse maps a domain schedule to its wire representation.
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

// newTargetResponse maps a domain target to its wire representation. It has
// no credential field at all: HasCredentials is the only thing the API says
// about them.
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

// decodeOptionalBody decodes a body that may be absent: an empty request
// body means "use the defaults" instead of a 400, which is what
// POST .../backup without arguments needs. A present body must still be
// valid JSON with no unknown fields.
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
