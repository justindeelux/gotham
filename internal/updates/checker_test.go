package updates

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/justindeelux/gotham/updatecore"
)

// fixtureRelease is a minimal GitHub release fixture; asset URLs are generated
// from the serving host so they stay absolute and reachable.
type fixtureRelease struct {
	Tag        string
	Prerelease bool
	Draft      bool
	Body       string
	Assets     []string
}

// releasesHandler serves a GitHub-Releases-like array. A non-zero status makes
// the request fail instead.
func releasesHandler(releases []fixtureRelease, status int, delay time.Duration) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if delay > 0 {
			select {
			case <-time.After(delay):
			case <-r.Context().Done():
				return
			}
		}
		if status != 0 {
			w.WriteHeader(status)
			return
		}
		base := "http://" + r.Host
		out := make([]map[string]any, 0, len(releases))
		for _, release := range releases {
			assets := make([]map[string]any, 0, len(release.Assets))
			for _, name := range release.Assets {
				assets = append(assets, map[string]any{
					"name":                 name,
					"browser_download_url": base + "/" + name,
					"size":                 1024,
				})
			}
			out = append(out, map[string]any{
				"tag_name":     release.Tag,
				"body":         release.Body,
				"draft":        release.Draft,
				"prerelease":   release.Prerelease,
				"published_at": "2026-01-02T15:04:05Z",
				"assets":       assets,
			})
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(out)
	}
}

// platformAssets is the asset set a complete, signable release carries.
func platformAssets() []string {
	return []string{
		"gotham-linux-amd64",
		updatecore.ManifestName("amd64"),
		updatecore.ManifestName("amd64") + ManifestSigSuffix,
	}
}

// newChecker points a Checker at a fixture server for the running architecture.
func newChecker(t *testing.T, server *httptest.Server) *Checker {
	t.Helper()
	return &Checker{
		BaseURL: server.URL,
		Repo:    "owner/name",
		Channel: ChannelStable,
		GOARCH:  "amd64",
		Client:  server.Client(),
	}
}

// TestCheckerResolvesNewerRelease covers newer/equal/older and asset mapping.
func TestCheckerResolvesNewerRelease(t *testing.T) {
	server := httptest.NewServer(releasesHandler([]fixtureRelease{
		{Tag: "v1.2.0", Body: "notes", Assets: platformAssets()},
	}, 0, 0))
	defer server.Close()

	release, err := newChecker(t, server).Check(context.Background(), "v1.0.0")
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if release == nil {
		t.Fatal("Check = nil, want v1.2.0")
	}
	if release.Version != "v1.2.0" || release.Tag != "v1.2.0" {
		t.Errorf("release version/tag = %q/%q", release.Version, release.Tag)
	}
	if release.AssetName != "gotham-linux-amd64" {
		t.Errorf("asset name = %q", release.AssetName)
	}
	if release.AssetURL != server.URL+"/gotham-linux-amd64" {
		t.Errorf("asset url = %q", release.AssetURL)
	}
	if release.ManifestURL != server.URL+"/"+updatecore.ManifestName("amd64") {
		t.Errorf("manifest url = %q", release.ManifestURL)
	}
	if release.ManifestSignatureURL != server.URL+"/"+updatecore.ManifestName("amd64")+ManifestSigSuffix {
		t.Errorf("manifest signature url = %q", release.ManifestSignatureURL)
	}

	for _, current := range []string{"v1.2.0", "v2.0.0"} {
		got, err := newChecker(t, server).Check(context.Background(), current)
		if err != nil {
			t.Fatalf("Check(%s): %v", current, err)
		}
		if got != nil {
			t.Errorf("Check(%s) = %+v, want nil", current, got)
		}
	}
}

// TestCheckerPicksNewest covers multiple releases and prerelease filtering per
// channel.
func TestCheckerPicksNewest(t *testing.T) {
	releases := []fixtureRelease{
		{Tag: "v1.1.0", Assets: platformAssets()},
		{Tag: "v1.3.0-rc.1", Prerelease: true, Assets: platformAssets()},
		{Tag: "v1.2.0", Assets: platformAssets()},
		{Tag: "v2.0.0", Draft: true, Assets: platformAssets()},
	}
	server := httptest.NewServer(releasesHandler(releases, 0, 0))
	defer server.Close()

	stable, err := newChecker(t, server).Check(context.Background(), "v1.0.0")
	if err != nil {
		t.Fatalf("stable Check: %v", err)
	}
	if stable == nil || stable.Version != "v1.2.0" {
		t.Fatalf("stable Check = %+v, want v1.2.0", stable)
	}
	if stable.Channel != string(ChannelStable) {
		t.Errorf("stable offer channel = %q, want stable", stable.Channel)
	}

	beta := newChecker(t, server)
	beta.Channel = ChannelBeta
	got, err := beta.Check(context.Background(), "v1.0.0")
	if err != nil {
		t.Fatalf("beta Check: %v", err)
	}
	if got == nil || got.Version != "v1.3.0-rc.1" {
		t.Fatalf("beta Check = %+v, want v1.3.0-rc.1", got)
	}
	if got.Channel != string(ChannelBeta) {
		t.Errorf("beta offer channel = %q, want beta", got.Channel)
	}
	if !got.Prerelease {
		t.Error("beta release not marked prerelease")
	}
}

