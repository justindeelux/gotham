package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/google/uuid"
)

// gitLabProvisionScopes is the exact scope set a Gotham GitLab OAuth
// application needs: full API access for webhooks, deploy keys and member
// project listing, plus the read scopes a least-privilege manual application
// carries.
const gitLabProvisionScopes = "api read_user read_repository"

// AutoProvisionGitLabInput carries the one-time details to create a GitLab
// OAuth application automatically. AdminToken is used for the single
// POST /api/v4/applications call and is never stored or logged.
type AutoProvisionGitLabInput struct {
	BaseURL     string
	AdminToken  string
	Name        string
	RedirectURL string
	Scopes      string
}

// GitLabSetupInfo is the manual-application fallback: the exact redirect URI
// and scopes to register on gitlab.com or a self-hosted instance when no
// admin token is available for automatic creation.
type GitLabSetupInfo struct {
	BaseURL     string `json:"base_url"`
	RedirectURI string `json:"redirect_uri"`
	Scopes      string `json:"scopes"`
}

// gitLabApplication is the subset of the POST /api/v4/applications response
// Gotham keeps: the client id and secret of the created OAuth application.
type gitLabApplication struct {
	ID            int64  `json:"id"`
	ApplicationID string `json:"application_id"`
	Secret        string `json:"secret"`
}

// AutoProvisionGitLab creates the GitLab OAuth application through the GitLab
// API from a one-time admin token and stores the connection (client id/secret
// sealed, tokens empty until the OAuth flow completes). The admin token never
// reaches the database: it authenticates the single provisioning call only.
//
// The admin token needs api scope (applications are administrator-only
// resources). Self-hosted limits: the instance must be reachable over TLS the
// control plane trusts, base_url must name the instance root
// (https://git.example.com, no /api/v4 suffix), and the redirect URL must be
// the exact public control-plane callback.
func (s *Service) AutoProvisionGitLab(ctx context.Context, userID uuid.UUID, input AutoProvisionGitLabInput) (Provider, error) {
	if s.repo == nil {
		return Provider{}, fmt.Errorf("providers: repository is not configured")
	}
	base := gitLabInstanceBase(strings.TrimSpace(input.BaseURL))
	if err := validateBaseURL(base, s.allowUnsafeBaseURL); err != nil {
		return Provider{}, err
	}
	if strings.TrimSpace(input.AdminToken) == "" {
		return Provider{}, fmt.Errorf("%w: admin token is required", ErrValidation)
	}
	redirectURL := strings.TrimSpace(input.RedirectURL)
	if err := validateRedirectURL(redirectURL); err != nil {
		return Provider{}, err
	}
	scopes := strings.TrimSpace(input.Scopes)
	if scopes == "" {
		scopes = gitLabProvisionScopes
	}
	name := strings.TrimSpace(input.Name)
	if name == "" {
		name = "gotham"
	}

	clientID, clientSecret, err := s.createGitLabApplication(ctx, base, input.AdminToken, name, redirectURL, scopes)
	if err != nil {
		return Provider{}, err
	}

	return s.repo.Create(ctx, Provider{
		UserID:       userID,
		Name:         NameGitLab,
		BaseURL:      base,
		ClientID:     clientID,
		ClientSecret: clientSecret,
		RedirectURL:  redirectURL,
		Scopes:       scopes,
	})
}

// GitLabSetupInfoFor returns the manual-application fallback for baseURL: the
// exact redirect URI and scopes to register when automatic creation is not
// possible. It validates the instance base URL but performs no network call.
func (s *Service) GitLabSetupInfoFor(baseURL, redirectURL string) (GitLabSetupInfo, error) {
	base := gitLabInstanceBase(strings.TrimSpace(baseURL))
	if err := validateBaseURL(base, s.allowUnsafeBaseURL); err != nil {
		return GitLabSetupInfo{}, err
	}
	if err := validateRedirectURL(strings.TrimSpace(redirectURL)); err != nil {
		return GitLabSetupInfo{}, err
	}
	if base == "" {
		base = gitLabDefaultBase
	}
	return GitLabSetupInfo{
		BaseURL:     base,
		RedirectURI: strings.TrimSpace(redirectURL),
		Scopes:      gitLabProvisionScopes,
	}, nil
}

