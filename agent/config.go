package agent

import (
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/justindeelux/gotham/agent/docker"
	"github.com/justindeelux/gotham/agent/files"
	"github.com/justindeelux/gotham/agent/updates"
	"github.com/justindeelux/gotham/updatecore"
)

// Environment variables understood by the agent.
const (
	envCPAddr          = "GOTHAM_AGENT_CP_ADDR"
	envNodeID          = "GOTHAM_AGENT_NODE_ID"
	envListenAddr      = "GOTHAM_AGENT_LISTEN_ADDR"
	envCA              = "GOTHAM_AGENT_CA"
	envCertDir         = "GOTHAM_AGENT_CERT_DIR"
	envKey             = "GOTHAM_AGENT_KEY"
	envInsecure        = "GOTHAM_AGENT_INSECURE"
	envEnrollmentToken = "GOTHAM_AGENT_ENROLLMENT_TOKEN"
	envDockerSock      = "GOTHAM_AGENT_DOCKER_SOCK"
	envComposeRoot     = "GOTHAM_AGENT_COMPOSE_ROOT"
	envLogLevel        = "GOTHAM_AGENT_LOG_LEVEL"

	envManagedVolumeRoot = "GOTHAM_AGENT_MANAGED_VOLUME_ROOT"
	// envSharedManagedVolumeRoot lets a node reuse the control plane's root
	// variable name; the agent-prefixed variable wins when both are set.
	envSharedManagedVolumeRoot = "GOTHAM_MANAGED_VOLUME_ROOT"

	// File service configuration (Phase 18). The file service is on unless
	// GOTHAM_AGENT_FILES=false; the installer asks the operator and writes the
	// answer. By default it browses the whole filesystem (root "/"), limited
	// only by the agent process's own permissions;
	// GOTHAM_AGENT_FILE_FULL_ACCESS=false restores the confined roots.
	envFiles            = "GOTHAM_AGENT_FILES"
	envFileFullAccess   = "GOTHAM_AGENT_FILE_FULL_ACCESS"
	envFileRoots        = "GOTHAM_AGENT_FILE_ROOTS"
	envFileMaxUpload    = "GOTHAM_AGENT_FILE_MAX_UPLOAD"
	envFileMaxTransfers = "GOTHAM_AGENT_FILE_MAX_TRANSFERS"
	// Archive (compress/extract) caps, Phase 18 step 9b.
	envFileArchiveMaxBytes   = "GOTHAM_AGENT_ARCHIVE_MAX_BYTES"
	envFileArchiveMaxEntries = "GOTHAM_AGENT_ARCHIVE_MAX_ENTRIES"
	// envFileArchiveMaxInput bounds the on-disk size of an archive file that
	// extract will read, before any parser allocates.
	envFileArchiveMaxInput = "GOTHAM_AGENT_ARCHIVE_MAX_INPUT_BYTES"

	// Host terminal (JUS-243). The shell must be an absolute, executable
	// file; empty selects $SHELL, /bin/bash or /bin/sh.
	envTerminalShell       = "GOTHAM_AGENT_TERMINAL_SHELL"
	envTerminalMaxSessions = "GOTHAM_AGENT_TERMINAL_MAX_SESSIONS"
	// envTerminalIntegration turns the additive bash startup integration off
	// ("false"/"0"/"no"/"off"), restoring the pre-JUS-263 shell selection and
	// startup files.
	envTerminalIntegration = "GOTHAM_AGENT_TERMINAL_INTEGRATION"

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
	defaultCPAddr     = "127.0.0.1:9442"
	defaultListenAddr = ":9443"
	defaultCertDir    = "./data/agent"
	defaultLogLevel   = "info"
	// Agent self-update layout, mirroring the control plane's split
	// privileges: the binary and its hardlink backup live in the agent-writable
	// StateDirectory, the wrapper and its config are root-owned, and the
	// authoritative status is root-owned in a directory the agent cannot write.
	defaultAgentStateRoot    = "/var/lib/gotham-agent"
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
	// defaultUpdateChannel is the release channel accepted when
	// GOTHAM_AGENT_UPDATE_CHANNEL is unset.
	defaultUpdateChannel = updates.DefaultChannel
)

