package backups

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/cronexpr"
)

type backupScheduler struct {
	manager  *BackupManager
	interval time.Duration
	now      func() time.Time
	logger   *slog.Logger
	loc      *time.Location

	mu       sync.Mutex
	inflight map[uuid.UUID]bool
	cancel   context.CancelFunc
	running  bool
	wait     sync.WaitGroup
}

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
		loc:      time.Local,
		inflight: make(map[uuid.UUID]bool),
	}
}

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
				s.logger.Error("backups: backup scheduler tick failed", "error", err)
			}
		}
	}
}

func (s *backupScheduler) tick(ctx context.Context) (int, error) {
	if s.manager == nil || s.manager.backups == nil {
		return 0, fmt.Errorf("backups: backup scheduler is not configured")
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

func (s *backupScheduler) fire(ctx context.Context, schedule BackupSchedule, now time.Time) int {
	resource, source, err := s.manager.resolve(ctx, schedule.ResourceRef)
	if err != nil {
		s.logger.Warn("backups: skipping backup schedule, the resource is gone",
			"schedule_id", schedule.ID.String(), "resource", schedule.ResourceRef.String(), "error", err)
		return 0
	}
	next, err := cronexpr.NextRun(schedule.Cron, schedule.NextRunAt, now, s.loc)
	if err != nil {
		s.logger.Error("backups: backup schedule has an invalid cron expression",
			"schedule_id", schedule.ID.String(), "cron", schedule.Cron, "error", err)
		return 0
	}
	if _, err := s.manager.backups.MarkBackupScheduleRun(ctx, schedule.ID, now, next); err != nil {
		s.logger.Error("backups: could not advance the backup schedule",
			"schedule_id", schedule.ID.String(), "error", err)
		return 0
	}

	var target *BackupTarget
	if schedule.TargetID != uuid.Nil {
		stored, err := s.manager.backups.GetBackupTarget(ctx, schedule.TargetID)
		if err != nil {
			s.logger.Warn("backups: skipping backup schedule, storage provider is gone",
				"schedule_id", schedule.ID.String(), "error", err)
			return 0
		}
		target = &stored
	} else if target, err = s.manager.defaultTarget(ctx, resource.OwnerID); err != nil {
		s.logger.Error("backups: could not resolve the default storage provider",
			"schedule_id", schedule.ID.String(), "error", err)
		return 0
	}
	if validator, ok := source.(Validator); ok {
		if err := validator.Validate(ctx, resource); err != nil {
			s.logger.Warn("backups: skipping backup schedule, the resource has no data to capture",
				"schedule_id", schedule.ID.String(), "resource", schedule.ResourceRef.String(), "error", err)
			return 0
		}
	}
	if _, err := s.manager.startRun(ctx, source, resource, target, BackupScheduled, schedule.ID, schedule.PauseDuringBackup); err != nil {
		if err != ErrBackupInFlight {
			s.logger.Error("backups: could not queue the scheduled backup",
				"schedule_id", schedule.ID.String(), "resource", resource.Ref().String(), "error", err)
		} else {
			s.logger.Info("backups: scheduled backup skipped, the resource is busy",
				"schedule_id", schedule.ID.String(), "resource", resource.Ref().String())
		}
		return 0
	}
	return 1
}

func (s *backupScheduler) claim(scheduleID uuid.UUID) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.inflight[scheduleID] {
		return false
	}
	s.inflight[scheduleID] = true
	return true
}

func (s *backupScheduler) release(scheduleID uuid.UUID) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.inflight, scheduleID)
}
