package agent

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"syscall"
	"testing"
	"time"

	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
	"github.com/justindeelux/gotham/updatecore"
	"google.golang.org/grpc"
)

const (
	// testAgentVersion is the release the fixture publishes; testAgentCurrent
	// is the version the simulated agent starts at.
	testAgentVersion = "v1.2.0"
	testAgentCurrent = "v1.0.0"
)

// agentReleaseServer serves a signed agent release (artifact + manifest + sig).
type agentReleaseServer struct {
	server       *httptest.Server
	public       ed25519.PublicKey
	artifact     []byte
	manifest     updatecore.Manifest
	assetName    string
	manifestName string
}

func newAgentReleaseServer(t *testing.T, tamperSignature bool) *agentReleaseServer {
	t.Helper()
	arch := runtime.GOARCH
	assetName := "gotham-agent-linux-" + arch
	manifestName := updatecore.ManifestNameWithPrefix("gotham-agent-manifest-", arch)

	public, private, err := updatecore.GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	signer, err := updatecore.NewSigner(private)
	if err != nil {
		t.Fatalf("NewSigner: %v", err)
	}
	artifact := []byte("agent binary " + testAgentVersion)
	manifest := updatecore.BuildManifest(testAgentVersion, "stable", arch, assetName, artifact)
	manifestBytes := manifest.Marshal()
	signature := signer.SignBase64(manifestBytes)
	if tamperSignature {
		signature = signer.SignBase64([]byte("different payload"))
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/"+assetName, func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(artifact) })
	mux.HandleFunc("/"+manifestName, func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(manifestBytes) })
	mux.HandleFunc("/"+manifestName+updatecore.ManifestSigSuffix, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(signature))
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return &agentReleaseServer{
		server: server, public: public, artifact: artifact, manifest: manifest,
		assetName: assetName, manifestName: manifestName,
	}
}

// offer builds the CP response the agent receives.
func (r *agentReleaseServer) offer() *agentv1.UpdateResponse {
	return &agentv1.UpdateResponse{
		UpdateAvailable:      true,
		LatestVersion:        testAgentVersion,
		AssetUrl:             r.server.URL + "/" + r.assetName,
		ManifestUrl:          r.server.URL + "/" + r.manifestName,
		ManifestSignatureUrl: r.server.URL + "/" + r.manifestName + updatecore.ManifestSigSuffix,
		Sha256:               r.manifest.SHA256,
		Channel:              "stable",
		Rollout:              true,
	}
}

// setTestPublicKey installs the release key the agent's embedded key would
// carry in a release build (the dev override applies only when none is
// embedded).
func setTestPublicKey(t *testing.T, public ed25519.PublicKey) {
	t.Helper()
	t.Setenv(updatecore.PublicKeyEnv, base64.StdEncoding.EncodeToString(public))
}

// noopAgentRestart reports the wrapper exited cleanly without restarting.
func noopAgentRestart(context.Context) (func() error, error) {
	return func() error { return nil }, nil
}

// healthyRestart models the privileged wrapper succeeding: it records an `ok`
// status for version in the authoritative status file and reports the wrapper
// exited cleanly. This is what a real wrapper writes after it restarts the unit
// and the health check passes.
func healthyRestart(statusPath, version string) updatecore.RestartFunc {
	return func(context.Context) (func() error, error) {
		if err := updatecore.NewStatusStore(statusPath).Write(updatecore.Status{
			Result: updatecore.StatusOK, Version: version,
		}); err != nil {
			return nil, err
		}
		return func() error { return nil }, nil
	}
}

// rolledBackRestart models a wrapper that restarted, failed its health check,
// restored the old binary and recorded rolled_back.
func rolledBackRestart(statusPath, version string) updatecore.RestartFunc {
	return func(context.Context) (func() error, error) {
		if err := updatecore.NewStatusStore(statusPath).Write(updatecore.Status{
			Result: updatecore.StatusRolledBack, Version: version, Detail: "health check failed",
		}); err != nil {
			return nil, err
		}
		return func() error { return nil }, nil
	}
}

// fakeUpdateClient is a minimal UpdateServiceClient.
type fakeUpdateClient struct {
	resp *agentv1.UpdateResponse
	err  error
}

