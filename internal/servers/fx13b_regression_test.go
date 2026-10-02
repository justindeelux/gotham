package servers

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/justindeelux/gotham/internal/store/sqlc"
	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
)

// TestServiceOfflineSweepMarksStaleNode covers A4-6: a node that stops
// heartbeating must transition ready -> offline and the persisted row must carry
// the status, so every read reports the contract the FE already understands.
func TestServiceOfflineSweepMarksStaleNode(t *testing.T) {
	service, st := newTestService(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	nodeID := uniqueNodeID("offline-node")
	if _, err := service.RegisterNode(ctx, &agentv1.RegisterRequest{NodeId: nodeID, Os: "linux"}); err != nil {
		t.Fatalf("RegisterNode: %v", err)
	}
	row, err := st.GetServerByNodeID(ctx, &nodeID)
	if err != nil {
		t.Fatalf("GetServerByNodeID: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		if err := st.DeleteServer(cleanupCtx, row.ID); err != nil {
			t.Logf("cleanup delete server: %v", err)
		}
	})

	if err := service.RecordHeartbeat(ctx, nodeID, &agentv1.HeartbeatRequest{CpuUsage: 0.1}); err != nil {
		t.Fatalf("RecordHeartbeat: %v", err)
	}
	id := uuidFromPG(row.ID)
	ready, err := service.Get(ctx, id)
	if err != nil {
		t.Fatalf("Get after heartbeat: %v", err)
	}
	if ready.Status != StatusReady {
		t.Fatalf("status after heartbeat = %q, want %q", ready.Status, StatusReady)
	}

	// Advance the service clock past the heartbeat window: the next read sweeps
	// the node offline.
	now := service.now()
	service.now = func() time.Time { return now.Add(heartbeatOfflineAfter + time.Second) }

	offline, err := service.Get(ctx, id)
	if err != nil {
		t.Fatalf("Get after the heartbeat window: %v", err)
	}
	if offline.Status != StatusOffline {
		t.Errorf("status after the heartbeat window = %q, want %q", offline.Status, StatusOffline)
	}
	persisted, err := st.GetServerByID(ctx, row.ID)
	if err != nil {
		t.Fatalf("GetServerByID: %v", err)
	}
	if persisted.Status != StatusOffline {
		t.Errorf("persisted status = %q, want %q", persisted.Status, StatusOffline)
	}

	// A fresh heartbeat brings the node back ready.
	service.now = time.Now
	if err := service.RecordHeartbeat(ctx, nodeID, &agentv1.HeartbeatRequest{CpuUsage: 0.2}); err != nil {
		t.Fatalf("RecordHeartbeat after offline: %v", err)
	}
	recovered, err := service.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	found := false
	for _, item := range recovered {
		if item.ID == id {
			found = true
			if item.Status != StatusReady {
				t.Errorf("status after recovery = %q, want %q", item.Status, StatusReady)
			}
		}
	}
	if !found {
		t.Error("List did not include the recovered node")
	}
}

// TestServiceConcurrentRegistrationConverges covers A4-12: a burst of
// registrations for one node id must converge on exactly one row, with no
// orphan row left behind with a NULL node_id.
func TestServiceConcurrentRegistrationConverges(t *testing.T) {
	service, st := newTestService(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	nodeID := uniqueNodeID("race-node")
	const racers = 8

	var wg sync.WaitGroup
	errs := make(chan error, racers)
	for i := 0; i < racers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := service.RegisterNode(ctx, &agentv1.RegisterRequest{
				NodeId: nodeID, Os: "linux", DockerVersion: "24.0.7",
			}); err != nil {
				errs <- err
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("concurrent RegisterNode: %v", err)
	}

	row, err := st.GetServerByNodeID(ctx, &nodeID)
	if err != nil {
		t.Fatalf("GetServerByNodeID: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		if err := st.DeleteServer(cleanupCtx, row.ID); err != nil {
			t.Logf("cleanup delete server: %v", err)
		}
	})

	var count int
	if err := st.DB.QueryRow(ctx,
		"SELECT count(*) FROM servers WHERE node_id = $1", nodeID).Scan(&count); err != nil {
		t.Fatalf("count by node id: %v", err)
	}
	if count != 1 {
		t.Errorf("rows for node %q = %d, want exactly 1", nodeID, count)
	}
	var orphans int
	if err := st.DB.QueryRow(ctx,
		"SELECT count(*) FROM servers WHERE node_id IS NULL AND name = $1", nodeID).Scan(&orphans); err != nil {
		t.Fatalf("count orphans: %v", err)
	}
	if orphans != 0 {
		t.Errorf("orphan node_id NULL rows for %q = %d, want 0", nodeID, orphans)
	}
}

