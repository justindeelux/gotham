package updates

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"

	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
	"github.com/justindeelux/gotham/updatecore"
	"google.golang.org/grpc"
)

const (
	testAgentVersion = "v1.2.0"
	testAgentCurrent = "v1.0.0"
)

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

func setTestPublicKey(t *testing.T, public ed25519.PublicKey) {
	t.Helper()
	t.Setenv(updatecore.PublicKeyEnv, base64.StdEncoding.EncodeToString(public))
}

func noopAgentRestart(context.Context) (func() error, error) {
	return func() error { return nil }, nil
}

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

type fakeUpdateClient struct {
	resp *agentv1.UpdateResponse
	err  error
}

func (f *fakeUpdateClient) RequestUpdate(context.Context, *agentv1.UpdateRequest, ...grpc.CallOption) (*agentv1.UpdateResponse, error) {
	return f.resp, f.err
}

func updaterTestConfig(t *testing.T, target string, restart updatecore.RestartFunc) Config {
	t.Helper()
	dir := filepath.Dir(target)
	return Config{
		BinaryPath:   target,
		LockPath:     filepath.Join(dir, "update.lock"),
		PendingPath:  filepath.Join(dir, "update.pending"),
		StatusPath:   filepath.Join(dir, "update.status"),
		RetryPath:    filepath.Join(dir, "update.retry"),
		BackoffPath:  filepath.Join(dir, "update.backoff"),
		UpdateScript: filepath.Join(dir, "gotham-agent-update"),
		AutoUpdate:   true,
		Interval:     time.Minute,
		Channel:      DefaultChannel,
		Version:      testAgentCurrent,
		Restart:      restart,
	}
}

func TestAgentUpdaterAppliesVerifiedOffer(t *testing.T) {
	release := newAgentReleaseServer(t, false)
	setTestPublicKey(t, release.public)

	target := filepath.Join(t.TempDir(), "gotham-agent")
	if err := os.WriteFile(target, []byte("old binary"), 0o755); err != nil {
		t.Fatalf("write target: %v", err)
	}
	restart := healthyRestart(filepath.Join(filepath.Dir(target), "update.status"), testAgentVersion)
	runner := newTestUpdater(t, updaterTestConfig(t, target, restart))
	if runner.Updater == nil {
		t.Fatal("Updater is nil with a configured key and binary path")
	}

	runner.checkOnce(context.Background(), &fakeUpdateClient{resp: release.offer()})

	if got := readFileString(t, target); got != string(release.artifact) {
		t.Fatalf("target = %q, want the new binary", got)
	}
	if got := readFileString(t, target+updatecore.OldSuffix); got != "old binary" {
		t.Fatalf("retained old binary = %q", got)
	}
	if runner.current != testAgentVersion {
		t.Fatalf("Version() = %q, want %s", runner.current, testAgentVersion)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(target), "update.backoff")); !os.IsNotExist(err) {
		t.Fatalf("update.backoff exists after a healthy update: %v", err)
	}
}

func TestAgentUpdaterLoadsKeyRing(t *testing.T) {
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
	t.Setenv(updatecore.PublicKeyEnv, "")

	target := filepath.Join(t.TempDir(), "gotham-agent")
	if err := os.WriteFile(target, []byte("old binary"), 0o755); err != nil {
		t.Fatalf("write target: %v", err)
	}
	runner := newTestUpdater(t, updaterTestConfig(t, target, noopAgentRestart))
	if runner.Updater == nil || runner.applier == nil || runner.applier.Verifier == nil {
		t.Fatal("Updater did not wire the applier verifier from the embedded key ring")
	}

	payload := []byte("signed manifest")
	ringSigner, err := updatecore.NewSigner(nextPrivate)
	if err != nil {
		t.Fatalf("NewSigner (next): %v", err)
	}
	if err := runner.applier.Verifier.Verify(payload, []byte(ringSigner.SignBase64(payload))); err != nil {
		t.Fatalf("the agent refused a manifest signed by the next key: %v", err)
	}

	_, unknownPrivate, err := updatecore.GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey (unknown): %v", err)
	}
	unknownSigner, err := updatecore.NewSigner(unknownPrivate)
	if err != nil {
		t.Fatalf("NewSigner (unknown): %v", err)
	}
	if err := runner.applier.Verifier.Verify(payload, unknownSigner.Sign(payload)); !errors.Is(err, updatecore.ErrBadSignature) {
		t.Fatalf("the agent accepted an unknown key: %v", err)
	}
}

