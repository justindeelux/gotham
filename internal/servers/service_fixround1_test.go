package servers

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/store/sqlc"
	"github.com/justindeelux/gotham/internal/teams"
	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
)

// validatedReadyNode creates a key-authenticated node, validates it against
// the in-process SSH probe (pinning the host key), attaches nodeID, and
// heartbeats it to ready. It returns the server and the probe fingerprint.
func validatedReadyNode(t *testing.T, service *ServerService, ctx context.Context, name string) (*Server, string) {
	t.Helper()

	key, err := service.AddPrivateKey(ctx, name+"-key", string(testPrivateKeyPEM(t)))
	if err != nil {
		t.Fatalf("AddPrivateKey: %v", err)
	}

	addr, hostFingerprint, stop := startSSHProbeServer(t, "", false)
	defer stop()
	host, port := target(t, addr)

	created, err := service.Add(ctx, uuid.New(), name, host, port, "root", key.ID, "")
	if err != nil {
		t.Fatalf("Add: %v", err)
	}

	if _, err := service.Validate(ctx, created.ID, ValidateAuth{}); err != nil {
		t.Fatalf("Validate: %v", err)
	}

	nodeID := uniqueNodeID(name)
	if _, err := service.store.UpdateServerAgentInfo(ctx, sqlc.UpdateServerAgentInfoParams{
		ID:     pgUUID(created.ID),
		NodeID: &nodeID,
	}); err != nil {
		t.Fatalf("attach node id: %v", err)
	}
	if err := service.RecordHeartbeat(ctx, nodeID, &agentv1.HeartbeatRequest{CpuUsage: 0.1}); err != nil {
		t.Fatalf("RecordHeartbeat: %v", err)
	}

	ready, err := service.Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if ready.Status != StatusReady {
		t.Fatalf("status = %q, want %q", ready.Status, StatusReady)
	}
	if ready.HostKeyFingerprint == nil || *ready.HostKeyFingerprint != hostFingerprint {
		t.Fatalf("HostKeyFingerprint = %v, want %q", ready.HostKeyFingerprint, hostFingerprint)
	}
	return ready, hostFingerprint
}

