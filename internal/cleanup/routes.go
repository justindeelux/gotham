package cleanup

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/servers"
	"github.com/justindeelux/gotham/internal/store"
)

const (
	maxBodyBytes = 1 << 20
	maxScanNodes = 50
	maxCleanIDs  = 1000
	maxAgeHours  = 8760
)

// UserIDFunc resolves the authenticated user of a protected request.
type UserIDFunc func(context.Context) (uuid.UUID, bool)

// CleanupService is the slice of the cleanup service the scan and clean routes
// use.
type CleanupService interface {
	Scan(ctx context.Context, serverIDs []string, minAge time.Duration) []NodeScan
	Clean(
		ctx context.Context, actor uuid.UUID, trigger, serverID string, ids []string, minAge time.Duration,
	) CleanResult
}

// CleanupCenter is the schedule, history and exclusion surface. It is mounted
// next to CleanupService so the scan flow keeps its narrow contract.
type CleanupCenter interface {
	ListSchedules(ctx context.Context) ([]store.CleanupSchedule, error)
	CreateSchedule(ctx context.Context, actor uuid.UUID, input ScheduleInput) (store.CleanupSchedule, error)
	UpdateSchedule(ctx context.Context, id uuid.UUID, input ScheduleInput) (store.CleanupSchedule, error)
	DeleteSchedule(ctx context.Context, id uuid.UUID) error
	RunScheduleNow(ctx context.Context, id uuid.UUID) error
	NextCronRun(expression string) (time.Time, error)
	ListRuns(ctx context.Context, filter store.CleanupRunFilter) ([]store.CleanupRunView, error)
	GetRun(ctx context.Context, id uuid.UUID) (store.CleanupRunView, []store.CleanupRunItem, error)
	ListExclusions(ctx context.Context) ([]store.CleanupExclusion, error)
	CreateExclusion(ctx context.Context, actor uuid.UUID, input ExclusionInput) (store.CleanupExclusion, error)
	DeleteExclusion(ctx context.Context, id uuid.UUID) error
}

type handler struct {
	svc    CleanupService
	center CleanupCenter
	userID UserIDFunc
}

// Mount registers the owner-only cleanup routes. Scanning is safe; cleaning
// removes what the operator selected and is never mounted for members.
func Mount(r chi.Router, admin func(http.Handler) http.Handler, userID UserIDFunc, svc CleanupService) {
	if svc == nil || admin == nil {
		return
	}
	h := &handler{svc: svc, userID: userID}
	r.Group(func(protected chi.Router) {
		protected.Use(admin)
		protected.Get("/v1/cleanup/categories", h.categories)
		protected.Post("/v1/cleanup/scan", h.scan)
		protected.Post("/v1/cleanup/clean", h.clean)
	})
}

// MountCenter registers the owner-only schedule, history and exclusion routes.
func MountCenter(r chi.Router, admin func(http.Handler) http.Handler, userID UserIDFunc, center CleanupCenter) {
	if center == nil || admin == nil {
		return
	}
	h := &handler{center: center, userID: userID}
	r.Group(func(protected chi.Router) {
		protected.Use(admin)
		protected.Get("/v1/cleanup/schedules", h.listSchedules)
		protected.Post("/v1/cleanup/schedules", h.createSchedule)
		protected.Put("/v1/cleanup/schedules/{id}", h.updateSchedule)
		protected.Delete("/v1/cleanup/schedules/{id}", h.deleteSchedule)
		protected.Post("/v1/cleanup/schedules/{id}/run", h.runSchedule)
		protected.Get("/v1/cleanup/cron/next", h.nextCron)
		protected.Get("/v1/cleanup/runs", h.listRuns)
		protected.Get("/v1/cleanup/runs/{id}", h.getRun)
		protected.Get("/v1/cleanup/exclusions", h.listExclusions)
		protected.Post("/v1/cleanup/exclusions", h.createExclusion)
		protected.Delete("/v1/cleanup/exclusions/{id}", h.deleteExclusion)
	})
}

type errorBody struct {
	Message string `json:"message"`
}

type categoryEnvelope struct {
	Categories []Category `json:"categories"`
}

type scanRequest struct {
	ServerIDs   []string `json:"server_ids"`
	MinAgeHours int      `json:"min_age_hours"`
}

type noticeResponse struct {
	Category string `json:"category"`
	Reason   string `json:"reason"`
}

