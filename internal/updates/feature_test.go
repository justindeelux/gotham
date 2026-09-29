package updates

import (
	"errors"
	"testing"
	"time"
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
	t.Setenv(CurrentEnv, "v9.9.9")
	if got := CurrentFromEnv(); got != "v9.9.9" {
		t.Errorf("CurrentFromEnv() = %q", got)
	}

	t.Setenv(RepoEnv, "")
	t.Setenv(BaseURLEnv, "")
	t.Setenv(ScriptEnv, "")
	t.Setenv(CurrentEnv, "")
	if RepoFromEnv() != DefaultRepo || BaseURLFromEnv() != DefaultBaseURL || ScriptFromEnv() != DefaultUpdateScript || CurrentFromEnv() != "" {
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
		if err := validateURL(raw); err != nil {
			t.Errorf("validateURL(%q) = %v, want nil", raw, err)
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
		if err := validateURL(raw); !errors.Is(err, ErrBadURL) {
			t.Errorf("validateURL(%q) = %v, want ErrBadURL", raw, err)
		}
	}
}
