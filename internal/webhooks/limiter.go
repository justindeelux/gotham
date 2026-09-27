package webhooks

import (
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// Delivery rate limits: a per-client-IP bucket refilling one delivery every
// three seconds with a burst of twenty — comfortably above a legitimate push
// burst (providers fan one push out over several of their own IPs) and far
// below what it takes to make signature verification expensive.
const (
	defaultDeliveryBurst    = 20
	defaultDeliveryInterval = 3 * time.Second
)

// Limiter bookkeeping: idle buckets are dropped lazily, so no goroutine and no
// Close call are needed, and a flood of distinct source IPs cannot grow the
// map without bound.
const (
	limiterEntryTTL   = 10 * time.Minute
	limiterMaxEntries = 4096
	limiterSweepEvery = 64 // sweep once every N new keys
)

// limiterEntry pairs a bucket with the last time it was used.
type limiterEntry struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

// deliveryLimiter keeps one token bucket per client IP. It is safe for
// concurrent use.
type deliveryLimiter struct {
	mu      sync.Mutex
	limiter rate.Limit
	burst   int
	entries map[string]*limiterEntry
	created int
}

// newDeliveryLimiter builds a limiter with the given refill rate and burst;
// zero values select the defaults.
func newDeliveryLimiter(limit rate.Limit, burst int) *deliveryLimiter {
	if limit <= 0 {
		limit = rate.Every(defaultDeliveryInterval)
	}
	if burst <= 0 {
		burst = defaultDeliveryBurst
	}
	return &deliveryLimiter{
		limiter: limit,
		burst:   burst,
		entries: make(map[string]*limiterEntry),
	}
}

// allow reports whether the bucket for key permits one more delivery.
func (l *deliveryLimiter) allow(key string) bool {
	now := time.Now()

	l.mu.Lock()
	defer l.mu.Unlock()

	if entry, ok := l.entries[key]; ok {
		if now.Sub(entry.lastSeen) <= limiterEntryTTL {
			entry.lastSeen = now
			return entry.limiter.Allow()
		}
		delete(l.entries, key) // idle too long: start fresh instead of bursting
	}

	l.created++
	if l.created%limiterSweepEvery == 0 || len(l.entries) >= limiterMaxEntries {
		l.evictLocked(now)
	}
	// The fresh bucket starts full; spending its first token here makes a new
	// key count against its budget immediately instead of getting a free pass.
	entry := &limiterEntry{limiter: rate.NewLimiter(l.limiter, l.burst), lastSeen: now}
	l.entries[key] = entry
	return entry.limiter.Allow()
}

// evictLocked drops idle entries and, when the map is still at its cap,
// the oldest ones. Callers hold l.mu.
func (l *deliveryLimiter) evictLocked(now time.Time) {
	for key, entry := range l.entries {
		if now.Sub(entry.lastSeen) > limiterEntryTTL {
			delete(l.entries, key)
		}
	}
	for len(l.entries) >= limiterMaxEntries {
		oldestKey := ""
		var oldest time.Time
		for key, entry := range l.entries {
			if oldestKey == "" || entry.lastSeen.Before(oldest) {
				oldestKey, oldest = key, entry.lastSeen
			}
		}
		if oldestKey == "" {
			return
		}
		delete(l.entries, oldestKey)
	}
}
