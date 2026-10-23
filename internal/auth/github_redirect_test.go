package auth

import (
	"net/url"
	"strings"
	"testing"
)

func TestResolveGitHubRedirectURL(t *testing.T) {
	for _, tc := range []struct {
		name     string
		explicit string
		base     string
		want     string
	}{
		{"explicit wins", "https://login.example/cb", "https://cp.example", "https://login.example/cb"},
		{"explicit wins over unset base", "https://login.example/cb", "", "https://login.example/cb"},
		{"derives from control plane", "", "https://cp.example", "https://cp.example" + GitHubCallbackPath},
		{"trims base slash", "", "https://cp.example/", "https://cp.example" + GitHubCallbackPath},
		{"empty keeps behavior", "", "", ""},
		{"blank is empty", "  ", "  ", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := ResolveGitHubRedirectURL(tc.explicit, tc.base); got != tc.want {
				t.Errorf("ResolveGitHubRedirectURL(%q, %q) = %q, want %q", tc.explicit, tc.base, got, tc.want)
			}
		})
	}
}

func TestGitHubProviderControlPlaneSource(t *testing.T) {
	derived, ok := NewGitHubProvider("id", "secret", "").(*GitHubProvider)
	if !ok {
		t.Fatal("NewGitHubProvider did not return a *GitHubProvider")
	}
	derived.SetControlPlaneURLSource(func() string { return "https://cp.example" })
	if authURL := derived.AuthCodeURL("s"); !strings.Contains(authURL, "redirect_uri="+url.QueryEscape("https://cp.example"+GitHubCallbackPath)) {
		t.Errorf("AuthCodeURL = %q, want the derived redirect_uri", authURL)
	}

	explicit, ok := NewGitHubProvider("id", "secret", "https://login.example/cb").(*GitHubProvider)
	if !ok {
		t.Fatal("NewGitHubProvider did not return a *GitHubProvider")
	}
	explicit.SetControlPlaneURLSource(func() string { return "https://cp.example" })
	if authURL := explicit.AuthCodeURL("s"); !strings.Contains(authURL, "redirect_uri="+url.QueryEscape("https://login.example/cb")) {
		t.Errorf("AuthCodeURL = %q, want the explicit redirect_uri to win", authURL)
	}

	unset, ok := NewGitHubProvider("id", "secret", "").(*GitHubProvider)
	if !ok {
		t.Fatal("NewGitHubProvider did not return a *GitHubProvider")
	}
	unset.SetControlPlaneURLSource(func() string { return "" })
	if authURL := unset.AuthCodeURL("s"); strings.Contains(authURL, "redirect_uri=") {
		t.Errorf("AuthCodeURL = %q, want no redirect_uri when unset", authURL)
	}

	plain, ok := NewGitHubProvider("id", "secret", "https://login.example/cb").(*GitHubProvider)
	if !ok {
		t.Fatal("NewGitHubProvider did not return a *GitHubProvider")
	}
	if authURL := plain.AuthCodeURL("s"); !strings.Contains(authURL, "redirect_uri="+url.QueryEscape("https://login.example/cb")) {
		t.Errorf("AuthCodeURL = %q, want today's behavior without a source", authURL)
	}
}

func TestOAuthServiceGitHubControlPlaneSource(t *testing.T) {
	provider, ok := NewGitHubProvider("id", "secret", "").(*GitHubProvider)
	if !ok {
		t.Fatal("NewGitHubProvider did not return a *GitHubProvider")
	}
	svc := NewOAuthService(nil, nil, provider)
	svc.SetGitHubControlPlaneURLSource(func() string { return "https://cp.example" })
	if authURL := provider.AuthCodeURL("s"); !strings.Contains(authURL, "redirect_uri="+url.QueryEscape("https://cp.example"+GitHubCallbackPath)) {
		t.Errorf("AuthCodeURL = %q, want the derived redirect_uri", authURL)
	}
}
