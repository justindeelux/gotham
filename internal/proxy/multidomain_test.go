package proxy

import (
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/containers"
)

func multiNode() []containers.Container {
	return []containers.Container{{ID: "c1", Name: "app", State: "running",
		Ports: []string{"32768:3000"}, PortsReported: true}}
}

func TestRoutesForServerEmitsOneRoutePerDomain(t *testing.T) {
	serverID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	appID := uuid.MustParse("aaaaaaaa-1111-1111-1111-111111111111")
	apps := []ProxiedApplication{{
		ID: appID, ServerID: serverID, Name: "demo",
		Domains:     []ProxiedDomain{{Domain: "app.example.com", Port: 3000}, {Domain: "www.example.com", Port: 3000}},
		ContainerID: "c1",
	}}

	routes, diagnostics := routesForServer(apps, serverID, DefaultBackendHost, multiNode(), nil)
	if len(diagnostics) != 0 {
		t.Fatalf("diagnostics = %#v, want none", diagnostics)
	}
	if len(routes) != 2 {
		t.Fatalf("routes = %#v, want one per domain", routes)
	}
	primary, alias := routes[0], routes[1]
	if primary.Domain != "app.example.com" || primary.Name != AliasRouteName(appID, "app.example.com") || primary.Service != primary.Name {
		t.Errorf("primary route = %#v, want its own stable router and service", primary)
	}
	if alias.Domain != "www.example.com" || alias.Name != AliasRouteName(appID, "www.example.com") || alias.Service != alias.Name {
		t.Errorf("alias route = %#v, want its own stable router and service", alias)
	}
	if primary.Target != alias.Target {
		t.Errorf("targets differ: %q vs %q, want one backend behind two names", primary.Target, alias.Target)
	}

	cfg := BuildConfig(routes, nil, nil, "", "")
	for _, route := range routes {
		name := route.Name
		router, ok := cfg.Routers[name+"-web"]
		if !ok {
			t.Fatalf("missing router %q in %#v", name, cfg.Routers)
		}
		if router.Service != name {
			t.Errorf("router %q serves %q, want its own service %q", name, router.Service, name)
		}
		if _, ok := cfg.Services[name]; !ok {
			t.Errorf("missing service %q", name)
		}
	}
	if len(cfg.Services) != 2 {
		t.Fatalf("services = %#v, want one per domain", cfg.Services)
	}
}

func TestRemovingADomainRemovesOnlyItsRouter(t *testing.T) {
	appID := uuid.MustParse("aaaaaaaa-1111-1111-1111-111111111111")
	target := "http://172.17.0.1:32768"
	both := []Route{
		{AppID: appID, Domain: "app.example.com", Target: target},
		{AppID: appID, Name: AliasRouteName(appID, "www.example.com"), Domain: "www.example.com", Target: target, Service: serviceName(appID)},
	}
	full := BuildConfig(both, nil, nil, "", "")
	if len(full.Routers) != 2 {
		t.Fatalf("full routers = %d, want 2", len(full.Routers))
	}
	remaining := BuildConfig(both[:1], nil, nil, "", "")
	if len(remaining.Routers) != 1 {
		t.Fatalf("remaining routers = %d, want only the primary", len(remaining.Routers))
	}
	single := BuildConfig([]Route{{AppID: appID, Domain: "app.example.com", Target: target}}, nil, nil, "", "")
	fullFiles, err := Generate(remaining, FormatYAML)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	singleFiles, err := Generate(single, FormatYAML)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	for i := range fullFiles {
		if string(fullFiles[i].Content) != string(singleFiles[i].Content) {
			t.Fatalf("survivor %q differs after alias removal:\n%s\n---\n%s",
				fullFiles[i].Name, fullFiles[i].Content, singleFiles[i].Content)
		}
	}
}

