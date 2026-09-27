package proxy

// Format selects the syntax of the generated Traefik documents.
type Format string

const (
	// FormatYAML renders traefik.yml / dynamic/gotham.yml (default).
	FormatYAML Format = "yaml"
	// FormatTOML renders traefik.toml / dynamic/gotham.toml.
	FormatTOML Format = "toml"
)

// File is one generated Traefik configuration document. Name is the path
// relative to the node's proxy config directory (no leading slash, no ".."):
// the static document sits at the config root so Traefik auto-loads it, and
// the dynamic document lives in the directory the file provider watches.
type File struct {
	Name    string
	Content []byte
}

// ProxyConfig is the complete Traefik configuration Gotham generates for one
// node: the routing model (routers, services, middlewares) plus the static
// sections that the generated static document is rendered from (entrypoints
// and the TLS certificate resolvers). Everything is a pure function of
// control-plane state, so two runs over the same state yield identical bytes.
type ProxyConfig struct {
	// Routers maps a router name to its rule, service and middleware chain
	// (dynamic configuration, http.routers).
	Routers map[string]Router
	// Services maps a service name to its load-balanced backends (dynamic
	// configuration, http.services).
	Services map[string]Service
	// Middlewares maps a middleware name to its definition (dynamic
	// configuration, http.middlewares).
	Middlewares map[string]Middleware
	// EntryPoints maps an entrypoint name to its listen address (static
	// configuration).
	EntryPoints map[string]EntryPoint
	// CertificatesResolvers maps a resolver name to its ACME settings
	// (static configuration, referenced from router TLS sections).
	CertificatesResolvers map[string]CertificatesResolver
}

// Router is a Traefik HTTP router (http.routers.<name>).
type Router struct {
	// Rule is the match rule, e.g. Host(`example.com`).
	Rule string `yaml:"rule" toml:"rule"`
	// Service names the backend service that serves this router.
	Service string `yaml:"service" toml:"service"`
	// EntryPoints limits the router to the named entrypoints; empty means all.
	EntryPoints []string `yaml:"entryPoints,omitempty" toml:"entryPoints,omitempty"`
	// Middlewares are middleware names applied in order before forwarding.
	Middlewares []string `yaml:"middlewares,omitempty" toml:"middlewares,omitempty"`
	// TLS configures certificate resolution for this router.
	TLS *RouterTLS `yaml:"tls,omitempty" toml:"tls,omitempty"`
}

// RouterTLS is the tls: section of a router.
type RouterTLS struct {
	// CertResolver names a certificate resolver from the static config.
	CertResolver string `yaml:"certResolver" toml:"certResolver"`
}

// Service is a Traefik service (http.services.<name>).
type Service struct {
	// LoadBalancer holds the backend servers of the service.
	LoadBalancer LoadBalancer `yaml:"loadBalancer" toml:"loadBalancer"`
}

// LoadBalancer balances requests across Servers.
type LoadBalancer struct {
	// Servers is the backend list; Gotham generates exactly one per service.
	Servers []Backend `yaml:"servers" toml:"servers"`
}

// Backend is one upstream server of a load balancer (rendered as
// `servers: [{url: ...}]`).
type Backend struct {
	// URL is the backend base URL, e.g. http://172.17.0.1:3000.
	URL string `yaml:"url" toml:"url"`
}

// Middleware is a Traefik middleware (http.middlewares.<name>). Only the
// redirect scheme middleware is generated today; the struct is shaped so more
// middleware kinds can be added without renaming the section.
type Middleware struct {
	// RedirectScheme redirects requests to another scheme (http → https).
	RedirectScheme *RedirectScheme `yaml:"redirectScheme,omitempty" toml:"redirectScheme,omitempty"`
}

// RedirectScheme is the redirectScheme middleware definition.
type RedirectScheme struct {
	// Scheme is the target scheme, e.g. "https".
	Scheme string `yaml:"scheme" toml:"scheme"`
	// Permanent emits a 308 instead of a 307 redirect.
	Permanent bool `yaml:"permanent,omitempty" toml:"permanent,omitempty"`
}

// EntryPoint is a Traefik entrypoint (static entryPoints.<name>).
type EntryPoint struct {
	// Address is the listen address, e.g. ":80".
	Address string `yaml:"address" toml:"address"`
}

// CertificatesResolver is a static certificate resolver
// (certificatesResolvers.<name>).
type CertificatesResolver struct {
	// ACME holds the ACME (Let's Encrypt) settings of the resolver.
	ACME ACMEConfig `yaml:"acme" toml:"acme"`
}

// ACMEConfig is the acme: section of a certificate resolver. The challenge
// type is HTTP-01 on the web entrypoint today; BE-6.2 extends this with DNS
// providers for wildcard certificates.
type ACMEConfig struct {
	// Email is the ACME registration contact. Omitted when empty (Let's
	// Encrypt accepts accounts without a contact address).
	Email string `yaml:"email,omitempty" toml:"email,omitempty"`
	// Storage is the path of the certificate storage file inside the
	// Traefik container.
	Storage string `yaml:"storage" toml:"storage"`
	// HTTPChallenge selects the HTTP-01 challenge on an entrypoint.
	HTTPChallenge *HTTPChallenge `yaml:"httpChallenge,omitempty" toml:"httpChallenge,omitempty"`
}

// HTTPChallenge is the httpChallenge: section of an ACME resolver.
type HTTPChallenge struct {
	// EntryPoint is the entrypoint the challenge is answered on; it must be
	// reachable from the internet on port 80.
	EntryPoint string `yaml:"entryPoint" toml:"entryPoint"`
}
