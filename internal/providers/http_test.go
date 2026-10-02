package providers

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/oauth2"
)

// mustURL parses a URL or fails the test.
func mustURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	parsed, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse %q: %v", raw, err)
	}
	return parsed
}

// TestValidateBaseURL is the C1-15 / U2 regression: stored base URLs are
// checked before they become outbound targets. Non-canonical numeric forms are
// left to the dial-time guard (providerDialControl), which sees the resolved IP.
func TestValidateBaseURL(t *testing.T) {
	cases := []struct {
		name        string
		raw         string
		allowUnsafe bool
		wantErr     bool
	}{
		{"empty means the public host", "", false, false},
		{"https public host", "https://api.github.com", false, false},
		{"http self-hosted host", "http://gitlab.corp.example", false, false},
		{"private range stays allowed", "https://10.0.0.5", false, false},
		{"loopback rejected", "http://127.0.0.1:3000", false, true},
		{"ipv6 loopback rejected", "http://[::1]:3000", false, true},
		{"link-local metadata rejected", "http://169.254.169.254/latest/meta-data", false, true},
		{"ipv6 link-local rejected", "http://[fe80::1]", false, true},
		{"unspecified rejected", "http://0.0.0.0", false, true},
		{"metadata hostname rejected", "http://metadata.google.internal", false, true},
		{"localhost rejected", "http://localhost:3000", false, true},
		{"localhost trailing dot rejected", "http://localhost.:3000", false, true},
		{"localhost subdomain rejected", "http://foo.localhost", false, true},
		{"cgnat metadata rejected", "http://100.100.100.200", false, true},
		{"cgnat range rejected", "http://100.64.0.1", false, true},
		{"ula metadata rejected", "http://[fd00:ec2::254]", false, true},
		{"ula range rejected", "http://[fd12::1]", false, true},
		{"userinfo rejected", "https://user:pass@gitea.example", false, true},
		{"non-http scheme rejected", "ftp://gitea.example", false, true},
		{"relative rejected", "gitea.example", false, true},
		{"loopback allowed when opted in", "http://127.0.0.1:3000", true, false},
		{"localhost allowed when opted in", "http://localhost:3000", true, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateBaseURL(tc.raw, tc.allowUnsafe)
			if tc.wantErr != (err != nil) {
				t.Fatalf("validateBaseURL(%q, %v) = %v, wantErr %v", tc.raw, tc.allowUnsafe, err, tc.wantErr)
			}
			if tc.wantErr && !errors.Is(err, ErrValidation) {
				t.Fatalf("error = %v, want ErrValidation", err)
			}
		})
	}
}

// TestProviderDialControl is the U2 regression: the resolved IP is refused even
// when validation only saw a name (covers DNS rebinding and non-canonical
// numeric forms such as 2130706433 / 0x7f.0.0.1 / 127.1, which resolve here).
func TestProviderDialControl(t *testing.T) {
	control := providerDialControl(false)
	cases := []struct {
		addr    string
		wantErr bool
	}{
		{"127.0.0.1:80", true},
		{"[::1]:80", true},
		{"169.254.169.254:80", true},
		{"100.100.100.200:80", true},
		{"[fd00:ec2::254]:80", true},
		{"10.0.0.5:80", false},
		{"192.168.1.10:443", false},
		{"8.8.8.8:443", false},
	}
	for _, tc := range cases {
		err := control("tcp", tc.addr, nil)
		if tc.wantErr != (err != nil) {
			t.Fatalf("control(%q) = %v, wantErr %v", tc.addr, err, tc.wantErr)
		}
	}
	if err := providerDialControl(true)("tcp", "127.0.0.1:80", nil); err != nil {
		t.Fatalf("allowUnsafe control: %v", err)
	}
}

// TestCheckProviderRedirect is the U1 regression: cross-origin and unsafe
// redirects are refused; same-origin redirects keep working.
func TestCheckProviderRedirect(t *testing.T) {
	check := checkProviderRedirect(false)
	origin := mustURL(t, "https://gitlab.example/api/v4/projects")

	cases := []struct {
		name    string
		reqURL  string
		via     int
		wantErr bool
	}{
		{"same origin allowed", "https://gitlab.example/api/v4/projects?page=2", 1, false},
		{"cross host refused", "https://evil.example/steal", 1, true},
		{"cross scheme refused", "http://gitlab.example/steal", 1, true},
		{"loopback refused", "http://127.0.0.1:3000/steal", 1, true},
		{"metadata refused", "http://169.254.169.254/latest/meta-data", 1, true},
		{"too many redirects", "https://gitlab.example/", providerRedirectLimit, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			via := make([]*http.Request, tc.via)
			for i := range via {
				via[i] = &http.Request{URL: origin}
			}
			req := &http.Request{URL: mustURL(t, tc.reqURL)}
			err := check(req, via)
			if tc.wantErr != (err != nil) {
				t.Fatalf("check(%q) = %v, wantErr %v", tc.reqURL, err, tc.wantErr)
			}
		})
	}
}

