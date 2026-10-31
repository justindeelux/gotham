package cleanup

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/justindeelux/gotham/internal/servers"
	"github.com/justindeelux/gotham/internal/store"
	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
)

type fakeCenter struct {
	schedules  []store.CleanupSchedule
	runs       []store.CleanupRunView
	items      []store.CleanupRunItem
	exclusions []store.CleanupExclusion
	threshold  store.CleanupThreshold
	latestRun  *store.CleanupRun
	marked     []uuid.UUID
	nextRun    time.Time
	err        error
}

func (f *fakeCenter) ListCleanupSchedules(context.Context) ([]store.CleanupSchedule, error) {
	return f.schedules, f.err
}

func (f *fakeCenter) GetCleanupSchedule(_ context.Context, id uuid.UUID) (store.CleanupSchedule, error) {
	for _, schedule := range f.schedules {
		if schedule.ID == id {
			return schedule, nil
		}
	}
	return store.CleanupSchedule{}, errors.New("not found")
}

func (f *fakeCenter) CreateCleanupSchedule(_ context.Context, schedule store.CleanupSchedule) (store.CleanupSchedule, error) {
	f.schedules = append(f.schedules, schedule)
	return schedule, f.err
}

func (f *fakeCenter) UpdateCleanupSchedule(_ context.Context, schedule store.CleanupSchedule) (store.CleanupSchedule, error) {
	for i, existing := range f.schedules {
		if existing.ID == schedule.ID {
			f.schedules[i] = schedule
			return schedule, f.err
		}
	}
	return store.CleanupSchedule{}, errors.New("not found")
}

func (f *fakeCenter) DeleteCleanupSchedule(context.Context, uuid.UUID) error { return f.err }

func (f *fakeCenter) ListDueCleanupSchedules(_ context.Context, now time.Time) ([]store.CleanupSchedule, error) {
	due := make([]store.CleanupSchedule, 0, len(f.schedules))
	for _, schedule := range f.schedules {
		if schedule.Enabled && !schedule.NextRunAt.After(now) {
			due = append(due, schedule)
		}
	}
	return due, f.err
}

func (f *fakeCenter) MarkCleanupScheduleRun(
	_ context.Context, id uuid.UUID, ranAt, next time.Time,
) (store.CleanupSchedule, error) {
	f.marked = append(f.marked, id)
	f.nextRun = next
	for i, schedule := range f.schedules {
		if schedule.ID == id {
			f.schedules[i].LastRunAt = &ranAt
			f.schedules[i].NextRunAt = next
			return f.schedules[i], nil
		}
	}
	return store.CleanupSchedule{}, errors.New("not found")
}

func (f *fakeCenter) ListCleanupRuns(context.Context, store.CleanupRunFilter) ([]store.CleanupRunView, error) {
	return f.runs, f.err
}

func (f *fakeCenter) GetCleanupRun(context.Context, uuid.UUID) (store.CleanupRunView, error) {
	if len(f.runs) == 0 {
		return store.CleanupRunView{}, errors.New("not found")
	}
	return f.runs[0], f.err
}

func (f *fakeCenter) ListCleanupRunItems(context.Context, uuid.UUID) ([]store.CleanupRunItem, error) {
	return f.items, f.err
}

func (f *fakeCenter) LatestCleanupThresholdRun(context.Context, uuid.UUID) (store.CleanupRun, error) {
	if f.latestRun == nil {
		return store.CleanupRun{}, pgx.ErrNoRows
	}
	return *f.latestRun, f.err
}

func (f *fakeCenter) GetCleanupThreshold(context.Context) (store.CleanupThreshold, error) {
	return f.threshold, f.err
}

func (f *fakeCenter) SaveCleanupThreshold(
	_ context.Context, _ uuid.UUID, threshold store.CleanupThreshold, _ any,
) error {
	f.threshold = threshold
	return f.err
}

func (f *fakeCenter) ListCleanupExclusions(context.Context) ([]store.CleanupExclusion, error) {
	return f.exclusions, f.err
}

