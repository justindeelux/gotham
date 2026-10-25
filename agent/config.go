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

	envManagedVolumeRoot       = "GOTHAM_AGENT_MANAGED_VOLUME_ROOT"
	envSharedManagedVolumeRoot = "GOTHAM_MANAGED_VOLUME_ROOT"

	envFiles                 = "GOTHAM_AGENT_FILES"
	envFileFullAccess        = "GOTHAM_AGENT_FILE_FULL_ACCESS"
	envFileRoots             = "GOTHAM_AGENT_FILE_ROOTS"
	envFileMaxUpload         = "GOTHAM_AGENT_FILE_MAX_UPLOAD"
	envFileMaxTransfers      = "GOTHAM_AGENT_FILE_MAX_TRANSFERS"
	envFileArchiveMaxBytes   = "GOTHAM_AGENT_ARCHIVE_MAX_BYTES"
	envFileArchiveMaxEntries = "GOTHAM_AGENT_ARCHIVE_MAX_ENTRIES"
	envFileArchiveMaxInput   = "GOTHAM_AGENT_ARCHIVE_MAX_INPUT_BYTES"
	envFileTrashMaxAgeDays   = "GOTHAM_AGENT_FILE_TRASH_MAX_AGE_DAYS"
	envFileTrashMaxBytes     = "GOTHAM_AGENT_FILE_TRASH_MAX_BYTES"

	envTerminalShell       = "GOTHAM_AGENT_TERMINAL_SHELL"
	envTerminalMaxSessions = "GOTHAM_AGENT_TERMINAL_MAX_SESSIONS"
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

const (
	defaultCPAddr            = "127.0.0.1:9442"
	defaultListenAddr        = ":9443"
	defaultCertDir           = "./data/agent"
	defaultLogLevel          = "info"
	defaultAgentStateRoot    = "/var/lib/gotham-agent"
	defaultAgentBinary       = "/var/lib/gotham-agent/bin/gotham-agent"
	defaultAgentUpdateScript = "/usr/libexec/gotham/gotham-agent-update"
	defaultAgentStatusPath   = "/var/lib/gotham-agent-updater/update.status"
	defaultAgentPendingPath  = "/var/lib/gotham-agent/update.pending"
	defaultAgentLockPath     = "/var/lib/gotham-agent/update.lock"
	defaultAgentRetryPath    = "/var/lib/gotham-agent/update.retry"
	defaultAgentBackoffPath  = "/var/lib/gotham-agent/update.backoff"
	defaultAgentHealthAddr   = "127.0.0.1:8001"
	defaultUpdateInterval    = 5 * time.Minute
	defaultUpdateChannel     = updates.DefaultChannel
)

type Config struct {
	CPAddr            string
	NodeID            string
	ListenAddr        string
	CA                string
	Insecure          bool
	EnrollmentToken   string
	CertDir           string
	KeyFile           string
	DockerSock        string
	ComposeRoot       string
	ManagedVolumeRoot string
	LogLevel          string

	Files                    bool
	FileFullAccess           bool
	FileRoots                string
	FileMaxUpload            int64
	FileMaxTransfers         int
	FileArchiveMaxBytes      int64
	FileArchiveMaxEntries    int64
	FileArchiveMaxInputBytes int64
	FileTrashMaxAgeDays      int
	FileTrashMaxBytes        int64

	TerminalShell       string
	TerminalMaxSessions int
	TerminalIntegration bool

	Version           string
	AutoUpdate        bool
	UpdateInterval    time.Duration
	UpdateChannel     string
	BinaryPath        string
	UpdateScript      string
	UpdateStatusPath  string
	UpdatePendingPath string
	UpdateLockPath    string
	UpdateRetryPath   string
	UpdateBackoffPath string
	HealthAddr        string
	Restart           updatecore.RestartFunc
}

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
		FileTrashMaxAgeDays:      int(int64FromEnvAllowingZero(envFileTrashMaxAgeDays, files.DefaultTrashMaxAgeDays)),
		FileTrashMaxBytes:        int64FromEnvAllowingZero(envFileTrashMaxBytes, 0),

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

func NewLogger(w io.Writer, level string) *slog.Logger {
	parsed, err := parseLevel(level)
	if err != nil {
		parsed = slog.LevelInfo
	}
	return slog.New(slog.NewTextHandler(w, &slog.HandlerOptions{Level: parsed}))
}

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

func dockerSock() string {
	return envOr(envDockerSock, envOr(envDockerHost, docker.DefaultDockerSocket))
}

func envOr(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

const maxNodeIDLength = 253

func validNodeID(nodeID string) bool {
	if nodeID == "" || len(nodeID) > maxNodeIDLength {
		return false
	}
	return !strings.ContainsAny(nodeID, "*\\/\x00") && !strings.ContainsAny(nodeID, " \t\r\n")
}

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

// int64FromEnvAllowingZero is int64FromEnv for a setting whose zero is
// meaningful (retention off); a negative or unparseable value still falls back.
func int64FromEnvAllowingZero(key string, fallback int64) int64 {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || value < 0 {
		return fallback
	}
	return value
}

func envOff(key string) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(key))) {
	case "false", "0", "no", "off":
		return true
	}
	return false
}

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