// TestServiceRegistrationClaimsOperatorServer covers A4-12's duplicate half: an
// operator-created server (node_id NULL) whose address matches the registering
// node is updated in place, not duplicated.
func TestServiceRegistrationClaimsOperatorServer(t *testing.T) {
	service, st := newTestService(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	nodeIP := fmt.Sprintf("10.42.%d.%d", rand.Intn(250)+1, rand.Intn(250)+1)
	created, err := service.Add(ctx, uuid.New(), "operator-label", nodeIP, 22, "root", uuid.Nil)
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		if err := st.DeleteServer(cleanupCtx, pgUUID(created.ID)); err != nil {
			t.Logf("cleanup delete server: %v", err)
		}
	})
	if created.NodeID != nil {
		t.Fatalf("operator-created node_id = %v, want NULL", created.NodeID)
	}

	if _, err := service.RegisterNode(ctx, &agentv1.RegisterRequest{NodeId: nodeIP, Os: "linux"}); err != nil {
		t.Fatalf("RegisterNode: %v", err)
	}

	registered, err := st.GetServerByNodeID(ctx, &nodeIP)
	if err != nil {
		t.Fatalf("GetServerByNodeID: %v", err)
	}
	if registered.ID != pgUUID(created.ID) {
		t.Errorf("registration created a duplicate row %s, want the operator row %s",
			uuidFromPG(registered.ID), created.ID)
	}
	if registered.Name != "operator-label" {
		t.Errorf("claimed row name = %q, want the operator label preserved", registered.Name)
	}
	var count int
	if err := st.DB.QueryRow(ctx, "SELECT count(*) FROM servers WHERE ip = $1", nodeIP).Scan(&count); err != nil {
		t.Fatalf("count by ip: %v", err)
	}
	if count != 1 {
		t.Errorf("rows for ip %q = %d, want exactly 1", nodeIP, count)
	}
}

// TestServiceRegistrationClaimsOperatorServerByHostname covers the common
// case: the agent's default node id is the hostname the operator typed as the
// server address, so enrollment must claim that row too.
func TestServiceRegistrationClaimsOperatorServerByHostname(t *testing.T) {
	service, st := newTestService(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	host := fmt.Sprintf("node-%d.internal", rand.Intn(1_000_000))
	created, err := service.Add(ctx, uuid.New(), "op-host", host, 22, "root", uuid.Nil)
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		if err := st.DeleteServer(cleanupCtx, pgUUID(created.ID)); err != nil {
			t.Logf("cleanup delete server: %v", err)
		}
	})

	if _, err := service.RegisterNode(ctx, &agentv1.RegisterRequest{NodeId: host, Os: "linux"}); err != nil {
		t.Fatalf("RegisterNode: %v", err)
	}

	registered, err := st.GetServerByNodeID(ctx, &host)
	if err != nil {
		t.Fatalf("GetServerByNodeID: %v", err)
	}
	if registered.ID != pgUUID(created.ID) {
		t.Errorf("registration created a duplicate row %s, want the operator row %s",
			uuidFromPG(registered.ID), created.ID)
	}
}

