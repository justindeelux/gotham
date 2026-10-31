package servers

import (
	"bytes"
	"context"
	"crypto/rsa"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/ssh"

	"github.com/justindeelux/gotham/internal/store"
	"github.com/justindeelux/gotham/internal/store/sqlc"
)

func TestGeneratePrivateKeyEachType(t *testing.T) {
	service, st := newTestService(t)
	ctx := testContext(t)

	tests := []struct {
		keyType  string
		wantBits int
	}{
		{keyType: PrivateKeyTypeEd25519},
		{keyType: PrivateKeyTypeRSA, wantBits: defaultRSABits},
		{keyType: PrivateKeyTypeECDSA},
	}
	for _, tt := range tests {
		t.Run(tt.keyType, func(t *testing.T) {
			key, err := service.GeneratePrivateKey(ctx, "generated-"+tt.keyType, tt.keyType, "generated in test", 0)
			if err != nil {
				t.Fatalf("GeneratePrivateKey: %v", err)
			}
			t.Cleanup(func() { deletePrivateKeyRow(t, st, key.ID) })

			if key.Type != tt.keyType {
				t.Errorf("type = %q, want %q", key.Type, tt.keyType)
			}
			if key.Description == nil || *key.Description != "generated in test" {
				t.Errorf("description = %v", key.Description)
			}

			publicKey, _, _, _, err := ssh.ParseAuthorizedKey([]byte(key.PublicKey))
			if err != nil {
				t.Fatalf("public_key %q does not parse: %v", key.PublicKey, err)
			}
			privatePEM, err := service.RevealPrivateKey(ctx, key.ID)
			if err != nil {
				t.Fatalf("RevealPrivateKey: %v", err)
			}
			signer, err := ssh.ParsePrivateKey([]byte(privatePEM))
			if err != nil {
				t.Fatalf("revealed private key does not parse: %v", err)
			}
			if !bytes.Equal(publicKey.Marshal(), signer.PublicKey().Marshal()) {
				t.Error("public_key does not match the revealed private key")
			}
			if tt.wantBits > 0 {
				cryptoKey, ok := publicKey.(ssh.CryptoPublicKey)
				if !ok {
					t.Fatalf("public key %T is not a crypto key", publicKey)
				}
				if bits := cryptoKey.CryptoPublicKey().(*rsa.PublicKey).N.BitLen(); bits != tt.wantBits {
					t.Errorf("rsa bits = %d, want %d", bits, tt.wantBits)
				}
			}
		})
	}

	if _, err := service.GeneratePrivateKey(ctx, "bad", "dsa", "", 0); !errors.Is(err, ErrValidation) {
		t.Errorf("unsupported type error = %v, want ErrValidation", err)
	}
	if _, err := service.GeneratePrivateKey(ctx, "bad", PrivateKeyTypeRSA, "", 512); !errors.Is(err, ErrValidation) {
		t.Errorf("small rsa error = %v, want ErrValidation", err)
	}
}

func TestGeneratedPrivateKeyValidatesNode(t *testing.T) {
	service, st := newTestService(t)
	ctx := testContext(t)

	key, err := service.GeneratePrivateKey(ctx, "generated-probe", PrivateKeyTypeEd25519, "", 0)
	if err != nil {
		t.Fatalf("GeneratePrivateKey: %v", err)
	}
	t.Cleanup(func() { deletePrivateKeyRow(t, st, key.ID) })

	authorizedKey, _, _, _, err := ssh.ParseAuthorizedKey([]byte(key.PublicKey))
	if err != nil {
		t.Fatalf("parse generated public key: %v", err)
	}
	addr, _, stop := startSSHServer(t, &sshProbeServer{authorizedKey: authorizedKey})
	defer stop()
	host, port := target(t, addr)

	created, err := service.Add(ctx, uuid.New(), "generated-key-node", host, port, "root", key.ID, "")
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := st.DeleteServer(cleanupCtx, pgUUID(created.ID)); err != nil {
			t.Logf("cleanup delete server: %v", err)
		}
	})

	result, err := service.Validate(ctx, created.ID, ValidateAuth{})
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if result == nil {
		t.Fatal("Validate returned no result")
	}
}

func TestAddPrivateKeyVerifiesPublicKey(t *testing.T) {
	service, st := newTestService(t)
	ctx := testContext(t)

	privatePEM := string(testPrivateKeyPEM(t))
	signer, err := ssh.ParsePrivateKey([]byte(privatePEM))
	if err != nil {
		t.Fatalf("parse test key: %v", err)
	}
	matching := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(signer.PublicKey())))
	other := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(publicKeyOf(t))))

	key, err := service.AddPrivateKey(ctx, "matching-public", privatePEM, "", matching)
	if err != nil {
		t.Fatalf("matching public key rejected: %v", err)
	}
	t.Cleanup(func() { deletePrivateKeyRow(t, st, key.ID) })
	if key.PublicKey != matching {
		t.Errorf("public_key = %q, want %q", key.PublicKey, matching)
	}

	_, err = service.AddPrivateKey(ctx, "mismatched-public", privatePEM, "", other)
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("mismatched public key error = %v, want ErrValidation", err)
	}
}

