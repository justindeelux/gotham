package updates

import (
	"context"
	"time"
)

// Scheduler is the optional schedule surface of a Service; the HTTP layer
// type-asserts for it so plain Service fakes keep working.
type Scheduler interface {
	// ScheduleState returns the live schedule and its runtime status.
	ScheduleState() ScheduleState
	// SetSchedule validates, persists and hot-applies a new schedule.
	SetSchedule(ctx context.Context, sc Schedule) (ScheduleState, error)
}

// BackoffState reports a version held back after a failed update.
type BackoffState struct {
	Version string    `json:"version"`
	Until   time.Time `json:"until"`
}

// ScheduleState is the schedule plus what the scheduler has done with it.
type ScheduleState struct {
	Schedule       Schedule      `json:"schedule"`
	Timezone       string        `json:"timezone"`
	NextRunAt      *time.Time    `json:"next_run_at,omitempty"`
	LastCheckedAt  *time.Time    `json:"last_checked_at,omitempty"`
	LastCheckError string        `json:"last_check_error,omitempty"`
	Backoff        *BackoffState `json:"backoff,omitempty"`
}

// defaultSchedule derives the no-row schedule from the environment-backed
// config: AUTO_UPDATE enables both the check and the apply, as before.
func defaultSchedule(cfg Config, interval time.Duration) Schedule {
	channel := cfg.Channel
	if channel != ChannelBeta {
		channel = ChannelStable
	}
	return Schedule{
		CheckEnabled:    cfg.Auto,
		AutoApply:       cfg.Auto,
		Channel:         channel,
		Frequency:       FrequencyInterval,
		IntervalMinutes: int(interval / time.Minute),
		AtTime:          "03:00",
	}
}

// loadSchedule overlays the persisted schedule on the defaults.
func (s *service) loadSchedule() {
	if s.schedules == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	saved, err := s.schedules.Load(ctx)
	if err != nil {
		s.logger.Warn("updates: could not load the saved schedule; using defaults", "error", err)
		return
	}
	if saved != nil {
		s.sched = *saved
	}
}

func (s *service) schedule() Schedule {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sched
}

func (s *service) recordCheck(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.checkedAt = time.Now()
	s.checkErr = ""
	if err != nil {
		s.checkErr = err.Error()
	}
}

// location is the instance timezone, falling back to UTC when it is unset or
// unknown.
func (s *service) location() *time.Location {
	if s.timezone != nil {
		if loc, err := time.LoadLocation(s.timezone()); err == nil {
			return loc
		}
	}
	return time.UTC
}

// ScheduleState implements Scheduler.
func (s *service) ScheduleState() ScheduleState {
	s.mu.Lock()
	state := ScheduleState{Schedule: s.sched, Timezone: s.location().String(), LastCheckError: s.checkErr}
	if !s.nextAt.IsZero() && s.sched.CheckEnabled {
		next := s.nextAt
		state.NextRunAt = &next
	}
	if !s.checkedAt.IsZero() {
		at := s.checkedAt
		state.LastCheckedAt = &at
	}
	s.mu.Unlock()
	if version, until, ok := s.backoff.active(); ok {
		state.Backoff = &BackoffState{Version: version, Until: until}
	}
	return state
}

// SetSchedule implements Scheduler. The loop is woken so the change takes
// effect without a restart.
func (s *service) SetSchedule(ctx context.Context, sc Schedule) (ScheduleState, error) {
	if err := sc.Validate(); err != nil {
		return ScheduleState{}, err
	}
	if s.schedules != nil {
		if err := s.schedules.Save(ctx, sc); err != nil {
			return ScheduleState{}, err
		}
	}
	s.mu.Lock()
	s.sched = sc
	s.nextAt = sc.nextRun(time.Now(), s.interval, s.location())
	s.mu.Unlock()
	select {
	case s.reload <- struct{}{}:
	default:
	}
	return s.ScheduleState(), nil
}

// StartAuto runs the scheduler until ctx is cancelled. It always runs so a
// schedule enabled later takes effect without a restart; while checks are
// disabled it just waits for a reload.
func (s *service) StartAuto(ctx context.Context) {
	go func() {
		for {
			sc := s.schedule()
			var fire <-chan time.Time
			var timer *time.Timer
			if sc.CheckEnabled {
				next := sc.nextRun(time.Now(), s.interval, s.location())
				s.mu.Lock()
				s.nextAt = next
				s.mu.Unlock()
				timer = time.NewTimer(time.Until(next))
				fire = timer.C
				s.logger.Info("updates: next scheduled check", "at", next, "auto_apply", sc.AutoApply)
			}
			select {
			case <-ctx.Done():
				if timer != nil {
					timer.Stop()
				}
				return
			case <-s.reload:
				if timer != nil {
					timer.Stop()
				}
			case <-fire:
				s.runScheduled(ctx, sc)
			}
		}
	}()
}

// runScheduled performs one scheduled check, applying when auto-apply is on.
func (s *service) runScheduled(ctx context.Context, sc Schedule) {
	if !sc.AutoApply {
		if _, err := s.Check(ctx); err != nil {
			s.logger.Warn("updates: scheduled check failed", "error", err)
		}
		return
	}
	version, applied, err := s.applyAutoUpdate(ctx)
	switch {
	case err != nil:
		s.logger.Warn("updates: auto-update failed", "error", err)
	case applied:
		s.logger.Info("updates: auto-update staged", "version", version)
	default:
		s.logger.Debug("updates: no auto-update available", "current", s.current)
	}
}
