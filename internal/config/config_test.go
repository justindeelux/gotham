package config

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// gothamEnvKeys lists every environment variable Load understands.
var gothamEnvKeys = []string{
	EnvServerAddr,
	EnvServerPort,
	EnvTrustedProxies,
	EnvDatabaseDSN,
	EnvRedisAddr,
	EnvLogLevel,
	EnvLogFormat,
	EnvAuthJWTPrivateKeyPath,
	EnvAuthJWTPublicKeyPath,
	EnvOAuthGitHubClientID,
	EnvOAuthGitHubClientSecret,
	EnvOAuthGitHubRedirectURL,
	EnvGRPCAddr,
	EnvCADir,
	EnvSecretKey,
	EnvDeployGitAllowPrivateHosts,
}

// chdir switches into dir for the duration of the test and restores the
// previous working directory afterwards.
func chdir(t *testing.T, dir string) {
	t.Helper()

	original, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir %s: %v", dir, err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(original); err != nil {
			t.Fatalf("restore cwd: %v", err)
		}
	})
}

// clearGothamEnv removes any inherited GOTHAM_* variables for the test and
// restores them afterwards.
func clearGothamEnv(t *testing.T) {
	t.Helper()

	for _, key := range gothamEnvKeys {
		value, ok := os.LookupEnv(key)
		if !ok {
			continue
		}
		t.Setenv(key, value) // register restore
		if err := os.Unsetenv(key); err != nil {
			t.Fatalf("unset %s: %v", key, err)
		}
	}
}

