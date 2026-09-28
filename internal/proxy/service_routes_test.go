package proxy

import (
	"context"
	"sort"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/containers"
)

// fakeServiceSource is a canned, mutable ServiceSource.
type fakeServiceSource struct {
	services []ProxiedService
	err      error
}

// ListProxiedServices returns the canned rows.
func (f *fakeServiceSource) ListProxiedServices(context.Context) ([]ProxiedService, error) {
	if f.err != nil {
		return nil, f.err
	}
	return append([]ProxiedService{}, f.services...), nil
}

// composeContainer builds a node container carrying Docker Compose's own
// project/service labels and one published port.
func composeContainer(project, service, containerPort, hostPort string) containers.Container {
	return containers.Container{
		ID:    "container-" + service,
		Name:  project + "-" + service + "-1",
		State: "running",
		Labels: map[string]string{
			ComposeProjectLabel: project,
			ComposeServiceLabel: service,
		},
		Ports:         []string{hostPort + ":" + containerPort},
		PortsReported: true,
	}
}

// TestServiceRoutesForServerResolution proves the domain map resolves to the
// right compose service's running container and that every unroutable case is
// isolated as a service diagnostic.
func TestServiceRoutesForServerResolution(t *testing.T) {
	serverID := uuid.New()
	otherServer := uuid.New()
	serviceID := uuid.New()
	project := "gotham-" + serviceID.String()
	web := composeContainer(project, "web", "80", "32768")

	cases := map[string]struct {
		proxied    []ProxiedService
		containers []containers.Container
		claimed    map[string]bool
		wantRoutes int
		wantReason string
	}{
		"healthy": {
			proxied: []ProxiedService{{
				ID: serviceID, ServerID: serverID, Name: "shop", Project: project,
				Domains: []ServiceDomain{{Service: "web", Host: "Shop.Example.com", Port: 80}},
			}},
			containers: []containers.Container{web},
			wantRoutes: 1,
		},
		"another node is skipped": {
			proxied: []ProxiedService{{
				ID: serviceID, ServerID: otherServer, Name: "shop", Project: project,
				Domains: []ServiceDomain{{Service: "web", Host: "shop.example.com", Port: 80}},
			}},
			containers: []containers.Container{web},
			wantRoutes: 0,
		},
		"no container": {
			proxied: []ProxiedService{{
				ID: serviceID, ServerID: serverID, Name: "shop", Project: project,
				Domains: []ServiceDomain{{Service: "web", Host: "shop.example.com", Port: 80}},
			}},
			wantReason: "no running container",
		},
		"container not running": {
			proxied: []ProxiedService{{
				ID: serviceID, ServerID: serverID, Name: "shop", Project: project,
				Domains: []ServiceDomain{{Service: "web", Host: "shop.example.com", Port: 80}},
			}},
			containers: func() []containers.Container {
				stopped := web
				stopped.State = "exited"
				return []containers.Container{stopped}
			}(),
			wantReason: "no running container",
		},
		"port not published": {
			proxied: []ProxiedService{{
				ID: serviceID, ServerID: serverID, Name: "shop", Project: project,
				Domains: []ServiceDomain{{Service: "web", Host: "shop.example.com", Port: 8080}},
			}},
			containers: []containers.Container{web},
			wantReason: "not published",
		},
		"no engine-reported ports": {
			proxied: []ProxiedService{{
				ID: serviceID, ServerID: serverID, Name: "shop", Project: project,
				Domains: []ServiceDomain{{Service: "web", Host: "shop.example.com", Port: 80}},
			}},
			containers: func() []containers.Container {
				container := web
				container.PortsReported = false
				return []containers.Container{container}
			}(),
			wantReason: "engine-reported",
		},
		"application claims the host": {
			proxied: []ProxiedService{{
				ID: serviceID, ServerID: serverID, Name: "shop", Project: project,
				Domains: []ServiceDomain{{Service: "web", Host: "shop.example.com", Port: 80}},
			}},
			containers: []containers.Container{web},
			claimed:    map[string]bool{"shop.example.com": true},
			wantReason: "already routed by an application",
		},
		"invalid host": {
			proxied: []ProxiedService{{
				ID: serviceID, ServerID: serverID, Name: "shop", Project: project,
				Domains: []ServiceDomain{{Service: "web", Host: "bad host", Port: 80}},
			}},
			containers: []containers.Container{web},
			wantReason: "invalid domain",
		},
		"no port": {
			proxied: []ProxiedService{{
				ID: serviceID, ServerID: serverID, Name: "shop", Project: project,
				Domains: []ServiceDomain{{Service: "web", Host: "shop.example.com", Port: 0}},
			}},
			containers: []containers.Container{web},
			wantReason: "no container port",
		},
		"unroutable row": {
			proxied: []ProxiedService{{
				ID: serviceID, ServerID: serverID, Name: "shop", Project: project,
				Unroutable: "the stored document does not render",
			}},
			wantReason: "does not render",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			routes, diagnostics, hosts := serviceRoutesForServer(tc.proxied, serverID, DefaultBackendHost, tc.containers, tc.claimed)
			if len(routes) != tc.wantRoutes {
				t.Fatalf("routes = %+v, want %d", routes, tc.wantRoutes)
			}
			if tc.wantReason == "" && len(diagnostics) != 0 {
				t.Fatalf("diagnostics = %+v, want none", diagnostics)
			}
			if tc.wantReason != "" {
				if len(diagnostics) != 1 || !strings.Contains(diagnostics[0].Reason, tc.wantReason) {
					t.Fatalf("diagnostics = %+v, want %q", diagnostics, tc.wantReason)
				}
				if diagnostics[0].Kind != "service" {
					t.Fatalf("diagnostic kind = %q, want service", diagnostics[0].Kind)
				}
			}
			// Every declared host joins the claimed set regardless of
			// routability, so a redirect can never shadow one.
			if tc.proxied[0].ServerID == serverID && len(tc.proxied[0].Domains) > 0 {
				if len(hosts) == 0 {
					t.Fatalf("hosts = %v, want the declared host", hosts)
				}
			}
		})
	}
}

