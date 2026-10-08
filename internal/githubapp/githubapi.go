package githubapp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
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
	// GetInstallation verifies an installation belongs to the app and returns
	// the account login. It authenticates with the app JWT.
	GetInstallation(ctx context.Context, installationID int64, jwt string) (InstallationInfo, error)
	// CreateInstallationToken mints a short-lived token for an installation,
	// authenticated with the app JWT.
	CreateInstallationToken(ctx context.Context, installationID int64, jwt string) (InstallationToken, error)
	// ListInstallationRepos lists the repositories of an installation,
	// reporting whether the page walk hit its bound (truncated).
	ListInstallationRepos(ctx context.Context, token string) ([]Repo, bool, error)
	// ListBranches lists the branches of repo ("owner/name").
	ListBranches(ctx context.Context, token, repo string) ([]Branch, error)
}

// InstallationInfo is what GET /app/installations/{id} proves: the
// installation exists, belongs to this app, and names the account.
type InstallationInfo struct {
	ID      int64
	Account string
	AppID   int64
}

// repoListPageSize is the installation repository page size, and
// maxRepoListPages bounds the page walk so a host that always answers full
// pages cannot loop the lister forever (1000 repos).
const (
	repoListPageSize = 100
	maxRepoListPages = 10
)

// httpAPI implements GitHubAPI against one API base URL
// (https://api.github.com or a GitHub Enterprise /api/v3 root).
type httpAPI struct {
	apiBase     string
	client      *http.Client
	allowUnsafe bool
}

// NewHTTPAPI builds the production GitHubAPI for apiBase. allowUnsafe permits
// the loopback hosts tests use; production leaves it false, and then every
// connection is dial-guarded to public IPs (see net.go).
func NewHTTPAPI(apiBase string, allowUnsafe bool) GitHubAPI {
	apiBase = strings.TrimRight(strings.TrimSpace(apiBase), "/")
	return &httpAPI{
		apiBase: apiBase,
		client: &http.Client{
			Timeout:       15 * time.Second,
			CheckRedirect: noRedirect,
			Transport: &http.Transport{
				DialContext:         guardedDialer(allowUnsafe),
				TLSHandshakeTimeout: 10 * time.Second,
			},
		},
		allowUnsafe: allowUnsafe,
	}
}

// noRedirect refuses redirects: the GitHub API answers directly, and a
// redirect to an attacker host must never be followed with credentials.
func noRedirect(_ *http.Request, _ []*http.Request) error {
	return errRedirect
}

func (a *httpAPI) ExchangeManifest(ctx context.Context, code string) (ManifestConversion, error) {
	if strings.TrimSpace(code) == "" {
		return ManifestConversion{}, fmt.Errorf("%w: manifest code is required", ErrValidation)
	}
	endpoint := a.apiBase + "/app-manifests/" + code + "/conversions"
	var out struct {
		ID            int64          `json:"id"`
		Slug          string         `json:"slug"`
		Name          string         `json:"name"`
		ClientID      string         `json:"client_id"`
		ClientSecret  string         `json:"client_secret"`
		WebhookSecret flexibleString `json:"webhook_secret"`
		PEM           string         `json:"pem"`
		HTMLURL       string         `json:"html_url"`
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
		WebhookSecret: string(out.WebhookSecret),
		PEM:           out.PEM,
	}, nil
}

// flexibleString decodes a JSON string that GitHub sometimes wraps: the
// manifest conversion returns webhook_secret as a plain string, but older
// Enterprise releases nest it, so both shapes are accepted defensively.
type flexibleString string

func (s *flexibleString) UnmarshalJSON(raw []byte) error {
	var plain string
	if err := json.Unmarshal(raw, &plain); err == nil {
		*s = flexibleString(plain)
		return nil
	}
	var nested struct {
		Secret string `json:"secret"`
	}
	if err := json.Unmarshal(raw, &nested); err != nil {
		return err
	}
	*s = flexibleString(nested.Secret)
	return nil
}

