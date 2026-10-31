package backups

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestSchedulerTickFiresOnlyDueSchedules(t *testing.T) {
	fixture := newBackupFixture(t)
	ctx := context.Background()

	due := fixture.backups.seedSchedule(BackupSchedule{
		ResourceRef: databaseRef(fixture.databaseID),
		Cron:        "0 2 * * *",
		Enabled:     true,
		NextRunAt:   time.Now().Add(-time.Minute),
	})
	disabled := fixture.backups.seedSchedule(BackupSchedule{
		ResourceRef: databaseRef(fixture.databaseID),
		Cron:        "0 4 * * *",
		Enabled:     false,
		NextRunAt:   time.Now().Add(-time.Minute),
	})
	future := fixture.backups.seedSchedule(BackupSchedule{
		ResourceRef: databaseRef(fixture.databaseID),
		Cron:        "0 5 * * *",
		Enabled:     true,
		NextRunAt:   time.Now().Add(time.Hour),
	})

	started, err := fixture.manager.scheduler.tick(ctx)
	if err != nil {
		t.Fatalf("tick: %v", err)
	}
	if started != 1 {
		t.Fatalf("started = %d, want 1", started)
	}

	backup := fixture.waitBackup(t, firstBackupID(t, fixture))
	if backup.Type != BackupScheduled {
		t.Errorf("type = %q, want scheduled", backup.Type)
	}
	if backup.ScheduleID != due.ID {
		t.Errorf("schedule id = %s, want %s", backup.ScheduleID, due.ID)
	}

	advanced, ok := fixture.backups.getSchedule(due.ID)
	if !ok {
		t.Fatal("due schedule disappeared")
	}
	if !advanced.NextRunAt.After(time.Now()) {
		t.Errorf("next_run_at = %v, want a future instant", advanced.NextRunAt)
	}
	if advanced.LastRunAt.IsZero() {
		t.Error("last_run_at must record the tick")
	}
	for _, id := range []uuid.UUID{disabled.ID, future.ID} {
		schedule, _ := fixture.backups.getSchedule(id)
		if !schedule.LastRunAt.IsZero() {
			t.Errorf("schedule %s fired although it should not", id)
		}
	}
	if fixture.backups.countBackups() != 1 {
		t.Errorf("created %d backups, want 1", fixture.backups.countBackups())
	}
}

func TestSchedulerSingleFlightPerSchedule(t *testing.T) {
	fixture := newBackupFixture(t)
	ctx := context.Background()
	schedule := fixture.backups.seedSchedule(BackupSchedule{
		ResourceRef: databaseRef(fixture.databaseID),
		Enabled:     true,
		NextRunAt:   time.Now().Add(-time.Minute),
	})
	if !fixture.manager.scheduler.claim(schedule.ID) {
		t.Fatal("could not claim the schedule")
	}

	started, err := fixture.manager.scheduler.tick(ctx)
	if err != nil {
		t.Fatalf("tick: %v", err)
	}
	if started != 0 {
		t.Errorf("started = %d, want 0 while the schedule is claimed", started)
	}
	stored, _ := fixture.backups.getSchedule(schedule.ID)
	if !stored.LastRunAt.IsZero() {
		t.Error("a claimed schedule must not be advanced by a second tick")
	}
	if fixture.backups.countBackups() != 0 {
		t.Errorf("created %d backups, want none", fixture.backups.countBackups())
	}
}

func TestSchedulerSkipsBusyDatabaseAndKeepsCadence(t *testing.T) {
	fixture := newBackupFixture(t)
	ctx := context.Background()
	schedule := fixture.backups.seedSchedule(BackupSchedule{
		ResourceRef: databaseRef(fixture.databaseID),
		Cron:        "0 2 * * *",
		Enabled:     true,
		NextRunAt:   time.Now().Add(-time.Minute),
	})
	if !fixture.manager.claim(fixture.databaseID) {
		t.Fatal("could not claim the database")
	}
	defer fixture.manager.release(fixture.databaseID)

	started, err := fixture.manager.scheduler.tick(ctx)
	if err != nil {
		t.Fatalf("tick: %v", err)
	}
	if started != 0 {
		t.Errorf("started = %d, want 0 while the database is busy", started)
	}
	stored, _ := fixture.backups.getSchedule(schedule.ID)
	if !stored.NextRunAt.After(time.Now()) {
		t.Errorf("next_run_at = %v, want the slot served so the schedule does not hammer", stored.NextRunAt)
	}
}

