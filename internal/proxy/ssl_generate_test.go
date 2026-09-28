package proxy

import (
	"strings"
	"testing"

	"github.com/google/uuid"
)

// sslGoldenProviders is the provider set the resolver goldens render from.
func sslGoldenProviders() []DNSProvider {
	return []DNSProvider{
		{
			ID:               uuid.MustParse("aaaaaaaa-0000-0000-0000-000000000001"),
			Provider:         ProviderCloudflare,
			Name:             "prod cloudflare",
			Zones:            []string{"example.com"},
			Enabled:          true,
			SealedCredential: "sealed-must-not-be-rendered",
		},
		{
			ID:               uuid.MustParse("bbbbbbbb-0000-0000-0000-000000000002"),
			Provider:         ProviderDigitalOcean,
			Zones:            []string{"example.org"},
			Enabled:          true,
			SealedCredential: "sealed-must-not-be-rendered",
		},
	}
}

const goldenSSLStaticYAML = `entryPoints:
  traefik:
    address: :8080
  web:
    address: :80
  websecure:
    address: :443
ping:
  entryPoint: traefik
providers:
  file:
    directory: /etc/traefik/dynamic
    watch: true
certificatesResolvers:
  letsencrypt:
    acme:
      email: ops@example.com
      storage: /acme/acme.json
      httpChallenge:
        entryPoint: web
  letsencrypt-dns-cloudflare:
    acme:
      email: ops@example.com
      storage: /acme/acme.json
      dnsChallenge:
        provider: cloudflare
  letsencrypt-dns-digitalocean:
    acme:
      email: ops@example.com
      storage: /acme/acme.json
      dnsChallenge:
        provider: digitalocean
`

const goldenSSLStaticTOML = `[entryPoints]
[entryPoints.traefik]
address = ':8080'

[entryPoints.web]
address = ':80'

[entryPoints.websecure]
address = ':443'

[ping]
entryPoint = 'traefik'

[providers]
[providers.file]
directory = '/etc/traefik/dynamic'
watch = true

[certificatesResolvers]
[certificatesResolvers.letsencrypt]
[certificatesResolvers.letsencrypt.acme]
email = 'ops@example.com'
storage = '/acme/acme.json'

[certificatesResolvers.letsencrypt.acme.httpChallenge]
entryPoint = 'web'

[certificatesResolvers.letsencrypt-dns-cloudflare]
[certificatesResolvers.letsencrypt-dns-cloudflare.acme]
email = 'ops@example.com'
storage = '/acme/acme.json'

[certificatesResolvers.letsencrypt-dns-cloudflare.acme.dnsChallenge]
provider = 'cloudflare'

[certificatesResolvers.letsencrypt-dns-digitalocean]
[certificatesResolvers.letsencrypt-dns-digitalocean.acme]
email = 'ops@example.com'
storage = '/acme/acme.json'

[certificatesResolvers.letsencrypt-dns-digitalocean.acme.dnsChallenge]
provider = 'digitalocean'
`

// goldenHTTPSCertificate is the resolved certificate activation of the
// records below: an HTTP-01 route and a wildcard DNS-01 route.
func http01Certificate() *RouteCertificate {
	return &RouteCertificate{Resolver: DefaultResolverName}
}

func wildcardCertificate() *RouteCertificate {
	return &RouteCertificate{
		Resolver:        DNSResolverName(ProviderCloudflare),
		DNSProviderID:   uuid.MustParse("aaaaaaaa-0000-0000-0000-000000000001"),
		DNSProviderType: ProviderCloudflare,
		WildcardMain:    "example.com",
	}
}

const goldenSSLHTTP01DynamicYAML = `http:
  routers:
    app-11111111-2222-3333-4444-555555555555-web:
      rule: Host(§app.example.com§)
      service: app-11111111-2222-3333-4444-555555555555
      entryPoints:
        - web
      middlewares:
        - gotham-https-redirect
    app-11111111-2222-3333-4444-555555555555-websecure:
      rule: Host(§app.example.com§)
      service: app-11111111-2222-3333-4444-555555555555
      entryPoints:
        - websecure
      tls:
        certResolver: letsencrypt
  services:
    app-11111111-2222-3333-4444-555555555555:
      loadBalancer:
        servers:
          - url: http://172.17.0.1:3000
  middlewares:
    gotham-https-redirect:
      redirectScheme:
        scheme: https
        permanent: true
`

const goldenSSLHTTP01DynamicTOML = `[http]
[http.routers]
[http.routers.app-11111111-2222-3333-4444-555555555555-web]
rule = 'Host(§app.example.com§)'
service = 'app-11111111-2222-3333-4444-555555555555'
entryPoints = ['web']
middlewares = ['gotham-https-redirect']

[http.routers.app-11111111-2222-3333-4444-555555555555-websecure]
rule = 'Host(§app.example.com§)'
service = 'app-11111111-2222-3333-4444-555555555555'
entryPoints = ['websecure']

[http.routers.app-11111111-2222-3333-4444-555555555555-websecure.tls]
certResolver = 'letsencrypt'

[http.services]
[http.services.app-11111111-2222-3333-4444-555555555555]
[http.services.app-11111111-2222-3333-4444-555555555555.loadBalancer]
[[http.services.app-11111111-2222-3333-4444-555555555555.loadBalancer.servers]]
url = 'http://172.17.0.1:3000'

[http.middlewares]
[http.middlewares.gotham-https-redirect]
[http.middlewares.gotham-https-redirect.redirectScheme]
scheme = 'https'
permanent = true
`

