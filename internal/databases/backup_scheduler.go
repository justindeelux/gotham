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

// nextRunTime returns the next run after a scheduler served `served` at tick
// `now`. Basing it on `now` alone is wrong across a fall-back: a catch-up run
// started during the repeated hour would otherwise schedule the second
// occurrence of the same wall-clock slot and fire it again. Taking the later of
// the next run after `now` and the next run after the served slot fires each
// wall slot once while still skipping a backlog.
func nextRunTime(cron string, served, now time.Time, loc *time.Location) (time.Time, error) {
	next, err := nextCronTime(cron, now, loc)
	if err != nil {
		return time.Time{}, err
	}
	if served.IsZero() {
		return next, nil
	}
	if fromServed, servedErr := nextCronTime(cron, served, loc); servedErr == nil && fromServed.After(next) {
		return fromServed, nil
	}
	return next, nil
}

// maxCronSteps bounds the scan independently of the four-year window, so a
// pathological expression can never spin the scheduler.
const maxCronSteps = 500_000

// nextCronTime returns the first time strictly after `after`, evaluated in
// loc, that matches the expression. It walks day → hour → minute so a yearly
// schedule does not iterate over four years of minutes, and it refuses an
// expression that can never fire (for example February 30th).
//
// The scan walks the local *wall clock*, not absolute time, and treats each
// wall-clock slot as served at most once. On a fall-back day a repeated hour
// therefore fires once, not twice — including wildcard-hour schedules, which
// skip the second pass of the repeated hour entirely. On a spring-forward day a
// slot inside the skipped interval fires at the transition instant (the first
// valid instant after the gap) instead of being missed. Walking wall time also
// makes the scan incapable of stalling on a transition, so the bounded step cap
// is only a safety net.
func nextCronTime(expression string, after time.Time, loc *time.Location) (time.Time, error) {
	spec, err := parseCron(expression)
	if err != nil {
		return time.Time{}, err
	}
	if loc == nil {
		loc = time.Local
	}
	// The cursor carries the local calendar fields as a synthetic UTC time so
	// calendar arithmetic never lands on a DST transition.
	afterLocal := after.In(loc).Truncate(time.Minute)
	cursor := wallClockOf(afterLocal).Add(time.Minute)
	limit := cursor.AddDate(4, 0, 0)

	for steps := 0; cursor.Before(limit) && steps < maxCronSteps; steps++ {
		if !spec.fields[3].has(int(cursor.Month())) {
			cursor = nextWallDay(cursor)
			continue
		}
		if !spec.dayMatches(cursor) {
			cursor = nextWallDay(cursor)
			continue
		}
		if !spec.fields[1].has(cursor.Hour()) {
			cursor = cursor.Truncate(time.Hour).Add(time.Hour)
			continue
		}
		if !spec.fields[0].has(cursor.Minute()) {
			cursor = cursor.Add(time.Minute)
			continue
		}
		if instant, ok := slotInstant(cursor, after, loc); ok {
			return instant, nil
		}
		// The slot's instants are all at/before the cursor (a repeated hour
		// already served); move on to the next slot.
		cursor = cursor.Add(time.Minute)
	}
	return time.Time{}, fmt.Errorf("%w: cron expression %q never runs", ErrValidation, expression)
}

// wallClockOf returns the local calendar fields of t as a synthetic UTC time,
// so Add and AddDate move through the calendar without DST interference.
func wallClockOf(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), t.Minute(), 0, 0, time.UTC)
}

// nextWallDay returns midnight of the following calendar day.
func nextWallDay(cursor time.Time) time.Time {
	return time.Date(cursor.Year(), cursor.Month(), cursor.Day()+1, 0, 0, 0, 0, time.UTC)
}

// slotInstant resolves a matching wall-clock slot to a concrete instant
// strictly after `after`. A normal slot has one instant; a fall-back slot may
// have two, and the earliest one after `after` is returned; a spring-forward
// slot has none and resolves to the first instant after the gap. ok is false
// when every occurrence is at or before `after`.
func slotInstant(wall, after time.Time, loc *time.Location) (time.Time, bool) {
	year, month, day := wall.Date()
	first := time.Date(year, month, day, wall.Hour(), wall.Minute(), 0, 0, loc)
	if !sameWall(first, wall, loc) {
		return afterGap(wall, loc)
	}
	if first.After(after) {
		return first, true
	}
	// The first occurrence is behind `after`: a fall-back repeated hour. A
	// repeated wall clock appears at most twice, so a bounded minute walk
	// finds the later occurrence if the transition has not been left yet.
	candidate := first.Add(time.Minute)
	for i := 0; i < 4*60; i++ {
		if candidate.After(after) && sameWall(candidate, wall, loc) {
			return candidate, true
		}
		candidate = candidate.Add(time.Minute)
	}
	return time.Time{}, false
}

// afterGap resolves a wall-clock slot swallowed by a DST gap to the transition
// instant at the end of the gap (the first valid instant after it). Go's
// time.Date reports a gap slot with an offset that is not guaranteed to place
// the instant before or after the gap (it depends on the zone and transition:
// America/New_York's 02:30 maps before the gap, Europe/Paris's 02:30 after
// it), so the transition is found by walking to the offset change from
// whichever side time.Date landed on.
func afterGap(wall time.Time, loc *time.Location) (time.Time, bool) {
	year, month, day := wall.Date()
	candidate := time.Date(year, month, day, wall.Hour(), wall.Minute(), 0, 0, loc)
	_, offset := candidate.Zone()
	if wallBefore(candidate.In(loc), wall) {
		// candidate is before the gap: the transition is the first following
		// instant whose offset differs.
		for i := 0; i < 4*60; i++ {
			next := candidate.Add(time.Minute)
			if _, nextOffset := next.Zone(); nextOffset != offset {
				return next, true
			}
			candidate = next
		}
		return time.Time{}, false
	}
	// candidate is after the gap: walking back, the first instant still on the
	// post-transition offset is the transition itself.
	for i := 0; i < 4*60; i++ {
		previous := candidate.Add(-time.Minute)
		if _, previousOffset := previous.Zone(); previousOffset != offset {
			return candidate, true
		}
		candidate = previous
	}
	return time.Time{}, false
}

// sameWall reports whether instant falls on the same local wall-clock minute
// as wall.
func sameWall(instant, wall time.Time, loc *time.Location) bool {
	local := instant.In(loc)
	return local.Year() == wall.Year() &&
		local.Month() == wall.Month() &&
		local.Day() == wall.Day() &&
		local.Hour() == wall.Hour() &&
		local.Minute() == wall.Minute()
}

// wallBefore reports whether wall clock a precedes wall clock b.
func wallBefore(a, b time.Time) bool {
	switch {
	case a.Year() != b.Year():
		return a.Year() < b.Year()
	case a.Month() != b.Month():
		return a.Month() < b.Month()
	case a.Day() != b.Day():
		return a.Day() < b.Day()
	case a.Hour() != b.Hour():
		return a.Hour() < b.Hour()
	default:
		return a.Minute() < b.Minute()
	}
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
	next, err := nextRunTime(schedule.Cron, schedule.NextRunAt, now, time.Local)
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
