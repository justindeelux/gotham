package config

import (
	"os"
	"path/filepath"
	"testing"
)

// gothamEnvKeys lists every environment variable Load understands.
var gothamEnvKeys = []string{
	EnvServerAddr,
	EnvServerPort,
	EnvDatabaseDSN,
	EnvRedisAddr,
	EnvLogLevel,
	EnvLogFormat,
	EnvAuthJWTPrivateKeyPath,
	EnvAuthJWTPublicKeyPath,
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

func TestLoadRejectsInvalidValues(t *testing.T) {
	tests := map[string]string{
		"bad format": "log:\n  format: xml\n",
		"bad port":   "server:\n  port: 70000\n",
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