func (f *fakeCenter) CreateCleanupExclusion(
	_ context.Context, exclusion store.CleanupExclusion,
) (store.CleanupExclusion, error) {
	f.exclusions = append(f.exclusions, exclusion)
	return exclusion, f.err
}

func (f *fakeCenter) DeleteCleanupExclusion(context.Context, uuid.UUID) error { return f.err }

type fakeNotifier struct {
	mu        sync.Mutex
	summaries []ScheduleRunSummary
}

func (f *fakeNotifier) CleanupFinished(_ context.Context, summary ScheduleRunSummary) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.summaries = append(f.summaries, summary)
}

func (f *fakeNotifier) notified() []ScheduleRunSummary {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]ScheduleRunSummary(nil), f.summaries...)
}

func testScheduledService(
	t *testing.T, agent *fakeAgent, st *fakeStore, center *fakeCenter, notifier *fakeNotifier,
) (*Service, uuid.UUID) {
	t.Helper()
	server := &servers.Server{ID: uuid.New(), Name: "node", Status: servers.StatusReady}
	svc := NewService(Config{
		Registry: &fakeRegistry{server: server},
		Dial: func(context.Context, *servers.Server) (DockerClient, error) {
			return agent, nil
		},
		Store:    st,
		Center:   center,
		Notifier: notifier,
		Timeout:  5 * time.Second,
	})
	svc.now = func() time.Time { return time.Date(2026, 10, 31, 3, 0, 0, 0, time.UTC) }
	svc.loc = time.UTC
	return svc, server.ID
}

func buildCacheAgent(size int64) *fakeAgent {
	return &fakeAgent{
		scanResp: &agentv1.CleanupScanResponse{Items: []*agentv1.CleanupItem{{
			Id: idBuildCache, Category: CategoryBuildCache, Name: "Build cache", SizeBytes: size,
		}}},
		cleanResp: &agentv1.CleanupCleanResponse{Results: []*agentv1.CleanupItemResult{{
			Id: idBuildCache, Status: "removed", ReclaimedBytes: size,
		}}},
	}
}

func TestSchedulerFiresDueScheduleOnceAndRecordsIt(t *testing.T) {
	server := &servers.Server{ID: uuid.New(), Name: "node", Status: servers.StatusReady}
	svc, _ := testScheduledService(t, buildCacheAgent(100), &fakeStore{}, &fakeCenter{}, &fakeNotifier{})
	serverID := server.ID
	center := &fakeCenter{schedules: []store.CleanupSchedule{{
		ID: uuid.New(), Name: "nightly", ServerIDs: []uuid.UUID{serverID},
		Categories: []string{CategoryBuildCache}, Cron: "0 3 * * *", MinAgeHours: 24,
		Enabled: true, NextRunAt: svc.now().Add(-time.Minute),
	}}}
	notifier := &fakeNotifier{}
	svc.center = center
	svc.notifier = notifier
	svc.registry = &fakeRegistry{server: &servers.Server{ID: serverID, Name: "node", Status: servers.StatusReady}}

	scheduler := NewScheduler(svc, time.Minute)
	started, err := scheduler.Tick(context.Background())
	if err != nil {
		t.Fatalf("Tick: %v", err)
	}
	if started != 1 {
		t.Fatalf("started = %d, want 1", started)
	}
	if len(center.marked) != 1 || !center.nextRun.After(svc.now()) {
		t.Fatalf("marked = %v next = %v; want the slot advanced", center.marked, center.nextRun)
	}
	if len(svc.store.(*fakeStore).runs) != 1 {
		t.Fatalf("runs = %+v, want one recorded run", svc.store.(*fakeStore).runs)
	}
	run := svc.store.(*fakeStore).runs[0]
	if run.Trigger != TriggerSchedule || run.ScheduleID == nil || run.ReclaimedBytes != 100 {
		t.Fatalf("run = %+v, want a schedule run with 100 bytes and its schedule id", run)
	}
	if len(notifier.summaries) != 1 || notifier.summaries[0].Failed() {
		t.Fatalf("summaries = %+v, want one success", notifier.summaries)
	}
	if notifier.summaries[0].TotalBytes() != 100 {
		t.Fatalf("freed = %d, want 100", notifier.summaries[0].TotalBytes())
	}

	started, err = scheduler.Tick(context.Background())
	if err != nil || started != 0 {
		t.Fatalf("second tick = %d, %v; want the advanced schedule not to fire", started, err)
	}
}

