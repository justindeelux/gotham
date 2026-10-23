package updates

import (
	"log/slog"
	"os"
	"strings"
	"time"
)

const (
	FeatureEnv         = "FEATURE_UPDATES"
	AutoUpdateEnv      = "AUTO_UPDATE"
	AutoIntervalEnv    = "AUTO_UPDATE_INTERVAL"
	ChannelEnv         = "GOTHAM_UPDATE_CHANNEL"
	RepoEnv            = "GOTHAM_UPDATE_REPO"
	BaseURLEnv         = "GOTHAM_UPDATE_BASE_URL"
	ScriptEnv          = "GOTHAM_UPDATE_SCRIPT"
	StatusPathEnv      = "GOTHAM_UPDATE_STATUS"
	PendingPathEnv     = "GOTHAM_UPDATE_PENDING"
	BinaryPathEnv      = "GOTHAM_UPDATE_BINARY"
	LockPathEnv        = "GOTHAM_UPDATE_LOCK"
	BackoffPathEnv     = "GOTHAM_UPDATE_BACKOFF"
	DownloadTimeoutEnv = "GOTHAM_UPDATE_DOWNLOAD_TIMEOUT"
	CurrentEnv         = "GOTHAM_UPDATE_CURRENT"
	OfferCacheTTLEnv   = "GOTHAM_UPDATE_OFFER_CACHE_TTL"
)

func Enabled() bool {
	return !strings.EqualFold(strings.TrimSpace(os.Getenv(FeatureEnv)), "false")
}

func AutoUpdateEnabled() bool {
	return strings.EqualFold(strings.TrimSpace(os.Getenv(AutoUpdateEnv)), "true")
}

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

func ChannelFromEnv() Channel {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(ChannelEnv))) {
	case string(ChannelBeta):
		return ChannelBeta
	default:
		return ChannelStable
	}
}

func RepoFromEnv() string {
	if raw := strings.TrimSpace(os.Getenv(RepoEnv)); raw != "" {
		return raw
	}
	return DefaultRepo
}

func BaseURLFromEnv() string {
	if raw := strings.TrimSpace(os.Getenv(BaseURLEnv)); raw != "" {
		return raw
	}
	return DefaultBaseURL
}

func ScriptFromEnv() string {
	if raw := strings.TrimSpace(os.Getenv(ScriptEnv)); raw != "" {
		return raw
	}
	return DefaultUpdateScript
}

func StatusPathFromEnv() string {
	if raw := strings.TrimSpace(os.Getenv(StatusPathEnv)); raw != "" {
		return raw
	}
	return DefaultStatusPath
}

func PendingPathFromEnv() string {
	if raw := strings.TrimSpace(os.Getenv(PendingPathEnv)); raw != "" {
		return raw
	}
	return DefaultPendingPath
}

func BinaryPathFromEnv() string {
	if raw := strings.TrimSpace(os.Getenv(BinaryPathEnv)); raw != "" {
		return raw
	}
	return DefaultBinaryPath
}

func LockPathFromEnv() string {
	if raw := strings.TrimSpace(os.Getenv(LockPathEnv)); raw != "" {
		return raw
	}
	return DefaultLockPath
}

func BackoffPathFromEnv() string {
	if raw := strings.TrimSpace(os.Getenv(BackoffPathEnv)); raw != "" {
		return raw
	}
	return DefaultBackoffPath
}

func CurrentFromEnv() string {
	return strings.TrimSpace(os.Getenv(CurrentEnv))
}

func OfferCacheTTLFromEnv() time.Duration {
	const fallback = 10 * time.Minute
	raw := strings.TrimSpace(os.Getenv(OfferCacheTTLEnv))
	if raw == "" {
		return fallback
	}
	d, err := time.ParseDuration(raw)
	if err != nil || d <= 0 {
		return fallback
	}
	return d
}

func DownloadTimeoutFromEnv() time.Duration {
	const fallback = 10 * time.Minute
	raw := strings.TrimSpace(os.Getenv(DownloadTimeoutEnv))
	if raw == "" {
		return fallback
	}
	d, err := time.ParseDuration(raw)
	if err != nil || d <= 0 {
		return fallback
	}
	return d
}

func FromEnv(current string, logger *slog.Logger) (Config, error) {
	cfg := Config{
		Current:         current,
		Repo:            RepoFromEnv(),
		BaseURL:         BaseURLFromEnv(),
		Channel:         ChannelFromEnv(),
		UpdateScript:    ScriptFromEnv(),
		StatusPath:      StatusPathFromEnv(),
		PendingPath:     PendingPathFromEnv(),
		BinaryPath:      BinaryPathFromEnv(),
		LockPath:        LockPathFromEnv(),
		BackoffPath:     BackoffPathFromEnv(),
		Logger:          logger,
		Auto:            AutoUpdateEnabled(),
		AutoInterval:    AutoIntervalFromEnv(),
		DownloadTimeout: DownloadTimeoutFromEnv(),
	}
	publicKeys, err := LoadPublicKeys()
	if err != nil {
		return cfg, err
	}
	cfg.PublicKeys = publicKeys
	return cfg, nil
}