// writeConfig creates gotham.yaml with contents under dir.
func writeConfig(t *testing.T, dir, contents string) {
	t.Helper()

	path := filepath.Join(dir, "gotham.yaml")
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func TestLoadDefaults(t *testing.T) {
	clearGothamEnv(t)
	chdir(t, t.TempDir())

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if cfg.Server.Addr != defaultServerAddr {
		t.Errorf("Server.Addr = %q, want %q", cfg.Server.Addr, defaultServerAddr)
	}
	if cfg.Server.Port != defaultServerPort {
		t.Errorf("Server.Port = %d, want %d", cfg.Server.Port, defaultServerPort)
	}
	if cfg.Database.DSN != defaultDatabaseDSN {
		t.Errorf("Database.DSN = %q, want %q", cfg.Database.DSN, defaultDatabaseDSN)
	}
	if cfg.Redis.Addr != defaultRedisAddr {
		t.Errorf("Redis.Addr = %q, want %q", cfg.Redis.Addr, defaultRedisAddr)
	}
	if cfg.Log.Level != defaultLogLevel {
		t.Errorf("Log.Level = %q, want %q", cfg.Log.Level, defaultLogLevel)
	}
	if cfg.Log.Format != defaultLogFormat {
		t.Errorf("Log.Format = %q, want %q", cfg.Log.Format, defaultLogFormat)
	}
}

func TestLoadYAMLOverridesDefaults(t *testing.T) {
	clearGothamEnv(t)

	dir := t.TempDir()
	writeConfig(t, dir, `
server:
  addr: 127.0.0.1
  port: 9090
database:
  dsn: postgres://gotham:secret@db.internal:5432/gotham?sslmode=require
redis:
  addr: redis.internal:6380
log:
  level: debug
  format: text
`)
	chdir(t, dir)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if cfg.Server.Addr != "127.0.0.1" {
		t.Errorf("Server.Addr = %q, want 127.0.0.1", cfg.Server.Addr)
	}
	if cfg.Server.Port != 9090 {
		t.Errorf("Server.Port = %d, want 9090", cfg.Server.Port)
	}
	if cfg.Database.DSN != "postgres://gotham:secret@db.internal:5432/gotham?sslmode=require" {
		t.Errorf("Database.DSN = %q", cfg.Database.DSN)
	}
	if cfg.Redis.Addr != "redis.internal:6380" {
		t.Errorf("Redis.Addr = %q, want redis.internal:6380", cfg.Redis.Addr)
	}
	if cfg.Log.Level != "debug" {
		t.Errorf("Log.Level = %q, want debug", cfg.Log.Level)
	}
	if cfg.Log.Format != FormatText {
		t.Errorf("Log.Format = %q, want %q", cfg.Log.Format, FormatText)
	}
}

func TestEnvOverridesYAML(t *testing.T) {
	clearGothamEnv(t)

	dir := t.TempDir()
	writeConfig(t, dir, `
server:
  addr: 127.0.0.1
  port: 9090
log:
  level: debug
`)
	chdir(t, dir)

	t.Setenv(EnvServerPort, "7000")
	t.Setenv(EnvLogLevel, "error")
	t.Setenv(EnvRedisAddr, "env-redis:6379")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if cfg.Server.Port != 7000 {
		t.Errorf("Server.Port = %d, want 7000 (env wins over yaml)", cfg.Server.Port)
	}
	if cfg.Log.Level != "error" {
		t.Errorf("Log.Level = %q, want error (env wins over yaml)", cfg.Log.Level)
	}
	if cfg.Redis.Addr != "env-redis:6379" {
		t.Errorf("Redis.Addr = %q, want env-redis:6379 (env-only key)", cfg.Redis.Addr)
	}
	if cfg.Server.Addr != "127.0.0.1" {
		t.Errorf("Server.Addr = %q, want 127.0.0.1 (from yaml)", cfg.Server.Addr)
	}
	if cfg.Log.Format != defaultLogFormat {
		t.Errorf("Log.Format = %q, want default %q", cfg.Log.Format, defaultLogFormat)
	}
}

func TestLoadAuthKeys(t *testing.T) {
	clearGothamEnv(t)

	dir := t.TempDir()
	writeConfig(t, dir, `
auth:
  jwt_private_key_path: /etc/gotham/private.pem
  jwt_public_key_path: /etc/gotham/public.pem
`)
	chdir(t, dir)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if cfg.Auth.JWTPrivateKeyPath != "/etc/gotham/private.pem" {
		t.Errorf("Auth.JWTPrivateKeyPath = %q", cfg.Auth.JWTPrivateKeyPath)
	}
	if cfg.Auth.JWTPublicKeyPath != "/etc/gotham/public.pem" {
		t.Errorf("Auth.JWTPublicKeyPath = %q", cfg.Auth.JWTPublicKeyPath)
	}
}

func TestLoadAuthKeysFromEnv(t *testing.T) {
	clearGothamEnv(t)
	chdir(t, t.TempDir())

	t.Setenv(EnvAuthJWTPrivateKeyPath, "/env/private.pem")
	t.Setenv(EnvAuthJWTPublicKeyPath, "/env/public.pem")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if cfg.Auth.JWTPrivateKeyPath != "/env/private.pem" {
		t.Errorf("Auth.JWTPrivateKeyPath = %q, want /env/private.pem", cfg.Auth.JWTPrivateKeyPath)
	}
	if cfg.Auth.JWTPublicKeyPath != "/env/public.pem" {
		t.Errorf("Auth.JWTPublicKeyPath = %q, want /env/public.pem", cfg.Auth.JWTPublicKeyPath)
	}
}

func TestLoadOAuthGitHub(t *testing.T) {
	clearGothamEnv(t)

	dir := t.TempDir()
	writeConfig(t, dir, `
oauth:
  github:
    client_id: yaml-client
    client_secret: yaml-secret
    redirect_url: http://localhost:8000/api/v1/auth/oauth/github/callback
`)
	chdir(t, dir)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if cfg.OAuth.GitHub.ClientID != "yaml-client" {
		t.Errorf("OAuth.GitHub.ClientID = %q", cfg.OAuth.GitHub.ClientID)
	}
	if cfg.OAuth.GitHub.ClientSecret != "yaml-secret" {
		t.Errorf("OAuth.GitHub.ClientSecret = %q", cfg.OAuth.GitHub.ClientSecret)
	}
	if cfg.OAuth.GitHub.RedirectURL != "http://localhost:8000/api/v1/auth/oauth/github/callback" {
		t.Errorf("OAuth.GitHub.RedirectURL = %q", cfg.OAuth.GitHub.RedirectURL)
	}
}

func TestLoadOAuthGitHubFromEnv(t *testing.T) {
	clearGothamEnv(t)
	chdir(t, t.TempDir())

	t.Setenv(EnvOAuthGitHubClientID, "env-client")
	t.Setenv(EnvOAuthGitHubClientSecret, "env-secret")
	t.Setenv(EnvOAuthGitHubRedirectURL, "https://gotham.example/api/v1/auth/oauth/github/callback")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if cfg.OAuth.GitHub.ClientID != "env-client" {
		t.Errorf("OAuth.GitHub.ClientID = %q, want env-client", cfg.OAuth.GitHub.ClientID)
	}
	if cfg.OAuth.GitHub.ClientSecret != "env-secret" {
		t.Errorf("OAuth.GitHub.ClientSecret = %q, want env-secret", cfg.OAuth.GitHub.ClientSecret)
	}
	if cfg.OAuth.GitHub.RedirectURL != "https://gotham.example/api/v1/auth/oauth/github/callback" {
		t.Errorf("OAuth.GitHub.RedirectURL = %q", cfg.OAuth.GitHub.RedirectURL)
	}
}

func TestLoadGRPCAndCADefaults(t *testing.T) {
	clearGothamEnv(t)
	chdir(t, t.TempDir())

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if cfg.GRPC.Addr != defaultGRPCAddr {
		t.Errorf("GRPC.Addr = %q, want %q", cfg.GRPC.Addr, defaultGRPCAddr)
	}
	if cfg.CA.Dir != defaultCADir {
		t.Errorf("CA.Dir = %q, want %q", cfg.CA.Dir, defaultCADir)
	}
	if cfg.SecretKey != "" {
		t.Errorf("SecretKey = %q, want empty by default", cfg.SecretKey)
	}
}

func TestLoadGRPCAndCAFromYAML(t *testing.T) {
	clearGothamEnv(t)

	dir := t.TempDir()
	writeConfig(t, dir, `
grpc:
  addr: 127.0.0.1:9442
ca:
  dir: /etc/gotham/ca
secret_key: yaml-secret
`)
	chdir(t, dir)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if cfg.GRPC.Addr != "127.0.0.1:9442" {
		t.Errorf("GRPC.Addr = %q", cfg.GRPC.Addr)
	}
	if cfg.CA.Dir != "/etc/gotham/ca" {
		t.Errorf("CA.Dir = %q", cfg.CA.Dir)
	}
	if cfg.SecretKey != "yaml-secret" {
		t.Errorf("SecretKey = %q", cfg.SecretKey)
	}
}

func TestLoadGRPCAndCAFromEnv(t *testing.T) {
	clearGothamEnv(t)
	chdir(t, t.TempDir())

	t.Setenv(EnvGRPCAddr, "0.0.0.0:19442")
	t.Setenv(EnvCADir, "/env/ca")
	t.Setenv(EnvSecretKey, "env-secret")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if cfg.GRPC.Addr != "0.0.0.0:19442" {
		t.Errorf("GRPC.Addr = %q", cfg.GRPC.Addr)
	}
	if cfg.CA.Dir != "/env/ca" {
		t.Errorf("CA.Dir = %q", cfg.CA.Dir)
	}
	if cfg.SecretKey != "env-secret" {
		t.Errorf("SecretKey = %q", cfg.SecretKey)
	}
	if len(cfg.GRPC.Hosts) != 0 {
		t.Errorf("GRPC.Hosts = %v, want empty when unset", cfg.GRPC.Hosts)
	}
}

func TestLoadGRPCHostsFromEnv(t *testing.T) {
	clearGothamEnv(t)
	chdir(t, t.TempDir())

	t.Setenv(EnvGRPCHosts, "cp.example.com,192.0.2.10")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	want := []string{"cp.example.com", "192.0.2.10"}
	if len(cfg.GRPC.Hosts) != len(want) {
		t.Fatalf("GRPC.Hosts = %v, want %v", cfg.GRPC.Hosts, want)
	}
	for i := range want {
		if cfg.GRPC.Hosts[i] != want[i] {
			t.Fatalf("GRPC.Hosts = %v, want %v", cfg.GRPC.Hosts, want)
		}
	}
}

func TestLoadGRPCInsecureFromEnv(t *testing.T) {
	clearGothamEnv(t)
	chdir(t, t.TempDir())

	if cfg, err := Load(); err != nil || cfg.GRPC.Insecure {
		t.Fatalf("Load() = (%v, %v), want Insecure=false by default", cfg.GRPC.Insecure, err)
	}

	t.Setenv(EnvGRPCInsecure, "true")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !cfg.GRPC.Insecure {
		t.Error("GRPC.Insecure = false, want true after GOTHAM_GRPC_INSECURE=true")
	}
}

func TestLoadDeployGitAllowPrivateHosts(t *testing.T) {
	clearGothamEnv(t)
	chdir(t, t.TempDir())

	if cfg, err := Load(); err != nil || cfg.Deploy.GitAllowPrivateHosts {
		t.Fatalf("Load() = (%v, %v), want GitAllowPrivateHosts=false by default", cfg.Deploy.GitAllowPrivateHosts, err)
	}

	t.Setenv(EnvDeployGitAllowPrivateHosts, "true")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !cfg.Deploy.GitAllowPrivateHosts {
		t.Error("Deploy.GitAllowPrivateHosts = false, want true after GOTHAM_GIT_ALLOW_PRIVATE_HOSTS=true")
	}
}

func TestLoadDeployGitAllowPrivateHostsFromYAML(t *testing.T) {
	clearGothamEnv(t)

	dir := t.TempDir()
	writeConfig(t, dir, "deploy:\n  git_allow_private_hosts: true\n")
	chdir(t, dir)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !cfg.Deploy.GitAllowPrivateHosts {
		t.Error("Deploy.GitAllowPrivateHosts = false, want true from gotham.yaml")
	}

	// YAML 1 agrees with env "1": both readers map it to true.
	writeConfig(t, dir, "deploy:\n  git_allow_private_hosts: 1\n")
	cfg, err = Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !cfg.Deploy.GitAllowPrivateHosts {
		t.Error("Deploy.GitAllowPrivateHosts = false, want true from gotham.yaml value 1")
	}
}

func TestLoadTrustedProxiesFromEnv(t *testing.T) {
	clearGothamEnv(t)
	chdir(t, t.TempDir())

	t.Setenv(EnvTrustedProxies, "10.0.0.0/8,127.0.0.1,::1")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	want := []string{"10.0.0.0/8", "127.0.0.1", "::1"}
	if len(cfg.Server.TrustedProxies) != len(want) {
		t.Fatalf("Server.TrustedProxies = %v, want %v", cfg.Server.TrustedProxies, want)
	}
	for i := range want {
		if cfg.Server.TrustedProxies[i] != want[i] {
			t.Fatalf("Server.TrustedProxies = %v, want %v", cfg.Server.TrustedProxies, want)
		}
	}
}

func TestLoadRejectsInvalidValues(t *testing.T) {
	tests := map[string]string{
		"bad format":          "log:\n  format: xml\n",
		"bad port":            "server:\n  port: 70000\n",
		"bad trusted proxies": "server:\n  trusted_proxies:\n    - not-an-ip\n",
	}

	for name, contents := range tests {
		t.Run(name, func(t *testing.T) {
			clearGothamEnv(t)

			dir := t.TempDir()
			writeConfig(t, dir, contents)
			chdir(t, dir)

			if _, err := Load(); err == nil {
				t.Fatal("Load() = nil error, want validation error")
			}
		})
	}
}

func TestSnapshotReflectsValues(t *testing.T) {
	clearGothamEnv(t)
	chdir(t, t.TempDir())

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	snapshot := cfg.Snapshot()
	if snapshot.Server.Port != defaultServerPort {
		t.Errorf("Snapshot().Server.Port = %d, want %d", snapshot.Server.Port, defaultServerPort)
	}
}

func TestApplyChangeReloadsAndDeduplicates(t *testing.T) {
	clearGothamEnv(t)

	dir := t.TempDir()
	writeConfig(t, dir, "log:\n  level: info\n")
	chdir(t, dir)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	calls := 0
	onChange := func() { calls++ }

	writeConfig(t, dir, "log:\n  level: debug\n")
	cfg.applyChange(onChange)

	if cfg.Log.Level != "debug" {
		t.Errorf("Log.Level = %q, want debug", cfg.Log.Level)
	}
	if calls != 1 {
		t.Errorf("onChange calls = %d, want 1", calls)
	}

	// A duplicate event with unchanged content must not reload again.
	cfg.applyChange(onChange)
	if calls != 1 {
		t.Errorf("onChange calls after duplicate = %d, want 1", calls)
	}
}

func TestApplyChangeRejectsInvalidValues(t *testing.T) {
	clearGothamEnv(t)

	dir := t.TempDir()
	writeConfig(t, dir, "log:\n  level: info\n")
	chdir(t, dir)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	writeConfig(t, dir, "server:\n  port: 70000\n")
	cfg.applyChange(nil)

	if cfg.Log.Level != defaultLogLevel {
		t.Errorf("Log.Level = %q, want previous %q after rejected reload", cfg.Log.Level, defaultLogLevel)
	}
}

// rewriteConfigAtomic replaces gotham.yaml the way editors do: write a temp
// file and rename it over the target. This is the case a file-level watch
// misses, so it exercises the directory watcher.
func rewriteConfigAtomic(t *testing.T, dir, contents string) {
	t.Helper()

	tmp := filepath.Join(dir, "gotham.yaml.tmp")
	if err := os.WriteFile(tmp, []byte(contents), 0o600); err != nil {
		t.Fatalf("write temp config: %v", err)
	}
	if err := os.Rename(tmp, filepath.Join(dir, "gotham.yaml")); err != nil {
		t.Fatalf("rename temp config: %v", err)
	}
}

// TestWatchReloadsOnFileChange pins that hot reload still fires after the viper
// file watcher was dropped: the directory watcher alone must pick up an edit.
func TestWatchReloadsOnFileChange(t *testing.T) {
	clearGothamEnv(t)

	dir := t.TempDir()
	writeConfig(t, dir, "log:\n  level: info\n")
	chdir(t, dir)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	changed := make(chan struct{}, 1)
	cfg.Watch(func() {
		select {
		case changed <- struct{}{}:
		default:
		}
	})

	rewriteConfigAtomic(t, dir, "log:\n  level: debug\n")

	select {
	case <-changed:
	case <-time.After(5 * time.Second):
		t.Fatal("hot reload did not fire on config change")
	}
	if got := cfg.Snapshot().Log.Level; got != "debug" {
		t.Fatalf("Log.Level = %q, want debug after reload", got)
	}
}

// TestWatchReloadNoRace hammers file changes while readers snapshot the config.
// It is meaningful under `go test -race`: a second watcher mutating viper state
// concurrently with a reload would be reported.
func TestWatchReloadNoRace(t *testing.T) {
	clearGothamEnv(t)

	dir := t.TempDir()
	writeConfig(t, dir, "log:\n  level: info\n")
	chdir(t, dir)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	cfg.Watch(nil)

	stop := make(chan struct{})
	var readers sync.WaitGroup
	for i := 0; i < 4; i++ {
		readers.Add(1)
		go func() {
			defer readers.Done()
			for {
				select {
				case <-stop:
					return
				default:
					_ = cfg.Snapshot()
				}
			}
		}()
	}

	levels := []string{"info", "debug", "warn", "error"}
	for i := 0; i < 100; i++ {
		writeConfig(t, dir, "log:\n  level: "+levels[i%len(levels)]+"\n")
		time.Sleep(2 * time.Millisecond)
	}
	close(stop)
	readers.Wait()
}

// TestApplyChangeReportsOnlyLevelApplied pins the honest reload report: only
// log.level is applied live, and a changed log.format says so instead of being
// logged as if it took effect.
func TestApplyChangeReportsOnlyLevelApplied(t *testing.T) {
	clearGothamEnv(t)

	dir := t.TempDir()
	writeConfig(t, dir, "log:\n  level: info\n  format: json\n")
	chdir(t, dir)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	var buf bytes.Buffer
	cfg.SetLogger(slog.New(slog.NewJSONHandler(&buf, nil)))

	writeConfig(t, dir, "log:\n  level: debug\n  format: text\n")
	cfg.applyChange(nil)

	if got := cfg.Snapshot().Log.Level; got != "debug" {
		t.Fatalf("Log.Level = %q, want debug", got)
	}

	sawRestartWarning := false
	for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		var entry map[string]any
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			t.Fatalf("decode log line %q: %v", line, err)
		}
		switch entry["msg"] {
		case "config changed":
			if _, ok := entry["log_format"]; ok {
				t.Errorf("config changed line reports log_format as applied: %v", entry)
			}
			if entry["log_level"] != "debug" {
				t.Errorf("config changed log_level = %v, want debug", entry["log_level"])
			}
		case "log.format changed; restart required to apply":
			sawRestartWarning = true
		}
	}
	if !sawRestartWarning {
		t.Errorf("missing restart warning for changed log.format; log = %q", buf.String())
	}

	// A level-only reload while the format still differs must not warn again.
	buf.Reset()
	writeConfig(t, dir, "log:\n  level: error\n  format: text\n")
	cfg.applyChange(nil)
	if strings.Contains(buf.String(), "restart required to apply") {
		t.Errorf("level-only reload warned about log.format: %q", buf.String())
	}

	// Returning to the startup format is already live: no warning either.
	buf.Reset()
	writeConfig(t, dir, "log:\n  level: warn\n  format: json\n")
	cfg.applyChange(nil)
	if strings.Contains(buf.String(), "restart required to apply") {
		t.Errorf("format returned to the startup value but warned again: %q", buf.String())
	}
}