type itemResponse struct {
	ID        string `json:"id"`
	Category  string `json:"category"`
	Tier      string `json:"tier"`
	Name      string `json:"name"`
	SizeBytes int64  `json:"size_bytes"`
	CreatedAt string `json:"created_at"`
	Reason    string `json:"reason"`
}

type nodeScanResponse struct {
	ServerID string           `json:"server_id"`
	Status   string           `json:"status"`
	Reason   string           `json:"reason"`
	Items    []itemResponse   `json:"items"`
	Notices  []noticeResponse `json:"notices"`
}

type scanEnvelope struct {
	Nodes []nodeScanResponse `json:"nodes"`
}

type cleanRequest struct {
	ServerID    string   `json:"server_id"`
	IDs         []string `json:"ids"`
	MinAgeHours int      `json:"min_age_hours"`
}

type cleanItemResponse struct {
	ID             string `json:"id"`
	Category       string `json:"category"`
	Name           string `json:"name"`
	Status         string `json:"status"`
	Message        string `json:"message"`
	ReclaimedBytes int64  `json:"reclaimed_bytes"`
}

type cleanEnvelope struct {
	ServerID       string              `json:"server_id"`
	Status         string              `json:"status"`
	Reason         string              `json:"reason"`
	RunID          string              `json:"run_id,omitempty"`
	ReclaimedBytes int64               `json:"reclaimed_bytes"`
	Items          []cleanItemResponse `json:"items"`
}

func (h *handler) categories(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, categoryEnvelope{Categories: Categories()})
}

func (h *handler) scan(w http.ResponseWriter, r *http.Request) {
	var req scanRequest
	if !decodeBody(w, r, &req) {
		return
	}
	if len(req.ServerIDs) < 1 || len(req.ServerIDs) > maxScanNodes {
		writeJSON(w, http.StatusBadRequest, errorBody{Message: "server_ids must contain 1 to 50 ids"})
		return
	}
	for _, raw := range req.ServerIDs {
		if _, err := uuid.Parse(raw); err != nil {
			writeJSON(w, http.StatusBadRequest, errorBody{Message: "invalid server id"})
			return
		}
	}
	if !validAgeHours(req.MinAgeHours) {
		writeJSON(w, http.StatusBadRequest, errorBody{Message: "min_age_hours must be between 0 and 8760"})
		return
	}

	scans := h.svc.Scan(r.Context(), req.ServerIDs, time.Duration(req.MinAgeHours)*time.Hour)
	nodes := make([]nodeScanResponse, 0, len(scans))
	for _, scan := range scans {
		nodes = append(nodes, scanNodeResponse(scan))
	}
	writeJSON(w, http.StatusOK, scanEnvelope{Nodes: nodes})
}

func (h *handler) clean(w http.ResponseWriter, r *http.Request) {
	var req cleanRequest
	if !decodeBody(w, r, &req) {
		return
	}
	if _, err := uuid.Parse(req.ServerID); err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody{Message: "invalid server id"})
		return
	}
	if len(req.IDs) < 1 || len(req.IDs) > maxCleanIDs {
		writeJSON(w, http.StatusBadRequest, errorBody{Message: "ids must contain 1 to 1000 ids"})
		return
	}
	for _, id := range req.IDs {
		if id == "" {
			writeJSON(w, http.StatusBadRequest, errorBody{Message: "ids must not be empty"})
			return
		}
	}
	if !validAgeHours(req.MinAgeHours) {
		writeJSON(w, http.StatusBadRequest, errorBody{Message: "min_age_hours must be between 0 and 8760"})
		return
	}
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}

	result := h.svc.Clean(
		r.Context(), actor, TriggerManual, req.ServerID, req.IDs,
		time.Duration(req.MinAgeHours)*time.Hour,
	)
	envelope := cleanEnvelope{
		ServerID: result.ServerID,
		Status:   result.Status,
		Reason:   nodeReason(result.Status, result.Reason),
		RunID:    result.RunID,
		Items:    make([]cleanItemResponse, 0, len(result.Items)),
	}
	for _, item := range result.Items {
		envelope.ReclaimedBytes += item.ReclaimedBytes
		envelope.Items = append(envelope.Items, cleanItemResponse(item))
	}
	writeJSON(w, http.StatusOK, envelope)
}

func (h *handler) actor(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	if h.userID == nil {
		writeJSON(w, http.StatusUnauthorized, errorBody{Message: "unauthorized"})
		return uuid.Nil, false
	}
	actor, ok := h.userID(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, errorBody{Message: "unauthorized"})
		return uuid.Nil, false
	}
	return actor, true
}

