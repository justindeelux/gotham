// Package config loads and watches the Gotham control-plane configuration.
//
// Configuration is layered; later sources win:
//
//  1. built-in defaults,
//  2. gotham.yaml in the current working directory (optional),
//  3. environment variables with the GOTHAM_ prefix.
//
// A missing gotham.yaml is not an error: the defaults apply. Hot reload is
// driven by viper's WatchConfig (fsnotify), so no SIGHUP handler is required;
// the operating system reports file changes directly.
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
)

// Environment variables recognised by Load.
const (
	EnvServerAddr            = "GOTHAM_SERVER_ADDR"
	EnvServerPort            = "GOTHAM_SERVER_PORT"
	EnvDatabaseDSN           = "GOTHAM_DATABASE_DSN"
	EnvRedisAddr             = "GOTHAM_REDIS_ADDR"
	EnvLogLevel              = "GOTHAM_LOG_LEVEL"
	EnvLogFormat             = "GOTHAM_LOG_FORMAT"
	EnvAuthJWTPrivateKeyPath = "GOTHAM_AUTH_JWT_PRIVATE_KEY_PATH"
	EnvAuthJWTPublicKeyPath  = "GOTHAM_AUTH_JWT_PUBLIC_KEY_PATH"
	// EnvAuthAllowRegistration reopens self-registration on an instance that
	// already has an account. Test/dev only: production relies on the closed
	// default (one admin, members join through invites, P-A2).
	EnvAuthAllowRegistration = "GOTHAM_AUTH_ALLOW_REGISTRATION"

	EnvOAuthGitHubClientID     = "GOTHAM_OAUTH_GITHUB_CLIENT_ID"
	EnvOAuthGitHubClientSecret = "GOTHAM_OAUTH_GITHUB_CLIENT_SECRET"
	EnvOAuthGitHubRedirectURL  = "GOTHAM_OAUTH_GITHUB_REDIRECT_URL"

	EnvGRPCAddr  = "GOTHAM_GRPC_ADDR"
	EnvGRPCHosts = "GOTHAM_GRPC_HOSTS"
	EnvCADir     = "GOTHAM_CA_DIR"
	EnvSecretKey = "GOTHAM_SECRET_KEY"

	envPrefix = "GOTHAM"
)

// Configuration file settings.
const (
	configName = "gotham"
	configType = "yaml"
)

// Log formats supported by Log.Format.
const (
	FormatJSON = "json"
	FormatText = "text"
)

// Built-in defaults.
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

// Server holds HTTP listener settings.
type Server struct {
	Addr string
	Port int
}

// Database holds PostgreSQL connection settings.
type Database struct {
	DSN string
}

// Redis holds Redis connection settings.
type Redis struct {
	Addr string
}

// GRPC holds the control-plane gRPC listener settings.
type GRPC struct {
	// Addr is the host:port the gateway binds. Default: :9442
	Addr string
	// Hosts are extra DNS names and IPs added to the gRPC listener certificate
	// SANs (for example the control plane's public hostname), so a remote agent
	// dialing by that name verifies. Empty by default; the loopback names and
	// the machine hostname are always present.
	Hosts []string `mapstructure:"hosts"`
}

// CA holds the certificate authority settings.
type CA struct {
	// Dir is the directory holding the CA certificate and key. When no CA is
	// present the gRPC gateway falls back to an insecure development listener.
	// Default: ./data/ca/
	Dir string
}

// Log holds logging settings.
type Log struct {
	Level  string
	Format string
}

// Auth holds authentication settings.
type Auth struct {
	// JWTPrivateKeyPath and JWTPublicKeyPath point at a PEM-encoded Ed25519
	// keypair. When either is empty the server generates an ephemeral keypair
	// and sessions do not survive a restart.
	JWTPrivateKeyPath string `mapstructure:"jwt_private_key_path"`
	JWTPublicKeyPath  string `mapstructure:"jwt_public_key_path"`
	// AllowRegistration reopens self-registration after the first account.
	// Test/dev only; see EnvAuthAllowRegistration.
	AllowRegistration bool `mapstructure:"allow_registration"`
}

// OAuth holds third-party identity provider settings. Each provider is disabled
// while its client credentials are empty.
type OAuth struct {
	GitHub OAuthGitHub `mapstructure:"github"`
}

// OAuthGitHub holds the GitHub OAuth2 application credentials. ClientID and
// ClientSecret are required to enable the provider; RedirectURL is the callback
// URL registered with the GitHub OAuth app.
type OAuthGitHub struct {
	ClientID     string `mapstructure:"client_id"`
	ClientSecret string `mapstructure:"client_secret"`
	RedirectURL  string `mapstructure:"redirect_url"`
}

// Values is the resolved configuration without any runtime bookkeeping. It is
// the value type other packages can copy and hold safely.
type Values struct {
	Server   Server
	Database Database
	Redis    Redis
	GRPC     GRPC
	CA       CA
	Log      Log
	Auth     Auth
	OAuth    OAuth
	// SecretKey encrypts stored SSH private keys (AES-256-GCM). When empty an
	// ephemeral secret is generated at startup and a warning is logged.
	SecretKey string `mapstructure:"secret_key"`
}

// Config is the resolved Gotham configuration. Values are populated by Load and
// refreshed in place on hot reload under an internal mutex. Long-lived
// goroutines should re-read fields (or call Snapshot) after a reload callback
// instead of caching them in locals.
type Config struct {
	Values

	mu     sync.RWMutex
	v      *viper.Viper
	logger *slog.Logger

	watchMu         sync.Mutex
	lastFingerprint [sha256.Size]byte
	haveFingerprint bool
}

