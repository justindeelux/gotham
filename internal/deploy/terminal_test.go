package deploy

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/services"
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
	project := services.ProjectName(app.ID)
	svc, node := terminalFixture(t, app, Deployment{State: StateRunning, ContainerID: "aaaaaaaaaaaa"})
	node.composeContainers = []ComposeContainer{
		{ID: "bbbbbbbbbbbb", Service: "web", Name: "shop-web-2", State: "running"},
		{ID: "cccccccccccc", Service: "db", Name: "shop-db-1", State: "running"},
		{ID: "aaaaaaaaaaaa", Service: "web", Name: "shop-web-1", State: "running"},
		{ID: "dddddddddddd", Service: "web", Name: "shop-web-old", State: "exited"},
	}
	webOne := testFullContainerID("aaaaaaaaaaaa")
	webTwo := testFullContainerID("bbbbbbbbbbbb")
	dbOne := testFullContainerID("cccccccccccc")
	node.listed = []*agentv1.ContainerInfo{
		composeContainerInfo(project, "web", webTwo, "shop-web-2", "running"),
		composeContainerInfo(project, "db", dbOne, "shop-db-1", "running"),
		composeContainerInfo(project, "web", webOne, "shop-web-1", "running"),
		// A stopped container of this project that shares web-1's prefix is
		// never a candidate, even though the node's label filter returns it.
		composeContainerInfo(project, "web", "aaaaaaaaaaaab"+strings.Repeat("0", 51), "shop-web-stopped", "exited"),
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
	if _, _, err := svc.ResolveTerminalContainer(context.Background(), userID, app.ID, "web", "bbbbbbbbbbbb"); !errors.Is(err, ErrContainerNotFound) {
		t.Fatalf("short id = %v, want ErrContainerNotFound", err)
	}
	// A container is never attached under another service's label.
	if _, _, err := svc.ResolveTerminalContainer(context.Background(), userID, app.ID, "db", webTwo); !errors.Is(err, ErrContainerNotFound) {
		t.Fatalf("service mismatch = %v, want ErrContainerNotFound", err)
	}
}

