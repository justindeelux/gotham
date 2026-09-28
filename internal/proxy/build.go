package proxy

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/google/uuid"
)

// Entrypoint and middleware names rendered into the generated documents.
const (
	// EntryPointWeb serves plain HTTP (port 80): ACME challenges and the
	// redirect routers live here.
	EntryPointWeb = "web"
	// EntryPointWebSecure serves HTTPS (port 443).
	EntryPointWebSecure = "websecure"
	// EntryPointInternal is the loopback-only entrypoint (port 8080) pinned
	// to /ping so the node agent can check the proxy's health without
	// exposing the endpoint to the network.
	EntryPointInternal = "traefik"
	// DefaultResolverName is the generated ACME certificate resolver.
	DefaultResolverName = "letsencrypt"
	// HTTPRedirectMiddleware is the shared http→https redirect middleware.
	// BE-6.2 attaches it to the web router once certificates are configured
	// and verified; emitting it earlier would break the plain-HTTP acceptance
	// path of BE-6.1.
	HTTPRedirectMiddleware = "gotham-https-redirect"
	// DefaultBackendHost is the docker0 bridge gateway: application
	// containers publish their ports on the host, and the Traefik container
	// reaches those published ports through the bridge gateway. Operators on
	// a node with a customized docker bip override it (proxy.Config.BackendHost).
	DefaultBackendHost = "172.17.0.1"
)

// Route is one domain binding derived from an application row: the router
// name comes from AppID, the rule from Domain, and the backend from Target.
type Route struct {
	// AppID identifies the application (stays in generated names for traceability).
	AppID uuid.UUID
	// Domain is the exact hostname to route, already lowercased and trimmed.
	Domain string
	// Target is the backend URL, e.g. http://172.17.0.1:3000.
	Target string
	// Certificate, when non-nil, activates the HTTPS router and the
	// HTTP→HTTPS redirect for this route. It is resolved from the
	// application's certificate configuration during generation, never from
	// the raw row: an inactive configuration leaves the route HTTP-only.
	Certificate *RouteCertificate
}

// RouteCertificate is the resolved certificate activation of one route.
type RouteCertificate struct {
	// Resolver names the certificate resolver from the static config: the
	// shared HTTP-01 default, or the DNS-01 resolver of the configured
	// provider.
	Resolver string
	// DNSProviderID identifies the provider whose credential the node
	// container needs; uuid.Nil for HTTP-01.
	DNSProviderID uuid.UUID
	// DNSProviderType selects the credential environment variable; empty for
	// HTTP-01.
	DNSProviderType DNSProviderType
	// WildcardBase, when set, requests a wildcard certificate for that base:
	// Traefik tls.domains main = the routed host (always covered), sans =
	// ["*."+WildcardBase]. A wildcard matches exactly one label, so the exact
	// host must be one of the requested names.
	WildcardBase string
}

