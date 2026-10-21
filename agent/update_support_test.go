package agent

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
	"github.com/justindeelux/gotham/updatecore"
)

// The fixtures below back TestTwoAgentsUpdateAllReportNewVersion, which drives
// the whole Agent loop (heartbeat plus updater) against a fake control plane.

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

func newAgentReleaseServer(t *testing.T) *agentReleaseServer {
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

// healthyRestart models the privileged wrapper succeeding: it records an `ok`
// status for version in the authoritative status file and reports the wrapper
// exited cleanly.
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

// updaterTestConfig builds the agent Config whose update paths live under the
// target's directory.
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
		UpdateChannel:     "stable",
		Version:           testAgentCurrent,
		Restart:           restart,
	}
}
