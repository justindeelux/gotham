package agent

import (
	"log/slog"
	"strings"
	"testing"
)

func clearAgentEnv(t *testing.T) {
	t.Helper()
	for _, key := range []string{
		envCPAddr, envNodeID, envListenAddr, envCA, envCertDir,
		envKey, envInsecure, envDockerSock, envComposeRoot, envLogLevel, envDockerHost,
		envManagedVolumeRoot, envSharedManagedVolumeRoot,
	} {
		t.Setenv(key, "")
	}
}

func TestLoadDefaults(t *testing.T) {
	clearAgentEnv(t)
	t.Setenv(envCA, "/etc/gotham/ca.pem")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.CPAddr != defaultCPAddr {
		t.Errorf("CPAddr = %q; want %q", cfg.CPAddr, defaultCPAddr)
	}
	if cfg.ListenAddr != defaultListenAddr {
		t.Errorf("ListenAddr = %q; want %q", cfg.ListenAddr, defaultListenAddr)
	}
	if cfg.CertDir != defaultCertDir {
		t.Errorf("CertDir = %q; want %q", cfg.CertDir, defaultCertDir)
	}
	if cfg.DockerSock != defaultDockerSock {
		t.Errorf("DockerSock = %q; want %q", cfg.DockerSock, defaultDockerSock)
	}
	if cfg.ManagedVolumeRoot != defaultManagedVolumeRoot {
		t.Errorf("ManagedVolumeRoot = %q; want %q", cfg.ManagedVolumeRoot, defaultManagedVolumeRoot)
	}
	if cfg.LogLevel != defaultLogLevel {
		t.Errorf("LogLevel = %q; want %q", cfg.LogLevel, defaultLogLevel)
	}
	if cfg.NodeID == "" {
		t.Error("NodeID is empty; want hostname fallback")
	}
	if cfg.CA != "/etc/gotham/ca.pem" {
		t.Errorf("CA = %q", cfg.CA)
	}
	if cfg.Insecure {
		t.Error("Insecure = true; want false by default")
	}
}

// TestLoadRejectsPlaintextWithoutOptIn is the FX-3 item-9 guard: no CA and no
// explicit insecure opt-in must fail closed rather than dial/serve plaintext.
func TestLoadRejectsPlaintextWithoutOptIn(t *testing.T) {
	// Loopback listen address so the failure is the missing opt-in, not the
	// loopback check.
	clearAgentEnv(t)
	t.Setenv(envListenAddr, "127.0.0.1:9443")

	if _, err := Load(); err == nil {
		t.Fatal("Load(no CA, no insecure) = nil error, want fail-closed")
	}

	// The opt-in alone is not enough: the listener must stay on loopback.
	clearAgentEnv(t)
	t.Setenv(envInsecure, "true")
	t.Setenv(envListenAddr, "0.0.0.0:9443")
	if _, err := Load(); err == nil {
		t.Fatal("Load(insecure, wildcard listen) = nil error, want loopback refusal")
	}

	clearAgentEnv(t)
	t.Setenv(envInsecure, "true")
	t.Setenv(envListenAddr, "127.0.0.1:9443")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load(insecure, loopback) = %v; want success", err)
	}
	if !cfg.Insecure {
		t.Error("Insecure = false; want true after opt-in")
	}
}

