package proxy

import (
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
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

func TestBuildConfigRouteProducesSecureAndRedirectRouters(t *testing.T) {
	appID := uuid.MustParse("11111111-2222-3333-4444-555555555555")
	cfg := BuildConfig([]Route{{AppID: appID, Domain: "app.example.com", Target: "http://172.17.0.1:3000"}})

	name := "app-" + appID.String()
	secure, ok := cfg.Routers[name]
	if !ok {
		t.Fatalf("missing secure router %q in %#v", name, cfg.Routers)
	}
	if secure.Rule != "Host(`app.example.com`)" {
		t.Errorf("secure rule = %q", secure.Rule)
	}
	if secure.Service != name {
		t.Errorf("secure service = %q, want %q", secure.Service, name)
	}
	if len(secure.EntryPoints) != 1 || secure.EntryPoints[0] != EntryPointWebSecure {
		t.Errorf("secure entrypoints = %#v", secure.EntryPoints)
	}
	if secure.TLS == nil || secure.TLS.CertResolver != DefaultResolverName {
		t.Errorf("secure TLS = %#v, want resolver %q", secure.TLS, DefaultResolverName)
	}
	if len(secure.Middlewares) != 0 {
		t.Errorf("secure router must not redirect: %#v", secure.Middlewares)
	}

	redirect, ok := cfg.Routers[name+"-web"]
	if !ok {
		t.Fatalf("missing redirect router %q", name+"-web")
	}
	if redirect.Rule != secure.Rule || redirect.Service != name {
		t.Errorf("redirect router = %#v, want rule %q service %q", redirect, secure.Rule, name)
	}
	if len(redirect.EntryPoints) != 1 || redirect.EntryPoints[0] != EntryPointWeb {
		t.Errorf("redirect entrypoints = %#v", redirect.EntryPoints)
	}
	if redirect.TLS != nil {
		t.Errorf("redirect router must not carry TLS: %#v", redirect.TLS)
	}
	if len(redirect.Middlewares) != 1 || redirect.Middlewares[0] != HTTPRedirectMiddleware {
		t.Errorf("redirect middlewares = %#v", redirect.Middlewares)
	}

	service, ok := cfg.Services[name]
	if !ok {
		t.Fatalf("missing service %q", name)
	}
	if len(service.LoadBalancer.Servers) != 1 || service.LoadBalancer.Servers[0].URL != "http://172.17.0.1:3000" {
		t.Errorf("service backends = %#v", service.LoadBalancer.Servers)
	}

	middleware, ok := cfg.Middlewares[HTTPRedirectMiddleware]
	if !ok || middleware.RedirectScheme == nil {
		t.Fatalf("missing redirect middleware: %#v", cfg.Middlewares)
	}
	if middleware.RedirectScheme.Scheme != "https" || !middleware.RedirectScheme.Permanent {
		t.Errorf("redirect middleware = %#v", middleware.RedirectScheme)
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
	if len(cfg.Routers) != 4 || len(cfg.Services) != 2 || len(cfg.Middlewares) != 1 {
		t.Fatalf("routers=%d services=%d middlewares=%d, want 4/2/1",
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
		{ID: appA, ServerID: serverA, BaseDomain: "  App.Example.COM ", Port: 3000, HostPort: 18080},
		{ID: appB, ServerID: serverB, BaseDomain: "other.example.com", Port: 3001, HostPort: 18081},
	}
	routes, err := routesForServer(apps, serverA, DefaultBackendHost)
	if err != nil {
		t.Fatalf("routesForServer: %v", err)
	}
	if len(routes) != 1 {
		t.Fatalf("routes = %#v, want only the server A route", routes)
	}
	if routes[0].AppID != appA || routes[0].Domain != "app.example.com" ||
		routes[0].Target != "http://172.17.0.1:18080" {
		t.Errorf("route = %#v", routes[0])
	}

	custom, err := routesForServer(apps, serverA, "10.0.0.1")
	if err != nil {
		t.Fatalf("routesForServer(custom host): %v", err)
	}
	if custom[0].Target != "http://10.0.0.1:18080" {
		t.Errorf("custom backend target = %q", custom[0].Target)
	}
}

func TestRoutesForServerRejectsUnroutableRows(t *testing.T) {
	serverID := uuid.New()
	appID := uuid.New()

	cases := map[string]ProxiedApplication{
		"invalid domain": {ID: appID, ServerID: serverID, BaseDomain: "bad domain", Port: 80, HostPort: 8080},
		"no port":        {ID: appID, ServerID: serverID, BaseDomain: "app.example.com", HostPort: 8080},
		"no host port":   {ID: appID, ServerID: serverID, BaseDomain: "app.example.com", Port: 8080},
	}
	for name, app := range cases {
		if _, err := routesForServer([]ProxiedApplication{app}, serverID, DefaultBackendHost); !errors.Is(err, ErrValidation) {
			t.Errorf("%s: err = %v, want ErrValidation", name, err)
		}
	}
}