func TestRoutesForServerHoldsBackDuplicateDomainsAcrossApplications(t *testing.T) {
	serverID := uuid.New()
	first, second := uuid.New(), uuid.New()
	apps := []ProxiedApplication{
		{ID: first, ServerID: serverID, Domains: []ProxiedDomain{{Domain: "app.example.com", Port: 3000}}, ContainerID: "c1"},
		{ID: second, ServerID: serverID, Domains: []ProxiedDomain{{Domain: "other.example.com", Port: 3000}, {Domain: "app.example.com", Port: 3000}}, ContainerID: "c1"},
	}
	routes, diagnostics := routesForServer(apps, serverID, DefaultBackendHost, multiNode(), nil)
	if len(routes) != 1 || routes[0].Domain != "other.example.com" {
		t.Fatalf("routes = %#v, want only the uncontested alias", routes)
	}
	held := 0
	for _, diagnostic := range diagnostics {
		if strings.Contains(diagnostic.Reason, "duplicate domain") {
			held++
		}
	}
	if held != 2 {
		t.Fatalf("diagnostics = %#v, want both conflicting bindings held back", diagnostics)
	}
}

func TestRouteCertificateReusesWildcardForSibling(t *testing.T) {
	serverID := uuid.New()
	appID := uuid.New()
	providerID := uuid.New()
	providers := map[uuid.UUID]providerAccess{
		providerID: {
			provider:   DNSProvider{ID: providerID, Provider: ProviderCloudflare, Zones: []string{"example.com"}, Enabled: true},
			credential: "token",
		},
	}
	apps := []ProxiedApplication{{
		ID: appID, ServerID: serverID, Domains: []ProxiedDomain{{Domain: "app.example.com", Port: 3000, Protocol: ProtocolHTTPS}, {Domain: "api.example.com", Port: 3000, Protocol: ProtocolHTTPS}},
		ContainerID: "c1",
		Certificates: []*CertificateIntent{{
			Domain: "app.example.com", Enabled: true,
			Challenge: ChallengeDNS01, Wildcard: true, DNSProviderID: providerID,
		}},
	}}
	routes, diagnostics := routesForServer(apps, serverID, DefaultBackendHost, multiNode(), providers)
	if len(diagnostics) != 0 {
		t.Fatalf("diagnostics = %#v, want none", diagnostics)
	}
	if len(routes) != 2 {
		t.Fatalf("routes = %#v, want both domains", routes)
	}
	for _, route := range routes {
		if route.Certificate == nil {
			t.Fatalf("route %q has no certificate, want the reused wildcard", route.Domain)
		}
		if route.Certificate.Resolver != DNSResolverName(ProviderCloudflare) {
			t.Errorf("route %q resolver = %q", route.Domain, route.Certificate.Resolver)
		}
	}
	cfg := BuildConfig(routes, nil, nil, "", "")
	sibling, ok := cfg.Routers[AliasRouteName(appID, "api.example.com")+"-websecure"]
	if !ok {
		t.Fatalf("missing sibling HTTPS router in %#v", cfg.Routers)
	}
	if len(sibling.TLS.Domains) != 1 || sibling.TLS.Domains[0].Main != "api.example.com" ||
		len(sibling.TLS.Domains[0].SANs) != 1 || sibling.TLS.Domains[0].SANs[0] != "*.example.com" {
		t.Errorf("sibling TLS domains = %#v, want main plus the shared wildcard", sibling.TLS.Domains)
	}
}

func TestRouteCertificatePerDomainIsolation(t *testing.T) {
	serverID := uuid.New()
	appID := uuid.New()
	apps := []ProxiedApplication{{
		ID: appID, ServerID: serverID, Domains: []ProxiedDomain{{Domain: "app.example.com", Port: 3000, Protocol: ProtocolHTTPS}, {Domain: "www.example.com", Port: 3000, Protocol: ProtocolHTTPS}},
		ContainerID: "c1",
		Certificates: []*CertificateIntent{{
			Domain: "www.example.com", Enabled: true, Challenge: ChallengeHTTP01,
		}},
	}}
	routes, diagnostics := routesForServer(apps, serverID, DefaultBackendHost, multiNode(), nil)
	if len(routes) != 2 {
		t.Fatalf("routes = %#v, want both domains", routes)
	}
	if routes[0].Certificate != nil {
		t.Errorf("primary route unexpectedly carries TLS")
	}
	if routes[1].Certificate == nil {
		t.Errorf("alias route has no certificate")
	}
	var stale bool
	for _, diagnostic := range diagnostics {
		if diagnostic.Domain == "app.example.com" && strings.Contains(diagnostic.Reason, "www.example.com") {
			stale = true
		}
	}
	if !stale {
		t.Fatalf("diagnostics = %#v, want the stale-host entry for the primary", diagnostics)
	}
}
