package proxy

import (
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestMatchZone(t *testing.T) {
	cases := []struct {
		name   string
		zones  []string
		domain string
		want   string
	}{
		{name: "exact", zones: []string{"example.com"}, domain: "example.com", want: "example.com"},
		{name: "subdomain", zones: []string{"example.com"}, domain: "app.example.com", want: "example.com"},
		{name: "deeper subdomain", zones: []string{"example.com"}, domain: "a.b.example.com", want: "example.com"},
		{name: "label boundary", zones: []string{"example.com"}, domain: "notexample.com", want: ""},
		{name: "suffix inside label", zones: []string{"example.com"}, domain: "example.com.evil.net", want: ""},
		{name: "longest match wins", zones: []string{"example.com", "sub.example.com"}, domain: "app.sub.example.com", want: "sub.example.com"},
		{name: "case normalized", zones: []string{"Example.COM"}, domain: "App.Example.com", want: "example.com"},
		{name: "no zone", zones: nil, domain: "example.com", want: ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := MatchZone(tc.zones, tc.domain); got != tc.want {
				t.Fatalf("MatchZone(%v, %q) = %q, want %q", tc.zones, tc.domain, got, tc.want)
			}
		})
	}
}

func TestNormalizeZones(t *testing.T) {
	zones, err := normalizeZones([]string{" Example.COM ", "example.org", "example.com"})
	if err != nil {
		t.Fatalf("normalizeZones: %v", err)
	}
	if len(zones) != 2 || zones[0] != "example.com" || zones[1] != "example.org" {
		t.Fatalf("zones = %v, want the deduplicated, sorted lowercased list", zones)
	}

	cases := []struct {
		name  string
		zones []string
	}{
		{name: "empty", zones: nil},
		{name: "invalid", zones: []string{"*.example.com"}},
		{name: "too many", zones: make([]string, MaxProviderZones+1)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			zones := tc.zones
			if tc.name == "too many" {
				for i := range zones {
					zones[i] = "z" + strings.Repeat("a", i%10) + ".example.com"
				}
			}
			if _, err := normalizeZones(zones); !errors.Is(err, ErrValidation) {
				t.Fatalf("normalizeZones(%v) = %v, want ErrValidation", zones, err)
			}
		})
	}
}

func TestProviderNaming(t *testing.T) {
	if got := DNSResolverName(ProviderCloudflare); got != "letsencrypt-dns-cloudflare" {
		t.Errorf("DNSResolverName(cloudflare) = %q", got)
	}
	if got := DNSProviderEnvVar(ProviderDigitalOcean); got != "DO_AUTH_TOKEN" {
		t.Errorf("DNSProviderEnvVar(digitalocean) = %q", got)
	}
	if DNSProviderTypeAllowed("route53") {
		t.Error("route53 is not part of the allowlist")
	}
	if DNSResolverName("route53") != "" || DNSProviderEnvVar("route53") != "" {
		t.Error("unknown types must not resolve to a name or variable")
	}
}

func TestEnvFingerprintIsKeyedAndRotationSensitive(t *testing.T) {
	env := []string{"CF_DNS_API_TOKEN=secret-token"}
	first := envFingerprint("key-a", env)
	if first != envFingerprint("key-a", []string{"CF_DNS_API_TOKEN=secret-token"}) {
		t.Fatal("fingerprint is not deterministic")
	}
	if first == envFingerprint("key-b", env) {
		t.Fatal("fingerprint must depend on the deployment key")
	}
	if first == envFingerprint("key-a", []string{"CF_DNS_API_TOKEN=rotated"}) {
		t.Fatal("fingerprint must change when a credential rotates")
	}
	if strings.Contains(first, "secret-token") {
		t.Fatal("fingerprint leaks the credential value")
	}
	if envFingerprint("key-a", nil) == first {
		t.Fatal("empty environment must not fingerprint like a credentialed one")
	}
}