// TestServicePassphraseProtectedKeyValidation covers A4-13: a
// passphrase-protected key validates when the passphrase is supplied, and fails
// closed with a clear, typed error when it is missing or wrong.
func TestServicePassphraseProtectedKeyValidation(t *testing.T) {
	service, st := newTestService(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	addr, _, stop := startSSHProbeServer(t, "unused", false)
	defer stop()
	host, port := target(t, addr)

	key, err := service.AddPrivateKey(ctx, "passphrase-key", string(testEncryptedPrivateKeyPEM(t, "s3cret")))
	if err != nil {
		t.Fatalf("AddPrivateKey: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		if _, err := st.DB.Exec(cleanupCtx, "DELETE FROM private_keys WHERE id = $1", pgUUID(key.ID)); err != nil {
			t.Logf("cleanup delete private key: %v", err)
		}
	})

	created, err := service.Add(ctx, uuid.New(), "passphrase-node", host, port, "root", key.ID)
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		if err := st.DeleteServer(cleanupCtx, pgUUID(created.ID)); err != nil {
			t.Logf("cleanup delete server: %v", err)
		}
	})

	// Missing passphrase: typed ErrValidation with a clear, actionable message.
	if _, err := service.Validate(ctx, created.ID, ValidateAuth{}); !errors.Is(err, ErrValidation) {
		t.Fatalf("Validate without a passphrase = %v, want ErrValidation", err)
	} else if !strings.Contains(err.Error(), "passphrase") {
		t.Errorf("err = %v, want it to name the missing passphrase", err)
	}

	// Wrong passphrase: still a typed validation failure.
	if _, err := service.Validate(ctx, created.ID, ValidateAuth{Passphrase: "wrong"}); !errors.Is(err, ErrValidation) {
		t.Fatalf("Validate with the wrong passphrase = %v, want ErrValidation", err)
	}

	// Correct passphrase: the node validates.
	if _, err := service.Validate(ctx, created.ID, ValidateAuth{Passphrase: "s3cret"}); err != nil {
		t.Fatalf("Validate with the passphrase: %v", err)
	}
	fetched, err := service.Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	// Validation alone never sets last_seen: that is the agent's heartbeat.
	if fetched.LastSeen != nil {
		t.Errorf("last_seen after SSH validation = %v, want nil (A4-15)", fetched.LastSeen)
	}
}

// TestServiceValidateReadyNodeKeepsReady covers fix round 1 U2: re-validating a
// ready node must not leave it stuck in `validating`. The status is restored
// from the node's fresh heartbeat.
func TestServiceValidateReadyNodeKeepsReady(t *testing.T) {
	service, st := newTestService(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	addr, _, stop := startSSHProbeServer(t, "unused", false)
	defer stop()
	host, port := target(t, addr)

	key, err := service.AddPrivateKey(ctx, "ready-key", string(testPrivateKeyPEM(t)))
	if err != nil {
		t.Fatalf("AddPrivateKey: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		if _, err := st.DB.Exec(cleanupCtx, "DELETE FROM private_keys WHERE id = $1", pgUUID(key.ID)); err != nil {
			t.Logf("cleanup delete private key: %v", err)
		}
	})

	created, err := service.Add(ctx, uuid.New(), "ready-node", host, port, "root", key.ID)
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		if err := st.DeleteServer(cleanupCtx, pgUUID(created.ID)); err != nil {
			t.Logf("cleanup delete server: %v", err)
		}
	})

	// Attach a node id and give the node a fresh heartbeat so it is ready.
	nodeID := uniqueNodeID("ready-node-id")
	if _, err := st.DB.Exec(ctx, "UPDATE servers SET node_id = $2 WHERE id = $1", pgUUID(created.ID), nodeID); err != nil {
		t.Fatalf("attach node id: %v", err)
	}
	if err := service.RecordHeartbeat(ctx, nodeID, &agentv1.HeartbeatRequest{CpuUsage: 0.1}); err != nil {
		t.Fatalf("RecordHeartbeat: %v", err)
	}
	before, err := service.Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("Get before validate: %v", err)
	}
	if before.Status != StatusReady {
		t.Fatalf("status before validate = %q, want %q", before.Status, StatusReady)
	}

	// Re-validation succeeds and must restore ready, not leave validating.
	result, err := service.Validate(ctx, created.ID, ValidateAuth{})
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if result.Server.Status != StatusReady {
		t.Errorf("response status after validation = %q, want %q", result.Server.Status, StatusReady)
	}
	after, err := service.Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("Get after validate: %v", err)
	}
	if after.Status != StatusReady {
		t.Errorf("status after validating a ready node = %q, want %q", after.Status, StatusReady)
	}
}

