package updates

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"
)

// newTestService wires a Service at a fixture release server with a no-op
// restart hook and a temporary binary/status path.
func newTestService(t *testing.T, server *httptest.Server, current string, cfg func(*Config)) Service {
	t.Helper()
	dir := t.TempDir()
	config := Config{
		Current:    current,
		Repo:       "owner/name",
		BaseURL:    server.URL,
		Channel:    ChannelStable,
		Client:     server.Client(),
		Logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
		Restart:    func(context.Context) error { return nil },
		GOARCH:     "amd64",
		BinaryPath: filepath.Join(dir, "gotham"),
		StatusPath: filepath.Join(dir, "run", "update.status"),
	}
	if cfg != nil {
		cfg(&config)
	}
	svc, err := NewService(config)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	return svc
}

// TestServiceUpToDate proves Apply reports a no-op when nothing is newer.
func TestServiceUpToDate(t *testing.T) {
	server := httptest.NewServer(releasesHandler([]fixtureRelease{
		{Tag: "v1.0.0", Assets: platformAssets()},
	}, 0, 0))
	defer server.Close()

	svc := newTestService(t, server, "v1.0.0", nil)
	result, err := svc.Apply(context.Background(), "")
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if result.Applied || result.Version != "v1.0.0" {
		t.Fatalf("Apply = %+v, want a no-op", result)
	}
}

// TestServiceApplyNeedsPublicKey proves Apply fails closed without a key even
// when a newer release exists.
func TestServiceApplyNeedsPublicKey(t *testing.T) {
	server := httptest.NewServer(releasesHandler([]fixtureRelease{
		{Tag: "v1.2.0", Assets: platformAssets()},
	}, 0, 0))
	defer server.Close()

	svc := newTestService(t, server, "v1.0.0", nil)
	release, err := svc.Check(context.Background())
	if err != nil || release == nil {
		t.Fatalf("Check = (%v, %v), want a release", release, err)
	}
	if _, err := svc.Apply(context.Background(), ""); !errors.Is(err, ErrNoPublicKey) {
		t.Fatalf("Apply = %v, want ErrNoPublicKey", err)
	}
}

// TestServiceApplyChannel proves the per-request channel override selects a
// prerelease on beta.
func TestServiceApplyChannel(t *testing.T) {
	server := httptest.NewServer(releasesHandler([]fixtureRelease{
		{Tag: "v1.2.0", Assets: platformAssets()},
		{Tag: "v1.3.0-rc.1", Prerelease: true, Assets: platformAssets()},
	}, 0, 0))
	defer server.Close()

	svc := newTestService(t, server, "v1.0.0", nil)
	release, err := svc.Check(context.Background())
	if err != nil || release == nil || release.Version != "v1.2.0" {
		t.Fatalf("stable Check = (%+v, %v), want v1.2.0", release, err)
	}

	beta := newTestService(t, server, "v1.0.0", func(c *Config) { c.Channel = ChannelBeta })
	release, err = beta.Check(context.Background())
	if err != nil || release == nil || release.Version != "v1.3.0-rc.1" {
		t.Fatalf("beta Check = (%+v, %v), want v1.3.0-rc.1", release, err)
	}
}

// TestServiceStartAutoDisabled proves the loop is a no-op when disabled.
func TestServiceStartAutoDisabled(t *testing.T) {
	server := httptest.NewServer(releasesHandler(nil, 0, 0))
	defer server.Close()

	t.Setenv(AutoUpdateEnv, "false")
	svc := newTestService(t, server, "v1.0.0", nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	svc.StartAuto(ctx) // must not block or panic
}

// TestServiceCurrentAndRollback covers the read-only accessors and the
// no-backup rollback path.
func TestServiceCurrentAndRollback(t *testing.T) {
	server := httptest.NewServer(releasesHandler(nil, 0, 0))
	defer server.Close()

	svc := newTestService(t, server, "v2.3.4", nil)
	if got := svc.Current(); got != "v2.3.4" {
		t.Errorf("Current() = %q, want v2.3.4", got)
	}
	if err := svc.Rollback(); !errors.Is(err, ErrNoBackup) {
		t.Errorf("Rollback() = %v, want ErrNoBackup", err)
	}
}

// TestDefaultRestartMissingScript proves a missing wrapper is reported rather
// than silently skipping the restart.
func TestDefaultRestartMissingScript(t *testing.T) {
	restart := defaultRestart("/nonexistent/gotham-update")
	if err := restart(context.Background()); err == nil {
		t.Fatal("defaultRestart(missing script) = nil error, want failure")
	}
}

// TestServiceStartAutoRuns exercises the auto-update loop once.
func TestServiceStartAutoRuns(t *testing.T) {
	t.Setenv(AutoUpdateEnv, "true")
	server := httptest.NewServer(releasesHandler([]fixtureRelease{
		{Tag: "v1.2.0", Assets: platformAssets()},
	}, 0, 0))
	defer server.Close()

	svc := newTestService(t, server, "v1.0.0", func(c *Config) {
		c.Auto = true
		c.AutoInterval = 20 * time.Millisecond
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	svc.StartAuto(ctx)
	time.Sleep(80 * time.Millisecond)
	cancel()
}
