package databases

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// fakeBackupService is a scriptable BackupService for route tests: one error
// and one canned answer for every method, plus the last call it saw.
type fakeBackupService struct {
	backup    Backup
	backups   []Backup
	schedule  BackupSchedule
	schedules []BackupSchedule
	target    BackupTarget
	targets   []BackupTarget
	restore   RestoreResult
	restores  []Restore
	check     TargetCheck

	err   error
	calls []string
	// request echoes the payload of the last mutating call.
	createBackupReq CreateBackupRequest
	restoreReq      RestoreRequest
	createSchedule  ScheduleRequest
	targetReq       TargetRequest
	// listLimit echoes the page size the last list call carried.
	listLimit int
}

// Compile-time guarantee.
var _ BackupService = (*fakeBackupService)(nil)

func (f *fakeBackupService) record(call string) {
	f.calls = append(f.calls, call)
}

func (f *fakeBackupService) CreateBackup(_ context.Context, _, _ uuid.UUID, req CreateBackupRequest) (Backup, error) {
	f.record("createBackup")
	f.createBackupReq = req
	if f.err != nil {
		return Backup{}, f.err
	}
	return f.backup, nil
}

func (f *fakeBackupService) ListBackups(_ context.Context, _, _ uuid.UUID, limit int) ([]Backup, error) {
	f.record("listBackups")
	f.listLimit = limit
	if f.err != nil {
		return nil, f.err
	}
	return f.backups, nil
}

func (f *fakeBackupService) GetBackup(_ context.Context, _, _, _ uuid.UUID) (Backup, error) {
	f.record("getBackup")
	if f.err != nil {
		return Backup{}, f.err
	}
	return f.backup, nil
}

func (f *fakeBackupService) DeleteBackup(_ context.Context, _, _, _ uuid.UUID) error {
	f.record("deleteBackup")
	return f.err
}

func (f *fakeBackupService) RestoreBackup(_ context.Context, _, _ uuid.UUID, req RestoreRequest) (RestoreResult, error) {
	f.record("restoreBackup")
	f.restoreReq = req
	if f.err != nil {
		return RestoreResult{}, f.err
	}
	return f.restore, nil
}

func (f *fakeBackupService) ListRestores(_ context.Context, _, _ uuid.UUID, limit int) ([]Restore, error) {
	f.record("listRestores")
	f.listLimit = limit
	if f.err != nil {
		return nil, f.err
	}
	return f.restores, nil
}

func (f *fakeBackupService) ListSchedules(_ context.Context, _, _ uuid.UUID) ([]BackupSchedule, error) {
	f.record("listSchedules")
	if f.err != nil {
		return nil, f.err
	}
	return f.schedules, nil
}

func (f *fakeBackupService) CreateSchedule(_ context.Context, _, _ uuid.UUID, req ScheduleRequest) (BackupSchedule, error) {
	f.record("createSchedule")
	f.createSchedule = req
	if f.err != nil {
		return BackupSchedule{}, f.err
	}
	return f.schedule, nil
}

func (f *fakeBackupService) UpdateSchedule(_ context.Context, _, _, _ uuid.UUID, req ScheduleRequest) (BackupSchedule, error) {
	f.record("updateSchedule")
	f.createSchedule = req
	if f.err != nil {
		return BackupSchedule{}, f.err
	}
	return f.schedule, nil
}

func (f *fakeBackupService) DeleteSchedule(_ context.Context, _, _, _ uuid.UUID) error {
	f.record("deleteSchedule")
	return f.err
}

func (f *fakeBackupService) ListTargets(_ context.Context, _ uuid.UUID) ([]BackupTarget, error) {
	f.record("listTargets")
	if f.err != nil {
		return nil, f.err
	}
	return f.targets, nil
}

func (f *fakeBackupService) CreateTarget(_ context.Context, _ uuid.UUID, req TargetRequest) (BackupTarget, error) {
	f.record("createTarget")
	f.targetReq = req
	if f.err != nil {
		return BackupTarget{}, f.err
	}
	return f.target, nil
}

func (f *fakeBackupService) UpdateTarget(_ context.Context, _, _ uuid.UUID, req TargetRequest) (BackupTarget, error) {
	f.record("updateTarget")
	f.targetReq = req
	if f.err != nil {
		return BackupTarget{}, f.err
	}
	return f.target, nil
}

func (f *fakeBackupService) DeleteTarget(_ context.Context, _, _ uuid.UUID) error {
	f.record("deleteTarget")
	return f.err
}