func (f *fakeUpdateClient) RequestUpdate(context.Context, *agentv1.UpdateRequest, ...grpc.CallOption) (*agentv1.UpdateResponse, error) {
	return f.resp, f.err
}

// updaterTestConfig builds a Config whose updater writes only under dir.
func updaterTestConfig(t *testing.T, target string, restart updatecore.RestartFunc) Config {
	t.Helper()
	dir := filepath.Dir(target)
	return Config{
		BinaryPath:        target,
		UpdateLockPath:    filepath.Join(dir, "update.lock"),
		UpdatePendingPath: filepath.Join(dir, "update.pending"),
		UpdateStatusPath:  filepath.Join(dir, "update.status"),
		UpdateRetryPath:   filepath.Join(dir, "update.retry"),
		UpdateBackoffPath: filepath.Join(dir, "update.backoff"),
		UpdateScript:      filepath.Join(dir, "gotham-agent-update"),
		AutoUpdate:        true,
		UpdateInterval:    time.Minute,
		Version:           testAgentCurrent,
		Restart:           restart,
	}
}

// TestAgentUpdaterAppliesVerifiedOffer is the agent end to end: a CP offer is
// downloaded, verified, swapped (keeping .old) and, once the wrapper records
// the new binary healthy, the new version is reported.
func TestAgentUpdaterAppliesVerifiedOffer(t *testing.T) {
	release := newAgentReleaseServer(t, false)
	setTestPublicKey(t, release.public)

	target := filepath.Join(t.TempDir(), "gotham-agent")
	if err := os.WriteFile(target, []byte("old binary"), 0o755); err != nil {
		t.Fatalf("write target: %v", err)
	}
	restart := healthyRestart(filepath.Join(filepath.Dir(target), "update.status"), testAgentVersion)
	runner := NewAgent(updaterTestConfig(t, target, restart), discardLogger(), nil)
	if runner.updater == nil {
		t.Fatal("updater is nil with a configured key and binary path")
	}

	runner.updater.checkOnce(context.Background(), &fakeUpdateClient{resp: release.offer()})

	if got := readFileString(t, target); got != string(release.artifact) {
		t.Fatalf("target = %q, want the new binary", got)
	}
	if got := readFileString(t, target+updatecore.OldSuffix); got != "old binary" {
		t.Fatalf("retained old binary = %q", got)
	}
	if runner.Version() != testAgentVersion {
		t.Fatalf("Version() = %q, want %s", runner.Version(), testAgentVersion)
	}
	// A healthy staged update must not leave a spurious failed-attempt count
	// (it would otherwise be snapshotted as an unexpected change).
	if _, err := os.Stat(filepath.Join(filepath.Dir(target), "update.backoff")); !os.IsNotExist(err) {
		t.Fatalf("update.backoff exists after a healthy update: %v", err)
	}
}

// TestAgentUpdaterKeepsVersionWhenWrapperRollsBack is M1: a wrapper that rolls
// back must not make the node report the new version.
func TestAgentUpdaterKeepsVersionWhenWrapperRollsBack(t *testing.T) {
	release := newAgentReleaseServer(t, false)
	setTestPublicKey(t, release.public)

	target := filepath.Join(t.TempDir(), "gotham-agent")
	if err := os.WriteFile(target, []byte("old binary"), 0o755); err != nil {
		t.Fatalf("write target: %v", err)
	}
	restart := rolledBackRestart(filepath.Join(filepath.Dir(target), "update.status"), testAgentVersion)
	runner := NewAgent(updaterTestConfig(t, target, restart), discardLogger(), nil)

	runner.updater.checkOnce(context.Background(), &fakeUpdateClient{resp: release.offer()})

	if runner.Version() != testAgentCurrent {
		t.Fatalf("Version() = %q, want the running %s after a rollback", runner.Version(), testAgentCurrent)
	}
}

