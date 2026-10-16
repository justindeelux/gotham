package servers

import (
	"context"
	"testing"
	"time"
)

// TestLeafClassificationCountsUnobservedNodes is the N1 regression: right after
// an upgrade every existing node has agent_legacy_leaf NULL until it connects,
// so the startup warning must count those nodes as unobserved (possibly legacy)
// instead of reading as "zero legacy nodes". Assertions are one-sided because
// the integration database is shared with other packages that also write
// servers rows.
func TestLeafClassificationCountsUnobservedNodes(t *testing.T) {
	service, st, _ := newTestServiceWithAuthority(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// A node that has not connected since the column was added: node_id set,
	// agent_legacy_leaf NULL (the first-boot-after-upgrade state).
	unobservedID := uniqueNodeID("node-unobserved")
	if _, err := st.DB.Exec(ctx,
		`INSERT INTO servers (name, ip, port, ssh_user, node_id) VALUES ($1, '127.0.0.1', 22, 'root', $1)`,
		unobservedID); err != nil {
		t.Fatalf("insert unobserved node: %v", err)
	}
	cleanupNode(t, service, unobservedID)

	count, err := service.UnobservedLeafNodeCount(ctx)
	if err != nil {
		t.Fatalf("UnobservedLeafNodeCount: %v", err)
	}
	if count < 1 {
		t.Fatalf("unobserved count = %d, want >= 1: a never-connected node must be reported, not silently 0", count)
	}
	row, err := st.GetServerByNodeID(ctx, &unobservedID)
	if err != nil {
		t.Fatalf("GetServerByNodeID: %v", err)
	}
	if row.AgentLegacyLeaf != nil {
		t.Fatalf("agent_legacy_leaf = %v before any connection, want NULL", *row.AgentLegacyLeaf)
	}

	// Once the gateway classifies it as modern, the row records that (and it
	// leaves the unobserved bucket for that node).
	if err := service.AuthorizeNodeKey(ctx, unobservedID, "fingerprint-modern", false); err != nil {
		t.Fatalf("AuthorizeNodeKey(modern): %v", err)
	}
	row, err = st.GetServerByNodeID(ctx, &unobservedID)
	if err != nil {
		t.Fatalf("GetServerByNodeID after classify: %v", err)
	}
	if row.AgentLegacyLeaf == nil || *row.AgentLegacyLeaf {
		t.Fatalf("agent_legacy_leaf = %v, want a recorded modern (false) classification", row.AgentLegacyLeaf)
	}

	// A node observed with a pre-mTLS leaf is counted as legacy.
	legacyID := uniqueNodeID("node-legacy")
	if _, err := st.DB.Exec(ctx,
		`INSERT INTO servers (name, ip, port, ssh_user, node_id) VALUES ($1, '127.0.0.1', 22, 'root', $1)`,
		legacyID); err != nil {
		t.Fatalf("insert legacy node: %v", err)
	}
	cleanupNode(t, service, legacyID)
	if err := service.AuthorizeNodeKey(ctx, legacyID, "fingerprint-legacy", true); err != nil {
		t.Fatalf("AuthorizeNodeKey(legacy): %v", err)
	}
	legacyCount, err := service.LegacyLeafNodeCount(ctx)
	if err != nil {
		t.Fatalf("LegacyLeafNodeCount: %v", err)
	}
	if legacyCount < 1 {
		t.Fatalf("legacy count = %d, want >= 1 after observing a serverAuth-only leaf", legacyCount)
	}
}
