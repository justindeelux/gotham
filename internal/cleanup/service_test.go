package cleanup

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pressly/goose/v3"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/justindeelux/gotham/internal/containers"
	"github.com/justindeelux/gotham/internal/servers"
	"github.com/justindeelux/gotham/internal/store"
	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
)

type fakeRegistry struct {
	server *servers.Server
	err    error
}

func (f *fakeRegistry) Get(context.Context, uuid.UUID) (*servers.Server, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.server, nil
}

type fakeAgent struct {
	scanResp  *agentv1.CleanupScanResponse
	scanErr   error
	scanBlock chan struct{}
	scanCalls atomic.Int64
	cleanResp *agentv1.CleanupCleanResponse
	cleanErr  error
	cleaned   []string
}

func (f *fakeAgent) ScanCleanup(context.Context, *agentv1.CleanupScanRequest, ...grpc.CallOption) (*agentv1.CleanupScanResponse, error) {
	f.scanCalls.Add(1)
	if f.scanBlock != nil {
		<-f.scanBlock
	}
	if f.scanErr != nil {
		return nil, f.scanErr
	}
	if f.scanResp == nil {
		return &agentv1.CleanupScanResponse{}, nil
	}
	return f.scanResp, nil
}

func (f *fakeAgent) Clean(_ context.Context, req *agentv1.CleanupCleanRequest, _ ...grpc.CallOption) (*agentv1.CleanupCleanResponse, error) {
	f.cleaned = req.GetIds()
	if f.cleanErr != nil {
		return nil, f.cleanErr
	}
	if f.cleanResp == nil {
		return &agentv1.CleanupCleanResponse{}, nil
	}
	return f.cleanResp, nil
}

type fakeVolumes struct {
	volumes   []containers.OrphanedVolume
	listErr   error
	removeErr error
	removed   []string
}

func (f *fakeVolumes) ListOrphanedVolumes(context.Context, uuid.UUID) ([]containers.OrphanedVolume, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.volumes, nil
}

func (f *fakeVolumes) RemoveOrphanedVolume(_ context.Context, _ uuid.UUID, name string) error {
	if f.removeErr != nil {
		return f.removeErr
	}
	f.removed = append(f.removed, name)
	return nil
}

type fakeHost struct {
	status    HostCleanupStatus
	err       error
	reclaimed int64
	cleaned   []string
}

func (f *fakeHost) CleanupStatus(context.Context, uuid.UUID) (HostCleanupStatus, error) {
	return f.status, f.err
}

func (f *fakeHost) CleanupHost(_ context.Context, _ uuid.UUID, category string, _ time.Duration) (int64, error) {
	f.cleaned = append(f.cleaned, category)
	return f.reclaimed, f.err
}

type fakeStore struct {
	mu         sync.Mutex
	active     int64
	err        error
	runs       []store.CleanupRun
	items      [][]store.CleanupRunItem
	recordE    error
	exclusions []store.CleanupExclusion
	excludeErr error
}

func (f *fakeStore) recordedRuns() []store.CleanupRun {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]store.CleanupRun(nil), f.runs...)
}

func (f *fakeStore) ListCleanupExclusions(context.Context) ([]store.CleanupExclusion, error) {
	return f.exclusions, f.excludeErr
}

func (f *fakeStore) CountActiveDeploymentsByServer(context.Context, uuid.UUID) (int64, error) {
	return f.active, f.err
}

