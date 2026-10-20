package deploy

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/teams"
	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
)

// terminalFixture seeds one application and its newest deployment.
func terminalFixture(t *testing.T, app Application, dep Deployment) (*Service, *mockNode) {
	t.Helper()
	repo := &fakeRepository{app: app}
	node := newMockNode()
	svc := newNodeService(t, repo, node)
	seedDeployment(t, repo, app, dep)
	return svc, node
}

func TestTerminalContainersSingleContainer(t *testing.T) {
	userID := uuid.New()
	app := Application{
		ID:         uuid.New(),
		UserID:     userID,
		TeamID:     teams.PersonalTeamID(userID),
		ServerID:   uuid.New(),
		Name:       "demo",
		SourceType: SourceImage,
		ImageRef:   "nginx:1",
	}
	svc, node := terminalFixture(t, app, Deployment{State: StateRunning, ContainerID: "cid"})
	node.listed = []*agentv1.ContainerInfo{{
		Id: "cid", Name: "gotham-demo", Image: "nginx:1", State: "running",
	}}

	set, err := svc.ListTerminalContainers(context.Background(), userID, app.ID)
	if err != nil {
		t.Fatalf("ListTerminalContainers: %v", err)
	}
	if set.ServerID != app.ServerID || len(set.Containers) != 1 {
		t.Fatalf("set = %+v, want one container on the app node", set)
	}
	container := set.Containers[0]
	if container.ContainerID != "cid" || container.Service != "" || container.Replica != 1 ||
		container.Label != "gotham-demo" {
		t.Fatalf("container = %+v", container)
	}

	_, resolved, err := svc.ResolveTerminalContainer(context.Background(), userID, app.ID, "", "cid")
	if err != nil || resolved.ContainerID != "cid" {
		t.Fatalf("resolve = (%+v, %v)", resolved, err)
	}
	// A forged id is never attached.
	if _, _, err := svc.ResolveTerminalContainer(context.Background(), userID, app.ID, "", "someone-else"); !errors.Is(err, ErrContainerNotFound) {
		t.Fatalf("forged target = %v, want ErrContainerNotFound", err)
	}
}

func TestTerminalContainersComposeServicesAreDeterministic(t *testing.T) {
	userID := uuid.New()
	app := Application{
		ID:             uuid.New(),
		UserID:         userID,
		TeamID:         teams.PersonalTeamID(userID),
		ServerID:       uuid.New(),
		Name:           "shop",
		SourceType:     SourceCompose,
		ComposeService: "web",
	}
	svc, node := terminalFixture(t, app, Deployment{State: StateRunning, ContainerID: "web-1"})
	node.composeContainers = []ComposeContainer{
		{ID: "web-2", Service: "web", Name: "shop-web-2", State: "running"},
		{ID: "db-1", Service: "db", Name: "shop-db-1", State: "running"},
		{ID: "web-1", Service: "web", Name: "shop-web-1", State: "running"},
		{ID: "old", Service: "web", Name: "shop-web-old", State: "exited"},
	}
	set, err := svc.ListTerminalContainers(context.Background(), userID, app.ID)
	if err != nil {
		t.Fatalf("ListTerminalContainers: %v", err)
	}
	if len(set.Containers) != 3 {
		t.Fatalf("containers = %+v, want the three running services only", set.Containers)
	}
	// Deterministic order: service, then name.
	wantOrder := []string{"db-1", "web-1", "web-2"}
	for i, want := range wantOrder {
		if set.Containers[i].ContainerID != want {
			t.Fatalf("order[%d] = %q, want %q (%+v)", i, set.Containers[i].ContainerID, want, set.Containers)
		}
	}
	if set.Containers[1].Replica != 1 || set.Containers[2].Replica != 2 {
		t.Fatalf("replicas = %d,%d, want 1,2", set.Containers[1].Replica, set.Containers[2].Replica)
	}
	if set.Containers[1].Label != "web · shop-web-1" {
		t.Fatalf("label = %q", set.Containers[1].Label)
	}

	_, resolved, err := svc.ResolveTerminalContainer(context.Background(), userID, app.ID, "web", "web-2")
	if err != nil || resolved.Service != "web" || resolved.Name != "shop-web-2" {
		t.Fatalf("resolve = (%+v, %v)", resolved, err)
	}
	// A container is never attached under another service's label.
	if _, _, err := svc.ResolveTerminalContainer(context.Background(), userID, app.ID, "db", "web-2"); !errors.Is(err, ErrContainerNotFound) {
		t.Fatalf("service mismatch = %v, want ErrContainerNotFound", err)
	}
}

