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

type ManifestConversion struct {
	ID            int64
	Slug          string
	Name          string
	ClientID      string
	WebhookSecret string
	PEM           string
}

type InstallationToken struct {
	Token     string
	ExpiresAt time.Time
}

type GitHubAPI interface {
	ExchangeManifest(ctx context.Context, code string) (ManifestConversion, error)
	GetInstallation(ctx context.Context, installationID int64, jwt string) (InstallationInfo, error)
	CreateInstallationToken(ctx context.Context, installationID int64, jwt string) (InstallationToken, error)
	ListInstallationRepos(ctx context.Context, token string) ([]Repo, bool, error)
	ListBranches(ctx context.Context, token, repo string) ([]Branch, error)
}

type InstallationInfo struct {
	ID      int64
	Account string
	AppID   int64
}

const (
	repoListPageSize = 100
	maxRepoListPages = 10
)

type httpAPI struct {
	apiBase     string
	client      *http.Client
	allowUnsafe bool
}

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

func redactCode(path string) string {
	return manifestCodePattern.ReplaceAllString(path, "/app-manifests/[redacted]/conversions")
}
