package deploy

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/teams"
)

// TestApplicationTeamIsolation is the Phase 8 exit criterion at the service
// layer: a member of one team can neither see nor mutate another team's
// applications, a read_only member reads but cannot mutate, and a user who
// belongs to both teams sees only the active team of each request.
func TestApplicationTeamIsolation(t *testing.T) {
	teamA, teamB := uuid.New(), uuid.New()
	alice, bob := uuid.New(), uuid.New()

	inTeamA := testApplication(alice)
	inTeamA.TeamID = teamA
	inTeamA.Name = "alice-app"
	inTeamB := testApplication(bob)
	inTeamB.TeamID = teamB
	inTeamB.Name = "bob-app"

	repo := &fakeRepository{app: inTeamA, apps: []Application{inTeamB}}
	svc := newTestService(t, repo)
	bg := context.Background()

	// A member of team A sees exactly their team's application.
	aliceCtx := teams.WithScope(bg, teams.Scope{UserID: alice, TeamID: teamA, Role: teams.RoleOwner})
	applications, err := svc.ListApplications(aliceCtx, alice, ApplicationFilter{})
	if err != nil {
		t.Fatalf("list team A: %v", err)
	}
	if len(applications) != 1 || applications[0].ID != inTeamA.ID {
		t.Fatalf("team A list = %+v, want only its own application", applications)
	}
	// The other team's application is not visible, let alone mutable.
	if _, err := svc.GetApplication(aliceCtx, alice, inTeamB.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-team Get = %v, want ErrNotFound", err)
	}
	if err := svc.DeleteApplication(aliceCtx, alice, inTeamB.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-team Delete = %v, want ErrNotFound", err)
	}
	if _, err := svc.Deploy(aliceCtx, alice, inTeamB.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-team Deploy = %v, want ErrNotFound", err)
	}
	if _, err := svc.GetEnv(aliceCtx, alice, inTeamB.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-team GetEnv = %v, want ErrNotFound", err)
	}

	// A read_only member of team B can read its application but not mutate it.
	viewerCtx := teams.WithScope(bg, teams.Scope{UserID: bob, TeamID: teamB, Role: teams.RoleReadOnly})
	if _, err := svc.GetApplication(viewerCtx, bob, inTeamB.ID); err != nil {
		t.Fatalf("read_only Get: %v", err)
	}
	if applications, err := svc.ListApplications(viewerCtx, bob, ApplicationFilter{}); err != nil || len(applications) != 1 {
		t.Fatalf("read_only list = %+v, %v", applications, err)
	}
	name := "renamed"
	if _, err := svc.UpdateApplication(viewerCtx, bob, inTeamB.ID, UpdateApplicationInput{Name: &name}); !errors.Is(err, teams.ErrForbidden) {
		t.Fatalf("read_only Update = %v, want ErrForbidden", err)
	}
	if _, err := svc.Deploy(viewerCtx, bob, inTeamB.ID); !errors.Is(err, teams.ErrForbidden) {
		t.Fatalf("read_only Deploy = %v, want ErrForbidden", err)
	}
	if err := svc.DeleteApplication(viewerCtx, bob, inTeamB.ID); !errors.Is(err, teams.ErrForbidden) {
		t.Fatalf("read_only Delete = %v, want ErrForbidden", err)
	}

	// An admin of team B may mutate.
	adminCtx := teams.WithScope(bg, teams.Scope{UserID: bob, TeamID: teamB, Role: teams.RoleAdmin})
	if _, err := svc.UpdateApplication(adminCtx, bob, inTeamB.ID, UpdateApplicationInput{Name: &name}); err != nil {
		t.Fatalf("admin Update: %v", err)
	}

	// The same user, active team A, sees only A's application again: the active
	// team decides per request.
	dualCtxA := teams.WithScope(bg, teams.Scope{UserID: bob, TeamID: teamA, Role: teams.RoleOwner})
	if applications, err := svc.ListApplications(dualCtxA, bob, ApplicationFilter{}); err != nil || len(applications) != 1 || applications[0].ID != inTeamA.ID {
		t.Fatalf("active team A list = %+v, %v; want team A's application", applications, err)
	}
	if _, err := svc.GetApplication(dualCtxA, bob, inTeamB.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("inactive team B Get = %v, want ErrNotFound", err)
	}
}

// TestApplicationServerMustBelongToTheActiveTeam is the F6 regression: a
// resource can only be bound to a node of the caller's active team (or a
// legacy node without a team). Creation, reassignment and deploy all refuse a
// foreign team's node as if it did not exist.
func TestApplicationServerMustBelongToTheActiveTeam(t *testing.T) {
	teamA, teamB := uuid.New(), uuid.New()
	alice := uuid.New()

	repo := &fakeRepository{}
	foreignNode := repo.seedServerForTeam(teamB)
	ownNode := repo.seedServerForTeam(teamA)
	legacyNode := repo.seedServerForTeam(uuid.Nil)
	svc := newTestService(t, repo)
	bg := context.Background()
	ctxA := teams.WithScope(bg, teams.Scope{UserID: alice, TeamID: teamA, Role: teams.RoleOwner})

	// A foreign team's node is not a valid target.
	if _, err := svc.CreateApplication(ctxA, alice, validCreateInput(foreignNode)); !errors.Is(err, ErrServerNotFound) {
		t.Fatalf("create on a foreign node = %v, want ErrServerNotFound", err)
	}
	// The team's own node and a legacy shared node are.
	ownInput := validCreateInput(ownNode)
	ownInput.Name = "own-node-app"
	own, err := svc.CreateApplication(ctxA, alice, ownInput)
	if err != nil {
		t.Fatalf("create on the team's node: %v", err)
	}
	legacyInput := validCreateInput(legacyNode)
	legacyInput.Name = "legacy-node-app"
	if _, err := svc.CreateApplication(ctxA, alice, legacyInput); err != nil {
		t.Fatalf("create on a legacy node: %v", err)
	}

	// Moving the application to a foreign node is refused too.
	if _, err := svc.UpdateApplication(ctxA, alice, own.ID, UpdateApplicationInput{ServerID: &foreignNode}); !errors.Is(err, ErrServerNotFound) {
		t.Fatalf("reassign to a foreign node = %v, want ErrServerNotFound", err)
	}

	// An application stored against a foreign node (pre-fix row) cannot be
	// deployed by a team that does not own that node.
	legacyApp := testApplication(alice)
	legacyApp.TeamID = teamA
	legacyApp.ServerID = foreignNode
	legacyApp.BaseDomain = ""
	repo.mu.Lock()
	repo.apps = append(repo.apps, legacyApp)
	repo.mu.Unlock()
	if _, err := svc.Deploy(ctxA, alice, legacyApp.ID); !errors.Is(err, ErrServerNotFound) {
		t.Fatalf("deploy targeting a foreign node = %v, want ErrServerNotFound", err)
	}
}
