package auth

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"golang.org/x/oauth2"
)

const testGoogleAvatar = "https://lh3.googleusercontent.com/a/test-avatar=s96-c"

func TestNewGoogleProviderDisabled(t *testing.T) {
	if p := NewGoogleProvider("", "secret"); p != nil {
		t.Errorf("NewGoogleProvider(no client id) = %v, want nil", p)
	}
	if p := NewGoogleProvider("id", ""); p != nil {
		t.Errorf("NewGoogleProvider(no secret) = %v, want nil", p)
	}
}

func TestGoogleProviderNameAndAuthCodeURL(t *testing.T) {
	provider := newTestGoogleProvider(t, "https://api.example")

	if provider.Name() != "google" {
		t.Errorf("Name = %q, want google", provider.Name())
	}

	authURL := provider.AuthCodeURL("the-state")
	if !strings.HasPrefix(authURL, googleAuthURL+"?") {
		t.Fatalf("AuthCodeURL = %q, want the Google authorize endpoint", authURL)
	}
	parsed, err := url.Parse(authURL)
	if err != nil {
		t.Fatalf("parse AuthCodeURL: %v", err)
	}
	query := parsed.Query()
	if got := query.Get("state"); got != "the-state" {
		t.Errorf("state = %q, want the-state", got)
	}
	if got := query.Get("client_id"); got != "client-id" {
		t.Errorf("client_id = %q, want client-id", got)
	}
	if got := query.Get("scope"); got != "openid email profile" {
		t.Errorf("scope = %q, want openid email profile", got)
	}
}

func TestGoogleProviderRedirectURL(t *testing.T) {
	provider := newTestGoogleProvider(t, "https://api.example")

	provider.SetControlPlaneURLSource(func() string { return "https://cp.example" })
	want := "https://cp.example/api/v1/auth/oauth/google/callback"
	if authURL := provider.AuthCodeURL("s"); !strings.Contains(authURL, "redirect_uri="+url.QueryEscape(want)) {
		t.Errorf("AuthCodeURL = %q, want the derived redirect_uri", authURL)
	}

	provider.SetControlPlaneURLSource(func() string { return "" })
	if authURL := provider.AuthCodeURL("s"); strings.Contains(authURL, "redirect_uri=") {
		t.Errorf("AuthCodeURL = %q, want no redirect_uri when unset", authURL)
	}
}

func TestGoogleProviderExchangeAndIdentity(t *testing.T) {
	srv := newGoogleTestServer(t, `{
		"email": "page@example.com",
		"email_verified": true,
		"name": "Page Owner",
		"picture": "`+testGoogleAvatar+`"
	}`)
	defer srv.Close()

	provider := newTestGoogleProvider(t, srv.URL)
	ctx := context.Background()

	tok, err := provider.Exchange(ctx, "the-code")
	if err != nil {
		t.Fatalf("Exchange: %v", err)
	}
	if tok.AccessToken != "google-access-token" {
		t.Fatalf("Exchange token = %q, want google-access-token", tok.AccessToken)
	}

	identity, err := provider.Identity(ctx, tok)
	if err != nil {
		t.Fatalf("Identity: %v", err)
	}
	if identity.Email != "page@example.com" {
		t.Errorf("Email = %q, want page@example.com", identity.Email)
	}
	if identity.Name != "Page Owner" {
		t.Errorf("Name = %q, want Page Owner", identity.Name)
	}
	if identity.AvatarURL != testGoogleAvatar {
		t.Errorf("AvatarURL = %q, want %q", identity.AvatarURL, testGoogleAvatar)
	}
	if got, ok := SanitizeAvatarURL(identity.AvatarURL); !ok || got != testGoogleAvatar {
		t.Errorf("SanitizeAvatarURL(google avatar) = (%q, %v), want it allowed", got, ok)
	}
}

