package databases

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/backupwatch"
)

func TestScheduleMaxRunSecondsDefaultsAndValidates(t *testing.T) {
	fixture := newBackupFixture(t)
	created, err := fixture.manager.CreateSchedule(context.Background(), fixture.userID, fixture.database.ID, ScheduleRequest{
		Cron: "0 2 * * *",
	})
	if err != nil {
		t.Fatalf("CreateSchedule: %v", err)
	}
	if created.MaxRunSeconds != DefaultMaxBackupRunSeconds {
		t.Fatalf("default max run = %d", created.MaxRunSeconds)
	}

	tooShort := MinMaxBackupRunSeconds - 1
	if _, err := fixture.manager.CreateSchedule(context.Background(), fixture.userID, fixture.database.ID, ScheduleRequest{
		Cron: "0 2 * * *", MaxRunSeconds: &tooShort,
	}); !errors.Is(err, ErrValidation) {
		t.Fatalf("too short error = %v", err)
	}
	tooLong := MaxMaxBackupRunSeconds + 1
	if _, err := fixture.manager.CreateSchedule(context.Background(), fixture.userID, fixture.database.ID, ScheduleRequest{
		Cron: "0 2 * * *", MaxRunSeconds: &tooLong,
	}); !errors.Is(err, ErrValidation) {
		t.Fatalf("too long error = %v", err)
	}

	valid := 45 * 60
	updated, err := fixture.manager.UpdateSchedule(context.Background(), fixture.userID, fixture.database.ID, created.ID, ScheduleRequest{
		Cron: "0 2 * * *", MaxRunSeconds: &valid,
	})
	if err != nil {
		t.Fatalf("UpdateSchedule: %v", err)
	}
	if updated.MaxRunSeconds != valid {
		t.Fatalf("max run = %d", updated.MaxRunSeconds)
	}
}

func TestScheduleEditKeepsAMissedSlot(t *testing.T) {
	fixture := newBackupFixture(t)
	missed := time.Now().UTC().Add(-2 * time.Hour).Truncate(time.Second)
	schedule := fixture.backups.seedSchedule(BackupSchedule{
		DatabaseID: fixture.database.ID,
		Cron:       "0 2 * * *",
		Enabled:    true,
		NextRunAt:  missed,
	})

	updated, err := fixture.manager.UpdateSchedule(context.Background(), fixture.userID, fixture.database.ID, schedule.ID, ScheduleRequest{
		Cron: "0 3 * * *", Enabled: boolPtr(true),
	})
	if err != nil {
		t.Fatalf("UpdateSchedule: %v", err)
	}
	if !updated.LastSlotAt.Equal(missed) {
		t.Fatalf("last slot = %v, want the missed slot %v", updated.LastSlotAt, missed)
	}
	if !updated.NextRunAt.After(time.Now()) {
		t.Fatalf("next run = %v, want a future slot", updated.NextRunAt)
	}

	// A second edit must not clear the evidence either.
	again, err := fixture.manager.UpdateSchedule(context.Background(), fixture.userID, fixture.database.ID, schedule.ID, ScheduleRequest{
		Cron: "0 4 * * *", Enabled: boolPtr(false),
	})
	if err != nil {
		t.Fatalf("second UpdateSchedule: %v", err)
	}
	if !again.LastSlotAt.Equal(missed) {
		t.Fatalf("last slot after second edit = %v", again.LastSlotAt)
	}
}

func TestSchedulerRecordsTheServedSlot(t *testing.T) {
	fixture := newBackupFixture(t)
	slot := time.Now().UTC().Add(-time.Minute)
	schedule := fixture.backups.seedSchedule(BackupSchedule{
		DatabaseID: fixture.database.ID, Cron: "0 2 * * *", Enabled: true, NextRunAt: slot,
	})
	if started := fixture.manager.scheduler.fire(context.Background(), schedule, time.Now().UTC()); started != 1 {
		t.Fatalf("fire started %d", started)
	}
	stored, ok := fixture.backups.getSchedule(schedule.ID)
	if !ok {
		t.Fatal("schedule disappeared")
	}
	if !stored.LastSlotAt.Equal(slot) {
		t.Fatalf("last slot = %v, want %v", stored.LastSlotAt, slot)
	}
}

func TestScheduleTargetChangeRecordsChange(t *testing.T) {
	fixture := newBackupFixture(t)
	targetA := fixture.backups.seedTarget(BackupTarget{UserID: fixture.userID, Name: "a", Kind: TargetS3})
	targetB := fixture.backups.seedTarget(BackupTarget{UserID: fixture.userID, Name: "b", Kind: TargetS3})
	created, err := fixture.manager.CreateSchedule(context.Background(), fixture.userID, fixture.database.ID, ScheduleRequest{
		Cron: "0 2 * * *", TargetID: strPtr(targetA.ID.String()),
	})
	if err != nil {
		t.Fatalf("CreateSchedule: %v", err)
	}
	if !created.TargetChangedAt.IsZero() {
		t.Fatalf("new schedule change = %v, want zero", created.TargetChangedAt)
	}
	updated, err := fixture.manager.UpdateSchedule(context.Background(), fixture.userID, fixture.database.ID, created.ID, ScheduleRequest{
		Cron: "0 2 * * *", TargetID: strPtr(targetB.ID.String()),
	})
	if err != nil {
		t.Fatalf("UpdateSchedule: %v", err)
	}
	if updated.TargetID != targetB.ID {
		t.Fatalf("target = %s, want %s", updated.TargetID, targetB.ID)
	}
	if updated.TargetChangedAt.IsZero() {
		t.Fatal("a target edit must record target_changed_at")
	}
}

