package updates

import (
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/justindeelux/gotham/updatecore"
)

// TestFeatureGetters covers the environment-driven configuration helpers.
func TestFeatureGetters(t *testing.T) {
	t.Setenv(FeatureEnv, "false")
	if Enabled() {
		t.Error("Enabled() = true with FEATURE_UPDATES=false")
	}
	t.Setenv(FeatureEnv, "")
	if !Enabled() {
		t.Error("Enabled() = false by default")
	}

	t.Setenv(AutoUpdateEnv, "true")
	if !AutoUpdateEnabled() {
		t.Error("AutoUpdateEnabled() = false with AUTO_UPDATE=true")
	}
	t.Setenv(AutoUpdateEnv, "")
	if AutoUpdateEnabled() {
		t.Error("AutoUpdateEnabled() = true by default")
	}

	t.Setenv(AutoIntervalEnv, "90m")
	if got := AutoIntervalFromEnv(); got != 90*time.Minute {
		t.Errorf("AutoIntervalFromEnv() = %v, want 90m", got)
	}
	t.Setenv(AutoIntervalEnv, "nonsense")
	if got := AutoIntervalFromEnv(); got != 6*time.Hour {
		t.Errorf("AutoIntervalFromEnv(invalid) = %v, want 6h", got)
	}

	t.Setenv(ChannelEnv, "beta")
	if got := ChannelFromEnv(); got != ChannelBeta {
		t.Errorf("ChannelFromEnv() = %q, want beta", got)
	}
	t.Setenv(ChannelEnv, "")
	if got := ChannelFromEnv(); got != ChannelStable {
		t.Errorf("ChannelFromEnv() = %q, want stable", got)
	}

	t.Setenv(RepoEnv, "acme/gotham")
	if got := RepoFromEnv(); got != "acme/gotham" {
		t.Errorf("RepoFromEnv() = %q", got)
	}
	t.Setenv(BaseURLEnv, "https://ghe.example.com")
	if got := BaseURLFromEnv(); got != "https://ghe.example.com" {
		t.Errorf("BaseURLFromEnv() = %q", got)
	}
	t.Setenv(ScriptEnv, "/opt/gotham-update")
	if got := ScriptFromEnv(); got != "/opt/gotham-update" {
		t.Errorf("ScriptFromEnv() = %q", got)
	}
	t.Setenv(StatusPathEnv, "/var/lib/gotham-updater/status")
	if got := StatusPathFromEnv(); got != "/var/lib/gotham-updater/status" {
		t.Errorf("StatusPathFromEnv() = %q", got)
	}
	t.Setenv(PendingPathEnv, "/var/lib/gotham/pending")
	if got := PendingPathFromEnv(); got != "/var/lib/gotham/pending" {
		t.Errorf("PendingPathFromEnv() = %q", got)
	}
	t.Setenv(BinaryPathEnv, "/opt/gotham/bin/gotham")
	if got := BinaryPathFromEnv(); got != "/opt/gotham/bin/gotham" {
		t.Errorf("BinaryPathFromEnv() = %q", got)
	}
	t.Setenv(LockPathEnv, "/var/lib/gotham/lock")
	if got := LockPathFromEnv(); got != "/var/lib/gotham/lock" {
		t.Errorf("LockPathFromEnv() = %q", got)
	}
	t.Setenv(BackoffPathEnv, "/var/lib/gotham/backoff")
	if got := BackoffPathFromEnv(); got != "/var/lib/gotham/backoff" {
		t.Errorf("BackoffPathFromEnv() = %q", got)
	}
	t.Setenv(DownloadTimeoutEnv, "3m")
	if got := DownloadTimeoutFromEnv(); got != 3*time.Minute {
		t.Errorf("DownloadTimeoutFromEnv() = %v", got)
	}
	t.Setenv(CurrentEnv, "v9.9.9")
	if got := CurrentFromEnv(); got != "v9.9.9" {
		t.Errorf("CurrentFromEnv() = %q", got)
	}

	t.Setenv(RepoEnv, "")
	t.Setenv(BaseURLEnv, "")
	t.Setenv(ScriptEnv, "")
	t.Setenv(StatusPathEnv, "")
	t.Setenv(PendingPathEnv, "")
	t.Setenv(BinaryPathEnv, "")
	t.Setenv(LockPathEnv, "")
	t.Setenv(BackoffPathEnv, "")
	t.Setenv(DownloadTimeoutEnv, "")
	t.Setenv(CurrentEnv, "")
	if RepoFromEnv() != DefaultRepo || BaseURLFromEnv() != DefaultBaseURL ||
		ScriptFromEnv() != DefaultUpdateScript || StatusPathFromEnv() != DefaultStatusPath ||
		PendingPathFromEnv() != DefaultPendingPath || BinaryPathFromEnv() != DefaultBinaryPath ||
		LockPathFromEnv() != DefaultLockPath || BackoffPathFromEnv() != DefaultBackoffPath ||
		DownloadTimeoutFromEnv() != 10*time.Minute || CurrentFromEnv() != "" {
		t.Error("defaults not applied")
	}
}

