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
		UpdateScript:      filepath.Join(dir, "gotham-agent-update"),
		AutoUpdate:        true,
		UpdateInterval:    time.Minute,
		Version:           testAgentCurrent,
		Restart:           restart,
	}
}

// TestAgentUpdaterAppliesVerifiedOffer is the agent end to end: a CP offer is
// downloaded, verified, swapped (keeping .old) and the new version is reported.
func TestAgentUpdaterAppliesVerifiedOffer(t *testing.T) {
	release := newAgentReleaseServer(t, false)
	setTestPublicKey(t, release.public)

	target := filepath.Join(t.TempDir(), "gotham-agent")
	if err := os.WriteFile(target, []byte("old binary"), 0o755); err != nil {
		t.Fatalf("write target: %v", err)
	}
	runner := NewAgent(updaterTestConfig(t, target, noopAgentRestart), discardLogger(), nil)
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
	if runner.Version() != "v1.2.0" {
		t.Fatalf("Version() = %q, want v1.2.0", runner.Version())
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

// readFileString reads path, failing the test on error.
func readFileString(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}
