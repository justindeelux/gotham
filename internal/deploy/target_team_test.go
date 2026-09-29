package deploy

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/teams"
)

// TestRollbackRefusesApplicationBoundToForeignNode is the R1 regression: the
// stored application→node check runs at the shared queue boundary, so a
// rollback cannot queue a job for an application whose stored team does not own
// its node (the deploy path already refused it).
func TestRollbackRefusesApplicationBoundToForeignNode(t *testing.T) {
	teamA, teamB := uuid.New(), uuid.New()
	alice := uuid.New()

	repo := &fakeRepository{}
	foreignNode := repo.seedServerForTeam(teamB)
	app := testApplication(alice)
	app.TeamID = teamA
	app.ServerID = foreignNode
	app.BaseDomain = ""
	repo.app = app
	release := Deployment{
		ID:            uuid.New(),
		ApplicationID: app.ID,
		Kind:          KindDeploy,
		State:         StateRunning,
		ImageTag:      "nginx:alpine",
		RegistryImage: "nginx:alpine",
	}
	repo.deployments = []Deployment{release}
	svc := newTestService(t, repo)
	ctxA := teams.WithScope(context.Background(), teams.Scope{UserID: alice, TeamID: teamA, Role: teams.RoleOwner})

	if _, err := svc.Rollback(ctxA, alice, app.ID, release.ID); !errors.Is(err, ErrServerNotFound) {
		t.Fatalf("cross-team rollback = %v, want ErrServerNotFound", err)
	}
	// Nothing was queued: the seeded release is the only deployment row.
	if queued := listStored(t, repo, app.ID); len(queued) != 1 {
		t.Fatalf("deployments after refused rollback = %d, want only the seeded release", len(queued))
	}
}

// TestSystemDeployRefusesApplicationBoundToForeignNode covers the other queue
// path that has no caller scope at all: a signature-verified webhook delivery
// must not run an application on a node its stored team does not own.
func TestSystemDeployRefusesApplicationBoundToForeignNode(t *testing.T) {
	teamA, teamB := uuid.New(), uuid.New()

	repo := &fakeRepository{}
	app := testApplication(uuid.New())
	app.TeamID = teamA
	app.ServerID = repo.seedServerForTeam(teamB)
	app.BaseDomain = ""
	repo.app = app
	svc := newTestService(t, repo)

	if _, err := svc.DeploySystem(context.Background(), app.ID); !errors.Is(err, ErrServerNotFound) {
		t.Fatalf("system deploy on a foreign node = %v, want ErrServerNotFound", err)
	}
	if queued := listStored(t, repo, app.ID); len(queued) != 0 {
		t.Fatalf("deployments after refused system deploy = %d, want none", len(queued))
	}
}

// TestQueueingAllowsLegacySharedNode keeps the documented exception honest: a
// node without a team stays shared, so deploy, rollback and system deploys of
// the application's own team keep working.
func TestQueueingAllowsLegacySharedNode(t *testing.T) {
	teamA := uuid.New()
	alice := uuid.New()

	repo := &fakeRepository{}
	app := testApplication(alice)
	app.TeamID = teamA
	app.ServerID = repo.seedServerForTeam(uuid.Nil)
	app.BaseDomain = ""
	repo.app = app
	release := Deployment{
		ID:            uuid.New(),
		ApplicationID: app.ID,
		Kind:          KindDeploy,
		State:         StateRunning,
		ImageTag:      "nginx:alpine",
		RegistryImage: "nginx:alpine",
	}
	repo.deployments = []Deployment{release}
	svc := newTestService(t, repo)
	ctxA := teams.WithScope(context.Background(), teams.Scope{UserID: alice, TeamID: teamA, Role: teams.RoleOwner})

	if _, err := svc.Deploy(ctxA, alice, app.ID); err != nil {
		t.Fatalf("deploy on a legacy node: %v", err)
	}
	if _, err := svc.Rollback(ctxA, alice, app.ID, release.ID); err != nil {
		t.Fatalf("rollback on a legacy node: %v", err)
	}
	if _, err := svc.DeploySystem(context.Background(), app.ID); err != nil {
		t.Fatalf("system deploy on a legacy node: %v", err)
	}
}
