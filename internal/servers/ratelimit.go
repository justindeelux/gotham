package servers

import (
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// Rate limits for the unauthenticated gRPC surface. An agent registers once and
// heartbeats every 10s, so these are generous for a real node and tight for a
// flood. They are per peer address.
const (
	registerRateLimit  = rate.Limit(1)
	registerBurst      = 5
	heartbeatRateLimit = rate.Limit(5)
	heartbeatBurst     = 20

	// peerBucketTTL and maxPeerBuckets bound the limiter's own memory so the
	// flood control cannot itself be the growth vector.
	peerBucketTTL  = 10 * time.Minute
	maxPeerBuckets = 4096
)

// peerRateLimiter is a small per-address token-bucket set. A nil limiter allows
// everything, which keeps the zero value usable in tests.
type peerRateLimiter struct {
	mu      sync.Mutex
	buckets map[string]*peerBucket
	limit   rate.Limit
	burst   int
}

// peerBucket is one peer's token bucket and its last touch time.
type peerBucket struct {
	limiter *rate.Limiter
	seen    time.Time
}

// newPeerRateLimiter builds a limiter with the given refill rate and burst.
func newPeerRateLimiter(limit rate.Limit, burst int) *peerRateLimiter {
	return &peerRateLimiter{buckets: map[string]*peerBucket{}, limit: limit, burst: burst}
}

// allow reports whether the peer may make one more call now.
func (p *peerRateLimiter) allow(peer string) bool {
	if p == nil {
		return true
	}
	now := time.Now()
	p.mu.Lock()
	defer p.mu.Unlock()

	bucket, ok := p.buckets[peer]
	if !ok {
		if len(p.buckets) >= maxPeerBuckets {
			p.sweepLocked(now)
			if len(p.buckets) >= maxPeerBuckets {
				p.evictOldestLocked()
			}
		}
		bucket = &peerBucket{limiter: rate.NewLimiter(p.limit, p.burst)}
		p.buckets[peer] = bucket
	}
	bucket.seen = now
	return bucket.limiter.Allow()
}

// sweepLocked drops idle buckets. The caller holds the mutex.
func (p *peerRateLimiter) sweepLocked(now time.Time) {
	for peer, bucket := range p.buckets {
		if now.Sub(bucket.seen) > peerBucketTTL {
			delete(p.buckets, peer)
		}
	}
}

// evictOldestLocked removes the least-recently-seen bucket so the map never
// exceeds maxPeerBuckets under a flood of fresh peers. The caller holds the
// mutex.
func (p *peerRateLimiter) evictOldestLocked() {
	var (
		oldestKey  string
		oldestSeen time.Time
	)
	for peer, bucket := range p.buckets {
		if oldestKey == "" || bucket.seen.Before(oldestSeen) {
			oldestKey, oldestSeen = peer, bucket.seen
		}
	}
	delete(p.buckets, oldestKey)
}