const goldenSSLWildcardDynamicYAML = `http:
  routers:
    app-11111111-2222-3333-4444-555555555555-web:
      rule: Host(§app.example.com§)
      service: app-11111111-2222-3333-4444-555555555555
      entryPoints:
        - web
      middlewares:
        - gotham-https-redirect
    app-11111111-2222-3333-4444-555555555555-websecure:
      rule: Host(§app.example.com§)
      service: app-11111111-2222-3333-4444-555555555555
      entryPoints:
        - websecure
      tls:
        certResolver: letsencrypt-dns-cloudflare
        domains:
          - main: example.com
            sans:
              - '*.example.com'
  services:
    app-11111111-2222-3333-4444-555555555555:
      loadBalancer:
        servers:
          - url: http://172.17.0.1:3000
  middlewares:
    gotham-https-redirect:
      redirectScheme:
        scheme: https
        permanent: true
`

const goldenSSLWildcardDynamicTOML = `[http]
[http.routers]
[http.routers.app-11111111-2222-3333-4444-555555555555-web]
rule = 'Host(§app.example.com§)'
service = 'app-11111111-2222-3333-4444-555555555555'
entryPoints = ['web']
middlewares = ['gotham-https-redirect']

[http.routers.app-11111111-2222-3333-4444-555555555555-websecure]
rule = 'Host(§app.example.com§)'
service = 'app-11111111-2222-3333-4444-555555555555'
entryPoints = ['websecure']

[http.routers.app-11111111-2222-3333-4444-555555555555-websecure.tls]
certResolver = 'letsencrypt-dns-cloudflare'

[[http.routers.app-11111111-2222-3333-4444-555555555555-websecure.tls.domains]]
main = 'example.com'
sans = ['*.example.com']

[http.services]
[http.services.app-11111111-2222-3333-4444-555555555555]
[http.services.app-11111111-2222-3333-4444-555555555555.loadBalancer]
[[http.services.app-11111111-2222-3333-4444-555555555555.loadBalancer.servers]]
url = 'http://172.17.0.1:3000'

[http.middlewares]
[http.middlewares.gotham-https-redirect]
[http.middlewares.gotham-https-redirect.redirectScheme]
scheme = 'https'
permanent = true
`

// TestGenerateStaticResolverGoldens pins every resolver shape: the HTTP-01
// default plus one DNS-01 resolver per enabled provider, with the shared
// storage/email and the credential never rendered.
func TestGenerateStaticResolverGoldens(t *testing.T) {
	cases := []struct {
		format Format
		golden string
	}{
		{format: FormatYAML, golden: goldenSSLStaticYAML},
		{format: FormatTOML, golden: goldenSSLStaticTOML},
	}
	for _, tc := range cases {
		files, err := Generate(BuildConfig(nil, sslGoldenProviders(), "ops@example.com"), tc.format)
		if err != nil {
			t.Fatalf("%s: Generate: %v", tc.format, err)
		}
		static := files[0]
		if static.Name != StaticFileName(tc.format) {
			t.Fatalf("%s: first file = %q, want the static document", tc.format, static.Name)
		}
		got := string(static.Content)
		if got != expandGolden(tc.golden) {
			t.Errorf("%s: static mismatch\n--- got ---\n%s\n--- want ---\n%s", tc.format, got, expandGolden(tc.golden))
		}
		if strings.Contains(got, "sealed-must-not-be-rendered") {
			t.Fatalf("%s: credential material was rendered into the configuration", tc.format)
		}
	}
}

// TestGenerateHTTPSActivationGoldens pins the HTTP-01 activation shape: the
// HTTP router carries the redirect middleware and an HTTPS router with the
// default resolver appears.
func TestGenerateHTTPSActivationGoldens(t *testing.T) {
	route := Route{
		AppID:       uuid.MustParse(goldenAppID),
		Domain:      "app.example.com",
		Target:      "http://172.17.0.1:3000",
		Certificate: http01Certificate(),
	}
	cases := []struct {
		format Format
		golden string
	}{
		{format: FormatYAML, golden: goldenSSLHTTP01DynamicYAML},
		{format: FormatTOML, golden: goldenSSLHTTP01DynamicTOML},
	}
	for _, tc := range cases {
		files, err := Generate(BuildConfig([]Route{route}, nil, ""), tc.format)
		if err != nil {
			t.Fatalf("%s: Generate: %v", tc.format, err)
		}
		dynamic := files[1]
		if dynamic.Name != DynamicFileName(tc.format) {
			t.Fatalf("%s: second file = %q, want the dynamic document", tc.format, dynamic.Name)
		}
		if got := string(dynamic.Content); got != expandGolden(tc.golden) {
			t.Errorf("%s: dynamic mismatch\n--- got ---\n%s\n--- want ---\n%s", tc.format, got, expandGolden(tc.golden))
		}
	}
}

