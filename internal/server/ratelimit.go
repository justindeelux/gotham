package server

import (
	"net/http"
	"sync"
	"time"

	"golang.org/x/time/rate"

	"github.com/justindeelux/gotham/internal/clientip"
)

// Auth rate-limit tuning: a per-IP token bucket refilling five requests per
// minute with a burst of five. Applied to register and login only.
const (
	authRateBurst    = 5
	authRateInterval = 12 * time.Second // one token every 12s == 5/min
)

// Refresh rate-limit tuning: a per-IP token bucket refilling one request per
// second with a burst of 30. Normal SPA rotation stays well under this, while
// an authenticated client cannot loop /auth/refresh to grow the sessions table
// without bound. Applied to refresh and logout.
const (
	refreshRateBurst    = 30
	refreshRateInterval = time.Second
)

// Rate-limiter bookkeeping: idle per-IP buckets are evicted periodically.
const (
	rateLimiterCleanupInterval = time.Minute
	rateLimiterEntryTTL        = 10 * time.Minute
)

// limiterEntry pairs a bucket with the last time it was used.
type limiterEntry struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

// ipRateLimiter keeps one token bucket per client IP. It is safe for concurrent
// use and evicts idle buckets in the background.
type ipRateLimiter struct {
	limit   rate.Limit
	burst   int
	mu      sync.Mutex
	entries map[string]*limiterEntry
	stop    chan struct{}
	once    sync.Once
}

// newDefaultAuthLimiter builds the production register/login limiter.
func newDefaultAuthLimiter() *ipRateLimiter {
	return newIPRateLimiter(rate.Every(authRateInterval), authRateBurst)
}

// newDefaultRefreshLimiter builds the production refresh/logout limiter.
func newDefaultRefreshLimiter() *ipRateLimiter {
	return newIPRateLimiter(rate.Every(refreshRateInterval), refreshRateBurst)
}

// newIPRateLimiter builds a limiter and starts its cleanup goroutine. Callers
// must Close it to stop that goroutine.
func newIPRateLimiter(limit rate.Limit, burst int) *ipRateLimiter {
	l := &ipRateLimiter{
		limit:   limit,
		burst:   burst,
		entries: make(map[string]*limiterEntry),
		stop:    make(chan struct{}),
	}
	go l.cleanupLoop()
	return l
}

// allow reports whether the bucket for key permits one more request.
func (l *ipRateLimiter) allow(key string) bool {
	l.mu.Lock()
	entry, ok := l.entries[key]
	if !ok {
		entry = &limiterEntry{limiter: rate.NewLimiter(l.limit, l.burst)}
		l.entries[key] = entry
	}
	entry.lastSeen = time.Now()
	limiter := entry.limiter
	l.mu.Unlock()

	return limiter.Allow()
}

// cleanupLoop evicts idle buckets until Close is called.
func (l *ipRateLimiter) cleanupLoop() {
	ticker := time.NewTicker(rateLimiterCleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-l.stop:
			return
		case now := <-ticker.C:
			l.cleanup(now)
		}
	}
}

// cleanup drops buckets that have not been used within rateLimiterEntryTTL.
func (l *ipRateLimiter) cleanup(now time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()

	for key, entry := range l.entries {
		if now.Sub(entry.lastSeen) > rateLimiterEntryTTL {
			delete(l.entries, key)
		}
	}
}

// Close stops the cleanup goroutine. It is safe to call more than once.
func (l *ipRateLimiter) Close() {
	l.once.Do(func() { close(l.stop) })
}

// rateLimit rejects requests from clients that exhaust their bucket.
func (s *Server) rateLimit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.authLimiter.allow(clientip.ClientIP(r, s.trustedProxies)) {
			writeJSON(w, http.StatusTooManyRequests, apiError{Message: "too many requests"})
			return
		}
		next.ServeHTTP(w, r)
	})
}

// refreshRateLimit rejects refresh/logout requests from clients that exhaust
// their bucket.
func (s *Server) refreshRateLimit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.refreshLimiter.allow(clientip.ClientIP(r, s.trustedProxies)) {
			writeJSON(w, http.StatusTooManyRequests, apiError{Message: "too many requests"})
			return
		}
		next.ServeHTTP(w, r)
	})
}
