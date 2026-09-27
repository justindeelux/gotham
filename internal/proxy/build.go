package proxy

import (
	"fmt"
	"regexp"
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
	// to /ping so the node agent can verify reloads without exposing the
	// endpoint to the network.
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

// BuildConfig renders the routing model for a node from its routes. The
// output is a pure function of the input (map iteration is sorted at marshal
// time), so identical state always yields identical configuration bytes.
//
// Each route produces one HTTP forwarding router on the web entrypoint. The
// HTTPS router and the HTTP→HTTPS redirect are emitted by BE-6.2 once
// certificates are configured and verified: emitting a TLS router now would
// have Traefik attempt ACME issuance for every domain before SSL is ready,
// and the redirect would break the plain-HTTP acceptance path of BE-6.1
// (F2/A1). The static resolver definition stays inert until then.
func BuildConfig(routes []Route) ProxyConfig {
	cfg := ProxyConfig{
		EntryPoints: map[string]EntryPoint{
			EntryPointWeb:       {Address: ":80"},
			EntryPointWebSecure: {Address: ":443"},
			EntryPointInternal:  {Address: ":8080"},
		},
		CertificatesResolvers: map[string]CertificatesResolver{
			DefaultResolverName: {
				ACME: ACMEConfig{
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
	if len(routes) == 0 {
		return cfg
	}

	for _, route := range routes {
		service := serviceName(route.AppID)
		cfg.Services[service] = Service{
			LoadBalancer: LoadBalancer{
				Servers: []Backend{{URL: route.Target}},
			},
		}
		cfg.Routers[service+"-web"] = Router{
			Rule:        fmt.Sprintf("Host(`%s`)", route.Domain),
			Service:     service,
			EntryPoints: []string{EntryPointWeb},
		}
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
