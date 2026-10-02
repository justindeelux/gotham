package databases

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

// cronFieldBounds is the accepted value range of one cron field, in the
// canonical order of a five-field expression. Day-of-week accepts 0-7 where 7
// is Sunday, the usual vixie-cron spelling.
var cronFieldBounds = [5][2]int{
	{0, 59}, // minute
	{0, 23}, // hour
	{1, 31}, // day of month
	{1, 12}, // month
	{0, 7},  // day of week
}

// cronSpec is a parsed five-field cron expression: minute, hour, day of
// month, month, day of week. Fields accept "*", "*/step", "a", "a-b",
// "a-b/step" and comma lists of those — the vocabulary the schedule editor
// in the UI offers.
type cronSpec struct {
	fields [5]cronField
	// any marks a field written as a bare "*", which decides the classic
	// day-of-month/day-of-week rule: when both are restricted, either one
	// matches.
	any [5]bool
}

// cronField is the set of allowed values of one field.
type cronField struct {
	values map[int]struct{}
}

// has reports whether v is allowed.
func (f cronField) has(v int) bool {
	_, ok := f.values[v]
	return ok
}

// parseCron parses a five-field expression. Numeric fields only: the schedule
// editor emits numbers, and keeping names out of the grammar keeps the parser
// (and its tests) small.
func parseCron(expression string) (cronSpec, error) {
	var spec cronSpec
	parts := strings.Fields(strings.TrimSpace(expression))
	if len(parts) != 5 {
		return cronSpec{}, fmt.Errorf("%w: cron expression must have 5 fields (minute hour day month weekday), got %d",
			ErrValidation, len(parts))
	}
	for i, part := range parts {
		field, any, err := parseCronField(part, cronFieldBounds[i][0], cronFieldBounds[i][1])
		if err != nil {
			return cronSpec{}, fmt.Errorf("%w: cron field %q: %v", ErrValidation, part, err)
		}
		spec.fields[i] = field
		spec.any[i] = any
	}
	return spec, nil
}

// parseCronField expands one field into its allowed values.
func parseCronField(expression string, min, max int) (cronField, bool, error) {
	field := cronField{values: make(map[int]struct{})}
	any := false
	for _, item := range strings.Split(expression, ",") {
		item = strings.TrimSpace(item)
		if item == "" {
			return field, any, fmt.Errorf("empty list item")
		}
		rangePart, stepPart, hasStep := strings.Cut(item, "/")
		step := 1
		if hasStep {
			parsed, err := strconv.Atoi(stepPart)
			if err != nil || parsed <= 0 {
				return field, any, fmt.Errorf("step must be a positive number")
			}
			step = parsed
		}
		var low, high int
		switch {
		case rangePart == "*":
			any = any || !hasStep
			low, high = min, max
		case strings.Contains(rangePart, "-"):
			bounds := strings.SplitN(rangePart, "-", 2)
			var err error
			if low, err = strconv.Atoi(bounds[0]); err != nil {
				return field, any, fmt.Errorf("invalid range %q", rangePart)
			}
			if high, err = strconv.Atoi(bounds[1]); err != nil {
				return field, any, fmt.Errorf("invalid range %q", rangePart)
			}
		default:
			parsed, err := strconv.Atoi(rangePart)
			if err != nil {
				return field, any, fmt.Errorf("invalid value %q", rangePart)
			}
			low = parsed
			if hasStep {
				// "a/step" means "from a to the end of the range".
				high = max
			} else {
				high = parsed
			}
		}
		if low < min || high > max || low > high {
			return field, any, fmt.Errorf("value out of range %d-%d", min, max)
		}
		for value := low; value <= high; value += step {
			// The day-of-week field accepts 7 as a second spelling of
			// Sunday, so it is canonicalised to 0 while expanding.
			canonical := value
			if max == 7 && canonical == 7 {
				canonical = 0
			}
			field.values[canonical] = struct{}{}
		}
	}
	if len(field.values) == 0 {
		return field, any, fmt.Errorf("field matches nothing")
	}
	return field, any, nil
}

