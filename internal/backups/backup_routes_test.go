package backups

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

type fakeBackupService struct {
	backup    Backup
	backups   []Backup
	schedule  BackupSchedule
	schedules []BackupSchedule
	provider  BackupTarget
	providers []BackupTarget
	restore   RestoreResult
	restores  []Restore
	check     TargetCheck

	err             error
	calls           []string
	createBackupReq CreateBackupRequest
	createSchedule  ScheduleRequest
	updateSchedule  ScheduleRequest
	providerReq     ProviderRequest
	backupFilter    BackupFilter
	restoreFilter   RestoreFilter
	scheduleRef     ResourceRef
}

var _ BackupService = (*fakeBackupService)(nil)

func (f *fakeBackupService) record(call string) {
	f.calls = append(f.calls, call)
}

func (f *fakeBackupService) CreateBackup(_ context.Context, _ uuid.UUID, req CreateBackupRequest) (Backup, error) {
	f.record("createBackup")
	f.createBackupReq = req
	if f.err != nil {
		return Backup{}, f.err
	}
	return f.backup, nil
}

func (f *fakeBackupService) ListBackups(_ context.Context, _ uuid.UUID, filter BackupFilter) ([]Backup, error) {
	f.record("listBackups")
	f.backupFilter = filter
	if f.err != nil {
		return nil, f.err
	}
	return f.backups, nil
}

func (f *fakeBackupService) GetBackup(context.Context, uuid.UUID, uuid.UUID) (Backup, error) {
	f.record("getBackup")
	if f.err != nil {
		return Backup{}, f.err
	}
	return f.backup, nil
}

func (f *fakeBackupService) DeleteBackup(context.Context, uuid.UUID, uuid.UUID) error {
	f.record("deleteBackup")
	return f.err
}

func (f *fakeBackupService) RestoreBackup(context.Context, uuid.UUID, uuid.UUID) (RestoreResult, error) {
	f.record("restoreBackup")
	if f.err != nil {
		return RestoreResult{}, f.err
	}
	return f.restore, nil
}

func (f *fakeBackupService) ListRestores(_ context.Context, _ uuid.UUID, filter RestoreFilter) ([]Restore, error) {
	f.record("listRestores")
	f.restoreFilter = filter
	if f.err != nil {
		return nil, f.err
	}
	return f.restores, nil
}

func (f *fakeBackupService) ListSchedules(_ context.Context, _ uuid.UUID, ref ResourceRef) ([]BackupSchedule, error) {
	f.record("listSchedules")
	f.scheduleRef = ref
	if f.err != nil {
		return nil, f.err
	}
	return f.schedules, nil
}

func (f *fakeBackupService) CreateSchedule(_ context.Context, _ uuid.UUID, req ScheduleRequest) (BackupSchedule, error) {
	f.record("createSchedule")
	f.createSchedule = req
	if f.err != nil {
		return BackupSchedule{}, f.err
	}
	return f.schedule, nil
}

func (f *fakeBackupService) UpdateSchedule(_ context.Context, _ uuid.UUID, _ uuid.UUID, req ScheduleRequest) (BackupSchedule, error) {
	f.record("updateSchedule")
	f.updateSchedule = req
	if f.err != nil {
		return BackupSchedule{}, f.err
	}
	return f.schedule, nil
}

func (f *fakeBackupService) DeleteSchedule(context.Context, uuid.UUID, uuid.UUID) error {
	f.record("deleteSchedule")
	return f.err
}

func (f *fakeBackupService) ListProviders(context.Context, uuid.UUID) ([]BackupTarget, error) {
	f.record("listProviders")
	if f.err != nil {
		return nil, f.err
	}
	return f.providers, nil
}

func (f *fakeBackupService) CreateProvider(_ context.Context, _ uuid.UUID, req ProviderRequest) (BackupTarget, error) {
	f.record("createProvider")
	f.providerReq = req
	if f.err != nil {
		return BackupTarget{}, f.err
	}
	return f.provider, nil
}