func scanNodeResponse(scan NodeScan) nodeScanResponse {
	response := nodeScanResponse{
		ServerID: scan.ServerID,
		Status:   scan.Status,
		Reason:   nodeReason(scan.Status, scan.Reason),
		Items:    make([]itemResponse, 0, len(scan.Items)),
		Notices:  []noticeResponse{},
	}
	for _, item := range scan.Items {
		createdAt := ""
		if item.CreatedUnix > 0 {
			createdAt = time.Unix(item.CreatedUnix, 0).UTC().Format(time.RFC3339)
		}
		response.Items = append(response.Items, itemResponse{
			ID:        item.ID,
			Category:  item.Category,
			Tier:      CategoryTier(item.Category),
			Name:      item.Name,
			SizeBytes: item.SizeBytes,
			CreatedAt: createdAt,
			Reason:    item.Reason,
		})
	}
	for _, notice := range scan.Notices {
		response.Notices = append(response.Notices, noticeResponse(notice))
	}
	return response
}

// nodeReason reports an unknown node as not_found instead of leaking the
// registry's internal error text.
func nodeReason(status, reason string) string {
	if status == StatusFailed && reason == servers.ErrNotFound.Error() {
		return ReasonNotFound
	}
	return reason
}

func validAgeHours(hours int) bool {
	return hours >= 0 && hours <= maxAgeHours
}

func decodeBody(w http.ResponseWriter, r *http.Request, dst any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	decoder := json.NewDecoder(r.Body)
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

type scheduleRequest struct {
	Name        string   `json:"name"`
	ServerIDs   []string `json:"server_ids"`
	Categories  []string `json:"categories"`
	Cron        string   `json:"cron"`
	MinAgeHours int      `json:"min_age_hours"`
	Enabled     *bool    `json:"enabled"`
}

type scheduleResponse struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	ServerIDs   []string `json:"server_ids"`
	Categories  []string `json:"categories"`
	Cron        string   `json:"cron"`
	MinAgeHours int      `json:"min_age_hours"`
	Enabled     bool     `json:"enabled"`
	NextRunAt   string   `json:"next_run_at"`
	LastRunAt   string   `json:"last_run_at,omitempty"`
	CreatedAt   string   `json:"created_at"`
	UpdatedAt   string   `json:"updated_at"`
}

type scheduleEnvelope struct {
	Schedule scheduleResponse `json:"schedule"`
}

type scheduleListEnvelope struct {
	Schedules []scheduleResponse `json:"schedules"`
}

type runResponse struct {
	ID             string           `json:"id"`
	ServerID       string           `json:"server_id"`
	Trigger        string           `json:"trigger"`
	ScheduleID     string           `json:"schedule_id,omitempty"`
	ScheduleName   string           `json:"schedule_name,omitempty"`
	Actor          string           `json:"actor,omitempty"`
	StartedAt      string           `json:"started_at"`
	FinishedAt     string           `json:"finished_at"`
	ReclaimedBytes int64            `json:"reclaimed_bytes"`
	Status         string           `json:"status"`
	Reason         string           `json:"reason,omitempty"`
	Categories     map[string]int64 `json:"categories"`
}

type runDetailResponse struct {
	runResponse
	Items []cleanItemResponse `json:"items"`
}

type runListEnvelope struct {
	Runs []runResponse `json:"runs"`
}

type runEnvelope struct {
	Run runDetailResponse `json:"run"`
}

type exclusionRequest struct {
	ServerID string `json:"server_id"`
	Category string `json:"category"`
	Match    string `json:"match"`
	Note     string `json:"note"`
}

type exclusionResponse struct {
	ID        string `json:"id"`
	ServerID  string `json:"server_id,omitempty"`
	Category  string `json:"category"`
	Match     string `json:"match"`
	Note      string `json:"note"`
	CreatedAt string `json:"created_at"`
}

type nextRunEnvelope struct {
	NextRunAt string `json:"next_run_at"`
}

type exclusionListEnvelope struct {
	Exclusions []exclusionResponse `json:"exclusions"`
}

type exclusionEnvelope struct {
	Exclusion exclusionResponse `json:"exclusion"`
}