// TestGenerateWildcardActivationGoldens pins the DNS-01 wildcard shape: the
// route resolves through the provider's resolver and requests the zone
// wildcard via tls.domains main/sans.
func TestGenerateWildcardActivationGoldens(t *testing.T) {
	route := Route{
		AppID:       uuid.MustParse(goldenAppID),
		Domain:      "app.example.com",
		Target:      "http://172.17.0.1:3000",
		Certificate: wildcardCertificate(),
	}
	cases := []struct {
		format Format
		golden string
	}{
		{format: FormatYAML, golden: goldenSSLWildcardDynamicYAML},
		{format: FormatTOML, golden: goldenSSLWildcardDynamicTOML},
	}
	for _, tc := range cases {
		files, err := Generate(BuildConfig([]Route{route}, sslGoldenProviders(), "ops@example.com"), tc.format)
		if err != nil {
			t.Fatalf("%s: Generate: %v", tc.format, err)
		}
		dynamic := files[1]
		if got := string(dynamic.Content); got != expandGolden(tc.golden) {
			t.Errorf("%s: dynamic mismatch\n--- got ---\n%s\n--- want ---\n%s", tc.format, got, expandGolden(tc.golden))
		}
	}
}

// TestGenerateInactiveCertificateKeepsHTTPOnly proves a route whose
// certificate configuration is not active (nil resolution) keeps the exact
// BE-6.1 plain-HTTP shape: no HTTPS router, no redirect middleware.
func TestGenerateInactiveCertificateKeepsHTTPOnly(t *testing.T) {
	route := Route{
		AppID:  uuid.MustParse(goldenAppID),
		Domain: "app.example.com",
		Target: "http://172.17.0.1:3000",
	}
	cfg := BuildConfig([]Route{route}, sslGoldenProviders(), "ops@example.com")
	if _, ok := cfg.Routers[serviceName(route.AppID)+"-websecure"]; ok {
		t.Fatal("inactive certificate emitted an HTTPS router")
	}
	web := cfg.Routers[serviceName(route.AppID)+"-web"]
	if len(web.Middlewares) != 0 {
		t.Fatalf("inactive certificate attached middlewares: %#v", web.Middlewares)
	}
	if len(cfg.Middlewares) != 0 {
		t.Fatalf("inactive certificate emitted middleware definitions: %#v", cfg.Middlewares)
	}

	// And the rendered bytes contain no TLS section or redirect at all.
	for _, format := range []Format{FormatYAML, FormatTOML} {
		files, err := Generate(cfg, format)
		if err != nil {
			t.Fatalf("%s: Generate: %v", format, err)
		}
		dynamic := string(files[1].Content)
		if strings.Contains(dynamic, "websecure") || strings.Contains(dynamic, "certResolver") || strings.Contains(dynamic, "redirectScheme") {
			t.Errorf("%s: inactive route leaked HTTPS configuration:\n%s", format, dynamic)
		}
	}
}

// TestGenerateResolverDeterminism proves the resolver set is a pure function
// of state: reversing the provider input order changes nothing.
func TestGenerateResolverDeterminism(t *testing.T) {
	providers := sslGoldenProviders()
	reversed := []DNSProvider{providers[1], providers[0]}
	for _, format := range []Format{FormatYAML, FormatTOML} {
		first, err := Generate(BuildConfig(nil, providers, "ops@example.com"), format)
		if err != nil {
			t.Fatalf("%s: %v", format, err)
		}
		second, err := Generate(BuildConfig(nil, reversed, "ops@example.com"), format)
		if err != nil {
			t.Fatalf("%s: %v", format, err)
		}
		if string(first[0].Content) != string(second[0].Content) {
			t.Errorf("%s: resolver order depends on input order:\n%s\n---\n%s",
				format, first[0].Content, second[0].Content)
		}
	}
}

// TestGenerateSkipsDisabledAndUnknownProviders proves disabled rows and
// unknown types never become resolvers (defense in depth behind the API).
func TestGenerateSkipsDisabledAndUnknownProviders(t *testing.T) {
	providers := []DNSProvider{
		{ID: uuid.New(), Provider: ProviderCloudflare, Zones: []string{"example.com"}, Enabled: false},
		{ID: uuid.New(), Provider: DNSProviderType("route53"), Zones: []string{"example.net"}, Enabled: true},
	}
	cfg := BuildConfig(nil, providers, "")
	if len(cfg.CertificatesResolvers) != 1 {
		t.Fatalf("resolvers = %#v, want only the HTTP-01 default", cfg.CertificatesResolvers)
	}
	if _, ok := cfg.CertificatesResolvers[DefaultResolverName]; !ok {
		t.Fatal("default HTTP-01 resolver missing")
	}
}
