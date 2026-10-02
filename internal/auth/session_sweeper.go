package auth

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/justindeelux/gotham/internal/store"
)

// Session sweep tuning. The interval is hours, not seconds: the refresh path
// only rotates one session at a time and never deletes, so the table needs an
// occasional trim.
const (
	// SessionSweepInterval is how often the stale-session sweep runs.
	SessionSweepInterval = time.Hour
	// sessionExpiredRetention keeps expired sessions for a grace period so a
	// late replay is still detected before the row is forgotten.
	sessionExpiredRetention = 7 * 24 * time.Hour
	// sessionRevokedRetention is the reuse-detection window: a revoked token
	// stays on record this long so replaying it can still revoke its family.
	sessionRevokedRetention = 30 * 24 * time.Hour
)

// SessionSweeper deletes stale rows from the sessions table on a periodic
// sweep, mirroring the metrics retention lifecycle. A single control-plane
// process owns the table, so no coordination is needed between instances.
type SessionSweeper struct {
	store    *store.Store
	interval time.Duration
	now      func() time.Time
	logger   *slog.Logger

	mu      sync.Mutex
	running bool
	cancel  context.CancelFunc
	wait    sync.WaitGroup
}

// NewSessionSweeper builds the stale-session sweep for st. The logger defaults
// to slog.Default; tests may override the interval and clock on the returned
// value.
func NewSessionSweeper(st *store.Store, logger *slog.Logger) *SessionSweeper {
	if logger == nil {
		logger = slog.Default()
	}
	return &SessionSweeper{
		store:    st,
		interval: SessionSweepInterval,
		now:      time.Now,
		logger:   logger,
	}
}

// Start launches the sweep loop. It is idempotent, and a sweeper without a
// store (the handler tests) starts nothing.
func (r *SessionSweeper) Start() {
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
// a sweeper that never started.
func (r *SessionSweeper) Close() {
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

// Sweep deletes every stale session and reports how many rows were removed. It
// is exported so tests can drive the sweep deterministically.
func (r *SessionSweeper) Sweep(ctx context.Context) (int64, error) {
	if r.store == nil {
		return 0, errors.New("auth: store is not configured")
	}
	expiredBefore, revokedBefore := sessionSweepCutoffs(r.now())
	return r.store.DeleteStaleSessions(ctx, expiredBefore, revokedBefore)
}

// sessionSweepCutoffs returns the delete thresholds: sessions that expired
// before the first, and sessions revoked before the second.
func sessionSweepCutoffs(now time.Time) (expiredBefore, revokedBefore time.Time) {
	return now.Add(-sessionExpiredRetention), now.Add(-sessionRevokedRetention)
}

// loop sweeps once per interval until the context is cancelled.
func (r *SessionSweeper) loop(ctx context.Context) {
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
					r.logger.Error("auth: session sweep failed", "error", err)
				}
				continue
			}
			if deleted > 0 {
				r.logger.Info("auth: session sweep removed stale sessions", "deleted", deleted)
			}
		}
	}
}