func TestSchedulerSkipsAnInFlightSchedule(t *testing.T) {
	svc, serverID := testScheduledService(t, buildCacheAgent(10), &fakeStore{}, &fakeCenter{}, &fakeNotifier{})
	center := &fakeCenter{schedules: []store.CleanupSchedule{{
		ID: uuid.New(), Name: "nightly", ServerIDs: []uuid.UUID{serverID},
		Categories: []string{CategoryBuildCache}, Cron: "0 3 * * *", MinAgeHours: 24,
		Enabled: true, NextRunAt: svc.now().Add(-time.Minute),
	}}}
	svc.center = center
	if !svc.claimSchedule(center.schedules[0].ID) {
		t.Fatal("could not seed the in-flight claim")
	}

	started, err := NewScheduler(svc, time.Minute).Tick(context.Background())
	if err != nil {
		t.Fatalf("Tick: %v", err)
	}
	if started != 0 || len(center.marked) != 0 {
		t.Fatalf("started = %d marked = %v; an in-flight schedule must not fire again", started, center.marked)
	}
}

func TestScheduledRunSkipsOfflineNodeAndRecordsReason(t *testing.T) {
	agent := &fakeAgent{scanErr: status.Error(codes.Unavailable, "down")}
	notifier := &fakeNotifier{}
	svc, serverID := testScheduledService(t, agent, &fakeStore{}, &fakeCenter{}, notifier)

	summary := svc.executeSchedule(context.Background(), store.CleanupSchedule{
		ID: uuid.New(), Name: "nightly", ServerIDs: []uuid.UUID{serverID},
		Categories: []string{CategoryBuildCache}, Cron: "0 3 * * *", MinAgeHours: 24, Enabled: true,
	})
	if len(summary.Nodes) != 1 || summary.Nodes[0].Status != StatusSkipped ||
		summary.Nodes[0].Reason != ReasonOffline {
		t.Fatalf("summary = %+v; want one skipped offline node", summary)
	}
	runs := svc.store.(*fakeStore).runs
	if len(runs) != 1 || runs[0].Status != StatusSkipped || runs[0].Reason != ReasonOffline ||
		runs[0].ReclaimedBytes != 0 {
		t.Fatalf("runs = %+v; want one zero-byte skipped run with the reason", runs)
	}
}

func TestScheduledRunSkipsANodeWhoseLockIsHeld(t *testing.T) {
	svc, serverID := testScheduledService(t, buildCacheAgent(10), &fakeStore{}, &fakeCenter{}, &fakeNotifier{})
	unlock := svc.lockNode(serverID.String())

	summary := svc.executeSchedule(context.Background(), store.CleanupSchedule{
		ID: uuid.New(), Name: "nightly", ServerIDs: []uuid.UUID{serverID},
		Categories: []string{CategoryBuildCache}, Cron: "0 3 * * *", MinAgeHours: 24, Enabled: true,
	})
	unlock()
	if len(summary.Nodes) != 1 || summary.Nodes[0].Status != StatusSkipped ||
		summary.Nodes[0].Reason != ReasonBusy {
		t.Fatalf("summary = %+v; want skipped busy", summary)
	}
	runs := svc.store.(*fakeStore).runs
	if len(runs) != 1 || runs[0].Reason != ReasonBusy {
		t.Fatalf("runs = %+v; want the busy attempt recorded", runs)
	}
}

