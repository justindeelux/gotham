package backupwatch

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/incidents"
)

type fakeSource struct {
	schedules []ScheduleEvidence
	active    []string
	database  DatabaseInfo
	dbErr     error
}

func (f *fakeSource) ProtectionSchedules(context.Context) ([]ScheduleEvidence, error) {
	return f.schedules, nil
}

func (f *fakeSource) ActiveProtectionIncidents(context.Context) ([]string, error) {
	return f.active, nil
}

func (f *fakeSource) Database(context.Context, uuid.UUID) (DatabaseInfo, error) {
	if f.dbErr != nil {
		return DatabaseInfo{}, f.dbErr
	}
	return f.database, nil
}

type fakeLeases struct {
	live map[uuid.UUID]bool
}

func (f fakeLeases) BackupJobActive(id uuid.UUID) bool { return f.live[id] }

type fakeIncidents struct {
	reports []incidents.Observation
	closed  []closedIncident
}

type closedIncident struct {
	identity string
	reason   string
}

func (f *fakeIncidents) Report(_ context.Context, obs incidents.Observation) (*incidents.Incident, incidents.Transition, error) {
	f.reports = append(f.reports, obs)
	return nil, incidents.Transition{}, nil
}

func (f *fakeIncidents) Close(_ context.Context, identity, reason string) error {
	f.closed = append(f.closed, closedIncident{identity: identity, reason: reason})
	return nil
}

func newTestService(source Source, reporter IncidentReporter, leases LeaseReader, now *time.Time) *Service {
	return NewService(Config{
		Store:     source,
		Incidents: reporter,
		Leases:    leases,
		Logger:    slog.New(slog.NewTextHandler(io.Discard, nil)),
		Now:       func() time.Time { return *now },
	})
}

func TestObserverFollowsOverdueStuckAndLateSuccess(t *testing.T) {
	slot := time.Date(2026, 10, 27, 2, 0, 0, 0, time.UTC)
	now := slot.Add(OverdueGrace + time.Minute)
	databaseID := uuid.New()
	schedule := ScheduleEvidence{
		ScheduleID: uuid.New(), DatabaseID: databaseID, DatabaseName: "orders",
		ProjectID: uuid.New(), TargetName: "offsite", Enabled: true,
		NextRunAt: slot, MaxRunSeconds: 7200,
	}
	source := &fakeSource{schedules: []ScheduleEvidence{schedule}}
	reporter := &fakeIncidents{}
	leases := fakeLeases{live: map[uuid.UUID]bool{}}
	service := newTestService(source, reporter, leases, &now)

	service.tick(context.Background())
	if len(reporter.reports) != 1 || !reporter.reports[0].Unhealthy || reporter.reports[0].Severity != incidents.SeverityWarning {
		t.Fatalf("overdue report = %+v", reporter.reports)
	}

	// A live job starts: it defers but never counts as recovery.
	running := slot.Add(time.Hour)
	source.schedules[0].RunningSince = &running
	leases.live[databaseID] = true
	service.tick(context.Background())
	last := reporter.reports[len(reporter.reports)-1]
	if last.Valid || last.Unhealthy {
		t.Fatalf("live run must report unknown, got valid=%v unhealthy=%v", last.Valid, last.Unhealthy)
	}
	if len(reporter.closed) != 0 {
		t.Fatalf("live run closed the incident: %+v", reporter.closed)
	}

	// The lease is gone and the run is past its maximum: stuck is critical.
	delete(leases.live, databaseID)
	now = slot.Add(3 * time.Hour)
	service.tick(context.Background())
	last = reporter.reports[len(reporter.reports)-1]
	if !last.Valid || !last.Unhealthy || last.Severity != incidents.SeverityCritical {
		t.Fatalf("stuck report = %+v", last)
	}

	// A failed run is not a success, so the next tick is still overdue.
	source.schedules[0].RunningSince = nil
	service.tick(context.Background())
	last = reporter.reports[len(reporter.reports)-1]
	if !last.Unhealthy || last.Severity != incidents.SeverityWarning {
		t.Fatalf("failed run report = %+v", last)
	}

	// A qualifying late success is the only healthy report.
	finished := now
	source.schedules[0].LastSuccessAt = &finished
	service.tick(context.Background())
	last = reporter.reports[len(reporter.reports)-1]
	if !last.Valid || last.Unhealthy {
		t.Fatalf("late success report = %+v", last)
	}
}

