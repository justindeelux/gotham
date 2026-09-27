package proxy

import (
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/containers"
)

func TestValidateDomain(t *testing.T) {
	valid := []string{
		"example.com",
		"app.example.com",
		"a-b.example.co.uk",
		"localhost",
		"xn--bcher-kva.example",
		strings.Repeat("a", 63) + ".example.com",
	}
	for _, domain := range valid {
		if err := ValidateDomain(domain); err != nil {
			t.Errorf("ValidateDomain(%q) = %v, want nil", domain, err)
		}
	}

	invalid := []string{
		"",
		" example.com",
		"example.com ",
		"Example.com",
		"*.example.com",
		"example.com.",
		"-example.com",
		"example-.com",
		"example..com",
		"example.com/path",
		"under_score.example.com",
		"example.com:443",
		"exa mple.com",
		"example.com`) || Host(`evil.com",
		"\"example.com\"",
		"café.example.com",
		strings.Repeat("a", 64) + ".example.com",
		strings.Repeat("a", 250) + ".example.com",
	}
	for _, domain := range invalid {
		err := ValidateDomain(domain)
		if !errors.Is(err, ErrValidation) {
			t.Errorf("ValidateDomain(%q) = %v, want ErrValidation", domain, err)
		}
	}
}

func TestNormalizeDomain(t *testing.T) {
	cases := map[string]string{
		"  App.Example.COM  ": "app.example.com",
		"example.com":         "example.com",
		"":                    "",
	}
	for in, want := range cases {
		if got := NormalizeDomain(in); got != want {
			t.Errorf("NormalizeDomain(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestBuildConfigEmpty(t *testing.T) {
	cfg := BuildConfig(nil)
	if len(cfg.Routers) != 0 || len(cfg.Services) != 0 {
		t.Fatalf("empty route set produced routers=%d services=%d, want 0/0", len(cfg.Routers), len(cfg.Services))
	}
	if len(cfg.Middlewares) != 0 {
		t.Fatalf("empty route set produced %d middlewares, want 0", len(cfg.Middlewares))
	}
	if cfg.EntryPoints[EntryPointWeb].Address != ":80" ||
		cfg.EntryPoints[EntryPointWebSecure].Address != ":443" ||
		cfg.EntryPoints[EntryPointInternal].Address != ":8080" {
		t.Fatalf("unexpected entrypoints: %#v", cfg.EntryPoints)
	}
	if _, ok := cfg.CertificatesResolvers[DefaultResolverName]; !ok {
		t.Fatalf("missing resolver %q", DefaultResolverName)
	}
}

func TestBuildConfigRouteProducesHTTPForwardingRouter(t *testing.T) {
	appID := uuid.MustParse("11111111-2222-3333-4444-555555555555")
	cfg := BuildConfig([]Route{{AppID: appID, Domain: "app.example.com", Target: "http://172.17.0.1:3000"}})

	name := "app-" + appID.String()
	if len(cfg.Routers) != 1 {
		t.Fatalf("routers = %#v, want only the HTTP router before BE-6.2", cfg.Routers)
	}
	// The HTTP router must forward to the application (BE-6.1 acceptance);
	// the HTTPS router and redirect arrive with BE-6.2 certificates, and no
	// TLS router may exist here or Traefik would attempt ACME issuance now.
	forward, ok := cfg.Routers[name+"-web"]
	if !ok {
		t.Fatalf("missing HTTP router %q in %#v", name+"-web", cfg.Routers)
	}
	if forward.Rule != "Host(`app.example.com`)" || forward.Service != name {
		t.Errorf("HTTP router = %#v, want rule %q service %q", forward, "Host(`app.example.com`)", name)
	}
	if len(forward.EntryPoints) != 1 || forward.EntryPoints[0] != EntryPointWeb {
		t.Errorf("HTTP entrypoints = %#v", forward.EntryPoints)
	}
	if forward.TLS != nil {
		t.Errorf("HTTP router must not carry TLS: %#v", forward.TLS)
	}
	if len(forward.Middlewares) != 0 {
		t.Errorf("HTTP router must forward, not redirect, before BE-6.2: %#v", forward.Middlewares)
	}
	if len(cfg.Middlewares) != 0 {
		t.Errorf("no middleware may be emitted before BE-6.2: %#v", cfg.Middlewares)
	}

	service, ok := cfg.Services[name]
	if !ok {
		t.Fatalf("missing service %q", name)
	}
	if len(service.LoadBalancer.Servers) != 1 || service.LoadBalancer.Servers[0].URL != "http://172.17.0.1:3000" {
		t.Errorf("service backends = %#v", service.LoadBalancer.Servers)
	}

	resolver := cfg.CertificatesResolvers[DefaultResolverName]
	if resolver.ACME.Storage != TraefikAcmeStorage {
		t.Errorf("acme storage = %q, want %q", resolver.ACME.Storage, TraefikAcmeStorage)
	}
	if resolver.ACME.HTTPChallenge == nil || resolver.ACME.HTTPChallenge.EntryPoint != EntryPointWeb {
		t.Errorf("http challenge = %#v", resolver.ACME.HTTPChallenge)
	}
}

func TestBuildConfigKeepsRoutesIndependent(t *testing.T) {
	first := uuid.MustParse("11111111-2222-3333-4444-555555555555")
	second := uuid.MustParse("66666666-7777-8888-9999-aaaaaaaaaaaa")
	cfg := BuildConfig([]Route{
		{AppID: first, Domain: "one.example.com", Target: "http://172.17.0.1:3000"},
		{AppID: second, Domain: "two.example.com", Target: "http://172.17.0.1:3001"},
	})
	if len(cfg.Routers) != 2 || len(cfg.Services) != 2 || len(cfg.Middlewares) != 0 {
		t.Fatalf("routers=%d services=%d middlewares=%d, want 2/2/0",
			len(cfg.Routers), len(cfg.Services), len(cfg.Middlewares))
	}
	if cfg.Services["app-"+first.String()].LoadBalancer.Servers[0].URL != "http://172.17.0.1:3000" ||
		cfg.Services["app-"+second.String()].LoadBalancer.Servers[0].URL != "http://172.17.0.1:3001" {
		t.Fatalf("services crossed: %#v", cfg.Services)
	}
}

func TestRoutesForServer(t *testing.T) {
	serverA := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	serverB := uuid.MustParse("22222222-2222-2222-2222-222222222222")
	appA := uuid.MustParse("aaaaaaaa-1111-1111-1111-111111111111")
	appB := uuid.MustParse("bbbbbbbb-2222-2222-2222-222222222222")

	apps := []ProxiedApplication{
		{ID: appA, ServerID: serverA, BaseDomain: "  App.Example.COM ", Port: 3000, HostPort: 18080, ContainerID: "c1"},
		{ID: appB, ServerID: serverB, BaseDomain: "other.example.com", Port: 3001, HostPort: 18081, ContainerID: "c2"},
	}
	routes, diagnostics := routesForServer(apps, serverA, DefaultBackendHost, nil)
	if len(diagnostics) != 0 {
		t.Fatalf("diagnostics = %#v, want none (pinned ports fall back)", diagnostics)
	}
	if len(routes) != 1 {
		t.Fatalf("routes = %#v, want only the server A route", routes)
	}
	if routes[0].AppID != appA || routes[0].Domain != "app.example.com" ||
		routes[0].Target != "http://172.17.0.1:18080" {
		t.Errorf("route = %#v", routes[0])
	}

	custom, diagnostics := routesForServer(apps, serverA, "10.0.0.1", nil)
	if len(diagnostics) != 0 {
		t.Fatalf("diagnostics = %#v, want none", diagnostics)
	}
	if custom[0].Target != "http://10.0.0.1:18080" {
		t.Errorf("custom backend target = %q", custom[0].Target)
	}
}

func TestRoutesForServerDiagnosesUnroutableRows(t *testing.T) {
	serverID := uuid.New()
	appID := uuid.New()

	cases := map[string]struct {
		app        ProxiedApplication
		containers []containers.Container
		want       string
	}{
		"invalid domain": {ProxiedApplication{ID: appID, ServerID: serverID, BaseDomain: "bad domain", Port: 80, HostPort: 8080, ContainerID: "c"}, nil, "invalid domain"},
		"no port":        {ProxiedApplication{ID: appID, ServerID: serverID, BaseDomain: "app.example.com", HostPort: 8080, ContainerID: "c"}, nil, "no container port"},
		"pending":        {ProxiedApplication{ID: appID, ServerID: serverID, BaseDomain: "app.example.com", Port: 80, HostPort: 8080}, nil, "no running deployment"},
		"container gone": {ProxiedApplication{ID: appID, ServerID: serverID, BaseDomain: "app.example.com", Port: 80, ContainerID: "gone"}, nil, "no running deployment container"},
		"not published": {
			app:        ProxiedApplication{ID: appID, ServerID: serverID, BaseDomain: "app.example.com", Port: 80, ContainerID: "c1"},
			containers: []containers.Container{{ID: "c1", Name: "app", State: "running", Ports: []string{"8080:8081"}}},
			want:       "container port 80 is not published",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			routes, diagnostics := routesForServer([]ProxiedApplication{tc.app}, serverID, DefaultBackendHost, tc.containers)
			if len(routes) != 0 {
				t.Fatalf("routes = %#v, want none", routes)
			}
			if len(diagnostics) != 1 || !strings.Contains(diagnostics[0].Reason, tc.want) {
				t.Fatalf("diagnostics = %#v, want reason containing %q", diagnostics, tc.want)
			}
			if diagnostics[0].ApplicationID != appID || diagnostics[0].Domain != NormalizeDomain(tc.app.BaseDomain) {
				t.Errorf("diagnostic identity = %#v", diagnostics[0])
			}
		})
	}
}