// TestServiceRoutesDuplicateHosts proves two compose services declaring the
// same host hold both bindings back.
func TestServiceRoutesDuplicateHosts(t *testing.T) {
	serverID := uuid.New()
	firstID, secondID := uuid.New(), uuid.New()
	routes, diagnostics, _ := serviceRoutesForServer([]ProxiedService{
		{ID: firstID, ServerID: serverID, Project: "gotham-" + firstID.String(),
			Domains: []ServiceDomain{{Service: "web", Host: "shop.example.com", Port: 80}}},
		{ID: secondID, ServerID: serverID, Project: "gotham-" + secondID.String(),
			Domains: []ServiceDomain{{Service: "api", Host: "SHOP.example.com", Port: 8080}}},
	}, serverID, DefaultBackendHost, []containers.Container{
		composeContainer("gotham-"+firstID.String(), "web", "80", "32768"),
		composeContainer("gotham-"+secondID.String(), "api", "8080", "32769"),
	}, nil)
	if len(routes) != 0 {
		t.Fatalf("routes = %+v, want both duplicate bindings held back", routes)
	}
	if len(diagnostics) != 2 {
		t.Fatalf("diagnostics = %+v, want two", diagnostics)
	}
	for _, diagnostic := range diagnostics {
		if !strings.Contains(diagnostic.Reason, "duplicate domain") {
			t.Errorf("diagnostic = %+v", diagnostic)
		}
	}
}

// TestServiceRoutesReachTheGeneratedConfiguration proves the resolved service
// route renders as its own router and service in the Traefik documents, with
// the live published backend port.
func TestServiceRoutesReachTheGeneratedConfiguration(t *testing.T) {
	serverID := uuid.New()
	serviceID := uuid.New()
	project := "gotham-" + serviceID.String()
	routes, diagnostics, _ := serviceRoutesForServer([]ProxiedService{{
		ID: serviceID, ServerID: serverID, Name: "shop", Project: project,
		Domains: []ServiceDomain{{Service: "web", Host: "shop.example.com", Port: 80}},
	}}, serverID, "172.17.0.1", []containers.Container{composeContainer(project, "web", "80", "32768")}, nil)
	if len(routes) != 1 || len(diagnostics) != 0 {
		t.Fatalf("routes = %+v, diagnostics = %+v", routes, diagnostics)
	}
	cfg := BuildConfig(routes, nil, nil, "", "")
	name := serviceRouteName(serviceID, 0)
	router, ok := cfg.Routers[name+"-web"]
	if !ok {
		t.Fatalf("routers = %v, want %s-web", keys(cfg.Routers), name)
	}
	if router.Rule != "Host(`shop.example.com`)" || router.Service != name {
		t.Fatalf("router = %+v", router)
	}
	service, ok := cfg.Services[name]
	if !ok || len(service.LoadBalancer.Servers) != 1 ||
		service.LoadBalancer.Servers[0].URL != "http://172.17.0.1:32768" {
		t.Fatalf("service = %+v", service)
	}
	if _, ok := cfg.Routers[name+"-websecure"]; ok {
		t.Fatalf("a service route must stay HTTP-only until certificates are configured for services")
	}
}

// TestServiceHostBlocksRedirectSource proves a redirect rule can never claim a
// host a compose service router answers for.
func TestServiceHostBlocksRedirectSource(t *testing.T) {
	serverID := uuid.New()
	rule := RedirectRule{
		ID: uuid.New(), SourceDomain: "shop.example.com", TargetDomain: "other.example.com",
		Code: RedirectCodeTemporary, Enabled: true, ServerID: serverID,
	}
	redirects, diagnostics := redirectsForServer(nil, []RedirectRule{rule}, serverID, "shop.example.com")
	if len(redirects) != 0 || len(diagnostics) != 1 {
		t.Fatalf("redirects = %+v, diagnostics = %+v", redirects, diagnostics)
	}
	if !strings.Contains(diagnostics[0].Reason, "base domain") {
		t.Fatalf("diagnostic = %+v, want the service host treated as a claimed domain", diagnostics[0])
	}
	// Without the service host the rule is generated normally.
	redirects, diagnostics = redirectsForServer(nil, []RedirectRule{rule}, serverID)
	if len(redirects) != 1 || len(diagnostics) != 0 {
		t.Fatalf("redirects = %+v, diagnostics = %+v", redirects, diagnostics)
	}
}

