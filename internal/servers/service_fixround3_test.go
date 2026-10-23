package servers

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/justindeelux/gotham/internal/store/sqlc"
	"github.com/justindeelux/gotham/internal/teams"
)

func TestLegacyAttachedKeyStillValidates(t *testing.T) {
	service, st := newTestService(t)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	addr, _, stop := startSSHProbeServer(t, "", false)
	defer stop()
	host, port := target(t, addr)

	suffix := time.Now().UnixNano()
	alice, err := st.CreateUser(ctx, fmt.Sprintf("be-r3-a-%d@example.com", suffix), nil)
	if err != nil {
		t.Fatalf("create user A: %v", err)
	}
	bob, err := st.CreateUser(ctx, fmt.Sprintf("be-r3-b-%d@example.com", suffix), nil)
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
		Name: fmt.Sprintf("legacy-team-%d", suffix),
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
		`INSERT INTO servers (name, ip, port, ssh_user, ssh_key_id, team_id)
		 VALUES ('legacy-node', $1, $2, 'root', $3, $4) RETURNING id`,
		host, port, pgUUID(legacyKey.ID), shared.ID).Scan(&serverRow); err != nil {
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

	if _, err := service.loadKeyAuth(aliceTeam, pgUUID(legacyKey.ID), "", false); err != nil {
		t.Fatalf("attached legacy key load = %v, want success", err)
	}

	result, err := service.Validate(aliceTeam, serverID, ValidateAuth{})
	if err != nil {
		t.Fatalf("Validate with the attached legacy key = %v, want success", err)
	}
	for _, check := range result.Checks {
		if !check.OK {
			t.Errorf("check %q not OK: %s", check.Name, check.Detail)
		}
	}

	if _, err := service.loadKeyAuth(bobPersonal, pgUUID(legacyKey.ID), "", true); !errors.Is(err, ErrValidation) {
		t.Errorf("unrelated-team explicit load = %v, want ErrValidation", err)
	}
}
