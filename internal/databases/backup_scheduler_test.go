package databases

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestParseCron(t *testing.T) {
	valid := []string{
		"0 2 * * *",
		"*/6 * * * *",
		"0 3 * * 0",
		"0 3 * * 7",
		"5,20,35 0-6 * * 1-5",
		"15/10 * * * *",
		"0 0 1 1 *",
	}
	for _, expression := range valid {
		if _, err := parseCron(expression); err != nil {
			t.Errorf("parseCron(%q) = %v, want success", expression, err)
		}
	}

	invalid := []string{
		"",
		"* * *",
		"61 * * * *",
		"* 24 * * *",
		"0 0 32 * *",
		"0 0 * 13 *",
		"0 0 * * 8",
		"0 2 * * */0",
		"0 2 * * -1",
		"a b c d e",
	}
	for _, expression := range invalid {
		if _, err := parseCron(expression); err == nil {
			t.Errorf("parseCron(%q) = nil, want an error", expression)
		}
	}
}

func TestNextCronTime(t *testing.T) {
	utc := time.UTC
	tests := []struct {
		name       string
		expression string
		after      time.Time
		want       time.Time
	}{
		{
			name:       "daily later the same day",
			expression: "0 2 * * *",
			after:      time.Date(2026, 9, 27, 1, 59, 0, 0, utc),
			want:       time.Date(2026, 9, 27, 2, 0, 0, 0, utc),
		},
		{
			name:       "daily rolls to tomorrow",
			expression: "0 2 * * *",
			after:      time.Date(2026, 9, 27, 3, 0, 0, 0, utc),
			want:       time.Date(2026, 9, 28, 2, 0, 0, 0, utc),
		},
		{
			name:       "every six hours",
			expression: "0 */6 * * *",
			after:      time.Date(2026, 9, 27, 7, 30, 0, 0, utc),
			want:       time.Date(2026, 9, 27, 12, 0, 0, 0, utc),
		},
		{
			name:       "sunday at three",
			expression: "0 3 * * 0",
			after:      time.Date(2026, 9, 27, 4, 0, 0, 0, utc), // Sunday
			want:       time.Date(2026, 10, 4, 3, 0, 0, 0, utc),
		},
		{
			name:       "weekday range",
			expression: "0 9 * * 1-5",
			after:      time.Date(2026, 9, 27, 10, 0, 0, 0, utc), // Sunday
			want:       time.Date(2026, 9, 28, 9, 0, 0, 0, utc),  // Monday
		},
		{
			name:       "list of minutes",
			expression: "5,20,35 * * * *",
			after:      time.Date(2026, 9, 27, 12, 21, 0, 0, utc),
			want:       time.Date(2026, 9, 27, 12, 35, 0, 0, utc),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := nextCronTime(test.expression, test.after, utc)
			if err != nil {
				t.Fatalf("nextCronTime(%q): %v", test.expression, err)
			}
			if !got.Equal(test.want) {
				t.Errorf("next = %v, want %v", got, test.want)
			}
			if !got.After(test.after) {
				t.Errorf("next = %v must be strictly after %v", got, test.after)
			}
		})
	}
}

func TestNextCronTimeRejectsImpossibleExpressions(t *testing.T) {
	if _, err := nextCronTime("0 0 30 2 *", time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), time.UTC); err == nil {
		t.Error("30 February must be rejected instead of looping forever")
	}
	if _, err := nextCronTime("bogus", time.Now(), time.UTC); err == nil {
		t.Error("expected a parse error")
	}
}

func TestSchedulerTickFiresOnlyDueSchedules(t *testing.T) {
	fixture := newBackupFixture(t)
	ctx := context.Background()

	due := fixture.backups.seedSchedule(BackupSchedule{
		DatabaseID: fixture.database.ID,
		Cron:       "0 2 * * *",
		Enabled:    true,
		NextRunAt:  time.Now().Add(-time.Minute),
	})
	disabled := fixture.backups.seedSchedule(BackupSchedule{
		DatabaseID: fixture.database.ID,
		Cron:       "0 4 * * *",
		Enabled:    false,
		NextRunAt:  time.Now().Add(-time.Minute),
	})
	future := fixture.backups.seedSchedule(BackupSchedule{
		DatabaseID: fixture.database.ID,
		Cron:       "0 5 * * *",
		Enabled:    true,
		NextRunAt:  time.Now().Add(time.Hour),
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
		DatabaseID: fixture.database.ID,
		Enabled:    true,
		NextRunAt:  time.Now().Add(-time.Minute),
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
		DatabaseID: fixture.database.ID,
		Cron:       "0 2 * * *",
		Enabled:    true,
		NextRunAt:  time.Now().Add(-time.Minute),
	})
	// A manual backup already holds the database.
	if !fixture.manager.claim(fixture.database.ID) {
		t.Fatal("could not claim the database")
	}
	defer fixture.manager.release(fixture.database.ID)

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
		DatabaseID: fixture.database.ID,
		Cron:       "not-a-cron",
		Enabled:    true,
		NextRunAt:  time.Now().Add(-time.Minute),
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
		DatabaseID: fixture.database.ID,
		Enabled:    true,
		NextRunAt:  time.Now().Add(-time.Minute),
	})

	fixture.manager.scheduler.Start()
	fixture.manager.scheduler.Start() // idempotent
	waitFor(t, "a scheduled backup", func() bool { return fixture.backups.countBackups() == 1 })
	fixture.manager.scheduler.Close()

	backup := fixture.waitBackup(t, firstBackupID(t, fixture))
	if backup.Status != BackupCompleted {
		t.Errorf("status = %q (%s), want completed", backup.Status, backup.Error)
	}
}

// firstBackupID returns the id of the only (or first) recorded run.
func firstBackupID(t *testing.T, fixture *backupFixture) uuid.UUID {
	t.Helper()
	fixture.backups.mu.Lock()
	defer fixture.backups.mu.Unlock()
	if len(fixture.backups.backupOrder) == 0 {
		t.Fatal("no backup was recorded")
	}
	return fixture.backups.backupOrder[0]
}

// TestSchedulerFallBackRecoveryAdvancesFromServedSlot is the round-3 U1
// regression: the fire call site must compute the next run from the served
// slot, not only from `now`, or a catch-up started in a fall-back repeated hour
// re-schedules (and then fires) the same wall-clock slot.
func TestSchedulerFallBackRecoveryAdvancesFromServedSlot(t *testing.T) {
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Skipf("tzdata unavailable: %v", err)
	}
	fixture := newBackupFixture(t)
	fixture.manager.scheduler.loc = loc

	// The schedule was due at 01:30 EDT; the scheduler catches up at 01:00 EST,
	// the second pass of the repeated hour. Ambiguous instants are built via
	// UTC because 01:00-01:59 local is ambiguous.
	served := time.Date(2025, 11, 2, 5, 30, 0, 0, time.UTC).In(loc) // 01:30 EDT
	now := time.Date(2025, 11, 2, 6, 0, 0, 0, time.UTC).In(loc)     // 01:00 EST
	schedule := fixture.backups.seedSchedule(BackupSchedule{
		DatabaseID: fixture.database.ID,
		Cron:       "30 1 * * *",
		Enabled:    true,
		NextRunAt:  served,
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
