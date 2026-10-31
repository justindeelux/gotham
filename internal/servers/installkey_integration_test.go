package servers

import (
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"golang.org/x/crypto/ssh"
)

func TestServiceInstallKeyFlow(t *testing.T) {
	service, st := newTestService(t)
	ctx := testContext(t)

	key, err := service.GeneratePrivateKey(ctx, "install-key-node", PrivateKeyTypeEd25519, "", 0)
	if err != nil {
		t.Fatalf("GeneratePrivateKey: %v", err)
	}
	t.Cleanup(func() { deletePrivateKeyRow(t, st, key.ID) })

	otherSigner, err := ssh.ParsePrivateKey(testPrivateKeyPEM(t))
	if err != nil {
		t.Fatalf("parse other key: %v", err)
	}
	sshServer := &sshProbeServer{
		password:       "root-password",
		authorizedKeys: []ssh.PublicKey{otherSigner.PublicKey()},
	}
	addr, fingerprint, stop := startSSHServer(t, sshServer)
	defer stop()
	host, port := target(t, addr)

	created, err := service.Add(ctx, uuid.New(), "install-key-node", host, port, "root", key.ID, "")
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	t.Cleanup(func() {
		if err := st.DeleteServer(ctx, pgUUID(created.ID)); err != nil {
			t.Logf("cleanup delete server: %v", err)
		}
	})

	result, err := service.Validate(ctx, created.ID, ValidateAuth{})
	if err == nil || !errors.Is(err, ErrValidation) {
		t.Fatalf("Validate with an untrusted key = %v, want ErrValidation", err)
	}
	if result == nil || !result.AuthFailed {
		t.Fatalf("result.AuthFailed = %v, want true", result)
	}

	if _, err := service.InstallKey(ctx, created.ID, "wrong-password"); !errors.Is(err, ErrValidation) {
		t.Fatalf("InstallKey with the wrong password = %v, want ErrValidation", err)
	} else if !strings.Contains(err.Error(), "password") {
		t.Errorf("wrong password error = %v, want it to name the rejected password", err)
	}

	result, err = service.InstallKey(ctx, created.ID, "root-password")
	if err != nil {
		t.Fatalf("InstallKey: %v", err)
	}
	if result.Server == nil || result.Server.Status == StatusError {
		t.Errorf("server after install = %+v, want a non-error status", result.Server)
	}
	for _, check := range result.Checks {
		if !check.OK {
			t.Errorf("check %q failed after install: %s", check.Name, check.Detail)
		}
	}

	fetched, err := service.Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if fetched.HasPassword {
		t.Error("the install password was persisted on the server")
	}
	if fetched.HostKeyFingerprint == nil || *fetched.HostKeyFingerprint != fingerprint {
		t.Errorf("host key fingerprint = %v, want %q", fetched.HostKeyFingerprint, fingerprint)
	}

	row, err := st.GetServerByID(ctx, pgUUID(created.ID))
	if err != nil {
		t.Fatalf("GetServerByID: %v", err)
	}
	if row.EncryptedPassword != nil {
		t.Error("encrypted_password is set after install, want nil")
	}

	if _, err := service.Validate(ctx, created.ID, ValidateAuth{}); err != nil {
		t.Fatalf("Validate with the key alone after install: %v", err)
	}
	if got := sshServer.authorizedKeyCount(); got != 2 {
		t.Errorf("authorized keys = %d, want the original plus the installed key", got)
	}
}
