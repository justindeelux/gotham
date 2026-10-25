package servers

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
)

func TestLegacyAttachedKeyStillValidates(t *testing.T) {
	service, st := newTestService(t)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	addr, _, stop := startSSHProbeServer(t, "", false)
	defer stop()
	host, port := target(t, addr)

	suffix := time.Now().UnixNano()
	legacyKey, err := service.AddPrivateKey(ctx, fmt.Sprintf("r3-legacy-%d", suffix), string(testPrivateKeyPEM(t)))
	if err != nil {
		t.Fatalf("AddPrivateKey legacy: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		if _, err := st.DB.Exec(cleanupCtx, "DELETE FROM private_keys WHERE id = $1", pgUUID(legacyKey.ID)); err != nil {
			t.Logf("cleanup delete private key: %v", err)
		}
	})

	var serverRow pgtype.UUID
	if err := st.DB.QueryRow(ctx,
		`INSERT INTO servers (name, ip, port, ssh_user, ssh_key_id)
		 VALUES ('legacy-node', $1, $2, 'root', $3) RETURNING id`,
		host, port, pgUUID(legacyKey.ID)).Scan(&serverRow); err != nil {
		t.Fatalf("seed legacy server: %v", err)
	}
	serverID := uuidFromPG(serverRow)
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		if err := st.DeleteServer(cleanupCtx, pgUUID(serverID)); err != nil {
			t.Logf("cleanup delete server: %v", err)
		}
	})

	if _, err := service.loadKeyAuth(ctx, pgUUID(legacyKey.ID), "", false); err != nil {
		t.Fatalf("attached legacy key load = %v, want success", err)
	}

	result, err := service.Validate(ctx, serverID, ValidateAuth{})
	if err != nil {
		t.Fatalf("Validate with the attached legacy key = %v, want success", err)
	}
	for _, check := range result.Checks {
		if !check.OK {
			t.Errorf("check %q not OK: %s", check.Name, check.Detail)
		}
	}
}