func TestScheduledRunFiltersDangerousCategoriesAndKeepsScheduledOnes(t *testing.T) {
	agent := &fakeAgent{
		scanResp: &agentv1.CleanupScanResponse{Items: []*agentv1.CleanupItem{
			{Id: idBuildCache, Category: CategoryBuildCache, Name: "Build cache", SizeBytes: 10},
			{Id: "image:sha256:x", Category: CategoryUnusedImages, Name: "app:1", SizeBytes: 20},
		}},
		cleanResp: &agentv1.CleanupCleanResponse{},
	}
	svc, serverID := testScheduledService(t, agent, &fakeStore{}, &fakeCenter{}, &fakeNotifier{})

	svc.executeSchedule(context.Background(), store.CleanupSchedule{
		ID: uuid.New(), Name: "nightly", ServerIDs: []uuid.UUID{serverID},
		Categories: []string{CategoryBuildCache, CategoryOrphanedVolumes},
		Cron:       "0 3 * * *", MinAgeHours: 24, Enabled: true,
	})
	if len(agent.cleaned) != 1 || agent.cleaned[0] != idBuildCache {
		t.Fatalf("cleaned = %v; want only the scheduled safe category", agent.cleaned)
	}
}

func TestExclusionsFilterScanAndCleanOnEveryTrigger(t *testing.T) {
	agent := buildCacheAgent(10)
	for _, trigger := range []string{TriggerManual, TriggerSchedule, TriggerThreshold} {
		t.Run(trigger, func(t *testing.T) {
			server := &servers.Server{ID: uuid.New(), Name: "node", Status: servers.StatusReady}
			st := &fakeStore{exclusions: []store.CleanupExclusion{
				{ID: uuid.New(), Category: CategoryBuildCache, Match: "Build cache"},
			}}
			svc := NewService(Config{
				Registry: &fakeRegistry{server: server},
				Dial: func(context.Context, *servers.Server) (DockerClient, error) {
					return agent, nil
				},
				Store: st,
			})
			agent.cleaned = nil

			scans := svc.Scan(context.Background(), []string{server.ID.String()}, 0)
			if len(scans) != 1 || len(scans[0].Items) != 0 {
				t.Fatalf("scan = %+v; an excluded item must not be offered", scans)
			}
			result := svc.Clean(context.Background(), uuid.New(), trigger, server.ID.String(),
				[]string{idBuildCache}, 0)
			if len(result.Items) != 1 || result.Items[0].Status != StatusSkipped ||
				result.Items[0].Message != "excluded" {
				t.Fatalf("clean = %+v; want one skipped excluded item", result)
			}
			if len(agent.cleaned) != 0 {
				t.Fatalf("agent ids = %v; an excluded item must never be removed", agent.cleaned)
			}
		})
	}
}

func TestExclusionIsScopedToItsNode(t *testing.T) {
	first := &servers.Server{ID: uuid.New(), Name: "one", Status: servers.StatusReady}
	second := &servers.Server{ID: uuid.New(), Name: "two", Status: servers.StatusReady}
	svc := NewService(Config{
		Registry: registryByID{first.ID: first, second.ID: second},
		Dial: func(context.Context, *servers.Server) (DockerClient, error) {
			return buildCacheAgent(10), nil
		},
		Store: &fakeStore{exclusions: []store.CleanupExclusion{{
			ID: uuid.New(), ServerID: &first.ID, Category: CategoryBuildCache, Match: idBuildCache,
		}}},
	})
	scans := svc.Scan(context.Background(), []string{first.ID.String(), second.ID.String()}, 0)
	if len(scans) != 2 || len(scans[0].Items) != 0 || len(scans[1].Items) != 1 {
		t.Fatalf("scans = %+v; the exclusion must only protect its own node", scans)
	}
}

type registryByID map[uuid.UUID]*servers.Server

func (r registryByID) Get(_ context.Context, id uuid.UUID) (*servers.Server, error) {
	if server, ok := r[id]; ok {
		return server, nil
	}
	return nil, servers.ErrNotFound
}