// TestValidateURL covers the allowed and refused destinations.
func TestValidateURL(t *testing.T) {
	allowed := []string{
		"https://api.github.com/repos/o/n/releases",
		"https://10.0.0.5/mirror",
		"http://127.0.0.1:8080/asset",
		"http://localhost:8080/asset",
	}
	for _, raw := range allowed {
		if err := updatecore.ValidateURL(raw); err != nil {
			t.Errorf("updatecore.ValidateURL(%q) = %v, want nil", raw, err)
		}
	}
	refused := []string{
		"",
		"file:///etc/passwd",
		"http://example.com/asset",
		"http://10.0.0.5/mirror",
		"http://169.254.169.254/latest/meta-data",
		"http://[fe80::1]/asset",
		"ftp://example.com/asset",
	}
	for _, raw := range refused {
		if err := updatecore.ValidateURL(raw); !errors.Is(err, ErrBadURL) {
			t.Errorf("updatecore.ValidateURL(%q) = %v, want ErrBadURL", raw, err)
		}
	}
}

// TestFromEnv covers the environment assembly and the missing-key fail-closed
// case.
func TestFromEnv(t *testing.T) {
	embedded, _, err := updatecore.GenerateKey()
	if err != nil {
		t.Fatalf("updatecore.GenerateKey: %v", err)
	}
	next, _, err := updatecore.GenerateKey()
	if err != nil {
		t.Fatalf("updatecore.GenerateKey (next): %v", err)
	}
	originalCurrent, originalNext := updatecore.PublicKey, updatecore.NextPublicKey
	t.Cleanup(func() { updatecore.PublicKey, updatecore.NextPublicKey = originalCurrent, originalNext })
	updatecore.PublicKey = updatecore.EncodePublicKeyBase64(embedded)
	updatecore.NextPublicKey = updatecore.EncodePublicKeyBase64(next)
	t.Setenv(PublicKeyEnv, "")
	t.Setenv(RepoEnv, "acme/gotham")
	t.Setenv(BaseURLEnv, "https://ghe.example.com")
	t.Setenv(ChannelEnv, "beta")
	t.Setenv(ScriptEnv, "/opt/gotham-update")
	t.Setenv(StatusPathEnv, "/var/lib/gotham-updater/status")
	t.Setenv(PendingPathEnv, "/var/lib/gotham/pending")
	t.Setenv(BinaryPathEnv, "/opt/gotham/bin/gotham")
	t.Setenv(LockPathEnv, "/var/lib/gotham/lock")
	t.Setenv(AutoUpdateEnv, "true")
	t.Setenv(AutoIntervalEnv, "90m")

	cfg, err := FromEnv("v1.0.0", slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("FromEnv: %v", err)
	}
	if cfg.Current != "v1.0.0" || cfg.Repo != "acme/gotham" || cfg.BaseURL != "https://ghe.example.com" ||
		cfg.Channel != ChannelBeta || cfg.UpdateScript != "/opt/gotham-update" ||
		cfg.StatusPath != "/var/lib/gotham-updater/status" || cfg.PendingPath != "/var/lib/gotham/pending" ||
		cfg.BinaryPath != "/opt/gotham/bin/gotham" || cfg.LockPath != "/var/lib/gotham/lock" ||
		!cfg.Auto || cfg.AutoInterval != 90*time.Minute ||
		len(cfg.PublicKeys) != 2 || !cfg.PublicKeys[0].Equal(embedded) || !cfg.PublicKeys[1].Equal(next) {
		t.Fatalf("FromEnv = %+v", cfg)
	}

	updatecore.PublicKey = ""
	updatecore.NextPublicKey = ""
	t.Setenv(PublicKeyEnv, "")
	cfg, err = FromEnv("v1.0.0", nil)
	if !errors.Is(err, ErrNoPublicKey) {
		t.Fatalf("FromEnv(no key) err = %v, want ErrNoPublicKey", err)
	}
	if len(cfg.PublicKeys) != 0 {
		t.Error("FromEnv(no key) returned a public key")
	}
}
