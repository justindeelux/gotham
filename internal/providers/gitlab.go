package providers

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"golang.org/x/oauth2"
)

// GitLab defaults. A stored base_url selects a self-hosted instance; the API
// lives under /api/v4 and OAuth under /oauth on the same host.
const (
	gitLabDefaultBase = "https://gitlab.com"
	gitLabAPIBase     = "https://gitlab.com/api/v4"

	// gitLabDefaultScopes grants API access for repo listing and webhooks plus
	// the read scopes a least-privilege manual application needs.
	gitLabDefaultScopes = "api read_user read_repository"

	// gitLabPageSize is GitLab's maximum per_page.
	gitLabPageSize = 100
)

// gitLabProject is the subset of the GitLab project object this provider uses.
type gitLabProject struct {
	ID                int64   `json:"id"`
	Name              string  `json:"name"`
	PathWithNamespace string  `json:"path_with_namespace"`
	Visibility        string  `json:"visibility"`
	DefaultBranch     *string `json:"default_branch"`
	HTTPURLToRepo     string  `json:"http_url_to_repo"`
	SSHURLToRepo      string  `json:"ssh_url_to_repo"`
	WebURL            string  `json:"web_url"`
}

// gitLabBranch is the subset of the GitLab branch object this provider uses.
type gitLabBranch struct {
	Name      string `json:"name"`
	Protected bool   `json:"protected"`
	Commit    struct {
		ID string `json:"id"`
	} `json:"commit"`
}

// gitLabSource implements SourceProvider against the GitLab REST API.
type gitLabSource struct {
	tokenTracking
	listingState
	apiBase string
}

// newGitLabSource builds the GitLab implementation for one stored connection.
// allowUnsafe disables the outbound guards for the loopback hosts tests use.
func newGitLabSource(p Provider, allowUnsafe bool) *gitLabSource {
	base := strings.TrimRight(strings.TrimSpace(p.BaseURL), "/")
	apiBase := gitLabAPIBase
	authURL := gitLabDefaultBase + "/oauth/authorize"
	tokenURL := gitLabDefaultBase + "/oauth/token"
	if base != "" {
		apiBase = base + "/api/v4"
		authURL = base + "/oauth/authorize"
		tokenURL = base + "/oauth/token"
	}
	return &gitLabSource{
		tokenTracking: tokenTracking{
			config: &oauth2.Config{
				ClientID:     p.ClientID,
				ClientSecret: p.ClientSecret,
				RedirectURL:  p.RedirectURL,
				Scopes:       splitScopes(p.Scopes, gitLabDefaultScopes),
				Endpoint:     oauth2.Endpoint{AuthURL: authURL, TokenURL: tokenURL},
			},
			allowUnsafe: allowUnsafe,
		},
		apiBase: apiBase,
	}
}

// Name identifies the provider.
func (p *gitLabSource) Name() string { return NameGitLab }

// ExchangeToken completes the OAuth2 flow with GitLab.
func (p *gitLabSource) ExchangeToken(ctx context.Context, code string) (*oauth2.Token, error) {
	ctx, cancel := withProviderTimeout(p.exchangeContext(ctx))
	defer cancel()
	return p.config.Exchange(ctx, code)
}

// AuthCodeURL builds the authorization URL for state. It satisfies the
// authorizer seam used by the connect flow.
func (p *gitLabSource) AuthCodeURL(state string) string {
	return p.config.AuthCodeURL(state)
}

// AuthCodeURLWithPKCE builds the authorization URL for state with the S256
// PKCE challenge, so the code the browser receives is bound to a verifier
// the browser never sees.
func (p *gitLabSource) AuthCodeURLWithPKCE(state, challenge string) string {
	return p.config.AuthCodeURL(state,
		oauth2.SetAuthURLParam("code_challenge", challenge),
		oauth2.SetAuthURLParam("code_challenge_method", "S256"),
	)
}

