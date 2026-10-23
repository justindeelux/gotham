package proxy

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/containers"
)

type fakeRedirectSource struct {
	rules []RedirectRule
	err   error
}

func (f *fakeRedirectSource) ListRedirectRules(context.Context) ([]RedirectRule, error) {
	if f.err != nil {
		return nil, f.err
	}
	return append([]RedirectRule{}, f.rules...), nil
}

func routerFixture(t *testing.T, serverID uuid.UUID) (*syncFixture, ProxiedApplication) {
	t.Helper()
	app := runningApp(serverID, "app.example.com", "app-container", appContainerPort, 8080)
	app.Name = "shop"
	app.Domains[0].Protocol = ProtocolHTTPS
	app.Certificate = &CertificateIntent{Domain: "app.example.com", Enabled: true, Challenge: ChallengeHTTP01}
	fixture := newSyncFixture(t, []ProxiedApplication{app}, serverID)
	fixture.containers.list = append(fixture.containers.list, appContainer("app-container", 8080))
	return fixture, app
}

func TestListRoutersReflectsGeneratedState(t *testing.T) {
	serverID := uuid.New()
	fixture, app := routerFixture(t, serverID)
	ruleID := uuid.New()
	fixture.service.redirects = &fakeRedirectSource{rules: []RedirectRule{{
		ID: ruleID, ApplicationID: app.ID, SourceDomain: "old.example.com",
		TargetDomain: "app.example.com", Code: RedirectCodePermanent, PreservePath: true,
		Enabled: true, ServerID: serverID,
	}}}

	response, err := fixture.service.ListRouters(context.Background(), serverID)
	if err != nil {
		t.Fatalf("ListRouters: %v", err)
	}
	if len(response.Routers) != 2 {
		t.Fatalf("routers = %#v, want the application host and the redirect", response.Routers)
	}
	byHost := map[string]RouterInfo{}
	for _, router := range response.Routers {
		byHost[router.Host] = router
	}
	appRow, ok := byHost["app.example.com"]
	if !ok {
		t.Fatalf("routers = %#v, want a row for app.example.com", response.Routers)
	}
	if appRow.Rule != "Host(`app.example.com`)" {
		t.Errorf("rule = %q, want the generated Host rule", appRow.Rule)
	}
	if appRow.Kind != RouterKindApplication || appRow.OwnerID != app.ID || appRow.OwnerName != "shop" {
		t.Errorf("owner = %q %s %q, want application %s shop", appRow.Kind, appRow.OwnerID, appRow.OwnerName, app.ID)
	}
	if appRow.TLSResolver != DefaultResolverName {
		t.Errorf("tls_resolver = %q, want %q", appRow.TLSResolver, DefaultResolverName)
	}
	if len(appRow.EntryPoints) != 2 {
		t.Errorf("entrypoints = %v, want the merged web+websecure routers", appRow.EntryPoints)
	}
	redirect, ok := byHost["old.example.com"]
	if !ok {
		t.Fatalf("routers = %#v, want a row for old.example.com", response.Routers)
	}
	if redirect.Kind != RouterKindRedirect || redirect.OwnerID != ruleID || redirect.Target != "https://app.example.com" {
		t.Errorf("redirect = %#v, want kind redirect owned by the rule targeting the new host", redirect)
	}
	if redirect.TLSResolver != "" {
		t.Errorf("redirect tls_resolver = %q, want empty (plain HTTP)", redirect.TLSResolver)
	}
	if len(response.Nodes) != 1 || response.Nodes[0].ServerID != serverID {
		t.Fatalf("nodes = %#v, want the single requested node", response.Nodes)
	}
}

