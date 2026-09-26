package auth

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"golang.org/x/oauth2"
)

func TestNewGitHubProviderDisabled(t *testing.T) {
	if p := NewGitHubProvider("", "secret", ""); p != nil {
		t.Errorf("NewGitHubProvider(no client id) = %v, want nil", p)
	}
	if p := NewGitHubProvider("id", "", ""); p != nil {
		t.Errorf("NewGitHubProvider(no secret) = %v, want nil", p)
	}
}

func TestGitHubProviderIdentity(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/user":
			_, _ = w.Write([]byte(`{"name":"Octo Cat","avatar_url":"https://avatars.example/octo.png"}`))
		case "/user/emails":
			_, _ = w.Write([]byte(`[
				{"email":"secondary@example.com","primary":false,"verified":true},
				{"email":"primary@example.com","primary":true,"verified":true}
			]`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	provider := newTestGitHubProvider(t, srv.URL)

	identity, err := provider.Identity(context.Background(), &oauth2.Token{AccessToken: "token"})
	if err != nil {
		t.Fatalf("Identity: %v", err)
	}
	if identity.Email != "primary@example.com" {
		t.Errorf("Email = %q, want primary@example.com", identity.Email)
	}
	if identity.Name != "Octo Cat" {
		t.Errorf("Name = %q, want Octo Cat", identity.Name)
	}
	if identity.AvatarURL != "https://avatars.example/octo.png" {
		t.Errorf("AvatarURL = %q", identity.AvatarURL)
	}
}

func TestGitHubProviderIdentityVerifiedFallback(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/user" {
			_, _ = w.Write([]byte(`{}`))
			return
		}
		_, _ = w.Write([]byte(`[{"email":"verified@example.com","primary":false,"verified":true}]`))
	}))
	defer srv.Close()

	provider := newTestGitHubProvider(t, srv.URL)

	identity, err := provider.Identity(context.Background(), &oauth2.Token{AccessToken: "token"})
	if err != nil {
		t.Fatalf("Identity: %v", err)
	}
	if identity.Email != "verified@example.com" {
		t.Errorf("Email = %q, want verified@example.com", identity.Email)
	}
}

func TestGitHubProviderIdentityMissingEmail(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/user" {
			_, _ = w.Write([]byte(`{}`))
			return
		}
		_, _ = w.Write([]byte(`[]`))
	}))
	defer srv.Close()

	provider := newTestGitHubProvider(t, srv.URL)

	if _, err := provider.Identity(context.Background(), &oauth2.Token{AccessToken: "token"}); !errors.Is(err, ErrMissingEmail) {
		t.Fatalf("Identity error = %v, want ErrMissingEmail", err)
	}
}

func TestGitHubProviderNameAndAuthCodeURL(t *testing.T) {
	provider := newTestGitHubProvider(t, "https://api.example")

	if provider.Name() != "github" {
		t.Errorf("Name = %q, want github", provider.Name())
	}
	url := provider.AuthCodeURL("the-state")
	if !strings.Contains(url, "state=the-state") {
		t.Errorf("AuthCodeURL = %q, want a URL carrying the state", url)
	}
}

// newTestGitHubProvider builds a GitHub provider with its API base pointed at
// base, so tests exercise the parsing logic without touching github.com.
func newTestGitHubProvider(t *testing.T, base string) *GitHubProvider {
	t.Helper()

	provider, ok := NewGitHubProvider("client-id", "client-secret", "http://localhost/callback").(*GitHubProvider)
	if !ok {
		t.Fatal("NewGitHubProvider did not return a *GitHubProvider")
	}
	provider.apiBase = base
	return provider
}
