package updates

import (
	"log/slog"
	"os"
	"strings"
	"time"
)

// Feature flags read directly from the environment, matching the pattern used
// by the other optional surfaces (for example services.FeatureEnv).
const (
	// FeatureEnv is the kill switch for the whole self-update surface:
	// FEATURE_UPDATES=false removes the routes and the auto-update loop.
	FeatureEnv = "FEATURE_UPDATES"
	// AutoUpdateEnv enables the scheduled check/apply loop when set to "true".
	// It is off by default so an unattended upgrade never happens by accident.
	AutoUpdateEnv = "AUTO_UPDATE"
	// AutoIntervalEnv overrides the auto-update interval (Go duration).
	AutoIntervalEnv = "AUTO_UPDATE_INTERVAL"
	// ChannelEnv selects the release channel: stable (default) or beta.
	ChannelEnv = "GOTHAM_UPDATE_CHANNEL"
	// RepoEnv overrides the GitHub repository (owner/name).
	RepoEnv = "GOTHAM_UPDATE_REPO"
	// BaseURLEnv overrides the release API base URL (for a GitHub Enterprise
	// host or a local mirror). Must be http(s); http is only accepted for
	// loopback hosts.
	BaseURLEnv = "GOTHAM_UPDATE_BASE_URL"
	// ScriptEnv overrides the installed restart/healthcheck wrapper path.
	ScriptEnv = "GOTHAM_UPDATE_SCRIPT"
	// CurrentEnv overrides the running version (useful for wrappers and
	// tests). When unset the version reported by the node registry is used.
	CurrentEnv = "GOTHAM_UPDATE_CURRENT"
)

// Enabled reports whether the self-update surface is available; it is on unless
// FEATURE_UPDATES=false.
func Enabled() bool {
	return !strings.EqualFold(strings.TrimSpace(os.Getenv(FeatureEnv)), "false")
}

// AutoUpdateEnabled reports whether the scheduled check/apply loop should run.
func AutoUpdateEnabled() bool {
	return strings.EqualFold(strings.TrimSpace(os.Getenv(AutoUpdateEnv)), "true")
}

// AutoIntervalFromEnv returns the configured auto-update interval, defaulting
// to 6h. Invalid or non-positive values fall back to the default.
func AutoIntervalFromEnv() time.Duration {
	const fallback = 6 * time.Hour
	raw := strings.TrimSpace(os.Getenv(AutoIntervalEnv))
	if raw == "" {
		return fallback
	}
	d, err := time.ParseDuration(raw)
	if err != nil || d <= 0 {
		return fallback
	}
	return d
}

// ChannelFromEnv returns the configured channel, defaulting to stable.
func ChannelFromEnv() Channel {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(ChannelEnv))) {
	case string(ChannelBeta):
		return ChannelBeta
	default:
		return ChannelStable
	}
}

// RepoFromEnv returns the configured repository, defaulting to the Gotham
// release repository.
func RepoFromEnv() string {
	if raw := strings.TrimSpace(os.Getenv(RepoEnv)); raw != "" {
		return raw
	}
	return DefaultRepo
}

// BaseURLFromEnv returns the configured release API base URL, defaulting to the
// public GitHub API.
func BaseURLFromEnv() string {
	if raw := strings.TrimSpace(os.Getenv(BaseURLEnv)); raw != "" {
		return raw
	}
	return DefaultBaseURL
}

// ScriptFromEnv returns the configured restart wrapper path.
func ScriptFromEnv() string {
	if raw := strings.TrimSpace(os.Getenv(ScriptEnv)); raw != "" {
		return raw
	}
	return DefaultUpdateScript
}

// CurrentFromEnv returns the configured running version, or "" when unset.
func CurrentFromEnv() string {
	return strings.TrimSpace(os.Getenv(CurrentEnv))
}

// FromEnv assembles a self-update Config from the environment for the given
// running version and health URL. A missing or malformed public key is
// returned as err with a usable Config (Apply then fails closed), so callers
// can log it and still serve the check route.
func FromEnv(current, healthURL string, logger *slog.Logger) (Config, error) {
	cfg := Config{
		Current:      current,
		Repo:         RepoFromEnv(),
		BaseURL:      BaseURLFromEnv(),
		Channel:      ChannelFromEnv(),
		UpdateScript: ScriptFromEnv(),
		HealthURL:    healthURL,
		Logger:       logger,
		Auto:         AutoUpdateEnabled(),
		AutoInterval: AutoIntervalFromEnv(),
	}
	publicKey, err := LoadPublicKey()
	if err != nil {
		return cfg, err
	}
	cfg.PublicKey = publicKey
	return cfg, nil
}