func TestDeleteTargetMarksScheduleForTargetChange(t *testing.T) {
	fixture := newBackupFixture(t)
	target := fixture.backups.seedTarget(BackupTarget{UserID: fixture.userID, Name: "offsite", Kind: TargetS3})
	schedule, err := fixture.manager.CreateSchedule(context.Background(), fixture.userID, fixture.database.ID, ScheduleRequest{
		Cron: "0 2 * * *", TargetID: strPtr(target.ID.String()),
	})
	if err != nil {
		t.Fatalf("CreateSchedule: %v", err)
	}
	if err := fixture.manager.DeleteTarget(context.Background(), fixture.userID, target.ID); err != nil {
		t.Fatalf("DeleteTarget: %v", err)
	}
	stored, ok := fixture.backups.getSchedule(schedule.ID)
	if !ok {
		t.Fatal("schedule disappeared")
	}
	if stored.TargetID != uuid.Nil {
		t.Fatalf("target = %s, want nil", stored.TargetID)
	}
	if stored.TargetChangedAt.IsZero() {
		t.Fatal("target deletion must record target_changed_at")
	}
}

func TestBackupRunLeaseIsPerRun(t *testing.T) {
	fixture := newBackupFixture(t)
	runID, other := uuid.New(), uuid.New()
	if !fixture.manager.claimRun(fixture.database.ID, runID) {
		t.Fatal("could not claim the run")
	}
	defer fixture.manager.release(fixture.database.ID)
	if !fixture.manager.BackupRunActive(fixture.database.ID, runID) {
		t.Fatal("the owning run must be active")
	}
	if fixture.manager.BackupRunActive(fixture.database.ID, other) {
		t.Fatal("another run on the same database must not look active")
	}
}

func TestBackupProtectionDSTDeadlinesUseCronSlots(t *testing.T) {
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Skipf("tzdata unavailable: %v", err)
	}
	assertDeadline := func(t *testing.T, slot time.Time) {
		t.Helper()
		ev := backupwatch.ScheduleEvidence{
			ScheduleID: uuid.New(), DatabaseID: uuid.New(), Enabled: true,
			NextRunAt: slot, MaxRunSeconds: int(2 * time.Hour / time.Second),
		}
		if got := backupwatch.Evaluate(slot.Add(29*time.Minute), ev, backupwatch.LeaseUnknown); got.Status != backupwatch.StatusPending {
			t.Fatalf("inside grace = %q, want pending", got.Status)
		}
		got := backupwatch.Evaluate(slot.Add(30*time.Minute), ev, backupwatch.LeaseUnknown)
		if got.Status != backupwatch.StatusOverdue {
			t.Fatalf("at deadline = %q, want overdue", got.Status)
		}
		if got.DeadlineAt == nil || !got.DeadlineAt.Equal(slot.Add(30*time.Minute)) {
			t.Fatalf("deadline = %v, want %v", got.DeadlineAt, slot.Add(30*time.Minute))
		}
	}

	// Spring forward: 02:00 does not exist; the scheduler serves 03:00.
	springSlot, err := nextCronTime("0 2 * * *", time.Date(2025, 3, 9, 1, 0, 0, 0, loc), loc)
	if err != nil {
		t.Fatalf("spring nextCronTime: %v", err)
	}
	if want := time.Date(2025, 3, 9, 3, 0, 0, 0, loc); !springSlot.Equal(want) {
		t.Fatalf("spring slot = %v, want %v", springSlot, want)
	}
	assertDeadline(t, springSlot)

	// Fall back: the 01:30 wall slot repeats; the served slot must not repeat.
	fallSlot, err := nextCronTime("30 1 * * *", time.Date(2025, 11, 1, 12, 0, 0, 0, loc), loc)
	if err != nil {
		t.Fatalf("fall nextCronTime: %v", err)
	}
	assertDeadline(t, fallSlot)
	nextSlot, err := nextRunTime("30 1 * * *", fallSlot, fallSlot.Add(10*time.Minute), loc)
	if err != nil {
		t.Fatalf("fall nextRunTime: %v", err)
	}
	if want := time.Date(2025, 11, 3, 1, 30, 0, 0, loc); !nextSlot.Equal(want) {
		t.Fatalf("fall next slot = %v, want %v", nextSlot, want)
	}
	assertDeadline(t, nextSlot)
}

func boolPtr(value bool) *bool { return &value }
