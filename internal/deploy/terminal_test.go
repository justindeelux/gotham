package deploy

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
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
	svc, node := terminalFixture(t, app, Deployment{State: StateRunning, ContainerID: "aaaaaaaaaaaa"})
	node.composeContainers = []ComposeContainer{
		{ID: "web-2", Service: "web", Name: "shop-web-2", State: "running"},
		{ID: "db-1", Service: "db", Name: "shop-db-1", State: "running"},
		{ID: "web-1", Service: "web", Name: "shop-web-1", State: "running"},
		{ID: "old", Service: "web", Name: "shop-web-old", State: "exited"},
	}
	webOne := testFullContainerID("aaaaaaaaaaaa")
	webTwo := testFullContainerID("bbbbbbbbbbbb")
	dbOne := testFullContainerID("cccccccccccc")
	node.listed = []*agentv1.ContainerInfo{
		{Id: webTwo, Name: "shop-web-2", State: "running"},
		{Id: dbOne, Name: "shop-db-1", State: "running"},
		{Id: webOne, Name: "shop-web-1", State: "running"},
	}
	set, err := svc.ListTerminalContainers(context.Background(), userID, app.ID)
	if err != nil {
		t.Fatalf("ListTerminalContainers: %v", err)
	}
	if len(set.Containers) != 3 {
		t.Fatalf("containers = %+v, want the three running services only", set.Containers)
	}
	// Deterministic order: service, then name, and every id is the full id.
	wantOrder := []string{dbOne, webOne, webTwo}
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

	_, resolved, err := svc.ResolveTerminalContainer(context.Background(), userID, app.ID, "web", webTwo)
	if err != nil || resolved.Service != "web" || resolved.Name != "shop-web-2" {
		t.Fatalf("resolve = (%+v, %v)", resolved, err)
	}
	// The CLI's short id is not a terminal target even though it is a prefix.
	if _, _, err := svc.ResolveTerminalContainer(context.Background(), userID, app.ID, "web", "web-2"); !errors.Is(err, ErrContainerNotFound) {
		t.Fatalf("short id = %v, want ErrContainerNotFound", err)
	}
	// A container is never attached under another service's label.
	if _, _, err := svc.ResolveTerminalContainer(context.Background(), userID, app.ID, "db", webTwo); !errors.Is(err, ErrContainerNotFound) {
		t.Fatalf("service mismatch = %v, want ErrContainerNotFound", err)
	}
}

// capturedComposePs is the shape real `docker compose ps --format json` emits
// for a Compose application: ID is the 12-character short id, every row carries
// the project/service labels, and replicas repeat the service. The values are
// from the test box's web-demo Compose app.
const capturedComposePs = `{"Command":"\"/docker-entrypoint.…\"","CreatedAt":"2026-10-20 08:04:32 +0000 UTC","ExitCode":0,"Health":"","ID":"88f32c9135cd","Image":"nginx:alpine","Labels":"com.docker.compose.project=gotham-8e6ba4a6-dd2d-4eab-89ab-4bd813e68986,com.docker.compose.service=web-demo","LocalVolumes":"0","Mounts":"","Name":"gotham-8e6ba4a6-dd2d-4eab-89ab-4bd813e68986-web-demo-1","Names":"gotham-8e6ba4a6-dd2d-4eab-89ab-4bd813e68986-web-demo-1","Networks":"gotham-8e6ba4a6-dd2d-4eab-89ab-4bd813e68986_default","Ports":"0.0.0.0:8080->80/tcp, [::]:8080->80/tcp","Project":"gotham-8e6ba4a6-dd2d-4eab-89ab-4bd813e68986","Publishers":[],"RunningFor":"2 hours ago","Service":"web-demo","Size":"0B","State":"running","Status":"Up 2 hours"}
{"Command":"\"/docker-entrypoint.…\"","CreatedAt":"2026-10-20 08:04:33 +0000 UTC","ExitCode":0,"Health":"","ID":"4c7d9e1a2b3f","Image":"nginx:alpine","Labels":"com.docker.compose.project=gotham-8e6ba4a6-dd2d-4eab-89ab-4bd813e68986,com.docker.compose.service=web-demo","LocalVolumes":"0","Mounts":"","Name":"gotham-8e6ba4a6-dd2d-4eab-89ab-4bd813e68986-web-demo-2","Names":"gotham-8e6ba4a6-dd2d-4eab-89ab-4bd813e68986-web-demo-2","Networks":"gotham-8e6ba4a6-dd2d-4eab-89ab-4bd813e68986_default","Ports":"","Project":"gotham-8e6ba4a6-dd2d-4eab-89ab-4bd813e68986","Publishers":[],"RunningFor":"2 hours ago","Service":"web-demo","Size":"0B","State":"running","Status":"Up 2 hours"}
{"Command":"\"docker-entrypoint.sh\"","CreatedAt":"2026-10-20 08:04:30 +0000 UTC","ExitCode":0,"Health":"healthy","ID":"1f0a2b3c4d5e","Image":"redis:7-alpine","Labels":"com.docker.compose.project=gotham-8e6ba4a6-dd2d-4eab-89ab-4bd813e68986,com.docker.compose.service=db","LocalVolumes":"0","Mounts":"","Name":"gotham-8e6ba4a6-dd2d-4eab-89ab-4bd813e68986-db-1","Names":"gotham-8e6ba4a6-dd2d-4eab-89ab-4bd813e68986-db-1","Networks":"gotham-8e6ba4a6-dd2d-4eab-89ab-4bd813e68986_default","Ports":"6379/tcp","Project":"gotham-8e6ba4a6-dd2d-4eab-89ab-4bd813e68986","Publishers":[],"RunningFor":"2 hours ago","Service":"db","Size":"0B","State":"running","Status":"Up 2 hours (healthy)"}
{"Command":"\"/docker-entrypoint.…\"","CreatedAt":"2026-10-20 07:04:32 +0000 UTC","ExitCode":0,"Health":"","ID":"deadbeef0000","Image":"nginx:alpine","Labels":"com.docker.compose.project=gotham-8e6ba4a6-dd2d-4eab-89ab-4bd813e68986,com.docker.compose.service=web-demo","LocalVolumes":"0","Mounts":"","Name":"gotham-8e6ba4a6-dd2d-4eab-89ab-4bd813e68986-web-demo-3","Names":"gotham-8e6ba4a6-dd2d-4eab-89ab-4bd813e68986-web-demo-3","Networks":"gotham-8e6ba4a6-dd2d-4eab-89ab-4bd813e68986_default","Ports":"","Project":"gotham-8e6ba4a6-dd2d-4eab-89ab-4bd813e68986","Publishers":[],"RunningFor":"3 hours ago","Service":"web-demo","Size":"0B","State":"exited","Status":"Exited (0) 1 hour ago"}`