// domainPattern matches a DNS hostname: lowercase labels of letters, digits
// and hyphens joined by dots. Wildcards, underscores, slashes and quotes are
// rejected so a stored domain can never escape the generated Host() rule.
var domainPattern = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?(\.[a-z0-9]([a-z0-9-]*[a-z0-9])?)*$`)

// ValidateDomain reports whether domain is safe to embed in a Traefik Host()
// rule. It is the security boundary between stored application state and the
// generated rule language, so anything that is not a plain hostname — a
// backtick, a space, a wildcard, an empty value — is rejected.
func ValidateDomain(domain string) error {
	if domain == "" {
		return fmt.Errorf("%w: domain is required", ErrValidation)
	}
	if len(domain) > 253 {
		return fmt.Errorf("%w: domain exceeds 253 characters", ErrValidation)
	}
	if !domainPattern.MatchString(domain) {
		return fmt.Errorf("%w: invalid domain %q", ErrValidation, domain)
	}
	// DNS labels are at most 63 characters; a longer label is not a hostname
	// a certificate could ever match.
	for _, label := range strings.Split(domain, ".") {
		if len(label) > 63 {
			return fmt.Errorf("%w: domain label exceeds 63 characters", ErrValidation)
		}
	}
	return nil
}

// BuildConfig renders the routing model for a node from its routes, the
// configured DNS providers and the optional ACME contact address. The output
// is a pure function of the input (map iteration is sorted at marshal time),
// so identical state always yields identical configuration bytes.
//
// Every route produces one HTTP forwarding router on the web entrypoint. A
// route whose certificate configuration is active additionally produces an
// HTTPS router on the websecure entrypoint and carries the shared
// gotham-https-redirect middleware; routes without one keep the BE-6.1
// plain-HTTP behavior. The static resolvers are one HTTP-01 resolver plus one
// DNS-01 resolver per enabled provider; credentials never appear here, they
// travel to the Traefik container as environment variables only.
func BuildConfig(routes []Route, providers []DNSProvider, acmeEmail string) ProxyConfig {
	cfg := ProxyConfig{
		EntryPoints: map[string]EntryPoint{
			EntryPointWeb:       {Address: ":80"},
			EntryPointWebSecure: {Address: ":443"},
			EntryPointInternal:  {Address: ":8080"},
		},
		CertificatesResolvers: map[string]CertificatesResolver{
			DefaultResolverName: {
				ACME: ACMEConfig{
					Email:   acmeEmail,
					Storage: TraefikAcmeStorage,
					HTTPChallenge: &HTTPChallenge{
						EntryPoint: EntryPointWeb,
					},
				},
			},
		},
		Routers:     map[string]Router{},
		Services:    map[string]Service{},
		Middlewares: map[string]Middleware{},
	}

	// One DNS-01 resolver per enabled provider type, sorted by type so the
	// result never depends on input order.
	types := make([]DNSProviderType, 0, len(providers))
	seenTypes := make(map[DNSProviderType]bool, len(providers))
	for _, provider := range providers {
		if !provider.Enabled || seenTypes[provider.Provider] {
			continue
		}
		seenTypes[provider.Provider] = true
		types = append(types, provider.Provider)
	}
	sort.Slice(types, func(i, j int) bool { return types[i] < types[j] })
	for _, providerType := range types {
		name := DNSResolverName(providerType)
		if name == "" {
			continue
		}
		cfg.CertificatesResolvers[name] = CertificatesResolver{
			ACME: ACMEConfig{
				Email:   acmeEmail,
				Storage: TraefikAcmeStorage,
				DNSChallenge: &DNSChallenge{
					Provider: string(providerType),
				},
			},
		}
	}

	for _, route := range routes {
		service := serviceName(route.AppID)
		cfg.Services[service] = Service{
			LoadBalancer: LoadBalancer{
				Servers: []Backend{{URL: route.Target}},
			},
		}
		web := Router{
			Rule:        fmt.Sprintf("Host(`%s`)", route.Domain),
			Service:     service,
			EntryPoints: []string{EntryPointWeb},
		}
		if route.Certificate != nil {
			web.Middlewares = []string{HTTPRedirectMiddleware}
			cfg.Middlewares[HTTPRedirectMiddleware] = Middleware{
				RedirectScheme: &RedirectScheme{Scheme: "https", Permanent: true},
			}
			tls := &RouterTLS{CertResolver: route.Certificate.Resolver}
			if route.Certificate.WildcardBase != "" {
				// The exact routed host is always the main name: a wildcard
				// SAN covers one label only, so `*.parent` alone would not
				// cover a multi-level host such as app.sub.example.com.
				tls.Domains = []TLSDomain{{
					Main: route.Domain,
					SANs: []string{"*." + route.Certificate.WildcardBase},
				}}
			}
			cfg.Routers[service+"-websecure"] = Router{
				Rule:        fmt.Sprintf("Host(`%s`)", route.Domain),
				Service:     service,
				EntryPoints: []string{EntryPointWebSecure},
				TLS:         tls,
			}
		}
		cfg.Routers[service+"-web"] = web
	}
	return cfg
}

// serviceName is the deterministic Traefik name shared by an application's
// router and service.
func serviceName(id uuid.UUID) string {
	return "app-" + id.String()
}

// NormalizeDomain trims and lowercases a stored base_domain before validation
// so Host() rules stay canonical.
func NormalizeDomain(domain string) string {
	return strings.ToLower(strings.TrimSpace(domain))
}

// wildcardBase derives the wildcard base that covers the routed host. A
// wildcard certificate name matches exactly one label, so the generated
// request keeps the exact host as the main name and adds `*.base` as a SAN:
//
//   - app.example.com      → base example.com (covers the host and siblings)
//   - app.sub.example.com  → base sub.example.com (the most specific parent
//     inside a configured zone; `*.example.com` would not cover the host)
//   - example.com (apex)   → base example.com (the host itself; the wildcard
//     then covers its subdomains)
//
// ok is false only when neither the parent nor the host sits inside a
// configured zone.
func wildcardBase(host string, zones []string) (string, bool) {
	labels := strings.Split(host, ".")
	if len(labels) >= 2 {
		parent := strings.Join(labels[1:], ".")
		if strings.Contains(parent, ".") && MatchZone(zones, parent) != "" {
			return parent, true
		}
	}
	if MatchZone(zones, host) != "" {
		return host, true
	}
	return "", false
}
