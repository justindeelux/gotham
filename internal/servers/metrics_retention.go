package servers

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/justindeelux/gotham/internal/store"
)

const MetricsSweepInterval = time.Hour

type MetricsSweeper struct {
	store     *store.Store
	interval  time.Duration
	retention time.Duration
	now       func() time.Time
	logger    *slog.Logger

	mu      sync.Mutex
	running bool
	cancel  context.CancelFunc
	wait    sync.WaitGroup
}

func NewMetricsSweeper(st *store.Store, logger *slog.Logger) *MetricsSweeper {
	if logger == nil {
		logger = slog.Default()
	}
	return &MetricsSweeper{
		store:     st,
		interval:  MetricsSweepInterval,
		retention: MetricsRetention,
		now:       time.Now,
		logger:    logger,
	}
}

func (r *MetricsSweeper) Start() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.running || r.store == nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	r.cancel = cancel
	r.running = true
	r.wait.Add(1)
	go r.loop(ctx)
}

func (r *MetricsSweeper) Close() {
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

func (r *MetricsSweeper) Sweep(ctx context.Context) (int64, error) {
	if r.store == nil {
		return 0, errors.New("servers: store is not configured")
	}
	cutoff := r.now().Add(-r.retention)
	return r.store.DeleteServerMetricsBefore(ctx, cutoff)
}

func (r *MetricsSweeper) loop(ctx context.Context) {
	defer r.wait.Done()
	ticker := time.NewTicker(r.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			deleted, err := r.Sweep(ctx)
			if err != nil {
				if ctx.Err() == nil {
					r.logger.Error("servers: metrics retention sweep failed", "error", err)
				}
				continue
			}
			if deleted > 0 {
				r.logger.Info("servers: metrics retention sweep removed old samples", "deleted", deleted)
			}
		}
	}
}