// composeContainersFromCaptured parses the captured CLI output the same way the
// agent's ComposePs adapter does, so the test feeds the control plane the exact
// rows a real node returns.
func composeContainersFromCaptured(t *testing.T, output string) []ComposeContainer {
	t.Helper()
	rows := []ComposeContainer{}
	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		var entry struct {
			ID      string `json:"ID"`
			Name    string `json:"Name"`
			Names   string `json:"Names"`
			Service string `json:"Service"`
			Image   string `json:"Image"`
			State   string `json:"State"`
			Status  string `json:"Status"`
		}
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			t.Fatalf("parse captured ps row: %v", err)
		}
		name := entry.Name
		if name == "" {
			name = entry.Names
		}
		rows = append(rows, ComposeContainer{
			ID: entry.ID, Service: entry.Service, Name: name,
			Image: entry.Image, State: entry.State, Status: entry.Status,
		})
	}
	return rows
}

// TestTerminalComposeTargetsUseFullIDsFromCapturedPs is the regression for
// JUS-256: a real Compose listing reports 12-character ids, but the targets
// list, the create match and the attach re-resolution must use the full
// 64-character id the agent validates.
func TestTerminalComposeTargetsUseFullIDsFromCapturedPs(t *testing.T) {
	const project = "gotham-8e6ba4a6-dd2d-4eab-89ab-4bd813e68986"
	webShort := "88f32c9135cd"
	webFull := testFullContainerID(webShort)
	webTwoFull := testFullContainerID("4c7d9e1a2b3f")
	dbFull := testFullContainerID("1f0a2b3c4d5e")

	userID := uuid.New()
	app := Application{
		ID:             uuid.New(),
		UserID:         userID,
		TeamID:         teams.PersonalTeamID(userID),
		ServerID:       uuid.New(),
		Name:           "web-demo",
		SourceType:     SourceCompose,
		ComposeService: "web-demo",
	}
	// An existing Compose deployment recorded the CLI's short id.
	svc, node := terminalFixture(t, app, Deployment{State: StateRunning, ContainerID: webShort})
	node.composeContainers = composeContainersFromCaptured(t, capturedComposePs)
	node.listed = []*agentv1.ContainerInfo{
		{Id: webFull, Name: project + "-web-demo-1", Image: "nginx:alpine", State: "running"},
		{Id: webTwoFull, Name: project + "-web-demo-2", Image: "nginx:alpine", State: "running"},
		{Id: dbFull, Name: project + "-db-1", Image: "redis:7-alpine", State: "running"},
	}

	set, err := svc.ListTerminalContainers(context.Background(), userID, app.ID)
	if err != nil {
		t.Fatalf("ListTerminalContainers: %v", err)
	}
	if len(set.Containers) != 3 {
		t.Fatalf("containers = %+v, want the three running containers", set.Containers)
	}
	for _, container := range set.Containers {
		if !isFullContainerID(container.ContainerID) {
			t.Fatalf("container_id = %q, want a full 64-character id", container.ContainerID)
		}
	}
	want := []struct{ service, id, name string }{
		{"db", dbFull, project + "-db-1"},
		{"web-demo", webFull, project + "-web-demo-1"},
		{"web-demo", webTwoFull, project + "-web-demo-2"},
	}
	for i, expected := range want {
		got := set.Containers[i]
		if got.Service != expected.service || got.ContainerID != expected.id || got.Name != expected.name {
			t.Fatalf("target[%d] = %+v, want service %q id %q name %q", i, got, expected.service, expected.id, expected.name)
		}
	}
	if set.Containers[1].Replica != 1 || set.Containers[2].Replica != 2 {
		t.Fatalf("web-demo replicas = %d,%d, want 1,2", set.Containers[1].Replica, set.Containers[2].Replica)
	}
	if set.Containers[1].Label != "web-demo · "+project+"-web-demo-1" {
		t.Fatalf("label = %q", set.Containers[1].Label)
	}

	// Create and attach both re-resolve through the same path and accept the
	// full id the list returned.
	for attempt := 1; attempt <= 2; attempt++ {
		_, resolved, err := svc.ResolveTerminalContainer(context.Background(), userID, app.ID, "web-demo", webFull)
		if err != nil || resolved.ContainerID != webFull || resolved.Service != "web-demo" {
			t.Fatalf("resolve attempt %d = (%+v, %v)", attempt, resolved, err)
		}
	}

	// The client-supplied short id (the old buggy list value), a container name
	// and a foreign id are all refused before any exec.
	for _, forged := range []string{webShort, project + "-web-demo-1", testFullContainerID("ffffffffffff")} {
		if _, _, err := svc.ResolveTerminalContainer(context.Background(), userID, app.ID, "web-demo", forged); !errors.Is(err, ErrContainerNotFound) {
			t.Fatalf("forged target %q = %v, want ErrContainerNotFound", forged, err)
		}
	}
}