func TestGoogleProviderIdentityRejectsUnverifiedEmail(t *testing.T) {
	for name, body := range map[string]string{
		"unverified":      `{"email":"victim@example.com","email_verified":false}`,
		"missing flag":    `{"email":"victim@example.com"}`,
		"empty email":     `{"email":"","email_verified":true}`,
		"whitespace mail": `{"email":"  ","email_verified":true}`,
	} {
		t.Run(name, func(t *testing.T) {
			srv := newGoogleTestServer(t, body)
			defer srv.Close()

			provider := newTestGoogleProvider(t, srv.URL)
			_, err := provider.Identity(context.Background(), &oauth2.Token{AccessToken: "google-access-token"})
			if !errors.Is(err, ErrMissingEmail) {
				t.Fatalf("Identity error = %v, want ErrMissingEmail", err)
			}
		})
	}
}

func TestGoogleProviderIdentityWithoutToken(t *testing.T) {
	provider := newTestGoogleProvider(t, "https://api.example")

	if _, err := provider.Identity(context.Background(), nil); !errors.Is(err, ErrMissingEmail) {
		t.Fatalf("Identity(nil token) error = %v, want ErrMissingEmail", err)
	}
}

func TestGoogleProviderIdentityRejectsUnusableStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusUnauthorized)
	}))
	defer srv.Close()

	provider := newTestGoogleProvider(t, srv.URL)
	_, err := provider.Identity(context.Background(), &oauth2.Token{AccessToken: "google-access-token"})
	if err == nil || errors.Is(err, ErrMissingEmail) {
		t.Fatalf("Identity error = %v, want the request failure", err)
	}
}

// TestGoogleLoginResolvesGitHubCreatedAccount covers the case where someone
// signed up with GitHub and later signs in with Google using the same address.
func TestGoogleLoginResolvesGitHubCreatedAccount(t *testing.T) {
	email := uniqueEmail("oauth-google-same")
	srv := newGoogleTestServer(t, `{
		"email": "`+email+`",
		"email_verified": true,
		"name": "Same Person",
		"picture": "`+testGoogleAvatar+`"
	}`)
	defer srv.Close()

	provider := newTestGoogleProvider(t, srv.URL)
	oauth, st := newTestOAuthWithStore(t, provider)
	ctx := context.Background()
	cleanupUser(t, st, email)

	existing, err := st.CreateUser(ctx, email, nil)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	_, state, err := oauth.Begin(ctx, "google", "https://cp.example")
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	result, err := oauth.Callback(ctx, "google", "the-code", state, SessionMeta{})
	if err != nil {
		t.Fatalf("Callback: %v", err)
	}
	if result.User.ID != existing.ID.String() {
		t.Errorf("Callback user = %s, want the existing account %s", result.User.ID, existing.ID)
	}
	if result.User.Avatar == nil || *result.User.Avatar != testGoogleAvatar {
		t.Errorf("Callback user avatar = %v, want %q", result.User.Avatar, testGoogleAvatar)
	}
}

func newGoogleTestServer(t *testing.T, userInfo string) *httptest.Server {
	t.Helper()

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/token":
			_ = r.ParseForm()
			if got := r.Form.Get("code"); got != "the-code" {
				t.Errorf("token code = %q, want the-code", got)
			}
			_, _ = w.Write([]byte(`{"access_token":"google-access-token","token_type":"Bearer","expires_in":3600}`))
		case "/userinfo":
			if got := r.Header.Get("Authorization"); got != "Bearer google-access-token" {
				t.Errorf("userinfo Authorization = %q, want the exchanged token", got)
			}
			_, _ = w.Write([]byte(userInfo))
		default:
			http.NotFound(w, r)
		}
	}))
}

func newTestGoogleProvider(t *testing.T, base string) *GoogleProvider {
	t.Helper()

	provider, ok := NewGoogleProvider("client-id", "client-secret").(*GoogleProvider)
	if !ok {
		t.Fatal("NewGoogleProvider did not return a *GoogleProvider")
	}
	provider.config.Endpoint.TokenURL = strings.TrimRight(base, "/") + "/token"
	provider.userInfoURL = strings.TrimRight(base, "/") + "/userinfo"
	return provider
}
