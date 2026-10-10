package updates

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
)

type memScheduleStore struct {
	mu    sync.Mutex
	saved *Schedule
}

func (m *memScheduleStore) Load(context.Context) (*Schedule, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.saved, nil
}

func (m *memScheduleStore) Save(_ context.Context, sc Schedule) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.saved = &sc
	return nil
}

func validSchedule() Schedule {
	return Schedule{CheckEnabled: true, AutoApply: true, Channel: ChannelBeta, Frequency: FrequencyDaily, IntervalMinutes: 360, AtTime: "03:30", Weekday: 2}
}

func TestScheduleValidate(t *testing.T) {
	if err := validSchedule().Validate(); err != nil {
		t.Fatalf("valid schedule rejected: %v", err)
	}
	cases := map[string]func(*Schedule){
		"channel":          func(s *Schedule) { s.Channel = "nightly" },
		"frequency":        func(s *Schedule) { s.Frequency = "hourly" },
		"interval_minutes": func(s *Schedule) { s.Frequency = FrequencyInterval; s.IntervalMinutes = 5 },
		"at_time":          func(s *Schedule) { s.AtTime = "25:00" },
		"weekday":          func(s *Schedule) { s.Weekday = 7 },
		"auto_apply":       func(s *Schedule) { s.CheckEnabled = false },
	}
	for field, mutate := range cases {
		sc := validSchedule()
		mutate(&sc)
		err, ok := sc.Validate().(*ScheduleError)
		if !ok || err.Fields[field] == "" {
			t.Errorf("%s: Validate() = %v, want field error", field, err)
		}
	}
}

func TestScheduleNextRun(t *testing.T) {
	from := time.Date(2026, 10, 10, 4, 0, 0, 0, time.UTC) // a Saturday
	daily := Schedule{Frequency: FrequencyDaily, AtTime: "03:30"}
	if got, want := daily.nextRun(from, time.Hour), time.Date(2026, 10, 11, 3, 30, 0, 0, time.UTC); !got.Equal(want) {
		t.Errorf("daily = %v, want %v", got, want)
	}
	weekly := Schedule{Frequency: FrequencyWeekly, AtTime: "05:00", Weekday: 1} // Monday
	if got, want := weekly.nextRun(from, time.Hour), time.Date(2026, 10, 12, 5, 0, 0, 0, time.UTC); !got.Equal(want) {
		t.Errorf("weekly = %v, want %v", got, want)
	}
	if got, want := (Schedule{Frequency: FrequencyInterval, IntervalMinutes: 90}).nextRun(from, time.Hour), from.Add(90*time.Minute); !got.Equal(want) {
		t.Errorf("interval = %v, want %v", got, want)
	}
}

// TestSetSchedulePersistsAndReloads proves a saved schedule is stored, applied
// to the live service and restored by a fresh service (restart).
func TestSetSchedulePersistsAndReloads(t *testing.T) {
	server := httptest.NewServer(releasesHandler(nil, 0, 0))
	defer server.Close()
	store := &memScheduleStore{}
	svc := newTestService(t, server, "v1.0.0", func(c *Config) { c.Schedules = store }).(*service)

	if svc.ScheduleState().Schedule.CheckEnabled {
		t.Fatal("default schedule must be off without AUTO_UPDATE")
	}
	state, err := svc.SetSchedule(context.Background(), validSchedule())
	if err != nil {
		t.Fatalf("SetSchedule: %v", err)
	}
	if state.NextRunAt == nil || svc.schedule().Channel != ChannelBeta {
		t.Fatalf("schedule not applied live: %+v", state)
	}
	select {
	case <-svc.reload:
	default:
		t.Fatal("scheduler was not signalled to reload")
	}
	if _, err := svc.SetSchedule(context.Background(), Schedule{}); err == nil {
		t.Fatal("invalid schedule accepted")
	}

	restarted := newTestService(t, server, "v1.0.0", func(c *Config) { c.Schedules = store }).(*service)
	if got := restarted.schedule(); got != validSchedule() {
		t.Fatalf("after restart schedule = %+v, want %+v", got, validSchedule())
	}
}

func TestScheduleRoutes(t *testing.T) {
	server := httptest.NewServer(releasesHandler(nil, 0, 0))
	defer server.Close()
	svc := newTestService(t, server, "v1.0.0", func(c *Config) { c.Schedules = &memScheduleStore{} })
	pass := func(next http.Handler) http.Handler { return next }
	router := chi.NewRouter()
	Mount(router, pass, pass, func(*http.Request) bool { return true }, svc)

	put := func(body string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(http.MethodPut, "/v1/updates/schedule", strings.NewReader(body)))
		return rec
	}
	if rec := put(`{"check_enabled":true,"auto_apply":false,"channel":"stable","frequency":"interval","interval_minutes":10,"at_time":"03:00","weekday":0}`); rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("short interval status = %d, want 422", rec.Code)
	}
	if rec := put(`{"bogus":1}`); rec.Code != http.StatusBadRequest {
		t.Errorf("unknown field status = %d, want 400", rec.Code)
	}
	if rec := put(`{"check_enabled":true,"auto_apply":false,"channel":"stable","frequency":"weekly","interval_minutes":360,"at_time":"04:00","weekday":1}`); rec.Code != http.StatusOK {
		t.Fatalf("valid put status = %d: %s", rec.Code, rec.Body)
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/updates/schedule", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"frequency":"weekly"`) {
		t.Errorf("get = %d %s", rec.Code, rec.Body)
	}
}
