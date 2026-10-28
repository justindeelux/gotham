package databases

import (
	"context"
	"errors"
	"testing"
	"time"
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

func boolPtr(value bool) *bool { return &value }
