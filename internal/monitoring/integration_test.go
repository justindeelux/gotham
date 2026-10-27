package monitoring

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/justindeelux/gotham/internal/incidents"
	"github.com/justindeelux/gotham/internal/servers"
	"github.com/justindeelux/gotham/internal/store"
	"github.com/justindeelux/gotham/internal/store/sqlc"
)

const defaultTestDSN = "postgres://gotham:gotham@localhost:5432/gotham?sslmode=disable"

type countingNotifier struct {
	deliveries []incidents.Delivery
}

func (n *countingNotifier) NotifyIncident(_ context.Context, delivery incidents.Delivery) error {
	n.deliveries = append(n.deliveries, delivery)
	return nil
}

func scratchStore(t *testing.T) *store.Store {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	dsn := os.Getenv("GOTHAM_TEST_DSN")
	if dsn == "" {
		dsn = defaultTestDSN
	}
	if err := store.ProbeOnce(ctx, dsn); err != nil {
		if os.Getenv("GOTHAM_TEST_DSN") != "" {
			t.Fatalf("GOTHAM_TEST_DSN is set but Postgres is unavailable: %v", err)
		}
		t.Skipf("Postgres not available: %v", err)
	}
	parsed, err := url.Parse(dsn)
	if err != nil {
		t.Fatalf("parse DSN: %v", err)
	}
	name := fmt.Sprintf("mon%d%d", time.Now().UnixNano(), os.Getpid())
	admin, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("admin connect: %v", err)
	}
	if _, err := admin.Exec(ctx, `CREATE DATABASE "`+name+`"`); err != nil {
		_ = admin.Close(ctx)
		t.Fatalf("create scratch database: %v", err)
	}
	_ = admin.Close(ctx)
	parsed.Path = "/" + name
	scratchDSN := parsed.String()
	if err := store.Migrate(ctx, scratchDSN, store.MigrateUp); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	pool, err := store.Open(ctx, scratchDSN)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() {
		pool.Close()
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		admin, err := pgx.Connect(cleanupCtx, dsn)
		if err != nil {
			t.Logf("reconnect for drop: %v", err)
			return
		}
		defer admin.Close(cleanupCtx)
		if _, err := admin.Exec(cleanupCtx, `DROP DATABASE IF EXISTS "`+name+`"`); err != nil {
			t.Logf("drop scratch database: %v", err)
		}
	})
	return store.New(pool)
}

func createNode(t *testing.T, st *store.Store) uuid.UUID {
	t.Helper()
	row, err := st.CreateServer(context.Background(), sqlc.CreateServerParams{
		Name: "node-a", Ip: "10.0.0.10", Port: 22, SshUser: "root",
		SshKeyID: pgtype.UUID{},
	})
	if err != nil {
		t.Fatalf("create server: %v", err)
	}
	return uuid.UUID(row.ID.Bytes)
}

func TestFullDataMountOpensOneIncidentWithHealthyRoot(t *testing.T) {
	st := scratchStore(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	nodeID := createNode(t, st)
	base := time.Date(2026, 4, 1, 12, 0, 0, 0, time.UTC)
	now := func() time.Time { return base }
	incidentSvc := incidents.NewService(incidents.Config{Store: st, Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), Now: now})
	svc := NewService(Config{Store: st, Incidents: incidentSvc, Now: now})

	root := servers.FilesystemObservation{
		Path: "/", Purpose: "root", Status: servers.MetricStatusOK,
		TotalBytes: 1000, AvailableBytes: 500, InodeStatus: servers.MetricStatusOK,
		TotalInodes: 1000, FreeInodes: 800,
	}
	backup := servers.FilesystemObservation{
		Path: "/mnt/backup", Purpose: "backup", Status: servers.MetricStatusOK,
		TotalBytes: 2000, AvailableBytes: 80, InodeStatus: servers.MetricStatusOK,
		TotalInodes: 1000, FreeInodes: 800,
	}
	alias := backup
	alias.Path = "/mnt/backup/data"
	alias.Purpose = "docker_data"

	for _, offset := range []time.Duration{0, 30 * time.Second, 60 * time.Second} {
		at := base.Add(offset)
		svc.RecordHostPressure(ctx, nodeID, "node-a", servers.NodeObservation{
			ServerID:   nodeID,
			ObservedAt: at,
			Host: servers.HostObservation{
				CPUUsage: servers.ObservedMetric{Status: servers.MetricStatusOK, Value: 0.4},
				MemUsage: servers.ObservedMetric{Status: servers.MetricStatusOK, Value: 0.4},
			},
			Filesystems: []servers.FilesystemObservation{root, backup, alias},
		})
	}

	active, err := incidentSvc.ActiveNodeIncidents(ctx)
	if err != nil {
		t.Fatalf("active incidents: %v", err)
	}
	space := 0
	for _, inc := range active {
		if inc.Detector != DetectorSpace {
			continue
		}
		space++
		if inc.ResourceName != "/mnt/backup" {
			t.Fatalf("incident resource = %q; want the full mount, not the healthy root", inc.ResourceName)
		}
	}
	if space != 1 {
		t.Fatalf("space incidents = %d (%+v); want exactly one for the aliased backup mount", space, active)
	}
}

