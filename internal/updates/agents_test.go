package updates

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/justindeelux/gotham/updatecore"
)

// agentAsset is the agent release asset name for amd64.
const agentAsset = "gotham-agent-linux-amd64"

// agentFixture serves a GitHub-Releases-like API plus the signed agent manifest
// and artifact for one release.
type agentFixture struct {
	server   *httptest.Server
	public   ed25519.PublicKey
	artifact []byte
	manifest updatecore.Manifest
}

// newAgentFixture starts a release server for v1.2.0. tamperSig signs a
// different payload so the manifest signature no longer verifies.
func newAgentFixture(t *testing.T, tamperSig bool) *agentFixture {
	t.Helper()
	public, private, err := updatecore.GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	signer, err := updatecore.NewSigner(private)
	if err != nil {
		t.Fatalf("NewSigner: %v", err)
	}
	artifact := []byte("agent binary v1.2.0")
	manifest := updatecore.BuildManifest("v1.2.0", "stable", "amd64", agentAsset, artifact)
	manifestBytes := manifest.Marshal()
	signature := signer.SignBase64(manifestBytes)
	if tamperSig {
		signature = signer.SignBase64([]byte("different payload"))
	}
	manifestName := updatecore.ManifestNameWithPrefix(AgentManifestPrefix, "amd64")

	mux := http.NewServeMux()
	var server *httptest.Server
	mux.HandleFunc("/repos/owner/name/releases", func(w http.ResponseWriter, _ *http.Request) {
		assets := []map[string]any{
			{"name": agentAsset, "browser_download_url": server.URL + "/" + agentAsset, "size": len(artifact)},
			{"name": manifestName, "browser_download_url": server.URL + "/" + manifestName, "size": len(manifestBytes)},
			{"name": manifestName + updatecore.ManifestSigSuffix, "browser_download_url": server.URL + "/" + manifestName + updatecore.ManifestSigSuffix, "size": len(signature)},
		}
		releases := []map[string]any{{
			"tag_name":     "v1.2.0",
			"draft":        false,
			"prerelease":   false,
			"published_at": "2026-01-02T15:04:05Z",
			"assets":       assets,
		}}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(releases)
	})
	mux.HandleFunc("/"+agentAsset, func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(artifact) })
	mux.HandleFunc("/"+manifestName, func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(manifestBytes) })
	mux.HandleFunc("/"+manifestName+updatecore.ManifestSigSuffix, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(signature))
	})
	server = httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return &agentFixture{server: server, public: public, artifact: artifact, manifest: manifest}
}

// newTestAgentUpdater points an AgentUpdater at a fixture.
func newTestAgentUpdater(t *testing.T, fixture *agentFixture) *AgentUpdater {
	t.Helper()
	updater, err := NewAgentUpdater(AgentUpdaterConfig{
		Repo:      "owner/name",
		BaseURL:   fixture.server.URL,
		Channel:   ChannelStable,
		PublicKey: fixture.public,
		Client:    fixture.server.Client(),
		Logger:    slog.New(slog.NewTextHandler(io.Discard, nil)),
		CacheTTL:  time.Minute,
	})
	if err != nil {
		t.Fatalf("NewAgentUpdater: %v", err)
	}
	return updater
}

// TestAgentUpdaterOffersVerifiedRelease proves a newer release is offered with
// the verified manifest digest bound into the offer.
func TestAgentUpdaterOffersVerifiedRelease(t *testing.T) {
	fixture := newAgentFixture(t, false)
	updater := newTestAgentUpdater(t, fixture)

	release, err := updater.Offer(context.Background(), "v1.0.0", "linux", "amd64")
	if err != nil {
		t.Fatalf("Offer: %v", err)
	}
	if release == nil {
		t.Fatal("Offer = nil, want a release")
	}
	if release.Version != "v1.2.0" || release.SHA256 != fixture.manifest.SHA256 {
		t.Fatalf("release = %+v, want v1.2.0 with the signed digest", release)
	}
	if release.AssetName != agentAsset || release.AssetURL != fixture.server.URL+"/"+agentAsset {
		t.Fatalf("release asset = %q %q", release.AssetName, release.AssetURL)
	}
}

