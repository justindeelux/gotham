package providers

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"golang.org/x/oauth2"
)

// Gitea constants. Gitea is self-hosted, so a stored base_url is required; the
// API lives under /api/v1 and OAuth under /login/oauth on the same host.
const (
	// giteaDefaultScopes grants repository access for listing and webhooks.
	giteaDefaultScopes = "repo"

	// giteaPageSize is Gitea's maximum limit.
	giteaPageSize = 50
)

// giteaRepo is the subset of the Gitea repository object this provider uses.
type giteaRepo struct {
	ID            int64  `json:"id"`
	Name          string `json:"name"`
	FullName      string `json:"full_name"`
	Private       bool   `json:"private"`
	DefaultBranch string `json:"default_branch"`
	CloneURL      string `json:"clone_url"`
	SSHURL        string `json:"ssh_url"`
	HTMLURL       string `json:"html_url"`
}

// giteaBranch is the subset of the Gitea branch object this provider uses.
type giteaBranch struct {
	Name      string `json:"name"`
	Protected bool   `json:"protected"`
	Commit    struct {
		ID string `json:"id"`
	} `json:"commit"`
}

// giteaSource implements SourceProvider against the Gitea REST API.
type giteaSource struct {
	config  *oauth2.Config
	apiBase string
}

// newGiteaSource builds the Gitea implementation for one stored connection. The
// base URL must be set (validated by the service) because Gitea is self-hosted.
func newGiteaSource(p Provider) *giteaSource {
	base := strings.TrimRight(strings.TrimSpace(p.BaseURL), "/")
	return &giteaSource{
		config: &oauth2.Config{
			ClientID:     p.ClientID,
			ClientSecret: p.ClientSecret,
			RedirectURL:  p.RedirectURL,
			Scopes:       splitScopes(p.Scopes, giteaDefaultScopes),
			Endpoint: oauth2.Endpoint{
				AuthURL:  base + "/login/oauth/authorize",
				TokenURL: base + "/login/oauth/access_token",
			},
		},
		apiBase: base + "/api/v1",
	}
}

// Name identifies the provider.
func (p *giteaSource) Name() string { return NameGitea }

// ExchangeToken completes the OAuth2 flow with Gitea.
func (p *giteaSource) ExchangeToken(ctx context.Context, code string) (*oauth2.Token, error) {
	return p.config.Exchange(ctx, code)
}

// ListRepos returns every repository visible to the token, including private
// ones.
func (p *giteaSource) ListRepos(ctx context.Context, tok *oauth2.Token) ([]Repo, error) {
	client := p.config.Client(ctx, tok)
	repos := make([]Repo, 0)
	for page := 1; ; page++ {
		endpoint := fmt.Sprintf("%s/user/repos?limit=%d&page=%d", p.apiBase, giteaPageSize, page)

		var batch []giteaRepo
		if err := getJSON(ctx, client, NameGitea, endpoint, "application/json", &batch); err != nil {
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
		if len(batch) < giteaPageSize {
			return repos, nil
		}
	}
}

// ListBranches returns the branches of repo ("owner/name").
func (p *giteaSource) ListBranches(ctx context.Context, tok *oauth2.Token, repo string) ([]Branch, error) {
	if err := validateRepo(repo, 2); err != nil {
		return nil, err
	}
	client := p.config.Client(ctx, tok)
	branches := make([]Branch, 0)
	for page := 1; ; page++ {
		endpoint := fmt.Sprintf("%s/repos/%s/branches?limit=%d&page=%d", p.apiBase, repo, giteaPageSize, page)

		var batch []giteaBranch
		if err := getJSON(ctx, client, NameGitea, endpoint, "application/json", &batch); err != nil {
			return nil, err
		}
		for _, item := range batch {
			branches = append(branches, Branch{
				Name:      item.Name,
				Commit:    item.Commit.ID,
				Protected: item.Protected,
			})
		}
		if len(batch) < giteaPageSize {
			return branches, nil
		}
	}
}

// CreateWebhook installs a push hook on repo ("owner/name") and returns the
// hook ID the Gitea instance assigned to it.
func (p *giteaSource) CreateWebhook(ctx context.Context, tok *oauth2.Token, repo string, hook Webhook) (string, error) {
	if err := validateRepo(repo, 2); err != nil {
		return "", err
	}
	client := p.config.Client(ctx, tok)
	payload := map[string]any{
		"type":   "gitea",
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
	if err := doJSON(ctx, client, NameGitea, http.MethodPost, endpoint, "application/json", payload, &created); err != nil {
		return "", err
	}
	if created.ID <= 0 {
		return "", fmt.Errorf("providers: %s: webhook response has no id", NameGitea)
	}
	return strconv.FormatInt(created.ID, 10), nil
}

// DeleteWebhook removes the hook identified by hookID from repo. A hook that
// is already gone (404) counts as deleted so the caller stays idempotent.
func (p *giteaSource) DeleteWebhook(ctx context.Context, tok *oauth2.Token, repo, hookID string) error {
	if err := validateRepo(repo, 2); err != nil {
		return err
	}
	if err := validateHookID(hookID); err != nil {
		return err
	}
	client := p.config.Client(ctx, tok)
	endpoint := fmt.Sprintf("%s/repos/%s/hooks/%s", p.apiBase, repo, hookID)
	if err := doJSON(ctx, client, NameGitea, http.MethodDelete, endpoint, "application/json", nil, nil); err != nil {
		if isStatus(err, http.StatusNotFound) {
			return nil
		}
		return err
	}
	return nil
}