func TestScheduleValidation(t *testing.T) {
	server := &servers.Server{ID: uuid.New(), Name: "node", Status: servers.StatusReady}
	center := &fakeCenter{}
	svc := NewService(Config{
		Registry: registryByID{server.ID: server},
		Store:    &fakeStore{},
		Center:   center,
	})
	svc.now = func() time.Time { return time.Date(2026, 10, 31, 3, 0, 0, 0, time.UTC) }
	svc.loc = time.UTC
	base := ScheduleInput{
		Name: "nightly", ServerIDs: []uuid.UUID{server.ID}, Categories: []string{CategoryBuildCache},
		Cron: "0 3 * * *", MinAgeHours: 24,
	}

	if _, err := svc.CreateSchedule(context.Background(), uuid.New(), base); err != nil {
		t.Fatalf("valid schedule: %v", err)
	}
	dangerous := base
	dangerous.Categories = []string{CategoryOrphanedVolumes}
	if _, err := svc.CreateSchedule(context.Background(), uuid.New(), dangerous); !errors.Is(err, ErrDangerousCategory) {
		t.Fatalf("dangerous = %v, want ErrDangerousCategory", err)
	}
	badCron := base
	badCron.Cron = "bogus"
	if _, err := svc.CreateSchedule(context.Background(), uuid.New(), badCron); !errors.Is(err, ErrValidation) {
		t.Fatalf("bad cron = %v, want ErrValidation", err)
	}
	unknownServer := base
	unknownServer.ServerIDs = []uuid.UUID{uuid.New()}
	if _, err := svc.CreateSchedule(context.Background(), uuid.New(), unknownServer); !errors.Is(err, ErrInvalidServerID) {
		t.Fatalf("unknown server = %v, want ErrInvalidServerID", err)
	}
	noName := base
	noName.Name = ""
	if _, err := svc.CreateSchedule(context.Background(), uuid.New(), noName); !errors.Is(err, ErrValidation) {
		t.Fatalf("missing name = %v, want ErrValidation", err)
	}
	unknownCategory := base
	unknownCategory.Categories = []string{"bogus"}
	if _, err := svc.CreateSchedule(context.Background(), uuid.New(), unknownCategory); !errors.Is(err, ErrInvalidCategory) {
		t.Fatalf("unknown category = %v, want ErrInvalidCategory", err)
	}
}

func TestScheduledNotificationIsFailureWhenANodeFails(t *testing.T) {
	agent := &fakeAgent{scanErr: status.Error(codes.Internal, "boom")}
	notifier := &fakeNotifier{}
	svc, serverID := testScheduledService(t, agent, &fakeStore{}, &fakeCenter{}, notifier)

	summary := svc.executeSchedule(context.Background(), store.CleanupSchedule{
		ID: uuid.New(), Name: "nightly", ServerIDs: []uuid.UUID{serverID},
		Categories: []string{CategoryBuildCache}, Cron: "0 3 * * *", MinAgeHours: 24, Enabled: true,
	})
	if !summary.Failed() {
		t.Fatalf("summary = %+v; want a failed node", summary)
	}
	if len(notifier.summaries) != 1 || !notifier.summaries[0].Failed() {
		t.Fatalf("summaries = %+v; want one failure event", notifier.summaries)
	}
}