// maxCronSteps bounds the scan independently of the four-year window, so a
// pathological expression can never spin the scheduler.
const maxCronSteps = 500_000

// nextCronTime returns the first time strictly after `after`, evaluated in
// loc, that matches the expression. It walks day → hour → minute so a yearly
// schedule does not iterate over four years of minutes, and it refuses an
// expression that can never fire (for example February 30th).
//
// A DST transition can make a wall-clock boundary (midnight, or the
// spring-forward hour) unrepresentable. Go's time.Date then returns an instant
// at or before the cursor, which would make the scan spin forever; nextDay and
// nextHour fall back to a fixed step so the cursor always advances.
func nextCronTime(expression string, after time.Time, loc *time.Location) (time.Time, error) {
	spec, err := parseCron(expression)
	if err != nil {
		return time.Time{}, err
	}
	if loc == nil {
		loc = time.Local
	}
	cursor := after.In(loc).Truncate(time.Minute).Add(time.Minute)
	limit := cursor.AddDate(4, 0, 0)

	for steps := 0; cursor.Before(limit) && steps < maxCronSteps; steps++ {
		if !spec.fields[3].has(int(cursor.Month())) {
			cursor = nextDay(cursor, loc)
			continue
		}
		if !spec.dayMatches(cursor) {
			cursor = nextDay(cursor, loc)
			continue
		}
		if !spec.fields[1].has(cursor.Hour()) {
			cursor = nextHour(cursor, loc)
			continue
		}
		if !spec.fields[0].has(cursor.Minute()) {
			cursor = cursor.Add(time.Minute)
			continue
		}
		return cursor, nil
	}
	return time.Time{}, fmt.Errorf("%w: cron expression %q never runs", ErrValidation, expression)
}

// nextDay returns the start of the following day in loc, falling back to a
// one-hour step when a DST transition makes midnight unrepresentable.
func nextDay(cursor time.Time, loc *time.Location) time.Time {
	next := time.Date(cursor.Year(), cursor.Month(), cursor.Day()+1, 0, 0, 0, 0, loc)
	if next.After(cursor) {
		return next
	}
	return cursor.Add(time.Hour)
}

// nextHour returns the top of the following hour in loc, falling back to a
// one-hour step across the nonexistent spring-forward hour.
func nextHour(cursor time.Time, loc *time.Location) time.Time {
	next := time.Date(cursor.Year(), cursor.Month(), cursor.Day(), cursor.Hour()+1, 0, 0, 0, loc)
	if next.After(cursor) {
		return next
	}
	return cursor.Add(time.Hour)
}

// dayMatches applies the day-of-month/day-of-week rule of classic cron: a
// bare "*" never restricts, and when both fields are restricted either one
// is enough.
func (s cronSpec) dayMatches(t time.Time) bool {
	dayOfMonth := s.fields[2].has(t.Day())
	dayOfWeek := s.fields[4].has(int(t.Weekday()))
	switch {
	case s.any[2] && s.any[4]:
		return true
	case s.any[2]:
		return dayOfWeek
	case s.any[4]:
		return dayOfMonth
	default:
		return dayOfMonth || dayOfWeek
	}
}

// backupScheduler is the internal cron loop of the backup surface: a ticker
// plus one due-time query (enabled AND next_run_at <= now), with a single
// flight per schedule so two ticks can never queue the same run twice. It has
// no external dependency on purpose — the control plane already owns the
// clock and the table.
type backupScheduler struct {
	manager  *BackupManager
	interval time.Duration
	now      func() time.Time
	logger   *slog.Logger

	mu       sync.Mutex
	inflight map[uuid.UUID]bool
	cancel   context.CancelFunc
	running  bool
	wait     sync.WaitGroup
}

// newBackupScheduler builds a scheduler for a manager's repository.
func newBackupScheduler(manager *BackupManager, interval time.Duration) *backupScheduler {
	logger := manager.logger
	if logger == nil {
		logger = slog.Default()
	}
	return &backupScheduler{
		manager:  manager,
		interval: interval,
		now:      manager.now,
		logger:   logger,
		inflight: make(map[uuid.UUID]bool),
	}
}