func TestAgentUpdaterKeepsVersionWhenWrapperRollsBack(t *testing.T) {
	release := newAgentReleaseServer(t, false)
	setTestPublicKey(t, release.public)

	target := filepath.Join(t.TempDir(), "gotham-agent")
	if err := os.WriteFile(target, []byte("old binary"), 0o755); err != nil {
		t.Fatalf("write target: %v", err)
	}
	restart := rolledBackRestart(filepath.Join(filepath.Dir(target), "update.status"), testAgentVersion)
	runner := newTestUpdater(t, updaterTestConfig(t, target, restart))

	runner.checkOnce(context.Background(), &fakeUpdateClient{resp: release.offer()})

	if runner.current != testAgentCurrent {
		t.Fatalf("Version() = %q, want the running %s after a rollback", runner.current, testAgentCurrent)
	}
}

func TestAgentUpdaterRefusesDowngrade(t *testing.T) {
	release := newAgentReleaseServer(t, false)
	setTestPublicKey(t, release.public)

	target := filepath.Join(t.TempDir(), "gotham-agent")
	if err := os.WriteFile(target, []byte("v9 binary"), 0o755); err != nil {
		t.Fatalf("write target: %v", err)
	}
	cfg := updaterTestConfig(t, target, noopAgentRestart)
	cfg.Version = "v9.0.0"
	runner := newTestUpdater(t, cfg)

	runner.checkOnce(context.Background(), &fakeUpdateClient{resp: release.offer()})

	if got := readFileString(t, target); got != "v9 binary" {
		t.Fatalf("target = %q, want it unchanged after a downgrade offer", got)
	}
	if runner.current != "v9.0.0" {
		t.Fatalf("Version() = %q, want v9.0.0", runner.current)
	}
}

func TestAgentUpdaterRefusesSameVersion(t *testing.T) {
	release := newAgentReleaseServer(t, false)
	setTestPublicKey(t, release.public)

	target := filepath.Join(t.TempDir(), "gotham-agent")
	if err := os.WriteFile(target, []byte("current binary"), 0o755); err != nil {
		t.Fatalf("write target: %v", err)
	}
	cfg := updaterTestConfig(t, target, noopAgentRestart)
	cfg.Version = testAgentVersion
	runner := newTestUpdater(t, cfg)

	runner.checkOnce(context.Background(), &fakeUpdateClient{resp: release.offer()})

	if got := readFileString(t, target); got != "current binary" {
		t.Fatalf("target = %q, want it unchanged for an equal-version offer", got)
	}
}

func TestAgentUpdaterRollsBackOnTamperedSignature(t *testing.T) {
	release := newAgentReleaseServer(t, true)
	setTestPublicKey(t, release.public)

	target := filepath.Join(t.TempDir(), "gotham-agent")
	if err := os.WriteFile(target, []byte("old binary"), 0o755); err != nil {
		t.Fatalf("write target: %v", err)
	}
	runner := newTestUpdater(t, updaterTestConfig(t, target, noopAgentRestart))

	runner.checkOnce(context.Background(), &fakeUpdateClient{resp: release.offer()})

	if got := readFileString(t, target); got != "old binary" {
		t.Fatalf("target = %q, want it unchanged after a tampered signature", got)
	}
	if _, err := os.Stat(target + updatecore.OldSuffix); !os.IsNotExist(err) {
		t.Fatalf("a refused update created a backup: %v", err)
	}
	if runner.current != "v1.0.0" {
		t.Fatalf("Version() = %q, want the old v1.0.0", runner.current)
	}
}