// ExchangeTokenWithVerifier completes the OAuth2 flow with GitLab, proving
// possession of the PKCE verifier minted for this authorization.
func (p *gitLabSource) ExchangeTokenWithVerifier(ctx context.Context, code, verifier string) (*oauth2.Token, error) {
	if verifier == "" {
		return nil, fmt.Errorf("%w: pkce verifier is empty", ErrValidation)
	}
	ctx, cancel := withProviderTimeout(p.exchangeContext(ctx))
	defer cancel()
	return p.config.Exchange(ctx, code, oauth2.SetAuthURLParam("code_verifier", verifier))
}

// ListRepos returns the projects the token's user is a member of, including
// private ones. membership=true keeps the listing to the user's own projects
// instead of enumerating the entire instance catalogue. GitLab caps offset
// pagination at 50k projects, but maxRepoPages stops the walk first (500 pages
// × 100 = 50k), so a repository listing is bounded before GitLab can answer
// 400. A listing that hits the page bound is marked truncated.
func (p *gitLabSource) ListRepos(ctx context.Context, tok *oauth2.Token) ([]Repo, error) {
	ctx, cancel := withListingTimeout(ctx)
	defer cancel()

	client := p.client(ctx, tok)
	repos := make([]Repo, 0)
	for page := 1; page <= maxRepoPages; page++ {
		endpoint := fmt.Sprintf(
			"%s/projects?membership=true&per_page=%d&page=%d&order_by=last_activity_at&sort=desc",
			p.apiBase, gitLabPageSize, page,
		)

		var batch []gitLabProject
		if err := getJSON(ctx, client, NameGitLab, endpoint, "application/json", &batch); err != nil {
			return nil, err
		}
		for _, item := range batch {
			defaultBranch := ""
			if item.DefaultBranch != nil {
				defaultBranch = *item.DefaultBranch
			}
			repos = append(repos, Repo{
				ExternalID:    fmt.Sprintf("%d", item.ID),
				Name:          item.Name,
				FullName:      item.PathWithNamespace,
				Private:       item.Visibility != "public",
				DefaultBranch: defaultBranch,
				CloneURL:      item.HTTPURLToRepo,
				SSHURL:        item.SSHURLToRepo,
				HTMLURL:       item.WebURL,
			})
		}
		if len(batch) < gitLabPageSize {
			return repos, nil
		}
	}
	p.truncated = true
	return repos, nil
}

// ListBranches returns the branches of repo ("group/project", nested allowed).
// Pagination is bounded by maxRepoPages.
func (p *gitLabSource) ListBranches(ctx context.Context, tok *oauth2.Token, repo string) ([]Branch, error) {
	if err := validateRepo(repo, 2); err != nil {
		return nil, err
	}
	ctx, cancel := withListingTimeout(ctx)
	defer cancel()

	client := p.client(ctx, tok)
	branches := make([]Branch, 0)
	for page := 1; page <= maxRepoPages; page++ {
		endpoint := fmt.Sprintf(
			"%s/projects/%s/repository/branches?per_page=%d&page=%d",
			p.apiBase, escapeProjectPath(repo), gitLabPageSize, page,
		)

		var batch []gitLabBranch
		if err := getJSON(ctx, client, NameGitLab, endpoint, "application/json", &batch); err != nil {
			return nil, err
		}
		for _, item := range batch {
			branches = append(branches, Branch{
				Name:      item.Name,
				Commit:    item.Commit.ID,
				Protected: item.Protected,
			})
		}
		if len(batch) < gitLabPageSize {
			return branches, nil
		}
	}
	p.truncated = true
	return branches, nil
}

// CreateWebhook installs a push hook on repo ("group/project", nested allowed)
// and returns the hook ID GitLab assigned to it. GitLab takes the secret in a
// token field and selects events with booleans rather than an event list.
func (p *gitLabSource) CreateWebhook(ctx context.Context, tok *oauth2.Token, repo string, hook Webhook) (string, error) {
	if err := validateRepo(repo, 1); err != nil {
		return "", err
	}
	client := p.client(ctx, tok)
	payload := map[string]any{
		"url":         hook.URL,
		"token":       hook.Secret,
		"push_events": wantsEvent(hook.Events, "push"),
		// Merge-request events are the GitLab shape of pull_request: a
		// previews-enabled hook (events include "pull_request") must subscribe
		// to them or the installed hook never delivers an MR notification.
		"merge_requests_events":   eventSelected(hook.Events, "pull_request"),
		"enable_ssl_verification": true,
	}

	var created struct {
		ID int64 `json:"id"`
	}
	endpoint := fmt.Sprintf("%s/projects/%s/hooks", p.apiBase, escapeProjectPath(repo))
	if err := doJSON(ctx, client, NameGitLab, http.MethodPost, endpoint, "application/json", payload, &created); err != nil {
		return "", err
	}
	if created.ID <= 0 {
		return "", fmt.Errorf("providers: %s: webhook response has no id", NameGitLab)
	}
	return strconv.FormatInt(created.ID, 10), nil
}