func (f *fakeBackupService) UpdateProvider(_ context.Context, _ uuid.UUID, _ uuid.UUID, req ProviderRequest) (BackupTarget, error) {
	f.record("updateProvider")
	f.providerReq = req
	if f.err != nil {
		return BackupTarget{}, f.err
	}
	return f.provider, nil
}

func (f *fakeBackupService) DeleteProvider(context.Context, uuid.UUID, uuid.UUID) error {
	f.record("deleteProvider")
	return f.err
}

func (f *fakeBackupService) TestProvider(context.Context, uuid.UUID, uuid.UUID) (TargetCheck, error) {
	f.record("testProvider")
	if f.err != nil {
		return TargetCheck{}, f.err
	}
	return f.check, nil
}

func (f *fakeBackupService) Close() error {
	f.record("close")
	return nil
}

func passthroughAuth(next http.Handler) http.Handler { return next }

func passthroughUser(_ context.Context) (uuid.UUID, bool) { return uuid.Nil, false }

func newBackupRouter(t *testing.T, svc BackupService, user uuid.UUID, present bool) http.Handler {
	t.Helper()
	r := chi.NewRouter()
	Mount(r, passthroughAuth, func(context.Context) (uuid.UUID, bool) {
		if !present {
			return uuid.Nil, false
		}
		return user, true
	}, svc)
	return r
}

func newLegacyRouter(t *testing.T, svc BackupService, user uuid.UUID, present bool) http.Handler {
	t.Helper()
	r := chi.NewRouter()
	MountLegacy(r, passthroughAuth, passthroughAuth, func(context.Context) (uuid.UUID, bool) {
		if !present {
			return uuid.Nil, false
		}
		return user, true
	}, svc)
	return r
}

