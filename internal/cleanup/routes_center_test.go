package cleanup

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/store"
)

type fakeCenterService struct {
	schedules  []store.CleanupSchedule
	created    ScheduleInput
	runs       []store.CleanupRunView
	items      []store.CleanupRunItem
	exclusions []store.CleanupExclusion
	err        error
}

func (f *fakeCenterService) ListSchedules(context.Context) ([]store.CleanupSchedule, error) {
	return f.schedules, f.err
}

func (f *fakeCenterService) CreateSchedule(
	_ context.Context, _ uuid.UUID, input ScheduleInput,
) (store.CleanupSchedule, error) {
	f.created = input
	return store.CleanupSchedule{
		ID: uuid.New(), Name: input.Name, ServerIDs: input.ServerIDs,
		Categories: input.Categories, Cron: input.Cron, MinAgeHours: input.MinAgeHours,
		Enabled: true, NextRunAt: time.Now(), CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}, f.err
}

func (f *fakeCenterService) UpdateSchedule(
	_ context.Context, id uuid.UUID, input ScheduleInput,
) (store.CleanupSchedule, error) {
	return store.CleanupSchedule{ID: id, Name: input.Name}, f.err
}

func (f *fakeCenterService) DeleteSchedule(context.Context, uuid.UUID) error { return f.err }

func (f *fakeCenterService) RunScheduleNow(context.Context, uuid.UUID) error { return f.err }

func (f *fakeCenterService) NextCronRun(string) (time.Time, error) {
	if f.err != nil {
		return time.Time{}, f.err
	}
	return time.Date(2026, 10, 31, 3, 0, 0, 0, time.UTC), nil
}

func (f *fakeCenterService) ListRuns(
	context.Context, store.CleanupRunFilter,
) ([]store.CleanupRunView, error) {
	return f.runs, f.err
}

func (f *fakeCenterService) GetRun(
	context.Context, uuid.UUID,
) (store.CleanupRunView, []store.CleanupRunItem, error) {
	return f.runs[0], f.items, f.err
}

func (f *fakeCenterService) ListExclusions(context.Context) ([]store.CleanupExclusion, error) {
	return f.exclusions, f.err
}

func (f *fakeCenterService) CreateExclusion(
	_ context.Context, _ uuid.UUID, input ExclusionInput,
) (store.CleanupExclusion, error) {
	return store.CleanupExclusion{
		ID: uuid.New(), ServerID: input.ServerID, Category: input.Category,
		Match: input.Match, Note: input.Note, CreatedAt: time.Now(),
	}, f.err
}

func (f *fakeCenterService) DeleteExclusion(context.Context, uuid.UUID) error { return f.err }

func newCenterRouter(svc CleanupCenter, userID UserIDFunc) http.Handler {
	r := chi.NewRouter()
	MountCenter(r, memberForbidden, userID, svc)
	return r
}

func newRequest(method, path, body string) *http.Request {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	return req
}

func serve(h http.Handler, req *http.Request) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestCenterRoutesAreOwnerOnly(t *testing.T) {
	router := newCenterRouter(&fakeCenterService{}, ownerID)
	for _, path := range []string{
		"/v1/cleanup/schedules", "/v1/cleanup/runs", "/v1/cleanup/exclusions",
	} {
		req := newRequest(http.MethodGet, path, "")
		req.Header.Set("X-Member", "1")
		rec := serve(router, req)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("%s = %d; a member must be refused", path, rec.Code)
		}
	}
}