func TestTerminalContainersExplicitFailures(t *testing.T) {
	userID := uuid.New()
	app := Application{
		ID: uuid.New(), UserID: userID, TeamID: teams.PersonalTeamID(userID),
		ServerID: uuid.New(), Name: "demo", SourceType: SourceImage,
	}
	t.Run("no running deployment", func(t *testing.T) {
		svc, _ := terminalFixture(t, app, Deployment{State: StateFailed, ContainerID: "old"})
		if _, err := svc.ListTerminalContainers(context.Background(), userID, app.ID); !errors.Is(err, ErrNoRunningDeployment) {
			t.Fatalf("err = %v, want ErrNoRunningDeployment", err)
		}
	})
	t.Run("deployment in progress", func(t *testing.T) {
		svc, node := terminalFixture(t, app, Deployment{State: StateBuilding})
		data := []*agentv1.ContainerInfo{{Id: "cid", State: "running"}}
		node.listed = data
		if _, err := svc.ListTerminalContainers(context.Background(), userID, app.ID); !errors.Is(err, ErrConflict) {
			t.Fatalf("err = %v, want ErrConflict", err)
		}
	})
	t.Run("container stopped", func(t *testing.T) {
		svc, node := terminalFixture(t, app, Deployment{State: StateRunning, ContainerID: "cid"})
		node.listed = []*agentv1.ContainerInfo{{Id: "cid", State: "exited", Status: "Exited (0)"}}
		if _, err := svc.ListTerminalContainers(context.Background(), userID, app.ID); !errors.Is(err, ErrContainerNotRunning) {
			t.Fatalf("err = %v, want ErrContainerNotRunning", err)
		}
	})
	t.Run("container replaced", func(t *testing.T) {
		svc, node := terminalFixture(t, app, Deployment{State: StateRunning, ContainerID: "cid"})
		node.listed = []*agentv1.ContainerInfo{{Id: "newer", State: "running"}}
		if _, err := svc.ListTerminalContainers(context.Background(), userID, app.ID); !errors.Is(err, ErrContainerNotFound) {
			t.Fatalf("err = %v, want ErrContainerNotFound", err)
		}
	})
	t.Run("no compose containers", func(t *testing.T) {
		compose := app
		compose.SourceType = SourceCompose
		compose.ComposeService = "web"
		svc, _ := terminalFixture(t, compose, Deployment{State: StateRunning, ContainerID: "web"})
		if _, err := svc.ListTerminalContainers(context.Background(), userID, app.ID); !errors.Is(err, ErrContainerNotRunning) {
			t.Fatalf("err = %v, want ErrContainerNotRunning", err)
		}
	})
}

func TestTerminalContainersRequireApplicationWriteAccess(t *testing.T) {
	ownerID := uuid.New()
	teamID := uuid.New()
	app := Application{
		ID: uuid.New(), UserID: ownerID, TeamID: teamID,
		ServerID: uuid.New(), Name: "demo", SourceType: SourceImage,
	}
	svc, node := terminalFixture(t, app, Deployment{State: StateRunning, ContainerID: "cid"})
	node.listed = []*agentv1.ContainerInfo{{Id: "cid", State: "running"}}

	cases := []struct {
		name    string
		teamID  uuid.UUID
		role    teams.Role
		wantErr error
	}{
		{"another team", uuid.New(), teams.RoleOwner, ErrNotFound},
		{"read-only member", teamID, teams.RoleReadOnly, teams.ErrForbidden},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			userID := uuid.New()
			ctx := teams.WithScope(context.Background(), teams.Scope{
				UserID: userID, TeamID: testCase.teamID, Role: testCase.role,
			})
			if _, err := svc.ListTerminalContainers(ctx, userID, app.ID); !errors.Is(err, testCase.wantErr) {
				t.Fatalf("err = %v, want %v", err, testCase.wantErr)
			}
		})
	}

	adminID := uuid.New()
	adminCtx := teams.WithScope(context.Background(), teams.Scope{
		UserID: adminID, TeamID: teamID, Role: teams.RoleAdmin,
	})
	if _, err := svc.ListTerminalContainers(adminCtx, adminID, app.ID); err != nil {
		t.Fatalf("admin within the team: %v", err)
	}
}
