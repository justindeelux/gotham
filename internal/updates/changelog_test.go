package updates

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
)

// changelogFixture is a release row for the changelog test server.
type changelogFixture struct {
	tag        string
	body       string
	htmlURL    string
	prerelease bool
	draft      bool
}

// serveChangelogFixtures serves a GitHub-Releases-like array including html_url.
func serveChangelogFixtures(t *testing.T, releases []changelogFixture) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		out := make([]map[string]any, 0, len(releases))
		for _, release := range releases {
			out = append(out, map[string]any{
				"tag_name":     release.tag,
				"body":         release.body,
				"html_url":     release.htmlURL,
				"draft":        release.draft,
				"prerelease":   release.prerelease,
				"published_at": "2026-01-02T15:04:05Z",
				"assets":       []any{},
			})
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(out); err != nil {
			t.Errorf("encode fixtures: %v", err)
		}
	}))
}

func TestChangelogListsNewerNewestFirst(t *testing.T) {
	server := serveChangelogFixtures(t, []changelogFixture{
		{tag: "v1.0.0", body: "old"},
		{tag: "v1.2.0", body: "## New\n- a", htmlURL: "https://github.com/o/r/releases/tag/v1.2.0"},
		{tag: "v1.1.0", body: "middle", htmlURL: "https://github.com/o/r/releases/tag/v1.1.0"},
		{tag: "v1.3.0", body: "draft", draft: true},
		{tag: "not-a-version", body: "skip"},
	})
	defer server.Close()

	checker := &Checker{BaseURL: server.URL, GOARCH: "amd64"}
	entries, err := checker.Changelog(context.Background(), "v1.0.0")
	if err != nil {
		t.Fatalf("Changelog: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("entries = %d, want 2", len(entries))
	}
	if entries[0].Version != "v1.2.0" || entries[1].Version != "v1.1.0" {
		t.Fatalf("order = %q, %q; want v1.2.0 then v1.1.0", entries[0].Version, entries[1].Version)
	}
	if entries[0].HTMLURL != "https://github.com/o/r/releases/tag/v1.2.0" {
		t.Fatalf("HTMLURL = %q", entries[0].HTMLURL)
	}
}

func TestChangelogSkipsPrereleasesOnStable(t *testing.T) {
	server := serveChangelogFixtures(t, []changelogFixture{
		{tag: "v1.1.0-beta", body: "beta", prerelease: true, htmlURL: "https://github.com/o/r/releases/tag/v1.1.0-beta"},
		{tag: "v1.1.0", body: "stable"},
	})
	defer server.Close()

	stable := &Checker{BaseURL: server.URL}
	entries, err := stable.Changelog(context.Background(), "v1.0.0")
	if err != nil {
		t.Fatalf("Changelog: %v", err)
	}
	if len(entries) != 1 || entries[0].Version != "v1.1.0" {
		t.Fatalf("stable entries = %+v, want only v1.1.0", entries)
	}

	beta := &Checker{BaseURL: server.URL, Channel: ChannelBeta}
	entries, err = beta.Changelog(context.Background(), "v1.0.0")
	if err != nil {
		t.Fatalf("Changelog: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("beta entries = %d, want 2", len(entries))
	}
}

func TestChangelogBoundsCountAndBody(t *testing.T) {
	var fixtures []changelogFixture
	for i := 1; i <= maxChangelogEntries+5; i++ {
		fixtures = append(fixtures, changelogFixture{
			tag:     fmt.Sprintf("v1.%d.0", i),
			body:    strings.Repeat("x", maxNoteBytes+100),
			htmlURL: "https://github.com/o/r/releases",
		})
	}
	server := serveChangelogFixtures(t, fixtures)
	defer server.Close()

	checker := &Checker{BaseURL: server.URL}
	entries, err := checker.Changelog(context.Background(), "v1.0.0")
	if err != nil {
		t.Fatalf("Changelog: %v", err)
	}
	if len(entries) != maxChangelogEntries {
		t.Fatalf("entries = %d, want bounded %d", len(entries), maxChangelogEntries)
	}
	for _, entry := range entries {
		if len(entry.Notes) > maxNoteBytes+50 {
			t.Fatalf("entry %s notes unbounded: %d bytes", entry.Version, len(entry.Notes))
		}
		if !strings.Contains(entry.Notes, "truncated") {
			t.Fatalf("entry %s notes missing truncation marker", entry.Version)
		}
	}
}

func TestChangelogDevVersionIsEmptyNotError(t *testing.T) {
	server := serveChangelogFixtures(t, []changelogFixture{{tag: "v1.0.0", body: "x"}})
	defer server.Close()

	checker := &Checker{BaseURL: server.URL}
	entries, err := checker.Changelog(context.Background(), "0.2.x-dev")
	if err != nil {
		t.Fatalf("Changelog dev: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("dev entries = %d, want 0", len(entries))
	}
	if _, err := checker.CurrentChangelog(context.Background(), "0.2.x-dev"); err != nil {
		t.Fatalf("CurrentChangelog dev: %v", err)
	}
}

func TestCurrentChangelogFindsRunningVersion(t *testing.T) {
	server := serveChangelogFixtures(t, []changelogFixture{
		{tag: "v1.0.0", body: "# 1.0\n- first", htmlURL: "https://github.com/o/r/releases/tag/v1.0.0"},
		{tag: "v1.1.0", body: "next"},
	})
	defer server.Close()

	checker := &Checker{BaseURL: server.URL}
	entry, err := checker.CurrentChangelog(context.Background(), "v1.0.0")
	if err != nil {
		t.Fatalf("CurrentChangelog: %v", err)
	}
	if entry == nil || entry.Version != "v1.0.0" || entry.HTMLURL == "" {
		t.Fatalf("entry = %+v, want v1.0.0 with link", entry)
	}

	missing, err := checker.CurrentChangelog(context.Background(), "v9.9.9")
	if err != nil {
		t.Fatalf("CurrentChangelog missing: %v", err)
	}
	if missing != nil {
		t.Fatalf("missing = %+v, want nil", missing)
	}
}

func TestHTTPSLinkValidation(t *testing.T) {
	cases := map[string]string{
		"https://github.com/o/r/releases/tag/v1": "https://github.com/o/r/releases/tag/v1",
		"javascript:alert(1)":                    "",
		"http://github.com/o/r":                  "",
		"/relative/path":                         "",
		"":                                       "",
		"::not a url::":                          "",
	}
	for raw, want := range cases {
		if got := httpsLink(raw); got != want {
			t.Errorf("httpsLink(%q) = %q, want %q", raw, got, want)
		}
	}
}

func TestCheckExposesHTMLURL(t *testing.T) {
	server := serveChangelogFixtures(t, []changelogFixture{
		{tag: "v1.0.0", body: "old"},
	})
	defer server.Close()
	_ = server
	// The check path resolves platform assets; reuse the releasesHandler
	// fixture style from checker_test.go instead.
	arch := "amd64"
	assets := []string{"gotham-linux-" + arch, "gotham-manifest-" + arch + ".txt", "gotham-manifest-" + arch + ".txt.sig"}
	mux := http.NewServeMux()
	base := ""
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		out := []map[string]any{{
			"tag_name":     "v1.1.0",
			"body":         "notes",
			"html_url":     "https://github.com/o/r/releases/tag/v1.1.0",
			"draft":        false,
			"prerelease":   false,
			"published_at": "2026-01-02T15:04:05Z",
			"assets": func() []map[string]any {
				var assetsOut []map[string]any
				for _, name := range assets {
					assetsOut = append(assetsOut, map[string]any{
						"name": name, "browser_download_url": base + "/" + name, "size": 1024,
					})
				}
				return assetsOut
			}(),
		}}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(out)
	})
	releaseServer := httptest.NewServer(mux)
	defer releaseServer.Close()
	base = releaseServer.URL

	checker := &Checker{BaseURL: releaseServer.URL, GOARCH: arch}
	release, err := checker.Check(context.Background(), "v1.0.0")
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if release == nil || release.HTMLURL != "https://github.com/o/r/releases/tag/v1.1.0" {
		t.Fatalf("release = %+v, want html_url", release)
	}
}

func TestChangelogCachedForOfferTTL(t *testing.T) {
	t.Setenv(OfferCacheTTLEnv, "10m")
	server := serveChangelogFixtures(t, []changelogFixture{{tag: "v1.1.0", body: "n"}})
	defer server.Close()

	svc, err := NewService(Config{
		Current:    "v1.0.0",
		BaseURL:    server.URL,
		BinaryPath: "/usr/local/bin/gotham",
		GOARCH:     "amd64",
	})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	first, err := svc.Changelog(context.Background())
	if err != nil {
		t.Fatalf("Changelog: %v", err)
	}
	second, err := svc.Changelog(context.Background())
	if err != nil {
		t.Fatalf("Changelog again: %v", err)
	}
	if len(first) != len(second) {
		t.Fatalf("cached mismatch: %d vs %d", len(first), len(second))
	}
	impl, ok := svc.(*service)
	if !ok {
		t.Fatal("service is not *service")
	}
	impl.mu.Lock()
	cached := impl.changelogUntil.After(time.Now())
	impl.mu.Unlock()
	if !cached {
		t.Fatal("changelog result was not cached")
	}
}

// newTestRouter mounts the update routes with the given admin predicate.
func newTestRouter(svc Service, isAdmin func(*http.Request) bool) chi.Router {
	router := chi.NewRouter()
	Mount(router, identityAuth, identityAuth, isAdmin, svc)
	return router
}

func TestChangelogEndpointGatesNotesForNonAdmin(t *testing.T) {
	published := time.Unix(0, 0).UTC()
	entries := []ChangelogEntry{{
		Version:     "v1.2.0",
		Tag:         "v1.2.0",
		Channel:     "stable",
		Notes:       "## New\n- a",
		PublishedAt: published,
		HTMLURL:     "https://github.com/o/r/releases/tag/v1.2.0",
	}}

	t.Run("non-admin gets metadata only", func(t *testing.T) {
		router := newTestRouter(&fakeService{changelog: entries}, denyAdmin)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/updates/changelog", nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
		var body changelogResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if len(body.Entries) != 1 {
			t.Fatalf("entries = %d, want 1", len(body.Entries))
		}
		if body.Entries[0].Notes != "" || body.Entries[0].HTMLURL != "" {
			t.Fatalf("non-admin saw gated fields: %+v", body.Entries[0])
		}
		if body.Entries[0].Version != "v1.2.0" {
			t.Fatalf("version = %q", body.Entries[0].Version)
		}
	})

	t.Run("admin gets notes and link", func(t *testing.T) {
		router := newTestRouter(&fakeService{changelog: entries}, allowAdmin)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/updates/changelog", nil))
		var body changelogResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if body.Entries[0].Notes != "## New\n- a" {
			t.Fatalf("notes = %q", body.Entries[0].Notes)
		}
		if body.Entries[0].HTMLURL != "https://github.com/o/r/releases/tag/v1.2.0" {
			t.Fatalf("html_url = %q", body.Entries[0].HTMLURL)
		}
	})

	t.Run("admin link is revalidated", func(t *testing.T) {
		bad := []ChangelogEntry{{
			Version: "v1.2.0",
			HTMLURL: "javascript:alert(1)",
		}}
		router := newTestRouter(&fakeService{changelog: bad}, allowAdmin)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/updates/changelog", nil))
		var body changelogResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if body.Entries[0].HTMLURL != "" {
			t.Fatalf("html_url = %q, want dropped", body.Entries[0].HTMLURL)
		}
	})

	t.Run("empty is 200 not an error", func(t *testing.T) {
		router := newTestRouter(&fakeService{}, allowAdmin)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/updates/changelog", nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
		var body changelogResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if body.Entries == nil || len(body.Entries) != 0 {
			t.Fatalf("entries = %+v, want empty list", body.Entries)
		}
	})

	t.Run("upstream failure maps to 502", func(t *testing.T) {
		router := newTestRouter(&fakeService{changelogErr: ErrHTTP}, allowAdmin)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/updates/changelog", nil))
		if rec.Code != http.StatusBadGateway {
			t.Fatalf("status = %d, want 502", rec.Code)
		}
	})
}

func TestCheckEndpointExposesHTMLURLToAdminOnly(t *testing.T) {
	release := &Release{Version: "v1.2.0", Channel: "stable", Notes: "n", HTMLURL: "https://github.com/o/r/releases/tag/v1.2.0"}

	router := newTestRouter(&fakeService{release: release}, denyAdmin)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/updates/check", nil))
	var anon checkResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &anon); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if anon.HTMLURL != "" {
		t.Fatalf("non-admin html_url = %q", anon.HTMLURL)
	}

	router = newTestRouter(&fakeService{release: release}, allowAdmin)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/updates/check", nil))
	var admin checkResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &admin); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if admin.HTMLURL != "https://github.com/o/r/releases/tag/v1.2.0" {
		t.Fatalf("admin html_url = %q", admin.HTMLURL)
	}
}
