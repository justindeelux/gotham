package servers

import (
	"context"
	"fmt"
	"net"
	"testing"

	"golang.org/x/time/rate"
	"google.golang.org/grpc/peer"
)

func TestPeerRateLimiterBoundsMemory(t *testing.T) {
	limiter := newPeerRateLimiter(rate.Limit(1), 1)

	keys := make([]string, maxPeerBuckets)
	for i := range keys {
		keys[i] = fmt.Sprintf("peer-%d", i)
		limiter.allow(keys[i])
	}
	oldest := keys[0]
	if _, ok := limiter.buckets[oldest]; !ok {
		t.Fatalf("the oldest bucket was evicted before the cap was reached")
	}

	limiter.allow("overflow-peer")

	if _, ok := limiter.buckets[oldest]; ok {
		t.Error("the least-recently-seen bucket was not evicted at the cap")
	}
	if _, ok := limiter.buckets["overflow-peer"]; !ok {
		t.Error("the new peer was not admitted after eviction")
	}
	if size := len(limiter.buckets); size > maxPeerBuckets {
		t.Errorf("limiter buckets = %d, want <= %d", size, maxPeerBuckets)
	}
}

func TestPeerHostKeysByHost(t *testing.T) {
	v4 := peer.NewContext(context.Background(), &peer.Peer{Addr: &net.TCPAddr{IP: net.ParseIP("203.0.113.9"), Port: 5555}})
	if got := peerHost(v4); got != "203.0.113.9" {
		t.Errorf("peerHost(v4) = %q, want 203.0.113.9", got)
	}
	v6 := peer.NewContext(context.Background(), &peer.Peer{Addr: &net.TCPAddr{IP: net.ParseIP("2001:db8::1"), Port: 4321}})
	if got := peerHost(v6); got != "2001:db8::1" {
		t.Errorf("peerHost(v6) = %q, want 2001:db8::1", got)
	}
}