// GetInstallation verifies the installation belongs to the app: GitHub only
// answers when the JWT's app owns the installation, so a recorded id is
// proof, not a claim. The manifest code never appears in errors (see
// redactCode): the installation id is not sensitive.
func (a *httpAPI) GetInstallation(ctx context.Context, installationID int64, jwt string) (InstallationInfo, error) {
	if installationID <= 0 {
		return InstallationInfo{}, fmt.Errorf("%w: installation id is required", ErrValidation)
	}
	endpoint := fmt.Sprintf("%s/app/installations/%d", a.apiBase, installationID)
	var out struct {
		ID      int64 `json:"id"`
		Account *struct {
			Login string `json:"login"`
		} `json:"account"`
		AppID int64 `json:"app_id"`
	}
	if err := a.getWithAuth(ctx, endpoint, jwt, &out); err != nil {
		return InstallationInfo{}, err
	}
	if out.ID != installationID {
		return InstallationInfo{}, fmt.Errorf("%w: github returned another installation", ErrValidation)
	}
	info := InstallationInfo{ID: out.ID, AppID: out.AppID}
	if out.Account != nil {
		info.Account = out.Account.Login
	}
	return info, nil
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
	return InstallationToken{Token: out.Token, ExpiresAt: expiresAt}, nil
}

func (a *httpAPI) ListInstallationRepos(ctx context.Context, token string) ([]Repo, bool, error) {
	repos := make([]Repo, 0)
	// Installations can hold more than one page: follow pages until a short
	// one, bounded so a misbehaving host cannot page forever. Hitting the
	// bound truncates and reports it, so callers can say so instead of
	// silently serving a partial list.
	for page := 1; page <= maxRepoListPages; page++ {
		endpoint := fmt.Sprintf("%s/installation/repositories?per_page=%d&page=%d", a.apiBase, repoListPageSize, page)
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
		if err := a.get(ctx, endpoint, token, &out); err != nil {
			return nil, false, err
		}
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
		if len(out.Repositories) < repoListPageSize {
			return repos, false, nil
		}
	}
	return repos, true, nil
}

func (a *httpAPI) ListBranches(ctx context.Context, token, repo string) ([]Branch, error) {
	if strings.Count(strings.TrimSpace(repo), "/") != 1 {
		return nil, fmt.Errorf("%w: repository must be owner/name", ErrValidation)
	}
	// Branches page like repositories: follow pages until a short one,
	// bounded the same way.
	branches := make([]Branch, 0)
	for page := 1; page <= maxRepoListPages; page++ {
		var out []struct {
			Name      string `json:"name"`
			Protected bool   `json:"protected"`
			Commit    struct {
				SHA string `json:"sha"`
			} `json:"commit"`
		}
		endpoint := fmt.Sprintf("%s/repos/%s/branches?per_page=%d&page=%d", a.apiBase, strings.TrimSpace(repo), repoListPageSize, page)
		if err := a.get(ctx, endpoint, token, &out); err != nil {
			return nil, err
		}
		for _, b := range out {
			branches = append(branches, Branch{Name: b.Name, Commit: b.Commit.SHA, Protected: b.Protected})
		}
		if len(out) < repoListPageSize {
			return branches, nil
		}
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
	return a.getWithAuth(ctx, endpoint, token, out)
}

// getWithAuth issues a GET, authenticating with a bearer token or app JWT.
// The credential is never part of the error text.
func (a *httpAPI) getWithAuth(ctx context.Context, endpoint, credential string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	if credential != "" {
		req.Header.Set("Authorization", "Bearer "+credential)
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
		// url.Error embeds the full request URL (including the single-use
		// manifest code), so only the wrapped reason crosses into the error:
		// redactCode covers the path this code prints, never the URL.
		var urlErr *url.Error
		if errors.As(err, &urlErr) {
			return fmt.Errorf("githubapp: request %s: %v", redactCode(req.URL.Path), urlErr.Err)
		}
		return fmt.Errorf("githubapp: request %s: %w", redactCode(req.URL.Path), err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("githubapp: request %s: unexpected status %d", redactCode(req.URL.Path), resp.StatusCode)
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("githubapp: decode %s: %w", redactCode(req.URL.Path), err)
	}
	return nil
}

// redactCode hides the manifest conversion code in request paths: the code is
// a single-use credential, so it must never reach logs through a wrapped
// url.Error. Every other path passes through unchanged.
func redactCode(path string) string {
	return manifestCodePattern.ReplaceAllString(path, "/app-manifests/[redacted]/conversions")
}
