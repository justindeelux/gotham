package servers

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/justindeelux/gotham/internal/store"
)

// MetricsSweepInterval is how often the retention sweep runs. It is hours, not
// seconds: the heartbeat path never deletes, so the table only needs an
// occasional trim.
const MetricsSweepInterval = time.Hour

// MetricsSweeper deletes samples older than MetricsRetention on a periodic
// sweep, mirroring the backup scheduler's Start/Close lifecycle. A single
// control-plane process owns the table, so no coordination is needed between
// instances.
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

// NewMetricsSweeper builds the retention sweep for st. The logger defaults to
// slog.Default; tests may override the interval and clock on the returned
// value.
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

// Start launches the sweep loop. It is idempotent, and a retention without a
// store (the handler tests) starts nothing.
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

// Close stops the loop and waits for an in-flight sweep. It is safe to call on
// a retention that never started.
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

// Sweep deletes every sample recorded before the retention horizon and reports
// how many rows were removed. It is exported so tests can drive the sweep
// deterministically.
func (r *MetricsSweeper) Sweep(ctx context.Context) (int64, error) {
	if r.store == nil {
		return 0, errors.New("servers: store is not configured")
	}
	cutoff := r.now().Add(-r.retention)
	return r.store.DeleteServerMetricsBefore(ctx, cutoff)
}

// loop sweeps once per interval until the context is cancelled.
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
