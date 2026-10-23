package config

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/fsnotify/fsnotify"
	"github.com/spf13/viper"

	"github.com/justindeelux/gotham/internal/clientip"
)

const (
	EnvServerAddr            = "GOTHAM_SERVER_ADDR"
	EnvServerPort            = "GOTHAM_SERVER_PORT"
	EnvTrustedProxies        = "GOTHAM_TRUSTED_PROXIES"
	EnvDatabaseDSN           = "GOTHAM_DATABASE_DSN"
	EnvRedisAddr             = "GOTHAM_REDIS_ADDR"
	EnvLogLevel              = "GOTHAM_LOG_LEVEL"
	EnvLogFormat             = "GOTHAM_LOG_FORMAT"
	EnvAuthJWTPrivateKeyPath = "GOTHAM_AUTH_JWT_PRIVATE_KEY_PATH"
	EnvAuthJWTPublicKeyPath  = "GOTHAM_AUTH_JWT_PUBLIC_KEY_PATH"
	EnvAuthAllowRegistration = "GOTHAM_AUTH_ALLOW_REGISTRATION"

	EnvOAuthGitHubClientID     = "GOTHAM_OAUTH_GITHUB_CLIENT_ID"
	EnvOAuthGitHubClientSecret = "GOTHAM_OAUTH_GITHUB_CLIENT_SECRET"
	EnvOAuthGitHubRedirectURL  = "GOTHAM_OAUTH_GITHUB_REDIRECT_URL"

	EnvGRPCAddr                = "GOTHAM_GRPC_ADDR"
	EnvGRPCHosts               = "GOTHAM_GRPC_HOSTS"
	EnvGRPCInsecure            = "GOTHAM_GRPC_INSECURE"
	EnvGRPCEnrollmentToken     = "GOTHAM_GRPC_ENROLLMENT_TOKEN"
	EnvGRPCAllowOpenEnrollment = "GOTHAM_GRPC_ALLOW_OPEN_ENROLLMENT"
	EnvGRPCLegacyLeafUntil     = "GOTHAM_GRPC_LEGACY_LEAF_UNTIL"
	EnvCADir                   = "GOTHAM_CA_DIR"
	EnvSecretKey               = "GOTHAM_SECRET_KEY"

	EnvDeployGitAllowPrivateHosts = "GOTHAM_GIT_ALLOW_PRIVATE_HOSTS"

	envPrefix = "GOTHAM"
)

const (
	configName = "gotham"
	configType = "yaml"
)

const (
	FormatJSON = "json"
	FormatText = "text"
)

const (
	defaultServerAddr  = "0.0.0.0"
	defaultServerPort  = 8000
	defaultDatabaseDSN = "postgres://gotham:gotham@localhost:5432/gotham?sslmode=disable"
	defaultRedisAddr   = "localhost:6379"
	defaultLogLevel    = "info"
	defaultLogFormat   = FormatJSON
	defaultGRPCAddr    = ":9442"
	defaultCADir       = "./data/ca/"

	minPort = 1
	maxPort = 65535
)

type Server struct {
	Addr           string
	Port           int
	TrustedProxies []string `mapstructure:"trusted_proxies"`
}

type Database struct {
	DSN string
}

type Redis struct {
	Addr string
}

type GRPC struct {
	Addr                string
	Hosts               []string `mapstructure:"hosts"`
	Insecure            bool     `mapstructure:"insecure"`
	EnrollmentToken     string   `mapstructure:"enrollment_token"`
	AllowOpenEnrollment bool     `mapstructure:"allow_open_enrollment"`
	LegacyLeafUntil     string   `mapstructure:"legacy_leaf_until"`
}

type CA struct {
	Dir string
}

type Deploy struct {
	GitAllowPrivateHosts bool `mapstructure:"git_allow_private_hosts"`
}

type Log struct {
	Level  string
	Format string
}

type Auth struct {
	JWTPrivateKeyPath string `mapstructure:"jwt_private_key_path"`
	JWTPublicKeyPath  string `mapstructure:"jwt_public_key_path"`
	AllowRegistration bool   `mapstructure:"allow_registration"`
}

