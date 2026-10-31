// Package cronexpr parses the five-field cron expressions Gotham schedules use
// and resolves the next slot in a node's local time zone. It is shared by the
// backup scheduler and the cleanup scheduler.
package cronexpr

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// ErrValidation marks an expression the parser rejects.
var ErrValidation = errors.New("cronexpr: validation")

var cronFieldBounds = [5][2]int{
	{0, 59},
	{0, 23},
	{1, 31},
	{1, 12},
	{0, 7},
}

type cronSpec struct {
	fields [5]cronField
	any    [5]bool
}

type cronField struct {
	values map[int]struct{}
}

func (f cronField) has(v int) bool {
	_, ok := f.values[v]
	return ok
}

// Validate reports whether expression is a five-field cron expression.
func Validate(expression string) error {
	_, err := parseCron(expression)
	return err
}

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
				high = max
			} else {
				high = parsed
			}
		}
		if low < min || high > max || low > high {
			return field, any, fmt.Errorf("value out of range %d-%d", min, max)
		}
		for value := low; value <= high; value += step {
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

// NextRun resolves the next slot, never reusing the slot a scheduler already
// served. backoff is the previously served instant; zero means none.
func NextRun(expression string, served, now time.Time, loc *time.Location) (time.Time, error) {
	next, err := Next(expression, now, loc)
	if err != nil {
		return time.Time{}, err
	}
	if served.IsZero() {
		return next, nil
	}
	if fromServed, servedErr := Next(expression, served, loc); servedErr == nil && fromServed.After(next) {
		return fromServed, nil
	}
	return next, nil
}

const maxCronSteps = 500_000

// Next resolves the first slot strictly after the given instant.
func Next(expression string, after time.Time, loc *time.Location) (time.Time, error) {
	spec, err := parseCron(expression)
	if err != nil {
		return time.Time{}, err
	}
	if loc == nil {
		loc = time.Local
	}
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
		cursor = cursor.Add(time.Minute)
	}
	return time.Time{}, fmt.Errorf("%w: cron expression %q never runs", ErrValidation, expression)
}

func wallClockOf(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), t.Minute(), 0, 0, time.UTC)
}

func nextWallDay(cursor time.Time) time.Time {
	return time.Date(cursor.Year(), cursor.Month(), cursor.Day()+1, 0, 0, 0, 0, time.UTC)
}

func slotInstant(wall, after time.Time, loc *time.Location) (time.Time, bool) {
	year, month, day := wall.Date()
	first := time.Date(year, month, day, wall.Hour(), wall.Minute(), 0, 0, loc)
	if !sameWall(first, wall, loc) {
		return afterGap(wall, loc)
	}
	if first.After(after) {
		return first, true
	}
	candidate := first.Add(time.Minute)
	for i := 0; i < 4*60; i++ {
		if candidate.After(after) && sameWall(candidate, wall, loc) {
			return candidate, true
		}
		candidate = candidate.Add(time.Minute)
	}
	return time.Time{}, false
}

func afterGap(wall time.Time, loc *time.Location) (time.Time, bool) {
	year, month, day := wall.Date()
	candidate := time.Date(year, month, day, wall.Hour(), wall.Minute(), 0, 0, loc)
	_, offset := candidate.Zone()
	if wallBefore(candidate.In(loc), wall) {
		for i := 0; i < 4*60; i++ {
			next := candidate.Add(time.Minute)
			if _, nextOffset := next.Zone(); nextOffset != offset {
				return next, true
			}
			candidate = next
		}
		return time.Time{}, false
	}
	for i := 0; i < 4*60; i++ {
		previous := candidate.Add(-time.Minute)
		if _, previousOffset := previous.Zone(); previousOffset != offset {
			return candidate, true
		}
		candidate = previous
	}
	return time.Time{}, false
}

func sameWall(instant, wall time.Time, loc *time.Location) bool {
	local := instant.In(loc)
	return local.Year() == wall.Year() &&
		local.Month() == wall.Month() &&
		local.Day() == wall.Day() &&
		local.Hour() == wall.Hour() &&
		local.Minute() == wall.Minute()
}

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
