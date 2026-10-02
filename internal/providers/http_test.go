package providers

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"
)

// TestValidateBaseURL is the C1-15 regression: stored base URLs are checked
// before they become outbound targets.
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
		{"userinfo rejected", "https://user:pass@gitea.example", false, true},
		{"non-http scheme rejected", "ftp://gitea.example", false, true},
		{"relative rejected", "gitea.example", false, true},
		{"loopback allowed when opted in", "http://127.0.0.1:3000", true, false},
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

// TestProviderHTTPTimeout is the C1-14 regression: a hung provider call is
// bounded by the shared timeout.
func TestProviderHTTPTimeout(t *testing.T) {
	old := providerHTTPTimeout
	providerHTTPTimeout = 50 * time.Millisecond
	t.Cleanup(func() { providerHTTPTimeout = old })

	srv := serve(t, func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	})

	source := newGitHubSource(Provider{BaseURL: srv.URL})
	start := time.Now()
	if _, err := source.ListRepos(context.Background(), staticToken); err == nil {
		t.Fatal("ListRepos against a hung provider: no error, want a timeout")
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("hung call took %v, want it bounded by the timeout", elapsed)
	}
}

// TestProviderHTTPHonorsCallerDeadline proves the timeout wraps the caller's
// context rather than replacing it.
func TestProviderHTTPHonorsCallerDeadline(t *testing.T) {
	old := providerHTTPTimeout
	providerHTTPTimeout = time.Hour
	t.Cleanup(func() { providerHTTPTimeout = old })

	srv := serve(t, func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	})

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()

	source := newGitHubSource(Provider{BaseURL: srv.URL})
	if _, err := source.ListRepos(ctx, staticToken); err == nil {
		t.Fatal("ListRepos with an expired caller context: no error, want a timeout")
	}
}