type OAuth struct {
	GitHub OAuthGitHub `mapstructure:"github"`
}

type OAuthGitHub struct {
	ClientID     string `mapstructure:"client_id"`
	ClientSecret string `mapstructure:"client_secret"`
	RedirectURL  string `mapstructure:"redirect_url"`
}

type Values struct {
	Server    Server
	Database  Database
	Redis     Redis
	GRPC      GRPC
	CA        CA
	Deploy    Deploy
	Log       Log
	Auth      Auth
	OAuth     OAuth
	SecretKey string `mapstructure:"secret_key"`
}

type Config struct {
	Values

	mu     sync.RWMutex
	v      *viper.Viper
	logger *slog.Logger

	watchMu          sync.Mutex
	lastFingerprint  [sha256.Size]byte
	haveFingerprint  bool
	startupLogFormat string
}

func Load() (*Config, error) {
	v := newViper()

	if err := readConfigFile(v); err != nil {
		return nil, err
	}

	cfg := &Config{v: v}
	if err := cfg.reload(); err != nil {
		return nil, err
	}
	cfg.startupLogFormat = cfg.Log.Format
	if fingerprint, ok := cfg.fingerprint(); ok {
		cfg.lastFingerprint = fingerprint
		cfg.haveFingerprint = true
	}
	return cfg, nil
}

func (c *Config) Snapshot() Values {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.Values
}

func (c *Config) SetLogger(logger *slog.Logger) {
	c.mu.Lock()
	c.logger = logger
	c.mu.Unlock()
}

func (c *Config) Watch(onChange func()) {
	if c.v == nil || c.v.ConfigFileUsed() == "" {
		return
	}
	c.watchDirectory(onChange)
}

func (c *Config) watchDirectory(onChange func()) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		c.logError("failed to create config watcher", err)
		return
	}

	configFile := filepath.Clean(c.v.ConfigFileUsed())
	if err := watcher.Add(filepath.Dir(configFile)); err != nil {
		c.logError("failed to watch config directory", err)
		_ = watcher.Close()
		return
	}

	go func() {
		defer func() { _ = watcher.Close() }()
		for {
			select {
			case event, ok := <-watcher.Events:
				if !ok {
					return
				}
				if filepath.Clean(event.Name) != configFile {
					continue
				}
				if event.Op&(fsnotify.Write|fsnotify.Create|fsnotify.Rename) == 0 {
					continue
				}
				c.applyChange(onChange)
			case err, ok := <-watcher.Errors:
				if !ok {
					return
				}
				c.logError("config watcher error", err)
			}
		}
	}()
}

func newViper() *viper.Viper {
	v := viper.New()
	v.SetConfigName(configName)
	v.SetConfigType(configType)
	v.AddConfigPath(".")

	v.SetDefault("server.addr", defaultServerAddr)
	v.SetDefault("server.port", defaultServerPort)
	v.SetDefault("database.dsn", defaultDatabaseDSN)
	v.SetDefault("redis.addr", defaultRedisAddr)
	v.SetDefault("grpc.addr", defaultGRPCAddr)
	v.SetDefault("ca.dir", defaultCADir)
	v.SetDefault("log.level", defaultLogLevel)
	v.SetDefault("log.format", defaultLogFormat)

	v.SetEnvPrefix(envPrefix)
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	for key, env := range map[string]string{
		"server.addr":                    EnvServerAddr,
		"server.port":                    EnvServerPort,
		"server.trusted_proxies":         EnvTrustedProxies,
		"database.dsn":                   EnvDatabaseDSN,
		"redis.addr":                     EnvRedisAddr,
		"grpc.addr":                      EnvGRPCAddr,
		"grpc.hosts":                     EnvGRPCHosts,
		"grpc.insecure":                  EnvGRPCInsecure,
		"grpc.enrollment_token":          EnvGRPCEnrollmentToken,
		"grpc.allow_open_enrollment":     EnvGRPCAllowOpenEnrollment,
		"grpc.legacy_leaf_until":         EnvGRPCLegacyLeafUntil,
		"ca.dir":                         EnvCADir,
		"secret_key":                     EnvSecretKey,
		"deploy.git_allow_private_hosts": EnvDeployGitAllowPrivateHosts,
		"log.level":                      EnvLogLevel,
		"log.format":                     EnvLogFormat,
		"auth.jwt_private_key_path":      EnvAuthJWTPrivateKeyPath,
		"auth.jwt_public_key_path":       EnvAuthJWTPublicKeyPath,
		"auth.allow_registration":        EnvAuthAllowRegistration,

		"oauth.github.client_id":     EnvOAuthGitHubClientID,
		"oauth.github.client_secret": EnvOAuthGitHubClientSecret,
		"oauth.github.redirect_url":  EnvOAuthGitHubRedirectURL,
	} {
		_ = v.BindEnv(key, env)
	}

	return v
}

