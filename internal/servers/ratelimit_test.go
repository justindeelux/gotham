package servers

import (
	"context"
	"fmt"
	"net"
	"testing"

	"golang.org/x/time/rate"
	"google.golang.org/grpc/peer"
)

// TestPeerRateLimiterBoundsMemory proves the limiter's own map cannot grow past
// maxPeerBuckets under a flood of fresh peers.
func TestPeerRateLimiterBoundsMemory(t *testing.T) {
	limiter := newPeerRateLimiter(rate.Limit(1), 1)
	for i := 0; i < maxPeerBuckets+100; i++ {
		limiter.allow(fmt.Sprintf("10.%d.%d.%d", (i>>16)&0xff, (i>>8)&0xff, i&0xff))
	}
	limiter.mu.Lock()
	size := len(limiter.buckets)
	limiter.mu.Unlock()
	if size > maxPeerBuckets {
		t.Fatalf("limiter buckets = %d, want <= %d", size, maxPeerBuckets)
	}
}

// TestPeerHostKeysByHost proves the limiter key drops the ephemeral port and
// folds IPv6 to a /64, so port rotation or one IPv6 allocation cannot mint
// unlimited keys.
func TestPeerHostKeysByHost(t *testing.T) {
	v4 := peer.NewContext(context.Background(), &peer.Peer{Addr: &net.TCPAddr{IP: net.ParseIP("203.0.113.9"), Port: 5555}})
	if got := peerHost(v4); got != "203.0.113.9" {
		t.Errorf("peerHost(v4) = %q, want 203.0.113.9", got)
	}
	v6 := peer.NewContext(context.Background(), &peer.Peer{Addr: &net.TCPAddr{IP: net.ParseIP("2001:db8::1"), Port: 4321}})
	if got := peerHost(v6); got != "2001:db8::/64" {
		t.Errorf("peerHost(v6) = %q, want 2001:db8::/64", got)
	}
}