// TestServerStatusAfterValidationTransitions covers the atomic status restore
// directly, including the mid-validation heartbeat case (a fresh last_seen must
// win over the pending an agentless validation would set).
func TestServerStatusAfterValidationTransitions(t *testing.T) {
	service, st := newTestService(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	created, err := service.Add(ctx, uuid.New(), "transition-node", "10.99.0.1", 22, "root", uuid.Nil)
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		if err := st.DeleteServer(cleanupCtx, pgUUID(created.ID)); err != nil {
			t.Logf("cleanup delete server: %v", err)
		}
	})

	cutoff := service.now().Add(-heartbeatOfflineAfter)
	transition := func(t *testing.T, lastSeen *time.Time) string {
		t.Helper()
		if _, err := st.DB.Exec(ctx, "UPDATE servers SET status = 'validating', last_seen = $2 WHERE id = $1",
			pgUUID(created.ID), lastSeen); err != nil {
			t.Fatalf("seed last_seen: %v", err)
		}
		row, err := st.SetServerStatusAfterValidation(ctx, sqlc.SetServerStatusAfterValidationParams{
			ID:       pgUUID(created.ID),
			LastSeen: pgtype.Timestamptz{Time: cutoff, Valid: true},
		})
		if err != nil {
			t.Fatalf("SetServerStatusAfterValidation: %v", err)
		}
		return row.Status
	}

	if got := transition(t, nil); got != StatusPending {
		t.Errorf("never seen -> %q, want %q", got, StatusPending)
	}
	stale := service.now().Add(-heartbeatOfflineAfter - time.Minute)
	if got := transition(t, &stale); got != StatusOffline {
		t.Errorf("stale heartbeat -> %q, want %q", got, StatusOffline)
	}
	fresh := service.now().Add(-time.Second)
	if got := transition(t, &fresh); got != StatusReady {
		t.Errorf("fresh heartbeat -> %q, want %q", got, StatusReady)
	}
}

// TestServiceValidateRestoresStatusWhenAgentInfoWriteFails covers fix round 2
// U1: a failure to record the SSH inventory must not leave the node stuck in
// validating.
func TestServiceValidateRestoresStatusWhenAgentInfoWriteFails(t *testing.T) {
	service, st := newTestService(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	addr, _, stop := startSSHProbeServer(t, "unused", false)
	defer stop()
	host, port := target(t, addr)

	key, err := service.AddPrivateKey(ctx, "info-fail-key", string(testPrivateKeyPEM(t)))
	if err != nil {
		t.Fatalf("AddPrivateKey: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		if _, err := st.DB.Exec(cleanupCtx, "DELETE FROM private_keys WHERE id = $1", pgUUID(key.ID)); err != nil {
			t.Logf("cleanup delete private key: %v", err)
		}
	})

	created, err := service.Add(ctx, uuid.New(), "info-fail-node", host, port, "root", key.ID)
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		if err := st.DeleteServer(cleanupCtx, pgUUID(created.ID)); err != nil {
			t.Logf("cleanup delete server: %v", err)
		}
	})

	// The SSH probes succeed, but recording the inventory fails.
	st.BeforeUpdateServerAgentInfo = func() error { return errors.New("agent info write forced to fail") }
	t.Cleanup(func() { st.BeforeUpdateServerAgentInfo = nil })

	if _, err := service.Validate(ctx, created.ID, ValidateAuth{}); err == nil {
		t.Fatal("Validate reported success although the inventory write failed")
	}

	fetched, err := service.Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if fetched.Status == StatusValidating {
		t.Errorf("status = %q, want it restored, not stuck in validating", fetched.Status)
	}
	if fetched.Status != StatusPending {
		t.Errorf("status = %q, want %q (agentless node)", fetched.Status, StatusPending)
	}
}
