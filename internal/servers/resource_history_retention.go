package servers

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/justindeelux/gotham/internal/store"
)

const (
	// ResourceSweepInterval rolls up and prunes recent resource history. One
	// minute keeps the 24-hour chart's newest minute bucket available.
	ResourceSweepInterval = time.Minute

	resourceRawRetention    = 24 * time.Hour
	resourceMinuteRetention = 7 * 24 * time.Hour
	resourceHourRetention   = 30 * 24 * time.Hour

	// A sweep recomputes a bounded recent window, never the whole retention.
	resourceMinuteLookback = 2 * time.Hour
	resourceHourLookback   = 25 * time.Hour
)

// ResourceHistorySweeper rolls raw resource samples into minute and hour
// aggregates and expires each tier independently. Host metrics keep their own
// retention schedule.
type ResourceHistorySweeper struct {
	store  *store.Store
	now    func() time.Time
	logger *slog.Logger

	mu       sync.Mutex
	running  bool
	backfill bool
	cancel   context.CancelFunc
	wait     sync.WaitGroup
}

func NewResourceHistorySweeper(st *store.Store, logger *slog.Logger) *ResourceHistorySweeper {
	if logger == nil {
		logger = slog.Default()
	}
	return &ResourceHistorySweeper{store: st, now: time.Now, logger: logger, backfill: true}
}

func (r *ResourceHistorySweeper) Start() {
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

func (r *ResourceHistorySweeper) Close() {
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

func (r *ResourceHistorySweeper) Sweep(ctx context.Context) (int64, error) {
	if r.store == nil {
		return 0, errors.New("servers: store is not configured")
	}
	now := r.now().UTC()
	minuteLookback := resourceMinuteLookback
	hourLookback := resourceHourLookback
	backfill := r.backfill
	if backfill {
		// One bounded backfill at start closes rollup gaps left by downtime;
		// later sweeps only recompute the recent window. The hour tier reads
		// the full minute retention so a long outage still reaches day 30.
		minuteLookback = resourceRawRetention
		hourLookback = resourceMinuteRetention
		r.backfill = false
	}
	var total int64

	// Align each window down to its tier bucket: an unaligned start would
	// rebuild the oldest bucket from only the rows after the boundary, and the
	// replace-on-conflict upsert would overwrite a complete bucket with that
	// partial one on every sweep.
	minuteStart := alignResourceStart(now.Add(-minuteLookback), time.Minute)
	hourStart := alignResourceStart(now.Add(-hourLookback), time.Hour)
	if backfill {
		// The previous process already expired rows before the retention edge.
		// Starting the backfill on that partly expired bucket would replace a
		// complete rollup with the surviving tail, so skip to the next whole
		// bucket. The steady window keeps the aligned-down start and only
		// revisits buckets whose rows are all still present.
		minuteStart = alignResourceStartCeil(now.Add(-minuteLookback), time.Minute)
		hourStart = alignResourceStartCeil(now.Add(-hourLookback), time.Hour)
	}

	minuteRows, err := r.store.UpsertResourceMinuteRollups(ctx, minuteStart, now)
	if err != nil {
		return total, err
	}
	total += minuteRows
	hourRows, err := r.store.UpsertResourceHourRollups(ctx, hourStart, now)
	if err != nil {
		return total, err
	}
	total += hourRows

	rawDeleted, err := r.store.DeleteResourceSamplesBefore(ctx, now.Add(-resourceRawRetention))
	if err != nil {
		return total, err
	}
	total += rawDeleted
	minuteDeleted, err := r.store.DeleteResourceRollupsBefore(ctx, ResourceTierMinute, now.Add(-resourceMinuteRetention))
	if err != nil {
		return total, err
	}
	total += minuteDeleted
	hourDeleted, err := r.store.DeleteResourceRollupsBefore(ctx, ResourceTierHour, now.Add(-resourceHourRetention))
	if err != nil {
		return total, err
	}
	total += hourDeleted
	return total, nil
}

func alignResourceStart(value time.Time, bucket time.Duration) time.Time {
	return value.UTC().Truncate(bucket)
}

func alignResourceStartCeil(value time.Time, bucket time.Duration) time.Time {
	value = value.UTC()
	floor := value.Truncate(bucket)
	if floor.Equal(value) {
		return floor
	}
	return floor.Add(bucket)
}

func (r *ResourceHistorySweeper) loop(ctx context.Context) {
	defer r.wait.Done()
	ticker := time.NewTicker(ResourceSweepInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if _, err := r.Sweep(ctx); err != nil {
				if ctx.Err() == nil {
					r.logger.Error("servers: resource history sweep failed", "error", err)
				}
			}
		}
	}
}