// DeleteWebhook removes the hook identified by hookID from repo. A hook that
// is already gone (404) counts as deleted so the caller stays idempotent.
func (p *gitLabSource) DeleteWebhook(ctx context.Context, tok *oauth2.Token, repo, hookID string) error {
	if err := validateRepo(repo, 1); err != nil {
		return err
	}
	if err := validateHookID(hookID); err != nil {
		return err
	}
	client := p.client(ctx, tok)
	endpoint := fmt.Sprintf("%s/projects/%s/hooks/%s", p.apiBase, escapeProjectPath(repo), hookID)
	if err := doJSON(ctx, client, NameGitLab, http.MethodDelete, endpoint, "application/json", nil, nil); err != nil {
		if isNotFound(err) {
			return nil
		}
		return err
	}
	return nil
}

// CreatePullRequestComment posts a note on merge request number of repo
// ("group/project", nested allowed) — GitLab's name for a PR conversation
// comment.
func (p *gitLabSource) CreatePullRequestComment(ctx context.Context, tok *oauth2.Token, repo string, number int, body string) error {
	if err := validateRepo(repo, 1); err != nil {
		return err
	}
	if err := validateComment(number, body); err != nil {
		return err
	}
	client := p.client(ctx, tok)
	endpoint := fmt.Sprintf("%s/projects/%s/merge_requests/%d/notes", p.apiBase, escapeProjectPath(repo), number)
	payload := map[string]string{"body": body}
	return doJSON(ctx, client, NameGitLab, http.MethodPost, endpoint, "application/json", payload, nil)
}

// AddDeployKey registers the public key on repo ("group/project", nested
// allowed) as a project deploy key and returns the key ID GitLab assigned to
// it. The key stays read-only: GitLab defaults can_push to false.
func (p *gitLabSource) AddDeployKey(ctx context.Context, tok *oauth2.Token, repo string, key DeployKey) (string, error) {
	if err := validateRepo(repo, 1); err != nil {
		return "", err
	}
	if err := validateDeployKey(key); err != nil {
		return "", err
	}
	client := p.client(ctx, tok)
	payload := map[string]any{
		"title": key.Title,
		"key":   key.Key,
	}

	var created struct {
		ID int64 `json:"id"`
	}
	endpoint := fmt.Sprintf("%s/projects/%s/deploy_keys", p.apiBase, escapeProjectPath(repo))
	if err := doJSON(ctx, client, NameGitLab, http.MethodPost, endpoint, "application/json", payload, &created); err != nil {
		return "", err
	}
	if created.ID <= 0 {
		return "", fmt.Errorf("providers: %s: deploy key response has no id", NameGitLab)
	}
	return strconv.FormatInt(created.ID, 10), nil
}

// RemoveDeployKey removes the key identified by keyID from repo. A key that is
// already gone (404) counts as removed so the caller stays idempotent.
func (p *gitLabSource) RemoveDeployKey(ctx context.Context, tok *oauth2.Token, repo, keyID string) error {
	if err := validateRepo(repo, 1); err != nil {
		return err
	}
	if err := validateDeployKeyID(keyID); err != nil {
		return err
	}
	client := p.client(ctx, tok)
	endpoint := fmt.Sprintf("%s/projects/%s/deploy_keys/%s", p.apiBase, escapeProjectPath(repo), keyID)
	if err := doJSON(ctx, client, NameGitLab, http.MethodDelete, endpoint, "application/json", nil, nil); err != nil {
		if isNotFound(err) {
			return nil
		}
		return err
	}
	return nil
}
