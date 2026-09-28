package proxy

import (
	"bytes"
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/pelletier/go-toml/v2"
)

// Node-side layout constants. TraefikDir is the host directory the node agent
// writes configuration into and that is bind-mounted into the Traefik
// container at TraefikContainerConfigDir. The agent package duplicates
// TraefikDir as its own default (constants are duplicated rather than
// imported across scopes, mirroring deploy's portsLabel precedent) — changing
// one side without the other leaves Traefik reading an empty mount.
const (
	// TraefikDir is the node host directory holding the Traefik
	// configuration. It lives under the agent's StateDirectory so the
	// unprivileged gotham-agent user can write it (systemd ProtectSystem
	// makes /etc read-only for the agent).
	TraefikDir = "/var/lib/gotham-agent/traefik"
	// TraefikAcmeDir is the node host directory mounted at TraefikAcmeMount
	// so ACME state survives config rewrites.
	TraefikAcmeDir = TraefikDir + "/acme"
	// TraefikContainerConfigDir is where TraefikDir is mounted inside the
	// Traefik container; traefik.yml at its root is auto-loaded.
	TraefikContainerConfigDir = "/etc/traefik"
	// TraefikDynamicDir is the file-provider directory inside the container.
	TraefikDynamicDir = TraefikContainerConfigDir + "/dynamic"
	// TraefikAcmeMount is where TraefikAcmeDir is mounted in the container;
	// the ACME storage path points into it.
	TraefikAcmeMount = "/acme"
	// TraefikAcmeStorage is the ACME certificate storage path inside the
	// container (static certificatesResolvers.acme.storage).
	TraefikAcmeStorage = TraefikAcmeMount + "/acme.json"
	// TraefikContainerName is the fixed name of the node's proxy container.
	TraefikContainerName = "gotham-traefik"
	// TraefikImage is the pinned Traefik 3.x image the bootstrap pulls.
	TraefikImage = "traefik:v3.7"
	// TraefikRestartPolicy is the native Docker restart policy for the
	// long-lived proxy container, so a node reboot or proxy crash recovers
	// without a custom supervisor (BE-6.1 A3).
	TraefikRestartPolicy = "unless-stopped"
	// PingURL is the loopback-only Traefik ping endpoint the agent verifies
	// reloads with. Port 8080 is published on 127.0.0.1 only.
	PingURL = "http://127.0.0.1:8080/ping"
)

// traefikLabels mark the bootstrapped proxy container like every other
// Gotham-managed container and record the desired convergence state: the
// source directories mounted into it and its native restart policy. The
// service verifies an existing container against these labels, the engine's
// mounts and the real restart policy before reusing it (R5); the ports label
// feeds the container list UI.
func traefikLabels(configDir, acmeDir string) map[string]string {
	return map[string]string{
		"gotham.managed":              "true",
		"gotham.component":            "proxy",
		"gotham.ports":                strings.Join(TraefikPorts, ","),
		"gotham.proxy.config_dir":     configDir,
		"gotham.proxy.acme_dir":       acmeDir,
		"gotham.proxy.restart_policy": TraefikRestartPolicy,
	}
}

// Port and volume specs passed to the container service when bootstrapping
// the Traefik container. The internal entrypoint is bound to loopback so the
// ping endpoint is reachable from the agent but never from the network; the
// configuration directory is mounted read-only inside Traefik, which only
// reads it (the writable state is the separate ACME volume).
var (
	// TraefikPorts publishes the gateway (80/443) and the loopback ping port.
	TraefikPorts = []string{"80:80", "443:443", "127.0.0.1:8080:8080"}
	// TraefikVolumes mounts the node config directory read-only and the ACME
	// volume writable (BE-6.1 F7).
	TraefikVolumes = TraefikVolumesFor(TraefikDir, TraefikAcmeDir)
)

// TraefikVolumesFor renders the mount specs for a config/ACME directory pair:
// the configuration directory is read-only inside Traefik, which only reads
// it, while the ACME volume stays writable (BE-6.1 F7).
func TraefikVolumesFor(configDir, acmeDir string) []string {
	return []string{
		configDir + ":" + TraefikContainerConfigDir + ":ro",
		acmeDir + ":" + TraefikAcmeMount,
	}
}

// staticDocument is the rendered static configuration (traefik.yml).
type staticDocument struct {
	EntryPoints           map[string]EntryPoint           `yaml:"entryPoints" toml:"entryPoints"`
	Ping                  pingSection                     `yaml:"ping" toml:"ping"`
	Providers             providersSection                `yaml:"providers" toml:"providers"`
	CertificatesResolvers map[string]CertificatesResolver `yaml:"certificatesResolvers,omitempty" toml:"certificatesResolvers,omitempty"`
}

// pingSection pins /ping to the loopback entrypoint the agent verifies.
type pingSection struct {
	EntryPoint string `yaml:"entryPoint" toml:"entryPoint"`
}

// providersSection configures the file provider that serves the dynamic
// document; watch makes every write hot-reload without a restart.
type providersSection struct {
	File fileProvider `yaml:"file" toml:"file"`
}

type fileProvider struct {
	Directory string `yaml:"directory" toml:"directory"`
	Watch     bool   `yaml:"watch" toml:"watch"`
}

// dynamicDocument is the rendered dynamic configuration served by the file
// provider (dynamic/gotham.yml).
type dynamicDocument struct {
	HTTP httpSection `yaml:"http" toml:"http"`
}

type httpSection struct {
	Routers     map[string]Router     `yaml:"routers,omitempty" toml:"routers,omitempty"`
	Services    map[string]Service    `yaml:"services,omitempty" toml:"services,omitempty"`
	Middlewares map[string]Middleware `yaml:"middlewares,omitempty" toml:"middlewares,omitempty"`
}

// StaticFileName is the root-level static document name for a format; Traefik
// auto-loads traefik.yml / traefik.toml from its config directory.
func StaticFileName(format Format) string {
	if format == FormatTOML {
		return "traefik.toml"
	}
	return "traefik.yml"
}

// DynamicFileName is the file-provider document name for a format; it lives
// in the watched dynamic subdirectory.
func DynamicFileName(format Format) string {
	if format == FormatTOML {
		return "dynamic/gotham.toml"
	}
	return "dynamic/gotham.yml"
}

// Generate renders the static and dynamic Traefik documents for cfg. The
// output is deterministic: maps are marshalled with sorted keys and struct
// fields keep their declaration order, so the same state always produces the
// same bytes (which in turn makes agent writes idempotent).
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
	dynamic := dynamicDocument{
		HTTP: httpSection{
			Routers:     cfg.Routers,
			Services:    cfg.Services,
			Middlewares: cfg.Middlewares,
		},
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

// marshalDocument renders v in the requested format.
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
