package proxy

import (
	"bytes"
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/pelletier/go-toml/v2"
)

const (
	TraefikDir                = "/var/lib/gotham-agent/traefik"
	TraefikAcmeDir            = TraefikDir + "/acme"
	TraefikContainerConfigDir = "/etc/traefik"
	TraefikDynamicDir         = TraefikContainerConfigDir + "/dynamic"
	TraefikAcmeMount          = "/acme"
	TraefikAcmeStorage        = TraefikAcmeMount + "/acme.json"
	TraefikContainerName      = "gotham-traefik"
	TraefikEnvHashLabel       = "gotham.proxy.env_hash"
	TraefikImage              = "traefik:v3.7"
	TraefikRestartPolicy      = "unless-stopped"
	PingURL                   = "http://127.0.0.1:8080/ping"
)

func traefikLabels(configDir, acmeDir, envHash string) map[string]string {
	return map[string]string{
		"gotham.managed":              "true",
		"gotham.component":            "proxy",
		"gotham.ports":                strings.Join(TraefikPorts, ","),
		"gotham.proxy.config_dir":     configDir,
		"gotham.proxy.acme_dir":       acmeDir,
		"gotham.proxy.restart_policy": TraefikRestartPolicy,
		TraefikEnvHashLabel:           envHash,
	}
}

var (
	TraefikPorts   = []string{"80:80", "443:443", "127.0.0.1:8080:8080"}
	TraefikVolumes = TraefikVolumesFor(TraefikDir, TraefikAcmeDir)
)

func TraefikVolumesFor(configDir, acmeDir string) []string {
	return []string{
		configDir + ":" + TraefikContainerConfigDir + ":ro",
		acmeDir + ":" + TraefikAcmeMount,
	}
}

type staticDocument struct {
	EntryPoints           map[string]EntryPoint           `yaml:"entryPoints" toml:"entryPoints"`
	Ping                  pingSection                     `yaml:"ping" toml:"ping"`
	Providers             providersSection                `yaml:"providers" toml:"providers"`
	CertificatesResolvers map[string]CertificatesResolver `yaml:"certificatesResolvers,omitempty" toml:"certificatesResolvers,omitempty"`
}

type pingSection struct {
	EntryPoint string `yaml:"entryPoint" toml:"entryPoint"`
}

type providersSection struct {
	File fileProvider `yaml:"file" toml:"file"`
}

type fileProvider struct {
	Directory string `yaml:"directory" toml:"directory"`
	Watch     bool   `yaml:"watch" toml:"watch"`
}

type dynamicDocument struct {
	HTTP *httpSection `yaml:"http,omitempty" toml:"http,omitempty"`
}

type httpSection struct {
	Routers     map[string]Router     `yaml:"routers,omitempty" toml:"routers,omitempty"`
	Services    map[string]Service    `yaml:"services,omitempty" toml:"services,omitempty"`
	Middlewares map[string]Middleware `yaml:"middlewares,omitempty" toml:"middlewares,omitempty"`
}

func StaticFileName(format Format) string {
	if format == FormatTOML {
		return "traefik.toml"
	}
	return "traefik.yml"
}

func DynamicFileName(format Format) string {
	if format == FormatTOML {
		return "dynamic/gotham.toml"
	}
	return "dynamic/gotham.yml"
}

func Generate(cfg ProxyConfig, format Format) ([]File, error) {
	static := staticDocument{
		EntryPoints: cfg.EntryPoints,
		Ping: pingSection{
			EntryPoint: EntryPointInternal,
		},
		Providers: providersSection{
			File: fileProvider{
				Directory: TraefikDynamicDir,
				Watch:     true,
			},
		},
		CertificatesResolvers: cfg.CertificatesResolvers,
	}
	dynamic := dynamicDocument{}
	if len(cfg.Routers)+len(cfg.Services)+len(cfg.Middlewares) > 0 {
		dynamic.HTTP = &httpSection{
			Routers:     cfg.Routers,
			Services:    cfg.Services,
			Middlewares: cfg.Middlewares,
		}
	}

	staticContent, err := marshalDocument(static, format)
	if err != nil {
		return nil, fmt.Errorf("proxy: render static config: %w", err)
	}
	dynamicContent, err := marshalDocument(dynamic, format)
	if err != nil {
		return nil, fmt.Errorf("proxy: render dynamic config: %w", err)
	}
	return []File{
		{Name: StaticFileName(format), Content: staticContent},
		{Name: DynamicFileName(format), Content: dynamicContent},
	}, nil
}

func marshalDocument(v any, format Format) ([]byte, error) {
	switch format {
	case FormatTOML:
		return toml.Marshal(v)
	case FormatYAML, "":
		var buf bytes.Buffer
		encoder := yaml.NewEncoder(&buf)
		encoder.SetIndent(2)
		if err := encoder.Encode(v); err != nil {
			return nil, err
		}
		if err := encoder.Close(); err != nil {
			return nil, err
		}
		return buf.Bytes(), nil
	default:
		return nil, fmt.Errorf("proxy: unsupported format %q", format)
	}
}