// composeContainerInfo builds a node container summary carrying the Compose
// labels Docker assigns. The tests use it to model exactly what the node's
// label-filtered list returns.
func composeContainerInfo(project, service, id, name, state string) *agentv1.ContainerInfo {
	return &agentv1.ContainerInfo{
		Id: id, Name: name, State: state,
		Labels: map[string]string{
			composeProjectLabel: project,
			composeServiceLabel: service,
		},
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
	const capturedProject = "gotham-8e6ba4a6-dd2d-4eab-89ab-4bd813e68986"
	webShort := "88f32c9135cd"
	webFull := testFullContainerID(webShort)
	webTwoFull := testFullContainerID("4c7d9e1a2b3f")
	dbFull := testFullContainerID("1f0a2b3c4d5e")

	userID := uuid.New()
	app := Application{
		ID:             uuid.MustParse("8e6ba4a6-dd2d-4eab-89ab-4bd813e68986"),
		UserID:         userID,
		TeamID:         teams.PersonalTeamID(userID),
		ServerID:       uuid.New(),
		Name:           "web-demo",
		SourceType:     SourceCompose,
		ComposeService: "web-demo",
	}
	if project := services.ProjectName(app.ID); project != capturedProject {
		t.Fatalf("project = %q, want the captured %q", project, capturedProject)
	}
	// An existing Compose deployment recorded the CLI's short id.
	svc, node := terminalFixture(t, app, Deployment{State: StateRunning, ContainerID: webShort})
	node.composeContainers = composeContainersFromCaptured(t, capturedComposePs)
	node.listed = []*agentv1.ContainerInfo{
		composeContainerInfo(capturedProject, "web-demo", webFull, capturedProject+"-web-demo-1", "running"),
		composeContainerInfo(capturedProject, "web-demo", webTwoFull, capturedProject+"-web-demo-2", "running"),
		composeContainerInfo(capturedProject, "db", dbFull, capturedProject+"-db-1", "running"),
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
		{"db", dbFull, capturedProject + "-db-1"},
		{"web-demo", webFull, capturedProject + "-web-demo-1"},
		{"web-demo", webTwoFull, capturedProject + "-web-demo-2"},
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
	if set.Containers[1].Label != "web-demo · "+capturedProject+"-web-demo-1" {
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
	for _, forged := range []string{webShort, capturedProject + "-web-demo-1", testFullContainerID("ffffffffffff")} {
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

// TestResolveComposeTargetIDsNeverSelectsForeignContainers drives the resolver
// with a node-wide list (every project, stopped and running) and proves the
// project/service/state scoping is what decides: a same-name foreign container
// or a stopped replica never supplies the id.
func TestResolveComposeTargetIDsNeverSelectsForeignContainers(t *testing.T) {
	userID := uuid.New()
	app := Application{
		ID: uuid.New(), UserID: userID, TeamID: teams.PersonalTeamID(userID),
		ServerID: uuid.New(), Name: "shop", SourceType: SourceCompose, ComposeService: "web",
	}
	project := services.ProjectName(app.ID)
	short := "88f32c9135cd"
	ownID := testFullContainerID(short)
	// All three share the target's short prefix; only the scoping (foreign
	// project, stopped, other service) keeps them out of the result.
	foreignRunning := "88f32c9135cdffff" + strings.Repeat("0", 48)
	foreignStopped := "88f32c9135cdeeee" + strings.Repeat("0", 48)
	otherService := "88f32c9135cddddd" + strings.Repeat("0", 48)
	// The first entry is a foreign project's container with the same name and
	// the same short prefix; a node-wide name/prefix scan would find it (and
	// with the stopped and other-service rows become ambiguous).
	listed := []*agentv1.ContainerInfo{
		composeContainerInfo("gotham-99999999-8888-4777-8666-555555555555", "web", foreignRunning, "shop-web-1", "running"),
		composeContainerInfo(project, "web", foreignStopped, "shop-web-1", "exited"),
		composeContainerInfo(project, "db", otherService, "shop-db-1", "running"),
		composeContainerInfo(project, "web", ownID, "shop-web-1", "running"),
	}
	svc, _ := terminalFixture(t, app, Deployment{State: StateRunning, ContainerID: short})
	targets := []TerminalContainer{{Service: "web", ContainerID: short, Name: "shop-web-1"}}
	resolved := svc.resolveComposeTargetIDs(targets, listed, project, app.ID)
	if len(resolved) != 1 || resolved[0].ContainerID != ownID {
		t.Fatalf("resolved = %+v, want only this project's running %s", resolved, ownID)
	}
	// A list holding only foreign containers resolves nothing.
	if resolved := svc.resolveComposeTargetIDs(targets, listed[:3], project, app.ID); len(resolved) != 0 {
		t.Fatalf("foreign-only list resolved = %+v, want none", resolved)
	}
}

// TestTerminalComposeTargetsResolveByPrefix proves the fallback when Compose
// prints no name, and that an ambiguous prefix is refused rather than guessed.
func TestTerminalComposeTargetsResolveByPrefix(t *testing.T) {
	userID := uuid.New()
	app := Application{
		ID: uuid.New(), UserID: userID, TeamID: teams.PersonalTeamID(userID),
		ServerID: uuid.New(), Name: "shop", SourceType: SourceCompose, ComposeService: "web",
	}
	project := services.ProjectName(app.ID)
	webFull := testFullContainerID("aaaaaaaaaaaa")
	svc, node := terminalFixture(t, app, Deployment{State: StateRunning, ContainerID: "aaaaaaaaaaaa"})
	node.composeContainers = []ComposeContainer{{ID: "aaaaaaaaaaaa", Service: "web", State: "running"}}
	node.listed = []*agentv1.ContainerInfo{composeContainerInfo(project, "web", webFull, "", "running")}

	set, err := svc.ListTerminalContainers(context.Background(), userID, app.ID)
	if err != nil || len(set.Containers) != 1 || set.Containers[0].ContainerID != webFull {
		t.Fatalf("prefix resolution = (%+v, %v)", set.Containers, err)
	}
	// The label falls back to the short form of the full id.
	if set.Containers[0].Label != "web · aaaaaaaaaaaa" {
		t.Fatalf("label = %q", set.Containers[0].Label)
	}

	// Two project containers sharing the prefix make it ambiguous: the target
	// is dropped, and the whole list has no resolvable target.
	node.listed = append(node.listed, composeContainerInfo(project, "web", "aaaaaaaaaaaa1"+strings.Repeat("0", 51), "", "running"))
	if _, err := svc.ListTerminalContainers(context.Background(), userID, app.ID); !errors.Is(err, ErrContainerNotFound) {
		t.Fatalf("ambiguous prefix = %v, want ErrContainerNotFound", err)
	}
}

// TestComposeContainerCandidatesScopeToProjectServiceAndState pins the candidate
// set itself: a foreign project, a foreign service, a stopped container and a
// non-full id are never candidates, even when the node's answer is wider than
// requested.
func TestComposeContainerCandidatesScopeToProjectServiceAndState(t *testing.T) {
	const project = "gotham-11111111-2222-4333-8444-555555555555"
	webShort := "88f32c9135cd"
	own := composeContainerInfo(project, "web", testFullContainerID(webShort), "proj-web-1", "running")
	listed := []*agentv1.ContainerInfo{
		own,
		// Foreign project, same 12-hex prefix, running.
		composeContainerInfo("gotham-99999999-8888-4777-8666-555555555555", "web", testFullContainerID(webShort), "foreign-web-1", "running"),
		// Foreign project, stopped.
		composeContainerInfo("gotham-99999999-8888-4777-8666-555555555555", "web", "88f32c9135cdffff"+strings.Repeat("0", 48), "foreign-stopped", "exited"),
		// Same project, another service.
		composeContainerInfo(project, "db", testFullContainerID("1f0a2b3c4d5e"), "proj-db-1", "running"),
		// Same project and service, stopped.
		composeContainerInfo(project, "web", "88f32c9135cd0000"+strings.Repeat("0", 48), "proj-web-stopped", "exited"),
		// Same project and service, but the node reported a short id: never a
		// candidate, so a short id can never be returned as a full one.
		composeContainerInfo(project, "web", webShort, "proj-web-short", "running"),
	}

	candidates := composeContainerCandidates(listed, project, "web")
	if len(candidates) != 1 || candidates[0] != own {
		t.Fatalf("candidates = %+v, want only the project's running web container", candidates)
	}

	// The resolution step refuses an id that is outside the accepted shape.
	for _, id := range []string{webShort + "F", "88F32C9135CD", "d", ""} {
		if resolved, ok := resolveComposeTargetID(TerminalContainer{Service: "web", ContainerID: id, Name: "proj-web-1"}, candidates); ok {
			t.Fatalf("target id %q resolved to %q, want a refusal", id, resolved)
		}
	}
}

// TestResolveComposeTargetIDNameAndPrefix pins the name and prefix rules: a name
// must match exactly (web never matches web-2), a name miss never falls through
// to a prefix scan, and only a unique prefix among the scoped candidates
// resolves.
func TestResolveComposeTargetIDNameAndPrefix(t *testing.T) {
	short := "88f32c9135cd"
	full := testFullContainerID(short)
	// The second replica shares the short prefix but has a different full id.
	webTwo := composeContainerInfo("proj", "web", "88f32c9135cd1"+strings.Repeat("0", 51), "proj-web-2", "running")
	web := composeContainerInfo("proj", "web", full, "proj-web-1", "running")

	cases := []struct {
		name       string
		target     TerminalContainer
		candidates []*agentv1.ContainerInfo
		wantID     string
		wantOK     bool
	}{
		{
			name:       "exact name",
			target:     TerminalContainer{Service: "web", ContainerID: short, Name: "proj-web-1"},
			candidates: []*agentv1.ContainerInfo{web, webTwo},
			wantID:     full, wantOK: true,
		},
		{
			// A prefix-based name comparison would match proj-web-2 here.
			name:       "name is a prefix of another name",
			target:     TerminalContainer{Service: "web", ContainerID: short, Name: "proj-web"},
			candidates: []*agentv1.ContainerInfo{webTwo},
			wantOK:     false,
		},
		{
			name:       "name miss with a single prefix match still refuses",
			target:     TerminalContainer{Service: "web", ContainerID: short, Name: "proj-web-gone"},
			candidates: []*agentv1.ContainerInfo{webTwo},
			wantOK:     false,
		},
		{
			name:       "name miss never falls through to a prefix scan",
			target:     TerminalContainer{Service: "web", ContainerID: short, Name: "proj-web-gone"},
			candidates: []*agentv1.ContainerInfo{web, webTwo},
			wantOK:     false,
		},
		{
			name:       "unnamed unique prefix",
			target:     TerminalContainer{Service: "web", ContainerID: short, Name: ""},
			candidates: []*agentv1.ContainerInfo{web},
			wantID:     full, wantOK: true,
		},
		{
			name:       "unnamed ambiguous prefix",
			target:     TerminalContainer{Service: "web", ContainerID: short, Name: ""},
			candidates: []*agentv1.ContainerInfo{web, webTwo},
			wantOK:     false,
		},
		{
			name:       "already full id is kept",
			target:     TerminalContainer{Service: "web", ContainerID: full, Name: ""},
			candidates: nil,
			wantID:     full, wantOK: true,
		},
		{
			// A node answer that still carries a short id must never be
			// returned as the resolved target.
			name:       "non-full candidate is never returned",
			target:     TerminalContainer{Service: "web", ContainerID: short, Name: ""},
			candidates: []*agentv1.ContainerInfo{composeContainerInfo("proj", "web", short, "proj-web-short", "running")},
			wantOK:     false,
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			id, ok := resolveComposeTargetID(testCase.target, testCase.candidates)
			if ok != testCase.wantOK || id != testCase.wantID {
				t.Fatalf("resolveComposeTargetID = (%q, %v), want (%q, %v)", id, ok, testCase.wantID, testCase.wantOK)
			}
		})
	}
}

// TestTerminalComposeTargetsOmitUnresolvableReplica proves a replica that
// vanished between ComposePs and the container list is omitted (with a WARN log
// carrying the application id) instead of failing the whole targets list.
func TestTerminalComposeTargetsOmitUnresolvableReplica(t *testing.T) {
	userID := uuid.New()
	app := Application{
		ID: uuid.New(), UserID: userID, TeamID: teams.PersonalTeamID(userID),
		ServerID: uuid.New(), Name: "shop", SourceType: SourceCompose, ComposeService: "web",
	}
	project := services.ProjectName(app.ID)
	webOneShort := "aaaaaaaaaaaa"
	webOneFull := testFullContainerID(webOneShort)
	svc, node := terminalFixture(t, app, Deployment{State: StateRunning, ContainerID: webOneShort})
	node.composeContainers = []ComposeContainer{
		{ID: webOneShort, Service: "web", Name: "shop-web-1", State: "running"},
		{ID: "bbbbbbbbbbbb", Service: "web", Name: "shop-web-2", State: "running"},
	}
	// Only replica 1 is still on the node; replica 2 vanished.
	node.listed = []*agentv1.ContainerInfo{
		composeContainerInfo(project, "web", webOneFull, "shop-web-1", "running"),
	}

	capture := &recordHandler{}
	svc.logger = slog.New(capture)

	set, err := svc.ListTerminalContainers(context.Background(), userID, app.ID)
	if err != nil {
		t.Fatalf("ListTerminalContainers: %v", err)
	}
	if len(set.Containers) != 1 || set.Containers[0].ContainerID != webOneFull {
		t.Fatalf("containers = %+v, want only the resolvable replica", set.Containers)
	}
	records := capture.snapshot()
	if len(records) != 1 || records[0].level != slog.LevelWarn {
		t.Fatalf("logs = %+v, want one WARN record", records)
	}
	if records[0].attrs["application_id"] != app.ID.String() {
		t.Fatalf("warn attrs = %v, want application_id %s", records[0].attrs, app.ID)
	}
	if records[0].attrs["service"] != "web" {
		t.Fatalf("warn attrs = %v, want service web", records[0].attrs)
	}
}

// capturedLog is one slog record captured by recordHandler.
type capturedLog struct {
	level   slog.Level
	message string
	attrs   map[string]string
}

// recordHandler is a minimal in-memory slog handler for asserting the
// resolution warnings.
type recordHandler struct {
	mu      sync.Mutex
	records []capturedLog
}

func (h *recordHandler) Enabled(context.Context, slog.Level) bool { return true }

func (h *recordHandler) Handle(_ context.Context, record slog.Record) error {
	attrs := make(map[string]string, record.NumAttrs())
	record.Attrs(func(attr slog.Attr) bool {
		attrs[attr.Key] = attr.Value.String()
		return true
	})
	h.mu.Lock()
	h.records = append(h.records, capturedLog{level: record.Level, message: record.Message, attrs: attrs})
	h.mu.Unlock()
	return nil
}

func (h *recordHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *recordHandler) WithGroup(string) slog.Handler      { return h }

func (h *recordHandler) snapshot() []capturedLog {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]capturedLog(nil), h.records...)
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
