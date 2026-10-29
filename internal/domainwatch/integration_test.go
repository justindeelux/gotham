package domainwatch

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/justindeelux/gotham/internal/store"
	"github.com/justindeelux/gotham/internal/store/sqlc"
)

func integrationStore(t *testing.T) *store.Store {
	t.Helper()
	dsn := os.Getenv("GOTHAM_TEST_DSN")
	explicit := dsn != ""
	if !explicit {
		dsn = "postgres://gotham:gotham@localhost:5432/gotham?sslmode=disable"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if err := store.ProbeOnce(ctx, dsn); err != nil {
		if explicit {
			t.Fatalf("GOTHAM_TEST_DSN is set but Postgres is unavailable: %v", err)
		}
		t.Skipf("Postgres not available: %v", err)
	}
	pool, err := store.Open(ctx, dsn)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(pool.Close)
	if err := store.Migrate(ctx, dsn, store.MigrateUp); err != nil {
		t.Fatalf("migration failed: %v", err)
	}
	return store.New(pool)
}

type integrationFixture struct {
	store    *store.Store
	appID    uuid.UUID
	domainID uuid.UUID
	project  uuid.UUID
	now      time.Time
}

func newIntegrationFixture(t *testing.T) integrationFixture {
	t.Helper()
	st := integrationStore(t)
	ctx := context.Background()
	user, err := st.CreateUser(ctx, "domain-watch-"+time.Now().Format("150405.000000")+"@example.com", nil)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, _ = st.DB.Exec(cleanupCtx, "DELETE FROM users WHERE id = $1", user.ID)
	})
	project, err := st.CreateProject(ctx, sqlc.CreateProjectParams{
		ID:   pgtype.UUID{Bytes: uuid.New(), Valid: true},
		Name: fmt.Sprintf("domain-watch-%d", time.Now().UnixNano()),
	})
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, _ = st.DB.Exec(cleanupCtx, "DELETE FROM projects WHERE id = $1", project.ID)
	})
	environment, err := st.CreateEnvironment(ctx, sqlc.CreateEnvironmentParams{
		ID:        pgtype.UUID{Bytes: uuid.New(), Valid: true},
		ProjectID: project.ID,
		Name:      "production",
	})
	if err != nil {
		t.Fatalf("CreateEnvironment: %v", err)
	}
	serverRow, err := st.CreateServer(ctx, sqlc.CreateServerParams{
		Name: "domain-watch-node-" + time.Now().Format("150405"), Ip: "127.0.0.1", Port: 22, SshUser: "root",
	})
	if err != nil {
		t.Fatalf("CreateServer: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, _ = st.DB.Exec(cleanupCtx, "DELETE FROM servers WHERE id = $1", serverRow.ID)
	})
	appRow, err := st.CreateApplication(ctx, sqlc.CreateApplicationParams{
		ID:            pgtype.UUID{Bytes: uuid.New(), Valid: true},
		UserID:        user.ID,
		ServerID:      serverRow.ID,
		EnvironmentID: environment.ID,
		Name:          "domain-watch-app",
		SourceType:    "image",
		Provider:      "image",
		Branch:        "main",
		BuildPack:     "image",
		ImageRef:      "nginx:alpine",
	})
	if err != nil {
		t.Fatalf("CreateApplication: %v", err)
	}
	now := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	domainCreatedAt := now.Add(-time.Hour)
	domainID := uuid.New()
	// created_at and updated_at must carry the fixture clock: graceUntil
	// derives the configuration grace from them, so a database default would
	// put the grace window on wall-clock time instead of the injected Now.
	_, err = st.DB.Exec(ctx, `INSERT INTO application_domains
		(id, application_id, domain, is_primary, disabled, port, protocol, created_at, updated_at)
		VALUES ($1, $2, $3, true, false, 80, 'https', $4, $4)`, domainID, appRow.ID, "app-"+domainID.String()[:8]+".example.com", domainCreatedAt)
	if err != nil {
		t.Fatalf("insert domain: %v", err)
	}
	var storedCreatedAt, storedUpdatedAt time.Time
	if err := st.DB.QueryRow(ctx,
		`SELECT created_at, updated_at FROM application_domains WHERE id = $1`, domainID,
	).Scan(&storedCreatedAt, &storedUpdatedAt); err != nil {
		t.Fatalf("read domain timestamps: %v", err)
	}
	// Regression guard: a database-defaulted timestamp here silently shifts
	// the grace window off the fixture clock and the tests stop covering it.
	if !storedCreatedAt.Equal(domainCreatedAt) || !storedUpdatedAt.Equal(domainCreatedAt) {
		t.Fatalf("domain timestamps not on the fixture clock: created_at=%v updated_at=%v want=%v",
			storedCreatedAt, storedUpdatedAt, domainCreatedAt)
	}
	return integrationFixture{
		store:    st,
		appID:    uuid.UUID(appRow.ID.Bytes),
		domainID: domainID,
		project:  uuid.UUID(project.ID.Bytes),
		now:      now,
	}
}

