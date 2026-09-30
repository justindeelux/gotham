package agent

import (
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"strings"
	"time"

	"github.com/justindeelux/gotham/updatecore"
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

	envAutoUpdate     = "GOTHAM_AGENT_AUTO_UPDATE"
	envUpdateInterval = "GOTHAM_AGENT_UPDATE_INTERVAL"
	envUpdateChannel  = "GOTHAM_AGENT_UPDATE_CHANNEL"
	envBinary         = "GOTHAM_AGENT_BINARY"
	envUpdateScript   = "GOTHAM_AGENT_UPDATE_SCRIPT"
	envUpdateStatus   = "GOTHAM_AGENT_UPDATE_STATUS"
	envUpdatePending  = "GOTHAM_AGENT_UPDATE_PENDING"
	envUpdateLock     = "GOTHAM_AGENT_UPDATE_LOCK"
	envUpdateRetry    = "GOTHAM_AGENT_UPDATE_RETRY"
	envUpdateBackoff  = "GOTHAM_AGENT_UPDATE_BACKOFF"
	envHealthAddr     = "GOTHAM_AGENT_HEALTH_ADDR"

	envDockerHost = "DOCKER_HOST"
)

// Defaults applied when the matching environment variable is unset.
const (
	defaultCPAddr     = "127.0.0.1:9090"
	defaultListenAddr = ":9443"
	defaultCertDir    = "./data/agent"
	defaultDockerSock = "/var/run/docker.sock"
	defaultLogLevel   = "info"

	// Agent self-update layout, mirroring the control plane's split
	// privileges: the binary and its hardlink backup live in the agent-writable
	// StateDirectory, the wrapper and its config are root-owned, and the
	// authoritative status is root-owned in a directory the agent cannot write.
	defaultAgentBinary       = "/var/lib/gotham-agent/bin/gotham-agent"
	defaultAgentUpdateScript = "/usr/libexec/gotham/gotham-agent-update"
	defaultAgentStatusPath   = "/var/lib/gotham-agent-updater/update.status"
	defaultAgentPendingPath  = "/var/lib/gotham-agent/update.pending"
	defaultAgentLockPath     = "/var/lib/gotham-agent/update.lock"
	// defaultAgentRetryPath is the agent-owned marker an operator reset writes
	// to make a running agent clear its failed-update backoff and retry.
	defaultAgentRetryPath = "/var/lib/gotham-agent/update.retry"
	// defaultAgentBackoffPath persists the failed-attempt count so the seeded
	// backoff escalates across wrapper restarts.
	defaultAgentBackoffPath = "/var/lib/gotham-agent/update.backoff"
	defaultAgentHealthAddr  = "127.0.0.1:8001"
	defaultUpdateInterval   = 5 * time.Minute
	// defaultUpdateChannel is the release channel an agent accepts when
	// GOTHAM_AGENT_UPDATE_CHANNEL is unset. An offer whose signed channel does
	// not match is refused (C2).
	defaultUpdateChannel = "stable"
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

	// Version is the running agent build version. It is set by the entrypoint
	// (not the environment) and reported to the CP in heartbeats and update
	// requests.
	Version string
	// AutoUpdate enables unattended updates: when set the agent applies any
	// verified offer on its poll. Off by default, so only an operator-triggered
	// rollout applies an update.
	AutoUpdate bool
	// UpdateInterval is how often the agent polls the CP for an update.
	UpdateInterval time.Duration
	// UpdateChannel is the release channel this agent accepts ("stable" by
	// default). An offer with a different or empty channel is refused.
	UpdateChannel string
	// BinaryPath is the fixed agent executable the updater swaps.
	BinaryPath string
	// UpdateScript is the root-owned restart/healthcheck wrapper.
	UpdateScript string
	// UpdateStatusPath is the root-owned authoritative update status.
	UpdateStatusPath string
	// UpdatePendingPath is the agent-owned staged gate marker.
	UpdatePendingPath string
	// UpdateLockPath is the lock shared with the wrapper.
	UpdateLockPath string
	// UpdateRetryPath is the agent-owned retry marker an operator reset writes
	// to clear the failed-update backoff of a running agent.
	UpdateRetryPath string
	// UpdateBackoffPath persists the failed-attempt count so the seeded backoff
	// escalates across wrapper restarts.
	UpdateBackoffPath string
	// HealthAddr is the loopback address the wrapper probes after a restart.
	HealthAddr string
	// Restart overrides the restart wrapper (tests). When nil the fixed
	// root-owned wrapper is run through sudo.
	Restart updatecore.RestartFunc
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

		AutoUpdate:        strings.EqualFold(strings.TrimSpace(os.Getenv(envAutoUpdate)), "true"),
		UpdateInterval:    updateIntervalFromEnv(),
		UpdateChannel:     envOr(envUpdateChannel, defaultUpdateChannel),
		BinaryPath:        envOr(envBinary, defaultAgentBinary),
		UpdateScript:      envOr(envUpdateScript, defaultAgentUpdateScript),
		UpdateStatusPath:  envOr(envUpdateStatus, defaultAgentStatusPath),
		UpdatePendingPath: envOr(envUpdatePending, defaultAgentPendingPath),
		UpdateLockPath:    envOr(envUpdateLock, defaultAgentLockPath),
		UpdateRetryPath:   envOr(envUpdateRetry, defaultAgentRetryPath),
		UpdateBackoffPath: envOr(envUpdateBackoff, defaultAgentBackoffPath),
		HealthAddr:        envOr(envHealthAddr, defaultAgentHealthAddr),
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

// updateIntervalFromEnv parses the agent update poll interval, defaulting to 5m.
func updateIntervalFromEnv() time.Duration {
	raw := strings.TrimSpace(os.Getenv(envUpdateInterval))
	if raw == "" {
		return defaultUpdateInterval
	}
	parsed, err := time.ParseDuration(raw)
	if err != nil || parsed <= 0 {
		return defaultUpdateInterval
	}
	return parsed
}