// TestProviderRedirectRefusedEndToEnd drives 302 and 307 through a real client
// and proves the cross-origin target is never reached (so a 307 cannot replay
// its body to another origin).
func TestProviderRedirectRefusedEndToEnd(t *testing.T) {
	for _, status := range []int{http.StatusFound, http.StatusTemporaryRedirect} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			var targetHits int32
			target := serve(t, func(w http.ResponseWriter, _ *http.Request) {
				atomic.AddInt32(&targetHits, 1)
				w.WriteHeader(http.StatusOK)
			})
			origin := serve(t, func(w http.ResponseWriter, r *http.Request) {
				http.Redirect(w, r, target.URL+"/steal", status)
			})

			client := &http.Client{CheckRedirect: checkProviderRedirect(false)}
			req, err := http.NewRequest(http.MethodPost, origin.URL, strings.NewReader("secret-body"))
			if err != nil {
				t.Fatalf("new request: %v", err)
			}
			if _, err := client.Do(req); err == nil {
				t.Fatal("cross-origin redirect was followed")
			}
			if atomic.LoadInt32(&targetHits) != 0 {
				t.Fatal("the redirect target was reached")
			}
		})
	}
}

// TestProviderRedirectSameOriginAllowed proves a same-origin redirect still
// works.
func TestProviderRedirectSameOriginAllowed(t *testing.T) {
	var hits int32
	srv := serve(t, func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		if r.URL.Path == "/start" {
			http.Redirect(w, r, "/final", http.StatusFound)
			return
		}
		w.WriteHeader(http.StatusOK)
	})

	client := &http.Client{CheckRedirect: checkProviderRedirect(true)}
	resp, err := client.Get(srv.URL + "/start")
	if err != nil {
		t.Fatalf("same-origin redirect: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if got := atomic.LoadInt32(&hits); got != 2 {
		t.Fatalf("requests = %d, want 2", got)
	}
}

// TestProviderClientRefusesCrossOriginRedirect proves the guard is wired into
// the client the sources actually use, not just the helper.
func TestProviderClientRefusesCrossOriginRedirect(t *testing.T) {
	var targetHits int32
	target := serve(t, func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&targetHits, 1)
		w.WriteHeader(http.StatusOK)
	})
	origin := serve(t, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+"/steal", http.StatusFound)
	})

	source := newGitHubSource(Provider{BaseURL: origin.URL}, true)
	if _, err := source.ListRepos(context.Background(), staticToken); err == nil {
		t.Fatal("the provider client followed a cross-origin redirect")
	}
	if atomic.LoadInt32(&targetHits) != 0 {
		t.Fatal("the redirect target was reached")
	}
}

// TestProviderDialGuardRefusesLoopbackSource proves the dial guard is wired
// into the transport: a strict source cannot reach a loopback provider even
// when base_url validation is bypassed.
func TestProviderDialGuardRefusesLoopbackSource(t *testing.T) {
	var requests int32
	srv := serve(t, func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&requests, 1)
		w.WriteHeader(http.StatusOK)
	})

	source := newGitHubSource(Provider{BaseURL: srv.URL}, false) // strict
	if _, err := source.ListRepos(context.Background(), staticToken); err == nil {
		t.Fatal("a strict source reached a loopback provider")
	}
	if got := atomic.LoadInt32(&requests); got != 0 {
		t.Fatalf("the loopback provider was reached %d times, want 0", got)
	}
}