func (f *fakeStore) RecordCleanupRun(_ context.Context, run store.CleanupRun, items []store.CleanupRunItem) error {
	if f.recordE != nil {
		return f.recordE
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.runs = append(f.runs, run)
	f.items = append(f.items, items)
	return nil
}

func testService(t *testing.T, agent *fakeAgent, volumes containers.VolumeService, st Store) (*Service, uuid.UUID) {
	t.Helper()
	server := &servers.Server{ID: uuid.New(), Name: "node", Status: servers.StatusReady}
	registry := &fakeRegistry{server: server}
	svc := NewService(Config{
		Registry: registry,
		Dial: func(context.Context, *servers.Server) (DockerClient, error) {
			return agent, nil
		},
		Volumes: volumes,
		Store:   st,
		Timeout: 5 * time.Second,
	})
	return svc, server.ID
}

func testHostService(t *testing.T, host HostCleaner, st Store) (*Service, uuid.UUID) {
	t.Helper()
	server := &servers.Server{ID: uuid.New(), Name: "node", Status: servers.StatusReady}
	svc := NewService(Config{
		Registry: &fakeRegistry{server: server},
		Dial: func(context.Context, *servers.Server) (DockerClient, error) {
			return &fakeAgent{}, nil
		},
		Host:    host,
		Store:   st,
		Timeout: 5 * time.Second,
	})
	return svc, server.ID
}

func TestScanAddsHostCategories(t *testing.T) {
	host := &fakeHost{status: HostCleanupStatus{
		Journal:      HostCleanupCategory{Supported: true, Bytes: 100},
		PackageCache: HostCleanupCategory{Supported: true, Manager: "apt", Bytes: 50},
	}}
	svc, serverID := testHostService(t, host, &fakeStore{})

	scans := svc.Scan(context.Background(), []string{serverID.String()}, 0)
	if len(scans) != 1 || scans[0].Status != StatusOK || len(scans[0].Notices) != 0 {
		t.Fatalf("scan = %+v; want one ok host scan without notices", scans)
	}
	byCategory := map[string]Item{}
	for _, item := range scans[0].Items {
		byCategory[item.Category] = item
	}
	if byCategory[CategoryJournal].SizeBytes != 100 || byCategory[CategoryJournal].ID != "host:journal" ||
		byCategory[CategoryJournal].Reason != ReasonJournalArchived {
		t.Fatalf("journal item = %+v", byCategory[CategoryJournal])
	}
	if byCategory[CategoryPackageCache].SizeBytes != 50 || byCategory[CategoryPackageCache].ID != "host:package_cache" ||
		byCategory[CategoryPackageCache].Reason != ReasonPackageCache {
		t.Fatalf("package cache item = %+v", byCategory[CategoryPackageCache])
	}
}

func TestScanEmitsHostNotices(t *testing.T) {
	host := &fakeHost{status: HostCleanupStatus{
		Journal:      HostCleanupCategory{Supported: false},
		PackageCache: HostCleanupCategory{Supported: true, Bytes: 0},
	}}
	svc, serverID := testHostService(t, host, &fakeStore{})

	scans := svc.Scan(context.Background(), []string{serverID.String()}, 0)
	if len(scans) != 1 || scans[0].Status != StatusOK {
		t.Fatalf("scan = %+v; the node must stay ok", scans)
	}
	if len(scans[0].Items) != 0 {
		t.Fatalf("items = %+v; an empty cache is not offered", scans[0].Items)
	}
	if len(scans[0].Notices) != 1 || scans[0].Notices[0].Category != CategoryJournal ||
		scans[0].Notices[0].Reason != ReasonUnsupported {
		t.Fatalf("notices = %+v; want one unsupported journal notice", scans[0].Notices)
	}

	for err, reason := range map[error]string{
		ErrHostHelperOutdated: ReasonHelperOutdated,
		ErrHostAgentOutdated:  ReasonAgentOutdated,
		ErrHostUnsupported:    ReasonUnsupported,
	} {
		noticed := &fakeHost{err: err}
		svc, serverID := testHostService(t, noticed, &fakeStore{})
		scans := svc.Scan(context.Background(), []string{serverID.String()}, 0)
		if len(scans) != 1 || scans[0].Status != StatusOK || len(scans[0].Notices) != 2 {
			t.Fatalf("%v: scan = %+v; want two notices on an ok node", err, scans)
		}
		for _, notice := range scans[0].Notices {
			if notice.Reason != reason {
				t.Fatalf("%v: notice = %+v; want reason %s", err, notice, reason)
			}
		}
	}
}

func TestCleanRoutesHostCategoriesAndRecordsRun(t *testing.T) {
	host := &fakeHost{reclaimed: 40}
	st := &fakeStore{}
	svc, serverID := testHostService(t, host, st)

	result := svc.Clean(context.Background(), uuid.New(), TriggerSchedule, serverID.String(),
		[]string{"host:journal", "host:package_cache"}, 0)
	if result.Status != StatusOK || len(result.Items) != 2 {
		t.Fatalf("clean = %+v; want two results", result)
	}
	for _, item := range result.Items {
		if item.Status != statusRemoved || item.ReclaimedBytes != 40 {
			t.Fatalf("item = %+v; want removed with 40 reclaimed", item)
		}
	}
	if len(host.cleaned) != 2 || host.cleaned[0] != CategoryJournal || host.cleaned[1] != CategoryPackageCache {
		t.Fatalf("cleaned = %v; want both host categories", host.cleaned)
	}
	if len(st.runs) != 1 || st.runs[0].ReclaimedBytes != 80 || st.runs[0].Trigger != TriggerSchedule {
		t.Fatalf("runs = %+v; want one recorded schedule run with 80 bytes", st.runs)
	}
}

func TestCleanSkipsUnavailableHostCategories(t *testing.T) {
	cases := map[error]string{
		ErrHostUnsupported:    "unsupported",
		ErrHostHelperOutdated: "helper outdated",
		ErrHostAgentOutdated:  "agent outdated",
	}
	for err, message := range cases {
		host := &fakeHost{err: err}
		st := &fakeStore{}
		svc, serverID := testHostService(t, host, st)

		result := svc.Clean(context.Background(), uuid.New(), TriggerManual, serverID.String(),
			[]string{"host:journal"}, 0)
		if result.Status != StatusOK || len(result.Items) != 1 {
			t.Fatalf("%v: clean = %+v", err, result)
		}
		if item := result.Items[0]; item.Status != StatusSkipped || item.Message != message {
			t.Fatalf("%v: item = %+v; want skipped %q", err, item, message)
		}
		if len(st.runs) != 1 || len(st.items[0]) != 1 {
			t.Fatalf("%v: the skipped attempt must be recorded", err)
		}
	}
}

func TestHostFeatureAbsentWhenNoCleaner(t *testing.T) {
	svc, serverID := testService(t, &fakeAgent{}, nil, &fakeStore{})

	scans := svc.Scan(context.Background(), []string{serverID.String()}, 0)
	if len(scans) != 1 || len(scans[0].Items) != 0 || len(scans[0].Notices) != 0 {
		t.Fatalf("scan = %+v; nil host cleaner must offer nothing", scans)
	}
	result := svc.Clean(context.Background(), uuid.New(), TriggerManual, serverID.String(),
		[]string{"host:journal"}, 0)
	if len(result.Items) != 1 || result.Items[0].Status != StatusSkipped ||
		result.Items[0].Message != ReasonUnsupported {
		t.Fatalf("clean = %+v; want a skipped unsupported item", result)
	}
}

func TestScanReportsOfflineAsSkipped(t *testing.T) {
	agent := &fakeAgent{scanErr: status.Error(codes.Unavailable, "agent down")}
	svc, serverID := testService(t, agent, nil, &fakeStore{})

	scans := svc.Scan(context.Background(), []string{serverID.String()}, 0)
	if len(scans) != 1 || scans[0].Status != StatusSkipped || scans[0].Reason != ReasonOffline {
		t.Fatalf("scan = %+v; want skipped offline", scans)
	}
}

func TestScanReportsUnsupportedAsSkipped(t *testing.T) {
	agent := &fakeAgent{scanErr: status.Error(codes.Unimplemented, "unknown method")}
	svc, serverID := testService(t, agent, nil, &fakeStore{})

	scans := svc.Scan(context.Background(), []string{serverID.String()}, 0)
	if len(scans) != 1 || scans[0].Status != StatusSkipped || scans[0].Reason != ReasonUnsupported {
		t.Fatalf("scan = %+v; want skipped unsupported", scans)
	}
}

func TestScanReportsBusyAsSkipped(t *testing.T) {
	agent := &fakeAgent{}
	svc, serverID := testService(t, agent, nil, &fakeStore{active: 1})

	scans := svc.Scan(context.Background(), []string{serverID.String()}, 0)
	if len(scans) != 1 || scans[0].Status != StatusSkipped || scans[0].Reason != ReasonBusy {
		t.Fatalf("scan = %+v; want skipped busy", scans)
	}
}

func TestScanAddsOrphanedVolumesAndGroupsItems(t *testing.T) {
	created := time.Now().Add(-48 * time.Hour)
	agent := &fakeAgent{scanResp: &agentv1.CleanupScanResponse{Items: []*agentv1.CleanupItem{{
		Id:          "image:sha256:abc",
		Category:    CategoryDanglingImages,
		Name:        "abc",
		SizeBytes:   10,
		CreatedUnix: created.Unix(),
		Reason:      "dangling",
	}}}}
	volumes := &fakeVolumes{volumes: []containers.OrphanedVolume{
		{Name: "gotham-app-" + uuid.New().String() + "-data", SizeBytes: 42, CreatedAt: &created},
	}}
	svc, serverID := testService(t, agent, volumes, &fakeStore{})
	stored := []string{serverID.String()}

	scans := svc.Scan(context.Background(), stored, 0)
	if len(scans) != 1 || scans[0].Status != StatusOK {
		t.Fatalf("scan = %+v; want ok", scans)
	}
	categories := map[string]int{}
	for _, item := range scans[0].Items {
		categories[item.Category]++
	}
	if categories[CategoryDanglingImages] != 1 || categories[CategoryOrphanedVolumes] != 1 {
		t.Fatalf("items = %+v; want one dangling image and one orphaned volume", scans[0].Items)
	}
}

func TestCleanRefusesVolumesForScheduleTrigger(t *testing.T) {
	created := time.Now().Add(-48 * time.Hour)
	volumes := &fakeVolumes{volumes: []containers.OrphanedVolume{
		{Name: "gotham-app-" + uuid.New().String() + "-data", SizeBytes: 7, CreatedAt: &created},
	}}
	st := &fakeStore{}
	svc, serverID := testService(t, &fakeAgent{}, volumes, st)

	result := svc.Clean(context.Background(), uuid.New(), TriggerSchedule, serverID.String(),
		[]string{"volume:" + volumes.volumes[0].Name}, 0)
	if result.Status != StatusOK || len(result.Items) != 1 {
		t.Fatalf("clean = %+v; want one item", result)
	}
	if result.Items[0].Status != StatusSkipped || result.Items[0].Message != "manual only" {
		t.Fatalf("item = %+v; want skipped manual only", result.Items[0])
	}
	if len(volumes.removed) != 0 {
		t.Fatalf("removed = %v; a scheduled clean must never remove volumes", volumes.removed)
	}
	if len(st.runs) != 1 || len(st.items[0]) != 1 {
		t.Fatalf("history = %d runs / %v items; want the skipped item recorded", len(st.runs), st.items)
	}
}

func TestCleanRemovesOnlySelectedAgentItems(t *testing.T) {
	agent := &fakeAgent{
		scanResp: &agentv1.CleanupScanResponse{Items: []*agentv1.CleanupItem{
			{Id: "image:sha256:a", Category: CategoryUnusedImages, Name: "app:1", SizeBytes: 10},
			{Id: "image:sha256:b", Category: CategoryUnusedImages, Name: "app:2", SizeBytes: 20},
		}},
		cleanResp: &agentv1.CleanupCleanResponse{Results: []*agentv1.CleanupItemResult{{
			Id: "image:sha256:a", Status: "removed", ReclaimedBytes: 10,
		}}},
	}
	st := &fakeStore{}
	svc, serverID := testService(t, agent, nil, st)

	result := svc.Clean(context.Background(), uuid.New(), TriggerManual, serverID.String(),
		[]string{"image:sha256:a", "image:sha256:a"}, 0)
	if len(agent.cleaned) != 1 || agent.cleaned[0] != "image:sha256:a" {
		t.Fatalf("agent ids = %v; want the selected id once (deduplicated)", agent.cleaned)
	}
	if len(result.Items) != 1 {
		t.Fatalf("items = %+v; want one deduplicated result", result.Items)
	}
	if result.Items[0].Category != CategoryUnusedImages || result.Items[0].Name != "app:1" {
		t.Fatalf("item = %+v; want the scanned name and category", result.Items[0])
	}
	if len(st.runs) != 1 || st.runs[0].Trigger != TriggerManual || st.runs[0].ReclaimedBytes != 10 {
		t.Fatalf("runs = %+v; want one manual run with 10 reclaimed bytes", st.runs)
	}
}

func TestCleanReportsBusyAsSkipped(t *testing.T) {
	svc, serverID := testService(t, &fakeAgent{}, nil, &fakeStore{active: 1})

	result := svc.Clean(context.Background(), uuid.New(), TriggerManual, serverID.String(), []string{"image:x"}, 0)
	if result.Status != StatusSkipped || result.Reason != ReasonBusy {
		t.Fatalf("clean = %+v; want skipped busy", result)
	}
}

func TestCleanOmitsRunIDWhenRecordFails(t *testing.T) {
	svc, serverID := testHostService(t, &fakeHost{reclaimed: 10}, &fakeStore{recordE: fmt.Errorf("record failed")})

	result := svc.Clean(context.Background(), uuid.New(), TriggerManual, serverID.String(),
		[]string{"host:journal"}, 0)
	if result.Status != StatusOK || len(result.Items) != 1 || result.Items[0].Status != statusRemoved {
		t.Fatalf("clean = %+v; want the host item removed", result)
	}
	if result.RunID != "" {
		t.Fatalf("run_id = %q; want empty when the run was not recorded", result.RunID)
	}
}

func TestCleanStoresRunAndItems(t *testing.T) {
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
		ownerID, fmt.Sprintf("cleanup-%s@example.com", ownerID)); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	if _, err := conn.Exec(ctx,
		`INSERT INTO servers(id,name,ip,ssh_user,status) VALUES($1,'node','127.0.0.1','root','ready')`,
		serverID); err != nil {
		t.Fatalf("insert server: %v", err)
	}

	st := store.New(pool)
	agent := &fakeAgent{
		scanResp: &agentv1.CleanupScanResponse{Items: []*agentv1.CleanupItem{{
			Id: "image:sha256:x", Category: CategoryUnusedImages, Name: "app:1", SizeBytes: 99,
		}}},
		cleanResp: &agentv1.CleanupCleanResponse{Results: []*agentv1.CleanupItemResult{{
			Id: "image:sha256:x", Status: "removed", ReclaimedBytes: 99,
		}}},
	}
	svc := NewService(Config{
		Registry: &fakeRegistry{server: &servers.Server{ID: serverID, Name: "node", Status: servers.StatusReady}},
		Dial: func(context.Context, *servers.Server) (DockerClient, error) {
			return agent, nil
		},
		Store: st,
	})

	result := svc.Clean(ctx, ownerID, TriggerManual, serverID.String(), []string{"image:sha256:x"}, 0)
	if result.Status != StatusOK || len(result.Items) != 1 || result.Items[0].Status != "removed" {
		t.Fatalf("clean = %+v; want one removed item", result)
	}
	if result.RunID == "" {
		t.Fatal("run_id is empty; want the recorded run id")
	}

	var runID pgtype.UUID
	var runTrigger string
	var actorID pgtype.UUID
	var reclaimed int64
	if err := conn.QueryRow(ctx,
		`SELECT id, trigger, actor_user_id, reclaimed_bytes FROM cleanup_runs WHERE server_id = $1`,
		serverID).Scan(&runID, &runTrigger, &actorID, &reclaimed); err != nil {
		t.Fatalf("read run: %v", err)
	}
	if runTrigger != TriggerManual || !actorID.Valid || reclaimed != 99 {
		t.Fatalf("run = trigger %q actor %v reclaimed %d; want manual/owner/99", runTrigger, actorID, reclaimed)
	}
	var itemID, category, name, itemStatus string
	var itemReclaimed int64
	if err := conn.QueryRow(ctx,
		`SELECT item_id, category, name, status, reclaimed_bytes FROM cleanup_run_items WHERE run_id = $1`,
		runID).Scan(&itemID, &category, &name, &itemStatus, &itemReclaimed); err != nil {
		t.Fatalf("read item: %v", err)
	}
	if itemID != "image:sha256:x" || category != CategoryUnusedImages || name != "app:1" ||
		itemStatus != "removed" || itemReclaimed != 99 {
		t.Fatalf("item = %s/%s/%s/%s/%d; want the removed image with its scan name",
			itemID, category, name, itemStatus, itemReclaimed)
	}

	// A deployment in a non-terminal state makes the node busy.
	projectID, envID, appID := uuid.New(), uuid.New(), uuid.New()
	if _, err := conn.Exec(ctx, `INSERT INTO projects(id,name) VALUES($1,'project')`, projectID); err != nil {
		t.Fatalf("insert project: %v", err)
	}
	if _, err := conn.Exec(ctx,
		`INSERT INTO environments(id,project_id,name) VALUES($1,$2,'production')`, envID, projectID); err != nil {
		t.Fatalf("insert environment: %v", err)
	}
	if _, err := conn.Exec(ctx,
		`INSERT INTO applications(id,user_id,server_id,environment_id,name) VALUES($1,$2,$3,$4,'app')`,
		appID, ownerID, serverID, envID); err != nil {
		t.Fatalf("insert application: %v", err)
	}
	if _, err := conn.Exec(ctx,
		`INSERT INTO deployments(application_id,state) VALUES($1,'building')`, appID); err != nil {
		t.Fatalf("insert deployment: %v", err)
	}
	scans := svc.Scan(ctx, []string{serverID.String()}, 0)
	if len(scans) != 1 || scans[0].Status != StatusSkipped || scans[0].Reason != ReasonBusy {
		t.Fatalf("scan = %+v; want skipped busy after an in-flight deployment", scans)
	}
}