// TestAgentUpdaterRefusesDowngrade is M3: a validly signed older release must
// not roll the agent back.
func TestAgentUpdaterRefusesDowngrade(t *testing.T) {
	release := newAgentReleaseServer(t, false)
	setTestPublicKey(t, release.public)

	target := filepath.Join(t.TempDir(), "gotham-agent")
	if err := os.WriteFile(target, []byte("v9 binary"), 0o755); err != nil {
		t.Fatalf("write target: %v", err)
	}
	cfg := updaterTestConfig(t, target, noopAgentRestart)
	cfg.Version = "v9.0.0"
	runner := NewAgent(cfg, discardLogger(), nil)

	runner.updater.checkOnce(context.Background(), &fakeUpdateClient{resp: release.offer()})

	if got := readFileString(t, target); got != "v9 binary" {
		t.Fatalf("target = %q, want it unchanged after a downgrade offer", got)
	}
	if runner.Version() != "v9.0.0" {
		t.Fatalf("Version() = %q, want v9.0.0", runner.Version())
	}
}

// TestAgentUpdaterRefusesSameVersion is M3: an equal-version offer is refused.
func TestAgentUpdaterRefusesSameVersion(t *testing.T) {
	release := newAgentReleaseServer(t, false)
	setTestPublicKey(t, release.public)

	target := filepath.Join(t.TempDir(), "gotham-agent")
	if err := os.WriteFile(target, []byte("current binary"), 0o755); err != nil {
		t.Fatalf("write target: %v", err)
	}
	cfg := updaterTestConfig(t, target, noopAgentRestart)
	cfg.Version = testAgentVersion
	runner := NewAgent(cfg, discardLogger(), nil)

	runner.updater.checkOnce(context.Background(), &fakeUpdateClient{resp: release.offer()})

	if got := readFileString(t, target); got != "current binary" {
		t.Fatalf("target = %q, want it unchanged for an equal-version offer", got)
	}
}

// TestAgentUpdaterRollsBackOnTamperedSignature proves a tampered manifest never
// touches the running binary and the old version keeps being reported.
func TestAgentUpdaterRollsBackOnTamperedSignature(t *testing.T) {
	release := newAgentReleaseServer(t, true)
	setTestPublicKey(t, release.public)

	target := filepath.Join(t.TempDir(), "gotham-agent")
	if err := os.WriteFile(target, []byte("old binary"), 0o755); err != nil {
		t.Fatalf("write target: %v", err)
	}
	runner := NewAgent(updaterTestConfig(t, target, noopAgentRestart), discardLogger(), nil)

	runner.updater.checkOnce(context.Background(), &fakeUpdateClient{resp: release.offer()})

	if got := readFileString(t, target); got != "old binary" {
		t.Fatalf("target = %q, want it unchanged after a tampered signature", got)
	}
	if _, err := os.Stat(target + updatecore.OldSuffix); !os.IsNotExist(err) {
		t.Fatalf("a refused update created a backup: %v", err)
	}
	if runner.Version() != "v1.0.0" {
		t.Fatalf("Version() = %q, want the old v1.0.0", runner.Version())
	}
}

// TestAgentUpdaterRejectsDigestMismatch proves an artifact that does not match
// the signed manifest digest is refused.
func TestAgentUpdaterRejectsDigestMismatch(t *testing.T) {
	release := newAgentReleaseServer(t, false)
	setTestPublicKey(t, release.public)

	target := filepath.Join(t.TempDir(), "gotham-agent")
	if err := os.WriteFile(target, []byte("old binary"), 0o755); err != nil {
		t.Fatalf("write target: %v", err)
	}
	runner := NewAgent(updaterTestConfig(t, target, noopAgentRestart), discardLogger(), nil)

	offer := release.offer()
	offer.Sha256 = "0000000000000000000000000000000000000000000000000000000000000000"
	runner.updater.checkOnce(context.Background(), &fakeUpdateClient{resp: offer})

	if got := readFileString(t, target); got != "old binary" {
		t.Fatalf("target = %q, want it unchanged after a digest mismatch", got)
	}
}