func TestTraefikEnvOnlyReferencedProviders(t *testing.T) {
	cloudflareID, digitaloceanID, unusedID := uuid.New(), uuid.New(), uuid.New()
	access := map[uuid.UUID]providerAccess{
		cloudflareID:   {provider: DNSProvider{ID: cloudflareID, Provider: ProviderCloudflare, Enabled: true}, credential: "cf-token"},
		digitaloceanID: {provider: DNSProvider{ID: digitaloceanID, Provider: ProviderDigitalOcean, Enabled: true}, credential: "do-token"},
		unusedID:       {provider: DNSProvider{ID: unusedID, Provider: ProviderCloudflare, Enabled: true}, credential: "unused-token"},
	}
	routes := []Route{
		{AppID: uuid.New(), Domain: "a.example.com", Certificate: &RouteCertificate{Resolver: DefaultResolverName}},
		{AppID: uuid.New(), Domain: "b.example.com", Certificate: &RouteCertificate{
			Resolver: DNSResolverName(ProviderCloudflare), DNSProviderID: cloudflareID, DNSProviderType: ProviderCloudflare,
		}},
		{AppID: uuid.New(), Domain: "c.example.org", Certificate: &RouteCertificate{
			Resolver: DNSResolverName(ProviderDigitalOcean), DNSProviderID: digitaloceanID, DNSProviderType: ProviderDigitalOcean,
		}},
	}
	env := traefikEnv(routes, access)
	want := []string{"CF_DNS_API_TOKEN=cf-token", "DO_AUTH_TOKEN=do-token"}
	if len(env) != len(want) {
		t.Fatalf("env = %v, want %v", env, want)
	}
	for i := range want {
		if env[i] != want[i] {
			t.Fatalf("env = %v, want %v", env, want)
		}
	}
	if strings.Contains(strings.Join(env, ","), "unused-token") {
		t.Fatal("a provider referenced by no active certificate leaked into the environment")
	}
}

func TestTraefikEnvSkipsUnopenableCredential(t *testing.T) {
	providerID := uuid.New()
	access := map[uuid.UUID]providerAccess{
		providerID: {
			provider: DNSProvider{ID: providerID, Provider: ProviderCloudflare, Enabled: true},
			err:      errors.New("bad key"),
		},
	}
	env := traefikEnv([]Route{{Certificate: &RouteCertificate{DNSProviderID: providerID, DNSProviderType: ProviderCloudflare}}}, access)
	if len(env) != 0 {
		t.Fatalf("env = %v, want none for an unopenable credential", env)
	}
}