func readConfigFile(v *viper.Viper) error {
	if err := v.ReadInConfig(); err != nil {
		var notFound viper.ConfigFileNotFoundError
		if errors.As(err, &notFound) {
			return nil
		}
		return fmt.Errorf("read config file: %w", err)
	}
	return nil
}

func (c *Config) reload() error {
	var values Values
	if err := c.v.Unmarshal(&values); err != nil {
		return fmt.Errorf("decode config: %w", err)
	}
	if err := validate(values); err != nil {
		return err
	}

	c.mu.Lock()
	c.Values = values
	c.mu.Unlock()
	return nil
}

func (c *Config) applyChange(onChange func()) {
	c.watchMu.Lock()
	defer c.watchMu.Unlock()

	fingerprint, ok := c.fingerprint()
	if !ok {
		return
	}
	if c.haveFingerprint && fingerprint == c.lastFingerprint {
		return
	}

	c.mu.RLock()
	logger := c.logger
	previousFormat := c.Log.Format
	c.mu.RUnlock()

	if err := c.v.ReadInConfig(); err != nil {
		c.logError("config read failed", err)
		return
	}
	if err := c.reload(); err != nil {
		c.logError("config reload failed", err)
		return
	}
	c.lastFingerprint = fingerprint
	c.haveFingerprint = true

	snap := c.Snapshot()

	if logger != nil {
		logger.Info("config changed",
			"path", c.v.ConfigFileUsed(),
			"log_level", snap.Log.Level,
		)
		if !strings.EqualFold(strings.TrimSpace(previousFormat), strings.TrimSpace(snap.Log.Format)) &&
			!strings.EqualFold(strings.TrimSpace(c.startupLogFormat), strings.TrimSpace(snap.Log.Format)) {
			logger.Warn("log.format changed; restart required to apply",
				"log_format", snap.Log.Format)
		}
	}
	if onChange != nil {
		onChange()
	}
}

func (c *Config) fingerprint() ([sha256.Size]byte, bool) {
	path := c.v.ConfigFileUsed()
	if path == "" {
		return [sha256.Size]byte{}, false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return [sha256.Size]byte{}, false
	}
	return sha256.Sum256(data), true
}

func (c *Config) logError(msg string, err error) {
	c.mu.RLock()
	logger := c.logger
	c.mu.RUnlock()

	if logger != nil {
		logger.Error(msg, "error", err)
	}
}

func validate(values Values) error {
	switch strings.ToLower(strings.TrimSpace(values.Log.Format)) {
	case FormatJSON, FormatText:
	default:
		return fmt.Errorf("log.format must be %q or %q, got %q", FormatJSON, FormatText, values.Log.Format)
	}
	if values.Server.Port < minPort || values.Server.Port > maxPort {
		return fmt.Errorf("server.port must be between %d and %d, got %d", minPort, maxPort, values.Server.Port)
	}
	if _, err := clientip.Parse(values.Server.TrustedProxies); err != nil {
		return fmt.Errorf("server.trusted_proxies: %w", err)
	}
	return nil
}