func (h *handler) listSchedules(w http.ResponseWriter, r *http.Request) {
	schedules, err := h.center.ListSchedules(r.Context())
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

func (h *handler) createSchedule(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	input, ok := h.scheduleInput(w, r)
	if !ok {
		return
	}
	schedule, err := h.center.CreateSchedule(r.Context(), actor, input)
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, scheduleEnvelope{Schedule: newScheduleResponse(schedule)})
}

func (h *handler) updateSchedule(w http.ResponseWriter, r *http.Request) {
	id, ok := h.paramUUID(w, r)
	if !ok {
		return
	}
	input, ok := h.scheduleInput(w, r)
	if !ok {
		return
	}
	schedule, err := h.center.UpdateSchedule(r.Context(), id, input)
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, scheduleEnvelope{Schedule: newScheduleResponse(schedule)})
}

func (h *handler) deleteSchedule(w http.ResponseWriter, r *http.Request) {
	id, ok := h.paramUUID(w, r)
	if !ok {
		return
	}
	if err := h.center.DeleteSchedule(r.Context(), id); err != nil {
		h.writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *handler) runSchedule(w http.ResponseWriter, r *http.Request) {
	id, ok := h.paramUUID(w, r)
	if !ok {
		return
	}
	if err := h.center.RunScheduleNow(r.Context(), id); err != nil {
		h.writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

func (h *handler) nextCron(w http.ResponseWriter, r *http.Request) {
	next, err := h.center.NextCronRun(r.URL.Query().Get("expr"))
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, nextRunEnvelope{NextRunAt: next.UTC().Format(time.RFC3339)})
}

func (h *handler) scheduleInput(w http.ResponseWriter, r *http.Request) (ScheduleInput, bool) {
	var body scheduleRequest
	if !decodeBody(w, r, &body) {
		return ScheduleInput{}, false
	}
	serverIDs := make([]uuid.UUID, 0, len(body.ServerIDs))
	for _, raw := range body.ServerIDs {
		id, err := uuid.Parse(raw)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, errorBody{Message: "invalid server id"})
			return ScheduleInput{}, false
		}
		serverIDs = append(serverIDs, id)
	}
	return ScheduleInput{
		Name:        body.Name,
		ServerIDs:   serverIDs,
		Categories:  body.Categories,
		Cron:        body.Cron,
		MinAgeHours: body.MinAgeHours,
		Enabled:     body.Enabled,
	}, true
}

func newScheduleResponse(schedule store.CleanupSchedule) scheduleResponse {
	serverIDs := make([]string, 0, len(schedule.ServerIDs))
	for _, id := range schedule.ServerIDs {
		serverIDs = append(serverIDs, id.String())
	}
	response := scheduleResponse{
		ID:          schedule.ID.String(),
		Name:        schedule.Name,
		ServerIDs:   serverIDs,
		Categories:  append([]string{}, schedule.Categories...),
		Cron:        schedule.Cron,
		MinAgeHours: schedule.MinAgeHours,
		Enabled:     schedule.Enabled,
		NextRunAt:   schedule.NextRunAt.UTC().Format(time.RFC3339),
		CreatedAt:   schedule.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:   schedule.UpdatedAt.UTC().Format(time.RFC3339),
	}
	if schedule.LastRunAt != nil {
		response.LastRunAt = schedule.LastRunAt.UTC().Format(time.RFC3339)
	}
	return response
}

func (h *handler) listRuns(w http.ResponseWriter, r *http.Request) {
	var filter store.CleanupRunFilter
	if raw := r.URL.Query().Get("server_id"); raw != "" {
		serverID, err := uuid.Parse(raw)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, errorBody{Message: "invalid server id"})
			return
		}
		filter.ServerID = &serverID
	}
	if raw := r.URL.Query().Get("before"); raw != "" {
		before, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, errorBody{Message: "before must be RFC3339"})
			return
		}
		filter.Before = &before
	}
	if raw := r.URL.Query().Get("limit"); raw != "" {
		limit, err := strconv.Atoi(raw)
		if err != nil || limit < 1 || limit > 100 {
			writeJSON(w, http.StatusBadRequest, errorBody{Message: "limit must be between 1 and 100"})
			return
		}
		filter.Limit = limit
	}
	runs, err := h.center.ListRuns(r.Context(), filter)
	if err != nil {
		h.writeError(w, err)
		return
	}
	response := make([]runResponse, 0, len(runs))
	for _, run := range runs {
		response = append(response, newRunResponse(run))
	}
	writeJSON(w, http.StatusOK, runListEnvelope{Runs: response})
}

