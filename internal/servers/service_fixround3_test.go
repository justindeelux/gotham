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

// TestLegacyAttachedKeyStillValidates covers the JUS-5 fix round 2 upgrade
// guarantee: a legacy (team_id NULL) key already attached to a team server
// still loads on the attached-key path, so the node validates under its own
// team's scope. An unrelated team that names the key explicitly still gets
// unknown-key. Removing the NULL-team skip (denying NULL keys outright, the
// pre-guarantee behavior) must fail this test.
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

	// No team scope: the stored row keeps team_id NULL, the legacy state.
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

	// A team server still pointing at the legacy key (the pre-teams layout).
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

	// The attached-key path loads the legacy key under the owning team's scope.
	if _, err := service.loadKeyAuth(aliceTeam, pgUUID(legacyKey.ID), "", false); err != nil {
		t.Fatalf("attached legacy key load = %v, want success", err)
	}

	// ... and the node validates over SSH with its stored key.
	result, err := service.Validate(aliceTeam, serverID, ValidateAuth{})
	if err != nil {
		t.Fatalf("Validate with the attached legacy key = %v, want success", err)
	}
	for _, check := range result.Checks {
		if !check.OK {
			t.Errorf("check %q not OK: %s", check.Name, check.Detail)
		}
	}

	// An unrelated team naming the key explicitly still gets unknown-key.
	if _, err := service.loadKeyAuth(bobPersonal, pgUUID(legacyKey.ID), "", true); !errors.Is(err, ErrValidation) {
		t.Errorf("unrelated-team explicit load = %v, want ErrValidation", err)
	}
}