func backupRequest(t *testing.T, handler http.Handler, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func TestMountWithoutServiceMountsNothing(t *testing.T) {
	r := chi.NewRouter()
	Mount(r, passthroughAuth, passthroughUser, nil)
	MountLegacy(r, passthroughAuth, passthroughAuth, passthroughUser, nil)

	id := uuid.New()
	for _, path := range []string{
		"/v1/backups",
		"/v1/databases/" + id.String() + "/backups",
	} {
		rec := backupRequest(t, r, http.MethodGet, path, "")
		if rec.Code != http.StatusNotFound {
			t.Errorf("%s status = %d, want 404 (no routes mounted)", path, rec.Code)
		}
	}
}

func TestRoutesRequireAuthentication(t *testing.T) {
	svc := &fakeBackupService{}
	handler := newBackupRouter(t, svc, uuid.New(), false)
	id := uuid.New()

	for _, request := range []struct{ method, path string }{
		{http.MethodGet, "/v1/backups"},
		{http.MethodPost, "/v1/backups"},
		{http.MethodGet, "/v1/backups/" + id.String()},
		{http.MethodGet, "/v1/backups/providers"},
		{http.MethodGet, "/v1/backups/schedules?resource_type=database&resource_id=" + id.String()},
		{http.MethodGet, "/v1/backups/restores"},
	} {
		rec := backupRequest(t, handler, request.method, request.path, "")
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
	resourceID := uuid.New()
	backupID := uuid.New()
	svc := &fakeBackupService{backup: Backup{
		ID:          backupID,
		ResourceRef: databaseRef(resourceID),
		Type:        BackupManual,
		Status:      BackupRunning,
		CreatedAt:   time.Now().UTC(),
	}}
	handler := newBackupRouter(t, svc, user, true)

	rec := backupRequest(t, handler, http.MethodPost, "/v1/backups",
		`{"resource_type":"database","resource_id":"`+resourceID.String()+`"}`)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}
	var envelope struct {
		Backup backupResponse `json:"backup"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if envelope.Backup.ID != backupID.String() || envelope.Backup.ResourceID != resourceID.String() {
		t.Errorf("response = %+v", envelope.Backup)
	}
	if svc.createBackupReq.Resource != databaseRef(resourceID) {
		t.Errorf("resource = %+v, want the database", svc.createBackupReq.Resource)
	}

	providerID := uuid.New().String()
	rec = backupRequest(t, handler, http.MethodPost, "/v1/backups",
		`{"resource_type":"database","resource_id":"`+resourceID.String()+`","provider_id":"`+providerID+`"}`)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}
	if svc.createBackupReq.TargetID.String() != providerID {
		t.Errorf("provider id = %s, want %s", svc.createBackupReq.TargetID, providerID)
	}

	for _, body := range []string{
		`{"nope":1}`,
		`{"resource_type":"database"}`,
		`{"resource_type":"database","resource_id":"not-a-uuid"}`,
		`{"resource_type":"cluster","resource_id":"` + resourceID.String() + `"}`,
	} {
		rec = backupRequest(t, handler, http.MethodPost, "/v1/backups", body)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("body %s → %d, want 400", body, rec.Code)
		}
	}
}

func TestCreateBackupRejectsUnsupportedResourceType(t *testing.T) {
	svc := &fakeBackupService{err: ErrUnsupportedResource}
	handler := newBackupRouter(t, svc, uuid.New(), true)
	rec := backupRequest(t, handler, http.MethodPost, "/v1/backups",
		`{"resource_type":"service","resource_id":"`+uuid.New().String()+`"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", rec.Code, rec.Body)
	}
}

func TestListBackupsFiltersAndLimit(t *testing.T) {
	resourceID := uuid.New()
	svc := &fakeBackupService{backups: []Backup{{ID: uuid.New(), ResourceRef: databaseRef(resourceID)}}}
	handler := newBackupRouter(t, svc, uuid.New(), true)

	rec := backupRequest(t, handler, http.MethodGet,
		"/v1/backups?resource_type=database&resource_id="+resourceID.String()+"&status=completed&limit=7", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}
	if svc.backupFilter.Resource == nil || *svc.backupFilter.Resource != databaseRef(resourceID) {
		t.Errorf("filter resource = %+v", svc.backupFilter.Resource)
	}
	if svc.backupFilter.Status != BackupCompleted || svc.backupFilter.Limit != 7 {
		t.Errorf("filter = %+v", svc.backupFilter)
	}
	var envelope map[string][]map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if _, ok := envelope["backups"]; !ok {
		t.Errorf("body has no backups key: %s", rec.Body)
	}

	for _, path := range []string{
		"/v1/backups?resource_type=database",
		"/v1/backups?limit=nope",
	} {
		rec = backupRequest(t, handler, http.MethodGet, path, "")
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s → %d, want 400", path, rec.Code)
		}
	}
}

func TestBackupGetDeleteRestoreRoutes(t *testing.T) {
	user := uuid.New()
	backupID := uuid.New()
	svc := &fakeBackupService{
		backup:  Backup{ID: backupID, ResourceRef: databaseRef(uuid.New())},
		restore: RestoreResult{RestoreID: uuid.New(), BackupID: backupID, Status: BackupRunning},
	}
	handler := newBackupRouter(t, svc, user, true)

	rec := backupRequest(t, handler, http.MethodGet, "/v1/backups/"+backupID.String(), "")
	if rec.Code != http.StatusOK {
		t.Fatalf("get status = %d", rec.Code)
	}
	rec = backupRequest(t, handler, http.MethodPost, "/v1/backups/"+backupID.String()+"/restore", "")
	if rec.Code != http.StatusAccepted {
		t.Fatalf("restore status = %d: %s", rec.Code, rec.Body)
	}
	rec = backupRequest(t, handler, http.MethodDelete, "/v1/backups/"+backupID.String(), "")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete status = %d", rec.Code)
	}
	rec = backupRequest(t, handler, http.MethodGet, "/v1/backups/not-a-uuid", "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid id status = %d", rec.Code)
	}
}