// Config holds the agent's runtime configuration, loaded from the environment.
type Config struct {
	// CPAddr is the control-plane gRPC address the agent connects to.
	CPAddr string
	// NodeID identifies this node; it defaults to localhost.
	NodeID string
	// ListenAddr is the address the agent's DockerService gRPC server binds.
	ListenAddr string
	// CA is the PEM bundle used to verify the CP server. An empty value selects
	// insecure transport for local development and is only accepted together
	// with Insecure.
	CA string
	// Insecure is the explicit development opt-in for a channel with no CA:
	// it allows plaintext and requires the listener to stay on loopback. It is
	// set from GOTHAM_AGENT_INSECURE=true; there is no implicit plaintext path.
	Insecure bool
	// EnrollmentToken is the bootstrap credential presented on the node's
	// first-ever Register, before it holds a client certificate. Set from
	// GOTHAM_AGENT_ENROLLMENT_TOKEN; needed only when the control plane sets
	// GOTHAM_GRPC_ENROLLMENT_TOKEN. It is a secret: never logged.
	EnrollmentToken string
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
	// ManagedVolumeRoot is the parent of every application bind mount the node
	// accepts; a bind whose source is not inside it is refused.
	ManagedVolumeRoot string
	// LogLevel is the slog level name (debug, info, warn, error).
	LogLevel string

	// Files enables the FileService. It is on by default; only an explicit
	// GOTHAM_AGENT_FILES=false turns it off.
	Files bool
	// FileFullAccess exposes the whole filesystem as one writable root "/" with
	// no protected paths (the operator accepted the risk). It is on by default;
	// GOTHAM_AGENT_FILE_FULL_ACCESS=false falls back to the confined roots.
	FileFullAccess bool
	// FileRoots lists extra browsable roots, colon-separated
	// (GOTHAM_AGENT_FILE_ROOTS). Every entry is read-only.
	FileRoots string
	// FileMaxUpload bounds one upload in bytes (GOTHAM_AGENT_FILE_MAX_UPLOAD).
	FileMaxUpload int64
	// FileMaxTransfers caps concurrent transfers per node
	// (GOTHAM_AGENT_FILE_MAX_TRANSFERS).
	FileMaxTransfers int
	// FileArchiveMaxBytes bounds the total uncompressed bytes one archive
	// operation may read or write (GOTHAM_AGENT_ARCHIVE_MAX_BYTES).
	FileArchiveMaxBytes int64
	// FileArchiveMaxEntries bounds the entry count one archive operation may
	// walk or unpack (GOTHAM_AGENT_ARCHIVE_MAX_ENTRIES).
	FileArchiveMaxEntries int64
	// FileArchiveMaxInputBytes bounds the on-disk size of an archive file that
	// extract will read (GOTHAM_AGENT_ARCHIVE_MAX_INPUT_BYTES).
	FileArchiveMaxInputBytes int64

	// TerminalShell is the absolute shell a host terminal session runs. Empty
	// honors $SHELL when it is bash or zsh, then prefers bash, then zsh, then
	// /bin/sh (GOTHAM_AGENT_TERMINAL_SHELL). The shell runs as the agent's
	// service identity; see the TerminalService contract in
	// proto/agent/v1/agent.proto.
	TerminalShell string
	// TerminalMaxSessions bounds concurrent host terminal sessions on this
	// node (GOTHAM_AGENT_TERMINAL_MAX_SESSIONS). Zero uses the package default.
	TerminalMaxSessions int
	// TerminalIntegration enables the optional bash/zsh startup integration:
	// the integrated shell selection, bash colour defaults and the constant
	// OSC 133 prompt-mark startup files. It is on by default and can be disabled
	// with GOTHAM_AGENT_TERMINAL_INTEGRATION=false.
	TerminalIntegration bool

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
	// default). A stable node refuses beta offers; a beta node accepts both
	// stable and beta releases (the offer carries the release's own channel).
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
		CPAddr:     envOr(envCPAddr, defaultCPAddr),
		NodeID:     envOr(envNodeID, "localhost"),
		ListenAddr: envOr(envListenAddr, defaultListenAddr),
		CA:         os.Getenv(envCA),
		Insecure:   strings.EqualFold(strings.TrimSpace(os.Getenv(envInsecure)), "true"),

		EnrollmentToken: strings.TrimSpace(os.Getenv(envEnrollmentToken)),
		CertDir:         envOr(envCertDir, defaultCertDir),
		KeyFile:         os.Getenv(envKey),
		DockerSock:      dockerSock(),
		ComposeRoot:     envOr(envComposeRoot, docker.DefaultComposeRoot),
		LogLevel:        envOr(envLogLevel, defaultLogLevel),

		ManagedVolumeRoot: envOr(envManagedVolumeRoot, envOr(envSharedManagedVolumeRoot, docker.DefaultManagedVolumeRoot)),

		Files:            !envOff(envFiles),
		FileFullAccess:   !envOff(envFileFullAccess),
		FileRoots:        os.Getenv(envFileRoots),
		FileMaxUpload:    int64FromEnv(envFileMaxUpload, files.DefaultMaxUpload),
		FileMaxTransfers: int(int64FromEnv(envFileMaxTransfers, files.DefaultMaxTransfers)),

		FileArchiveMaxBytes:      int64FromEnv(envFileArchiveMaxBytes, files.DefaultArchiveMaxBytes),
		FileArchiveMaxEntries:    int64FromEnv(envFileArchiveMaxEntries, files.DefaultArchiveMaxEntries),
		FileArchiveMaxInputBytes: int64FromEnv(envFileArchiveMaxInput, files.DefaultArchiveMaxInputBytes),

		TerminalShell:       strings.TrimSpace(os.Getenv(envTerminalShell)),
		TerminalMaxSessions: int(int64FromEnv(envTerminalMaxSessions, 0)),
		TerminalIntegration: !envOff(envTerminalIntegration),

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
	if !validNodeID(cfg.NodeID) {
		return Config{}, fmt.Errorf("%s %q is not a valid node id", envNodeID, cfg.NodeID)
	}
	if cfg.CA == "" {
		if !cfg.Insecure {
			return Config{}, fmt.Errorf("%s is required; set %s=true only for local development", envCA, envInsecure)
		}
		if !isLoopbackListenAddr(cfg.ListenAddr) {
			return Config{}, fmt.Errorf("%s must be a loopback address when %s=true, got %q", envListenAddr, envInsecure, cfg.ListenAddr)
		}
	}
	if !strings.HasPrefix(cfg.ManagedVolumeRoot, "/") {
		return Config{}, fmt.Errorf("%s must be an absolute path", envManagedVolumeRoot)
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
	return envOr(envDockerSock, envOr(envDockerHost, docker.DefaultDockerSocket))
}

// envOr returns the value of key, or fallback when it is unset.
func envOr(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

// maxNodeIDLength is the DNS name maximum, matching the control plane's
// servers.validateNodeID.
const maxNodeIDLength = 253

// validNodeID mirrors the control plane's node-id gate so an agent fails fast
// at startup instead of retrying an identity every Register will reject.
func validNodeID(nodeID string) bool {
	if nodeID == "" || len(nodeID) > maxNodeIDLength {
		return false
	}
	return !strings.ContainsAny(nodeID, "*\\/\x00") && !strings.ContainsAny(nodeID, " \t\r\n")
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

// int64FromEnv parses a positive integer environment value, falling back to
// fallback for an unset, unparseable or non-positive value.
func int64FromEnv(key string, fallback int64) int64 {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || value <= 0 {
		return fallback
	}
	return value
}

// envOff reports whether a default-on switch was explicitly turned off. It
// accepts the usual spellings (false, 0, no, off), so a typo such as "no" cannot
// silently leave a risky default enabled.
func envOff(key string) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(key))) {
	case "false", "0", "no", "off":
		return true
	}
	return false
}

// updateConfig maps the agent configuration onto the self-update package.
func (c Config) updateConfig() updates.Config {
	return updates.Config{
		AutoUpdate:   c.AutoUpdate,
		Interval:     c.UpdateInterval,
		Channel:      c.UpdateChannel,
		BinaryPath:   c.BinaryPath,
		UpdateScript: c.UpdateScript,
		StatusPath:   c.UpdateStatusPath,
		PendingPath:  c.UpdatePendingPath,
		LockPath:     c.UpdateLockPath,
		RetryPath:    c.UpdateRetryPath,
		BackoffPath:  c.UpdateBackoffPath,
		Version:      c.Version,
		Restart:      c.Restart,
	}
}
