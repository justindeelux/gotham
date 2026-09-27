package providers

import (
	"context"
	"fmt"
	"strings"

	"golang.org/x/oauth2"
)

// GitLab defaults. A stored base_url selects a self-hosted instance; the API
// lives under /api/v4 and OAuth under /oauth on the same host.
const (
	gitLabDefaultBase = "https://gitlab.com"
	gitLabAPIBase     = "https://gitlab.com/api/v4"

	// gitLabDefaultScopes grants API access for repo listing and webhooks.
	gitLabDefaultScopes = "api"

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
	config  *oauth2.Config
	apiBase string
}

// newGitLabSource builds the GitLab implementation for one stored connection.
func newGitLabSource(p Provider) *gitLabSource {
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
		config: &oauth2.Config{
			ClientID:     p.ClientID,
			ClientSecret: p.ClientSecret,
			RedirectURL:  p.RedirectURL,
			Scopes:       splitScopes(p.Scopes, gitLabDefaultScopes),
			Endpoint:     oauth2.Endpoint{AuthURL: authURL, TokenURL: tokenURL},
		},
		apiBase: apiBase,
	}
}

// Name identifies the provider.
func (p *gitLabSource) Name() string { return NameGitLab }

// ExchangeToken completes the OAuth2 flow with GitLab.
func (p *gitLabSource) ExchangeToken(ctx context.Context, code string) (*oauth2.Token, error) {
	return p.config.Exchange(ctx, code)
}

// ListRepos returns every project visible to the token, including private ones.
func (p *gitLabSource) ListRepos(ctx context.Context, tok *oauth2.Token) ([]Repo, error) {
	client := p.config.Client(ctx, tok)
	repos := make([]Repo, 0)
	for page := 1; ; page++ {
		endpoint := fmt.Sprintf(
			"%s/projects?per_page=%d&page=%d&order_by=last_activity_at&sort=desc",
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
}

// ListBranches returns the branches of repo ("group/project", nested allowed).
func (p *gitLabSource) ListBranches(ctx context.Context, tok *oauth2.Token, repo string) ([]Branch, error) {
	if err := validateRepo(repo, 2); err != nil {
		return nil, err
	}
	client := p.config.Client(ctx, tok)
	branches := make([]Branch, 0)
	for page := 1; ; page++ {
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
}

// CreateWebhook is deferred to BE-4.4.
func (p *gitLabSource) CreateWebhook(context.Context, *oauth2.Token, string, Webhook) error {
	return fmt.Errorf("%w: %s", ErrNotWired, NameGitLab)
}