func TestResizeKeepsTheSameIncidentAndRecoversAfterClear(t *testing.T) {
	st := scratchStore(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	nodeID := createNode(t, st)
	base := time.Date(2026, 4, 2, 12, 0, 0, 0, time.UTC)
	now := func() time.Time { return base }
	notifier := &countingNotifier{}
	incidentSvc := incidents.NewService(incidents.Config{Store: st, Notifier: notifier, Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), Now: now})
	svc := NewService(Config{Store: st, Incidents: incidentSvc, Now: now})
	fs := servers.FilesystemObservation{
		Path: "/mnt/backup", Purpose: "backup", Status: servers.MetricStatusOK,
		TotalBytes: 1000, AvailableBytes: 80, InodeStatus: servers.MetricStatusOK,
		TotalInodes: 1000, FreeInodes: 800,
	}
	observe := func(at time.Time, observation servers.FilesystemObservation) {
		t.Helper()
		svc.RecordHostPressure(ctx, nodeID, "node-a", servers.NodeObservation{
			ServerID: nodeID, ObservedAt: at,
			Host: servers.HostObservation{
				CPUUsage: servers.ObservedMetric{Status: servers.MetricStatusOK, Value: 0.4},
				MemUsage: servers.ObservedMetric{Status: servers.MetricStatusOK, Value: 0.4},
			},
			Filesystems: []servers.FilesystemObservation{observation},
		})
	}
	for offset := 0; offset <= 60; offset += 15 {
		observe(base.Add(time.Duration(offset)*time.Second), fs)
	}
	before, err := incidentSvc.ActiveNodeIncidents(ctx)
	if err != nil {
		t.Fatalf("active after warning: %v", err)
	}
	var incidentID uuid.UUID
	for _, inc := range before {
		if inc.Detector == DetectorSpace {
			incidentID = inc.ID
		}
	}
	if incidentID == uuid.Nil {
		t.Fatalf("no space incident after sustained warning: %+v", before)
	}

	fs.TotalBytes = 2000
	fs.AvailableBytes = 100
	for offset := 75; offset <= 135; offset += 15 {
		observe(base.Add(time.Duration(offset)*time.Second), fs)
	}
	afterResize, err := incidentSvc.ActiveNodeIncidents(ctx)
	if err != nil {
		t.Fatalf("active after resize: %v", err)
	}
	var resizedIncident *incidents.Incident
	for i, inc := range afterResize {
		if inc.Detector == DetectorSpace {
			resizedIncident = &afterResize[i]
		}
	}
	if resizedIncident == nil || resizedIncident.ID != incidentID {
		t.Fatalf("resize replaced the incident: old=%s active=%+v", incidentID, afterResize)
	}
	opened := 0
	for _, delivery := range notifier.deliveries {
		if delivery.Transition == incidents.TransitionOpened {
			opened++
		}
	}
	if opened != 1 {
		t.Fatalf("opened deliveries = %d (%+v); resize must not re-open", opened, notifier.deliveries)
	}

	fs.TotalBytes = 4000
	fs.AvailableBytes = 1000
	for offset := 150; offset <= 210; offset += 15 {
		observe(base.Add(time.Duration(offset)*time.Second), fs)
	}
	recovered := 0
	for _, delivery := range notifier.deliveries {
		if delivery.Transition == incidents.TransitionRecovered {
			recovered++
		}
	}
	if recovered != 1 {
		t.Fatalf("recovered deliveries = %d (%+v); want one stable recovery", recovered, notifier.deliveries)
	}
	remaining, err := incidentSvc.ActiveNodeIncidents(ctx)
	if err != nil {
		t.Fatalf("active after recovery: %v", err)
	}
	for _, inc := range remaining {
		if inc.Detector == DetectorSpace {
			t.Fatalf("space incident still active after stable clear: %+v", inc)
		}
	}
}