// TestUpdateRejectsKeyAndPassword covers defect 3a: PATCH with both
// ssh_key_id and a password is a 400 like create, and must not echo the
// secret.
func TestUpdateRejectsKeyAndPassword(t *testing.T) {
	service, _ := newTestService(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	key, err := service.AddPrivateKey(ctx, "both-key", string(testPrivateKeyPEM(t)))
	if err != nil {
		t.Fatalf("AddPrivateKey: %v", err)
	}

	created, err := service.Add(ctx, uuid.New(), "both-node", "127.0.0.1", 22, "root", uuid.Nil, "")
	if err != nil {
		t.Fatalf("Add: %v", err)
	}

	keyID := key.ID
	params := UpdateParams{SSHKeyID: &keyID, Password: strPtr("s3cret")}
	if _, err := service.Update(ctx, created.ID, params); !errors.Is(err, ErrValidation) {
		t.Errorf("Update with key+password = %v, want ErrValidation", err)
	}
}

// TestUpdateNameOnlyKeepsStatusAndPin covers defects 2 and 3b: a name-only
// PATCH must not touch status or the pin.
func TestUpdateNameOnlyKeepsStatusAndPin(t *testing.T) {
	service, _ := newTestService(t)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	ready, fingerprint := validatedReadyNode(t, service, ctx, "name-only")
	renamed, err := service.Update(ctx, ready.ID, UpdateParams{Name: strPtr("name-only-renamed")})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if renamed.Name != "name-only-renamed" {
		t.Errorf("Name = %q, want the rename applied", renamed.Name)
	}
	if renamed.Status != StatusReady {
		t.Errorf("Status = %q, want %q untouched", renamed.Status, StatusReady)
	}
	if renamed.HostKeyFingerprint == nil || *renamed.HostKeyFingerprint != fingerprint {
		t.Errorf("HostKeyFingerprint = %v, want the pin %q untouched", renamed.HostKeyFingerprint, fingerprint)
	}
}

// TestUpdateUnchangedValuesNoReset covers defect 3b: re-sending the stored
// address or the attached key (or the stored password) must not reset the pin
// or status, while a real change must.
func TestUpdateUnchangedValuesNoReset(t *testing.T) {
	service, _ := newTestService(t)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	ready, fingerprint := validatedReadyNode(t, service, ctx, "unchanged")
	sameIP := ready.IP
	samePort := ready.Port
	sameUser := ready.SSHUser
	sameKey := *ready.SSHKeyID

	unchanged, err := service.Update(ctx, ready.ID, UpdateParams{
		IP: &sameIP, Port: &samePort, SSHUser: &sameUser, SSHKeyID: &sameKey,
	})
	if err != nil {
		t.Fatalf("Update with identical values: %v", err)
	}
	if unchanged.Status != StatusReady {
		t.Errorf("Status = %q, want %q (identical re-send must not reset)", unchanged.Status, StatusReady)
	}
	if unchanged.HostKeyFingerprint == nil || *unchanged.HostKeyFingerprint != fingerprint {
		t.Errorf("HostKeyFingerprint = %v, want the pin %q kept", unchanged.HostKeyFingerprint, fingerprint)
	}

	// Detaching the key IS a real credential change: it resets.
	detached, err := service.Update(ctx, ready.ID, UpdateParams{SSHKeyID: &uuid.Nil})
	if err != nil {
		t.Fatalf("Update detach key: %v", err)
	}
	if detached.Status != StatusPending {
		t.Errorf("Status after detach = %q, want %q", detached.Status, StatusPending)
	}
	if detached.HostKeyFingerprint != nil {
		t.Errorf("HostKeyFingerprint after detach = %v, want nil", detached.HostKeyFingerprint)
	}
}

// TestUpdateIdenticalPasswordNoReset covers defect 3b for secrets: re-sending
// the stored password is not a change, while a new one resets.
func TestUpdateIdenticalPasswordNoReset(t *testing.T) {
	service, _ := newTestService(t)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	addr, hostFingerprint, stop := startSSHProbeServer(t, "pw1", false)
	defer stop()
	host, port := target(t, addr)

	created, err := service.Add(ctx, uuid.New(), "pw-node", host, port, "root", uuid.Nil, "pw1")
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if _, err := service.Validate(ctx, created.ID, ValidateAuth{TrustHostKey: true}); err != nil {
		t.Fatalf("Validate with stored password: %v", err)
	}

	nodeID := uniqueNodeID("pw-node")
	if _, err := service.store.UpdateServerAgentInfo(ctx, sqlc.UpdateServerAgentInfoParams{
		ID:     pgUUID(created.ID),
		NodeID: &nodeID,
	}); err != nil {
		t.Fatalf("attach node id: %v", err)
	}
	if err := service.RecordHeartbeat(ctx, nodeID, &agentv1.HeartbeatRequest{CpuUsage: 0.1}); err != nil {
		t.Fatalf("RecordHeartbeat: %v", err)
	}

	same, err := service.Update(ctx, created.ID, UpdateParams{Password: strPtr("pw1")})
	if err != nil {
		t.Fatalf("Update with identical password: %v", err)
	}
	if same.Status != StatusReady {
		t.Errorf("Status = %q, want %q (identical password must not reset)", same.Status, StatusReady)
	}
	if same.HostKeyFingerprint == nil || *same.HostKeyFingerprint != hostFingerprint {
		t.Errorf("HostKeyFingerprint = %v, want the pin %q kept", same.HostKeyFingerprint, hostFingerprint)
	}

	rotated, err := service.Update(ctx, created.ID, UpdateParams{Password: strPtr("pw2")})
	if err != nil {
		t.Fatalf("Update with a new password: %v", err)
	}
	if rotated.Status != StatusPending {
		t.Errorf("Status after rotation = %q, want %q", rotated.Status, StatusPending)
	}
	if rotated.HostKeyFingerprint != nil {
		t.Errorf("HostKeyFingerprint after rotation = %v, want nil", rotated.HostKeyFingerprint)
	}
}

// TestUpdateVsHeartbeatNoRevert covers defect 2: a PATCH racing a heartbeat
// must not revert the heartbeat's status write, and a name-only PATCH must
// never move a ready node out of ready.
func TestUpdateVsHeartbeatNoRevert(t *testing.T) {
	service, _ := newTestService(t)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	ready, _ := validatedReadyNode(t, service, ctx, "hb-race")

	nodeID := ""
	if ready.NodeID != nil {
		nodeID = *ready.NodeID
	}
	heartbeat := func() error {
		return service.RecordHeartbeat(ctx, nodeID, &agentv1.HeartbeatRequest{CpuUsage: 0.3})
	}

	// Sequential first: heartbeat, then a name-only PATCH, must stay ready.
	if err := heartbeat(); err != nil {
		t.Fatalf("RecordHeartbeat: %v", err)
	}
	renamed, err := service.Update(ctx, ready.ID, UpdateParams{Name: strPtr("hb-race-renamed")})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if renamed.Status != StatusReady {
		t.Fatalf("Status after name PATCH = %q, want %q", renamed.Status, StatusReady)
	}

	// Concurrent: neither writer moves the node out of ready, so every
	// interleaving must end ready with a fresh heartbeat.
	var wg sync.WaitGroup
	errs := make(chan error, 32)
	for i := 0; i < 8; i++ {
		wg.Add(2)
		go func(n int) {
			defer wg.Done()
			name := fmt.Sprintf("hb-race-%d", n)
			if _, err := service.Update(ctx, ready.ID, UpdateParams{Name: &name}); err != nil {
				errs <- err
			}
		}(i)
		go func() {
			defer wg.Done()
			if err := heartbeat(); err != nil {
				errs <- err
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("concurrent PATCH/heartbeat: %v", err)
	}

	final, err := service.Get(ctx, ready.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if final.Status != StatusReady {
		t.Errorf("Status after concurrent PATCH+heartbeat = %q, want %q", final.Status, StatusReady)
	}
	if final.LastSeen == nil || time.Since(*final.LastSeen) > heartbeatOfflineAfter {
		t.Errorf("LastSeen = %v, want a fresh heartbeat", final.LastSeen)
	}
}

// TestValidateAfterPatchRace covers defect 1: when a PATCH moves the node to
// a new address while a validation against the OLD address is in flight, the
// stale validation must neither pin the old host's key onto the new address
// nor overwrite the new pending status.
func TestValidateAfterPatchRace(t *testing.T) {
	service, st := newTestService(t)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	key, err := service.AddPrivateKey(ctx, "race-key", string(testPrivateKeyPEM(t)))
	if err != nil {
		t.Fatalf("AddPrivateKey: %v", err)
	}

	addr, _, stop := startSSHProbeServer(t, "", false)
	defer stop()
	host, port := target(t, addr)

	created, err := service.Add(ctx, uuid.New(), "race-node", host, port, "root", key.ID, "")
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		if _, err := st.DB.Exec(cleanupCtx, "DELETE FROM private_keys WHERE id = $1", pgUUID(key.ID)); err != nil {
			t.Logf("cleanup delete private key: %v", err)
		}
		if err := st.DeleteServer(cleanupCtx, pgUUID(created.ID)); err != nil {
			t.Logf("cleanup delete server: %v", err)
		}
	})

	// Land a PATCH to a new address after the SSH dial succeeds but before
	// the pin write: the seam runs inside the guarded pin write.
	newIP := "203.0.113.99"
	st.BeforePinServerHostKey = func() error {
		_, err := service.Update(ctx, created.ID, UpdateParams{IP: &newIP})
		return err
	}
	defer func() { st.BeforePinServerHostKey = nil }()

	result, err := service.Validate(ctx, created.ID, ValidateAuth{})
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("Validate = (%v, %v), want an ErrValidation for the stale run", result, err)
	}

	fresh, err := service.Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if fresh.IP != newIP {
		t.Fatalf("IP = %q, want the PATCHed %q", fresh.IP, newIP)
	}
	if fresh.HostKeyFingerprint != nil {
		t.Errorf("HostKeyFingerprint = %q, want nil (stale pin must be dropped)", *fresh.HostKeyFingerprint)
	}
	if fresh.Status != StatusPending {
		t.Errorf("Status = %q, want %q (stale success must not overwrite)", fresh.Status, StatusPending)
	}
}

// TestCrossTeamKeyAttach covers defect 4a: a key from another team answers
// unknown-key on Add and Update, like a missing key.
func TestCrossTeamKeyAttach(t *testing.T) {
	service, st := newTestService(t)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	suffix := time.Now().UnixNano()
	alice, err := st.CreateUser(ctx, fmt.Sprintf("be-4a-a-%d@example.com", suffix), nil)
	if err != nil {
		t.Fatalf("create user A: %v", err)
	}
	bob, err := st.CreateUser(ctx, fmt.Sprintf("be-4a-b-%d@example.com", suffix), nil)
	if err != nil {
		t.Fatalf("create user B: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		if _, err := st.DB.Exec(cleanupCtx, "DELETE FROM users WHERE id = ANY($1)", []any{alice.ID, bob.ID}); err != nil {
			t.Logf("cleanup users: %v", err)
		}
	})

	shared, err := st.CreateTeamWithOwner(ctx, sqlc.CreateTeamParams{
		ID:   pgUUID(uuid.New()),
		Name: fmt.Sprintf("key-team-%d", suffix),
	}, alice.ID)
	if err != nil {
		t.Fatalf("create team: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		if err := st.DeleteTeam(cleanupCtx, shared.ID); err != nil {
			t.Logf("cleanup team: %v", err)
		}
	})

	aliceTeam := teams.WithScope(ctx, teams.Scope{UserID: uuid.UUID(alice.ID.Bytes), TeamID: uuid.UUID(shared.ID.Bytes), Role: teams.RoleOwner})
	bobPersonal := teams.WithScope(ctx, teams.Scope{UserID: uuid.UUID(bob.ID.Bytes), TeamID: teams.PersonalTeamID(uuid.UUID(bob.ID.Bytes)), Role: teams.RoleOwner})

	teamKey, err := service.AddPrivateKey(aliceTeam, "team-key", string(testPrivateKeyPEM(t)))
	if err != nil {
		t.Fatalf("AddPrivateKey: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		if _, err := st.DB.Exec(cleanupCtx, "DELETE FROM private_keys WHERE id = $1", pgUUID(teamKey.ID)); err != nil {
			t.Logf("cleanup delete private key: %v", err)
		}
	})

	// Another team's key is not attachable on Add: same error as a missing key.
	if _, err := service.Add(bobPersonal, uuid.UUID(bob.ID.Bytes), "bob-node", "127.0.0.1", 22, "root", teamKey.ID, ""); !errors.Is(err, ErrValidation) {
		t.Errorf("cross-team Add = %v, want ErrValidation", err)
	}
	if _, err := service.Add(bobPersonal, uuid.UUID(bob.ID.Bytes), "bob-node", "127.0.0.1", 22, "root", uuid.New(), ""); !errors.Is(err, ErrValidation) {
		t.Errorf("missing-key Add = %v, want ErrValidation", err)
	}

	// ... nor on Update.
	bobNode, err := service.Add(bobPersonal, uuid.UUID(bob.ID.Bytes), "bob-node", "127.0.0.1", 22, "root", uuid.Nil, "")
	if err != nil {
		t.Fatalf("Add bob node: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		if err := st.DeleteServer(cleanupCtx, pgUUID(bobNode.ID)); err != nil {
			t.Logf("cleanup delete server: %v", err)
		}
	})
	if _, err := service.Update(bobPersonal, bobNode.ID, UpdateParams{SSHKeyID: &teamKey.ID}); !errors.Is(err, ErrValidation) {
		t.Errorf("cross-team Update = %v, want ErrValidation", err)
	}

	// The owning team can attach it on both paths.
	aliceNode, err := service.Add(aliceTeam, uuid.UUID(alice.ID.Bytes), "alice-node", "127.0.0.1", 22, "root", teamKey.ID, "")
	if err != nil {
		t.Fatalf("own-team Add with the key: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		if err := st.DeleteServer(cleanupCtx, pgUUID(aliceNode.ID)); err != nil {
			t.Logf("cleanup delete server: %v", err)
		}
	})
	if _, err := service.Update(aliceTeam, aliceNode.ID, UpdateParams{SSHKeyID: &teamKey.ID}); err != nil {
		t.Errorf("own-team Update re-sending the key: %v", err)
	}

	// A legacy key (team_id NULL, created without a team scope) stays usable
	// by every caller, mirroring legacy shared nodes.
	legacyKey, err := service.AddPrivateKey(ctx, "legacy-key", string(testPrivateKeyPEM(t)))
	if err != nil {
		t.Fatalf("AddPrivateKey legacy: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		if _, err := st.DB.Exec(cleanupCtx, "DELETE FROM private_keys WHERE id = $1", pgUUID(legacyKey.ID)); err != nil {
			t.Logf("cleanup delete private key: %v", err)
		}
	})
	if _, err := service.Update(bobPersonal, bobNode.ID, UpdateParams{SSHKeyID: &legacyKey.ID}); err != nil {
		t.Errorf("legacy-key Update = %v, want success", err)
	}
}
