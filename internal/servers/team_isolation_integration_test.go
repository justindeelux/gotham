package servers

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/store/sqlc"
	"github.com/justindeelux/gotham/internal/teams"
)

// TestServerTeamIsolation is the Phase 8 exit criterion for the node registry,
// which has no owner column: a node stamped with a team is only visible to that
// team, while a legacy node (team_id NULL) stays visible to every authenticated
// caller. It runs against the dev database and skips when none is available.
func TestServerTeamIsolation(t *testing.T) {
	service, st := newTestService(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	suffix := time.Now().UnixNano()
	alice, err := st.CreateUser(ctx, fmt.Sprintf("be-8.2-a-%d@example.com", suffix), nil)
	if err != nil {
		t.Fatalf("create user A: %v", err)
	}
	bob, err := st.CreateUser(ctx, fmt.Sprintf("be-8.2-b-%d@example.com", suffix), nil)
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
		Name: fmt.Sprintf("shared-%d", suffix),
	}, alice.ID)
	if err != nil {
		t.Fatalf("create shared team: %v", err)
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

	sharedNode, err := service.Add(aliceTeam, uuid.UUID(alice.ID.Bytes), "shared-node", "127.0.0.1", 22, "root", uuid.Nil)
	if err != nil {
		t.Fatalf("add shared node: %v", err)
	}
	bobNode, err := service.Add(bobPersonal, uuid.UUID(bob.ID.Bytes), "bob-node", "127.0.0.1", 22, "root", uuid.Nil)
	if err != nil {
		t.Fatalf("add bob node: %v", err)
	}
	legacyNode, err := service.Add(ctx, uuid.UUID(alice.ID.Bytes), "legacy-node", "127.0.0.1", 22, "root", uuid.Nil)
	if err != nil {
		t.Fatalf("add legacy node: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		for _, id := range []uuid.UUID{sharedNode.ID, bobNode.ID, legacyNode.ID} {
			if err := st.DeleteServer(cleanupCtx, pgUUID(id)); err != nil {
				t.Logf("cleanup server %s: %v", id, err)
			}
		}
	})

	if sharedNode.TeamID != uuid.UUID(shared.ID.Bytes) {
		t.Fatalf("shared node team = %s, want %s", sharedNode.TeamID, uuid.UUID(shared.ID.Bytes))
	}
	if legacyNode.TeamID != uuid.Nil {
		t.Fatalf("legacy node team = %s, want unset", legacyNode.TeamID)
	}

	// The active team decides what the list returns: team nodes of the active
	// team plus every legacy node.
	listFor := func(scope teams.Scope) map[uuid.UUID]bool {
		t.Helper()
		rows, err := service.List(teams.WithScope(context.Background(), scope))
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		seen := map[uuid.UUID]bool{}
		for _, row := range rows {
			seen[row.ID] = true
		}
		return seen
	}
	aliceSees := listFor(teams.Scope{UserID: uuid.UUID(alice.ID.Bytes), TeamID: uuid.UUID(shared.ID.Bytes), Role: teams.RoleOwner})
	if !aliceSees[sharedNode.ID] || !aliceSees[legacyNode.ID] {
		t.Errorf("shared-team list = %v, want the team node and the legacy node", aliceSees)
	}
	if aliceSees[bobNode.ID] {
		t.Error("shared-team list leaked another team's node")
	}
	bobSees := listFor(teams.Scope{UserID: uuid.UUID(bob.ID.Bytes), TeamID: teams.PersonalTeamID(uuid.UUID(bob.ID.Bytes)), Role: teams.RoleOwner})
	if !bobSees[bobNode.ID] || !bobSees[legacyNode.ID] || bobSees[sharedNode.ID] {
		t.Errorf("bob's list = %v, want his node and the legacy node only", bobSees)
	}

	// Detail and delete follow the same rule; the legacy node stays reachable.
	if _, err := service.Get(aliceTeam, bobNode.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("cross-team Get = %v, want ErrNotFound", err)
	}
	if err := service.Delete(aliceTeam, bobNode.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("cross-team Delete = %v, want ErrNotFound", err)
	}
	if _, err := service.Get(aliceTeam, legacyNode.ID); err != nil {
		t.Errorf("legacy Get = %v, want the shared pre-teams behavior", err)
	}

	// A read_only member reads the team's node but cannot delete it.
	if _, err := st.CreateTeamMember(ctx, sqlc.CreateTeamMemberParams{
		TeamID: shared.ID,
		UserID: bob.ID,
		Role:   string(teams.RoleReadOnly),
	}); err != nil {
		t.Fatalf("add read_only member: %v", err)
	}
	viewer := teams.Scope{UserID: uuid.UUID(bob.ID.Bytes), TeamID: uuid.UUID(shared.ID.Bytes), Role: teams.RoleReadOnly}
	if _, err := service.Get(teams.WithScope(ctx, viewer), sharedNode.ID); err != nil {
		t.Errorf("read_only Get: %v", err)
	}
	if err := service.Delete(teams.WithScope(ctx, viewer), sharedNode.ID); !errors.Is(err, teams.ErrForbidden) {
		t.Errorf("read_only Delete = %v, want ErrForbidden", err)
	}
}
