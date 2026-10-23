package updates

import (
	"context"
	"time"
)

type Scheduler interface {
	ScheduleState() ScheduleState
	SetSchedule(ctx context.Context, sc Schedule) (ScheduleState, error)
}

type BackoffState struct {
	Version string    `json:"version"`
	Until   time.Time `json:"until"`
}

type ScheduleState struct {
	Schedule       Schedule      `json:"schedule"`
	Timezone       string        `json:"timezone"`
	NextRunAt      *time.Time    `json:"next_run_at,omitempty"`
	LastCheckedAt  *time.Time    `json:"last_checked_at,omitempty"`
	LastCheckError string        `json:"last_check_error,omitempty"`
	Backoff        *BackoffState `json:"backoff,omitempty"`
}

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

func (s *service) location() *time.Location {
	if s.timezone != nil {
		if loc, err := time.LoadLocation(s.timezone()); err == nil {
			return loc
		}
	}
	return time.UTC
}

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
