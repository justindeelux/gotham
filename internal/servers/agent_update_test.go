package servers

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

// TestAgentVersionMapBounds covers the M2 hardening: control characters are
// stripped, the version is length-bounded, an empty version is refused, the map
// is capped and stale entries are evicted.
func TestAgentVersionMapBounds(t *testing.T) {
	service := NewService(Config{Secret: "test-secret", Logger: discardLogger()})

	service.RecordAgentVersion("node-a", "v1.2.3\nwith\rcontrol\t"+strings.Repeat("x", 200))
	versions := service.KnownAgentVersions()
	if len(versions) != 1 {
		t.Fatalf("versions = %+v, want one entry", versions)
	}
	if len(versions[0].Version) > maxAgentVersionLength {
		t.Fatalf("version length = %d, want <= %d", len(versions[0].Version), maxAgentVersionLength)
	}
	if strings.ContainsAny(versions[0].Version, "\n\r\t") {
		t.Fatalf("version = %q, want no control characters", versions[0].Version)
	}

	// A blank version is refused.
	service.RecordAgentVersion("node-b", "   ")
	if got := len(service.KnownAgentVersions()); got != 1 {
		t.Fatalf("versions = %d after a blank version, want 1", got)
	}

	// The map is capped.
	for i := 0; i < maxAgentVersionEntries+50; i++ {
		service.RecordAgentVersion(fmt.Sprintf("cap-%d", i), "v1.0.0")
	}
	if got := len(service.KnownAgentVersions()); got > maxAgentVersionEntries {
		t.Fatalf("versions = %d, want <= %d", got, maxAgentVersionEntries)
	}

	// Stale entries are evicted on read.
	stale := NewService(Config{Secret: "test-secret", Logger: discardLogger()})
	stale.RecordAgentVersion("old", "v1.0.0")
	stale.agentUpdate.mu.Lock()
	entry := stale.agentUpdate.agents["old"]
	entry.At = time.Now().Add(-2 * agentVersionTTL)
	stale.agentUpdate.agents["old"] = entry
	stale.agentUpdate.mu.Unlock()
	if got := stale.KnownAgentVersions(); len(got) != 0 {
		t.Fatalf("versions = %+v, want the stale entry evicted", got)
	}
}

// TestRolloutForces covers L5: a rollout forces any offered version at or above
// the target, so a partially-updated fleet converges even if a newer release was
// published within the TTL.
func TestRolloutForces(t *testing.T) {
	cases := []struct {
		target  string
		offered string
		want    bool
	}{
		{"", "v1.2.0", false},
		{"v1.2.0", "v1.2.0", true},
		{"v1.2.0", "v1.3.0", true},
		{"v1.3.0", "v1.2.0", false},
		{"garbage", "v1.2.0", false},
		{"v1.2.0", "garbage", false},
	}
	for _, tc := range cases {
		if got := rolloutForces(tc.target, tc.offered); got != tc.want {
			t.Errorf("rolloutForces(%q, %q) = %v, want %v", tc.target, tc.offered, got, tc.want)
		}
	}
}