func (f integrationFixture) target(t *testing.T) Target {
	t.Helper()
	target, err := (&Service{st: f.store}).target(context.Background(), f.appID, f.domainID)
	if err != nil {
		t.Fatalf("target: %v", err)
	}
	return target
}

func TestRecordPersistsIntervalsAndConfirmsIncidents(t *testing.T) {
	fixture := newIntegrationFixture(t)
	reporter := &recordingReporter{}
	now := fixture.now
	svc := NewService(Config{
		Store:     fixture.store,
		Incidents: reporter,
		Now:       func() time.Time { return now },
		Grace:     time.Minute,
	})
	svc.startedAt = now.Add(-time.Hour)
	target := fixture.target(t)
	ctx := context.Background()

	record := func(at time.Time, result Result) {
		t.Helper()
		result.At = at
		if err := svc.record(ctx, target, result); err != nil {
			t.Fatalf("record: %v", err)
		}
	}

	// First completed probe has no previous interval, so it contributes no
	// healthy time even though the target is reachable.
	record(now, Result{Outcome: OutcomeHealthy, StatusCode: 200, Latency: 40 * time.Millisecond})
	state, err := fixture.store.GetDomainMonitorState(ctx, pgUUID(fixture.domainID))
	if err != nil {
		t.Fatalf("state: %v", err)
	}
	if state.LastCompletedAt.Time.UTC().Equal(time.Time{}) {
		t.Fatal("completed probe not stored")
	}
	rows, err := fixture.store.ListDomainProbeResults(ctx, sqlc.ListDomainProbeResultsParams{
		DomainID:   pgUUID(fixture.domainID),
		ObservedAt: pgTimestamptz(now.Add(-time.Hour)),
		Limit:      10,
	})
	if err != nil {
		t.Fatalf("results: %v", err)
	}
	if len(rows) != 1 || rows[0].ValidSeconds != 0 {
		t.Fatalf("first interval = %+v", rows)
	}

	// A failed streak below the confirmation count is local only.
	now = now.Add(time.Minute)
	record(now, Result{Outcome: OutcomeFailed, Reason: ReasonTimeout})
	now = now.Add(time.Minute)
	record(now, Result{Outcome: OutcomeFailed, Reason: ReasonTimeout})
	if len(reporter.observations) != 0 {
		t.Fatalf("incident reported before three failures")
	}
	now = now.Add(time.Minute)
	record(now, Result{Outcome: OutcomeFailed, Reason: ReasonTimeout})
	if len(reporter.observations) != 1 || !reporter.observations[0].Unhealthy {
		t.Fatalf("incident not opened after three failures: %+v", reporter.observations)
	}
	state, _ = fixture.store.GetDomainMonitorState(ctx, pgUUID(fixture.domainID))
	if state.ConsecutiveFailures != 3 {
		t.Fatalf("consecutive failures = %d", state.ConsecutiveFailures)
	}
	rows, _ = fixture.store.ListDomainProbeResults(ctx, sqlc.ListDomainProbeResultsParams{
		DomainID:   pgUUID(fixture.domainID),
		ObservedAt: pgTimestamptz(now.Add(-time.Hour)),
		Limit:      10,
	})
	valid := int32(0)
	for _, row := range rows {
		valid += row.ValidSeconds
	}
	if valid == 0 {
		t.Fatal("completed probe intervals were not persisted")
	}

	// Two successes recover; the first success alone does not.
	now = now.Add(time.Minute)
	record(now, Result{Outcome: OutcomeHealthy, StatusCode: 200, Latency: 30 * time.Millisecond})
	if len(reporter.observations) != 1 {
		t.Fatalf("recovered after one success")
	}
	now = now.Add(time.Minute)
	record(now, Result{Outcome: OutcomeHealthy, StatusCode: 200, Latency: 30 * time.Millisecond})
	if len(reporter.observations) != 2 || reporter.observations[1].Unhealthy {
		t.Fatalf("recovery observation = %+v", reporter.observations)
	}

	// A long gap is unknown time: it is not counted and the failure streak
	// cannot survive a restart-sized gap.
	state, _ = fixture.store.GetDomainMonitorState(ctx, pgUUID(fixture.domainID))
	before := state.LastCompletedAt.Time
	now = now.Add(10 * time.Minute)
	record(now, Result{Outcome: OutcomeFailed, Reason: ReasonTimeout})
	rows, _ = fixture.store.ListDomainProbeResults(ctx, sqlc.ListDomainProbeResultsParams{
		DomainID:   pgUUID(fixture.domainID),
		ObservedAt: pgTimestamptz(before),
		Limit:      10,
	})
	if len(rows) == 0 || rows[0].ValidSeconds != 0 {
		t.Fatalf("gap interval counted as observed: %+v", rows[0])
	}
	state, _ = fixture.store.GetDomainMonitorState(ctx, pgUUID(fixture.domainID))
	if state.ConsecutiveFailures != 1 {
		t.Fatalf("failure streak survived a gap: %d", state.ConsecutiveFailures)
	}
}

