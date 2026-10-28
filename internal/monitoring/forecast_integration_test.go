package monitoring

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/justindeelux/gotham/internal/incidents"
	"github.com/justindeelux/gotham/internal/store"
	"github.com/justindeelux/gotham/internal/store/sqlc"
)

type forecastFixture struct {
	t     *testing.T
	store *store.Store
	ctx   context.Context
	svc   *Service
	inc   *incidents.Service
	node  uuid.UUID
	id    sqlc.FilesystemIdentity
	now   time.Time
}

func newForecastFixture(t *testing.T, notifier incidents.Notifier) *forecastFixture {
	t.Helper()
	st := scratchStore(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	t.Cleanup(cancel)
	f := &forecastFixture{
		t:     t,
		store: st,
		ctx:   ctx,
		node:  createNode(t, st),
		now:   time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC),
	}
	clock := func() time.Time { return f.now }
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	f.inc = incidents.NewService(incidents.Config{
		Store: st, Notifier: notifier, Logger: logger, Now: clock,
	})
	f.svc = NewService(Config{Store: st, Incidents: f.inc, Logger: logger, Now: clock})
	identity, err := st.CreateFilesystemIdentity(ctx, sqlc.CreateFilesystemIdentityParams{
		ID:            pgUUID(uuid.New()),
		NodeID:        pgUUID(f.node),
		FilesystemKey: "/data",
		Path:          "/data",
		Purpose:       "data",
		TotalBytes:    100 * gib,
		FirstSeenAt:   pgtype.Timestamptz{Time: f.now.Add(-48 * time.Hour), Valid: true},
	})
	if err != nil {
		t.Fatalf("create filesystem identity: %v", err)
	}
	f.id = identity
	return f
}

func (f *forecastFixture) addHour(at time.Time, used int64) {
	f.t.Helper()
	value := used
	err := f.store.UpsertFilesystemUsageBucket(f.ctx, sqlc.UpsertFilesystemUsageBucketParams{
		IdentityID:    f.id.ID,
		Segment:       f.id.Segment,
		BucketAt:      pgtype.Timestamptz{Time: at.UTC().Truncate(time.Hour), Valid: true},
		Samples:       1,
		FreshSamples:  1,
		MinUsedBytes:  &value,
		MaxUsedBytes:  &value,
		LastUsedBytes: &value,
		TotalBytes:    f.id.TotalBytes,
		UpdatedAt:     pgtype.Timestamptz{Time: at.UTC(), Valid: true},
	})
	if err != nil {
		f.t.Fatalf("insert usage bucket: %v", err)
	}
}

// addSeries inserts hourly buckets ending at f.now. used is called with the
// hour offset from the first bucket.
func (f *forecastFixture) addSeries(hours int, used func(int) int64) {
	for i := 0; i < hours; i++ {
		at := f.now.Add(-time.Duration(hours-1-i) * time.Hour)
		f.addHour(at, used(i))
	}
}

func (f *forecastFixture) evaluate() {
	f.t.Helper()
	if err := f.svc.EvaluateForecasts(f.ctx); err != nil {
		f.t.Fatalf("evaluate forecasts: %v", err)
	}
}

func (f *forecastFixture) lastForecast() (incidents.Lifecycle, string) {
	f.t.Helper()
	key := filesystemIdentity(DetectorForecast, f.node, f.id.FilesystemKey)
	var lifecycle, closeReason string
	if err := f.store.DB.QueryRow(f.ctx,
		`SELECT lifecycle, close_reason FROM incidents WHERE identity_key = $1 ORDER BY created_at DESC LIMIT 1`,
		key).Scan(&lifecycle, &closeReason); err != nil {
		f.t.Fatalf("load forecast incident: %v", err)
	}
	return incidents.Lifecycle(lifecycle), closeReason
}