func TestSettingsPersistAcrossRestartAndReset(t *testing.T) {
	st := scratchStore(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	nodeID := createNode(t, st)
	svc := NewService(Config{Store: st, Incidents: &recordingReporter{}})

	invalid := Defaults()
	invalid.CPU.Clear = invalid.CPU.Warning
	if _, err := svc.SaveSettings(ctx, uuid.Nil, nil, invalid); !errors.Is(err, ErrValidation) {
		t.Fatalf("invalid rules error = %v; want validation", err)
	}
	missing := uuid.New()
	if _, err := svc.SaveSettings(ctx, uuid.Nil, &missing, Defaults()); !errors.Is(err, ErrValidation) {
		t.Fatalf("missing node error = %v; want validation", err)
	}

	rules := Defaults()
	rules.CPU.Warning = 80
	rules.CPU.Clear = 70
	if _, err := svc.SaveSettings(ctx, uuid.Nil, nil, rules); err != nil {
		t.Fatalf("save instance: %v", err)
	}
	restarted := NewService(Config{Store: st, Incidents: &recordingReporter{}})
	got, err := restarted.GetSettings(ctx, nil)
	if err != nil {
		t.Fatalf("get instance: %v", err)
	}
	if got.Source != "instance" || got.Rules.CPU.Warning != 80 || got.Rules.CPU.Clear != 70 {
		t.Fatalf("instance settings = %+v; want persisted 80/70", got)
	}

	nodeRules := rules
	nodeRules.Memory.Warning = 75
	nodeRules.Memory.Clear = 65
	if _, err := restarted.SaveSettings(ctx, uuid.Nil, &nodeID, nodeRules); err != nil {
		t.Fatalf("save node: %v", err)
	}
	got, err = restarted.GetSettings(ctx, &nodeID)
	if err != nil {
		t.Fatalf("get node: %v", err)
	}
	if got.Source != "node" || !got.HasOverride || got.Effective.Memory.Warning != 75 {
		t.Fatalf("node settings = %+v; want node override", got)
	}

	if err := restarted.ResetSettings(ctx, &nodeID); err != nil {
		t.Fatalf("reset node: %v", err)
	}
	got, _ = restarted.GetSettings(ctx, &nodeID)
	if got.Source != "instance" || got.HasOverride || got.Rules.CPU.Warning != 80 {
		t.Fatalf("node after reset = %+v; want instance inheritance", got)
	}
	if err := restarted.ResetSettings(ctx, nil); err != nil {
		t.Fatalf("reset instance: %v", err)
	}
	got, _ = restarted.GetSettings(ctx, nil)
	if got.Source != "default" || got.Rules.CPU.Warning != 90 {
		t.Fatalf("instance after reset = %+v; want built-in defaults", got)
	}
}

func TestFilesystemHistoryKeepsCapacityBoundariesAndQuality(t *testing.T) {
	st := scratchStore(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	nodeID := createNode(t, st)
	reporter := &recordingReporter{}
	base := time.Now().UTC().Add(-3 * time.Hour).Truncate(time.Hour)
	svc := NewService(Config{Store: st, Incidents: reporter, Now: func() time.Time { return base.Add(10 * time.Minute) }})

	fs := servers.FilesystemObservation{
		Path: "/mnt/backup", Purpose: "backup", Status: servers.MetricStatusOK,
		TotalBytes: 1000, AvailableBytes: 50, InodeStatus: servers.MetricStatusOK,
		TotalInodes: 100, FreeInodes: 5,
	}
	first, err := svc.observeFilesystem(ctx, nodeID, fs, base)
	if err != nil {
		t.Fatalf("observe first: %v", err)
	}
	if first.Segment != 1 {
		t.Fatalf("segment = %d, want 1", first.Segment)
	}
	if _, err := svc.observeFilesystem(ctx, nodeID, fs, base.Add(15*time.Second)); err != nil {
		t.Fatalf("observe second: %v", err)
	}

	fs.TotalBytes = 2000
	fs.AvailableBytes = 100
	resized, err := svc.observeFilesystem(ctx, nodeID, fs, base.Add(time.Minute))
	if err != nil {
		t.Fatalf("observe resize: %v", err)
	}
	if resized.Segment != 2 || resized.TotalBytes != 2000 {
		t.Fatalf("resize = %+v; want segment 2 with new capacity", resized)
	}
	if resized.ID != first.ID {
		t.Fatalf("resize changed filesystem identity: %v -> %v", first.ID, resized.ID)
	}
	if len(reporter.closed) != 0 {
		t.Fatalf("resize closed an incident: %v", reporter.closed)
	}

	var samples, fresh int
	var minBytes, maxBytes *int64
	err = st.DB.QueryRow(ctx, `SELECT samples, fresh_samples, min_used_bytes, max_used_bytes
		FROM filesystem_usage_buckets WHERE identity_id = $1 AND segment = $2`, first.ID, int32(1)).
		Scan(&samples, &fresh, &minBytes, &maxBytes)
	if err != nil {
		t.Fatalf("read bucket: %v", err)
	}
	if samples != 2 || fresh != 2 || minBytes == nil || maxBytes == nil || *minBytes != 950 || *maxBytes != 950 {
		rows, _ := st.DB.Query(ctx, `SELECT segment, bucket_at, samples FROM filesystem_usage_buckets WHERE identity_id = $1`, first.ID)
		var detail string
		for rows.Next() {
			var seg int32
			var at time.Time
			var n int32
			_ = rows.Scan(&seg, &at, &n)
			detail += fmt.Sprintf(" [%d %s %d]", seg, at, n)
		}
		rows.Close()
		t.Fatalf("bucket = samples %d fresh %d min %v max %v; want two 950 samples; rows:%s", samples, fresh, minBytes, maxBytes, detail)
	}

	unknown := fs
	unknown.Status = servers.MetricStatusError
	unknown.InodeStatus = servers.MetricStatusError
	if _, err := svc.observeFilesystem(ctx, nodeID, unknown, base.Add(2*time.Minute)); err != nil {
		t.Fatalf("observe unknown: %v", err)
	}
	var unknownSamples int
	var unknownMin *int64
	err = st.DB.QueryRow(ctx, `SELECT unknown_samples, min_used_bytes FROM filesystem_usage_buckets
		WHERE identity_id = $1 AND segment = $2`, resized.ID, int32(2)).
		Scan(&unknownSamples, &unknownMin)
	if err != nil {
		t.Fatalf("read unknown bucket: %v", err)
	}
	if unknownSamples != 1 || unknownMin == nil || *unknownMin != 1900 {
		t.Fatalf("unknown bucket = %d/%v; want one unknown sample keeping the known 1900 history", unknownSamples, unknownMin)
	}

	old := fs
	old.Status = servers.MetricStatusOK
	if _, err := svc.observeFilesystem(ctx, nodeID, old, base.Add(-31*24*time.Hour)); err != nil {
		t.Fatalf("observe old: %v", err)
	}
	if err := svc.Sweep(ctx); err != nil {
		t.Fatalf("sweep: %v", err)
	}
	var remaining int
	if err := st.DB.QueryRow(ctx, `SELECT count(*) FROM filesystem_usage_buckets
		WHERE identity_id = $1 AND bucket_at < $2`, resized.ID, base.Add(-30*24*time.Hour)).
		Scan(&remaining); err != nil {
		t.Fatalf("count old buckets: %v", err)
	}
	if remaining != 0 {
		t.Fatalf("retention left %d old buckets", remaining)
	}
}