// Start launches the loop. It is idempotent: a second call while the loop
// runs does nothing.
func (s *backupScheduler) Start() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.running {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel
	s.running = true
	s.wait.Add(1)
	go s.loop(ctx)
}

// Close stops the loop and waits for the in-flight tick to finish. It is safe
// to call on a scheduler that never started.
func (s *backupScheduler) Close() {
	s.mu.Lock()
	cancel := s.cancel
	s.cancel = nil
	s.running = false
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	s.wait.Wait()
}

// loop ticks every interval until the context is cancelled.
func (s *backupScheduler) loop(ctx context.Context) {
	defer s.wait.Done()
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if _, err := s.tick(ctx); err != nil && ctx.Err() == nil {
				s.logger.Error("databases: backup scheduler tick failed", "error", err)
			}
		}
	}
}

// tick queues one backup for every due schedule and reports how many it
// started. A schedule whose database is busy (a manual backup is running) is
// skipped for this slot: next_run_at is advanced before the job is queued, so
// a slow database never turns into a queue of duplicate runs.
func (s *backupScheduler) tick(ctx context.Context) (int, error) {
	if s.manager == nil || s.manager.backups == nil || s.manager.repo == nil {
		return 0, fmt.Errorf("databases: backup scheduler is not configured")
	}
	now := s.now()
	due, err := s.manager.backups.ListDueBackupSchedules(ctx, now)
	if err != nil {
		return 0, err
	}
	started := 0
	for _, schedule := range due {
		if !s.claim(schedule.ID) {
			continue
		}
		started += s.fire(ctx, schedule, now)
		s.release(schedule.ID)
	}
	return started, nil
}

// fire queues the backup of one due schedule.
func (s *backupScheduler) fire(ctx context.Context, schedule BackupSchedule, now time.Time) int {
	database, err := s.manager.repo.GetDatabase(ctx, schedule.DatabaseID)
	if err != nil {
		s.logger.Warn("databases: skipping backup schedule, database is gone",
			"schedule_id", schedule.ID.String(), "database_id", schedule.DatabaseID.String(), "error", err)
		return 0
	}
	next, err := nextCronTime(schedule.Cron, now, time.Local)
	if err != nil {
		s.logger.Error("databases: backup schedule has an invalid cron expression",
			"schedule_id", schedule.ID.String(), "cron", schedule.Cron, "error", err)
		return 0
	}
	// Advance first: the row is the source of truth for "has this slot been
	// served", and a restart must not replay it.
	if _, err := s.manager.backups.MarkBackupScheduleRun(ctx, schedule.ID, now, next); err != nil {
		s.logger.Error("databases: could not advance the backup schedule",
			"schedule_id", schedule.ID.String(), "error", err)
		return 0
	}

	var target *BackupTarget
	if schedule.TargetID != uuid.Nil {
		stored, err := s.manager.backups.GetBackupTarget(ctx, schedule.TargetID)
		if err != nil {
			s.logger.Warn("databases: skipping backup schedule, storage target is gone",
				"schedule_id", schedule.ID.String(), "error", err)
			return 0
		}
		target = &stored
	}
	if _, err := s.manager.startRun(ctx, database, target, BackupScheduled, schedule.ID); err != nil {
		if err != ErrBackupInFlight {
			s.logger.Error("databases: could not queue the scheduled backup",
				"schedule_id", schedule.ID.String(), "database_id", database.ID.String(), "error", err)
		} else {
			s.logger.Info("databases: scheduled backup skipped, the database is busy",
				"schedule_id", schedule.ID.String(), "database_id", database.ID.String())
		}
		return 0
	}
	return 1
}

// claim marks a schedule as being fired.
func (s *backupScheduler) claim(scheduleID uuid.UUID) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.inflight[scheduleID] {
		return false
	}
	s.inflight[scheduleID] = true
	return true
}

// release drops a schedule's claim.
func (s *backupScheduler) release(scheduleID uuid.UUID) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.inflight, scheduleID)
}