func cleanupScratch(t *testing.T) (context.Context, string) {
	t.Helper()
	base := os.Getenv("GOTHAM_TEST_DSN")
	if base == "" {
		t.Skip("GOTHAM_TEST_DSN is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	t.Cleanup(cancel)

	admin, err := pgx.Connect(ctx, base)
	if err != nil {
		t.Fatalf("GOTHAM_TEST_DSN is set but Postgres is unavailable: %v", err)
	}
	name := fmt.Sprintf("cleanup%d", time.Now().UnixNano())
	if _, err := admin.Exec(ctx, `CREATE DATABASE "`+name+`"`); err != nil {
		_ = admin.Close(ctx)
		t.Fatalf("create scratch database: %v", err)
	}
	_ = admin.Close(ctx)
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		conn, err := pgx.Connect(cleanupCtx, base)
		if err != nil {
			t.Logf("reconnect for scratch drop: %v", err)
			return
		}
		defer conn.Close(cleanupCtx)
		if _, err := conn.Exec(cleanupCtx, `DROP DATABASE IF EXISTS "`+name+`" WITH (FORCE)`); err != nil {
			t.Logf("drop scratch database: %v", err)
		}
	})

	parsed, err := url.Parse(base)
	if err != nil {
		t.Fatalf("parse test DSN: %v", err)
	}
	parsed.Path = "/" + name
	dsn := parsed.String()

	sqldb, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(func() { _ = sqldb.Close() })
	provider, err := goose.NewProvider(goose.DialectPostgres, sqldb, store.Migrations())
	if err != nil {
		t.Fatalf("init migrations: %v", err)
	}
	if _, err := provider.Up(ctx); err != nil {
		t.Fatalf("migrate scratch database: %v", err)
	}
	return ctx, dsn
}