// Load builds a Config from defaults, the optional gotham.yaml in the current
// working directory, and GOTHAM_* environment variables.
func Load() (*Config, error) {
	v := newViper()

	if err := readConfigFile(v); err != nil {
		return nil, err
	}

	cfg := &Config{v: v}
	if err := cfg.reload(); err != nil {
		return nil, err
	}
	if fingerprint, ok := cfg.fingerprint(); ok {
		cfg.lastFingerprint = fingerprint
		cfg.haveFingerprint = true
	}
	return cfg, nil
}

// Snapshot returns a consistent copy of the current configuration values.
func (c *Config) Snapshot() Values {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.Values
}

// SetLogger attaches the logger used for hot-reload diagnostics. Call it once,
// after building the logger from the initial Log section.
func (c *Config) SetLogger(logger *slog.Logger) {
	c.mu.Lock()
	c.logger = logger
	c.mu.Unlock()
}

// Watch enables hot reload of gotham.yaml. onChange is invoked after every
// reload that actually changed the file contents. Watch is a no-op when Load
// did not find a config file, because there is nothing to watch.
//
// viper's WatchConfig plus OnConfigChange handle in-place edits. It stops,
// however, when the operating system reports the config file as removed, which
// is exactly what an atomic save looks like to kqueue (macOS): write a temp
// file, remove the target, recreate it. To keep hot reload working across
// editors and platforms, Watch additionally watches the config directory with
// fsnotify and re-applies on change. A content fingerprint de-duplicates the
// two sources, so one edit logs "config changed" once.
func (c *Config) Watch(onChange func()) {
	if c.v == nil || c.v.ConfigFileUsed() == "" {
		return
	}

	c.v.OnConfigChange(func(fsnotify.Event) {
		c.applyChange(onChange)
	})
	c.v.WatchConfig()

	c.watchDirectory(onChange)
}

// watchDirectory is the atomic-save-resilient companion to viper's own watch.
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

// newViper configures a fresh viper instance with defaults, file lookup, and
// environment bindings.
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

	// Explicit bindings document the supported variables and guarantee that
	// Unmarshal sees environment-only values.
	for key, env := range map[string]string{
		"server.addr":               EnvServerAddr,
		"server.port":               EnvServerPort,
		"database.dsn":              EnvDatabaseDSN,
		"redis.addr":                EnvRedisAddr,
		"grpc.addr":                 EnvGRPCAddr,
		"grpc.hosts":                EnvGRPCHosts,
		"ca.dir":                    EnvCADir,
		"secret_key":                EnvSecretKey,
		"log.level":                 EnvLogLevel,
		"log.format":                EnvLogFormat,
		"auth.jwt_private_key_path": EnvAuthJWTPrivateKeyPath,
		"auth.jwt_public_key_path":  EnvAuthJWTPublicKeyPath,
		"auth.allow_registration":   EnvAuthAllowRegistration,

		"oauth.github.client_id":     EnvOAuthGitHubClientID,
		"oauth.github.client_secret": EnvOAuthGitHubClientSecret,
		"oauth.github.redirect_url":  EnvOAuthGitHubRedirectURL,
	} {
		_ = v.BindEnv(key, env)
	}

	return v
}

// readConfigFile loads gotham.yaml when present, tolerating its absence.
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

// reload decodes the current viper state into the Config, applying validation
// before the new values become visible to readers.
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

// applyChange reloads after a file change and reports the outcome. A reload
// that fails validation leaves the previous values in place. Events that carry
// no content change (or arrive while the file is momentarily absent during an
// atomic save) are ignored.
func (c *Config) applyChange(onChange func()) {
	c.watchMu.Lock()
	defer c.watchMu.Unlock()

	fingerprint, ok := c.fingerprint()
	if !ok {
		// The file is momentarily absent (atomic save); the following
		// create/write event will reload it.
		return
	}
	if c.haveFingerprint && fingerprint == c.lastFingerprint {
		return
	}

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

	c.mu.RLock()
	logger := c.logger
	level := c.Log.Level
	format := c.Log.Format
	c.mu.RUnlock()

	if logger != nil {
		logger.Info("config changed",
			"path", c.v.ConfigFileUsed(),
			"log_level", level,
			"log_format", format,
		)
	}
	if onChange != nil {
		onChange()
	}
}

// fingerprint hashes the raw config file so duplicate change events from the
// viper watcher and the directory watcher collapse into a single reload.
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

// logError reports a watcher problem once a logger is attached.
func (c *Config) logError(msg string, err error) {
	c.mu.RLock()
	logger := c.logger
	c.mu.RUnlock()

	if logger != nil {
		logger.Error(msg, "error", err)
	}
}

// validate rejects values that would make the server or logger unusable.
func validate(values Values) error {
	switch strings.ToLower(strings.TrimSpace(values.Log.Format)) {
	case FormatJSON, FormatText:
	default:
		return fmt.Errorf("log.format must be %q or %q, got %q", FormatJSON, FormatText, values.Log.Format)
	}
	if values.Server.Port < minPort || values.Server.Port > maxPort {
		return fmt.Errorf("server.port must be between %d and %d, got %d", minPort, maxPort, values.Server.Port)
	}
	return nil
}