func TestProviderRoutes(t *testing.T) {
	providerID := uuid.New()
	svc := &fakeBackupService{
		provider: BackupTarget{ID: providerID, Name: "r2", Kind: TargetS3, Builtin: false},
		check:    TargetCheck{OK: true, Message: "connected"},
	}
	handler := newBackupRouter(t, svc, uuid.New(), true)

	rec := backupRequest(t, handler, http.MethodGet, "/v1/backups/providers", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("list status = %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `"providers"`) {
		t.Errorf("body = %s", rec.Body)
	}

	rec = backupRequest(t, handler, http.MethodPost, "/v1/backups/providers",
		`{"name":"r2","kind":"s3","endpoint":"https://s3.example.com","bucket":"b","access_key":"ak","secret_key":"sk"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status = %d: %s", rec.Code, rec.Body)
	}
	if svc.providerReq.SecretKey != "sk" {
		t.Errorf("provider request = %+v", svc.providerReq)
	}
	if strings.Contains(rec.Body.String(), "sk") && !strings.Contains(rec.Body.String(), `"has_credentials":true`) {
		t.Errorf("response leaked a credential: %s", rec.Body)
	}

	rec = backupRequest(t, handler, http.MethodPatch, "/v1/backups/providers/"+providerID.String(), `{"name":"renamed"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("update status = %d", rec.Code)
	}
	rec = backupRequest(t, handler, http.MethodPost, "/v1/backups/providers/"+providerID.String()+"/test", "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"ok":true`) {
		t.Fatalf("test status = %d body=%s", rec.Code, rec.Body)
	}
	rec = backupRequest(t, handler, http.MethodDelete, "/v1/backups/providers/"+providerID.String(), "")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete status = %d", rec.Code)
	}
}

func TestScheduleRoutes(t *testing.T) {
	resourceID := uuid.New()
	scheduleID := uuid.New()
	svc := &fakeBackupService{schedule: BackupSchedule{
		ID:          scheduleID,
		ResourceRef: databaseRef(resourceID),
		Cron:        "0 2 * * *",
		Enabled:     true,
	}}
	handler := newBackupRouter(t, svc, uuid.New(), true)

	rec := backupRequest(t, handler, http.MethodGet,
		"/v1/backups/schedules?resource_type=database&resource_id="+resourceID.String(), "")
	if rec.Code != http.StatusOK {
		t.Fatalf("list status = %d: %s", rec.Code, rec.Body)
	}
	if svc.scheduleRef != databaseRef(resourceID) {
		t.Errorf("resource ref = %+v", svc.scheduleRef)
	}

	rec = backupRequest(t, handler, http.MethodPost, "/v1/backups/schedules",
		`{"resource_type":"database","resource_id":"`+resourceID.String()+`","cron":"0 2 * * *"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status = %d: %s", rec.Code, rec.Body)
	}
	if svc.createSchedule.Resource != databaseRef(resourceID) {
		t.Errorf("created resource = %+v", svc.createSchedule.Resource)
	}

	rec = backupRequest(t, handler, http.MethodPatch, "/v1/backups/schedules/"+scheduleID.String(), `{"cron":"0 3 * * *"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("update status = %d: %s", rec.Code, rec.Body)
	}
	rec = backupRequest(t, handler, http.MethodDelete, "/v1/backups/schedules/"+scheduleID.String(), "")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete status = %d", rec.Code)
	}

	rec = backupRequest(t, handler, http.MethodGet, "/v1/backups/schedules", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("unscoped list status = %d, want 200", rec.Code)
	}
}

func TestErrorMapping(t *testing.T) {
	id := uuid.New()
	tests := []struct {
		name string
		err  error
		want int
	}{
		{"not found", ErrNotFound, http.StatusNotFound},
		{"validation", ErrValidation, http.StatusBadRequest},
		{"unsupported", ErrUnsupportedResource, http.StatusBadRequest},
		{"already running", ErrBackupInFlight, http.StatusConflict},
		{"not restorable", ErrBackupNotCompleted, http.StatusConflict},
		{"stranded", ErrTargetStranded, http.StatusConflict},
		{"builtin", ErrBuiltinProvider, http.StatusConflict},
		{"conflict", ErrConflict, http.StatusConflict},
		{"disabled", ErrDisabled, http.StatusServiceUnavailable},
		{"internal", errors.New("boom"), http.StatusInternalServerError},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			svc := &fakeBackupService{err: test.err}
			handler := newBackupRouter(t, svc, uuid.New(), true)
			rec := backupRequest(t, handler, http.MethodPost, "/v1/backups/"+id.String()+"/restore", "")
			if rec.Code != test.want {
				t.Errorf("status = %d, want %d: %s", rec.Code, test.want, rec.Body)
			}
		})
	}
}