func (f *fakeBackupService) TestTarget(_ context.Context, _, _ uuid.UUID) (TargetCheck, error) {
	f.record("testTarget")
	if f.err != nil {
		return TargetCheck{}, f.err
	}
	return f.check, nil
}

func (f *fakeBackupService) Close() error {
	f.record("close")
	return nil
}

// passthroughUser stands in for the server's UserIDFromContext.
func passthroughUser(_ context.Context) (uuid.UUID, bool) { return uuid.Nil, false }

// newBackupRouter mounts the backup routes behind a stub auth middleware.
func newBackupRouter(svc BackupService, user uuid.UUID, present bool) http.Handler {
	r := chi.NewRouter()
	MountBackups(r, passthroughAuth, passthroughAuth, func(context.Context) (uuid.UUID, bool) {
		if !present {
			return uuid.Nil, false
		}
		return user, true
	}, svc)
	return r
}

// backupRequest issues one request against the router.
func backupRequest(t *testing.T, handler http.Handler, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	var reader *strings.Reader
	if body == "" {
		reader = strings.NewReader("")
	} else {
		reader = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, reader)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func TestMountBackupsWithoutServiceMountsNothing(t *testing.T) {
	r := chi.NewRouter()
	MountBackups(r, passthroughAuth, passthroughAuth, passthroughUser, nil)

	id := uuid.New()
	rec := backupRequest(t, r, http.MethodPost, "/v1/databases/"+id.String()+"/backup", "")
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404 (no routes mounted)", rec.Code)
	}
}

func TestBackupRoutesRequireAuthentication(t *testing.T) {
	svc := &fakeBackupService{}
	handler := newBackupRouter(svc, uuid.New(), false)
	id := uuid.New()

	for _, request := range []struct{ method, path, body string }{
		{http.MethodPost, "/v1/databases/" + id.String() + "/backup", ""},
		{http.MethodGet, "/v1/databases/" + id.String() + "/backups", ""},
		{http.MethodPost, "/v1/databases/" + id.String() + "/restore", `{"backup_id":"` + uuid.New().String() + `"}`},
		{http.MethodGet, "/v1/databases/backup-targets", ""},
	} {
		rec := backupRequest(t, handler, request.method, request.path, request.body)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s %s → %d, want 401", request.method, request.path, rec.Code)
		}
	}
	if len(svc.calls) != 0 {
		t.Errorf("service was called without authentication: %v", svc.calls)
	}
}

func TestCreateBackupRoute(t *testing.T) {
	user := uuid.New()
	id := uuid.New()
	backupID := uuid.New()
	svc := &fakeBackupService{backup: Backup{
		ID:         backupID,
		DatabaseID: id,
		Type:       BackupManual,
		Status:     BackupRunning,
		CreatedAt:  time.Now().UTC(),
	}}
	handler := newBackupRouter(svc, user, true)

	rec := backupRequest(t, handler, http.MethodPost, "/v1/databases/"+id.String()+"/backup", "")
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}
	var envelope struct {
		Backup backupResponse `json:"backup"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if envelope.Backup.ID != backupID.String() || envelope.Backup.Status != BackupRunning {
		t.Errorf("response = %+v", envelope.Backup)
	}

	// An optional target id travels in the body.
	targetID := uuid.New().String()
	rec = backupRequest(t, handler, http.MethodPost, "/v1/databases/"+id.String()+"/backup",
		`{"target_id":"`+targetID+`"}`)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}
	if svc.createBackupReq.TargetID.String() != targetID {
		t.Errorf("target id = %s, want %s", svc.createBackupReq.TargetID, targetID)
	}

	// A malformed body is rejected before the service is called.
	rec = backupRequest(t, handler, http.MethodPost, "/v1/databases/"+id.String()+"/backup", `{"nope":1}`)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}

	// A malformed database id is a 400 without touching the service.
	rec = backupRequest(t, handler, http.MethodPost, "/v1/databases/not-a-uuid/backup", "")
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

func TestBackupRouteErrorMapping(t *testing.T) {
	user := uuid.New()
	id := uuid.New()
	tests := []struct {
		name string
		err  error
		want int
	}{
		{"not found", ErrNotFound, http.StatusNotFound},
		{"server not found", ErrServerNotFound, http.StatusNotFound},
		{"validation", ErrValidation, http.StatusBadRequest},
		{"already running", ErrBackupInFlight, http.StatusConflict},
		{"not restorable", ErrBackupNotCompleted, http.StatusConflict},
		{"disabled", ErrDisabled, http.StatusServiceUnavailable},
		{"agent unavailable", ErrAgentUnavailable, http.StatusBadGateway},
		{"internal", errors.New("boom"), http.StatusInternalServerError},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			svc := &fakeBackupService{err: test.err}
			handler := newBackupRouter(svc, user, true)
			rec := backupRequest(t, handler, http.MethodPost, "/v1/databases/"+id.String()+"/backup", "")
			if rec.Code != test.want {
				t.Errorf("status = %d, want %d", rec.Code, test.want)
			}
		})
	}
}

func TestRestoreRoute(t *testing.T) {
	user := uuid.New()
	id := uuid.New()
	backupID := uuid.New()
	svc := &fakeBackupService{restore: RestoreResult{
		BackupID:   backupID,
		DatabaseID: id,
		Status:     BackupRunning,
	}}
	handler := newBackupRouter(svc, user, true)

	rec := backupRequest(t, handler, http.MethodPost, "/v1/databases/"+id.String()+"/restore",
		`{"backup_id":"`+backupID.String()+`"}`)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}
	var envelope struct {
		Restore RestoreResult `json:"restore"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if envelope.Restore.BackupID != backupID || envelope.Restore.Status != BackupRunning {
		t.Errorf("response = %+v", envelope.Restore)
	}
	if svc.restoreReq.BackupID != backupID {
		t.Errorf("service got backup id %s", svc.restoreReq.BackupID)
	}

	if rec := backupRequest(t, handler, http.MethodPost, "/v1/databases/"+id.String()+"/restore", ""); rec.Code != http.StatusBadRequest {
		t.Errorf("missing body → %d, want 400", rec.Code)
	}
	if rec := backupRequest(t, handler, http.MethodPost, "/v1/databases/"+id.String()+"/restore",
		`{"backup_id":"nope"}`); rec.Code != http.StatusBadRequest {
		t.Errorf("invalid backup id → %d, want 400", rec.Code)
	}
}

