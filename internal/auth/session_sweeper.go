package auth

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/justindeelux/gotham/internal/store"
)

const (
	SessionSweepInterval    = time.Hour
	sessionExpiredRetention = 7 * 24 * time.Hour
	sessionRevokedRetention = 30 * 24 * time.Hour
)

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

func (r *SessionSweeper) Sweep(ctx context.Context) (int64, error) {
	if r.store == nil {
		return 0, errors.New("auth: store is not configured")
	}
	expiredBefore, revokedBefore := sessionSweepCutoffs(r.now())
	return r.store.DeleteStaleSessions(ctx, expiredBefore, revokedBefore)
}

func sessionSweepCutoffs(now time.Time) (expiredBefore, revokedBefore time.Time) {
	return now.Add(-sessionExpiredRetention), now.Add(-sessionRevokedRetention)
}

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