// TestAgentUpdaterUpToDate proves no offer is made for a current agent.
func TestAgentUpdaterUpToDate(t *testing.T) {
	fixture := newAgentFixture(t, false)
	updater := newTestAgentUpdater(t, fixture)

	release, err := updater.Offer(context.Background(), "v1.2.0", "linux", "amd64")
	if err != nil {
		t.Fatalf("Offer: %v", err)
	}
	if release != nil {
		t.Fatalf("Offer = %+v, want nil (up to date)", release)
	}
}

// TestAgentUpdaterUnknownArch proves an unsupported architecture is refused.
func TestAgentUpdaterUnknownArch(t *testing.T) {
	fixture := newAgentFixture(t, false)
	updater := newTestAgentUpdater(t, fixture)

	if _, err := updater.Offer(context.Background(), "v1.0.0", "linux", "riscv64"); !errors.Is(err, ErrAssetNotFound) {
		t.Fatalf("Offer(riscv64) = %v, want ErrAssetNotFound", err)
	}
}

// TestAgentUpdaterNonLinux proves a non-linux platform gets no offer.
func TestAgentUpdaterNonLinux(t *testing.T) {
	fixture := newAgentFixture(t, false)
	updater := newTestAgentUpdater(t, fixture)

	release, err := updater.Offer(context.Background(), "v1.0.0", "windows", "amd64")
	if err != nil || release != nil {
		t.Fatalf("Offer(windows) = (%+v, %v), want (nil, nil)", release, err)
	}
}

// TestAgentUpdaterRejectsTamperedManifest proves an unverifiable manifest is
// never turned into an offer.
func TestAgentUpdaterRejectsTamperedManifest(t *testing.T) {
	fixture := newAgentFixture(t, true)
	updater := newTestAgentUpdater(t, fixture)

	if _, err := updater.Offer(context.Background(), "v1.0.0", "linux", "amd64"); !errors.Is(err, ErrBadSignature) {
		t.Fatalf("Offer(tampered) = %v, want ErrBadSignature", err)
	}
}

// TestAgentUpdaterNoKey proves the updater is disabled without a public key.
func TestAgentUpdaterNoKey(t *testing.T) {
	updater, err := NewAgentUpdater(AgentUpdaterConfig{Repo: "owner/name", BaseURL: "https://api.github.com"})
	if err != nil {
		t.Fatalf("NewAgentUpdater: %v", err)
	}
	if updater != nil {
		t.Fatal("NewAgentUpdater without a key = non-nil, want nil (fail closed)")
	}
	var disabled *AgentUpdater
	release, err := disabled.Offer(context.Background(), "v1.0.0", "linux", "amd64")
	if err != nil || release != nil {
		t.Fatalf("nil Offer = (%+v, %v), want (nil, nil)", release, err)
	}
}

// TestAgentUpdaterCheckerError proves a release-API failure is an error, not a
// silent no-update.
func TestAgentUpdaterCheckerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	public, _, _ := updatecore.GenerateKey()
	updater, err := NewAgentUpdater(AgentUpdaterConfig{
		Repo: "owner/name", BaseURL: server.URL, Channel: ChannelStable,
		PublicKey: public, Client: server.Client(),
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatalf("NewAgentUpdater: %v", err)
	}
	if _, err := updater.Offer(context.Background(), "v1.0.0", "linux", "amd64"); !errors.Is(err, ErrHTTP) {
		t.Fatalf("Offer(500) = %v, want ErrHTTP", err)
	}
}

// TestAgentUpdaterFromEnvFeatureOff proves FEATURE_UPDATES=false disables the
// offerer entirely.
func TestAgentUpdaterFromEnvFeatureOff(t *testing.T) {
	t.Setenv(FeatureEnv, "false")
	updater, err := AgentUpdaterFromEnv(slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil || updater != nil {
		t.Fatalf("AgentUpdaterFromEnv(feature off) = (%+v, %v), want (nil, nil)", updater, err)
	}
}
