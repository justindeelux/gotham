package servers

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/justindeelux/gotham/updatecore"
)

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

	service.RecordAgentVersion("node-b", "   ")
	if got := len(service.KnownAgentVersions()); got != 1 {
		t.Fatalf("versions = %d after a blank version, want 1", got)
	}

	for i := 0; i < maxAgentVersionEntries+50; i++ {
		service.RecordAgentVersion(fmt.Sprintf("cap-%d", i), "v1.0.0")
	}
	if got := len(service.KnownAgentVersions()); got > maxAgentVersionEntries {
		t.Fatalf("versions = %d, want <= %d", got, maxAgentVersionEntries)
	}

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

func TestAgentUpdateAutoRolloutFollowsControlPlaneVersion(t *testing.T) {
	release := &updatecore.Release{Version: "v1.2.0"}

	current := NewService(Config{Secret: "test-secret", Version: "1.2.0", Logger: discardLogger(), Updater: &fakeAgentUpdater{release: release}})
	_, rollout, retry, err := current.OfferAgentUpdate(context.Background(), "node-a", "v1.0.0", "linux", "amd64")
	if err != nil {
		t.Fatalf("OfferAgentUpdate: %v", err)
	}
	if !rollout {
		t.Fatal("rollout = false for the release the control plane runs; want an automatic rollout")
	}
	if retry {
		t.Fatal("retry = true on an automatic rollout; only a manual update retries")
	}

	behind := NewService(Config{Secret: "test-secret", Version: "1.1.0", Logger: discardLogger(), Updater: &fakeAgentUpdater{release: release}})
	_, rollout, _, err = behind.OfferAgentUpdate(context.Background(), "node-a", "v1.0.0", "linux", "amd64")
	if err != nil {
		t.Fatalf("OfferAgentUpdate: %v", err)
	}
	if rollout {
		t.Fatal("rollout = true before the control plane runs that release; want a plain offer")
	}

	dev := NewService(Config{Secret: "test-secret", Version: "dev", Logger: discardLogger(), Updater: &fakeAgentUpdater{release: release}})
	_, rollout, _, err = dev.OfferAgentUpdate(context.Background(), "node-a", "v1.0.0", "linux", "amd64")
	if err != nil {
		t.Fatalf("OfferAgentUpdate: %v", err)
	}
	if rollout {
		t.Fatal("rollout = true on a dev build; want a plain offer")
	}
}

func TestAgentUpdateStatusAndManualRetry(t *testing.T) {
	release := &updatecore.Release{Version: "v1.2.0"}
	service := NewService(Config{Secret: "test-secret", Version: "dev", Logger: discardLogger(), Updater: &fakeAgentUpdater{release: release}})
	service.RecordAgentVersion("node-a", "v1.0.0")

	if state, _ := service.AgentUpdateStatus("node-a", "v1.2.0"); state != AgentUpdatePending {
		t.Fatalf("state before an offer = %q, want pending", state)
	}

	_, rollout, _, err := service.OfferAgentUpdate(context.Background(), "node-a", "v1.0.0", "linux", "amd64")
	if err != nil {
		t.Fatalf("OfferAgentUpdate: %v", err)
	}
	if rollout {
		t.Fatal("rollout = true without a manual trigger on a dev build")
	}
	// A plain offer is not a rollout, so the node stays pending.
	if state, _ := service.AgentUpdateStatus("node-a", "v1.2.0"); state != AgentUpdatePending {
		t.Fatalf("state after a plain offer = %q, want pending", state)
	}

	service.StartAgentRolloutFor("node-a", "v1.2.0")
	if state, _ := service.AgentUpdateStatus("node-a", "v1.2.0"); state != AgentUpdatePending {
		t.Fatalf("state after the manual update = %q, want pending", state)
	}
	_, rollout, retry, err := service.OfferAgentUpdate(context.Background(), "node-a", "v1.0.0", "linux", "amd64")
	if err != nil {
		t.Fatalf("OfferAgentUpdate: %v", err)
	}
	if !rollout || !retry {
		t.Fatalf("manual rollout = %v retry = %v, want both true", rollout, retry)
	}
	if state, _ := service.AgentUpdateStatus("node-a", "v1.2.0"); state != AgentUpdateUpdating {
		t.Fatalf("state after the offer = %q, want updating", state)
	}
	// The retry is one-shot: a second poll keeps the rollout but does not clear
	// the agent's backoff again.
	_, _, retry, err = service.OfferAgentUpdate(context.Background(), "node-a", "v1.0.0", "linux", "amd64")
	if err != nil {
		t.Fatalf("OfferAgentUpdate: %v", err)
	}
	if retry {
		t.Fatal("retry = true on a second poll; want a one-shot retry")
	}

	service.RecordAgentUpdateReport("node-a", updatecore.StatusRolledBack, "health check failed", "v1.2.0")
	state, detail := service.AgentUpdateStatus("node-a", "v1.2.0")
	if state != AgentUpdateFailed || detail != "health check failed" {
		t.Fatalf("state = %q detail = %q, want failed with the wrapper reason", state, detail)
	}

	service.StartAgentRolloutFor("node-a", "v1.2.0")
	if state, detail := service.AgentUpdateStatus("node-a", "v1.2.0"); state != AgentUpdatePending || detail != "" {
		t.Fatalf("state after retry = %q detail = %q, want pending with no old failure", state, detail)
	}

	service.RecordAgentVersion("node-a", "v1.2.0")
	if state, _ := service.AgentUpdateStatus("node-a", "v1.2.0"); state != AgentUpdateUpToDate {
		t.Fatalf("state after reaching the target = %q, want up_to_date", state)
	}
}

func TestAgentUpdateRetryDropsTheFailureReportedInTheSamePoll(t *testing.T) {
	release := &updatecore.Release{Version: "v1.2.0"}
	service := NewService(Config{Secret: "test-secret", Version: "dev", Logger: discardLogger(), Updater: &fakeAgentUpdater{release: release}})
	service.RecordAgentVersion("node-a", "v1.0.0")
	service.StartAgentRolloutFor("node-a", "v1.2.0")

	// The agent reports its previous failure before it reads the retry flag.
	service.RecordAgentUpdateReport("node-a", updatecore.StatusRolledBack, "old failure", "v1.2.0")
	_, _, retry, err := service.OfferAgentUpdate(context.Background(), "node-a", "v1.0.0", "linux", "amd64")
	if err != nil {
		t.Fatalf("OfferAgentUpdate: %v", err)
	}
	if !retry {
		t.Fatal("retry = false on the first manual update poll")
	}
	if state, detail := service.AgentUpdateStatus("node-a", "v1.2.0"); state != AgentUpdateUpdating || detail != "" {
		t.Fatalf("state = %q detail = %q, want updating without the retried failure", state, detail)
	}

	// A new attempt's failure is reported normally.
	service.RecordAgentUpdateReport("node-a", updatecore.StatusRolledBack, "new failure", "v1.2.0")
	if state, detail := service.AgentUpdateStatus("node-a", "v1.2.0"); state != AgentUpdateFailed || detail != "new failure" {
		t.Fatalf("state = %q detail = %q, want the new failure", state, detail)
	}
}