func TestListPrivateKeysBackfillsLegacyRows(t *testing.T) {
	service, st := newTestService(t)
	ctx := testContext(t)

	privatePEM := string(testPrivateKeyPEM(t))
	encrypted, err := EncryptKey(privatePEM, "gateway-test-secret")
	if err != nil {
		t.Fatalf("EncryptKey: %v", err)
	}
	legacy, err := st.CreatePrivateKey(ctx, sqlc.CreatePrivateKeyParams{
		Name:         "legacy-key",
		EncryptedKey: encrypted,
	})
	if err != nil {
		t.Fatalf("create legacy private key: %v", err)
	}
	t.Cleanup(func() { deletePrivateKeyRow(t, st, uuidFromPG(legacy.ID)) })

	keys, err := service.ListPrivateKeys(ctx)
	if err != nil {
		t.Fatalf("ListPrivateKeys: %v", err)
	}
	var found *PrivateKey
	for i := range keys {
		if keys[i].ID == uuidFromPG(legacy.ID) {
			found = &keys[i]
		}
	}
	if found == nil {
		t.Fatal("legacy key missing from the list")
	}
	if found.Type != PrivateKeyTypeEd25519 || found.PublicKey == "" {
		t.Errorf("legacy key metadata = %+v, want backfilled", found)
	}
	if strings.Contains(fmt.Sprintf("%+v", keys), "PRIVATE KEY") {
		t.Error("list response carries private key material")
	}

	row, err := st.GetPrivateKeyByID(ctx, legacy.ID)
	if err != nil {
		t.Fatalf("GetPrivateKeyByID: %v", err)
	}
	if row.Type == nil || *row.Type != PrivateKeyTypeEd25519 || row.PublicKey == nil || *row.PublicKey == "" {
		t.Errorf("stored metadata = %+v, want persisted backfill", row)
	}
}

func TestDeletePrivateKeyRefusesKeysInUse(t *testing.T) {
	service, st := newTestService(t)
	ctx := testContext(t)

	key, err := service.AddPrivateKey(ctx, "in-use-key", string(testPrivateKeyPEM(t)), "", "")
	if err != nil {
		t.Fatalf("AddPrivateKey: %v", err)
	}
	t.Cleanup(func() { deletePrivateKeyRow(t, st, key.ID) })

	created, err := service.Add(ctx, uuid.New(), "key-holder", "10.0.0.9", 22, "root", key.ID, "")
	if err != nil {
		t.Fatalf("Add: %v", err)
	}

	if err := service.DeletePrivateKey(ctx, key.ID); !errors.Is(err, ErrConflict) {
		t.Fatalf("delete while attached error = %v, want ErrConflict", err)
	}

	if err := st.DeleteServer(ctx, pgUUID(created.ID)); err != nil {
		t.Fatalf("delete server: %v", err)
	}
	if err := service.DeletePrivateKey(ctx, key.ID); err != nil {
		t.Fatalf("delete after detach: %v", err)
	}
	var count int64
	if err := st.DB.QueryRow(ctx, "SELECT count(*) FROM private_keys WHERE id = $1", pgUUID(key.ID)).Scan(&count); err != nil {
		t.Fatalf("count private keys: %v", err)
	}
	if count != 0 {
		t.Errorf("private key still stored after delete")
	}
}

func TestUpdatePrivateKeyProfile(t *testing.T) {
	service, st := newTestService(t)
	ctx := testContext(t)

	key, err := service.AddPrivateKey(ctx, "original", string(testPrivateKeyPEM(t)), "first", "")
	if err != nil {
		t.Fatalf("AddPrivateKey: %v", err)
	}
	t.Cleanup(func() { deletePrivateKeyRow(t, st, key.ID) })

	name := "renamed"
	description := "second"
	updated, err := service.UpdatePrivateKey(ctx, key.ID, PrivateKeyUpdateParams{Name: &name, Description: &description})
	if err != nil {
		t.Fatalf("UpdatePrivateKey: %v", err)
	}
	if updated.Name != name || updated.Description == nil || *updated.Description != description {
		t.Errorf("updated = %+v", updated)
	}

	empty := ""
	cleared, err := service.UpdatePrivateKey(ctx, key.ID, PrivateKeyUpdateParams{Description: &empty})
	if err != nil {
		t.Fatalf("clear description: %v", err)
	}
	if cleared.Description != nil {
		t.Errorf("description = %v, want nil", cleared.Description)
	}

	blank := "  "
	if _, err := service.UpdatePrivateKey(ctx, key.ID, PrivateKeyUpdateParams{Name: &blank}); !errors.Is(err, ErrValidation) {
		t.Errorf("blank name error = %v, want ErrValidation", err)
	}
	if _, err := service.UpdatePrivateKey(ctx, key.ID, PrivateKeyUpdateParams{}); !errors.Is(err, ErrValidation) {
		t.Errorf("empty patch error = %v, want ErrValidation", err)
	}
}

func TestPrivateKeyMethodsRejectUnknownID(t *testing.T) {
	service, _ := newTestService(t)
	ctx := testContext(t)
	unknown := uuid.New()

	if _, err := service.GetPrivateKey(ctx, unknown); !errors.Is(err, ErrNotFound) {
		t.Errorf("get error = %v, want ErrNotFound", err)
	}
	if _, err := service.RevealPrivateKey(ctx, unknown); !errors.Is(err, ErrNotFound) {
		t.Errorf("reveal error = %v, want ErrNotFound", err)
	}
	if err := service.DeletePrivateKey(ctx, unknown); !errors.Is(err, ErrNotFound) {
		t.Errorf("delete error = %v, want ErrNotFound", err)
	}
}

func testContext(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	t.Cleanup(cancel)
	return ctx
}

func publicKeyOf(t *testing.T) ssh.PublicKey {
	t.Helper()
	signer, err := ssh.ParsePrivateKey(testPrivateKeyPEM(t))
	if err != nil {
		t.Fatalf("parse test key: %v", err)
	}
	return signer.PublicKey()
}

func deletePrivateKeyRow(t *testing.T, st *store.Store, id uuid.UUID) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := st.DB.Exec(ctx, "DELETE FROM private_keys WHERE id = $1", pgUUID(id)); err != nil {
		t.Logf("cleanup delete private key: %v", err)
	}
}
