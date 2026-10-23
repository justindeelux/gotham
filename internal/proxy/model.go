package proxy

type Format string

const (
	FormatYAML Format = "yaml"
	FormatTOML Format = "toml"
)

type File struct {
	Name    string
	Content []byte
}

type ProxyConfig struct {
	Routers               map[string]Router
	Services              map[string]Service
	Middlewares           map[string]Middleware
	EntryPoints           map[string]EntryPoint
	CertificatesResolvers map[string]CertificatesResolver
}

type Router struct {
	Rule        string     `yaml:"rule" toml:"rule"`
	Service     string     `yaml:"service" toml:"service"`
	EntryPoints []string   `yaml:"entryPoints,omitempty" toml:"entryPoints,omitempty"`
	Middlewares []string   `yaml:"middlewares,omitempty" toml:"middlewares,omitempty"`
	TLS         *RouterTLS `yaml:"tls,omitempty" toml:"tls,omitempty"`
}

type RouterTLS struct {
	CertResolver string      `yaml:"certResolver,omitempty" toml:"certResolver,omitempty"`
	Domains      []TLSDomain `yaml:"domains,omitempty" toml:"domains,omitempty"`
}

type TLSDomain struct {
	Main string   `yaml:"main" toml:"main"`
	SANs []string `yaml:"sans,omitempty" toml:"sans,omitempty"`
}

type Service struct {
	LoadBalancer LoadBalancer `yaml:"loadBalancer" toml:"loadBalancer"`
}

type LoadBalancer struct {
	Servers []Backend `yaml:"servers" toml:"servers"`
}

type Backend struct {
	URL string `yaml:"url" toml:"url"`
}

type Middleware struct {
	RedirectScheme *RedirectScheme `yaml:"redirectScheme,omitempty" toml:"redirectScheme,omitempty"`
	RedirectRegex  *RedirectRegex  `yaml:"redirectRegex,omitempty" toml:"redirectRegex,omitempty"`
}

type RedirectScheme struct {
	Scheme    string `yaml:"scheme" toml:"scheme"`
	Permanent bool   `yaml:"permanent,omitempty" toml:"permanent,omitempty"`
}

type RedirectRegex struct {
	Regex       string `yaml:"regex" toml:"regex"`
	Replacement string `yaml:"replacement" toml:"replacement"`
	Permanent   bool   `yaml:"permanent,omitempty" toml:"permanent,omitempty"`
}

type EntryPoint struct {
	Address string `yaml:"address" toml:"address"`
}

type CertificatesResolver struct {
	ACME ACMEConfig `yaml:"acme" toml:"acme"`
}

type ACMEConfig struct {
	Email         string         `yaml:"email,omitempty" toml:"email,omitempty"`
	CAServer      string         `yaml:"caServer,omitempty" toml:"caServer,omitempty"`
	Storage       string         `yaml:"storage" toml:"storage"`
	HTTPChallenge *HTTPChallenge `yaml:"httpChallenge,omitempty" toml:"httpChallenge,omitempty"`
	DNSChallenge  *DNSChallenge  `yaml:"dnsChallenge,omitempty" toml:"dnsChallenge,omitempty"`
}

type HTTPChallenge struct {
	EntryPoint string `yaml:"entryPoint" toml:"entryPoint"`
}

type DNSChallenge struct {
	Provider string `yaml:"provider" toml:"provider"`
}
