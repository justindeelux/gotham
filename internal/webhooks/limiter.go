package webhooks

import (
	"sync"
	"time"

	"golang.org/x/time/rate"
)

const (
	defaultDeliveryBurst    = 20
	defaultDeliveryInterval = 3 * time.Second
)

const (
	limiterEntryTTL   = 10 * time.Minute
	limiterMaxEntries = 4096
	limiterSweepEvery = 64
)

type limiterEntry struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

type deliveryLimiter struct {
	mu      sync.Mutex
	limiter rate.Limit
	burst   int
	entries map[string]*limiterEntry
	created int
}

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

func (l *deliveryLimiter) allow(key string) bool {
	now := time.Now()

	l.mu.Lock()
	defer l.mu.Unlock()

	if entry, ok := l.entries[key]; ok {
		if now.Sub(entry.lastSeen) <= limiterEntryTTL {
			entry.lastSeen = now
			return entry.limiter.Allow()
		}
		delete(l.entries, key)
	}

	l.created++
	if l.created%limiterSweepEvery == 0 || len(l.entries) >= limiterMaxEntries {
		l.evictLocked(now)
	}
	entry := &limiterEntry{limiter: rate.NewLimiter(l.limiter, l.burst), lastSeen: now}
	l.entries[key] = entry
	return entry.limiter.Allow()
}

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
