package deploy

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/teams"
)

// TestListRecentDeploymentsRespectsTeamScope is the JUS-193 dashboard-feed
// scoping regression: the aggregate never lists a deployment of another
// team's application (nor a preview), the creator-scoped fallback stays
// inside the caller's own rows, and the page is bounded by limit.
func TestListRecentDeploymentsRespectsTeamScope(t *testing.T) {
	teamA, teamB := uuid.New(), uuid.New()
	alice := uuid.New()

	repo := &fakeRepository{}
	appA := testApplication(alice)
	appA.TeamID = teamA
	appA.Name = "team-a-app"
	appB := testApplication(uuid.New())
	appB.TeamID = teamB
	appB.Name = "team-b-app"
	preview := testApplication(alice)
	preview.TeamID = teamA
	preview.Name = "team-a-preview"
	preview.IsPreview = true
	repo.apps = []Application{appA, appB, preview}

	base := time.Now().UTC()
	repo.deployments = []Deployment{
		{ID: uuid.New(), ApplicationID: appA.ID, Kind: KindDeploy, State: StateRunning, CreatedAt: base},
		{ID: uuid.New(), ApplicationID: appB.ID, Kind: KindDeploy, State: StateRunning, CreatedAt: base.Add(time.Minute)},
		{ID: uuid.New(), ApplicationID: appB.ID, Kind: KindDeploy, State: StateFailed, CreatedAt: base.Add(2 * time.Minute)},
		{ID: uuid.New(), ApplicationID: preview.ID, Kind: KindDeploy, State: StateRunning, CreatedAt: base.Add(3 * time.Minute)},
	}
	svc := newTestService(t, repo)
	ctxA := teams.WithScope(context.Background(), teams.Scope{UserID: alice, TeamID: teamA, Role: teams.RoleOwner})
	ctxB := teams.WithScope(context.Background(), teams.Scope{UserID: uuid.New(), TeamID: teamB, Role: teams.RoleOwner})

	got, err := svc.ListRecentDeployments(ctxA, alice, 10)
	if err != nil {
		t.Fatalf("team A feed: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("team A rows = %d, want 1 (its own non-preview app only)", len(got))
	}
	if got[0].ApplicationID != appA.ID || got[0].ApplicationName != "team-a-app" {
		t.Errorf("team A row = %s/%q, want team A's app only (no cross-team leak)",
			got[0].ApplicationID, got[0].ApplicationName)
	}

	// team B sees its own two rows, newest first, bounded by limit.
	gotB, err := svc.ListRecentDeployments(ctxB, teamB, 1)
	if err != nil {
		t.Fatalf("team B feed: %v", err)
	}
	if len(gotB) != 1 {
		t.Fatalf("team B rows = %d, want 1 (limit)", len(gotB))
	}
	if gotB[0].ApplicationID != appB.ID || gotB[0].State != StateFailed {
		t.Errorf("team B row = %s/%s, want the newest failed row of its own app",
			gotB[0].ApplicationID, gotB[0].State)
	}

	// Without a team context the feed falls back to the creator's own rows.
	gotUser, err := svc.ListRecentDeployments(context.Background(), alice, 10)
	if err != nil {
		t.Fatalf("creator feed: %v", err)
	}
	if len(gotUser) != 1 || gotUser[0].ApplicationID != appA.ID {
		t.Fatalf("creator rows = %d, want alice's own non-preview app only", len(gotUser))
	}
}

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
