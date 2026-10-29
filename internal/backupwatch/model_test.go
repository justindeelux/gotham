package backupwatch

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/incidents"
)

func TestEvaluateDeadlinesDailyWeeklyAndDST(t *testing.T) {
	slot := time.Date(2026, 10, 27, 2, 0, 0, 0, time.UTC)
	base := ScheduleEvidence{
		ScheduleID:   uuid.New(),
		ResourceType: ResourceDatabase, ResourceID: uuid.New(),
		TargetName:    "offsite",
		Enabled:       true,
		NextRunAt:     slot,
		MaxRunSeconds: int(MaxRunDefault / time.Second),
	}

	t.Run("daily inside grace", func(t *testing.T) {
		got := Evaluate(slot.Add(29*time.Minute), base, LeaseUnknown)
		if got.Status != StatusPending {
			t.Fatalf("status = %q, want pending", got.Status)
		}
		if got.DeadlineAt == nil || !got.DeadlineAt.Equal(slot.Add(OverdueGrace)) {
			t.Fatalf("deadline = %v, want %v", got.DeadlineAt, slot.Add(OverdueGrace))
		}
	})

	t.Run("daily at deadline", func(t *testing.T) {
		got := Evaluate(slot.Add(OverdueGrace), base, LeaseUnknown)
		if got.Status != StatusOverdue {
			t.Fatalf("status = %q, want overdue", got.Status)
		}
		if got.OverdueSince == nil || !got.OverdueSince.Equal(slot.Add(OverdueGrace)) {
			t.Fatalf("overdue since = %v", got.OverdueSince)
		}
	})

	t.Run("weekly and monthly use their own slot", func(t *testing.T) {
		weekly := base
		weekly.NextRunAt = slot.Add(-7 * 24 * time.Hour)
		if got := Evaluate(slot, weekly, LeaseUnknown); got.Status != StatusOverdue {
			t.Fatalf("weekly status = %q, want overdue", got.Status)
		}
		monthly := base
		monthly.NextRunAt = slot.Add(-31 * 24 * time.Hour)
		if got := Evaluate(slot, monthly, LeaseUnknown); got.Status != StatusOverdue {
			t.Fatalf("monthly status = %q, want overdue", got.Status)
		}
	})
}

func TestEvaluateDeadlineAtDSTBoundary(t *testing.T) {
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Skipf("tzdata unavailable: %v", err)
	}
	// Fall back: 01:30 occurs twice; the scheduler persists one instant for
	// the slot. The deadline is an absolute +30m, not a wall-clock guess.
	slot := time.Date(2025, 11, 2, 5, 30, 0, 0, time.UTC)
	ev := ScheduleEvidence{
		ScheduleID: uuid.New(), ResourceType: ResourceDatabase, ResourceID: uuid.New(), Enabled: true,
		NextRunAt: slot.In(loc), MaxRunSeconds: int(MaxRunDefault / time.Second),
	}
	got := Evaluate(slot.Add(OverdueGrace), ev, LeaseUnknown)
	if got.Status != StatusOverdue {
		t.Fatalf("status = %q, want overdue", got.Status)
	}
	if !got.DueSlotAt.Equal(slot) || !got.DeadlineAt.Equal(slot.Add(OverdueGrace)) {
		t.Fatalf("slot/deadline drift: %v / %v", got.DueSlotAt, got.DeadlineAt)
	}
	// A second evaluation at the same slot must not move the deadline.
	again := Evaluate(slot.Add(OverdueGrace+time.Minute), ev, LeaseUnknown)
	if !again.DeadlineAt.Equal(*got.DeadlineAt) {
		t.Fatalf("deadline drifted from %v to %v", got.DeadlineAt, again.DeadlineAt)
	}
}

func TestEvaluateSuccessMustCoverTheSlot(t *testing.T) {
	slot := time.Date(2026, 10, 27, 2, 0, 0, 0, time.UTC)
	ev := ScheduleEvidence{
		ScheduleID: uuid.New(), ResourceType: ResourceDatabase, ResourceID: uuid.New(), Enabled: true,
		NextRunAt: slot, MaxRunSeconds: int(MaxRunDefault / time.Second),
	}
	before := slot.Add(-time.Minute)
	ev.LastSuccessAt = &before
	if got := Evaluate(slot.Add(OverdueGrace), ev, LeaseUnknown); got.Status != StatusOverdue {
		t.Fatalf("success before slot = %q, want overdue", got.Status)
	}
	after := slot.Add(time.Minute)
	ev.LastSuccessAt = &after
	if got := Evaluate(slot.Add(time.Minute), ev, LeaseUnknown); got.Status != StatusProtected {
		t.Fatalf("success after slot = %q, want protected", got.Status)
	}
}

