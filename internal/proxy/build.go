package proxy

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/google/uuid"
)

const (
	EntryPointWeb          = "web"
	EntryPointWebSecure    = "websecure"
	EntryPointInternal     = "traefik"
	DefaultResolverName    = "letsencrypt"
	HTTPRedirectMiddleware = "gotham-https-redirect"
	RedirectServiceName    = "gotham-redirect-noop"
	RedirectNamePrefix     = "gotham-redirect-"
	DefaultBackendHost     = "172.17.0.1"
)

type Route struct {
	PanelCertificate *PanelCertificateState
	Name             string
	AppID            uuid.UUID
	Domain           string
	Protocol         string
	Target           string
	Service          string
	Certificate      *RouteCertificate
}

type RouteCertificate struct {
	Resolver        string
	DNSProviderID   uuid.UUID
	DNSProviderType DNSProviderType
	WildcardBase    string
}

var domainPattern = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?(\.[a-z0-9]([a-z0-9-]*[a-z0-9])?)*$`)

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
	for _, label := range strings.Split(domain, ".") {
		if len(label) > 63 {
			return fmt.Errorf("%w: domain label exceeds 63 characters", ErrValidation)
		}
	}
	return nil
}

type Redirect struct {
	ID           uuid.UUID
	Source       string
	Target       string
	Code         int
	PreservePath bool
}

func BuildConfig(routes []Route, redirects []Redirect, providers []DNSProvider, acmeEmail, caServer string) ProxyConfig {
	cfg := ProxyConfig{
		EntryPoints: map[string]EntryPoint{
			EntryPointWeb:       {Address: ":80"},
			EntryPointWebSecure: {Address: ":443"},
			EntryPointInternal:  {Address: ":8080"},
		},
		CertificatesResolvers: map[string]CertificatesResolver{
			DefaultResolverName: {
				ACME: ACMEConfig{
					Email:    acmeEmail,
					CAServer: caServer,
					Storage:  TraefikAcmeStorage,
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
				Email:    acmeEmail,
				CAServer: caServer,
				Storage:  TraefikAcmeStorage,
				DNSChallenge: &DNSChallenge{
					Provider: string(providerType),
				},
			},
		}
	}

	for _, route := range routes {
		name := route.Name
		if name == "" {
			name = serviceName(route.AppID)
		}
		service := route.Service
		if service == "" {
			service = name
		}
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
		if NormalizeProtocol(route.Protocol) == ProtocolHTTPS || route.Certificate != nil {
			certificate := route.Certificate
			if certificate == nil {
				certificate = &RouteCertificate{Resolver: DefaultResolverName}
			}
			if route.PanelCertificate == nil || route.PanelCertificate.Status == "issued" {
				web.Middlewares = []string{HTTPRedirectMiddleware}
				cfg.Middlewares[HTTPRedirectMiddleware] = Middleware{
					RedirectScheme: &RedirectScheme{Scheme: "https", Permanent: true},
				}
			}
			tls := &RouterTLS{CertResolver: certificate.Resolver}
			if certificate.WildcardBase != "" {
				tls.Domains = []TLSDomain{{
					Main: route.Domain,
					SANs: []string{"*." + certificate.WildcardBase},
				}}
			}
			cfg.Routers[name+"-websecure"] = Router{
				Rule:        fmt.Sprintf("Host(`%s`)", route.Domain),
				Service:     service,
				EntryPoints: []string{EntryPointWebSecure},
				TLS:         tls,
			}
		}
		cfg.Routers[name+"-web"] = web
	}

	if len(redirects) > 0 {
		cfg.Services[RedirectServiceName] = Service{
			LoadBalancer: LoadBalancer{Servers: []Backend{}},
		}
	}
	for _, redirect := range redirects {
		name := RedirectNamePrefix + redirect.ID.String()
		cfg.Middlewares[name] = Middleware{
			RedirectRegex: &RedirectRegex{
				Regex:       RedirectRegexPattern(redirect.PreservePath),
				Replacement: RedirectReplacement(redirect.Target, redirect.PreservePath),
				Permanent:   redirect.Code == RedirectCodePermanent,
			},
		}
		cfg.Routers[name] = Router{
			Rule:        fmt.Sprintf("Host(`%s`)", redirect.Source),
			Service:     RedirectServiceName,
			EntryPoints: []string{EntryPointWeb},
			Middlewares: []string{name},
		}
	}
	return cfg
}

const (
	RedirectCodePermanent = 301
	RedirectCodeTemporary = 302
)

func RedirectRegexPattern(preservePath bool) string {
	if preservePath {
		return `(?i)^https?://[^/?#]*(.*)$`
	}
	return `(?i)^https?://.*$`
}

func RedirectReplacement(target string, preservePath bool) string {
	if preservePath {
		return "https://" + target + "${1}"
	}
	return "https://" + target + "/"
}

func serviceName(id uuid.UUID) string {
	return "app-" + id.String()
}

func AliasRouteName(id uuid.UUID, domain string) string {
	return "app-" + id.String() + "-" + domain
}

func NormalizeDomain(domain string) string {
	return strings.ToLower(strings.TrimSpace(domain))
}

const (
	ProtocolHTTP  = "http"
	ProtocolHTTPS = "https"
)

func NormalizeProtocol(protocol string) string {
	switch strings.ToLower(strings.TrimSpace(protocol)) {
	case ProtocolHTTPS:
		return ProtocolHTTPS
	case "", ProtocolHTTP:
		return ProtocolHTTP
	default:
		return strings.TrimSpace(protocol)
	}
}

func WildcardBase(host string, zones []string) (string, bool) {
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