// TestCheckerLabelsOfferWithReleaseChannel is M4: the offer carries the
// release's own channel, not the subscriber's, so a beta subscriber can take a
// newer stable release and still bind it to the stable signed manifest.
func TestCheckerLabelsOfferWithReleaseChannel(t *testing.T) {
	server := httptest.NewServer(releasesHandler([]fixtureRelease{
		{Tag: "v1.2.0", Body: "stable notes", Assets: platformAssets()},
	}, 0, 0))
	defer server.Close()

	beta := newChecker(t, server)
	beta.Channel = ChannelBeta
	release, err := beta.Check(context.Background(), "v1.0.0")
	if err != nil {
		t.Fatalf("beta Check: %v", err)
	}
	if release == nil || release.Version != "v1.2.0" {
		t.Fatalf("beta Check = %+v, want v1.2.0", release)
	}
	if release.Channel != string(ChannelStable) || release.Prerelease {
		t.Errorf("beta subscriber offer = channel %q prerelease %t, want stable/false", release.Channel, release.Prerelease)
	}

	// A stable subscriber still never sees a prerelease.
	prerelease := httptest.NewServer(releasesHandler([]fixtureRelease{
		{Tag: "v1.3.0-rc.1", Prerelease: true, Assets: platformAssets()},
	}, 0, 0))
	defer prerelease.Close()
	stable, err := newChecker(t, prerelease).Check(context.Background(), "v1.0.0")
	if err != nil {
		t.Fatalf("stable Check: %v", err)
	}
	if stable != nil {
		t.Fatalf("stable Check = %+v, want nil (prereleases never offered)", stable)
	}
}

// TestCheckerFailures covers missing assets, HTTP errors, empty releases and
// timeouts.
func TestCheckerFailures(t *testing.T) {
	t.Run("missing asset", func(t *testing.T) {
		server := httptest.NewServer(releasesHandler([]fixtureRelease{
			{Tag: "v1.2.0", Assets: []string{"gotham-linux-arm64"}},
		}, 0, 0))
		defer server.Close()
		if _, err := newChecker(t, server).Check(context.Background(), "v1.0.0"); !errors.Is(err, ErrAssetNotFound) {
			t.Fatalf("Check = %v, want ErrAssetNotFound", err)
		}
	})

	t.Run("http error", func(t *testing.T) {
		server := httptest.NewServer(releasesHandler(nil, http.StatusInternalServerError, 0))
		defer server.Close()
		if _, err := newChecker(t, server).Check(context.Background(), "v1.0.0"); !errors.Is(err, ErrHTTP) {
			t.Fatalf("Check = %v, want ErrHTTP", err)
		}
	})

	t.Run("empty releases", func(t *testing.T) {
		server := httptest.NewServer(releasesHandler(nil, 0, 0))
		defer server.Close()
		if _, err := newChecker(t, server).Check(context.Background(), "v1.0.0"); !errors.Is(err, ErrNoRelease) {
			t.Fatalf("Check = %v, want ErrNoRelease", err)
		}
	})

	t.Run("timeout", func(t *testing.T) {
		server := httptest.NewServer(releasesHandler([]fixtureRelease{
			{Tag: "v1.2.0", Assets: platformAssets()},
		}, 0, 200*time.Millisecond))
		defer server.Close()
		checker := newChecker(t, server)
		checker.Client = nil
		checker.Timeout = 20 * time.Millisecond
		if _, err := checker.Check(context.Background(), "v1.0.0"); !errors.Is(err, ErrHTTP) {
			t.Fatalf("Check = %v, want ErrHTTP (timeout)", err)
		}
	})

	t.Run("unparseable current version", func(t *testing.T) {
		server := httptest.NewServer(releasesHandler(nil, 0, 0))
		defer server.Close()
		if _, err := newChecker(t, server).Check(context.Background(), "dev"); err == nil {
			t.Fatal("Check(dev) = nil error, want failure")
		}
	})
}
