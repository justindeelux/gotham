package servers

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

// TestServiceLifecycleWithValidation exercises the full stack against the dev
// database and the in-process SSH server: key storage, server creation, SSH
// validation with the stored key, and CRUD.
func TestServiceLifecycleWithValidation(t *testing.T) {
	service, st := newTestService(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	key, err := service.AddPrivateKey(ctx, "deploy-key", string(testPrivateKeyPEM(t)))
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

	addr, hostFingerprint, stop := startSSHProbeServer(t, "unused", false)
	defer stop()
	host, port := target(t, addr)

	created, err := service.Add(ctx, uuid.New(), "edge-1", host, port, "root", key.ID)
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

	if created.Status != StatusPending {
		t.Errorf("status = %q, want %q", created.Status, StatusPending)
	}
	if created.SSHKeyID == nil || *created.SSHKeyID != key.ID {
		t.Errorf("SSHKeyID = %v, want %s", created.SSHKeyID, key.ID)
	}

	// Validation uses the stored (encrypted) key, which must round-trip
	// through AES-GCM decryption.
	result, err := service.Validate(ctx, created.ID, ValidateAuth{})
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if len(result.Checks) != 4 {
		t.Fatalf("checks = %d, want 4", len(result.Checks))
	}
	for _, check := range result.Checks {
		if !check.OK {
			t.Errorf("check %q failed: %s", check.Name, check.Detail)
		}
	}

	fetched, err := service.Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if fetched.Status != StatusReady {
		t.Errorf("status after validation = %q, want %q", fetched.Status, StatusReady)
	}
	if fetched.DockerVersion == nil || *fetched.DockerVersion != "24.0.7" {
		t.Errorf("DockerVersion = %v, want 24.0.7", fetched.DockerVersion)
	}
	if fetched.TotalMem == nil || *fetched.TotalMem != int64(8192000)*1024 {
		t.Errorf("TotalMem = %v, want 8192000 KiB", fetched.TotalMem)
	}
	// The first successful validation pins the node's host key; a later
	// validation must present the same key.
	if fetched.HostKeyFingerprint == nil || *fetched.HostKeyFingerprint != hostFingerprint {
		t.Errorf("HostKeyFingerprint = %v, want the pinned %q", fetched.HostKeyFingerprint, hostFingerprint)
	}
	// A second validation against the pin succeeds.
	if _, err := service.Validate(ctx, created.ID, ValidateAuth{}); err != nil {
		t.Fatalf("Validate with pinned host key: %v", err)
	}
	// The operator reset forgets the pin so a rotated key can be re-pinned.
	reset, err := service.ResetHostKey(ctx, created.ID)
	if err != nil {
		t.Fatalf("ResetHostKey: %v", err)
	}
	if reset.HostKeyFingerprint != nil {
		t.Errorf("HostKeyFingerprint after reset = %v, want nil", reset.HostKeyFingerprint)
	}
	if _, err := service.Validate(ctx, created.ID, ValidateAuth{}); err != nil {
		t.Fatalf("Validate after reset: %v", err)
	}

	list, err := service.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	found := false
	for _, item := range list {
		if item.ID == created.ID {
			found = true
			break
		}
	}
	if !found {
		t.Error("List did not include the created server")
	}

	if err := service.Delete(ctx, created.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := service.Get(ctx, created.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get after Delete = %v, want ErrNotFound", err)
	}
}

// TestServiceAddValidation checks the create-input guards without a database
// round trip beyond the key lookup.
func TestServiceAddValidation(t *testing.T) {
	service, _ := newTestService(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if _, err := service.Add(ctx, uuid.New(), "", "10.0.0.1", 22, "root", uuid.Nil); !errors.Is(err, ErrValidation) {
		t.Errorf("empty name = %v, want ErrValidation", err)
	}
	if _, err := service.Add(ctx, uuid.New(), "n", "", 22, "root", uuid.Nil); !errors.Is(err, ErrValidation) {
		t.Errorf("empty ip = %v, want ErrValidation", err)
	}
	if _, err := service.Add(ctx, uuid.New(), "n", "10.0.0.1", 22, "", uuid.Nil); !errors.Is(err, ErrValidation) {
		t.Errorf("empty user = %v, want ErrValidation", err)
	}
	if _, err := service.Add(ctx, uuid.New(), "n", "10.0.0.1", 70000, "root", uuid.Nil); !errors.Is(err, ErrValidation) {
		t.Errorf("bad port = %v, want ErrValidation", err)
	}
	if _, err := service.Add(ctx, uuid.New(), "n", "10.0.0.1", 22, "root", uuid.New()); !errors.Is(err, ErrValidation) {
		t.Errorf("unknown ssh_key_id = %v, want ErrValidation", err)
	}
}

// TestServicePasswordAuthRequiresHostKeyTrust proves password auth to an
// unpinned node is refused unless the operator explicitly trusts the host key,
// and that the explicit trust pins the key.
func TestServicePasswordAuthRequiresHostKeyTrust(t *testing.T) {
	service, st := newTestService(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	addr, fingerprint, stop := startSSHProbeServer(t, "hunter2", false)
	defer stop()
	host, port := target(t, addr)

	created, err := service.Add(ctx, uuid.New(), "pw-node", host, port, "root", uuid.Nil)
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

	// Without explicit trust, an unpinned host is refused before any password
	// is sent.
	if _, err := service.Validate(ctx, created.ID, ValidateAuth{Password: "hunter2"}); err == nil {
		t.Fatal("password validation trusted an unpinned host key")
	} else if !strings.Contains(err.Error(), "not pinned") {
		t.Errorf("err = %v, want it to report the host is not pinned", err)
	}

	// The operator's explicit trust accepts and pins the key.
	if _, err := service.Validate(ctx, created.ID, ValidateAuth{Password: "hunter2", TrustHostKey: true}); err != nil {
		t.Fatalf("Validate with trust_host_key: %v", err)
	}
	fetched, err := service.Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if fetched.HostKeyFingerprint == nil || *fetched.HostKeyFingerprint != fingerprint {
		t.Errorf("HostKeyFingerprint = %v, want %q", fetched.HostKeyFingerprint, fingerprint)
	}
}

// TestServiceValidateMissingServer checks that validating an unknown server
// returns ErrNotFound rather than an SSH error.
func TestServiceValidateMissingServer(t *testing.T) {
	service, _ := newTestService(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if _, err := service.Validate(ctx, uuid.New(), ValidateAuth{Password: "x"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Validate(unknown) = %v, want ErrNotFound", err)
	}
}
