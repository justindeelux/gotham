package incidents

import (
	"testing"
	"time"
)

func at(base time.Time, seconds int) time.Time { return base.Add(time.Duration(seconds) * time.Second) }

func unhealthy(ob time.Time) Observation {
	return Observation{
		Identity: "app:1", Detector: "app.health", At: ob, Valid: true, Unhealthy: true,
		Severity: SeverityWarning, ConfirmFor: 60 * time.Second, RecoverFor: 60 * time.Second,
		MaxGap: 30 * time.Second,
	}
}

func healthy(ob time.Time) Observation {
	o := unhealthy(ob)
	o.Unhealthy = false
	return o
}

func TestPendingFiringResolvedEmitsOneOpeningAndOneRecovery(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	obs := unhealthy(at(base, 0))
	inc, tr := advance(nil, obs, at(base, 0))
	if inc == nil || inc.Lifecycle != LifecyclePending || tr.Event != "" {
		t.Fatalf("first report = %+v, %+v; want pending without a notification", inc, tr)
	}

	obs = unhealthy(at(base, 30))
	inc, tr = advance(inc, obs, at(base, 30))
	if inc.Lifecycle != LifecyclePending || tr.Event != "" {
		t.Fatalf("half-window = %s %q; want still pending", inc.Lifecycle, tr.Event)
	}

	obs = unhealthy(at(base, 60))
	inc, tr = advance(inc, obs, at(base, 60))
	if inc.Lifecycle != LifecycleFiring || tr.Event != TransitionOpened {
		t.Fatalf("confirmation = %s %q; want firing opened", inc.Lifecycle, tr.Event)
	}

	obs = unhealthy(at(base, 61))
	inc, tr = advance(inc, obs, at(base, 61))
	if tr.Event != "" {
		t.Fatalf("duplicate unhealthy emitted %q", tr.Event)
	}

	obs = healthy(at(base, 90))
	inc, tr = advance(inc, obs, at(base, 90))
	if inc.Lifecycle != LifecycleFiring {
		t.Fatalf("first healthy = %s; recovery must be stable", inc.Lifecycle)
	}
	obs = healthy(at(base, 120))
	inc, tr = advance(inc, obs, at(base, 120))
	obs = healthy(at(base, 150))
	inc, tr = advance(inc, obs, at(base, 150))
	if inc.Lifecycle != LifecycleResolved || tr.Event != TransitionRecovered {
		t.Fatalf("recovery = %s %q; want resolved recovered", inc.Lifecycle, tr.Event)
	}
	if inc.CloseReason != "recovered" {
		t.Fatalf("close reason = %q", inc.CloseReason)
	}
}

func TestDuplicateReportDoesNotAdvanceConfirmation(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	inc, _ := advance(nil, unhealthy(at(base, 0)), at(base, 0))
	inc, tr := advance(inc, unhealthy(at(base, 0)), at(base, 0))
	if tr.Event != "" || inc.Lifecycle != LifecyclePending {
		t.Fatalf("duplicate advanced state: %s %q", inc.Lifecycle, tr.Event)
	}
	if !inc.FirstObservedAt.Equal(at(base, 0)) {
		t.Fatalf("first observed moved on duplicate: %s", inc.FirstObservedAt)
	}
}