func (f *forecastFixture) activeForecast() incidents.Incident {
	f.t.Helper()
	rows, err := f.inc.ActiveNodeIncidents(f.ctx)
	if err != nil {
		f.t.Fatalf("list active incidents: %v", err)
	}
	for _, inc := range rows {
		if inc.Detector == DetectorForecast {
			return inc
		}
	}
	f.t.Fatal("no active forecast incident")
	return incidents.Incident{}
}

func TestForecastLifecycleWarningDowngradeAndRecovery(t *testing.T) {
	notifier := &countingNotifier{}
	f := newForecastFixture(t, notifier)
	// About 54 hours of available space at 100 MiB per hour: warning band.
	f.addSeries(48, func(i int) int64 { return 90*gib + int64(i)*100*(1<<20) })

	f.evaluate()
	inc := f.activeForecast()
	if inc.Lifecycle != incidents.LifecyclePending || inc.Severity != incidents.SeverityWarning {
		t.Fatalf("first evaluation = %s/%s, want pending/warning", inc.Lifecycle, inc.Severity)
	}
	if len(notifier.deliveries) != 0 {
		t.Fatalf("pending incident delivered %d notifications", len(notifier.deliveries))
	}

	f.now = f.now.Add(time.Hour)
	f.addHour(f.now, 90*gib+48*100*(1<<20))
	f.evaluate()
	inc = f.activeForecast()
	if inc.Lifecycle != incidents.LifecycleFiring || inc.Severity != incidents.SeverityWarning {
		t.Fatalf("second evaluation = %s/%s, want firing/warning", inc.Lifecycle, inc.Severity)
	}
	if len(notifier.deliveries) != 1 {
		t.Fatalf("deliveries = %d, want one opening notification", len(notifier.deliveries))
	}

	// A large cleanup resets the baseline; the slower trend is assessed from
	// the new baseline, never fitted through the jump.
	f.now = f.now.Add(time.Hour)
	f.addHour(f.now, 20*gib)
	for i := 0; i < 30; i++ {
		f.now = f.now.Add(time.Hour)
		f.addHour(f.now, 20*gib+int64(i+1)*800*(1<<20))
	}
	f.evaluate()
	inc = f.activeForecast()
	if inc.Severity != incidents.SeverityWarning {
		t.Fatalf("after cleanup severity = %s, want warning", inc.Severity)
	}

	// A long flat run makes the recent half non-growing, then the second fresh
	// valid evaluation resolves the incident.
	for i := 0; i < 40; i++ {
		f.now = f.now.Add(time.Hour)
		f.addHour(f.now, 20*gib+30*800*(1<<20))
	}
	f.evaluate()
	if inc = f.activeForecast(); inc.Lifecycle != incidents.LifecycleFiring {
		t.Fatalf("first non-growing evaluation = %s, want still firing", inc.Lifecycle)
	}
	f.now = f.now.Add(time.Hour)
	f.addHour(f.now, 20*gib+30*800*(1<<20))
	f.evaluate()
	lifecycle, closeReason := f.lastForecast()
	if lifecycle != incidents.LifecycleResolved || closeReason != "recovered" {
		t.Fatalf("second non-growing evaluation = %s/%s, want resolved/recovered", lifecycle, closeReason)
	}
}

func TestForecastStaleHistoryDoesNotResolveAnOpenIncident(t *testing.T) {
	f := newForecastFixture(t, &countingNotifier{})
	f.addSeries(48, func(i int) int64 { return 50*gib + int64(i)*gib })
	f.evaluate()
	f.now = f.now.Add(time.Hour)
	f.addHour(f.now, 50*gib+48*gib)
	f.evaluate()
	if inc := f.activeForecast(); inc.Lifecycle != incidents.LifecycleFiring {
		t.Fatalf("lifecycle = %s, want firing before the gap", inc.Lifecycle)
	}

	// No new buckets: the latest observation is now far older than two hours.
	// Unknown evidence must not close the incident.
	f.now = f.now.Add(6 * time.Hour)
	f.evaluate()
	inc := f.activeForecast()
	if inc.Lifecycle != incidents.LifecycleFiring || inc.Coverage != incidents.CoverageUnknown {
		t.Fatalf("stale evaluation = %s/%s, want firing/unknown", inc.Lifecycle, inc.Coverage)
	}
}