func TestDisabledDomainClosesIncidentAndSettingsArmGrace(t *testing.T) {
	fixture := newIntegrationFixture(t)
	reporter := &recordingReporter{}
	now := fixture.now
	svc := NewService(Config{
		Store:     fixture.store,
		Incidents: reporter,
		Now:       func() time.Time { return now },
		Grace:     ConfigGrace,
	})
	svc.startedAt = now.Add(-time.Hour)
	ctx := context.Background()

	view, err := svc.SaveSettings(ctx, fixture.appID, fixture.domainID, SettingsInput{
		Enabled:     false,
		Path:        "/health",
		ExpectedMin: 200,
		ExpectedMax: 399,
		Timeout:     5,
	})
	if err != nil {
		t.Fatalf("SaveSettings: %v", err)
	}
	if view.Enabled || view.Path != "/health" || view.TimeoutSeconds != 5 {
		t.Fatalf("view = %+v", view)
	}
	target := fixture.target(t)
	if got := svc.graceUntil(target); !got.Equal(now.Add(ConfigGrace)) {
		t.Fatalf("grace = %v", got)
	}

	if _, err := svc.SaveSettings(ctx, fixture.appID, fixture.domainID, SettingsInput{
		Enabled: true, Path: "health", ExpectedMin: 200, ExpectedMax: 399, Timeout: 5,
	}); err == nil {
		t.Fatal("invalid path was accepted")
	}
	if _, err := svc.SaveSettings(ctx, fixture.appID, fixture.domainID, SettingsInput{
		Enabled: true, Path: "/", ExpectedMin: 500, ExpectedMax: 400, Timeout: 5,
	}); err == nil {
		t.Fatal("inverted status range was accepted")
	}
	if _, err := svc.SaveSettings(ctx, fixture.appID, fixture.domainID, SettingsInput{
		Enabled: true, Path: "/", ExpectedMin: 200, ExpectedMax: 399, Timeout: 31,
	}); err == nil {
		t.Fatal("out-of-range timeout was accepted")
	}
}

