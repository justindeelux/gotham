package monitoring

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/incidents"
	"github.com/justindeelux/gotham/internal/servers"
	"github.com/justindeelux/gotham/internal/store/sqlc"
)

type recordingReporter struct {
	reports []incidents.Observation
	closed  []string
}

func (r *recordingReporter) Report(_ context.Context, obs incidents.Observation) (*incidents.Incident, incidents.Transition, error) {
	r.reports = append(r.reports, obs)
	return nil, incidents.Transition{}, nil
}

func (r *recordingReporter) Close(_ context.Context, identity, _ string) error {
	r.closed = append(r.closed, identity)
	return nil
}

func (r *recordingReporter) ActiveNodeIncidents(context.Context) ([]incidents.Incident, error) {
	return nil, nil
}

func hostObservation(at time.Time, cpu servers.ObservedMetric, mem servers.ObservedMetric) servers.NodeObservation {
	return servers.NodeObservation{ObservedAt: at, Host: servers.HostObservation{CPUUsage: cpu, MemUsage: mem}}
}

func TestHostDefaultsCarrySustainedWindowsAndUnknownInterrupts(t *testing.T) {
	base := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	reporter := &recordingReporter{}
	svc := &Service{incidents: reporter}
	nodeID := uuid.New()
	rules := Defaults()

	svc.evaluateHostMetric(context.Background(), nodeID, "node-a", hostObservation(base, servers.ObservedMetric{Status: servers.MetricStatusOK, Value: 0.92}, servers.ObservedMetric{}), DetectorCPU, rules.CPU, servers.ObservedMetric{Status: servers.MetricStatusOK, Value: 0.92})
	if len(reporter.reports) != 1 {
		t.Fatalf("reports = %d, want 1", len(reporter.reports))
	}
	got := reporter.reports[0]
	if !got.Valid || got.Severity != incidents.SeverityWarning {
		t.Fatalf("report = %+v; want warning", got)
	}
	if got.ConfirmFor != 5*time.Minute || got.RecoverFor != time.Minute {
		t.Fatalf("windows = %s/%s; want 5m/1m", got.ConfirmFor, got.RecoverFor)
	}
	if got.Value == nil || *got.Value != 92 || *got.TriggerThreshold != 90 || *got.ClearThreshold != 85 {
		t.Fatalf("thresholds = %+v; want 92 against 90/85", got)
	}

	reporter.reports = nil
	svc.evaluateHostMetric(context.Background(), nodeID, "node-a", hostObservation(base, servers.ObservedMetric{}, servers.ObservedMetric{}), DetectorCPU, rules.CPU, servers.ObservedMetric{Status: servers.MetricStatusUnsupported, Reason: "no_counter"})
	if len(reporter.reports) != 1 || reporter.reports[0].Valid {
		t.Fatalf("unsupported counter = %+v; want one unknown report", reporter.reports)
	}
	if reporter.reports[0].Evidence != "counter unsupported (no_counter)" {
		t.Fatalf("unknown evidence = %q", reporter.reports[0].Evidence)
	}
}

func TestDisabledRulesCloseWithoutRecovery(t *testing.T) {
	reporter := &recordingReporter{}
	svc := &Service{incidents: reporter}
	rule := Defaults().CPU
	rule.Enabled = false
	svc.evaluateHostMetric(context.Background(), uuid.New(), "node-a", servers.NodeObservation{}, DetectorCPU, rule, servers.ObservedMetric{})
	if len(reporter.reports) != 0 || len(reporter.closed) != 1 {
		t.Fatalf("reports=%d closed=%d; want a silent close", len(reporter.reports), len(reporter.closed))
	}
}

func TestFilesystemBandsAndAliasDeduplication(t *testing.T) {
	if severity, _ := filesystemSeverity(95, Defaults().Space); severity != incidents.SeverityCritical {
		t.Fatalf("95 = %s, want critical", severity)
	}
	if severity, _ := filesystemSeverity(94, Defaults().Space); severity != "" {
		t.Fatalf("94 = %s, want unchanged severity", severity)
	}
	if severity, _ := filesystemSeverity(90, Defaults().Space); severity != incidents.SeverityWarning {
		t.Fatalf("90 = %s, want warning (downgrade band)", severity)
	}

	root := servers.FilesystemObservation{
		Path: "/", Purpose: "root", Status: servers.MetricStatusOK,
		TotalBytes: 100, AvailableBytes: 50, InodeStatus: servers.MetricStatusOK,
		TotalInodes: 100, FreeInodes: 50,
	}
	backup := servers.FilesystemObservation{
		Path: "/mnt/backup", Purpose: "backup", Status: servers.MetricStatusOK,
		TotalBytes: 200, AvailableBytes: 5, InodeStatus: servers.MetricStatusOK,
		TotalInodes: 100, FreeInodes: 5,
	}
	alias := backup
	alias.Path = "/mnt/backup/data"
	alias.Purpose = "docker_data"

	merged := mergeAliases([]servers.FilesystemObservation{root, backup, alias})
	if len(merged) != 2 {
		t.Fatalf("merged = %+v; want root plus one backup filesystem", merged)
	}
	if merged[1].Path != "/mnt/backup" || merged[1].Purpose != "backup+docker_data" {
		t.Fatalf("alias merge = %+v", merged[1])
	}
}

func TestUnsupportedFilesystemCountersReportUnknown(t *testing.T) {
	reporter := &recordingReporter{}
	svc := &Service{incidents: reporter}
	nodeID := uuid.New()
	identity := sqlc.FilesystemIdentity{
		ID: pgUUID(uuid.New()), FilesystemKey: "/", Path: "/", Purpose: "root", Segment: 1,
	}
	fs := servers.FilesystemObservation{
		Path: "/", Purpose: "root", Status: servers.MetricStatusError, Reason: "read_failed",
		InodeStatus: servers.MetricStatusUnsupported, InodeReason: "unsupported",
	}
	svc.evaluateFilesystemCounter(context.Background(), nodeID, "node-a", fs, identity, Defaults().Space, DetectorSpace, time.Now())
	svc.evaluateFilesystemCounter(context.Background(), nodeID, "node-a", fs, identity, Defaults().Inode, DetectorInodes, time.Now())
	if len(reporter.reports) != 2 {
		t.Fatalf("reports = %d, want 2 unknown reports", len(reporter.reports))
	}
	for _, report := range reporter.reports {
		if report.Valid || report.Value != nil {
			t.Fatalf("report = %+v; counter failures must stay unknown, not zero", report)
		}
	}
}

func TestRulesValidationRejectsBadOrderingAndRanges(t *testing.T) {
	valid := Defaults()
	if err := valid.Validate(); err != nil {
		t.Fatalf("defaults invalid: %v", err)
	}
	bad := valid
	bad.CPU.Warning = bad.CPU.Clear
	if err := bad.Validate(); err == nil {
		t.Fatal("equal warning and clear accepted")
	}
	bad = valid
	bad.Space.Critical = bad.Space.Warning
	if err := bad.Validate(); err == nil {
		t.Fatal("critical not above warning accepted")
	}
	bad = valid
	bad.Space.Clear = 101
	if err := bad.Validate(); err == nil {
		t.Fatal("percentage above 100 accepted")
	}
	bad = valid
	bad.Inode.ConfirmSeconds = 1
	if err := bad.Validate(); err == nil {
		t.Fatal("too-short duration accepted")
	}
}
