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
	// ErrTooManyRequests is returned when a caller has too many pending OAuth
	// authorizations, so a single account cannot exhaust the state store.
	ErrTooManyRequests = errors.New("providers: too many pending requests")
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

// Webhook is a webhook to install on a repository. Secret is the shared secret
// the Git host signs every delivery with; URL is the public control-plane
// endpoint that receives them.
type Webhook struct {
	URL    string
	Secret string
	Events []string
}

// DeployKey is a public SSH key to install on a repository so the control
// plane can clone it over SSH. Title is what the Git host shows in its key
// list; Key is one OpenSSH public-key line ("ssh-ed25519 AAAA… comment").
type DeployKey struct {
	Title string
	Key   string
}

// HookTarget names the repository a provider API call operates on (installing
// a hook, registering a deploy key), together with the caller whose stored
// connection authenticates the call. CloneURL selects between several
// connections of the same provider (two self-hosted Gitea instances, for
// example).
type HookTarget struct {
	UserID   uuid.UUID
	Provider string
	CloneURL string
	Repo     string
}

// SourceProvider is the API surface of a Git host. It mirrors the Phase-1
// OAuthProvider for the token exchange and adds source access plus the webhook
// lifecycle the auto-deploy flow needs.
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
	// CreateWebhook installs hook on repo and returns the provider's own hook
	// ID, which DeleteWebhook needs to remove it later.
	CreateWebhook(ctx context.Context, tok *oauth2.Token, repo string, hook Webhook) (string, error)
	// DeleteWebhook removes the hook identified by hookID from repo. A hook
	// that is already gone is a success, so deleting is idempotent.
	DeleteWebhook(ctx context.Context, tok *oauth2.Token, repo, hookID string) error
	// CreatePullRequestComment posts body as a comment on pull request number
	// of repo (GitLab calls it a merge request note). It is best-effort from
	// the caller's point of view: a failure is reported but never rolls back
	// the work the comment reports on.
	CreatePullRequestComment(ctx context.Context, tok *oauth2.Token, repo string, number int, body string) error
	// AddDeployKey registers the public key on repo and returns the
	// provider's own key ID, which RemoveDeployKey needs to remove it later.
	AddDeployKey(ctx context.Context, tok *oauth2.Token, repo string, key DeployKey) (string, error)
	// RemoveDeployKey removes the key identified by keyID from repo. A key
	// that is already gone is a success, so removing stays idempotent.
	RemoveDeployKey(ctx context.Context, tok *oauth2.Token, repo, keyID string) error
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

// listingState records whether a paged listing stopped at a bound rather than a
// natural end, so the service can avoid overwriting the cache with a partial
// list.
type listingState struct{ truncated bool }

// Truncated reports whether the last listing hit a page/offset bound.
func (l *listingState) Truncated() bool { return l.truncated }

// tokenTracking builds oauth2 clients and remembers the token source, so a
// refresh performed during a call can be read back and persisted. Embedding it
// promotes config on each source implementation.
type tokenTracking struct {
	config      *oauth2.Config
	allowUnsafe bool
	source      *trackingSource
}

// trackingSource wraps an oauth2.TokenSource and records the latest token it
// returned. The read-back never refreshes: a post-failure Token() must not
// trigger a second, unbounded refresh.
type trackingSource struct {
	inner  oauth2.TokenSource
	latest *oauth2.Token
}

// Token returns the wrapped source's token and records it.
func (s *trackingSource) Token() (*oauth2.Token, error) {
	tok, err := s.inner.Token()
	if err != nil {
		return nil, err
	}
	s.latest = tok
	return tok, nil
}

// client returns an oauth2 client for tok and records its token source. The
// context carries a guarded, timeout-bounded base client so an automatic token
// refresh inside the transport is bounded too.
func (t *tokenTracking) client(ctx context.Context, tok *oauth2.Token) *http.Client {
	ctx = providerHTTPContext(ctx, t.allowUnsafe)
	t.source = &trackingSource{inner: t.config.TokenSource(ctx, tok)}
	client := oauth2.NewClient(ctx, t.source)
	client.CheckRedirect = checkProviderRedirect(t.allowUnsafe)
	client.Timeout = providerHTTPTimeout
	return client
}

// exchangeContext applies the guarded base client to a token exchange.
// oauth2.Config.Exchange runs on the client carried by the context and falls
// back to http.DefaultClient otherwise, which would skip the dial/redirect
// guards entirely.
func (t *tokenTracking) exchangeContext(ctx context.Context) context.Context {
	return providerHTTPContext(ctx, t.allowUnsafe)
}

// Token returns the latest token the recorded source held, or nil when no call
// has been made. It never refreshes.
func (t *tokenTracking) Token() *oauth2.Token {
	if t.source == nil {
		return nil
	}
	return t.source.latest
}
