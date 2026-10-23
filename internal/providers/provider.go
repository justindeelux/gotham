package providers

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"golang.org/x/oauth2"
)

const (
	NameGitHub = "github"
	NameGitLab = "gitlab"
	NameGitea  = "gitea"
)

var (
	ErrNotFound        = errors.New("providers: provider not found")
	ErrNotConnected    = errors.New("providers: provider is not connected")
	ErrUnsupported     = errors.New("providers: unsupported provider")
	ErrValidation      = errors.New("providers: validation failed")
	ErrTooManyRequests = errors.New("providers: too many pending requests")
	ErrConflict        = errors.New("providers: provider already exists")
	ErrInUse           = errors.New("providers: provider is still in use")
)

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

type Branch struct {
	Name      string
	Commit    string
	Protected bool
}

type Webhook struct {
	URL    string
	Secret string
	Events []string
}

type DeployKey struct {
	Title string
	Key   string
}

type HookTarget struct {
	UserID   uuid.UUID
	Provider string
	CloneURL string
	Repo     string
}

type SourceProvider interface {
	Name() string
	ExchangeToken(ctx context.Context, code string) (*oauth2.Token, error)
	ListRepos(ctx context.Context, tok *oauth2.Token) ([]Repo, error)
	ListBranches(ctx context.Context, tok *oauth2.Token, repo string) ([]Branch, error)
	CreateWebhook(ctx context.Context, tok *oauth2.Token, repo string, hook Webhook) (string, error)
	DeleteWebhook(ctx context.Context, tok *oauth2.Token, repo, hookID string) error
	CreatePullRequestComment(ctx context.Context, tok *oauth2.Token, repo string, number int, body string) error
	AddDeployKey(ctx context.Context, tok *oauth2.Token, repo string, key DeployKey) (string, error)
	RemoveDeployKey(ctx context.Context, tok *oauth2.Token, repo, keyID string) error
}

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

func (p Provider) Connected() bool {
	return strings.TrimSpace(p.AccessToken) != ""
}

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

type listingState struct{ truncated bool }

func (l *listingState) Truncated() bool { return l.truncated }

type tokenTracking struct {
	config      *oauth2.Config
	allowUnsafe bool
	source      *trackingSource
}

type trackingSource struct {
	inner  oauth2.TokenSource
	latest *oauth2.Token
}

func (s *trackingSource) Token() (*oauth2.Token, error) {
	tok, err := s.inner.Token()
	if err != nil {
		return nil, err
	}
	s.latest = tok
	return tok, nil
}

func (t *tokenTracking) client(ctx context.Context, tok *oauth2.Token) *http.Client {
	ctx = providerHTTPContext(ctx, t.allowUnsafe)
	t.source = &trackingSource{inner: t.config.TokenSource(ctx, tok)}
	client := oauth2.NewClient(ctx, t.source)
	client.CheckRedirect = checkProviderRedirect(t.allowUnsafe)
	client.Timeout = providerHTTPTimeout
	return client
}

func (t *tokenTracking) exchangeContext(ctx context.Context) context.Context {
	return providerHTTPContext(ctx, t.allowUnsafe)
}

func (t *tokenTracking) Token() *oauth2.Token {
	if t.source == nil {
		return nil
	}
	return t.source.latest
}