func TestAgentUpdaterRejectsDigestMismatch(t *testing.T) {
	release := newAgentReleaseServer(t, false)
	setTestPublicKey(t, release.public)

	target := filepath.Join(t.TempDir(), "gotham-agent")
	if err := os.WriteFile(target, []byte("old binary"), 0o755); err != nil {
		t.Fatalf("write target: %v", err)
	}
	runner := newTestUpdater(t, updaterTestConfig(t, target, noopAgentRestart))

	offer := release.offer()
	offer.Sha256 = "0000000000000000000000000000000000000000000000000000000000000000"
	runner.checkOnce(context.Background(), &fakeUpdateClient{resp: offer})

	if got := readFileString(t, target); got != "old binary" {
		t.Fatalf("target = %q, want it unchanged after a digest mismatch", got)
	}
}

func TestAgentUpdaterIgnoresPlainOfferWithoutAutoUpdate(t *testing.T) {
	release := newAgentReleaseServer(t, false)
	setTestPublicKey(t, release.public)

	target := filepath.Join(t.TempDir(), "gotham-agent")
	if err := os.WriteFile(target, []byte("old binary"), 0o755); err != nil {
		t.Fatalf("write target: %v", err)
	}
	cfg := updaterTestConfig(t, target, noopAgentRestart)
	cfg.AutoUpdate = false
	runner := newTestUpdater(t, cfg)

	offer := release.offer()
	offer.Rollout = false
	runner.checkOnce(context.Background(), &fakeUpdateClient{resp: offer})

	if got := readFileString(t, target); got != "old binary" {
		t.Fatalf("target = %q, want it unchanged for a plain offer", got)
	}
	if runner.current != "v1.0.0" {
		t.Fatalf("Version() = %q, want v1.0.0", runner.current)
	}
}

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
	runner := newTestUpdater(t, updaterTestConfig(t, target, noopAgentRestart))

	done := make(chan struct{})
	go func() {
		defer close(done)
		runner.checkOnce(context.Background(), &fakeUpdateClient{resp: release.offer()})
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("checkOnce blocked on a non-regular pending/status marker")
	}
}

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
	runner := newTestUpdater(t, updaterTestConfig(t, target, noopAgentRestart))

	runner.checkOnce(context.Background(), &fakeUpdateClient{resp: release.offer()})

	if runner.current != testAgentCurrent {
		t.Fatalf("Version() = %q, want the running %s (stale ok must not be trusted)", runner.current, testAgentCurrent)
	}
}

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
	runner := newTestUpdater(t, updaterTestConfig(t, target, restart))
	client := &fakeUpdateClient{resp: release.offer()}

	runner.checkOnce(context.Background(), client)
	if launches != 1 {
		t.Fatalf("wrapper launches = %d, want 1", launches)
	}
	if !runner.inBackoff(testAgentVersion) {
		t.Fatal("no backoff recorded after a rolled_back outcome")
	}

	runner.checkOnce(context.Background(), client)
	if launches != 1 {
		t.Fatalf("wrapper launches = %d, want 1 (backoff must suppress the retry)", launches)
	}
}

func TestAgentUpdaterSeedsBackoffFromRolledBackStatus(t *testing.T) {
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
	runner := newTestUpdater(t, updaterTestConfig(t, target, restart))

	if !runner.inBackoff(testAgentVersion) {
		t.Fatal("backoff was not seeded from the durable rolled_back status")
	}
	runner.checkOnce(context.Background(), &fakeUpdateClient{resp: release.offer()})
	if launches != 0 {
		t.Fatalf("wrapper launches = %d, want 0 (the failed release must not be re-applied)", launches)
	}
	if got := readFileString(t, target); got != "old binary" {
		t.Fatalf("target = %q, want it unchanged", got)
	}
}

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
	runner := newTestUpdater(t, cfg)
	client := &fakeUpdateClient{resp: release.offer()}

	runner.checkOnce(context.Background(), client)
	if launches != 0 {
		t.Fatalf("wrapper launches = %d before reset, want 0", launches)
	}

	if err := ResetState(cfg); err != nil {
		t.Fatalf("ResetState: %v", err)
	}
	runner.checkOnce(context.Background(), client)
	if launches != 1 {
		t.Fatalf("wrapper launches = %d after reset, want 1", launches)
	}
}