// TestAgentUpdaterIgnoresPlainOfferWithoutAutoUpdate proves a non-rollout offer
// is not applied unless unattended auto-update is on.
func TestAgentUpdaterIgnoresPlainOfferWithoutAutoUpdate(t *testing.T) {
	release := newAgentReleaseServer(t, false)
	setTestPublicKey(t, release.public)

	target := filepath.Join(t.TempDir(), "gotham-agent")
	if err := os.WriteFile(target, []byte("old binary"), 0o755); err != nil {
		t.Fatalf("write target: %v", err)
	}
	cfg := updaterTestConfig(t, target, noopAgentRestart)
	cfg.AutoUpdate = false
	runner := NewAgent(cfg, discardLogger(), nil)

	offer := release.offer()
	offer.Rollout = false
	runner.updater.checkOnce(context.Background(), &fakeUpdateClient{resp: offer})

	if got := readFileString(t, target); got != "old binary" {
		t.Fatalf("target = %q, want it unchanged for a plain offer", got)
	}
	if runner.Version() != "v1.0.0" {
		t.Fatalf("Version() = %q, want v1.0.0", runner.Version())
	}
}

// TestAgentUpdaterDoesNotBlockOnNonRegularMarker proves a planted FIFO at the
// pending/status path cannot hang the updater (the BE-9.1 guard class).
func TestAgentUpdaterDoesNotBlockOnNonRegularMarker(t *testing.T) {
	release := newAgentReleaseServer(t, false)
	setTestPublicKey(t, release.public)

	dir := t.TempDir()
	target := filepath.Join(dir, "gotham-agent")
	if err := os.WriteFile(target, []byte("old binary"), 0o755); err != nil {
		t.Fatalf("write target: %v", err)
	}
	if err := syscall.Mkfifo(filepath.Join(dir, "update.pending"), 0o644); err != nil {
		t.Fatalf("mkfifo pending: %v", err)
	}
	if err := syscall.Mkfifo(filepath.Join(dir, "update.status"), 0o644); err != nil {
		t.Fatalf("mkfifo status: %v", err)
	}
	runner := NewAgent(updaterTestConfig(t, target, noopAgentRestart), discardLogger(), nil)

	done := make(chan struct{})
	go func() {
		defer close(done)
		runner.updater.checkOnce(context.Background(), &fakeUpdateClient{resp: release.offer()})
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("checkOnce blocked on a non-regular pending/status marker")
	}
}

// TestAgentUpdaterIgnoresStaleOKStatus is N2: a stale `ok` for the same version
// (for example from a previous install) is not trusted; only a recent healthy
// status is adopted.
func TestAgentUpdaterIgnoresStaleOKStatus(t *testing.T) {
	release := newAgentReleaseServer(t, false)
	setTestPublicKey(t, release.public)

	target := filepath.Join(t.TempDir(), "gotham-agent")
	if err := os.WriteFile(target, []byte("old binary"), 0o755); err != nil {
		t.Fatalf("write target: %v", err)
	}
	statusPath := filepath.Join(filepath.Dir(target), "update.status")
	if err := updatecore.NewStatusStore(statusPath).Write(updatecore.Status{
		Result: updatecore.StatusOK, Version: testAgentVersion, At: time.Now().Add(-time.Hour),
	}); err != nil {
		t.Fatalf("write stale status: %v", err)
	}
	runner := NewAgent(updaterTestConfig(t, target, noopAgentRestart), discardLogger(), nil)

	runner.updater.checkOnce(context.Background(), &fakeUpdateClient{resp: release.offer()})

	if runner.Version() != testAgentCurrent {
		t.Fatalf("Version() = %q, want the running %s (stale ok must not be trusted)", runner.Version(), testAgentCurrent)
	}
}

// TestAgentUpdaterBacksOffAfterFailedUpdate is N5: after a failed attempt the
// same version is not re-applied until the backoff lapses.
func TestAgentUpdaterBacksOffAfterFailedUpdate(t *testing.T) {
	release := newAgentReleaseServer(t, false)
	setTestPublicKey(t, release.public)

	target := filepath.Join(t.TempDir(), "gotham-agent")
	if err := os.WriteFile(target, []byte("old binary"), 0o755); err != nil {
		t.Fatalf("write target: %v", err)
	}
	launches := 0
	restart := func(context.Context) (func() error, error) {
		launches++
		if err := updatecore.NewStatusStore(filepath.Join(filepath.Dir(target), "update.status")).Write(updatecore.Status{
			Result: updatecore.StatusRolledBack, Version: testAgentVersion,
		}); err != nil {
			return nil, err
		}
		return func() error { return nil }, nil
	}
	runner := NewAgent(updaterTestConfig(t, target, restart), discardLogger(), nil)
	client := &fakeUpdateClient{resp: release.offer()}

	runner.updater.checkOnce(context.Background(), client)
	if launches != 1 {
		t.Fatalf("wrapper launches = %d, want 1", launches)
	}
	if !runner.updater.inBackoff(testAgentVersion) {
		t.Fatal("no backoff recorded after a rolled_back outcome")
	}

	// A second poll within the backoff must not re-download/re-apply.
	runner.updater.checkOnce(context.Background(), client)
	if launches != 1 {
		t.Fatalf("wrapper launches = %d, want 1 (backoff must suppress the retry)", launches)
	}
}

