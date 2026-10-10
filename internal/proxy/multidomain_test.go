package proxy

import (
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/containers"
)

// multiNode is one running container publishing the application port, so the
// multi-domain routes resolve an endpoint.
func multiNode() []containers.Container {
	return []containers.Container{{ID: "c1", Name: "app", State: "running",
		Ports: []string{"32768:3000"}, PortsReported: true}}
}

// TestRoutesForServerEmitsOneRoutePerDomain proves the JUS-89 routing model:
// an application with two domains emits two routes, the primary keeping the
// legacy router/service names (single-domain output is byte-identical) while
// the alias gets its own stable routers over the shared service.
func TestRoutesForServerEmitsOneRoutePerDomain(t *testing.T) {
	serverID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	appID := uuid.MustParse("aaaaaaaa-1111-1111-1111-111111111111")
	apps := []ProxiedApplication{{
		ID: appID, ServerID: serverID, Name: "demo",
		BaseDomain: "app.example.com",
		Domains:    []string{"app.example.com", "www.example.com"},
		Port:       3000, HostPort: 18080, ContainerID: "c1",
	}}

	routes, diagnostics := routesForServer(apps, serverID, DefaultBackendHost, multiNode(), nil)
	if len(diagnostics) != 0 {
		t.Fatalf("diagnostics = %#v, want none", diagnostics)
	}
	if len(routes) != 2 {
		t.Fatalf("routes = %#v, want one per domain", routes)
	}
	primary, alias := routes[0], routes[1]
	if primary.Domain != "app.example.com" || primary.Name != "" || primary.Service != "" {
		t.Errorf("primary route = %#v, want the legacy unnamed route", primary)
	}
	if alias.Domain != "www.example.com" || alias.Name != AliasRouteName(appID, "www.example.com") {
		t.Errorf("alias route = %#v, want stable alias routers", alias)
	}
	if alias.Service != serviceName(appID) {
		t.Errorf("alias service = %q, want the shared application service", alias.Service)
	}
	if primary.Target != alias.Target {
		t.Errorf("targets differ: %q vs %q, want one backend behind two names", primary.Target, alias.Target)
	}

	cfg := BuildConfig(routes, nil, nil, "", "")
	legacy := serviceName(appID)
	for _, name := range []string{legacy + "-web", AliasRouteName(appID, "www.example.com") + "-web"} {
		router, ok := cfg.Routers[name]
		if !ok {
			t.Fatalf("missing router %q in %#v", name, cfg.Routers)
		}
		if router.Service != legacy {
			t.Errorf("router %q serves %q, want the shared service %q", name, router.Service, legacy)
		}
	}
	if len(cfg.Services) != 1 {
		t.Fatalf("services = %#v, want exactly the shared application service", cfg.Services)
	}
}

// TestRemovingADomainRemovesOnlyItsRouter proves router removal is scoped:
// dropping the alias route from the input removes only the alias routers,
// and the surviving primary renders exactly the single-domain document.
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

// TestRoutesForServerHoldsBackDuplicateDomainsAcrossApplications proves the
// per-node duplicate rule covers aliases: every binding of a host two
// applications claim is held back, like the single-domain rule.
func TestRoutesForServerHoldsBackDuplicateDomainsAcrossApplications(t *testing.T) {
	serverID := uuid.New()
	first, second := uuid.New(), uuid.New()
	apps := []ProxiedApplication{
		{ID: first, ServerID: serverID, BaseDomain: "app.example.com",
			Domains: []string{"app.example.com"}, Port: 3000, ContainerID: "c1"},
		{ID: second, ServerID: serverID, BaseDomain: "other.example.com",
			Domains: []string{"other.example.com", "app.example.com"}, Port: 3000, ContainerID: "c1"},
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

// TestRouteCertificateReusesWildcardForSibling proves per-domain TLS (JUS-89):
// a wildcard intent recorded for one domain activates the sibling's HTTPS
// router through the same resolver, while an intent recorded for only one
// domain leaves the other HTTP-only with a stale-host diagnostic.
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
		ID: appID, ServerID: serverID, BaseDomain: "app.example.com",
		Domains:     []string{"app.example.com", "api.example.com"},
		Port:        3000,
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

// TestRouteCertificatePerDomainIsolation proves an intent recorded for one
// domain never activates another: the uncertified route stays HTTP-only and
// explains itself through the stale-host diagnostic.
func TestRouteCertificatePerDomainIsolation(t *testing.T) {
	serverID := uuid.New()
	appID := uuid.New()
	apps := []ProxiedApplication{{
		ID: appID, ServerID: serverID, BaseDomain: "app.example.com",
		Domains:     []string{"app.example.com", "www.example.com"},
		Port:        3000,
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