// TestSyncServerRoutesServiceDomains proves the sync path includes the compose
// service source end to end: the written dynamic document carries the service
// router and its live backend.
func TestSyncServerRoutesServiceDomains(t *testing.T) {
	serverID := uuid.New()
	serviceID := uuid.New()
	project := "gotham-" + serviceID.String()
	fixture := newSyncFixture(t, nil, serverID)
	fixture.service.services = &fakeServiceSource{services: []ProxiedService{{
		ID: serviceID, ServerID: serverID, Name: "shop", Project: project,
		Domains: []ServiceDomain{{Service: "web", Host: "shop.example.com", Port: 80}},
	}}}
	fixture.containers.list = append(fixture.containers.list, composeContainer(project, "web", "80", "32768"))

	if err := fixture.service.SyncServer(context.Background(), serverID); err != nil {
		t.Fatalf("SyncServer: %v", err)
	}
	dynamic := fixture.dynamicFile(t)
	if !strings.Contains(dynamic, "svc-"+serviceID.String()+"-0-web") {
		t.Fatalf("dynamic document is missing the service router:\n%s", dynamic)
	}
	if !strings.Contains(dynamic, "http://172.17.0.1:32768") {
		t.Fatalf("dynamic document is missing the live backend:\n%s", dynamic)
	}

	// Stopping the project (no running container) removes the route while the
	// healthy Traefik configuration is still pushed.
	fixture.containers.list = []containers.Container{runningTraefik()}
	if err := fixture.service.SyncServer(context.Background(), serverID); err == nil {
		t.Fatal("a sync with a stopped service project must report a diagnostic")
	}
	dynamic = fixture.dynamicFile(t)
	if strings.Contains(dynamic, "svc-"+serviceID.String()+"-0-web") {
		t.Fatalf("a stopped project must lose its route:\n%s", dynamic)
	}
}

// TestSyncServerApplicationWinsServiceHost proves a host claimed by an
// application is not routed to a compose service.
func TestSyncServerApplicationWinsServiceHost(t *testing.T) {
	serverID := uuid.New()
	appID := uuid.New()
	serviceID := uuid.New()
	appContainer := "app-container"
	fixture := newSyncFixture(t, []ProxiedApplication{{
		ID: appID, ServerID: serverID, BaseDomain: "shop.example.com", Port: 80, ContainerID: appContainer,
	}}, serverID)
	fixture.containers.list = append(fixture.containers.list, containers.Container{
		ID: appContainer, Name: "app", State: "running", Ports: []string{"32770:80"}, PortsReported: true,
	})
	project := "gotham-" + serviceID.String()
	fixture.service.services = &fakeServiceSource{services: []ProxiedService{{
		ID: serviceID, ServerID: serverID, Name: "shop", Project: project,
		Domains: []ServiceDomain{{Service: "web", Host: "shop.example.com", Port: 80}},
	}}}
	fixture.containers.list = append(fixture.containers.list, composeContainer(project, "web", "80", "32768"))

	err := fixture.service.SyncServer(context.Background(), serverID)
	var partial *PartialError
	if err == nil || !asPartial(err, &partial) || len(partial.Diagnostics) != 1 {
		t.Fatalf("SyncServer = %v, want one service diagnostic", err)
	}
	if partial.Diagnostics[0].Kind != "service" || !strings.Contains(partial.Diagnostics[0].Reason, "application") {
		t.Fatalf("diagnostics = %+v", partial.Diagnostics)
	}
	dynamic := fixture.dynamicFile(t)
	if strings.Contains(dynamic, "svc-"+serviceID.String()) {
		t.Fatalf("the service must not claim an application host:\n%s", dynamic)
	}
	if !strings.Contains(dynamic, "app-"+appID.String()+"-web") {
		t.Fatalf("the application route must survive:\n%s", dynamic)
	}
}

// asPartial is errors.As for *PartialError.
func asPartial(err error, target **PartialError) bool {
	for err != nil {
		if partial, ok := err.(*PartialError); ok {
			*target = partial
			return true
		}
		unwrapper, ok := err.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		err = unwrapper.Unwrap()
	}
	return false
}

// dynamicFile returns the dynamic Traefik document written by the last sync.
func (f *syncFixture) dynamicFile(t *testing.T) string {
	t.Helper()
	if len(f.agent.calls) == 0 {
		t.Fatal("no proxy configuration was written")
	}
	files := f.agent.calls[len(f.agent.calls)-1].GetFiles()
	for _, file := range files {
		if file.GetPath() == DynamicFileName(FormatYAML) {
			return string(file.GetContent())
		}
	}
	t.Fatalf("no dynamic document in %v", files)
	return ""
}

// keys returns the sorted keys of a router map.
func keys[V any](m map[string]V) []string {
	names := make([]string, 0, len(m))
	for name := range m {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