func TestLegacyAliasesKeepTheirWireShape(t *testing.T) {
	user := uuid.New()
	databaseID := uuid.New()
	backupID := uuid.New()
	targetID := uuid.New()
	backup := Backup{
		ID:          backupID,
		ResourceRef: databaseRef(databaseID),
		Type:        BackupManual,
		Status:      BackupCompleted,
		Size:        42,
		Location:    locationFilePrefix + "/tmp/a.gz",
		CreatedAt:   time.Now().UTC(),
	}
	svc := &fakeBackupService{
		backup:   backup,
		backups:  []Backup{backup},
		restore:  RestoreResult{RestoreID: uuid.New(), BackupID: backupID, Status: BackupRunning},
		schedule: BackupSchedule{ID: uuid.New(), ResourceRef: databaseRef(databaseID), Cron: "0 2 * * *", Enabled: true},
		provider: BackupTarget{ID: targetID, Name: "r2", Kind: TargetS3},
	}
	handler := newLegacyRouter(t, svc, user, true)

	rec := backupRequest(t, handler, http.MethodGet, "/v1/databases/"+databaseID.String()+"/backups", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("list status = %d: %s", rec.Code, rec.Body)
	}
	var list struct {
		Backups []legacyBackupResponse `json:"backups"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatalf("decode: %v", err)
	}
	for _, key := range []string{`"database_id"`, `"location"`, `"size"`} {
		if !strings.Contains(rec.Body.String(), key) {
			t.Errorf("legacy list response lost %s: %s", key, rec.Body)
		}
	}

	rec = backupRequest(t, handler, http.MethodPost, "/v1/databases/"+databaseID.String()+"/backup",
		`{"target_id":"`+targetID.String()+`"}`)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("create status = %d: %s", rec.Code, rec.Body)
	}
	if svc.createBackupReq.Resource != databaseRef(databaseID) || svc.createBackupReq.TargetID != targetID {
		t.Errorf("create request = %+v", svc.createBackupReq)
	}

	rec = backupRequest(t, handler, http.MethodPost, "/v1/databases/"+databaseID.String()+"/schedules",
		`{"cron":"0 2 * * *","target_id":"`+targetID.String()+`"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("schedule status = %d: %s", rec.Code, rec.Body)
	}
	if svc.createSchedule.Resource != databaseRef(databaseID) || svc.createSchedule.TargetID == nil ||
		*svc.createSchedule.TargetID != targetID.String() {
		t.Errorf("schedule request = %+v", svc.createSchedule)
	}

	rec = backupRequest(t, handler, http.MethodGet, "/v1/databases/backup-targets", "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"targets"`) {
		t.Errorf("targets response = %d %s", rec.Code, rec.Body)
	}

	// A backup that belongs to another database is hidden behind the alias.
	otherDatabase := uuid.New()
	rec = backupRequest(t, handler, http.MethodGet, "/v1/databases/"+otherDatabase.String()+"/backups/"+backupID.String(), "")
	if rec.Code != http.StatusNotFound {
		t.Errorf("cross-database get status = %d, want 404", rec.Code)
	}
}

func TestLegacyRestoreRejectsAnotherDatabase(t *testing.T) {
	databaseID := uuid.New()
	backupID := uuid.New()
	svc := &fakeBackupService{backup: Backup{ID: backupID, ResourceRef: databaseRef(uuid.New())}}
	handler := newLegacyRouter(t, svc, uuid.New(), true)

	rec := backupRequest(t, handler, http.MethodPost, "/v1/databases/"+databaseID.String()+"/restore",
		`{"backup_id":"`+backupID.String()+`"}`)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}