func TestInvalidEvidenceInterruptsConfirmationAndRecovery(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	inc, _ := advance(nil, unhealthy(at(base, 0)), at(base, 0))
	inc, _ = advance(inc, unhealthy(at(base, 30)), at(base, 30))

	unknown := unhealthy(at(base, 45))
	unknown.Valid = false
	inc, tr := advance(inc, unknown, at(base, 45))
	if tr.Event != "" || inc.Lifecycle != LifecyclePending || inc.Coverage != CoverageUnknown {
		t.Fatalf("invalid evidence = %s %s %q", inc.Lifecycle, inc.Coverage, tr.Event)
	}

	inc, tr = advance(inc, unhealthy(at(base, 70)), at(base, 70))
	if tr.Event != "" || inc.Lifecycle != LifecyclePending {
		t.Fatalf("confirmation borrowed invalid time: %s %q", inc.Lifecycle, tr.Event)
	}
	inc, tr = advance(inc, unhealthy(at(base, 130)), at(base, 130))
	if inc.Lifecycle != LifecyclePending || tr.Event != "" {
		t.Fatalf("gap did not restart confirmation: %s %q", inc.Lifecycle, tr.Event)
	}
	inc, tr = advance(inc, unhealthy(at(base, 160)), at(base, 160))
	inc, tr = advance(inc, unhealthy(at(base, 190)), at(base, 190))
	if inc.Lifecycle != LifecycleFiring || tr.Event != TransitionOpened {
		t.Fatalf("fresh confirmation did not fire: %s %q", inc.Lifecycle, tr.Event)
	}

	inc, _ = advance(inc, healthy(at(base, 200)), at(base, 200))
	unknown = unhealthy(at(base, 210))
	unknown.Valid = false
	inc, tr = advance(inc, unknown, at(base, 210))
	if inc.RecoveryStartedAt != nil {
		t.Fatalf("invalid evidence kept recovery timing")
	}
	inc, tr = advance(inc, healthy(at(base, 220)), at(base, 220))
	if inc.Lifecycle != LifecycleFiring || tr.Event != "" {
		t.Fatalf("recovery borrowed invalid time: %s %q", inc.Lifecycle, tr.Event)
	}
	inc, tr = advance(inc, healthy(at(base, 250)), at(base, 250))
	inc, tr = advance(inc, healthy(at(base, 280)), at(base, 280))
	if inc.Lifecycle != LifecycleResolved || tr.Event != TransitionRecovered {
		t.Fatalf("stable recovery failed: %s %q", inc.Lifecycle, tr.Event)
	}
}

func TestPendingCannotAccrueAcrossRestartGap(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	inc, _ := advance(nil, unhealthy(at(base, 0)), at(base, 0))
	inc, _ = advance(inc, unhealthy(at(base, 30)), at(base, 30))
	if inc.Lifecycle != LifecyclePending {
		t.Fatalf("precondition: %s", inc.Lifecycle)
	}

	// The control plane was down for five minutes. The persisted pending row
	// is reloaded, but the gap must restart confirmation instead of firing.
	inc, tr := advance(inc, unhealthy(at(base, 330)), at(base, 330))
	if inc.Lifecycle != LifecyclePending || tr.Event != "" {
		t.Fatalf("pending accrued across restart: %s %q", inc.Lifecycle, tr.Event)
	}
	inc, tr = advance(inc, unhealthy(at(base, 360)), at(base, 360))
	if tr.Event != "" {
		t.Fatalf("second fresh sample fired early: %q", tr.Event)
	}
	inc, tr = advance(inc, unhealthy(at(base, 390)), at(base, 390))
	if inc.Lifecycle != LifecycleFiring || tr.Event != TransitionOpened {
		t.Fatalf("fresh confirmation after restart: %s %q", inc.Lifecycle, tr.Event)
	}
}

func TestSeverityUpdatesOncePerChange(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	obs := unhealthy(at(base, 0))
	obs.ConfirmFor = 0
	inc, tr := advance(nil, obs, at(base, 0))
	if inc.Lifecycle != LifecycleFiring || tr.Event != TransitionOpened {
		t.Fatalf("open: %s %q", inc.Lifecycle, tr.Event)
	}

	obs = unhealthy(at(base, 10))
	obs.Severity = SeverityCritical
	inc, tr = advance(inc, obs, at(base, 10))
	if tr.Event != TransitionSeverityChanged || inc.Severity != SeverityCritical {
		t.Fatalf("escalation: %s %q", inc.Severity, tr.Event)
	}
	obs = unhealthy(at(base, 20))
	obs.Severity = SeverityCritical
	_, tr = advance(inc, obs, at(base, 20))
	if tr.Event != "" {
		t.Fatalf("unchanged severity emitted %q", tr.Event)
	}
}