func TestListRoutersSyncStatusPendingThenSynced(t *testing.T) {
	serverID := uuid.New()
	fixture, _ := routerFixture(t, serverID)

	response, err := fixture.service.ListRouters(context.Background(), serverID)
	if err != nil {
		t.Fatalf("ListRouters: %v", err)
	}
	if response.Nodes[0].SyncStatus != RouterSyncPending {
		t.Fatalf("sync status = %q, want pending before the first sync", response.Nodes[0].SyncStatus)
	}
	if err := fixture.service.SyncServer(context.Background(), serverID); err != nil {
		t.Fatalf("SyncServer: %v", err)
	}
	response, err = fixture.service.ListRouters(context.Background(), serverID)
	if err != nil {
		t.Fatalf("ListRouters: %v", err)
	}
	if response.Nodes[0].SyncStatus != RouterSyncSynced {
		t.Fatalf("sync status = %q, want synced after the sync", response.Nodes[0].SyncStatus)
	}
	fixture.source.setApps(append(fixture.source.apps, ProxiedApplication{
		ID: uuid.New(), ServerID: serverID, Domains: []ProxiedDomain{{Domain: "app.example.com", Port: 80}},
		ContainerID: "other",
	})...)
	response, err = fixture.service.ListRouters(context.Background(), serverID)
	if err != nil {
		t.Fatalf("ListRouters: %v", err)
	}
	if len(response.Routers) != 0 {
		t.Fatalf("routers = %#v, want every duplicate binding held back", response.Routers)
	}
	if len(response.Nodes[0].Diagnostics) != 2 {
		t.Fatalf("diagnostics = %#v, want both held-back reasons", response.Nodes[0].Diagnostics)
	}
	if response.Nodes[0].SyncStatus != RouterSyncPending {
		t.Fatalf("sync status = %q, want pending once state drifts", response.Nodes[0].SyncStatus)
	}
}

func TestListRoutersUnreachableNode(t *testing.T) {
	serverID := uuid.New()
	fixture, _ := routerFixture(t, serverID)
	fixture.containers.listErr = containers.ErrAgentUnavailable

	if _, err := fixture.service.ListRouters(context.Background(), serverID); !errors.Is(err, ErrAgentUnavailable) {
		t.Fatalf("ListRouters err = %v, want the agent-unavailable sentinel", err)
	}

	otherID := uuid.New()
	other := runningApp(otherID, "other.example.com", "other-container", appContainerPort, 8081)
	fixture.source.setApps(append(fixture.source.apps, other)...)
	fixture.service.nodes = fakeNodes{nodes: []uuid.UUID{serverID, otherID}}
	fixture.containers.listErr = nil
	fixture.containers.list = []containers.Container{
		runningTraefik(),
		appContainer("other-container", 8081),
	}

	lists := map[uuid.UUID][]containers.Container{otherID: fixture.containers.list}
	fixture.service.containers = &perNodeContainers{lists: lists, err: containers.ErrAgentUnavailable}

	response, err := fixture.service.ListRouters(context.Background(), uuid.Nil)
	if err != nil {
		t.Fatalf("ListRouters: %v", err)
	}
	for _, router := range response.Routers {
		if router.ServerID == serverID {
			t.Fatalf("routers = %#v, want no fabricated rows for the unreachable node", response.Routers)
		}
	}
	if len(response.Routers) != 1 || response.Routers[0].Host != "other.example.com" {
		t.Fatalf("routers = %#v, want the healthy node's row", response.Routers)
	}
	byNode := map[uuid.UUID]RouterNodeState{}
	for _, node := range response.Nodes {
		byNode[node.ServerID] = node
	}
	if byNode[serverID].Error == "" {
		t.Fatalf("nodes = %#v, want the unreachable node reported with an error", response.Nodes)
	}
	if byNode[otherID].Error != "" {
		t.Fatalf("nodes = %#v, want the healthy node without an error", response.Nodes)
	}
}

type perNodeContainers struct {
	containers.ContainerService
	lists map[uuid.UUID][]containers.Container
	err   error
}

func (f *perNodeContainers) List(_ context.Context, serverID uuid.UUID) ([]containers.Container, error) {
	if list, ok := f.lists[serverID]; ok {
		return list, nil
	}
	return nil, f.err
}

func TestListRoutersAllNodesIncludesRegisteredNodes(t *testing.T) {
	serverID := uuid.New()
	fixture, _ := routerFixture(t, serverID)
	emptyID := uuid.New()
	fixture.service.nodes = fakeNodes{nodes: []uuid.UUID{serverID, emptyID}}

	response, err := fixture.service.ListRouters(context.Background(), uuid.Nil)
	if err != nil {
		t.Fatalf("ListRouters: %v", err)
	}
	if len(response.Nodes) != 2 {
		t.Fatalf("nodes = %#v, want both nodes", response.Nodes)
	}
}

func TestAsRouterServiceNil(t *testing.T) {
	if AsRouterService(nil) != nil {
		t.Fatal("AsRouterService(nil) != nil")
	}
	if AsRouterService(&fakeProxyService{}) != nil {
		t.Fatal("AsRouterService of a sync-only service != nil")
	}
}