// TestProviderHTTPTimeout is the C1-14 regression: a hung provider call is
// bounded by the shared timeout, and the server sees exactly one request.
func TestProviderHTTPTimeout(t *testing.T) {
	old := providerHTTPTimeout
	providerHTTPTimeout = 50 * time.Millisecond
	t.Cleanup(func() { providerHTTPTimeout = old })

	var requests int32
	srv := serve(t, func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requests, 1)
		select {
		case <-r.Context().Done():
		case <-time.After(3 * time.Second):
		}
	})
	defer srv.CloseClientConnections()

	source := newGitHubSource(Provider{BaseURL: srv.URL}, true)
	start := time.Now()
	if _, err := source.ListRepos(context.Background(), staticToken); err == nil {
		t.Fatal("ListRepos against a hung provider: no error, want a timeout")
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("hung call took %v, want it bounded by the timeout", elapsed)
	}
	if got := atomic.LoadInt32(&requests); got != 1 {
		t.Fatalf("requests = %d, want 1", got)
	}
}

// TestProviderHTTPHonorsCallerDeadline proves the timeout wraps the caller's
// context rather than replacing it.
func TestProviderHTTPHonorsCallerDeadline(t *testing.T) {
	old := providerHTTPTimeout
	providerHTTPTimeout = time.Hour
	t.Cleanup(func() { providerHTTPTimeout = old })

	srv := serve(t, func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(3 * time.Second):
		}
	})
	defer srv.CloseClientConnections()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()

	source := newGitHubSource(Provider{BaseURL: srv.URL}, true)
	start := time.Now()
	if _, err := source.ListRepos(ctx, staticToken); err == nil {
		t.Fatal("ListRepos with an expired caller context: no error, want a timeout")
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("call took %v, want it bounded by the caller deadline", elapsed)
	}
}

// TestProviderRefreshBounded is the U3 regression: an oauth2 refresh inside the
// transport against a hanging token endpoint is bounded, even with no caller
// deadline.
func TestProviderRefreshBounded(t *testing.T) {
	old := providerHTTPTimeout
	providerHTTPTimeout = 80 * time.Millisecond
	t.Cleanup(func() { providerHTTPTimeout = old })

	var tokenHits int32
	tokenSrv := serve(t, func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&tokenHits, 1)
		select {
		case <-r.Context().Done():
		case <-time.After(3 * time.Second):
		}
	})
	defer tokenSrv.CloseClientConnections()
	apiSrv := serve(t, func(w http.ResponseWriter, _ *http.Request) {
		writeJSONTest(t, w, []map[string]any{})
	})

	source := newGitHubSource(Provider{BaseURL: apiSrv.URL, ClientID: "id", ClientSecret: "s"}, true)
	source.config.Endpoint.TokenURL = tokenSrv.URL

	expired := &oauth2.Token{
		AccessToken:  "old",
		RefreshToken: "refresh",
		TokenType:    "Bearer",
		Expiry:       time.Now().Add(-time.Hour),
	}
	start := time.Now()
	if _, err := source.ListRepos(context.Background(), expired); err == nil {
		t.Fatal("ListRepos with a hanging token endpoint: no error, want a timeout")
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("refresh call took %v, want it bounded by the timeout", elapsed)
	}
	if got := atomic.LoadInt32(&tokenHits); got == 0 {
		t.Fatal("the token endpoint was never hit")
	}
}

// TestProviderListingDeadline is the U6 regression: a listing that drip-feeds
// full pages is bounded by the overall listing deadline, not just the per-call
// timeout.
func TestProviderListingDeadline(t *testing.T) {
	old := providerListingTimeout
	providerListingTimeout = 80 * time.Millisecond
	t.Cleanup(func() { providerListingTimeout = old })

	var requests int32
	srv := serve(t, func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requests, 1)
		// A full page every time, but slow enough that the overall deadline
		// fires after the first few.
		select {
		case <-time.After(40 * time.Millisecond):
		case <-r.Context().Done():
			return
		}
		batch := make([]map[string]any, 0, gitHubPageSize)
		for i := 0; i < gitHubPageSize; i++ {
			batch = append(batch, map[string]any{"id": i, "name": "r", "full_name": "o/r"})
		}
		writeJSONTest(t, w, batch)
	})
	defer srv.CloseClientConnections()

	source := newGitHubSource(Provider{BaseURL: srv.URL}, true)
	start := time.Now()
	if _, err := source.ListRepos(context.Background(), staticToken); err == nil {
		t.Fatal("ListRepos with a slow provider: no error, want the listing deadline")
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("listing took %v, want it bounded by the listing deadline", elapsed)
	}
	if got := atomic.LoadInt32(&requests); got == 0 {
		t.Fatal("the provider was never called")
	}
}