// TestAgentUpdaterSeedsBackoffFromRolledBackStatus is the restart-loop fix: a
// durable rolled_back status for a newer version makes a freshly started agent
// skip that release on its first poll (the backoff is seeded, not just the
// in-memory map).
func TestAgentUpdaterSeedsBackoffFromRolledBackStatus(t *testing.T) {
	release := newAgentReleaseServer(t, false)
	setTestPublicKey(t, release.public)

	target := filepath.Join(t.TempDir(), "gotham-agent")
	if err := os.WriteFile(target, []byte("old binary"), 0o755); err != nil {
		t.Fatalf("write target: %v", err)
	}
	dir := filepath.Dir(target)
	// The wrapper recorded a rollback of the offered version.
	if err := updatecore.NewStatusStore(filepath.Join(dir, "update.status")).Write(updatecore.Status{
		Result: updatecore.StatusRolledBack, Version: testAgentVersion, At: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("write rolled_back status: %v", err)
	}
	launches := 0
	restart := func(context.Context) (func() error, error) {
		launches++
		return func() error { return nil }, nil
	}
	runner := NewAgent(updaterTestConfig(t, target, restart), discardLogger(), nil)

	if !runner.updater.inBackoff(testAgentVersion) {
		t.Fatal("backoff was not seeded from the durable rolled_back status")
	}
	runner.updater.checkOnce(context.Background(), &fakeUpdateClient{resp: release.offer()})
	if launches != 0 {
		t.Fatalf("wrapper launches = %d, want 0 (the failed release must not be re-applied)", launches)
	}
	if got := readFileString(t, target); got != "old binary" {
		t.Fatalf("target = %q, want it unchanged", got)
	}
}

// TestAgentUpdaterAppliesAfterReset proves the operator retry path: after
// `gotham-agent update reset` the agent clears the seeded backoff and applies
// the same version again.
func TestAgentUpdaterAppliesAfterReset(t *testing.T) {
	release := newAgentReleaseServer(t, false)
	setTestPublicKey(t, release.public)

	target := filepath.Join(t.TempDir(), "gotham-agent")
	if err := os.WriteFile(target, []byte("old binary"), 0o755); err != nil {
		t.Fatalf("write target: %v", err)
	}
	dir := filepath.Dir(target)
	if err := updatecore.NewStatusStore(filepath.Join(dir, "update.status")).Write(updatecore.Status{
		Result: updatecore.StatusRolledBack, Version: testAgentVersion, At: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("write rolled_back status: %v", err)
	}
	launches := 0
	restart := func(context.Context) (func() error, error) {
		launches++
		return func() error { return nil }, nil
	}
	cfg := updaterTestConfig(t, target, restart)
	runner := NewAgent(cfg, discardLogger(), nil)
	client := &fakeUpdateClient{resp: release.offer()}

	// Before the reset the failed release is skipped.
	runner.updater.checkOnce(context.Background(), client)
	if launches != 0 {
		t.Fatalf("wrapper launches = %d before reset, want 0", launches)
	}

	if err := ResetUpdateState(cfg); err != nil {
		t.Fatalf("ResetUpdateState: %v", err)
	}
	runner.updater.checkOnce(context.Background(), client)
	if launches != 1 {
		t.Fatalf("wrapper launches = %d after reset, want 1", launches)
	}
}

// readFileString reads path, failing the test on error.
func readFileString(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}
