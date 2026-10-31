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
	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
)

func validatedReadyNode(t *testing.T, service *ServerService, ctx context.Context, name string) (*Server, string) {
	t.Helper()

	key, err := service.AddPrivateKey(ctx, name+"-key", string(testPrivateKeyPEM(t)), "", "")
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

func TestUpdateRejectsKeyAndPassword(t *testing.T) {
	service, _ := newTestService(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	key, err := service.AddPrivateKey(ctx, "both-key", string(testPrivateKeyPEM(t)), "", "")
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

func TestValidateAfterPatchRace(t *testing.T) {
	service, st := newTestService(t)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	key, err := service.AddPrivateKey(ctx, "race-key", string(testPrivateKeyPEM(t)), "", "")
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
