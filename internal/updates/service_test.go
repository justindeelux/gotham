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

	"github.com/justindeelux/gotham/updatecore"
)

// newTestService wires a Service at a fixture release server with a no-op
// restart hook and a temporary binary/status path.
func newTestService(t *testing.T, server *httptest.Server, current string, cfg func(*Config)) Service {
	t.Helper()
	dir := t.TempDir()
	config := Config{
		Current:     current,
		Repo:        "owner/name",
		BaseURL:     server.URL,
		Channel:     ChannelStable,
		Client:      server.Client(),
		Logger:      slog.New(slog.NewTextHandler(io.Discard, nil)),
		Restart:     noopRestart,
		GOARCH:      "amd64",
		BinaryPath:  filepath.Join(dir, "gotham"),
		LockPath:    filepath.Join(dir, "update.lock"),
		StatusPath:  filepath.Join(dir, "status", "update.status"),
		PendingPath: filepath.Join(dir, "update.pending"),
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

// TestServiceApplierUsesKeyRing proves the embedded current + next key ring
// flows through FromEnv into the applier: a manifest signed by the
// pre-positioned next key verifies, and an unknown key is refused.
func TestServiceApplierUsesKeyRing(t *testing.T) {
	current, _, err := updatecore.GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey (current): %v", err)
	}
	next, nextPrivate, err := updatecore.GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey (next): %v", err)
	}
	originalCurrent, originalNext := updatecore.PublicKey, updatecore.NextPublicKey
	t.Cleanup(func() { updatecore.PublicKey, updatecore.NextPublicKey = originalCurrent, originalNext })
	updatecore.PublicKey = updatecore.EncodePublicKeyBase64(current)
	updatecore.NextPublicKey = updatecore.EncodePublicKeyBase64(next)
	t.Setenv(PublicKeyEnv, "")

	cfg, err := FromEnv("v1.0.0", nil)
	if err != nil {
		t.Fatalf("FromEnv: %v", err)
	}
	dir := t.TempDir()
	cfg.BinaryPath = filepath.Join(dir, "gotham")
	cfg.LockPath = filepath.Join(dir, "update.lock")
	cfg.StatusPath = filepath.Join(dir, "status", "update.status")
	cfg.PendingPath = filepath.Join(dir, "update.pending")
	svc, err := NewService(cfg)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	inner, ok := svc.(*service)
	if !ok || inner.applier == nil || inner.applier.Verifier == nil {
		t.Fatal("NewService did not wire the applier verifier from the key ring")
	}

	payload := []byte("signed manifest")
	ringSigner, err := NewSigner(nextPrivate)
	if err != nil {
		t.Fatalf("NewSigner (next): %v", err)
	}
	if err := inner.applier.Verifier.Verify(payload, []byte(ringSigner.SignBase64(payload))); err != nil {
		t.Fatalf("the applier refused a manifest signed by the next key: %v", err)
	}

	_, unknownPrivate, err := updatecore.GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey (unknown): %v", err)
	}
	unknownSigner, err := NewSigner(unknownPrivate)
	if err != nil {
		t.Fatalf("NewSigner (unknown): %v", err)
	}
	if err := inner.applier.Verifier.Verify(payload, unknownSigner.Sign(payload)); !errors.Is(err, ErrBadSignature) {
		t.Fatalf("the applier accepted an unknown key: %v", err)
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
	_, err := defaultRestart("/nonexistent/gotham-update")(context.Background())
	if err == nil {
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
