package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"golang.org/x/oauth2"
)

const (
	gitHubAuthURL    = "https://github.com/login/oauth/authorize"
	gitHubTokenURL   = "https://github.com/login/oauth/access_token"
	gitHubAPIBase    = "https://api.github.com"
	gitHubEmailScope = "user:email"

	maxGitHubBodyBytes = 1 << 20
)

type gitHubUser struct {
	Name      string `json:"name"`
	AvatarURL string `json:"avatar_url"`
}

type gitHubEmail struct {
	Email    string `json:"email"`
	Primary  bool   `json:"primary"`
	Verified bool   `json:"verified"`
}

const GitHubCallbackPath = "/api/v1/auth/oauth/github/callback"

func ResolveGitHubRedirectURL(explicit, controlPlaneBase string) string {
	if v := strings.TrimSpace(explicit); v != "" {
		return v
	}
	if base := strings.TrimSpace(controlPlaneBase); base != "" {
		return strings.TrimRight(base, "/") + GitHubCallbackPath
	}
	return ""
}

type GitHubProvider struct {
	config           *oauth2.Config
	apiBase          string
	explicitRedirect string
	controlPlaneBase func() string
}

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
		apiBase:          gitHubAPIBase,
		explicitRedirect: redirectURL,
	}
}

func (p *GitHubProvider) SetControlPlaneURLSource(src func() string) {
	p.controlPlaneBase = src
}

func (p *GitHubProvider) resolved() *oauth2.Config {
	if p.controlPlaneBase == nil {
		return p.config
	}
	cfg := *p.config
	cfg.RedirectURL = ResolveGitHubRedirectURL(p.explicitRedirect, p.controlPlaneBase())
	return &cfg
}

func (p *GitHubProvider) Name() string { return "github" }

func (p *GitHubProvider) AuthCodeURL(state string) string {
	return p.resolved().AuthCodeURL(state)
}

func (p *GitHubProvider) Exchange(ctx context.Context, code string) (*oauth2.Token, error) {
	return p.resolved().Exchange(ctx, code)
}

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