func TestCenterStoresSchedulesExclusionsAndRuns(t *testing.T) {
	ctx, dsn := cleanupScratch(t)
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("open pool: %v", err)
	}
	t.Cleanup(pool.Close)
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close(context.Background()) })

	ownerID, serverID := uuid.New(), uuid.New()
	if _, err := conn.Exec(ctx,
		`INSERT INTO users(id,email,password_hash,role) VALUES($1,$2,'x','owner')`,
		ownerID, "cleanup-center-"+ownerID.String()+"@example.com"); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	if _, err := conn.Exec(ctx,
		`INSERT INTO servers(id,name,ip,ssh_user,status) VALUES($1,'node','127.0.0.1','root','ready')`,
		serverID); err != nil {
		t.Fatalf("insert server: %v", err)
	}

	st := store.New(pool)
	svc := NewService(Config{
		Registry: registryByID{serverID: &servers.Server{ID: serverID, Name: "node", Status: servers.StatusReady}},
		Store:    st,
		Center:   st,
	})
	svc.now = func() time.Time { return time.Date(2026, 10, 31, 3, 0, 0, 0, time.UTC) }
	svc.loc = time.UTC

	enabled := true
	schedule, err := svc.CreateSchedule(ctx, ownerID, ScheduleInput{
		Name: "nightly", ServerIDs: []uuid.UUID{serverID}, Categories: []string{CategoryBuildCache},
		Cron: "0 3 * * *", MinAgeHours: 24, Enabled: &enabled,
	})
	if err != nil {
		t.Fatalf("CreateSchedule: %v", err)
	}
	if schedule.Name != "nightly" || !schedule.NextRunAt.After(svc.now()) {
		t.Fatalf("schedule = %+v", schedule)
	}
	listed, err := svc.ListSchedules(ctx)
	if err != nil || len(listed) != 1 {
		t.Fatalf("ListSchedules = %+v, %v", listed, err)
	}

	// A due schedule advances before its work starts.
	if _, err := conn.Exec(ctx, `UPDATE cleanup_schedules SET next_run_at = $2 WHERE id = $1`,
		schedule.ID, svc.now().Add(-time.Minute)); err != nil {
		t.Fatalf("make due: %v", err)
	}
	due, err := st.ListDueCleanupSchedules(ctx, svc.now())
	if err != nil || len(due) != 1 {
		t.Fatalf("ListDueCleanupSchedules = %+v, %v", due, err)
	}
	advanced, err := st.MarkCleanupScheduleRun(ctx, schedule.ID, svc.now(), svc.now().Add(24*time.Hour))
	if err != nil || !advanced.LastRunAt.Equal(svc.now()) || !advanced.NextRunAt.After(svc.now()) {
		t.Fatalf("MarkCleanupScheduleRun = %+v, %v", advanced, err)
	}

	// Exclusions are idempotent per server/category/match, and a NULL server is
	// the global variant.
	global, err := svc.CreateExclusion(ctx, ownerID, ExclusionInput{
		Category: CategoryBuildCache, Match: "Build cache", Note: "keep",
	})
	if err != nil {
		t.Fatalf("CreateExclusion: %v", err)
	}
	again, err := svc.CreateExclusion(ctx, ownerID, ExclusionInput{
		Category: CategoryBuildCache, Match: "Build cache",
	})
	if err != nil || again.ID != global.ID {
		t.Fatalf("duplicate exclusion = %+v, %v; want the same row", again, err)
	}
	if _, err := svc.CreateExclusion(ctx, ownerID, ExclusionInput{
		ServerID: &serverID, Category: CategoryBuildCache, Match: "Build cache",
	}); err != nil {
		t.Fatalf("CreateExclusion per node: %v", err)
	}
	exclusions, err := svc.ListExclusions(ctx)
	if err != nil || len(exclusions) != 2 {
		t.Fatalf("ListExclusions = %+v, %v", exclusions, err)
	}

	// A scheduled run records its schedule, status and per-category totals.
	runID := uuid.New()
	started := svc.now()
	if err := st.RecordCleanupRun(ctx, store.CleanupRun{
		ID: runID, ServerID: serverID, Trigger: TriggerSchedule, ScheduleID: &schedule.ID,
		StartedAt: started, FinishedAt: started.Add(time.Second), ReclaimedBytes: 42,
		Status: StatusOK,
	}, []store.CleanupRunItem{{
		ItemID: idBuildCache, Category: CategoryBuildCache, Name: "Build cache",
		Status: "removed", ReclaimedBytes: 42,
	}}); err != nil {
		t.Fatalf("RecordCleanupRun: %v", err)
	}
	runs, err := svc.ListRuns(ctx, store.CleanupRunFilter{ServerID: &serverID})
	if err != nil || len(runs) != 1 {
		t.Fatalf("ListRuns = %+v, %v", runs, err)
	}
	if runs[0].ScheduleName != "nightly" || runs[0].Categories[CategoryBuildCache] != 42 {
		t.Fatalf("run = %+v; want the schedule name and category total", runs[0])
	}
	detail, items, err := svc.GetRun(ctx, runID)
	if err != nil || detail.ScheduleName != "nightly" || len(items) != 1 {
		t.Fatalf("GetRun = %+v %+v %v", detail, items, err)
	}

	// Deleting the schedule keeps the history but drops the link.
	if err := svc.DeleteSchedule(ctx, schedule.ID); err != nil {
		t.Fatalf("DeleteSchedule: %v", err)
	}
	runs, err = svc.ListRuns(ctx, store.CleanupRunFilter{})
	if err != nil || len(runs) != 1 || runs[0].ScheduleID != nil || runs[0].ScheduleName != "" {
		t.Fatalf("runs after delete = %+v, %v", runs, err)
	}
}