// TestListRestoresRoute is the D3-7 regression: the durable restore rows are
// exposed so a client can follow a queued restore to its terminal state.
func TestListRestoresRoute(t *testing.T) {
	user := uuid.New()
	id := uuid.New()
	backupID := uuid.New()
	finished := time.Now().UTC()
	svc := &fakeBackupService{restores: []Restore{
		{ID: uuid.New(), DatabaseID: id, BackupID: backupID, Status: RestoreCompleted, CreatedAt: finished, FinishedAt: finished},
		{ID: uuid.New(), DatabaseID: id, BackupID: backupID, Status: RestoreFailed, Error: "apply failed", CreatedAt: finished},
	}}
	handler := newBackupRouter(svc, user, true)

	rec := backupRequest(t, handler, http.MethodGet, "/v1/databases/"+id.String()+"/restores?limit=5", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}
	var envelope struct {
		Restores []restoreResponse `json:"restores"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(envelope.Restores) != 2 {
		t.Fatalf("restores = %d, want 2", len(envelope.Restores))
	}
	if envelope.Restores[0].Status != RestoreCompleted || envelope.Restores[0].BackupID != backupID.String() {
		t.Errorf("first restore = %+v", envelope.Restores[0])
	}
	if envelope.Restores[1].Status != RestoreFailed || envelope.Restores[1].Error != "apply failed" {
		t.Errorf("second restore = %+v", envelope.Restores[1])
	}
	if envelope.Restores[0].FinishedAt == nil {
		t.Error("completed restore should carry finished_at")
	}
	if svc.listLimit != 5 {
		t.Errorf("limit = %d, want 5", svc.listLimit)
	}

	if rec := backupRequest(t, handler, http.MethodGet, "/v1/databases/"+id.String()+"/restores?limit=-1", ""); rec.Code != http.StatusBadRequest {
		t.Errorf("negative limit → %d, want 400", rec.Code)
	}
}

func TestBackupListGetDeleteRoutes(t *testing.T) {
	user := uuid.New()
	id := uuid.New()
	backupID := uuid.New()
	svc := &fakeBackupService{
		backup: Backup{ID: backupID, DatabaseID: id, Status: BackupCompleted, Size: 42,
			Location: "s3://bucket/key", CreatedAt: time.Now().UTC(), FinishedAt: time.Now().UTC()},
		backups: []Backup{{ID: backupID, DatabaseID: id, Status: BackupCompleted}},
	}
	handler := newBackupRouter(svc, user, true)

	rec := backupRequest(t, handler, http.MethodGet, "/v1/databases/"+id.String()+"/backups", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("list status = %d", rec.Code)
	}
	var list struct {
		Backups []backupResponse `json:"backups"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil || len(list.Backups) != 1 {
		t.Fatalf("list = %s (err %v)", rec.Body, err)
	}

	rec = backupRequest(t, handler, http.MethodGet, "/v1/databases/"+id.String()+"/backups/"+backupID.String(), "")
	if rec.Code != http.StatusOK {
		t.Errorf("get status = %d", rec.Code)
	}

	// ?limit= reaches the service; a non-numeric limit is a 400.
	rec = backupRequest(t, handler, http.MethodGet, "/v1/databases/"+id.String()+"/backups?limit=7", "")
	if rec.Code != http.StatusOK {
		t.Errorf("limited list status = %d", rec.Code)
	}
	if svc.listLimit != 7 {
		t.Errorf("service limit = %d, want 7", svc.listLimit)
	}
	rec = backupRequest(t, handler, http.MethodGet, "/v1/databases/"+id.String()+"/backups?limit=abc", "")
	if rec.Code != http.StatusBadRequest {
		t.Errorf("invalid limit status = %d, want 400", rec.Code)
	}

	rec = backupRequest(t, handler, http.MethodDelete, "/v1/databases/"+id.String()+"/backups/"+backupID.String(), "")
	if rec.Code != http.StatusNoContent {
		t.Errorf("delete status = %d", rec.Code)
	}

	rec = backupRequest(t, handler, http.MethodGet, "/v1/databases/"+id.String()+"/backups/nope", "")
	if rec.Code != http.StatusBadRequest {
		t.Errorf("invalid backup id → %d, want 400", rec.Code)
	}
}

func TestScheduleRoutes(t *testing.T) {
	user := uuid.New()
	id := uuid.New()
	scheduleID := uuid.New()
	now := time.Now().UTC()
	svc := &fakeBackupService{
		schedule: BackupSchedule{ID: scheduleID, DatabaseID: id, Cron: "0 2 * * *",
			Enabled: true, NextRunAt: now.Add(time.Hour), CreatedAt: now, UpdatedAt: now},
		schedules: []BackupSchedule{{ID: scheduleID, DatabaseID: id, Cron: "0 2 * * *"}},
	}
	handler := newBackupRouter(svc, user, true)

	rec := backupRequest(t, handler, http.MethodPost, "/v1/databases/"+id.String()+"/schedules",
		`{"cron":"0 2 * * *"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status = %d, body = %s", rec.Code, rec.Body)
	}
	if svc.createSchedule.Cron != "0 2 * * *" {
		t.Errorf("service got cron %q", svc.createSchedule.Cron)
	}

	rec = backupRequest(t, handler, http.MethodGet, "/v1/databases/"+id.String()+"/schedules", "")
	if rec.Code != http.StatusOK {
		t.Errorf("list status = %d", rec.Code)
	}

	rec = backupRequest(t, handler, http.MethodPatch, "/v1/databases/"+id.String()+"/schedules/"+scheduleID.String(),
		`{"cron":"0 */6 * * *","enabled":false}`)
	if rec.Code != http.StatusOK {
		t.Errorf("update status = %d, body = %s", rec.Code, rec.Body)
	}
	if svc.createSchedule.Cron != "0 */6 * * *" || svc.createSchedule.Enabled == nil || *svc.createSchedule.Enabled {
		t.Errorf("service got %+v", svc.createSchedule)
	}

	rec = backupRequest(t, handler, http.MethodDelete, "/v1/databases/"+id.String()+"/schedules/"+scheduleID.String(), "")
	if rec.Code != http.StatusNoContent {
		t.Errorf("delete status = %d", rec.Code)
	}

	// An invalid cron expression is reported as 400 by the service.
	svc.err = ErrValidation
	rec = backupRequest(t, handler, http.MethodPost, "/v1/databases/"+id.String()+"/schedules", `{"cron":"nope"}`)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("invalid cron → %d, want 400", rec.Code)
	}
}

// TestScheduleRouteTargetClearWireSemantics pins the JSON tri-state of
// target_id on the update endpoint: an absent field arrives as nil (keep), an
// empty string as a pointer to "" (clear) and a uuid as that id (replace).
func TestScheduleRouteTargetClearWireSemantics(t *testing.T) {
	user := uuid.New()
	id := uuid.New()
	scheduleID := uuid.New()
	targetID := uuid.New()
	svc := &fakeBackupService{schedule: BackupSchedule{ID: scheduleID, DatabaseID: id, Cron: "0 2 * * *"}}
	handler := newBackupRouter(svc, user, true)
	path := "/v1/databases/" + id.String() + "/schedules/" + scheduleID.String()

	rec := backupRequest(t, handler, http.MethodPatch, path, `{"cron":"0 2 * * *"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("absent target_id: status = %d, body = %s", rec.Code, rec.Body)
	}
	if svc.createSchedule.TargetID != nil {
		t.Fatalf("absent target_id decoded to %q, want nil (keep)", *svc.createSchedule.TargetID)
	}

	rec = backupRequest(t, handler, http.MethodPatch, path, `{"cron":"0 2 * * *","target_id":""}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("empty target_id: status = %d, body = %s", rec.Code, rec.Body)
	}
	if svc.createSchedule.TargetID == nil || *svc.createSchedule.TargetID != "" {
		t.Fatal("empty target_id did not decode as an explicit clear")
	}

	rec = backupRequest(t, handler, http.MethodPatch, path, `{"cron":"0 2 * * *","target_id":"`+targetID.String()+`"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("uuid target_id: status = %d, body = %s", rec.Code, rec.Body)
	}
	if svc.createSchedule.TargetID == nil || *svc.createSchedule.TargetID != targetID.String() {
		t.Fatalf("uuid target_id decoded to %v, want %s", svc.createSchedule.TargetID, targetID)
	}
}

func TestTargetRoutesNeverExposeCredentials(t *testing.T) {
	user := uuid.New()
	targetID := uuid.New()
	now := time.Now().UTC()
	svc := &fakeBackupService{
		target: BackupTarget{ID: targetID, UserID: user, Name: "r2", Kind: TargetS3,
			Endpoint: "https://account.r2.cloudflarestorage.com", Bucket: "gotham-backups",
			CreatedAt: now, UpdatedAt: now},
		targets: []BackupTarget{{ID: targetID, Name: "r2", Kind: TargetS3}},
		check:   TargetCheck{OK: true, Message: "connected to gotham-backups"},
	}
	handler := newBackupRouter(svc, user, true)

	rec := backupRequest(t, handler, http.MethodPost, "/v1/databases/backup-targets",
		`{"name":"r2","kind":"s3","endpoint":"https://e","bucket":"b","access_key":"AK","secret_key":"SK"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status = %d, body = %s", rec.Code, rec.Body)
	}
	// The service received the credentials so it can seal them…
	if svc.targetReq.AccessKey != "AK" || svc.targetReq.SecretKey != "SK" {
		t.Errorf("service got credentials %q / %q", svc.targetReq.AccessKey, svc.targetReq.SecretKey)
	}
	// …but the response must not carry them in any form.
	body := rec.Body.String()
	for _, forbidden := range []string{"AK", "SK", "access_key", "secret_key"} {
		if strings.Contains(body, forbidden) {
			t.Errorf("response leaks %q: %s", forbidden, body)
		}
	}
	var envelope struct {
		Target targetResponse `json:"target"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !envelope.Target.HasCredentials {
		t.Error("has_credentials must be true for an s3 target")
	}

	rec = backupRequest(t, handler, http.MethodGet, "/v1/databases/backup-targets", "")
	if rec.Code != http.StatusOK {
		t.Errorf("list status = %d", rec.Code)
	}
	rec = backupRequest(t, handler, http.MethodPatch, "/v1/databases/backup-targets/"+targetID.String(),
		`{"name":"r2-renamed"}`)
	if rec.Code != http.StatusOK {
		t.Errorf("update status = %d", rec.Code)
	}
	rec = backupRequest(t, handler, http.MethodPost, "/v1/databases/backup-targets/"+targetID.String()+"/test", "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "connected to") {
		t.Errorf("test status = %d, body = %s", rec.Code, rec.Body)
	}
	rec = backupRequest(t, handler, http.MethodDelete, "/v1/databases/backup-targets/"+targetID.String(), "")
	if rec.Code != http.StatusNoContent {
		t.Errorf("delete status = %d", rec.Code)
	}
}

func TestTargetRoutesOwnershipErrors(t *testing.T) {
	user := uuid.New()
	targetID := uuid.New()
	svc := &fakeBackupService{err: ErrNotFound}
	handler := newBackupRouter(svc, user, true)

	for _, request := range []struct{ method, path, body string }{
		{http.MethodPatch, "/v1/databases/backup-targets/" + targetID.String(), `{"name":"x"}`},
		{http.MethodDelete, "/v1/databases/backup-targets/" + targetID.String(), ""},
		{http.MethodPost, "/v1/databases/backup-targets/" + targetID.String() + "/test", ""},
		{http.MethodPost, "/v1/databases/backup-targets", `{"name":"x","kind":"local"}`},
	} {
		rec := backupRequest(t, handler, request.method, request.path, request.body)
		if rec.Code != http.StatusNotFound {
			t.Errorf("%s %s → %d, want 404", request.method, request.path, rec.Code)
		}
	}
}
