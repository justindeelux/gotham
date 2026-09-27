package webhooks

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/justindeelux/gotham/internal/providers"
)

func TestVerifySignature(t *testing.T) {
	const secret = "hook-secret"
	body := []byte(`{"ref":"refs/heads/main"}`)

	cases := []struct {
		name     string
		provider string
		headers  func() http.Header
		want     bool
	}{
		{
			name:     "github valid",
			provider: providers.NameGitHub,
			headers: func() http.Header {
				h := http.Header{}
				h.Set(headerGitHubSignature, githubSignaturePrefix+hmacHex(secret, body))
				return h
			},
			want: true,
		},
		{
			name:     "github sha1-style header is refused",
			provider: providers.NameGitHub,
			headers: func() http.Header {
				h := http.Header{}
				h.Set(headerGitHubSignature, "sha1="+hmacHex(secret, body))
				return h
			},
			want: false,
		},
		{
			name:     "github wrong secret",
			provider: providers.NameGitHub,
			headers: func() http.Header {
				h := http.Header{}
				h.Set(headerGitHubSignature, githubSignaturePrefix+hmacHex("other", body))
				return h
			},
			want: false,
		},
		{
			name:     "gitlab valid",
			provider: providers.NameGitLab,
			headers: func() http.Header {
				h := http.Header{}
				h.Set(headerGitLabToken, secret)
				return h
			},
			want: true,
		},
		{
			name:     "gitlab wrong token",
			provider: providers.NameGitLab,
			headers: func() http.Header {
				h := http.Header{}
				h.Set(headerGitLabToken, secret+"x")
				return h
			},
			want: false,
		},
		{
			name:     "gitea valid",
			provider: providers.NameGitea,
			headers: func() http.Header {
				h := http.Header{}
				h.Set(headerGiteaSignature, hmacHex(secret, body))
				return h
			},
			want: true,
		},
		{
			name:     "gitea wrong secret",
			provider: providers.NameGitea,
			headers: func() http.Header {
				h := http.Header{}
				h.Set(headerGiteaSignature, hmacHex("other", body))
				return h
			},
			want: false,
		},
		{
			name:     "empty stored secret never verifies",
			provider: providers.NameGitHub,
			headers: func() http.Header {
				h := http.Header{}
				h.Set(headerGitHubSignature, githubSignaturePrefix+hmacHex("", body))
				return h
			},
			want: false,
		},
		{
			name:     "unknown provider",
			provider: "bitbucket",
			headers:  func() http.Header { return http.Header{} },
			want:     false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := verifySignature(tc.provider, tc.headers(), body, secret); got != tc.want {
				t.Errorf("verifySignature = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestParseDelivery(t *testing.T) {
	cases := []struct {
		name     string
		provider string
		header   map[string]string
		body     string
		wantRepo string
		wantRef  string
		wantSHA  string
		wantID   string
		wantErr  bool
	}{
		{
			name:     "github push",
			provider: providers.NameGitHub,
			header:   map[string]string{headerGitHubEvent: "Push", headerGitHubDelivery: "d-1"},
			body:     `{"ref":"refs/heads/main","after":"abc","repository":{"full_name":"o/r"}}`,
			wantRepo: "o/r", wantRef: "refs/heads/main", wantSHA: "abc", wantID: "d-1",
		},
		{
			name:     "gitlab push falls back to checkout_sha",
			provider: providers.NameGitLab,
			header:   map[string]string{headerGitLabEvent: "Push Hook", headerGitLabDelivery: "d-2"},
			body:     `{"ref":"refs/heads/main","checkout_sha":"def","project":{"path_with_namespace":"g/p"}}`,
			wantRepo: "g/p", wantRef: "refs/heads/main", wantSHA: "def", wantID: "d-2",
		},
		{
			name:     "deleted ref drops the zero sha",
			provider: providers.NameGitHub,
			header:   map[string]string{headerGitHubEvent: "push"},
			body:     `{"ref":"refs/heads/main","after":"0000000000000000000000000000000000000000","repository":{"full_name":"o/r"}}`,
			wantRepo: "o/r", wantRef: "refs/heads/main",
		},
		{
			name:     "head commit fallback",
			provider: providers.NameGitea,
			header:   map[string]string{headerGiteaEvent: "push"},
			body:     `{"ref":"refs/heads/main","repository":{"full_name":"o/r"},"head_commit":{"id":"1234"}}`,
			wantRepo: "o/r", wantRef: "refs/heads/main", wantSHA: "1234",
		},
		{
			name:     "no repository",
			provider: providers.NameGitHub,
			body:     `{"ref":"refs/heads/main"}`,
			wantErr:  true,
		},
		{
			name:     "not json",
			provider: providers.NameGitHub,
			body:     "<html>maintenance</html>",
			wantErr:  true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			header := http.Header{}
			for name, value := range tc.header {
				header.Set(name, value)
			}
			got, err := parseDelivery(tc.provider, header, []byte(tc.body))
			if tc.wantErr {
				if err == nil {
					t.Fatal("parseDelivery: no error, want failure")
				}
				return
			}
			if err != nil {
				t.Fatalf("parseDelivery: %v", err)
			}
			if got.Repository != tc.wantRepo || got.Ref != tc.wantRef ||
				got.Commit != tc.wantSHA || got.DeliveryID != tc.wantID {
				t.Errorf("delivery = %+v, want repo=%q ref=%q sha=%q id=%q",
					got, tc.wantRepo, tc.wantRef, tc.wantSHA, tc.wantID)
			}
		})
	}
}

func TestBranchOf(t *testing.T) {
	cases := map[string]string{
		"refs/heads/main":   "main",
		"refs/heads/feat/x": "feat/x",
		"refs/tags/v1":      "",
		"main":              "",
		"":                  "",
	}
	for ref, want := range cases {
		if got := branchOf(ref); got != want {
			t.Errorf("branchOf(%q) = %q, want %q", ref, got, want)
		}
	}
}

func TestReadBodyRejectsOversizedDelivery(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/v1/webhooks/github",
		strings.NewReader(strings.Repeat("a", maxBodyBytes+2)))
	if _, err := readBody(req); err == nil {
		t.Fatal("readBody: no error, want a body-too-large failure")
	}
}

func TestIsPushEvent(t *testing.T) {
	cases := []struct {
		provider string
		event    string
		want     bool
	}{
		{providers.NameGitHub, "push", true},
		{providers.NameGitHub, "ping", false},
		{providers.NameGitHub, "pull_request", false},
		{providers.NameGitLab, "push hook", true},
		{providers.NameGitLab, "tag push hook", false},
		{providers.NameGitea, "push", true},
		{providers.NameGitea, "issues", false},
	}
	for _, tc := range cases {
		if got := isPushEvent(tc.provider, tc.event); got != tc.want {
			t.Errorf("isPushEvent(%s, %q) = %v, want %v", tc.provider, tc.event, got, tc.want)
		}
	}
}
