package githubapp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

// TestManifestEnterpriseURLs proves the manifest base URL is bound to the
// state and used by the callback: a GitHub Enterprise base stores Enterprise
// URLs, and the exchange never goes to api.github.com.
func TestManifestEnterpriseURLs(t *testing.T) {
	svc, _, api, userID := testFixture()
	api.pem = testKeyPEM(t)
	var exchanged []string
	svc.newAPI = func(apiBase string) GitHubAPI {
		exchanged = append(exchanged, apiBase)
		return api
	}

	manifest, err := svc.StartManifest(context.Background(), userID, "https://ghe.example.com", "gotham", "https://gotham.example")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(manifest.ActionURL, "https://ghe.example.com/settings/apps/new") {
		t.Fatalf("action url = %q", manifest.ActionURL)
	}
	app, err := svc.Callback(context.Background(), userID, "manifest-code", manifest.State)
	if err != nil {
		t.Fatal(err)
	}
	if app.BaseURL != "https://ghe.example.com" || app.APIBaseURL != "https://ghe.example.com/api/v3" {
		t.Fatalf("stored urls = %q %q", app.BaseURL, app.APIBaseURL)
	}
	if len(exchanged) != 1 || exchanged[0] != "https://ghe.example.com/api/v3" {
		t.Fatalf("exchange went to %v", exchanged)
	}

	// github.com stays on the defaults.
	manifest2, err := svc.StartManifest(context.Background(), userID, "", "gotham", "https://gotham.example")
	if err != nil {
		t.Fatal(err)
	}
	app2, err := svc.Callback(context.Background(), userID, "manifest-code", manifest2.State)
	if err != nil {
		t.Fatal(err)
	}
	if app2.BaseURL != defaultBaseURL || app2.APIBaseURL != defaultAPIBaseURL {
		t.Fatalf("stored urls = %q %q", app2.BaseURL, app2.APIBaseURL)
	}

	// Hostile base URLs are refused: downgrade, userinfo, path, query and
	// malformed input. A lookalike domain is accepted as an Enterprise host
	// but must never map to the github.com defaults.
	for _, base := range []string{
		"http://ghe.example.com",
		"https://github.com@127.0.0.1",
		"https://ghe.example.com/extra/path",
		"https://ghe.example.com?x=1",
		"not-a-url://[",
	} {
		if _, err := svc.StartManifest(context.Background(), userID, base, "gotham", "https://gotham.example"); err == nil {
			t.Errorf("base url %q was accepted", base)
		}
	}
	web, apiBase, err := svc.normalizeGitHubURLs("https://github.com.evil.example")
	if err != nil {
		t.Fatal(err)
	}
	if web == defaultBaseURL || apiBase == defaultAPIBaseURL {
		t.Errorf("lookalike domain mapped to github.com defaults: %q %q", web, apiBase)
	}
}

// TestManifestStateExpiry proves an expired state refuses redeem, using an
// injected clock.
func TestManifestStateExpiry(t *testing.T) {
	now := time.Now()
	store := &stateStore{entries: make(map[string]stateEntry), now: func() time.Time { return now }}
	userID := uuid.New()
	state, err := store.new(userID, uuid.Nil, stateManifest, defaultBaseURL, defaultAPIBaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := store.redeem(state, userID, uuid.Nil, stateManifest); !ok {
		t.Fatal("fresh state was refused")
	}
	state2, err := store.new(userID, uuid.Nil, stateManifest, defaultBaseURL, defaultAPIBaseURL)
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(stateTTL + time.Second)
	if _, ok := store.redeem(state2, userID, uuid.Nil, stateManifest); ok {
		t.Fatal("expired state was accepted")
	}
	if _, ok := store.peek(state2, stateManifest); ok {
		t.Fatal("expired state is still peekable")
	}
}

// TestManifestCodeRedacted proves a failed manifest conversion never carries
// the single-use code in its error text.
func TestManifestCodeRedacted(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	api := NewHTTPAPI(server.URL, true)
	_, err := api.ExchangeManifest(context.Background(), "super-secret-code")
	if err == nil {
		t.Fatal("expected an exchange error")
	}
	if strings.Contains(err.Error(), "super-secret-code") {
		t.Fatalf("error leaks the manifest code: %v", err)
	}
}

// TestGuardHostRejectsNonPublic proves literal non-public IPs are refused,
// with and without ports, in v4 and v6, including transition embeddings and
// IPv4-mapped forms.
func TestGuardHostRejectsNonPublic(t *testing.T) {
	for _, host := range []string{
		"127.0.0.1", "::1", "10.0.0.1", "192.168.1.1", "172.16.0.1",
		"169.254.169.254", "0.0.0.0", "", "224.0.0.1",
		"100.100.100.200", "192.0.0.1", "198.18.0.1",
		"64:ff9b::7f00:1", "2002:7f00:1::", "::ffff:127.0.0.1",
	} {
		if err := guardHostAllow(host, false); err == nil {
			t.Errorf("host %q was accepted", host)
		}
		if err := guardHostAllow(host, true); err != nil {
			t.Errorf("allowUnsafe host %q was refused: %v", host, err)
		}
	}
}

// TestNormalizeGitHubURLsEnterpriseShapes pins URL normalization: ports are
// kept, github.com (any case) selects the defaults, paths are refused.
func TestNormalizeGitHubURLsEnterpriseShapes(t *testing.T) {
	svc, _, _, _ := testFixture()
	web, api, err := svc.normalizeGitHubURLs("https://ghe.example.com:8443")
	if err != nil {
		t.Fatal(err)
	}
	if web != "https://ghe.example.com:8443" || api != "https://ghe.example.com:8443/api/v3" {
		t.Fatalf("urls = %q %q", web, api)
	}
	web, api, err = svc.normalizeGitHubURLs("https://GitHub.com")
	if err != nil {
		t.Fatal(err)
	}
	if web != defaultBaseURL || api != defaultAPIBaseURL {
		t.Fatalf("urls = %q %q", web, api)
	}
}

// TestManifestSetupURL proves the manifest carries the browser setup route,
// so GitHub returns the installation_id to the SPA after install.
func TestManifestSetupURL(t *testing.T) {
	svc, _, _, userID := testFixture()
	manifest, err := svc.StartManifest(context.Background(), userID, "", "gotham", "https://gotham.example")
	if err != nil {
		t.Fatal(err)
	}
	if manifest.Manifest["setup_url"] != "https://gotham.example/applications/github-app/callback" {
		t.Fatalf("setup_url = %v", manifest.Manifest["setup_url"])
	}
	redirect, _ := manifest.Manifest["redirect_url"].(string)
	if !strings.HasPrefix(redirect, "https://gotham.example/api/v1/providers/github-app/callback?state=") {
		t.Fatalf("redirect_url = %v", manifest.Manifest["redirect_url"])
	}
	// pull_request stays unsubscribed until previews support app-signed
	// deliveries; push drives deploys and the installation events refresh
	// the cache.
	events, _ := manifest.Manifest["default_events"].([]string)
	want := []string{"push", "installation", "installation_repositories"}
	if len(events) != len(want) {
		t.Fatalf("default_events = %v, want %v", events, want)
	}
	for i := range want {
		if events[i] != want[i] {
			t.Fatalf("default_events = %v, want %v", events, want)
		}
	}
}