func TestThresholdJitterCannotRecover(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	trigger, clear := 0.9, 0.7
	obs := unhealthy(at(base, 0))
	obs.Unhealthy = false
	obs.Value, obs.TriggerThreshold, obs.ClearThreshold = &trigger, &trigger, &clear
	obs.ConfirmFor = 0
	obs.RecoverFor = 60 * time.Second
	inc, _ := advance(nil, obs, at(base, 0))

	inBand := 0.8
	obs = unhealthy(at(base, 30))
	obs.Unhealthy = false
	obs.Value, obs.TriggerThreshold, obs.ClearThreshold = &inBand, &trigger, &clear
	inc, tr := advance(inc, obs, at(base, 30))
	if tr.Event != "" || inc.RecoveryStartedAt != nil || inc.Coverage != CoveragePartial {
		t.Fatalf("jitter = %s %s %q; want indeterminate", inc.Lifecycle, inc.Coverage, tr.Event)
	}
	obs = healthy(at(base, 90))
	clearValue := 0.5
	obs.Value = &clearValue
	inc, tr = advance(inc, obs, at(base, 90))
	if inc.Lifecycle != LifecycleFiring || tr.Event != "" {
		t.Fatalf("jitter recovered early: %s %q", inc.Lifecycle, tr.Event)
	}
}

func TestExpectedWorkBlocksOpeningAndRecovery(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	expected := unhealthy(at(base, 0))
	expected.Expected = true
	if inc, tr := advance(nil, expected, at(base, 0)); inc != nil || tr.Event != "" {
		t.Fatalf("expected work opened an incident: %+v %q", inc, tr.Event)
	}

	obs := unhealthy(at(base, 0))
	obs.ConfirmFor = 0
	obs.RecoverFor = 0
	inc, _ := advance(nil, obs, at(base, 0))
	deployExpected := healthy(at(base, 10))
	deployExpected.Expected = true
	deployExpected.RecoverFor = 0
	inc, tr := advance(inc, deployExpected, at(base, 10))
	if inc.Lifecycle != LifecycleFiring || tr.Event != "" {
		t.Fatalf("expected work recovered an incident: %s %q", inc.Lifecycle, tr.Event)
	}
	recovered := healthy(at(base, 20))
	recovered.RecoverFor = 0
	inc, tr = advance(inc, recovered, at(base, 20))
	if inc.Lifecycle != LifecycleResolved || tr.Event != TransitionRecovered {
		t.Fatalf("real recovery after expected work: %s %q", inc.Lifecycle, tr.Event)
	}
}

func TestRecurrenceCreatesNewOccurrence(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	obs := unhealthy(at(base, 0))
	obs.ConfirmFor = 0
	obs.RecoverFor = 0
	inc, _ := advance(nil, obs, at(base, 0))
	firstID := inc.ID
	recovered := healthy(at(base, 10))
	recovered.RecoverFor = 0
	inc, _ = advance(inc, recovered, at(base, 10))
	if inc.Lifecycle != LifecycleResolved {
		t.Fatalf("not resolved: %s", inc.Lifecycle)
	}
	// A resolved occurrence is no longer the active row; the next unhealthy
	// report starts a fresh pending/firing occurrence.
	recurrence := unhealthy(at(base, 100))
	recurrence.ConfirmFor = 0
	next, tr := advance(nil, recurrence, at(base, 100))
	if next == nil || next.ID == firstID || tr.Event != TransitionOpened {
		t.Fatalf("recurrence = %+v %q; want a new occurrence", next, tr.Event)
	}
}

func filesystemObservation(ob time.Time, value float64, severity Severity) Observation {
	warning, clear := 90.0, 85.0
	return Observation{
		Identity: "filesystem_space|filesystem|node|/mnt/backup|1", Detector: "filesystem_space",
		At: ob, Valid: true, Severity: severity, Value: &value,
		TriggerThreshold: &warning, ClearThreshold: &clear,
		ConfirmFor: 60 * time.Second, RecoverFor: 60 * time.Second,
		SeverityFor: 60 * time.Second, MaxGap: 120 * time.Second,
	}
}

