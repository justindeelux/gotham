package providers

import (
	"context"
	"fmt"
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
	config  *oauth2.Config
	apiBase string
}

// newGitHubSource builds the GitHub implementation for one stored connection.
func newGitHubSource(p Provider) *gitHubSource {
	apiBase := strings.TrimRight(strings.TrimSpace(p.BaseURL), "/")
	if apiBase == "" {
		apiBase = gitHubDefaultAPIBase
	}
	return &gitHubSource{
		config: &oauth2.Config{
			ClientID:     p.ClientID,
			ClientSecret: p.ClientSecret,
			RedirectURL:  p.RedirectURL,
			Scopes:       splitScopes(p.Scopes, gitHubDefaultScopes),
			Endpoint:     oauth2.Endpoint{AuthURL: gitHubAuthURL, TokenURL: gitHubTokenURL},
		},
		apiBase: apiBase,
	}
}

// Name identifies the provider.
func (p *gitHubSource) Name() string { return NameGitHub }

// ExchangeToken completes the OAuth2 flow with GitHub.
func (p *gitHubSource) ExchangeToken(ctx context.Context, code string) (*oauth2.Token, error) {
	return p.config.Exchange(ctx, code)
}

// ListRepos returns every repository the token can reach, including private
// ones.
func (p *gitHubSource) ListRepos(ctx context.Context, tok *oauth2.Token) ([]Repo, error) {
	client := p.config.Client(ctx, tok)
	repos := make([]Repo, 0)
	for page := 1; ; page++ {
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
}

// ListBranches returns the branches of repo ("owner/name").
func (p *gitHubSource) ListBranches(ctx context.Context, tok *oauth2.Token, repo string) ([]Branch, error) {
	if err := validateRepo(repo, 2); err != nil {
		return nil, err
	}
	client := p.config.Client(ctx, tok)
	branches := make([]Branch, 0)
	for page := 1; ; page++ {
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
}

// CreateWebhook is deferred to BE-4.4.
func (p *gitHubSource) CreateWebhook(context.Context, *oauth2.Token, string, Webhook) error {
	return fmt.Errorf("%w: %s", ErrNotWired, NameGitHub)
}
