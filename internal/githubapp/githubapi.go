package githubapp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Repo is one repository visible to an installation.
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

// ManifestConversion is the app credential set GitHub returns for a manifest
// code. WebhookSecret and PEM are sensitive and must never be logged.
type ManifestConversion struct {
	ID            int64
	Slug          string
	Name          string
	ClientID      string
	WebhookSecret string
	PEM           string
}

// InstallationToken is a short-lived token for one installation.
type InstallationToken struct {
	Token     string
	ExpiresAt time.Time
}

// GitHubAPI is the GitHub REST surface this package needs. Production code
// uses httpAPI; tests substitute a fake, so no test touches the network.
type GitHubAPI interface {
	// ExchangeManifest converts a manifest code into the app credentials.
	ExchangeManifest(ctx context.Context, code string) (ManifestConversion, error)
	// CreateInstallationToken mints a short-lived token for an installation,
	// authenticated with the app JWT.
	CreateInstallationToken(ctx context.Context, installationID int64, jwt string) (InstallationToken, error)
	// ListInstallationRepos lists the repositories of an installation.
	ListInstallationRepos(ctx context.Context, token string) ([]Repo, error)
	// ListBranches lists the branches of repo ("owner/name").
	ListBranches(ctx context.Context, token, repo string) ([]Branch, error)
}

// httpAPI implements GitHubAPI against one API base URL
// (https://api.github.com or a GitHub Enterprise /api/v3 root).
type httpAPI struct {
	apiBase     string
	client      *http.Client
	allowUnsafe bool
}

// NewHTTPAPI builds the production GitHubAPI for apiBase. allowUnsafe permits
// the loopback hosts tests use; production leaves it false.
func NewHTTPAPI(apiBase string, allowUnsafe bool) GitHubAPI {
	apiBase = strings.TrimRight(strings.TrimSpace(apiBase), "/")
	return &httpAPI{
		apiBase:     apiBase,
		client:      &http.Client{Timeout: 15 * time.Second},
		allowUnsafe: allowUnsafe,
	}
}

func (a *httpAPI) ExchangeManifest(ctx context.Context, code string) (ManifestConversion, error) {
	if strings.TrimSpace(code) == "" {
		return ManifestConversion{}, fmt.Errorf("%w: manifest code is required", ErrValidation)
	}
	endpoint := a.apiBase + "/app-manifests/" + code + "/conversions"
	var out struct {
		ID       int64  `json:"id"`
		Slug     string `json:"slug"`
		Name     string `json:"name"`
		ClientID string `json:"client_id"`
		Webhook  struct {
			Secret string `json:"secret"`
		} `json:"webhook_secret"`
		PEM string `json:"pem"`
	}
	if err := a.post(ctx, endpoint, nil, &out); err != nil {
		return ManifestConversion{}, err
	}
	if out.ID == 0 || strings.TrimSpace(out.PEM) == "" {
		return ManifestConversion{}, fmt.Errorf("%w: github returned an incomplete app conversion", ErrValidation)
	}
	return ManifestConversion{
		ID:            out.ID,
		Slug:          out.Slug,
		Name:          out.Name,
		ClientID:      out.ClientID,
		WebhookSecret: out.Webhook.Secret,
		PEM:           out.PEM,
	}, nil
}

func (a *httpAPI) CreateInstallationToken(ctx context.Context, installationID int64, jwt string) (InstallationToken, error) {
	if installationID <= 0 {
		return InstallationToken{}, fmt.Errorf("%w: installation id is required", ErrValidation)
	}
	endpoint := fmt.Sprintf("%s/app/installations/%d/access_tokens", a.apiBase, installationID)
	var out struct {
		Token     string `json:"token"`
		ExpiresAt string `json:"expires_at"`
	}
	if err := a.postWithAuth(ctx, endpoint, jwt, &out); err != nil {
		return InstallationToken{}, err
	}
	if strings.TrimSpace(out.Token) == "" {
		return InstallationToken{}, fmt.Errorf("%w: github returned no installation token", ErrValidation)
	}
	expiresAt := time.Now().Add(time.Hour)
	if parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(out.ExpiresAt)); err == nil {
		expiresAt = parsed
	}
	_ = jwt
	return InstallationToken{Token: out.Token, ExpiresAt: expiresAt}, nil
}

func (a *httpAPI) ListInstallationRepos(ctx context.Context, token string) ([]Repo, error) {
	var out struct {
		Repositories []struct {
			ID            int64  `json:"id"`
			Name          string `json:"name"`
			FullName      string `json:"full_name"`
			Private       bool   `json:"private"`
			DefaultBranch string `json:"default_branch"`
			CloneURL      string `json:"clone_url"`
			SSHURL        string `json:"ssh_url"`
			HTMLURL       string `json:"html_url"`
		} `json:"repositories"`
	}
	if err := a.get(ctx, a.apiBase+"/installation/repositories?per_page=100", token, &out); err != nil {
		return nil, err
	}
	repos := make([]Repo, 0, len(out.Repositories))
	for _, r := range out.Repositories {
		repos = append(repos, Repo{
			ExternalID:    fmt.Sprintf("%d", r.ID),
			Name:          r.Name,
			FullName:      r.FullName,
			Private:       r.Private,
			DefaultBranch: r.DefaultBranch,
			CloneURL:      r.CloneURL,
			SSHURL:        r.SSHURL,
			HTMLURL:       r.HTMLURL,
		})
	}
	return repos, nil
}

func (a *httpAPI) ListBranches(ctx context.Context, token, repo string) ([]Branch, error) {
	if strings.Count(strings.TrimSpace(repo), "/") != 1 {
		return nil, fmt.Errorf("%w: repository must be owner/name", ErrValidation)
	}
	var out []struct {
		Name      string `json:"name"`
		Protected bool   `json:"protected"`
		Commit    struct {
			SHA string `json:"sha"`
		} `json:"commit"`
	}
	if err := a.get(ctx, a.apiBase+"/repos/"+strings.TrimSpace(repo)+"/branches?per_page=100", token, &out); err != nil {
		return nil, err
	}
	branches := make([]Branch, 0, len(out))
	for _, b := range out {
		branches = append(branches, Branch{Name: b.Name, Commit: b.Commit.SHA, Protected: b.Protected})
	}
	return branches, nil
}

func (a *httpAPI) post(ctx context.Context, endpoint string, body any, out any) error {
	if body != nil {
		return fmt.Errorf("githubapp: request %s: unexpected body", endpoint)
	}
	return a.postWithAuth(ctx, endpoint, "", out)
}

// postWithAuth posts an empty body, authenticating with the app JWT when set.
// Neither endpoint takes parameters and neither sends credentials in the
// body, so the body stays empty.
func (a *httpAPI) postWithAuth(ctx context.Context, endpoint, jwt string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	if jwt != "" {
		req.Header.Set("Authorization", "Bearer "+jwt)
	}
	return a.do(req, out)
}

func (a *httpAPI) get(ctx context.Context, endpoint, token string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	return a.do(req, out)
}

func (a *httpAPI) do(req *http.Request, out any) error {
	if !a.allowUnsafe {
		if err := guardHost(req.URL.Hostname()); err != nil {
			return err
		}
	}
	resp, err := a.client.Do(req)
	if err != nil {
		return fmt.Errorf("githubapp: request %s: %w", req.URL.Path, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("githubapp: request %s: unexpected status %d", req.URL.Path, resp.StatusCode)
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("githubapp: decode %s: %w", req.URL.Path, err)
	}
	return nil
}
