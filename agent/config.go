package agent

import (
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"strings"
)

// Environment variables understood by the agent.
const (
	envCPAddr      = "GOTHAM_AGENT_CP_ADDR"
	envNodeID      = "GOTHAM_AGENT_NODE_ID"
	envListenAddr  = "GOTHAM_AGENT_LISTEN_ADDR"
	envCA          = "GOTHAM_AGENT_CA"
	envCertDir     = "GOTHAM_AGENT_CERT_DIR"
	envKey         = "GOTHAM_AGENT_KEY"
	envDockerSock  = "GOTHAM_AGENT_DOCKER_SOCK"
	envComposeRoot = "GOTHAM_AGENT_COMPOSE_ROOT"
	envLogLevel    = "GOTHAM_AGENT_LOG_LEVEL"

	envDockerHost = "DOCKER_HOST"
)

// Defaults applied when the matching environment variable is unset.
const (
	defaultCPAddr     = "127.0.0.1:9090"
	defaultListenAddr = ":9443"
	defaultCertDir    = "./data/agent"
	defaultDockerSock = "/var/run/docker.sock"
	defaultLogLevel   = "info"
)

// Config holds the agent's runtime configuration, loaded from the environment.
type Config struct {
	// CPAddr is the control-plane gRPC address the agent connects to.
	CPAddr string
	// NodeID identifies this node; it defaults to the hostname.
	NodeID string
	// ListenAddr is the address the agent's DockerService gRPC server binds.
	ListenAddr string
	// CA is the PEM bundle used to verify the CP server. An empty value selects
	// insecure transport for local development.
	CA string
	// CertDir stores the certificate and private key received or generated at
	// registration.
	CertDir string
	// KeyFile optionally overrides the agent private key path.
	KeyFile string
	// DockerSock is the Docker Engine endpoint (a unix path or a tcp:// URL).
	DockerSock string
	// ComposeRoot is the directory that holds one subdirectory per compose
	// service project. It must be writable by the agent user.
	ComposeRoot string
	// LogLevel is the slog level name (debug, info, warn, error).
	LogLevel string
}

// Load builds a Config from GOTHAM_AGENT_* environment variables, applying
// defaults for unset values. It returns an error for values that cannot be
// used, such as an invalid CP address or log level.
func Load() (Config, error) {
	cfg := Config{
		CPAddr:      envOr(envCPAddr, defaultCPAddr),
		NodeID:      envOr(envNodeID, hostname()),
		ListenAddr:  envOr(envListenAddr, defaultListenAddr),
		CA:          os.Getenv(envCA),
		CertDir:     envOr(envCertDir, defaultCertDir),
		KeyFile:     os.Getenv(envKey),
		DockerSock:  dockerSock(),
		ComposeRoot: envOr(envComposeRoot, defaultComposeRoot),
		LogLevel:    envOr(envLogLevel, defaultLogLevel),
	}

	if _, _, err := net.SplitHostPort(cfg.CPAddr); err != nil {
		return Config{}, fmt.Errorf("invalid %s %q: %w", envCPAddr, cfg.CPAddr, err)
	}
	if _, err := parseLevel(cfg.LogLevel); err != nil {
		return Config{}, fmt.Errorf("invalid %s %q: %w", envLogLevel, cfg.LogLevel, err)
	}
	if cfg.CertDir == "" {
		return Config{}, fmt.Errorf("%s must not be empty", envCertDir)
	}
	if cfg.NodeID == "" {
		return Config{}, fmt.Errorf("%s must not be empty", envNodeID)
	}
	return cfg, nil
}

// NewLogger returns a text slog logger writing to w at the configured level.
// Unknown levels fall back to info.
func NewLogger(w io.Writer, level string) *slog.Logger {
	parsed, err := parseLevel(level)
	if err != nil {
		parsed = slog.LevelInfo
	}
	return slog.New(slog.NewTextHandler(w, &slog.HandlerOptions{Level: parsed}))
}

// parseLevel maps a level name to a slog.Level.
func parseLevel(name string) (slog.Level, error) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "debug":
		return slog.LevelDebug, nil
	case "", "info":
		return slog.LevelInfo, nil
	case "warn", "warning":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	default:
		return 0, fmt.Errorf("unknown log level")
	}
}

// dockerSock resolves the Docker endpoint, preferring the agent-specific
// variable, then DOCKER_HOST, then the platform default socket.
func dockerSock() string {
	return envOr(envDockerSock, envOr(envDockerHost, defaultDockerSock))
}

// hostname returns the local hostname, or "unknown" when it cannot be read.
func hostname() string {
	name, err := os.Hostname()
	if err != nil || name == "" {
		return "unknown"
	}
	return name
}

// envOr returns the value of key, or fallback when it is unset.
func envOr(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