func TestEvaluateRunningLeaseStates(t *testing.T) {
	slot := time.Date(2026, 10, 27, 2, 0, 0, 0, time.UTC)
	running := slot.Add(time.Minute)
	ev := ScheduleEvidence{
		ScheduleID: uuid.New(), ResourceType: ResourceDatabase, ResourceID: uuid.New(), Enabled: true,
		NextRunAt: slot, RunningSince: &running,
		MaxRunSeconds: int(15 * time.Minute / time.Second),
	}

	t.Run("live lease still defers inside the maximum", func(t *testing.T) {
		got := Evaluate(slot.Add(10*time.Minute), ev, LeaseLive)
		if got.Status != StatusRunning {
			t.Fatalf("status = %q, want running", got.Status)
		}
	})

	t.Run("live lease past the maximum is stuck", func(t *testing.T) {
		got := Evaluate(slot.Add(6*time.Hour), ev, LeaseLive)
		if got.Status != StatusStuck {
			t.Fatalf("status = %q, want stuck", got.Status)
		}
	})

	t.Run("expired lease is stuck", func(t *testing.T) {
		got := Evaluate(slot.Add(6*time.Hour), ev, LeaseExpired)
		if got.Status != StatusStuck {
			t.Fatalf("status = %q, want stuck", got.Status)
		}
	})

	t.Run("unknown lease uses the schedule maximum", func(t *testing.T) {
		if got := Evaluate(slot.Add(10*time.Minute), ev, LeaseUnknown); got.Status != StatusRunning {
			t.Fatalf("within max = %q, want running", got.Status)
		}
		got := Evaluate(slot.Add(17*time.Minute), ev, LeaseUnknown)
		if got.Status != StatusStuck {
			t.Fatalf("past max = %q, want stuck", got.Status)
		}
	})
}

func TestEvaluateTargetChangeDoesNotReuseTheOldSlot(t *testing.T) {
	slot := time.Date(2026, 10, 27, 2, 0, 0, 0, time.UTC)
	changed := slot.Add(10 * time.Minute)
	ev := ScheduleEvidence{
		ScheduleID: uuid.New(), ResourceType: ResourceDatabase, ResourceID: uuid.New(), Enabled: true,
		NextRunAt: slot, LastSlotAt: slot, TargetChangedAt: changed,
		MaxRunSeconds: int(MaxRunDefault / time.Second),
	}
	if got := Evaluate(slot.Add(2*time.Hour), ev, LeaseUnknown); got.Status != StatusScheduled {
		t.Fatalf("target-changed status = %q, want scheduled", got.Status)
	}
	if got := EvaluateOpen(slot.Add(2*time.Hour), ev, LeaseUnknown); got.Status != StatusOverdue {
		t.Fatalf("open incident status = %q, want overdue", got.Status)
	}

	// The next real slot after the change is not suppressed.
	next := changed.Add(24 * time.Hour)
	ev.NextRunAt = next
	ev.LastSlotAt = next
	if got := Evaluate(next.Add(OverdueGrace), ev, LeaseUnknown); got.Status != StatusOverdue {
		t.Fatalf("next-slot status = %q, want overdue", got.Status)
	}
}

func TestSummarizeDoesNotHideAMissedSchedule(t *testing.T) {
	now := time.Date(2026, 10, 27, 3, 0, 0, 0, time.UTC)
	databaseID := uuid.New()
	successAt := now.Add(-time.Minute)
	protected := ScheduleEvidence{
		ScheduleID: uuid.New(), ResourceType: ResourceDatabase, ResourceID: databaseID, Enabled: true,
		NextRunAt: now.Add(-time.Hour), LastSuccessAt: &successAt,
		MaxRunSeconds: int(MaxRunDefault / time.Second),
	}
	missed := ScheduleEvidence{
		ScheduleID: uuid.New(), ResourceType: ResourceDatabase, ResourceID: databaseID, Enabled: true,
		NextRunAt:     now.Add(-3 * time.Hour),
		MaxRunSeconds: int(MaxRunDefault / time.Second),
	}
	summary := Summarize([]ScheduleEvidence{protected, missed}, now, nil, nil)
	if summary.Status != StatusOverdue {
		t.Fatalf("status = %q, want overdue", summary.Status)
	}
	if len(summary.Schedules) != 2 {
		t.Fatalf("schedules = %d", len(summary.Schedules))
	}
}

func TestSummarizeNotConfigured(t *testing.T) {
	now := time.Now().UTC()
	disabled := ScheduleEvidence{Enabled: false}
	if got := Summarize([]ScheduleEvidence{disabled}, now, nil, nil); got.Status != StatusNotConfigured {
		t.Fatalf("status = %q, want not_configured", got.Status)
	}
	if got := Summarize(nil, now, nil, nil); got.Status != StatusNotConfigured {
		t.Fatalf("empty status = %q, want not_configured", got.Status)
	}
}

func TestEvaluateWorksForEveryResourceType(t *testing.T) {
	slot := time.Date(2026, 10, 27, 2, 0, 0, 0, time.UTC)
	for _, resourceType := range []string{ResourceApplication, ResourceDatabase, ResourceService} {
		t.Run(resourceType, func(t *testing.T) {
			ev := ScheduleEvidence{
				ScheduleID: uuid.New(), ResourceType: resourceType, ResourceID: uuid.New(), Enabled: true,
				NextRunAt: slot, MaxRunSeconds: int(MaxRunDefault / time.Second),
			}
			if got := Evaluate(slot.Add(OverdueGrace), ev, LeaseUnknown); got.Status != StatusOverdue {
				t.Fatalf("status = %q, want overdue", got.Status)
			}
		})
	}
}

func TestIdentityIsStableForDatabasesAndDistinctPerResourceType(t *testing.T) {
	scheduleID := uuid.New()
	database := Identity(ResourceDatabase, scheduleID)
	if want := incidents.Identity(Detector, ResourceDatabase, scheduleID); database != want {
		t.Fatalf("database identity = %q, want the existing fingerprint %q", database, want)
	}
	application := Identity(ResourceApplication, scheduleID)
	service := Identity(ResourceService, scheduleID)
	if application == database || service == database || application == service {
		t.Fatalf("identities collide: %q %q %q", database, application, service)
	}
}
