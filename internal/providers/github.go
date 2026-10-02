package providers

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"golang.org/x/oauth2"
)

// GitHub OAuth2 endpoints and REST API root, mirroring the Phase-1 login flow.
const (
	gitHubAuthURL  = "https://github.com/login/oauth/authorize"
	gitHubTokenURL = "https://github.com/login/oauth/access_token"

	gitHubDefaultAPIBase = "https://api.github.com"
	gitHubAccept         = "application/vnd.github+json"

	// gitHubDefaultScopes grants access to public and private repositories,
	// which is what listing repos, branches and installing webhooks requires.
	gitHubDefaultScopes = "repo"

	// gitHubPageSize is GitHub's maximum per_page.
	gitHubPageSize = 100
)

// gitHubRepo is the subset of the GitHub repository object this provider uses.
type gitHubRepo struct {
	ID            int64  `json:"id"`
	Name          string `json:"name"`
	FullName      string `json:"full_name"`
	Private       bool   `json:"private"`
	DefaultBranch string `json:"default_branch"`
	CloneURL      string `json:"clone_url"`
	SSHURL        string `json:"ssh_url"`
	HTMLURL       string `json:"html_url"`
}

// gitHubBranch is the subset of the GitHub branch object this provider uses.
type gitHubBranch struct {
	Name      string `json:"name"`
	Protected bool   `json:"protected"`
	Commit    struct {
		SHA string `json:"sha"`
	} `json:"commit"`
}

// gitHubSource implements SourceProvider against the GitHub REST API.
type gitHubSource struct {
	tokenTracking
	listingState
	apiBase string
}

// newGitHubSource builds the GitHub implementation for one stored connection.
// allowUnsafe disables the outbound guards for the loopback hosts tests use.
func newGitHubSource(p Provider, allowUnsafe bool) *gitHubSource {
	apiBase := strings.TrimRight(strings.TrimSpace(p.BaseURL), "/")
	if apiBase == "" {
		apiBase = gitHubDefaultAPIBase
	}
	return &gitHubSource{
		tokenTracking: tokenTracking{
			config: &oauth2.Config{
				ClientID:     p.ClientID,
				ClientSecret: p.ClientSecret,
				RedirectURL:  p.RedirectURL,
				Scopes:       splitScopes(p.Scopes, gitHubDefaultScopes),
				Endpoint:     oauth2.Endpoint{AuthURL: gitHubAuthURL, TokenURL: gitHubTokenURL},
			},
			allowUnsafe: allowUnsafe,
		},
		apiBase: apiBase,
	}
}

// Name identifies the provider.
func (p *gitHubSource) Name() string { return NameGitHub }

// ExchangeToken completes the OAuth2 flow with GitHub.
func (p *gitHubSource) ExchangeToken(ctx context.Context, code string) (*oauth2.Token, error) {
	ctx, cancel := withProviderTimeout(ctx)
	defer cancel()
	return p.config.Exchange(ctx, code)
}

// AuthCodeURL builds the authorization URL for state. It satisfies the
// authorizer seam used by the connect flow.
func (p *gitHubSource) AuthCodeURL(state string) string {
	return p.config.AuthCodeURL(state)
}

// ListRepos returns every repository the token can reach, including private
// ones. Pagination is bounded by maxRepoPages; a listing that hits the bound is
// marked truncated so the caller keeps the previous cache.
func (p *gitHubSource) ListRepos(ctx context.Context, tok *oauth2.Token) ([]Repo, error) {
	ctx, cancel := withListingTimeout(ctx)
	defer cancel()

	client := p.client(ctx, tok)
	repos := make([]Repo, 0)
	for page := 1; page <= maxRepoPages; page++ {
		endpoint := fmt.Sprintf(
			"%s/user/repos?per_page=%d&page=%d&sort=updated&affiliation=owner,collaborator,organization_member",
			p.apiBase, gitHubPageSize, page,
		)

		var batch []gitHubRepo
		if err := getJSON(ctx, client, NameGitHub, endpoint, gitHubAccept, &batch); err != nil {
			return nil, err
		}
		for _, item := range batch {
			repos = append(repos, Repo{
				ExternalID:    strconv.FormatInt(item.ID, 10),
				Name:          item.Name,
				FullName:      item.FullName,
				Private:       item.Private,
				DefaultBranch: item.DefaultBranch,
				CloneURL:      item.CloneURL,
				SSHURL:        item.SSHURL,
				HTMLURL:       item.HTMLURL,
			})
		}
		if len(batch) < gitHubPageSize {
			return repos, nil
		}
	}
	p.truncated = true
	return repos, nil
}

