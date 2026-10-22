package servers

import (
	"testing"
	"time"
)

func TestAgentVersionForNode(t *testing.T) {
	service := NewService(Config{Secret: "test-secret", Logger: discardLogger()})
	service.RecordAgentVersion("node-a", "v1.2.3")
	service.RecordAgentVersion("node-b", "v9.9.9")
	entry, ok := service.AgentVersionForNode("node-a")
	if !ok || entry.Version != "v1.2.3" || entry.At.IsZero() {
		t.Fatalf("version=%+v found=%v", entry, ok)
	}
	for _, id := range []string{"", "missing", "server-uuid"} {
		if _, ok := service.AgentVersionForNode(id); ok {
			t.Fatalf("unexpected version for %q", id)
		}
	}
	service.agentUpdate.mu.Lock()
	entry.At = time.Now().Add(-agentVersionTTL - time.Second)
	service.agentUpdate.agents["node-a"] = entry
	service.agentUpdate.mu.Unlock()
	if _, ok := service.AgentVersionForNode("node-a"); ok {
		t.Fatal("expired entry exposed")
	}
	restarted := NewService(Config{Secret: "test-secret", Logger: discardLogger()})
	if _, ok := restarted.AgentVersionForNode("node-b"); ok {
		t.Fatal("restart invented a version")
	}
}