// testFullContainerID expands a 12-character Docker id prefix into the full
// lowercase 64-character id the terminal contract requires.
func testFullContainerID(short string) string {
	const suffix = "0123456789abcdef0123456789abcdef0123456789abcdef0123"
	id := short + suffix
	if len(id) != 64 {
		panic("test container id is not 64 characters")
	}
	return id
}

// TestTerminalComposeTargetsResolveByPrefix proves the fallback when Compose
// prints no name, and that an ambiguous prefix is refused rather than guessed.
func TestTerminalComposeTargetsResolveByPrefix(t *testing.T) {
	userID := uuid.New()
	app := Application{
		ID: uuid.New(), UserID: userID, TeamID: teams.PersonalTeamID(userID),
		ServerID: uuid.New(), Name: "shop", SourceType: SourceCompose, ComposeService: "web",
	}
	webFull := testFullContainerID("aaaaaaaaaaaa")
	svc, node := terminalFixture(t, app, Deployment{State: StateRunning, ContainerID: "aaaaaaaaaaaa"})
	node.composeContainers = []ComposeContainer{{ID: "aaaaaaaaaaaa", Service: "web", State: "running"}}
	node.listed = []*agentv1.ContainerInfo{{Id: webFull, State: "running"}}

	set, err := svc.ListTerminalContainers(context.Background(), userID, app.ID)
	if err != nil || len(set.Containers) != 1 || set.Containers[0].ContainerID != webFull {
		t.Fatalf("prefix resolution = (%+v, %v)", set.Containers, err)
	}
	// The label falls back to the short form of the full id.
	if set.Containers[0].Label != "web · aaaaaaaaaaaa" {
		t.Fatalf("label = %q", set.Containers[0].Label)
	}

	// Two containers sharing the prefix make it ambiguous: never attach.
	node.listed = append(node.listed, &agentv1.ContainerInfo{
		Id: "aaaaaaaaaaaa1" + strings.Repeat("0", 51), Name: "other", State: "running",
	})
	if _, err := svc.ListTerminalContainers(context.Background(), userID, app.ID); !errors.Is(err, ErrContainerNotFound) {
		t.Fatalf("ambiguous prefix = %v, want ErrContainerNotFound", err)
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
