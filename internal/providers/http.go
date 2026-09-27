package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// maxResponseBytes bounds how much of a provider response is decoded.
const maxResponseBytes = 4 << 20 // 4 MiB

// httpError is a non-2xx response from a provider API. Handlers map it to a
// gateway status instead of leaking the provider body.
type httpError struct {
	provider string
	url      string
	status   int
}

// Error implements error without embedding the response body.
func (e *httpError) Error() string {
	return fmt.Sprintf("providers: %s %s: unexpected status %d", e.provider, e.url, e.status)
}

// getJSON performs a GET with the given Accept header and decodes the JSON body
// into dst. A non-2xx status yields *httpError.
func getJSON(ctx context.Context, client *http.Client, provider, url, accept string, dst any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("providers: %s request: %w", provider, err)
	}
	if accept != "" {
		req.Header.Set("Accept", accept)
	}

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("providers: %s request: %w", provider, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxResponseBytes))
		return &httpError{provider: provider, url: url, status: resp.StatusCode}
	}

	if dst == nil {
		return nil
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxResponseBytes)).Decode(dst); err != nil {
		return fmt.Errorf("providers: %s decode: %w", provider, err)
	}
	return nil
}

// validateRepo rejects a repository identifier that could escape the intended
// API path. minSegments is 1 for providers with nested groups (GitLab) and 2 for
// "owner/name" identifiers (GitHub, Gitea).
func validateRepo(repo string, minSegments int) error {
	repo = strings.TrimSpace(repo)
	if repo == "" || strings.HasPrefix(repo, "/") || strings.Contains(repo, "..") {
		return fmt.Errorf("%w: invalid repository identifier", ErrValidation)
	}
	if strings.ContainsAny(repo, " \t\n\r?#") {
		return fmt.Errorf("%w: invalid repository identifier", ErrValidation)
	}
	if strings.Count(repo, "/")+1 < minSegments {
		return fmt.Errorf("%w: invalid repository identifier", ErrValidation)
	}
	return nil
}

// escapeProjectPath percent-encodes a GitLab project path ("group/project")
// into the single path segment the GitLab API expects.
func escapeProjectPath(repo string) string {
	return url.PathEscape(strings.TrimSpace(repo))
}

// splitScopes turns a comma/space separated scope string into a slice, falling
// back to fallback when empty.
func splitScopes(scopes, fallback string) []string {
	fields := strings.FieldsFunc(scopes, func(r rune) bool {
		return r == ',' || r == ' ' || r == '\t'
	})
	if len(fields) == 0 {
		fields = strings.FieldsFunc(fallback, func(r rune) bool {
			return r == ',' || r == ' ' || r == '\t'
		})
	}
	return fields
}