func TestFilesystemThresholdsSustainEscalateDowngradeAndRecover(t *testing.T) {
	base := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)

	inc, tr := advance(nil, filesystemObservation(at(base, 0), 92, SeverityWarning), at(base, 0))
	if inc == nil || inc.Lifecycle != LifecyclePending || tr.Event != "" {
		t.Fatalf("warning onset = %+v %+v; want silent pending", inc, tr)
	}
	inc, tr = advance(inc, filesystemObservation(at(base, 30), 92, SeverityWarning), at(base, 30))
	if inc.Lifecycle != LifecyclePending || tr.Event != "" {
		t.Fatalf("short warning spike fired early: %s %q", inc.Lifecycle, tr.Event)
	}
	inc, tr = advance(inc, filesystemObservation(at(base, 60), 96, SeverityCritical), at(base, 60))
	if inc.Lifecycle != LifecycleFiring || tr.Event != TransitionOpened || inc.Severity != SeverityWarning {
		t.Fatalf("warning confirmation = %s %s %q; want firing warning", inc.Lifecycle, inc.Severity, tr.Event)
	}

	inc, tr = advance(inc, filesystemObservation(at(base, 90), 94, ""), at(base, 90))
	if tr.Event != "" || inc.Severity != SeverityWarning {
		t.Fatalf("intermediate band changed severity: %s %q", inc.Severity, tr.Event)
	}
	inc, tr = advance(inc, filesystemObservation(at(base, 120), 96, SeverityCritical), at(base, 120))
	if tr.Event != "" || inc.Severity != SeverityWarning {
		t.Fatalf("critical escalated before its window: %s %q", inc.Severity, tr.Event)
	}
	inc, tr = advance(inc, filesystemObservation(at(base, 180), 96, SeverityCritical), at(base, 180))
	if tr.Event != TransitionSeverityChanged || inc.Severity != SeverityCritical {
		t.Fatalf("sustained critical = %s %q; want severity changed", inc.Severity, tr.Event)
	}

	inc, tr = advance(inc, filesystemObservation(at(base, 240), 90, SeverityWarning), at(base, 240))
	if tr.Event != "" || inc.Severity != SeverityCritical {
		t.Fatalf("downgrade was immediate: %s %q", inc.Severity, tr.Event)
	}
	inc, tr = advance(inc, filesystemObservation(at(base, 300), 89, SeverityWarning), at(base, 300))
	if tr.Event != TransitionSeverityChanged || inc.Severity != SeverityWarning {
		t.Fatalf("sustained downgrade = %s %q; want warning", inc.Severity, tr.Event)
	}

	inc, tr = advance(inc, filesystemObservation(at(base, 330), 84, ""), at(base, 330))
	if tr.Event != "" || inc.Lifecycle != LifecycleFiring {
		t.Fatalf("recovery was immediate: %s %q", inc.Lifecycle, tr.Event)
	}
	inc, tr = advance(inc, filesystemObservation(at(base, 390), 84, ""), at(base, 390))
	if inc.Lifecycle != LifecycleResolved || tr.Event != TransitionRecovered {
		t.Fatalf("stable recovery = %s %q; want resolved", inc.Lifecycle, tr.Event)
	}
}

func TestFilesystemSpikeShorterThanWindowIsSilent(t *testing.T) {
	base := time.Date(2026, 2, 2, 0, 0, 0, 0, time.UTC)
	inc, _ := advance(nil, filesystemObservation(at(base, 0), 96, SeverityCritical), at(base, 0))
	inc, tr := advance(inc, filesystemObservation(at(base, 30), 80, ""), at(base, 30))
	if tr.Event != "" && tr.Event != "closed" {
		t.Fatalf("short spike emitted %q", tr.Event)
	}
	if inc.Lifecycle == LifecycleFiring || inc.DeliveryState == DeliveryPending {
		t.Fatalf("short spike fired: %+v", inc)
	}
}
