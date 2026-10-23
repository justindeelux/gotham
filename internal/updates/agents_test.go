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
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/justindeelux/gotham/updatecore"
)

const agentAsset = "gotham-agent-linux-amd64"

type agentFixture struct {
	server      *httptest.Server
	public      ed25519.PublicKey
	artifact    []byte
	manifest    updatecore.Manifest
	releaseHits atomic.Int64
	fail        atomic.Bool
}

func newAgentFixture(t *testing.T, tamperSig bool) *agentFixture {
	t.Helper()
	return newAgentFixtureRelease(t, "v1.2.0", "stable", false, tamperSig)
}

func newAgentFixtureRelease(t *testing.T, tag, manifestChannel string, prerelease, tamperSig bool) *agentFixture {
	t.Helper()
	public, private, err := updatecore.GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	signer, err := updatecore.NewSigner(private)
	if err != nil {
		t.Fatalf("NewSigner: %v", err)
	}
	artifact := []byte("agent binary " + tag)
	manifest := updatecore.BuildManifest(tag, manifestChannel, "amd64", agentAsset, artifact)
	manifestBytes := manifest.Marshal()
	signature := signer.SignBase64(manifestBytes)
	if tamperSig {
		signature = signer.SignBase64([]byte("different payload"))
	}
	manifestName := updatecore.ManifestNameWithPrefix(AgentManifestPrefix, "amd64")

	fixture := &agentFixture{public: public, artifact: artifact, manifest: manifest}

	mux := http.NewServeMux()
	var server *httptest.Server
	mux.HandleFunc("/repos/owner/name/releases", func(w http.ResponseWriter, _ *http.Request) {
		fixture.releaseHits.Add(1)
		if fixture.fail.Load() {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		assets := []map[string]any{
			{"name": agentAsset, "browser_download_url": server.URL + "/" + agentAsset, "size": len(artifact)},
			{"name": manifestName, "browser_download_url": server.URL + "/" + manifestName, "size": len(manifestBytes)},
			{"name": manifestName + updatecore.ManifestSigSuffix, "browser_download_url": server.URL + "/" + manifestName + updatecore.ManifestSigSuffix, "size": len(signature)},
		}
		releases := []map[string]any{{
			"tag_name":     tag,
			"draft":        false,
			"prerelease":   prerelease,
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
	fixture.server = server
	return fixture
}

func newTestAgentUpdater(t *testing.T, fixture *agentFixture) *AgentUpdater {
	t.Helper()
	return newTestAgentUpdaterChannel(t, fixture, ChannelStable)
}

func newTestAgentUpdaterChannel(t *testing.T, fixture *agentFixture, channel Channel) *AgentUpdater {
	t.Helper()
	updater, err := NewAgentUpdater(AgentUpdaterConfig{
		Repo:       "owner/name",
		BaseURL:    fixture.server.URL,
		Channel:    channel,
		PublicKeys: []ed25519.PublicKey{fixture.public},
		Client:     fixture.server.Client(),
		Logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
		CacheTTL:   time.Minute,
	})
	if err != nil {
		t.Fatalf("NewAgentUpdater: %v", err)
	}
	return updater
}

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

func TestAgentUpdaterUnknownArch(t *testing.T) {
	fixture := newAgentFixture(t, false)
	updater := newTestAgentUpdater(t, fixture)

	if _, err := updater.Offer(context.Background(), "v1.0.0", "linux", "riscv64"); !errors.Is(err, ErrAssetNotFound) {
		t.Fatalf("Offer(riscv64) = %v, want ErrAssetNotFound", err)
	}
}

func TestAgentUpdaterNonLinux(t *testing.T) {
	fixture := newAgentFixture(t, false)
	updater := newTestAgentUpdater(t, fixture)

	release, err := updater.Offer(context.Background(), "v1.0.0", "windows", "amd64")
	if err != nil || release != nil {
		t.Fatalf("Offer(windows) = (%+v, %v), want (nil, nil)", release, err)
	}
}

func TestAgentUpdaterRejectsTamperedManifest(t *testing.T) {
	fixture := newAgentFixture(t, true)
	updater := newTestAgentUpdater(t, fixture)

	if _, err := updater.Offer(context.Background(), "v1.0.0", "linux", "amd64"); !errors.Is(err, ErrBadSignature) {
		t.Fatalf("Offer(tampered) = %v, want ErrBadSignature", err)
	}
}

func TestAgentUpdaterBetaChannel(t *testing.T) {
	t.Run("beta subscriber takes a newer stable release", func(t *testing.T) {
		fixture := newAgentFixtureRelease(t, "v1.2.0", "stable", false, false)
		updater := newTestAgentUpdaterChannel(t, fixture, ChannelBeta)

		release, err := updater.Offer(context.Background(), "v1.0.0", "linux", "amd64")
		if err != nil {
			t.Fatalf("Offer: %v", err)
		}
		if release == nil {
			t.Fatal("Offer = nil, want the stable v1.2.0")
		}
		if release.Channel != string(ChannelStable) || release.Prerelease {
			t.Fatalf("offer channel/prerelease = %q/%t, want stable/false", release.Channel, release.Prerelease)
		}
		if release.SHA256 != fixture.manifest.SHA256 {
			t.Errorf("offer sha256 = %q, want the verified manifest digest %q", release.SHA256, fixture.manifest.SHA256)
		}
	})

	t.Run("beta subscriber takes a newer prerelease", func(t *testing.T) {
		fixture := newAgentFixtureRelease(t, "v1.3.0-rc.1", "beta", true, false)
		updater := newTestAgentUpdaterChannel(t, fixture, ChannelBeta)

		release, err := updater.Offer(context.Background(), "v1.0.0", "linux", "amd64")
		if err != nil {
			t.Fatalf("Offer: %v", err)
		}
		if release == nil || release.Channel != string(ChannelBeta) || !release.Prerelease {
			t.Fatalf("Offer = %+v, want the beta prerelease labelled beta", release)
		}
	})

	t.Run("stable subscriber never sees a prerelease", func(t *testing.T) {
		fixture := newAgentFixtureRelease(t, "v1.3.0-rc.1", "beta", true, false)
		updater := newTestAgentUpdater(t, fixture)

		release, err := updater.Offer(context.Background(), "v1.0.0", "linux", "amd64")
		if err != nil || release != nil {
			t.Fatalf("Offer = (%+v, %v), want (nil, nil)", release, err)
		}
	})

	t.Run("mismatched manifest channel is refused", func(t *testing.T) {
		fixture := newAgentFixtureRelease(t, "v1.2.0", "beta", false, false)
		updater := newTestAgentUpdaterChannel(t, fixture, ChannelBeta)

		if _, err := updater.Offer(context.Background(), "v1.0.0", "linux", "amd64"); !errors.Is(err, ErrManifest) {
			t.Fatalf("Offer = %v, want ErrManifest", err)
		}
	})
}

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

func TestAgentUpdaterCheckerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	public, _, _ := updatecore.GenerateKey()
	updater, err := NewAgentUpdater(AgentUpdaterConfig{
		Repo: "owner/name", BaseURL: server.URL, Channel: ChannelStable,
		PublicKeys: []ed25519.PublicKey{public}, Client: server.Client(),
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatalf("NewAgentUpdater: %v", err)
	}
	if _, err := updater.Offer(context.Background(), "v1.0.0", "linux", "amd64"); !errors.Is(err, ErrHTTP) {
		t.Fatalf("Offer(500) = %v, want ErrHTTP", err)
	}
}

func TestAgentUpdaterCachesEmptyResult(t *testing.T) {
	var hits atomic.Int64
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/owner/name/releases", func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{"tag_name": "v0.0.0", "draft": false, "prerelease": false, "assets": []any{}},
		})
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	public, _, err := updatecore.GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	updater, err := NewAgentUpdater(AgentUpdaterConfig{
		Repo: "owner/name", BaseURL: server.URL, Channel: ChannelStable,
		PublicKeys: []ed25519.PublicKey{public}, Client: server.Client(),
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatalf("NewAgentUpdater: %v", err)
	}

	for i := 0; i < 5; i++ {
		release, err := updater.Offer(context.Background(), "v1.0.0", "linux", "amd64")
		if err != nil || release != nil {
			t.Fatalf("Offer %d = (%+v, %v), want (nil, nil)", i, release, err)
		}
	}
	if got := hits.Load(); got != 1 {
		t.Fatalf("release-API hits = %d, want 1 (the empty result must be cached)", got)
	}
	if target, err := updater.TargetVersion(context.Background()); err != nil || target != "" {
		t.Fatalf("TargetVersion = (%q, %v), want (\"\", nil)", target, err)
	}
}

func TestAgentUpdaterEmptyReleasesIsNotAnError(t *testing.T) {
	var hits atomic.Int64
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/owner/name/releases", func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("[]"))
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	public, _, err := updatecore.GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	updater, err := NewAgentUpdater(AgentUpdaterConfig{
		Repo: "owner/name", BaseURL: server.URL, Channel: ChannelStable,
		PublicKeys: []ed25519.PublicKey{public}, Client: server.Client(),
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatalf("NewAgentUpdater: %v", err)
	}

	if release, err := updater.Offer(context.Background(), "v1.0.0", "linux", "amd64"); err != nil || release != nil {
		t.Fatalf("Offer = (%+v, %v), want (nil, nil)", release, err)
	}
	if target, err := updater.TargetVersion(context.Background()); err != nil || target != "" {
		t.Fatalf("TargetVersion = (%q, %v), want (\"\", nil)", target, err)
	}
}

func TestAgentUpdaterFromEnvFeatureOff(t *testing.T) {
	t.Setenv(FeatureEnv, "false")
	updater, err := AgentUpdaterFromEnv(slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil || updater != nil {
		t.Fatalf("AgentUpdaterFromEnv(feature off) = (%+v, %v), want (nil, nil)", updater, err)
	}
}

func TestAgentUpdaterUsesKeyRing(t *testing.T) {
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

	updater, err := AgentUpdaterFromEnv(slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil || updater == nil {
		t.Fatalf("AgentUpdaterFromEnv = (%+v, %v), want an updater", updater, err)
	}
	if updater.verifier == nil {
		t.Fatal("AgentUpdaterFromEnv did not wire the key-ring verifier")
	}

	payload := []byte("signed manifest")
	ringSigner, err := NewSigner(nextPrivate)
	if err != nil {
		t.Fatalf("NewSigner (next): %v", err)
	}
	if err := updater.verifier.Verify(payload, []byte(ringSigner.SignBase64(payload))); err != nil {
		t.Fatalf("the updater refused a manifest signed by the next key: %v", err)
	}

	_, unknownPrivate, err := updatecore.GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey (unknown): %v", err)
	}
	unknownSigner, err := NewSigner(unknownPrivate)
	if err != nil {
		t.Fatalf("NewSigner (unknown): %v", err)
	}
	if err := updater.verifier.Verify(payload, unknownSigner.Sign(payload)); !errors.Is(err, ErrBadSignature) {
		t.Fatalf("the updater accepted an unknown key: %v", err)
	}
}

func TestAgentUpdaterCachesReleaseAPI(t *testing.T) {
	fixture := newAgentFixture(t, false)
	updater := newTestAgentUpdater(t, fixture)

	for i := 0; i < 20; i++ {
		release, err := updater.Offer(context.Background(), "v1.0.0", "linux", "amd64")
		if err != nil || release == nil {
			t.Fatalf("Offer %d = (%+v, %v), want a release", i, release, err)
		}
	}
	if got := fixture.releaseHits.Load(); got != 1 {
		t.Fatalf("release-API hits = %d, want 1 (cache must absorb the fleet)", got)
	}
}

func TestAgentUpdaterServesStaleOnTransientFailure(t *testing.T) {
	fixture := newAgentFixture(t, false)
	updater, err := NewAgentUpdater(AgentUpdaterConfig{
		Repo: "owner/name", BaseURL: fixture.server.URL, Channel: ChannelStable,
		PublicKeys: []ed25519.PublicKey{fixture.public}, Client: fixture.server.Client(),
		Logger:   slog.New(slog.NewTextHandler(io.Discard, nil)),
		CacheTTL: time.Millisecond,
	})
	if err != nil {
		t.Fatalf("NewAgentUpdater: %v", err)
	}

	if _, err := updater.Offer(context.Background(), "v1.0.0", "linux", "amd64"); err != nil {
		t.Fatalf("first Offer: %v", err)
	}
	time.Sleep(5 * time.Millisecond)
	fixture.fail.Store(true)

	release, err := updater.Offer(context.Background(), "v1.0.0", "linux", "amd64")
	if err != nil || release == nil || release.Version != "v1.2.0" {
		t.Fatalf("Offer after failure = (%+v, %v), want the stale v1.2.0", release, err)
	}

	fresh, err := NewAgentUpdater(AgentUpdaterConfig{
		Repo: "owner/name", BaseURL: fixture.server.URL, Channel: ChannelStable,
		PublicKeys: []ed25519.PublicKey{fixture.public}, Client: fixture.server.Client(),
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatalf("NewAgentUpdater: %v", err)
	}
	if _, err := fresh.Offer(context.Background(), "v1.0.0", "linux", "amd64"); !errors.Is(err, ErrHTTP) {
		t.Fatalf("Offer(no cache, failure) = %v, want ErrHTTP", err)
	}
}

func TestAgentUpdaterTargetVersion(t *testing.T) {
	fixture := newAgentFixture(t, false)
	updater := newTestAgentUpdater(t, fixture)

	target, err := updater.TargetVersion(context.Background())
	if err != nil || target != "v1.2.0" {
		t.Fatalf("TargetVersion = (%q, %v), want v1.2.0", target, err)
	}
	if got := fixture.releaseHits.Load(); got != 1 {
		t.Fatalf("release-API hits = %d, want 1", got)
	}
	if _, err := updater.TargetVersion(context.Background()); err != nil {
		t.Fatalf("TargetVersion (cached): %v", err)
	}
	if got := fixture.releaseHits.Load(); got != 1 {
		t.Fatalf("release-API hits = %d, want 1 after a cached call", got)
	}
}

func TestAgentUpdaterNegativeCachesFailure(t *testing.T) {
	var hits atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	public, _, err := updatecore.GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	updater, err := NewAgentUpdater(AgentUpdaterConfig{
		Repo: "owner/name", BaseURL: server.URL, Channel: ChannelStable,
		PublicKeys: []ed25519.PublicKey{public}, Client: server.Client(),
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatalf("NewAgentUpdater: %v", err)
	}

	for i := 0; i < 20; i++ {
		if _, err := updater.Offer(context.Background(), "v1.0.0", "linux", "amd64"); !errors.Is(err, ErrHTTP) {
			t.Fatalf("Offer %d = %v, want ErrHTTP", i, err)
		}
	}
	if got := hits.Load(); got != 1 {
		t.Fatalf("release-API hits = %d, want 1 (negative cache must absorb the fleet)", got)
	}
}

func TestAgentUpdaterSingleFlightConcurrent(t *testing.T) {
	var hits atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	public, _, err := updatecore.GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	updater, err := NewAgentUpdater(AgentUpdaterConfig{
		Repo: "owner/name", BaseURL: server.URL, Channel: ChannelStable,
		PublicKeys: []ed25519.PublicKey{public}, Client: server.Client(),
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatalf("NewAgentUpdater: %v", err)
	}

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = updater.Offer(context.Background(), "v1.0.0", "linux", "amd64")
		}()
	}
	wg.Wait()
	if got := hits.Load(); got != 1 {
		t.Fatalf("release-API hits = %d, want 1 for 50 concurrent cold offers", got)
	}
}

func TestAgentUpdaterStaleServeDoesNotRefetch(t *testing.T) {
	fixture := newAgentFixture(t, false)
	updater, err := NewAgentUpdater(AgentUpdaterConfig{
		Repo: "owner/name", BaseURL: fixture.server.URL, Channel: ChannelStable,
		PublicKeys: []ed25519.PublicKey{fixture.public}, Client: fixture.server.Client(),
		Logger:   slog.New(slog.NewTextHandler(io.Discard, nil)),
		CacheTTL: time.Millisecond,
	})
	if err != nil {
		t.Fatalf("NewAgentUpdater: %v", err)
	}

	if _, err := updater.Offer(context.Background(), "v1.0.0", "linux", "amd64"); err != nil {
		t.Fatalf("first Offer: %v", err)
	}
	time.Sleep(5 * time.Millisecond)
	fixture.fail.Store(true)

	for i := 0; i < 10; i++ {
		release, err := updater.Offer(context.Background(), "v1.0.0", "linux", "amd64")
		if err != nil || release == nil {
			t.Fatalf("Offer %d = (%+v, %v), want the stale release", i, release, err)
		}
	}
	if got := fixture.releaseHits.Load(); got != 2 {
		t.Fatalf("release-API hits = %d, want 2 (stale serve must not refetch)", got)
	}
}
