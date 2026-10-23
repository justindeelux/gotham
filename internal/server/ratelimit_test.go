package server

import (
	"testing"
	"time"

	"golang.org/x/time/rate"
)

func TestLimiterKeyMasksIPv6To64(t *testing.T) {
	same := [][2]string{
		{"2001:db8:1:2::1", "2001:db8:1:2:ffff:ffff:ffff:ffff"},
		{"2001:db8::1", "2001:db8::abcd"},
	}
	for _, pair := range same {
		if limiterKey(pair[0]) != limiterKey(pair[1]) {
			t.Errorf("limiterKey(%q) != limiterKey(%q), want one /64 bucket", pair[0], pair[1])
		}
	}

	distinct := [][2]string{
		{"2001:db8:1:2::1", "2001:db8:1:3::1"},
		{"1.2.3.4", "1.2.3.5"},
		{"::ffff:1.2.3.4", "::ffff:1.2.3.5"},
	}
	for _, pair := range distinct {
		if limiterKey(pair[0]) == limiterKey(pair[1]) {
			t.Errorf("limiterKey(%q) == limiterKey(%q), want separate buckets", pair[0], pair[1])
		}
	}

	if got := limiterKey("not-an-ip"); got != "not-an-ip" {
		t.Errorf("limiterKey(unparseable) = %q, want it verbatim", got)
	}
}

func TestIPRateLimiterSharesBucketAcrossIPv6Prefix(t *testing.T) {
	l := newIPRateLimiter(rate.Every(time.Hour), 2)
	t.Cleanup(l.Close)

	if !l.allow(limiterKey("2001:db8:1:2::1")) || !l.allow(limiterKey("2001:db8:1:2::9")) {
		t.Fatal("the first two requests from one /64 should be allowed")
	}
	if l.allow(limiterKey("2001:db8:1:2::ff")) {
		t.Fatal("a third request from the same /64 should be refused")
	}
	if !l.allow(limiterKey("2001:db8:1:3::1")) {
		t.Fatal("a request from a different /64 should be allowed")
	}
}
