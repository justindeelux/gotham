package servers

import (
	"sync"
	"time"

	"golang.org/x/time/rate"
)

const (
	registerRateLimit  = rate.Limit(1)
	registerBurst      = 5
	heartbeatRateLimit = rate.Limit(5)
	heartbeatBurst     = 20

	peerBucketTTL  = 10 * time.Minute
	maxPeerBuckets = 4096
)

type peerRateLimiter struct {
	mu      sync.Mutex
	buckets map[string]*peerBucket
	limit   rate.Limit
	burst   int
}

type peerBucket struct {
	limiter *rate.Limiter
	seen    time.Time
}

func newPeerRateLimiter(limit rate.Limit, burst int) *peerRateLimiter {
	return &peerRateLimiter{buckets: map[string]*peerBucket{}, limit: limit, burst: burst}
}

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

func (p *peerRateLimiter) sweepLocked(now time.Time) {
	for peer, bucket := range p.buckets {
		if now.Sub(bucket.seen) > peerBucketTTL {
			delete(p.buckets, peer)
		}
	}
}

func (p *peerRateLimiter) evictOldestLocked() {
	var (
		oldestKey  string
		oldestSeen time.Time
		found      bool
	)
	for peer, bucket := range p.buckets {
		if !found || bucket.seen.Before(oldestSeen) {
			oldestKey, oldestSeen, found = peer, bucket.seen, true
		}
	}
	if found {
		delete(p.buckets, oldestKey)
	}
}
