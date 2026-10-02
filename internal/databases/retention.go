package databases

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/containers"
	"github.com/justindeelux/gotham/internal/store"
)

// VolumeRetention is how long a soft-deleted database's named volume is kept
// before the retention sweep removes it. It is the window the Phase 5 rollback
// note documents, now enforced rather than only described.
const VolumeRetention = 7 * 24 * time.Hour

// RetentionSweepInterval is how often the grace-window sweep runs. The window
// is measured in days, so an hourly tick is ample; the sweep also runs once
// when the loop starts.
const RetentionSweepInterval = time.Hour

// RetentionSweeper removes the named volume of every database deleted longer
// than VolumeRetention ago and then purges the row. It mirrors the repo's
// other sweepers (auth.SessionSweeper, servers.MetricsSweeper): a ticker loop
// started at wiring time, with an exported Sweep for deterministic tests and
// for an operator-triggered run.
type RetentionSweeper struct {
	repo       Repository
	containers containers.ContainerService
	interval   time.Duration
	now        func() time.Time
	logger     *slog.Logger

	mu      sync.Mutex
	running bool
	cancel  context.CancelFunc
	wait    sync.WaitGroup
}

// NewRetentionSweeper builds the production sweeper over the control-plane
// store. It returns nil when the store or container service is missing, so the
// caller skips wiring a sweep that could not run.
func NewRetentionSweeper(st *store.Store, containerService containers.ContainerService, logger *slog.Logger) *RetentionSweeper {
	if st == nil || containerService == nil {
		return nil
	}
	return newRetentionSweeper(newStoreRepository(st), containerService, logger)
}

// newRetentionSweeper builds the sweeper over an explicit repository (tests).
func newRetentionSweeper(repo Repository, containerService containers.ContainerService, logger *slog.Logger) *RetentionSweeper {
	if logger == nil {
		logger = slog.Default()
	}
	return &RetentionSweeper{
		repo:       repo,
		containers: containerService,
		interval:   RetentionSweepInterval,
		now:        time.Now,
		logger:     logger,
	}
}

// Start launches the sweep loop. It is idempotent; a nil sweeper starts
// nothing.
func (r *RetentionSweeper) Start() {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.running {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	r.cancel = cancel
	r.running = true
	r.wait.Add(1)
	go r.loop(ctx)
}

// Close stops the loop and waits for an in-flight sweep. It is safe to call on
// a sweeper that never started.
func (r *RetentionSweeper) Close() {
	if r == nil {
		return
	}
	r.mu.Lock()
	cancel := r.cancel
	r.cancel = nil
	r.running = false
	r.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	r.wait.Wait()
}

// Sweep removes the volume of every database past the grace window and purges
// its row, returning how many databases it expired. The volume is removed
// before the row: if the agent is unreachable the row survives so the next
// sweep retries, instead of purging the row and leaking the volume forever.
// A database whose node is gone (server_id NULL) has no reachable volume, so
// its row is purged directly.
func (r *RetentionSweeper) Sweep(ctx context.Context) (int, error) {
	if r == nil || r.repo == nil {
		return 0, errors.New("databases: retention repository is not configured")
	}
	if r.containers == nil {
		return 0, errors.New("databases: retention container service is not configured")
	}
	expired, err := r.repo.ListExpiredDatabases(ctx, r.now().UTC().Add(-VolumeRetention))
	if err != nil {
		return 0, err
	}
	removed := 0
	for _, database := range expired {
		if err := r.expire(ctx, database); err != nil {
			r.logger.Warn("databases: could not expire a deleted database",
				"database_id", database.ID.String(), "volume", database.StoragePath, "error", err)
			continue
		}
		r.logger.Info("databases: removed an expired database volume",
			"database_id", database.ID.String(), "volume", database.StoragePath)
		removed++
	}
	return removed, nil
}

// expire removes one database's volume, then purges the row.
func (r *RetentionSweeper) expire(ctx context.Context, database Database) error {
	if database.StoragePath != "" && database.ServerID != uuid.Nil {
		if err := r.containers.RemoveVolume(ctx, database.ServerID, database.StoragePath); err != nil {
			return err
		}
	}
	return r.repo.PurgeDatabase(ctx, database.ID)
}

// loop sweeps once immediately, then once per interval until cancelled.
func (r *RetentionSweeper) loop(ctx context.Context) {
	defer r.wait.Done()
	r.sweepOnce(ctx)
	ticker := time.NewTicker(r.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.sweepOnce(ctx)
		}
	}
}

// sweepOnce runs one sweep and logs its outcome; a sweep failure is never
// fatal to the loop.
func (r *RetentionSweeper) sweepOnce(ctx context.Context) {
	removed, err := r.Sweep(ctx)
	if err != nil {
		if ctx.Err() == nil {
			r.logger.Error("databases: retention sweep failed", "error", err)
		}
		return
	}
	if removed > 0 {
		r.logger.Info("databases: retention sweep expired deleted databases", "removed", removed)
	}
}
