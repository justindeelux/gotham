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

// The endpoints from golang.org/x/oauth2/google. Importing that package would
// pull in the application-default-credentials code and its metadata module for
// two constants.
const (
	googleAuthURL     = "https://accounts.google.com/o/oauth2/v2/auth"
	googleTokenURL    = "https://oauth2.googleapis.com/token"
	googleUserInfoURL = "https://openidconnect.googleapis.com/v1/userinfo"

	maxGoogleBodyBytes = 1 << 20
)

type googleUser struct {
	Email         string `json:"email"`
	EmailVerified bool   `json:"email_verified"`
	Name          string `json:"name"`
	Picture       string `json:"picture"`
}

type GoogleProvider struct {
	config           *oauth2.Config
	userInfoURL      string
	controlPlaneBase func() string
}

func NewGoogleProvider(clientID, clientSecret string) OAuthProvider {
	if clientID == "" || clientSecret == "" {
		return nil
	}
	return &GoogleProvider{
		config: &oauth2.Config{
			ClientID:     clientID,
			ClientSecret: clientSecret,
			Scopes:       []string{"openid", "email", "profile"},
			Endpoint: oauth2.Endpoint{
				AuthURL:  googleAuthURL,
				TokenURL: googleTokenURL,
			},
		},
		userInfoURL: googleUserInfoURL,
	}
}

func (p *GoogleProvider) SetControlPlaneURLSource(src func() string) {
	p.controlPlaneBase = src
}

func (p *GoogleProvider) resolved() *oauth2.Config {
	cfg := *p.config
	if p.controlPlaneBase != nil {
		cfg.RedirectURL = resolveOAuthRedirectURL(p.controlPlaneBase(), p.Name())
	}
	return &cfg
}

func (p *GoogleProvider) Name() string { return "google" }

func (p *GoogleProvider) AuthCodeURL(state string) string {
	return p.resolved().AuthCodeURL(state)
}

func (p *GoogleProvider) Exchange(ctx context.Context, code string) (*oauth2.Token, error) {
	return p.resolved().Exchange(ctx, code)
}

// Identity reads the OpenID Connect profile. An account without a verified
// email is refused, so an unverified Google address can never take over an
// account that was created with that address.
func (p *GoogleProvider) Identity(ctx context.Context, tok *oauth2.Token) (*OAuthIdentity, error) {
	if tok == nil {
		return nil, ErrMissingEmail
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.userInfoURL, nil)
	if err != nil {
		return nil, fmt.Errorf("auth: google request: %w", err)
	}

	resp, err := p.config.Client(ctx, tok).Do(req)
	if err != nil {
		return nil, fmt.Errorf("auth: google request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxGoogleBodyBytes))
		return nil, fmt.Errorf("auth: google userinfo: unexpected status %d", resp.StatusCode)
	}

	var profile googleUser
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxGoogleBodyBytes)).Decode(&profile); err != nil {
		return nil, fmt.Errorf("auth: google decode userinfo: %w", err)
	}

	if strings.TrimSpace(profile.Email) == "" || !profile.EmailVerified {
		return nil, ErrMissingEmail
	}

	return &OAuthIdentity{
		Email:     profile.Email,
		Name:      profile.Name,
		AvatarURL: profile.Picture,
	}, nil
}
