package mcp

import (
	"strings"
	"testing"
	"time"

	"github.com/justindeelux/gotham/internal/requestreplay"
	"github.com/justindeelux/gotham/internal/servers"
)

func TestMutationKeyParity(t *testing.T) {
	keys := []string{"", "a", "stable-1._:key", strings.Repeat("a", 128), strings.Repeat("a", 129), "bad key", "a\n", "é", "\x00"}
	for value := 0; value < 128; value++ {
		keys = append(keys, string(rune(value)))
	}
	for _, key := range keys {
		if got, want := mutationKeyPattern.MatchString(key), requestreplay.ValidKey(key); got != want {
			t.Errorf("key %q: got %v want %v", key, got, want)
		}
	}
}
func TestMetricsQueryParity(t *testing.T) {
	now := time.Now().UTC()
	for _, step := range []string{"", "1m", "1h", "1d", "1s", " 1m"} {
		for _, window := range []time.Duration{-time.Second, 0, time.Second, 24 * time.Hour, 24*time.Hour + time.Nanosecond, 30 * 24 * time.Hour, 30*24*time.Hour + time.Nanosecond} {
			for _, from := range []time.Time{now, {}} {
				for _, to := range []time.Time{now.Add(window), {}} {
					got, err := parseMetricsQuery(from, to, step)
					want, backendErr := servers.ParseMetricsQuery(from, to, step)
					if got != want || (err == nil) != (backendErr == nil) {
						t.Errorf("step=%q from=%v to=%v: (%v,%v), backend (%v,%v)", step, from, to, got, err, want, backendErr)
					}
				}
			}
		}
	}
}
