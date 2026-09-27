package providers

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"golang.org/x/oauth2"
)

// Provider identifiers. The value is stored in providers.name and used to
// select the SourceProvider implementation.
const (
	NameGitHub = "github"
	NameGitLab = "gitlab"
	NameGitea  = "gitea"
)

// Sentinel errors. HTTP handlers map these to status codes; anything else is an
// unexpected internal failure.
var (
	// ErrNotFound is returned when a provider is unknown or not owned by the
	// caller.
	ErrNotFound = errors.New("providers: provider not found")
	// ErrNotConnected is returned when a provider has no access token yet.
	ErrNotConnected = errors.New("providers: provider is not connected")
	// ErrUnsupported is returned for a provider name with no implementation.
	ErrUnsupported = errors.New("providers: unsupported provider")
	// ErrValidation is returned when user-supplied input fails validation.
	ErrValidation = errors.New("providers: validation failed")
	// ErrNotWired is returned by CreateWebhook until the BE-4.4 webhook wiring
	// lands. The provider API clients are real; only the webhook call is stubbed
	// so no caller mistakes it for a working integration.
	ErrNotWired = errors.New("providers: webhook creation is not wired until BE-4.4")
)

// Repo is the provider-neutral repository representation.
type Repo struct {
	ExternalID    string
	Name          string
	FullName      string
	Private       bool
	DefaultBranch string
	CloneURL      string
	SSHURL        string
	HTMLURL       string
}

// Branch is one branch of a repository.
type Branch struct {
	Name      string
	Commit    string
	Protected bool
}

// Webhook is a webhook to install on a repository.
type Webhook struct {
	URL    string
	Secret string
	Events []string
}

// SourceProvider is the API surface of a Git host. It mirrors the Phase-1
// OAuthProvider for the token exchange and adds source access. CreateWebhook is
// declared here so the interface is complete, but implementations return
// ErrNotWired until BE-4.4.
//
// repo identifies a repository in the provider's own notation: "owner/name" for
// GitHub and Gitea, "group/project" (possibly nested) for GitLab.
type SourceProvider interface {
	// Name is the stable provider identifier ("github", "gitlab", "gitea").
	Name() string
	// ExchangeToken swaps an authorization code for an access token, reusing
	// the Phase-1 OAuth token flow.
	ExchangeToken(ctx context.Context, code string) (*oauth2.Token, error)
	// ListRepos returns every repository visible to tok, including private
	// ones when the token grants access.
	ListRepos(ctx context.Context, tok *oauth2.Token) ([]Repo, error)
	// ListBranches returns the branches of repo.
	ListBranches(ctx context.Context, tok *oauth2.Token, repo string) ([]Branch, error)
	// CreateWebhook installs hook on repo.
	CreateWebhook(ctx context.Context, tok *oauth2.Token, repo string, hook Webhook) error
}

// Provider is a stored source-provider connection. ClientSecret, AccessToken
// and RefreshToken are plaintext in memory and sealed at rest by the
// repository adapter.
type Provider struct {
	ID             uuid.UUID
	UserID         uuid.UUID
	Name           string
	BaseURL        string
	ClientID       string
	ClientSecret   string
	RedirectURL    string
	AccessToken    string
	RefreshToken   string
	TokenExpiresAt *time.Time
	Scopes         string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// Connected reports whether the provider has an access token.
func (p Provider) Connected() bool {
	return strings.TrimSpace(p.AccessToken) != ""
}

// token materialises the stored credentials as an oauth2.Token for API calls.
func (p Provider) token() *oauth2.Token {
	tok := &oauth2.Token{
		AccessToken:  p.AccessToken,
		RefreshToken: p.RefreshToken,
		TokenType:    "Bearer",
	}
	if p.TokenExpiresAt != nil {
		tok.Expiry = *p.TokenExpiresAt
	}
	return tok
}