func readFileString(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

func TestReleaseFromOfferBinding(t *testing.T) {
	arch := runtime.GOARCH
	otherArch := "arm64"
	if arch == "arm64" {
		otherArch = "amd64"
	}
	base := func() *agentv1.UpdateResponse {
		return &agentv1.UpdateResponse{
			UpdateAvailable:      true,
			LatestVersion:        testAgentVersion,
			AssetUrl:             "https://releases.example/gotham-agent-linux-" + arch,
			ManifestUrl:          "https://releases.example/gotham-agent-manifest-" + arch + ".txt",
			ManifestSignatureUrl: "https://releases.example/gotham-agent-manifest-" + arch + ".txt.sig",
			Sha256:               strings.Repeat("a", 64),
			Channel:              "stable",
		}
	}

	tests := map[string]struct {
		mutate  func(*agentv1.UpdateResponse)
		channel string
		wantErr bool
	}{
		"valid agent offer": {mutate: func(*agentv1.UpdateResponse) {}, channel: "stable"},
		"control-plane family": {
			mutate: func(r *agentv1.UpdateResponse) {
				r.AssetUrl = "https://releases.example/gotham-linux-" + arch
			},
			channel: "stable", wantErr: true,
		},
		"control-plane family and empty channel (PoC)": {
			mutate: func(r *agentv1.UpdateResponse) {
				r.AssetUrl = "https://releases.example/gotham-linux-" + arch
				r.Channel = ""
			},
			channel: "stable", wantErr: true,
		},
		"wrong arch": {
			mutate: func(r *agentv1.UpdateResponse) {
				r.AssetUrl = "https://releases.example/gotham-agent-linux-" + otherArch
			},
			channel: "stable", wantErr: true,
		},
		"wrong manifest name": {
			mutate: func(r *agentv1.UpdateResponse) {
				r.ManifestUrl = "https://releases.example/gotham-manifest-" + arch + ".txt"
			},
			channel: "stable", wantErr: true,
		},
		"empty channel": {
			mutate:  func(r *agentv1.UpdateResponse) { r.Channel = "" },
			channel: "stable", wantErr: true,
		},
		"beta offer on a stable node": {
			mutate:  func(r *agentv1.UpdateResponse) { r.Channel = "beta" },
			channel: "stable", wantErr: true,
		},
		"unknown configured channel accepts a stable offer (F3 fallback)": {
			mutate:  func(*agentv1.UpdateResponse) {},
			channel: "canary",
		},
		"unknown configured channel refuses a beta offer (F3 fallback)": {
			mutate:  func(r *agentv1.UpdateResponse) { r.Channel = "beta" },
			channel: "canary", wantErr: true,
		},
		"stable offer on a beta node": {
			mutate:  func(*agentv1.UpdateResponse) {},
			channel: "beta",
		},
		"beta offer on a beta node": {
			mutate:  func(r *agentv1.UpdateResponse) { r.Channel = "beta" },
			channel: "beta",
		},
		"unknown offer channel": {
			mutate:  func(r *agentv1.UpdateResponse) { r.Channel = "canary" },
			channel: "beta", wantErr: true,
		},
		"empty configured channel still enforces stable": {
			mutate: func(*agentv1.UpdateResponse) {}, channel: "",
		},
		"beta offer with an empty configured channel": {
			mutate:  func(r *agentv1.UpdateResponse) { r.Channel = "beta" },
			channel: "", wantErr: true,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			resp := base()
			tc.mutate(resp)
			release, err := releaseFromOffer(resp, tc.channel)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("releaseFromOffer accepted %+v, want an error", release)
				}
				if !errors.Is(err, errOffer) {
					t.Errorf("err = %v, want errOffer", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("releaseFromOffer: %v", err)
			}
			if release.AssetName != "gotham-agent-linux-"+arch {
				t.Errorf("AssetName = %q", release.AssetName)
			}
			if release.Channel != resp.GetChannel() {
				t.Errorf("Channel = %q, want %q", release.Channel, resp.GetChannel())
			}
		})
	}
}
