package cleanup

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/justindeelux/gotham/internal/cronexpr"
)

const defaultSchedulerInterval = time.Minute

// Scheduler fires due cleanup schedules on a ticker. It is bounded to one
// schedule at a time, like the backup scheduler.
type Scheduler struct {
	svc      *Service
	interval time.Duration
	logger   *slog.Logger

	mu      sync.Mutex
	cancel  context.CancelFunc
	running bool
	wait    sync.WaitGroup
}

// NewScheduler builds a scheduler around a cleanup service.
func NewScheduler(svc *Service, interval time.Duration) *Scheduler {
	if interval <= 0 {
		interval = defaultSchedulerInterval
	}
	logger := slog.Default()
	if svc != nil && svc.logger != nil {
		logger = svc.logger
	}
	return &Scheduler{svc: svc, interval: interval, logger: logger}
}

// Start launches the ticker loop.
func (s *Scheduler) Start() {
	if s == nil {
		return
	}
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

// Close stops the loop and waits for an in-flight tick.
func (s *Scheduler) Close() {
	if s == nil {
		return
	}
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

func (s *Scheduler) loop(ctx context.Context) {
	defer s.wait.Done()
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if _, err := s.Tick(ctx); err != nil && ctx.Err() == nil {
				s.logger.Error("cleanup: schedule tick failed", "error", err)
			}
		}
	}
}

// Tick fires every due schedule once. A schedule already running is skipped, and
// next_run_at advances before the work starts so a crash cannot re-fire a slot.
func (s *Scheduler) Tick(ctx context.Context) (int, error) {
	if s == nil || s.svc == nil || s.svc.center == nil {
		return 0, ErrUnavailable
	}
	now := s.svc.now()
	due, err := s.svc.center.ListDueCleanupSchedules(ctx, now)
	if err != nil {
		return 0, err
	}
	started := 0
	for _, schedule := range due {
		if !s.svc.claimSchedule(schedule.ID) {
			continue
		}
		next, err := cronexpr.NextRun(schedule.Cron, schedule.NextRunAt, now, s.svc.loc)
		if err != nil {
			s.logger.Error("cleanup: schedule has an invalid cron expression",
				"schedule_id", schedule.ID.String(), "cron", schedule.Cron, "error", err)
			s.svc.releaseSchedule(schedule.ID)
			continue
		}
		if _, err := s.svc.center.MarkCleanupScheduleRun(ctx, schedule.ID, now, next); err != nil {
			s.logger.Error("cleanup: could not advance the schedule",
				"schedule_id", schedule.ID.String(), "error", err)
			s.svc.releaseSchedule(schedule.ID)
			continue
		}
		started++
		s.svc.executeSchedule(ctx, schedule)
		s.svc.releaseSchedule(schedule.ID)
	}
	return started, nil
}
