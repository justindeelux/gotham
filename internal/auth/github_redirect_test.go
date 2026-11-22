package auth

import (
	"net/url"
	"strings"
	"testing"
)

func TestResolveOAuthRedirectURL(t *testing.T) {
	for _, tc := range []struct {
		name string
		base string
		want string
	}{
		{"derives from control plane", "https://cp.example", "https://cp.example/api/v1/auth/oauth/github/callback"},
		{"trims base slash", "https://cp.example/", "https://cp.example/api/v1/auth/oauth/github/callback"},
		{"empty keeps behavior", "", ""},
		{"blank is empty", "  ", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := resolveOAuthRedirectURL(tc.base, "github"); got != tc.want {
				t.Errorf("resolveOAuthRedirectURL(%q, github) = %q, want %q", tc.base, got, tc.want)
			}
		})
	}

	if got := oauthCallbackPath("google"); got != "/api/v1/auth/oauth/google/callback" {
		t.Errorf("oauthCallbackPath(google) = %q", got)
	}
}

func TestGitHubProviderControlPlaneSource(t *testing.T) {
	derived, ok := NewGitHubProvider("id", "secret").(*GitHubProvider)
	if !ok {
		t.Fatal("NewGitHubProvider did not return a *GitHubProvider")
	}
	derived.SetControlPlaneURLSource(func() string { return "https://cp.example" })
	if authURL := derived.AuthCodeURL("s"); !strings.Contains(authURL, "redirect_uri="+url.QueryEscape(resolveOAuthRedirectURL("https://cp.example", "github"))) {
		t.Errorf("AuthCodeURL = %q, want the derived redirect_uri", authURL)
	}

	unset, ok := NewGitHubProvider("id", "secret").(*GitHubProvider)
	if !ok {
		t.Fatal("NewGitHubProvider did not return a *GitHubProvider")
	}
	unset.SetControlPlaneURLSource(func() string { return "" })
	if authURL := unset.AuthCodeURL("s"); strings.Contains(authURL, "redirect_uri=") {
		t.Errorf("AuthCodeURL = %q, want no redirect_uri when unset", authURL)
	}
}

func TestOAuthServiceControlPlaneSource(t *testing.T) {
	provider, ok := NewGitHubProvider("id", "secret").(*GitHubProvider)
	if !ok {
		t.Fatal("NewGitHubProvider did not return a *GitHubProvider")
	}
	svc := NewOAuthService(nil, nil, nil, provider)
	svc.SetControlPlaneURLSource(func() string { return "https://cp.example" })
	if authURL := provider.AuthCodeURL("s"); !strings.Contains(authURL, "redirect_uri="+url.QueryEscape(resolveOAuthRedirectURL("https://cp.example", "github"))) {
		t.Errorf("AuthCodeURL = %q, want the derived redirect_uri", authURL)
	}
}