// ListBranches returns the branches of repo ("owner/name"). Pagination is
// bounded by maxRepoPages.
func (p *gitHubSource) ListBranches(ctx context.Context, tok *oauth2.Token, repo string) ([]Branch, error) {
	if err := validateRepo(repo, 2); err != nil {
		return nil, err
	}
	ctx, cancel := withListingTimeout(ctx)
	defer cancel()

	client := p.client(ctx, tok)
	branches := make([]Branch, 0)
	for page := 1; page <= maxRepoPages; page++ {
		endpoint := fmt.Sprintf(
			"%s/repos/%s/branches?per_page=%d&page=%d",
			p.apiBase, repo, gitHubPageSize, page,
		)

		var batch []gitHubBranch
		if err := getJSON(ctx, client, NameGitHub, endpoint, gitHubAccept, &batch); err != nil {
			return nil, err
		}
		for _, item := range batch {
			branches = append(branches, Branch{
				Name:      item.Name,
				Commit:    item.Commit.SHA,
				Protected: item.Protected,
			})
		}
		if len(batch) < gitHubPageSize {
			return branches, nil
		}
	}
	p.truncated = true
	return branches, nil
}

// CreateWebhook installs a push hook on repo ("owner/name") and returns the
// hook ID GitHub assigned to it.
func (p *gitHubSource) CreateWebhook(ctx context.Context, tok *oauth2.Token, repo string, hook Webhook) (string, error) {
	if err := validateRepo(repo, 2); err != nil {
		return "", err
	}
	client := p.client(ctx, tok)
	payload := map[string]any{
		"name":   "web",
		"active": true,
		"events": hookEvents(hook.Events, "push"),
		"config": map[string]string{
			"url":          hook.URL,
			"content_type": "json",
			"secret":       hook.Secret,
		},
	}

	var created struct {
		ID int64 `json:"id"`
	}
	endpoint := fmt.Sprintf("%s/repos/%s/hooks", p.apiBase, repo)
	if err := doJSON(ctx, client, NameGitHub, http.MethodPost, endpoint, gitHubAccept, payload, &created); err != nil {
		return "", err
	}
	if created.ID <= 0 {
		return "", fmt.Errorf("providers: %s: webhook response has no id", NameGitHub)
	}
	return strconv.FormatInt(created.ID, 10), nil
}

// DeleteWebhook removes the hook identified by hookID from repo. A hook that
// is already gone (404) counts as deleted so the caller stays idempotent.
func (p *gitHubSource) DeleteWebhook(ctx context.Context, tok *oauth2.Token, repo, hookID string) error {
	if err := validateRepo(repo, 2); err != nil {
		return err
	}
	if err := validateHookID(hookID); err != nil {
		return err
	}
	client := p.client(ctx, tok)
	endpoint := fmt.Sprintf("%s/repos/%s/hooks/%s", p.apiBase, repo, hookID)
	if err := doJSON(ctx, client, NameGitHub, http.MethodDelete, endpoint, gitHubAccept, nil, nil); err != nil {
		if isNotFound(err) {
			return nil
		}
		return err
	}
	return nil
}

// CreatePullRequestComment posts a comment on pull request number of repo
// ("owner/name") through the issues API, which is where GitHub stores PR
// conversation.
func (p *gitHubSource) CreatePullRequestComment(ctx context.Context, tok *oauth2.Token, repo string, number int, body string) error {
	if err := validateRepo(repo, 2); err != nil {
		return err
	}
	if err := validateComment(number, body); err != nil {
		return err
	}
	client := p.client(ctx, tok)
	endpoint := fmt.Sprintf("%s/repos/%s/issues/%d/comments", p.apiBase, repo, number)
	payload := map[string]string{"body": body}
	return doJSON(ctx, client, NameGitHub, http.MethodPost, endpoint, gitHubAccept, payload, nil)
}

// AddDeployKey registers the public key on repo ("owner/name") as a read-only
// deploy key and returns the key ID GitHub assigned to it.
func (p *gitHubSource) AddDeployKey(ctx context.Context, tok *oauth2.Token, repo string, key DeployKey) (string, error) {
	if err := validateRepo(repo, 2); err != nil {
		return "", err
	}
	if err := validateDeployKey(key); err != nil {
		return "", err
	}
	client := p.client(ctx, tok)
	payload := map[string]any{
		"title":     key.Title,
		"key":       key.Key,
		"read_only": true,
	}

	var created struct {
		ID int64 `json:"id"`
	}
	endpoint := fmt.Sprintf("%s/repos/%s/keys", p.apiBase, repo)
	if err := doJSON(ctx, client, NameGitHub, http.MethodPost, endpoint, gitHubAccept, payload, &created); err != nil {
		return "", err
	}
	if created.ID <= 0 {
		return "", fmt.Errorf("providers: %s: deploy key response has no id", NameGitHub)
	}
	return strconv.FormatInt(created.ID, 10), nil
}

// RemoveDeployKey removes the key identified by keyID from repo. A key that is
// already gone (404) counts as removed so the caller stays idempotent.
func (p *gitHubSource) RemoveDeployKey(ctx context.Context, tok *oauth2.Token, repo, keyID string) error {
	if err := validateRepo(repo, 2); err != nil {
		return err
	}
	if err := validateDeployKeyID(keyID); err != nil {
		return err
	}
	client := p.client(ctx, tok)
	endpoint := fmt.Sprintf("%s/repos/%s/keys/%s", p.apiBase, repo, keyID)
	if err := doJSON(ctx, client, NameGitHub, http.MethodDelete, endpoint, gitHubAccept, nil, nil); err != nil {
		if isNotFound(err) {
			return nil
		}
		return err
	}
	return nil
}
