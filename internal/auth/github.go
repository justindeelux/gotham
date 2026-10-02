package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"golang.org/x/oauth2"
)

// GitHub OAuth2 endpoints and API base. authURL/tokenURL come from GitHub's
// documented OAuth flow; apiBase is the REST API root used to read the profile.
const (
	gitHubAuthURL    = "https://github.com/login/oauth/authorize"
	gitHubTokenURL   = "https://github.com/login/oauth/access_token"
	gitHubAPIBase    = "https://api.github.com"
	gitHubEmailScope = "user:email"

	// maxGitHubBodyBytes bounds how much of a GitHub response is decoded.
	maxGitHubBodyBytes = 1 << 20 // 1 MiB
)

// gitHubUser is the subset of GET /user this provider consumes.
type gitHubUser struct {
	Name      string `json:"name"`
	AvatarURL string `json:"avatar_url"`
}

// gitHubEmail is one entry of GET /user/emails.
type gitHubEmail struct {
	Email    string `json:"email"`
	Primary  bool   `json:"primary"`
	Verified bool   `json:"verified"`
}

// GitHubProvider implements OAuthProvider against GitHub. It is safe for
// concurrent use.
type GitHubProvider struct {
	config  *oauth2.Config
	apiBase string
}

// NewGitHubProvider builds a GitHub provider. It returns nil when clientID or
// clientSecret is empty, signalling that the provider is disabled.
func NewGitHubProvider(clientID, clientSecret, redirectURL string) OAuthProvider {
	if clientID == "" || clientSecret == "" {
		return nil
	}
	return &GitHubProvider{
		config: &oauth2.Config{
			ClientID:     clientID,
			ClientSecret: clientSecret,
			RedirectURL:  redirectURL,
			Scopes:       []string{gitHubEmailScope},
			Endpoint: oauth2.Endpoint{
				AuthURL:  gitHubAuthURL,
				TokenURL: gitHubTokenURL,
			},
		},
		apiBase: gitHubAPIBase,
	}
}

// Name identifies the provider.
func (p *GitHubProvider) Name() string { return "github" }

// AuthCodeURL returns GitHub's authorization URL.
func (p *GitHubProvider) AuthCodeURL(state string) string {
	return p.config.AuthCodeURL(state)
}

// Exchange swaps the authorization code for an access token.
func (p *GitHubProvider) Exchange(ctx context.Context, code string) (*oauth2.Token, error) {
	return p.config.Exchange(ctx, code)
}

// Identity reads the user profile and email list, preferring the primary
// verified address. It returns ErrMissingEmail when GitHub exposes no verified
// address.
func (p *GitHubProvider) Identity(ctx context.Context, tok *oauth2.Token) (*OAuthIdentity, error) {
	if tok == nil {
		return nil, ErrMissingEmail
	}

	client := p.config.Client(ctx, tok)

	var profile gitHubUser
	if err := p.getJSON(ctx, client, "/user", &profile); err != nil {
		return nil, err
	}

	email, err := p.primaryEmail(ctx, client)
	if err != nil {
		return nil, err
	}

	return &OAuthIdentity{
		Email:     email,
		Name:      profile.Name,
		AvatarURL: profile.AvatarURL,
	}, nil
}

// primaryEmail fetches GET /user/emails and picks the primary verified address,
// falling back to the first verified address. An address GitHub has not
// verified is never accepted: a user may mark an address primary before
// verifying it, so trusting it would let an attacker claim someone else's email
// (and account) through OAuth.
func (p *GitHubProvider) primaryEmail(ctx context.Context, client *http.Client) (string, error) {
	var emails []gitHubEmail
	if err := p.getJSON(ctx, client, "/user/emails", &emails); err != nil {
		return "", err
	}

	var verifiedFallback string
	for _, email := range emails {
		if email.Email == "" {
			continue
		}
		if email.Primary {
			if email.Verified {
				return email.Email, nil
			}
			continue
		}
		if email.Verified && verifiedFallback == "" {
			verifiedFallback = email.Email
		}
	}

	if verifiedFallback != "" {
		return verifiedFallback, nil
	}
	return "", ErrMissingEmail
}

// getJSON performs an authenticated GET against the GitHub API and decodes the
// JSON body into dst.
func (p *GitHubProvider) getJSON(ctx context.Context, client *http.Client, path string, dst any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.apiBase+path, nil)
	if err != nil {
		return fmt.Errorf("auth: github request %s: %w", path, err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("auth: github request %s: %w", path, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxGitHubBodyBytes))
		return fmt.Errorf("auth: github %s: unexpected status %d", path, resp.StatusCode)
	}

	if err := json.NewDecoder(io.LimitReader(resp.Body, maxGitHubBodyBytes)).Decode(dst); err != nil {
		return fmt.Errorf("auth: github decode %s: %w", path, err)
	}
	return nil
}