func TestScheduleRoutes(t *testing.T) {
	serverID := uuid.New()
	svc := &fakeCenterService{schedules: []store.CleanupSchedule{{
		ID: uuid.New(), Name: "nightly", ServerIDs: []uuid.UUID{serverID},
		Categories: []string{CategoryBuildCache}, Cron: "0 3 * * *", MinAgeHours: 24,
		Enabled: true, NextRunAt: time.Now(), CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}}}
	router := newCenterRouter(svc, ownerID)

	rec := serve(router, newRequest(http.MethodGet, "/v1/cleanup/schedules", ""))
	if rec.Code != http.StatusOK {
		t.Fatalf("list schedules = %d", rec.Code)
	}
	var listed scheduleListEnvelope
	decodeTestJSON(t, rec, &listed)
	if len(listed.Schedules) != 1 || listed.Schedules[0].Name != "nightly" ||
		len(listed.Schedules[0].ServerIDs) != 1 {
		t.Fatalf("schedules = %+v", listed.Schedules)
	}

	body := `{"name":"nightly","server_ids":["` + serverID.String() +
		`"],"categories":["build_cache"],"cron":"0 3 * * *","min_age_hours":24}`
	rec = serve(router, newRequest(http.MethodPost, "/v1/cleanup/schedules", body))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create schedule = %d (%s)", rec.Code, rec.Body)
	}
	if svc.created.Name != "nightly" || len(svc.created.ServerIDs) != 1 || svc.created.Enabled != nil {
		t.Fatalf("created = %+v", svc.created)
	}

	rec = serve(router, newRequest(http.MethodPost, "/v1/cleanup/schedules/"+uuid.NewString()+"/run", ""))
	if rec.Code != http.StatusAccepted {
		t.Fatalf("run now = %d", rec.Code)
	}

	rec = serve(router, newRequest(http.MethodGet, "/v1/cleanup/cron/next?expr=0+3+*+*+*", ""))
	if rec.Code != http.StatusOK {
		t.Fatalf("cron next = %d", rec.Code)
	}
	var next nextRunEnvelope
	decodeTestJSON(t, rec, &next)
	if next.NextRunAt != "2026-10-31T03:00:00Z" {
		t.Fatalf("next run = %q", next.NextRunAt)
	}
	svc.err = ErrValidation
	rec = serve(router, newRequest(http.MethodGet, "/v1/cleanup/cron/next?expr=bogus", ""))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad cron = %d, want 400", rec.Code)
	}

	svc.err = ErrScheduleRunning
	rec = serve(router, newRequest(http.MethodPost, "/v1/cleanup/schedules/"+uuid.NewString()+"/run", ""))
	if rec.Code != http.StatusConflict {
		t.Fatalf("run again = %d, want 409", rec.Code)
	}

	svc.err = ErrDangerousCategory
	rec = serve(router, newRequest(http.MethodPost, "/v1/cleanup/schedules", body))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("dangerous schedule = %d, want 400", rec.Code)
	}
}

func TestRunRoutes(t *testing.T) {
	runID := uuid.New()
	svc := &fakeCenterService{
		runs: []store.CleanupRunView{{
			CleanupRun: store.CleanupRun{
				ID: runID, ServerID: uuid.New(), Trigger: TriggerSchedule,
				StartedAt: time.Now(), FinishedAt: time.Now(), ReclaimedBytes: 42,
				Status: StatusOK,
			},
			ScheduleName: "nightly",
			Categories:   map[string]int64{CategoryBuildCache: 42},
		}},
		items: []store.CleanupRunItem{{
			ItemID: idBuildCache, Category: CategoryBuildCache, Name: "Build cache",
			Status: "removed", ReclaimedBytes: 42,
		}},
	}
	router := newCenterRouter(svc, ownerID)

	rec := serve(router, newRequest(http.MethodGet, "/v1/cleanup/runs?limit=10", ""))
	if rec.Code != http.StatusOK {
		t.Fatalf("list runs = %d", rec.Code)
	}
	var listed runListEnvelope
	decodeTestJSON(t, rec, &listed)
	if len(listed.Runs) != 1 || listed.Runs[0].ScheduleName != "nightly" ||
		listed.Runs[0].Categories[CategoryBuildCache] != 42 {
		t.Fatalf("runs = %+v", listed.Runs)
	}

	rec = serve(router, newRequest(http.MethodGet, "/v1/cleanup/runs/"+runID.String(), ""))
	if rec.Code != http.StatusOK {
		t.Fatalf("get run = %d", rec.Code)
	}
	var detail runEnvelope
	decodeTestJSON(t, rec, &detail)
	if len(detail.Run.Items) != 1 || detail.Run.Items[0].ID != idBuildCache {
		t.Fatalf("detail = %+v", detail.Run)
	}

	rec = serve(router, newRequest(http.MethodGet, "/v1/cleanup/runs?limit=0", ""))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad limit = %d, want 400", rec.Code)
	}
}

func TestExclusionRoutes(t *testing.T) {
	svc := &fakeCenterService{}
	router := newCenterRouter(svc, ownerID)
	body := `{"category":"build_cache","match":"Build cache","note":"keep"}`
	rec := serve(router, newRequest(http.MethodPost, "/v1/cleanup/exclusions", body))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create exclusion = %d (%s)", rec.Code, rec.Body)
	}
	var created exclusionEnvelope
	decodeTestJSON(t, rec, &created)
	if created.Exclusion.Category != CategoryBuildCache || created.Exclusion.Match != "Build cache" {
		t.Fatalf("exclusion = %+v", created.Exclusion)
	}

	rec = serve(router, newRequest(http.MethodGet, "/v1/cleanup/exclusions", ""))
	if rec.Code != http.StatusOK {
		t.Fatalf("list exclusions = %d", rec.Code)
	}

	rec = serve(router, newRequest(http.MethodDelete, "/v1/cleanup/exclusions/"+uuid.NewString(), ""))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete exclusion = %d", rec.Code)
	}
}

func decodeTestJSON(t *testing.T, rec *httptest.ResponseRecorder, dst any) {
	t.Helper()
	if err := json.NewDecoder(rec.Result().Body).Decode(dst); err != nil {
		t.Fatalf("decode: %v", err)
	}
}