func TestSchedulerTickWithInvalidCronDoesNothing(t *testing.T) {
	fixture := newBackupFixture(t)
	schedule := fixture.backups.seedSchedule(BackupSchedule{
		ResourceRef: databaseRef(fixture.databaseID),
		Cron:        "not-a-cron",
		Enabled:     true,
		NextRunAt:   time.Now().Add(-time.Minute),
	})

	started, err := fixture.manager.scheduler.tick(context.Background())
	if err != nil {
		t.Fatalf("tick: %v", err)
	}
	if started != 0 {
		t.Errorf("started = %d, want 0", started)
	}
	stored, _ := fixture.backups.getSchedule(schedule.ID)
	if !stored.LastRunAt.IsZero() {
		t.Error("an invalid cron expression must not advance the schedule")
	}
	if fixture.backups.countBackups() != 0 {
		t.Errorf("created %d backups, want none", fixture.backups.countBackups())
	}
}

func TestSchedulerStartAndClose(t *testing.T) {
	fixture := newBackupFixture(t)
	fixture.manager.scheduler.interval = 5 * time.Millisecond
	fixture.backups.seedSchedule(BackupSchedule{
		ResourceRef: databaseRef(fixture.databaseID),
		Enabled:     true,
		NextRunAt:   time.Now().Add(-time.Minute),
	})

	fixture.manager.scheduler.Start()
	fixture.manager.scheduler.Start()
	waitFor(t, "a scheduled backup", func() bool { return fixture.backups.countBackups() == 1 })
	fixture.manager.scheduler.Close()

	backup := fixture.waitBackup(t, firstBackupID(t, fixture))
	if backup.Status != BackupCompleted {
		t.Errorf("status = %q (%s), want completed", backup.Status, backup.Error)
	}
}

func firstBackupID(t *testing.T, fixture *backupFixture) uuid.UUID {
	t.Helper()
	fixture.backups.mu.Lock()
	defer fixture.backups.mu.Unlock()
	if len(fixture.backups.backupOrder) == 0 {
		t.Fatal("no backup was recorded")
	}
	return fixture.backups.backupOrder[0]
}

func TestSchedulerFallBackRecoveryAdvancesFromServedSlot(t *testing.T) {
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Skipf("tzdata unavailable: %v", err)
	}
	fixture := newBackupFixture(t)
	fixture.manager.scheduler.loc = loc

	served := time.Date(2025, 11, 2, 5, 30, 0, 0, time.UTC).In(loc)
	now := time.Date(2025, 11, 2, 6, 0, 0, 0, time.UTC).In(loc)
	schedule := fixture.backups.seedSchedule(BackupSchedule{
		ResourceRef: databaseRef(fixture.databaseID),
		Cron:        "30 1 * * *",
		Enabled:     true,
		NextRunAt:   served,
	})

	if started := fixture.manager.scheduler.fire(context.Background(), schedule, now); started != 1 {
		t.Fatalf("fire started %d, want 1", started)
	}
	fixture.waitBackup(t, firstBackupID(t, fixture))

	advanced, ok := fixture.backups.getSchedule(schedule.ID)
	if !ok {
		t.Fatal("schedule disappeared")
	}
	want := time.Date(2025, 11, 3, 1, 30, 0, 0, loc)
	if !advanced.NextRunAt.Equal(want) {
		t.Errorf("next_run_at = %v, want %v (the repeated slot must not fire twice)", advanced.NextRunAt, want)
	}
}

func TestSchedulerPassesTheSchedulePauseChoice(t *testing.T) {
	fixture := newBackupFixture(t)
	now := time.Now().UTC()
	schedule := fixture.backups.seedSchedule(BackupSchedule{
		ResourceRef:       databaseRef(fixture.databaseID),
		Cron:              "0 2 * * *",
		NextRunAt:         now.Add(-time.Minute),
		PauseDuringBackup: false,
	})
	if started := fixture.manager.scheduler.fire(context.Background(), schedule, now); started != 1 {
		t.Fatalf("fire started %d, want 1", started)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		fixture.source.mu.Lock()
		captures := append([]CaptureRequest(nil), fixture.source.captures...)
		fixture.source.mu.Unlock()
		if len(captures) > 0 {
			if captures[0].Pause {
				t.Fatal("capture paused although the schedule opted out")
			}
			// The capture is async and keeps writing under t.TempDir; wait for
			// the run before returning so cleanup cannot race it.
			fixture.waitBackup(t, firstBackupID(t, fixture))
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("the scheduled run never captured")
}