func TestResolveRouteCertificate(t *testing.T) {
	providerID := uuid.New()
	providers := map[uuid.UUID]providerAccess{
		providerID: {
			provider:   DNSProvider{ID: providerID, Provider: ProviderCloudflare, Zones: []string{"example.com"}, Enabled: true},
			credential: "token",
		},
	}
	disabledProviders := map[uuid.UUID]providerAccess{
		providerID: {
			provider: DNSProvider{ID: providerID, Provider: ProviderCloudflare, Zones: []string{"example.com"}, Enabled: false},
		},
	}
	appID := uuid.New()
	cases := []struct {
		name       string
		app        ProxiedApplication
		providers  map[uuid.UUID]providerAccess
		wantActive bool
		wantReason string
	}{
		{name: "no configuration", app: ProxiedApplication{ID: appID, BaseDomain: "app.example.com"}},
		{
			name: "explicitly disabled",
			app: ProxiedApplication{ID: appID, BaseDomain: "app.example.com", Certificate: &CertificateIntent{
				Domain: "app.example.com", Enabled: false,
			}},
		},
		{
			name: "stale recorded domain",
			app: ProxiedApplication{ID: appID, BaseDomain: "app.example.com", Certificate: &CertificateIntent{
				Domain: "old.example.com", Enabled: true, Challenge: ChallengeHTTP01,
			}},
			wantReason: "recorded for",
		},
		{
			name: "http-01 active",
			app: ProxiedApplication{ID: appID, BaseDomain: "app.example.com", Certificate: &CertificateIntent{
				Domain: "app.example.com", Enabled: true, Challenge: ChallengeHTTP01,
			}},
			wantActive: true,
		},
		{
			name: "wildcard requires dns-01",
			app: ProxiedApplication{ID: appID, BaseDomain: "app.example.com", Certificate: &CertificateIntent{
				Domain: "app.example.com", Enabled: true, Challenge: ChallengeHTTP01, Wildcard: true,
			}},
			wantReason: "wildcard",
		},
		{
			name: "dns-01 wildcard active",
			app: ProxiedApplication{ID: appID, BaseDomain: "app.example.com", Certificate: &CertificateIntent{
				Domain: "app.example.com", Enabled: true, Challenge: ChallengeDNS01, Wildcard: true,
				DNSProviderID: providerID,
			}},
			wantActive: true,
		},
		{
			name: "provider disabled",
			app: ProxiedApplication{ID: appID, BaseDomain: "app.example.com", Certificate: &CertificateIntent{
				Domain: "app.example.com", Enabled: true, Challenge: ChallengeDNS01,
				DNSProviderID: providerID,
			}},
			providers:  disabledProviders,
			wantReason: "provider is disabled",
		},
		{
			name: "unknown provider",
			app: ProxiedApplication{ID: appID, BaseDomain: "app.example.com", Certificate: &CertificateIntent{
				Domain: "app.example.com", Enabled: true, Challenge: ChallengeDNS01,
				DNSProviderID: uuid.New(),
			}},
			wantReason: "provider is disabled",
		},
		{
			name: "domain outside zones",
			app: ProxiedApplication{ID: appID, BaseDomain: "app.example.org", Certificate: &CertificateIntent{
				Domain: "app.example.org", Enabled: true, Challenge: ChallengeDNS01,
				DNSProviderID: providerID,
			}},
			wantReason: "not under any zone",
		},
		{
			name: "unknown challenge",
			app: ProxiedApplication{ID: appID, BaseDomain: "app.example.com", Certificate: &CertificateIntent{
				Domain: "app.example.com", Enabled: true, Challenge: ChallengeMode("tls-alpn-01"),
			}},
			wantReason: "unknown challenge",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			providerMap := tc.providers
			if providerMap == nil {
				providerMap = providers
			}
			domain := NormalizeDomain(tc.app.BaseDomain)
			certificate, reason := resolveRouteCertificate(tc.app, domain, providerMap)
			if tc.wantActive {
				if certificate == nil || reason != "" {
					t.Fatalf("certificate = %#v reason = %q, want active", certificate, reason)
				}
				return
			}
			if certificate != nil {
				t.Fatalf("certificate = %#v, want inactive", certificate)
			}
			if tc.wantReason == "" {
				if reason != "" {
					t.Fatalf("reason = %q, want none", reason)
				}
				return
			}
			if !strings.Contains(reason, tc.wantReason) {
				t.Fatalf("reason = %q, want it to contain %q", reason, tc.wantReason)
			}
		})
	}
}

func TestResolveRouteCertificateWildcardUsesMatchedZone(t *testing.T) {
	providerID := uuid.New()
	providers := map[uuid.UUID]providerAccess{
		providerID: {
			provider: DNSProvider{
				ID: providerID, Provider: ProviderCloudflare, Enabled: true,
				Zones: []string{"example.com", "sub.example.com"},
			},
			credential: "token",
		},
	}
	app := ProxiedApplication{
		ID:         uuid.New(),
		BaseDomain: "app.sub.example.com",
		Certificate: &CertificateIntent{
			Domain: "app.sub.example.com", Enabled: true, Challenge: ChallengeDNS01, Wildcard: true,
			DNSProviderID: providerID,
		},
	}
	certificate, reason := resolveRouteCertificate(app, "app.sub.example.com", providers)
	if reason != "" {
		t.Fatalf("reason = %q, want an active certificate", reason)
	}
	if certificate.WildcardMain != "sub.example.com" {
		t.Fatalf("wildcard main = %q, want the most specific matching zone", certificate.WildcardMain)
	}
}

func TestOpenCredentialRejectsForeignKey(t *testing.T) {
	sealed, err := sealCredential("right-key", "token")
	if err != nil {
		t.Fatalf("sealCredential: %v", err)
	}
	if _, err := openCredential("other-key", sealed); err == nil {
		t.Fatal("openCredential accepted a credential sealed with another key")
	}
	plain, err := openCredential("right-key", sealed)
	if err != nil || plain != "token" {
		t.Fatalf("openCredential = (%q, %v), want the token", plain, err)
	}
}