// TestDeletedNodeAndSameSecondHistory covers the two rundown edges: a schedule
// must not fail (or write an FK-violating run) for a node that was deleted, and
// the history cursor must keep runs that share a whole second.
func TestDeletedNodeAndSameSecondHistory(t *testing.T) {
	ctx, dsn := cleanupScratch(t)
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("open pool: %v", err)
	}
	t.Cleanup(pool.Close)
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close(context.Background()) })

	ownerID, serverID := uuid.New(), uuid.New()
	if _, err := conn.Exec(ctx,
		`INSERT INTO users(id,email,password_hash,role) VALUES($1,$2,'x','owner')`,
		ownerID, "cleanup-edge-"+ownerID.String()+"@example.com"); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	if _, err := conn.Exec(ctx,
		`INSERT INTO servers(id,name,ip,ssh_user,status) VALUES($1,'node','127.0.0.1','root','ready')`,
		serverID); err != nil {
		t.Fatalf("insert server: %v", err)
	}

	st := store.New(pool)
	notifier := &fakeNotifier{}
	svc := NewService(Config{
		Registry: registryByID{}, // every lookup is not_found
		Dial: func(context.Context, *servers.Server) (DockerClient, error) {
			return &fakeAgent{}, nil
		},
		Store:    st,
		Center:   st,
		Notifier: notifier,
	})
	svc.now = func() time.Time { return time.Date(2026, 10, 31, 3, 0, 0, 0, time.UTC) }
	svc.loc = time.UTC

	deletedID := uuid.New()
	summary := svc.executeSchedule(ctx, store.CleanupSchedule{
		ID: uuid.New(), Name: "nightly", ServerIDs: []uuid.UUID{deletedID},
		Categories: []string{CategoryBuildCache}, Cron: "0 3 * * *", MinAgeHours: 24, Enabled: true,
	})
	if len(summary.Nodes) != 1 || summary.Nodes[0].Status != StatusSkipped ||
		summary.Nodes[0].Reason != ReasonNotFound {
		t.Fatalf("summary = %+v; want one skipped not_found node", summary)
	}
	if summary.Failed() || len(notifier.summaries) != 1 || notifier.summaries[0].Failed() {
		t.Fatalf("summary/notifier = %+v/%+v; a deleted node is not a failure", summary, notifier.summaries)
	}
	var runs int
	if err := conn.QueryRow(ctx, `SELECT count(*) FROM cleanup_runs`).Scan(&runs); err != nil {
		t.Fatalf("count runs: %v", err)
	}
	if runs != 0 {
		t.Fatalf("runs = %d; a deleted node cannot be recorded (FK)", runs)
	}

	// Three runs inside one second; a second-precision cursor loses the oldest.
	base := time.Date(2026, 10, 31, 2, 0, 0, 100_000_000, time.UTC)
	for _, started := range []time.Time{
		base.Add(800 * time.Millisecond),
		base.Add(200 * time.Millisecond),
		base,
	} {
		if err := st.RecordCleanupRun(ctx, store.CleanupRun{
			ID: uuid.New(), ServerID: serverID, Trigger: TriggerSchedule,
			StartedAt: started, FinishedAt: started.Add(time.Second), Status: StatusOK,
		}, nil); err != nil {
			t.Fatalf("record run: %v", err)
		}
	}
	first, err := st.ListCleanupRuns(ctx, store.CleanupRunFilter{ServerID: &serverID, Limit: 2})
	if err != nil || len(first) != 2 {
		t.Fatalf("first page = %+v, %v", first, err)
	}
	cursor, err := time.Parse(time.RFC3339, newRunResponse(first[1]).StartedAt)
	if err != nil {
		t.Fatalf("parse cursor: %v", err)
	}
	second, err := st.ListCleanupRuns(ctx, store.CleanupRunFilter{ServerID: &serverID, Limit: 2, Before: &cursor})
	if err != nil || len(second) != 1 {
		t.Fatalf("second page = %+v, %v; the same-second run must not be skipped", second, err)
	}
}