type countingRunner struct {
	calls int
	now   func() time.Time
}

func (r *countingRunner) Probe(_ context.Context, _ Target) Result {
	r.calls++
	return Result{Outcome: OutcomeHealthy, StatusCode: 200, Latency: 10 * time.Millisecond, At: r.now()}
}

func TestTickSuppressesStoppedAndNeverDeployedThenResumes(t *testing.T) {
	fixture := newIntegrationFixture(t)
	ctx := context.Background()
	now := fixture.now
	runner := &countingRunner{now: func() time.Time { return now }}
	reporter := &recordingReporter{}
	svc := NewService(Config{
		Store:     fixture.store,
		Incidents: reporter,
		Now:       func() time.Time { return now },
		Prober:    runner,
		Grace:     ConfigGrace,
	})
	svc.startedAt = now.Add(-time.Hour)

	results := func() int {
		t.Helper()
		rows, err := fixture.store.ListDomainProbeResults(ctx, sqlc.ListDomainProbeResultsParams{
			DomainID:   pgUUID(fixture.domainID),
			ObservedAt: pgTimestamptz(now.Add(-time.Hour)),
			Limit:      10,
		})
		if err != nil {
			t.Fatalf("results: %v", err)
		}
		return len(rows)
	}

	// No deployment yet: never probes and closes as not_deployed.
	svc.tick(ctx)
	if runner.calls != 0 || results() != 0 {
		t.Fatalf("never-deployed app was probed: calls=%d results=%d", runner.calls, results())
	}
	if len(reporter.closedReasons) == 0 || reporter.closedReasons[0] != "not_deployed" {
		t.Fatalf("never-deployed close reasons = %v", reporter.closedReasons)
	}

	// First deployment makes the target probeable once its grace has passed.
	if _, err := fixture.store.DB.Exec(ctx, `INSERT INTO deployments
		(application_id, kind, state, started_at, finished_at)
		VALUES ($1, 'deploy', 'running', $2, $3)`,
		fixture.appID, now.Add(-5*time.Minute), now.Add(-4*time.Minute)); err != nil {
		t.Fatalf("insert deployment: %v", err)
	}
	svc.tick(ctx)
	if runner.calls != 1 {
		t.Fatalf("running deployed app was not probed: calls=%d", runner.calls)
	}
	if results() != 1 {
		t.Fatalf("probe result not persisted: %d", results())
	}

	// Stopping the application suppresses probes and closes any incident.
	if _, err := fixture.store.DB.Exec(ctx, `INSERT INTO runtime_monitor_settings
		(resource_type, resource_id, desired_state)
		VALUES ('application', $1, 'stopped')`, fixture.appID); err != nil {
		t.Fatalf("stop app: %v", err)
	}
	svc.tick(ctx)
	if runner.calls != 1 {
		t.Fatalf("stopped app was probed: calls=%d", runner.calls)
	}
	if len(reporter.closedReasons) < 2 || reporter.closedReasons[len(reporter.closedReasons)-1] != "intentionally_stopped" {
		t.Fatalf("stopped close reasons = %v", reporter.closedReasons)
	}

	// Starting again arms the runtime grace: no probe until it expires.
	now = now.Add(time.Minute)
	if _, err := fixture.store.DB.Exec(ctx, `UPDATE runtime_monitor_settings
		SET desired_state = 'running', grace_until = $2 WHERE resource_type = 'application' AND resource_id = $1`,
		fixture.appID, now.Add(ConfigGrace)); err != nil {
		t.Fatalf("start app: %v", err)
	}
	svc.tick(ctx)
	if runner.calls != 1 {
		t.Fatalf("started app ignored runtime grace: calls=%d", runner.calls)
	}

	now = now.Add(ConfigGrace + time.Second)
	svc.tick(ctx)
	if runner.calls != 2 {
		t.Fatalf("started app did not resume probing: calls=%d", runner.calls)
	}
}
