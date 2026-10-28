package domainwatch

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/incidents"
)

type recordingReporter struct {
	observations []incidents.Observation
	closed       []string
}

func (r *recordingReporter) Report(_ context.Context, obs incidents.Observation) (*incidents.Incident, incidents.Transition, error) {
	r.observations = append(r.observations, obs)
	return nil, incidents.Transition{}, nil
}

func (r *recordingReporter) Close(_ context.Context, identity, _ string) error {
	r.closed = append(r.closed, identity)
	return nil
}

func testService(reporter *recordingReporter) *Service {
	return &Service{
		incidents: reporter,
		logger:    slog.New(slog.NewTextHandler(io.Discard, nil)),
		now:       time.Now,
		grace:     StartupGrace,
	}
}

func serviceTarget() Target {
	return Target{
		DomainID:        uuid.MustParse("11111111-1111-1111-1111-111111111111"),
		ApplicationID:   uuid.MustParse("22222222-2222-2222-2222-222222222222"),
		ProjectID:       uuid.MustParse("33333333-3333-3333-3333-333333333333"),
		ApplicationName: "shop",
		Domain:          "shop.example.com",
		Protocol:        "https",
		Settings:        DefaultSettings(),
	}
}

func TestReportOpensOnlyAfterThreeConsecutiveFailures(t *testing.T) {
	reporter := &recordingReporter{}
	svc := testService(reporter)
	target := serviceTarget()
	at := time.Now().UTC()

	svc.report(context.Background(), target, Result{Outcome: OutcomeFailed, Reason: ReasonTimeout, At: at}, 2, 0)
	if len(reporter.observations) != 0 {
		t.Fatalf("reported before the confirmation window: %d", len(reporter.observations))
	}
	svc.report(context.Background(), target, Result{Outcome: OutcomeFailed, Reason: ReasonTimeout, At: at}, 3, 0)
	if len(reporter.observations) != 1 {
		t.Fatalf("observations = %d", len(reporter.observations))
	}
	obs := reporter.observations[0]
	if !obs.Valid || !obs.Unhealthy || obs.ConfirmFor != 0 {
		t.Fatalf("observation = %+v", obs)
	}
	if obs.Identity != target.incidentIdentity() || obs.ResourceType != ResourceDomain {
		t.Fatalf("identity/resource = %q %q", obs.Identity, obs.ResourceType)
	}
	if obs.ResourceName != "shop / shop.example.com" {
		t.Fatalf("resource name = %q", obs.ResourceName)
	}
	if obs.ConfirmFor != 0 || obs.RecoverFor != 0 {
		t.Fatalf("confirmation windows = %v/%v", obs.ConfirmFor, obs.RecoverFor)
	}
}

func TestReportRecoversOnlyAfterTwoSuccesses(t *testing.T) {
	reporter := &recordingReporter{}
	svc := testService(reporter)
	target := serviceTarget()
	at := time.Now().UTC()

	svc.report(context.Background(), target, Result{Outcome: OutcomeHealthy, At: at}, 0, 1)
	if len(reporter.observations) != 0 {
		t.Fatalf("reported recovery too early")
	}
	svc.report(context.Background(), target, Result{Outcome: OutcomeHealthy, At: at}, 0, 2)
	if len(reporter.observations) != 1 {
		t.Fatalf("observations = %d", len(reporter.observations))
	}
	obs := reporter.observations[0]
	if !obs.Valid || obs.Unhealthy {
		t.Fatalf("observation = %+v", obs)
	}
}

func TestReportUnsupportedIsUnknownEvidence(t *testing.T) {
	reporter := &recordingReporter{}
	svc := testService(reporter)
	svc.report(context.Background(), serviceTarget(), Result{Outcome: OutcomeUnsupported, Reason: ReasonBlockedDestination}, 0, 0)
	if len(reporter.observations) != 1 {
		t.Fatalf("observations = %d", len(reporter.observations))
	}
	if reporter.observations[0].Valid {
		t.Fatal("unsupported coverage must not be valid evidence")
	}
}

func TestGraceCoversStartupAndConfigChange(t *testing.T) {
	now := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	svc := &Service{startedAt: now.Add(-time.Minute), grace: ConfigGrace}
	target := serviceTarget()
	target.CreatedAt = now.Add(-10 * time.Minute)
	target.DomainUpdatedAt = now.Add(-10 * time.Minute)
	target.SettingsUpdated = now.Add(-30 * time.Second)
	if got := svc.graceUntil(target); !got.After(now) {
		t.Fatalf("config change grace = %v", got)
	}
	target.SettingsUpdated = now.Add(-10 * time.Minute)
	if got := svc.graceUntil(target); !got.Equal(now.Add(time.Minute)) {
		t.Fatalf("startup grace = %v", got)
	}
}

func TestDeploymentSuppressionAndGrace(t *testing.T) {
	now := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	svc := &Service{startedAt: now.Add(-time.Hour), grace: ConfigGrace}
	target := serviceTarget()
	target.DeploymentState = "building"
	started := now.Add(-time.Minute)
	target.DeploymentStarted = &started
	if !deploymentInProgress(target, now) {
		t.Fatal("active deployment should suppress probes")
	}
	stuck := now.Add(-30 * time.Minute)
	target.DeploymentStarted = &stuck
	if deploymentInProgress(target, now) {
		t.Fatal("stuck deployment must not hide failures forever")
	}

	target.DeploymentState = "running"
	finished := now.Add(-time.Minute)
	target.DeploymentFinished = &finished
	if got := svc.graceUntil(target); !got.Equal(finished.Add(ConfigGrace)) {
		t.Fatalf("deployment grace = %v", got)
	}
}