func TestForecastResizeStartsFromTheNewSegment(t *testing.T) {
	f := newForecastFixture(t, &countingNotifier{})
	f.addSeries(48, func(i int) int64 { return 50*gib + int64(i)*gib })
	updated, err := f.store.UpdateFilesystemIdentity(f.ctx, sqlc.UpdateFilesystemIdentityParams{
		Path: "/data", Purpose: "data", Segment: 2, TotalBytes: 200 * gib,
		LastSeenAt: pgtype.Timestamptz{Time: f.now, Valid: true}, ID: f.id.ID,
	})
	if err != nil {
		t.Fatalf("resize filesystem: %v", err)
	}
	f.id = updated
	for i := 0; i < 6; i++ {
		f.addHour(f.now.Add(-time.Duration(5-i)*time.Hour), 90*gib+int64(i)*gib)
	}
	f.evaluate()
	rows, err := f.inc.ActiveNodeIncidents(f.ctx)
	if err != nil {
		t.Fatalf("list active incidents: %v", err)
	}
	for _, inc := range rows {
		if inc.Detector == DetectorForecast {
			t.Fatalf("forecast used history across the resize: %+v", inc)
		}
	}
}

func TestForecastGroupsUnderAnExistingSpaceIncidentWithoutAnotherNotification(t *testing.T) {
	notifier := &countingNotifier{}
	f := newForecastFixture(t, notifier)
	spaceIdentity := filesystemIdentity(DetectorSpace, f.node, f.id.FilesystemKey)
	_, _, err := f.inc.Report(f.ctx, incidents.Observation{
		Identity: spaceIdentity, Detector: DetectorSpace, ScopeKind: incidents.ScopeNode,
		ScopeID: f.node, NodeID: f.node, ResourceType: ResourceFilesystem,
		ResourceID: uuid.UUID(f.id.ID.Bytes), ResourceName: f.id.Path, At: f.now,
		Valid: true, Unhealthy: true, Severity: incidents.SeverityWarning,
		Evidence: "90% used",
	})
	if err != nil {
		t.Fatalf("open disk space incident: %v", err)
	}
	if len(notifier.deliveries) != 1 {
		t.Fatalf("space incident deliveries = %d, want 1", len(notifier.deliveries))
	}

	active, err := f.inc.ActiveNodeIncidents(f.ctx)
	if err != nil {
		t.Fatalf("list active incidents: %v", err)
	}
	forecast := FilesystemForecast{
		Status: ForecastEstimated, AvailableBytes: uint64(5 * gib), TotalBytes: uint64(100 * gib),
		SpanHours: 47, Samples: 48, Fit: 0.93, HorizonHours: 5,
	}
	f.svc.reportForecast(f.ctx, f.node, f.id, filesystemIdentity(DetectorForecast, f.node, f.id.FilesystemKey), forecast, f.now, active)
	f.now = f.now.Add(time.Hour)
	active, err = f.inc.ActiveNodeIncidents(f.ctx)
	if err != nil {
		t.Fatalf("list active incidents: %v", err)
	}
	f.svc.reportForecast(f.ctx, f.node, f.id, filesystemIdentity(DetectorForecast, f.node, f.id.FilesystemKey), forecast, f.now, active)

	if len(notifier.deliveries) != 1 {
		t.Fatalf("forecast delivery duplicated the disk alert: %d deliveries", len(notifier.deliveries))
	}
	inc := f.activeForecast()
	if inc.DeliveryState != incidents.DeliverySuppressed {
		t.Fatalf("forecast delivery state = %s, want suppressed", inc.DeliveryState)
	}
}