// createGitLabApplication registers one confidential OAuth application on the
// GitLab instance and returns its client id and secret. The admin token travels
// in the Authorization header of this call only; error values never include it.
func (s *Service) createGitLabApplication(ctx context.Context, base, adminToken, name, redirectURL, scopes string) (string, string, error) {
	if base == "" {
		base = gitLabDefaultBase
	}
	ctx, cancel := withProviderTimeout(providerHTTPContext(ctx, s.allowUnsafeBaseURL))
	defer cancel()

	payload := map[string]any{
		"name":         name,
		"redirect_uri": redirectURL,
		"scopes":       scopes,
		"confidential": true,
	}
	client := &http.Client{
		Transport:     newProviderTransport(s.allowUnsafeBaseURL),
		Timeout:       providerHTTPTimeout,
		CheckRedirect: checkProviderRedirect(s.allowUnsafeBaseURL),
	}
	var created gitLabApplication
	if err := postGitLabApplication(ctx, client, base, adminToken, payload, &created); err != nil {
		return "", "", err
	}
	if strings.TrimSpace(created.ApplicationID) == "" || strings.TrimSpace(created.Secret) == "" {
		return "", "", fmt.Errorf("providers: %s: application response has no credentials", NameGitLab)
	}
	return created.ApplicationID, created.Secret, nil
}

// postGitLabApplication POSTs payload to /api/v4/applications with the admin
// token as a bearer credential and decodes the created application. A 401/403
// means the token was rejected (validation, so the caller gets a 400); any
// other non-2xx is a provider failure. Errors never include the token.
func postGitLabApplication(ctx context.Context, client *http.Client, base, adminToken string, payload map[string]any, dst *gitLabApplication) error {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("providers: %s encode: %w", NameGitLab, err)
	}
	endpoint := base + "/api/v4/applications"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(encoded))
	if err != nil {
		return fmt.Errorf("providers: %s request: %w", NameGitLab, err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+adminToken)

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("providers: %s request: %w", NameGitLab, err)
	}
	defer func() { _ = resp.Body.Close() }()

	switch {
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxResponseBytes))
		return fmt.Errorf("%w: gitlab rejected the admin token", ErrValidation)
	case resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices:
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxResponseBytes))
		return &httpError{provider: NameGitLab, url: endpoint, status: resp.StatusCode}
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxResponseBytes)).Decode(dst); err != nil {
		return fmt.Errorf("providers: %s decode: %w", NameGitLab, err)
	}
	return nil
}

// gitLabInstanceBase normalizes a caller-supplied instance root: an empty
// value selects gitlab.com (stored as "" so the default applies everywhere),
// and a value that already carries the /api/v4 suffix is trimmed back to the
// root the OAuth and API paths derive from.
func gitLabInstanceBase(raw string) string {
	base := strings.TrimRight(strings.TrimSpace(raw), "/")
	if base == gitLabDefaultBase || base == gitLabDefaultBase+"/api/v4" {
		return ""
	}
	base = strings.TrimSuffix(base, "/api/v4")
	return strings.TrimRight(base, "/")
}

// validateRedirectURL accepts only absolute http(s) callback URLs without
// userinfo: GitLab matches the redirect exactly, so a relative or
// non-http(s) value could never complete the flow.
func validateRedirectURL(raw string) error {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return fmt.Errorf("%w: redirect_url must be an absolute http(s) URL", ErrValidation)
	}
	if parsed.User != nil {
		return fmt.Errorf("%w: redirect_url must not contain userinfo", ErrValidation)
	}
	return nil
}
