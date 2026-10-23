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

const VolumeRetention = 7 * 24 * time.Hour

const RetentionSweepInterval = time.Hour

type RetentionSweeper struct {
	repo       Repository
	containers containers.ContainerService
	interval   time.Duration
	now        func() time.Time
	logger     *slog.Logger

	mu      sync.Mutex
	running bool
	closed  bool
	cancel  context.CancelFunc
	wait    sync.WaitGroup
}

func NewRetentionSweeper(st *store.Store, containerService containers.ContainerService, logger *slog.Logger) *RetentionSweeper {
	if st == nil || containerService == nil {
		return nil
	}
	return newRetentionSweeper(newStoreRepository(st), containerService, logger)
}

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

func (r *RetentionSweeper) Start() {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.running || r.closed {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	r.cancel = cancel
	r.running = true
	r.wait.Add(1)
	go r.loop(ctx)
}

func (r *RetentionSweeper) Close() {
	if r == nil {
		return
	}
	r.mu.Lock()
	cancel := r.cancel
	r.cancel = nil
	r.running = false
	r.closed = true
	r.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	r.wait.Wait()
}

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

func (r *RetentionSweeper) expire(ctx context.Context, database Database) error {
	if database.StoragePath != "" && database.ServerID != uuid.Nil {
		if err := r.containers.RemoveVolume(ctx, database.ServerID, database.StoragePath); err != nil {
			return err
		}
	}
	return r.repo.PurgeDatabase(ctx, database.ID)
}

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