func TestLoadOverrides(t *testing.T) {
	clearAgentEnv(t)
	t.Setenv(envCPAddr, "cp.example.com:8443")
	t.Setenv(envNodeID, "node-7")
	t.Setenv(envListenAddr, "0.0.0.0:9443")
	t.Setenv(envCA, "/etc/gotham/ca.pem")
	t.Setenv(envCertDir, "/var/lib/gotham-agent")
	t.Setenv(envKey, "/etc/gotham/agent.key")
	t.Setenv(envDockerSock, "unix:///run/docker.sock")
	t.Setenv(envComposeRoot, "/srv/gotham/compose")
	t.Setenv(envLogLevel, "debug")
	t.Setenv(envManagedVolumeRoot, "/srv/gotham/volumes")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.CPAddr != "cp.example.com:8443" {
		t.Errorf("CPAddr = %q", cfg.CPAddr)
	}
	if cfg.NodeID != "node-7" {
		t.Errorf("NodeID = %q", cfg.NodeID)
	}
	if cfg.ListenAddr != "0.0.0.0:9443" {
		t.Errorf("ListenAddr = %q", cfg.ListenAddr)
	}
	if cfg.CA != "/etc/gotham/ca.pem" {
		t.Errorf("CA = %q", cfg.CA)
	}
	if cfg.CertDir != "/var/lib/gotham-agent" {
		t.Errorf("CertDir = %q", cfg.CertDir)
	}
	if cfg.KeyFile != "/etc/gotham/agent.key" {
		t.Errorf("KeyFile = %q", cfg.KeyFile)
	}
	if cfg.DockerSock != "unix:///run/docker.sock" {
		t.Errorf("DockerSock = %q", cfg.DockerSock)
	}
	if cfg.ComposeRoot != "/srv/gotham/compose" {
		t.Errorf("ComposeRoot = %q", cfg.ComposeRoot)
	}
	if cfg.LogLevel != "debug" {
		t.Errorf("LogLevel = %q", cfg.LogLevel)
	}
	if cfg.ManagedVolumeRoot != "/srv/gotham/volumes" {
		t.Errorf("ManagedVolumeRoot = %q", cfg.ManagedVolumeRoot)
	}
}

func TestLoadFallsBackToDockerHost(t *testing.T) {
	clearAgentEnv(t)
	t.Setenv(envInsecure, "true")
	t.Setenv(envListenAddr, "127.0.0.1:9443")
	t.Setenv(envDockerHost, "tcp://127.0.0.1:2375")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.DockerSock != "tcp://127.0.0.1:2375" {
		t.Errorf("DockerSock = %q; want DOCKER_HOST value", cfg.DockerSock)
	}
}

func TestLoadRejectsInvalidValues(t *testing.T) {
	tests := []struct {
		name string
		key  string
		val  string
	}{
		{"bad cp addr", envCPAddr, "not-a-host-port"},
		{"bad log level", envLogLevel, "loud"},
		{"wildcard node id", envNodeID, "bad*id"},
		{"node id with space", envNodeID, "bad id"},
		{"node id with slash", envNodeID, "bad/id"},
		{"overlong node id", envNodeID, strings.Repeat("a", maxNodeIDLength+1)},
		{"relative managed volume root", envManagedVolumeRoot, "data/volumes"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clearAgentEnv(t)
			t.Setenv(tt.key, tt.val)
			if _, err := Load(); err == nil {
				t.Fatalf("Load() = nil error; want error for %s=%q", tt.key, tt.val)
			}
		})
	}
}

// TestValidNodeID mirrors the control plane's node-id gate so the agent rejects
// identities the CP would always refuse.
func TestValidNodeID(t *testing.T) {
	for _, ok := range []string{"node-1", "10.0.0.9", "2001:db8::1", "cp.example.com"} {
		if !validNodeID(ok) {
			t.Errorf("validNodeID(%q) = false, want true", ok)
		}
	}
	for _, bad := range []string{"", "bad*id", "bad id", "bad/id", "bad\\id", "bad\x00id", strings.Repeat("a", maxNodeIDLength+1)} {
		if validNodeID(bad) {
			t.Errorf("validNodeID(%q) = true, want false", bad)
		}
	}
}

func TestParseLevel(t *testing.T) {
	tests := []struct {
		name string
		val  string
		want slog.Level
		ok   bool
	}{
		{"debug", "debug", slog.LevelDebug, true},
		{"info", "info", slog.LevelInfo, true},
		{"empty defaults to info", "", slog.LevelInfo, true},
		{"mixed case", "WARN", slog.LevelWarn, true},
		{"error", "error", slog.LevelError, true},
		{"unknown", "trace", 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseLevel(tt.val)
			if (err == nil) != tt.ok {
				t.Fatalf("parseLevel(%q) error = %v; want ok=%v", tt.val, err, tt.ok)
			}
			if tt.ok && got != tt.want {
				t.Errorf("parseLevel(%q) = %v; want %v", tt.val, got, tt.want)
			}
		})
	}
}
