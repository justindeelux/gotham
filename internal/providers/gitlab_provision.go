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
	"time"

	"github.com/google/uuid"
)

const gitLabDefaultProvisionScopes = "api read_user read_repository"

var gitLabAllowedScopes = map[string]bool{
	"api": true, "read_user": true, "read_repository": true,
	"openid": true, "profile": true, "email": true,
}

type AutoProvisionGitLabInput struct {
	BaseURL     string
	AdminToken  string
	Name        string
	RedirectURL string
	Scopes      string
}

type GitLabSetupInfo struct {
	BaseURL     string `json:"base_url"`
	RedirectURI string `json:"redirect_uri"`
	Scopes      string `json:"scopes"`
}

type gitLabApplication struct {
	ID            int64  `json:"id"`
	ApplicationID string `json:"application_id"`
	Secret        string `json:"secret"`
}

func (s *Service) AutoProvisionGitLab(ctx context.Context, userID uuid.UUID, input AutoProvisionGitLabInput) (Provider, error) {
	if s.repo == nil {
		return Provider{}, fmt.Errorf("providers: repository is not configured")
	}
	base := gitLabInstanceBase(strings.TrimSpace(input.BaseURL))
	if err := validateBaseURL(base, s.allowUnsafeBaseURL); err != nil {
		return Provider{}, err
	}
	if !s.allowUnsafeBaseURL && gitLabScheme(base) != "https" {
		return Provider{}, fmt.Errorf("%w: provisioning requires an https instance URL", ErrValidation)
	}
	if strings.TrimSpace(input.AdminToken) == "" {
		return Provider{}, fmt.Errorf("%w: admin token is required", ErrValidation)
	}
	redirectURL := strings.TrimSpace(input.RedirectURL)
	if err := validateRedirectURL(redirectURL); err != nil {
		return Provider{}, err
	}
	scopes, err := gitLabProvisionScopes(strings.TrimSpace(input.Scopes))
	if err != nil {
		return Provider{}, err
	}
	name := strings.TrimSpace(input.Name)
	if name == "" {
		name = "gotham"
	}

	if duplicate, err := s.gitLabConnected(ctx, userID, base); err != nil {
		return Provider{}, err
	} else if duplicate {
		return Provider{}, fmt.Errorf("%w: gitlab is already connected for this instance", ErrConflict)
	}

	applicationID, clientID, clientSecret, err := s.createGitLabApplication(ctx, base, input.AdminToken, name, redirectURL, scopes)
	if err != nil {
		return Provider{}, err
	}

	created, err := s.repo.Create(ctx, Provider{
		UserID:       userID,
		Name:         NameGitLab,
		BaseURL:      base,
		ClientID:     clientID,
		ClientSecret: clientSecret,
		RedirectURL:  redirectURL,
		Scopes:       scopes,
	})
	if err != nil {
		s.bestEffortRemoveApplication(ctx, base, input.AdminToken, applicationID)
		return Provider{}, err
	}
	return created, nil
}

func (s *Service) gitLabConnected(ctx context.Context, userID uuid.UUID, base string) (bool, error) {
	connections, err := s.repo.List(ctx, userID)
	if err != nil {
		return false, err
	}
	for _, connection := range connections {
		if connection.Name == NameGitLab && gitLabInstanceBase(connection.BaseURL) == base {
			return true, nil
		}
	}
	return false, nil
}

func gitLabProvisionScopes(scopes string) (string, error) {
	if strings.TrimSpace(scopes) == "" {
		return gitLabDefaultProvisionScopes, nil
	}
	var out []string
	seen := make(map[string]bool)
	for _, scope := range strings.Fields(scopes) {
		if !gitLabAllowedScopes[scope] {
			return "", fmt.Errorf("%w: unknown gitlab scope %q", ErrValidation, scope)
		}
		if !seen[scope] {
			seen[scope] = true
			out = append(out, scope)
		}
	}
	if len(out) == 0 {
		return gitLabDefaultProvisionScopes, nil
	}
	return strings.Join(out, " "), nil
}

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
		Scopes:      gitLabDefaultProvisionScopes,
	}, nil
}

func (s *Service) createGitLabApplication(ctx context.Context, base, adminToken, name, redirectURL, scopes string) (int64, string, string, error) {
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
		return 0, "", "", err
	}
	if strings.TrimSpace(created.ApplicationID) == "" || strings.TrimSpace(created.Secret) == "" {
		return 0, "", "", fmt.Errorf("providers: %s: application response has no credentials", NameGitLab)
	}
	return created.ID, created.ApplicationID, created.Secret, nil
}

const gitLabRemoveApplicationTimeout = 5 * time.Second

func (s *Service) bestEffortRemoveApplication(ctx context.Context, base, adminToken string, applicationID int64) {
	if applicationID <= 0 {
		return
	}
	if base == "" {
		base = gitLabDefaultBase
	}
	rollbackCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), gitLabRemoveApplicationTimeout)
	defer cancel()
	client := &http.Client{
		Transport:     newProviderTransport(s.allowUnsafeBaseURL),
		Timeout:       providerHTTPTimeout,
		CheckRedirect: checkProviderRedirect(s.allowUnsafeBaseURL),
	}
	endpoint := fmt.Sprintf("%s/api/v4/applications/%d", base, applicationID)
	req, err := http.NewRequestWithContext(rollbackCtx, http.MethodDelete, endpoint, nil)
	if err != nil {
		s.logger.Warn("providers: could not roll back gitlab application", "error", err)
		return
	}
	req.Header.Set("Authorization", "Bearer "+adminToken)
	resp, err := client.Do(req)
	if err != nil {
		s.logger.Warn("providers: could not roll back gitlab application", "error", err)
		return
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxResponseBytes))
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		s.logger.Warn("providers: could not roll back gitlab application", "status", resp.StatusCode)
	}
}

func gitLabScheme(base string) string {
	if base == "" {
		return "https"
	}
	if !strings.Contains(base, "://") {
		return ""
	}
	parsed, err := url.Parse(base)
	if err != nil {
		return ""
	}
	return parsed.Scheme
}

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

func gitLabInstanceBase(raw string) string {
	base := strings.TrimRight(strings.TrimSpace(raw), "/")
	if base == gitLabDefaultBase || base == gitLabDefaultBase+"/api/v4" {
		return ""
	}
	base = strings.TrimSuffix(base, "/api/v4")
	return strings.TrimRight(base, "/")
}

const gitLabCallbackPath = "/api/v1/providers/gitlab/callback"

func validateRedirectURL(raw string) error {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return fmt.Errorf("%w: redirect_url must be an absolute http(s) URL", ErrValidation)
	}
	if parsed.User != nil {
		return fmt.Errorf("%w: redirect_url must not contain userinfo", ErrValidation)
	}
	if parsed.Path != gitLabCallbackPath || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.RawPath != "" {
		return fmt.Errorf("%w: redirect_url must be the provider callback %q", ErrValidation, gitLabCallbackPath)
	}
	return nil
}
