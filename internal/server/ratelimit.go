package server

import (
	"net/http"
	"net/netip"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"golang.org/x/time/rate"

	"github.com/justindeelux/gotham/internal/clientip"
)

const (
	authRateBurst    = 5
	authRateInterval = 12 * time.Second
)

const (
	sshRateBurst    = 5
	sshRateInterval = 12 * time.Second
)

const (
	refreshRateBurst    = 30
	refreshRateInterval = time.Second
)

const (
	rateLimiterCleanupInterval = time.Minute
	rateLimiterEntryTTL        = 10 * time.Minute
)

type limiterEntry struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

type ipRateLimiter struct {
	limit   rate.Limit
	burst   int
	mu      sync.Mutex
	entries map[string]*limiterEntry
	stop    chan struct{}
	once    sync.Once
}

func newDefaultAuthLimiter() *ipRateLimiter {
	return newIPRateLimiter(rate.Every(authRateInterval), authRateBurst)
}

func newDefaultRefreshLimiter() *ipRateLimiter {
	return newIPRateLimiter(rate.Every(refreshRateInterval), refreshRateBurst)
}

func newDefaultSSHLimiter() *ipRateLimiter {
	return newIPRateLimiter(rate.Every(sshRateInterval), sshRateBurst)
}

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

func (l *ipRateLimiter) cleanup(now time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()

	for key, entry := range l.entries {
		if now.Sub(entry.lastSeen) > rateLimiterEntryTTL {
			delete(l.entries, key)
		}
	}
}

func (l *ipRateLimiter) Close() {
	l.once.Do(func() { close(l.stop) })
}

func limiterKey(ip string) string {
	addr, err := netip.ParseAddr(ip)
	if err != nil || !addr.Is6() || addr.Is4In6() {
		return ip
	}
	return netip.PrefixFrom(addr, 64).Masked().String()
}

func (s *Server) rateLimit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.authLimiter.allow(limiterKey(clientip.ClientIP(r, s.trustedProxies))) {
			writeJSON(w, http.StatusTooManyRequests, apiError{Message: "too many requests"})
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) refreshRateLimit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.refreshLimiter.allow(limiterKey(clientip.ClientIP(r, s.trustedProxies))) {
			writeJSON(w, http.StatusTooManyRequests, apiError{Message: "too many requests"})
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) sshRateLimit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Per server so checking every node at once still works; validation and
		// key install share the bucket because both may carry a password.
		key := chi.URLParam(r, "id")
		if key == "" {
			key = limiterKey(clientip.ClientIP(r, s.trustedProxies))
		}
		if !s.sshLimiter.allow(key) {
			writeJSON(w, http.StatusTooManyRequests, apiError{Message: "too many requests"})
			return
		}
		next.ServeHTTP(w, r)
	})
}