func (h *handler) getRun(w http.ResponseWriter, r *http.Request) {
	id, ok := h.paramUUID(w, r)
	if !ok {
		return
	}
	run, items, err := h.center.GetRun(r.Context(), id)
	if err != nil {
		h.writeError(w, err)
		return
	}
	detail := runDetailResponse{
		runResponse: newRunResponse(run),
		Items:       make([]cleanItemResponse, 0, len(items)),
	}
	for _, item := range items {
		detail.Items = append(detail.Items, cleanItemResponse{
			ID:             item.ItemID,
			Category:       item.Category,
			Name:           item.Name,
			Status:         item.Status,
			Message:        item.Message,
			ReclaimedBytes: item.ReclaimedBytes,
		})
	}
	writeJSON(w, http.StatusOK, runEnvelope{Run: detail})
}

func newRunResponse(run store.CleanupRunView) runResponse {
	response := runResponse{
		ID:             run.ID.String(),
		ServerID:       run.ServerID.String(),
		Trigger:        run.Trigger,
		ScheduleName:   run.ScheduleName,
		Actor:          run.ActorEmail,
		StartedAt:      run.StartedAt.UTC().Format(time.RFC3339Nano),
		FinishedAt:     run.FinishedAt.UTC().Format(time.RFC3339Nano),
		ReclaimedBytes: run.ReclaimedBytes,
		Status:         run.Status,
		Reason:         run.Reason,
		Categories:     map[string]int64{},
	}
	if run.ScheduleID != nil {
		response.ScheduleID = run.ScheduleID.String()
	}
	for category, bytes := range run.Categories {
		response.Categories[category] = bytes
	}
	return response
}

func (h *handler) listExclusions(w http.ResponseWriter, r *http.Request) {
	exclusions, err := h.center.ListExclusions(r.Context())
	if err != nil {
		h.writeError(w, err)
		return
	}
	response := make([]exclusionResponse, 0, len(exclusions))
	for _, exclusion := range exclusions {
		response = append(response, newExclusionResponse(exclusion))
	}
	writeJSON(w, http.StatusOK, exclusionListEnvelope{Exclusions: response})
}

func (h *handler) createExclusion(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	var body exclusionRequest
	if !decodeBody(w, r, &body) {
		return
	}
	input := ExclusionInput{Category: body.Category, Match: body.Match, Note: body.Note}
	if body.ServerID != "" {
		serverID, err := uuid.Parse(body.ServerID)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, errorBody{Message: "invalid server id"})
			return
		}
		input.ServerID = &serverID
	}
	exclusion, err := h.center.CreateExclusion(r.Context(), actor, input)
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, exclusionEnvelope{Exclusion: newExclusionResponse(exclusion)})
}

func (h *handler) deleteExclusion(w http.ResponseWriter, r *http.Request) {
	id, ok := h.paramUUID(w, r)
	if !ok {
		return
	}
	if err := h.center.DeleteExclusion(r.Context(), id); err != nil {
		h.writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func newExclusionResponse(exclusion store.CleanupExclusion) exclusionResponse {
	response := exclusionResponse{
		ID:        exclusion.ID.String(),
		Category:  exclusion.Category,
		Match:     exclusion.Match,
		Note:      exclusion.Note,
		CreatedAt: exclusion.CreatedAt.UTC().Format(time.RFC3339),
	}
	if exclusion.ServerID != nil {
		response.ServerID = exclusion.ServerID.String()
	}
	return response
}

func (h *handler) paramUUID(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody{Message: "invalid id"})
		return uuid.Nil, false
	}
	return id, true
}

func (h *handler) writeError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrValidation), errors.Is(err, ErrInvalidCategory),
		errors.Is(err, ErrDangerousCategory), errors.Is(err, ErrInvalidServerID):
		writeJSON(w, http.StatusBadRequest, errorBody{Message: err.Error()})
	case errors.Is(err, ErrNotFound):
		writeJSON(w, http.StatusNotFound, errorBody{Message: "not found"})
	case errors.Is(err, ErrScheduleRunning):
		writeJSON(w, http.StatusConflict, errorBody{Message: err.Error()})
	case errors.Is(err, ErrUnavailable):
		writeJSON(w, http.StatusServiceUnavailable, errorBody{Message: err.Error()})
	default:
		writeJSON(w, http.StatusInternalServerError, errorBody{Message: "cleanup request failed"})
	}
}
