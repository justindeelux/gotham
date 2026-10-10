package updates

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/justindeelux/gotham/internal/store"
	"github.com/justindeelux/gotham/internal/store/sqlc"
)

// Schedule frequencies.
const (
	FrequencyInterval = "interval"
	FrequencyDaily    = "daily"
	FrequencyWeekly   = "weekly"
)

// Interval bounds for the "interval" frequency, in minutes (1h .. 30d).
const (
	minIntervalMinutes = 60
	maxIntervalMinutes = 43200
)

var atTimePattern = regexp.MustCompile(`^([01][0-9]|2[0-3]):[0-5][0-9]$`)

// Schedule is the operator-controlled check / auto-apply schedule.
type Schedule struct {
	CheckEnabled bool    `json:"check_enabled"`
	AutoApply    bool    `json:"auto_apply"`
	Channel      Channel `json:"channel"`
	Frequency    string  `json:"frequency"`
	// IntervalMinutes is used by the interval frequency. Zero (env defaults
	// only) means "use the service's configured interval".
	IntervalMinutes int `json:"interval_minutes"`
	// AtTime is HH:MM in the instance timezone for daily/weekly.
	AtTime string `json:"at_time"`
	// Weekday is 0 (Sunday) .. 6 for weekly.
	Weekday int `json:"weekday"`
}

// ScheduleError carries per-field validation messages.
type ScheduleError struct {
	Fields map[string]string
}

func (e *ScheduleError) Error() string { return "invalid update schedule" }

// Validate checks a schedule submitted through the API.
func (sc Schedule) Validate() error {
	fields := map[string]string{}
	if sc.Channel != ChannelStable && sc.Channel != ChannelBeta {
		fields["channel"] = "channel must be stable or beta"
	}
	switch sc.Frequency {
	case FrequencyInterval:
		if sc.IntervalMinutes < minIntervalMinutes || sc.IntervalMinutes > maxIntervalMinutes {
			fields["interval_minutes"] = fmt.Sprintf("interval must be between %d and %d minutes", minIntervalMinutes, maxIntervalMinutes)
		}
	case FrequencyDaily, FrequencyWeekly:
	default:
		fields["frequency"] = "frequency must be interval, daily or weekly"
	}
	if !atTimePattern.MatchString(sc.AtTime) {
		fields["at_time"] = "time must be HH:MM (24-hour)"
	}
	if sc.Weekday < 0 || sc.Weekday > 6 {
		fields["weekday"] = "weekday must be between 0 (Sunday) and 6"
	}
	if sc.AutoApply && !sc.CheckEnabled {
		fields["auto_apply"] = "automatic apply requires automatic checks"
	}
	if len(fields) > 0 {
		return &ScheduleError{Fields: fields}
	}
	return nil
}

// nextRun returns the next time the scheduler should fire after from.
// fallback is the interval used when IntervalMinutes is unset.
// Daily/weekly times are evaluated in loc.
func (sc Schedule) nextRun(from time.Time, fallback time.Duration, loc *time.Location) time.Time {
	from = from.In(loc)
	if sc.Frequency == FrequencyInterval {
		d := time.Duration(sc.IntervalMinutes) * time.Minute
		if d <= 0 {
			d = fallback
		}
		return from.Add(d)
	}
	var hour, minute int
	_, _ = fmt.Sscanf(sc.AtTime, "%d:%d", &hour, &minute)
	next := time.Date(from.Year(), from.Month(), from.Day(), hour, minute, 0, 0, loc)
	if sc.Frequency == FrequencyWeekly {
		next = next.AddDate(0, 0, (sc.Weekday-int(next.Weekday())+7)%7)
	}
	if !next.After(from) {
		if sc.Frequency == FrequencyWeekly {
			return next.AddDate(0, 0, 7)
		}
		return next.AddDate(0, 0, 1)
	}
	return next
}

// ScheduleStore persists the schedule. Load returns (nil, nil) when none is saved.
type ScheduleStore interface {
	Load(ctx context.Context) (*Schedule, error)
	Save(ctx context.Context, sc Schedule) error
}

type storeSchedule struct{ store *store.Store }

// NewStoreSchedule adapts the shared store to ScheduleStore.
func NewStoreSchedule(st *store.Store) ScheduleStore { return storeSchedule{store: st} }

func (s storeSchedule) Load(ctx context.Context) (*Schedule, error) {
	row, err := s.store.GetUpdateSchedule(ctx)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &Schedule{
		CheckEnabled:    row.CheckEnabled,
		AutoApply:       row.AutoApply,
		Channel:         Channel(row.Channel),
		Frequency:       row.Frequency,
		IntervalMinutes: int(row.IntervalMinutes),
		AtTime:          row.AtTime,
		Weekday:         int(row.Weekday),
	}, nil
}

func (s storeSchedule) Save(ctx context.Context, sc Schedule) error {
	_, err := s.store.UpsertUpdateSchedule(ctx, sqlc.UpsertUpdateScheduleParams{
		CheckEnabled:    sc.CheckEnabled,
		AutoApply:       sc.AutoApply,
		Channel:         string(sc.Channel),
		Frequency:       sc.Frequency,
		IntervalMinutes: int32(sc.IntervalMinutes),
		AtTime:          sc.AtTime,
		Weekday:         int16(sc.Weekday),
	})
	return err
}