func TestObserverWithoutLeaseEvidenceUsesMaxRun(t *testing.T) {
	slot := time.Date(2026, 10, 27, 2, 0, 0, 0, time.UTC)
	running := slot.Add(time.Minute)
	now := slot.Add(90 * time.Minute)
	schedule := ScheduleEvidence{
		ScheduleID: uuid.New(), DatabaseID: uuid.New(), Enabled: true,
		NextRunAt: slot, RunningSince: &running, MaxRunSeconds: 7200,
	}
	source := &fakeSource{schedules: []ScheduleEvidence{schedule}}
	reporter := &fakeIncidents{}
	service := newTestService(source, reporter, nil, &now)

	service.tick(context.Background())
	if got := reporter.reports[0].Valid; got {
		t.Fatal("a run inside the max window must stay unknown")
	}
	now = slot.Add(3 * time.Hour)
	service.tick(context.Background())
	last := reporter.reports[len(reporter.reports)-1]
	if !last.Unhealthy || last.Severity != incidents.SeverityCritical {
		t.Fatalf("past max run = %+v", last)
	}
}

func TestObserverClosesDisabledAndRemovedSchedules(t *testing.T) {
	now := time.Now().UTC()
	schedule := ScheduleEvidence{ScheduleID: uuid.New(), DatabaseID: uuid.New(), Enabled: false}
	source := &fakeSource{
		schedules: []ScheduleEvidence{schedule},
		active:    []string{Identity(schedule.ScheduleID)},
	}
	reporter := &fakeIncidents{}
	service := newTestService(source, reporter, nil, &now)

	service.tick(context.Background())
	if len(reporter.closed) != 1 || reporter.closed[0].reason != "schedule_disabled" {
		t.Fatalf("disabled closes = %+v", reporter.closed)
	}

	removedIdentity := Identity(uuid.New())
	source.schedules = nil
	source.active = []string{removedIdentity}
	reporter.closed = nil
	service.tick(context.Background())
	if len(reporter.closed) != 1 || reporter.closed[0].reason != "schedule_removed" {
		t.Fatalf("removed closes = %+v", reporter.closed)
	}
	if len(reporter.reports) != 0 {
		t.Fatalf("removed schedule reported: %+v", reporter.reports)
	}
}

func TestObserverSkipsDisabledScheduleWithoutActiveIncident(t *testing.T) {
	now := time.Now().UTC()
	source := &fakeSource{schedules: []ScheduleEvidence{{
		ScheduleID: uuid.New(), DatabaseID: uuid.New(), Enabled: false,
	}}}
	reporter := &fakeIncidents{}
	newTestService(source, reporter, nil, &now).tick(context.Background())
	if len(reporter.reports) != 0 || len(reporter.closed) != 0 {
		t.Fatalf("disabled schedule produced %+v %+v", reporter.reports, reporter.closed)
	}
}

func TestSummaryNotConfiguredWithoutEnabledSchedule(t *testing.T) {
	databaseID := uuid.New()
	source := &fakeSource{
		database: DatabaseInfo{ID: databaseID, Name: "orders"},
		schedules: []ScheduleEvidence{{
			ScheduleID: uuid.New(), DatabaseID: databaseID, Enabled: false,
		}},
	}
	service := NewService(Config{Store: source, Incidents: &fakeIncidents{}})
	summary, err := service.Summary(context.Background(), databaseID)
	if err != nil {
		t.Fatal(err)
	}
	if summary.Status != StatusNotConfigured {
		t.Fatalf("status = %q, want not_configured", summary.Status)
	}
}
