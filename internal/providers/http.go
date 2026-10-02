package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"
)

// maxResponseBytes bounds how much of a provider response is decoded.
const maxResponseBytes = 4 << 20 // 4 MiB

// maxRepoPages bounds how many pages a repository/branch listing walks. A
// provider that never returns a short page — a hostile or misconfigured
// self-hosted instance — must not make the control plane allocate without
// limit. At the largest provider page size this caps a listing at 50k items;
// GitLab additionally stops itself at its 50k offset ceiling.
const maxRepoPages = 500

// providerHTTPTimeout bounds one provider HTTP call. It is a variable so a test
// can shorten it; production keeps the sane default.
var providerHTTPTimeout = 15 * time.Second

// withProviderTimeout bounds a provider call with the shared timeout while
// preserving any shorter deadline the caller already set on ctx.
func withProviderTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, providerHTTPTimeout)
}

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
	ctx, cancel := withProviderTimeout(ctx)
	defer cancel()

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

// doJSON performs method against url, encoding payload as JSON when it is not
// nil, and decodes a non-empty response body into dst when dst is not nil.
// Every 2xx status is a success (create answers 201, delete 204); anything
// else yields *httpError so callers can branch on the status.
func doJSON(ctx context.Context, client *http.Client, provider, method, url, accept string, payload, dst any) error {
	var body io.Reader
	if payload != nil {
		encoded, err := json.Marshal(payload)
		if err != nil {
			return fmt.Errorf("providers: %s encode: %w", provider, err)
		}
		body = bytes.NewReader(encoded)
	}

	ctx, cancel := withProviderTimeout(ctx)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return fmt.Errorf("providers: %s request: %w", provider, err)
	}
	if accept != "" {
		req.Header.Set("Accept", accept)
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("providers: %s request: %w", provider, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxResponseBytes))
		return &httpError{provider: provider, url: url, status: resp.StatusCode}
	}
	if dst == nil {
		return nil
	}
	limited := io.LimitReader(resp.Body, maxResponseBytes)
	if err := json.NewDecoder(limited).Decode(dst); err != nil {
		return fmt.Errorf("providers: %s decode: %w", provider, err)
	}
	return nil
}

// isNotFound reports whether err is an *httpError with status 404 — the "the
// resource is already gone" answer that deletes treat as success so callers
// stay idempotent.
func isNotFound(err error) bool {
	var httpErr *httpError
	return errors.As(err, &httpErr) && httpErr.status == http.StatusNotFound
}

// isBadRequest reports whether err is an *httpError with status 400. GitLab
// answers 400 once offset pagination passes its 50k ceiling; the caller treats
// that as the end of a listing instead of a failure.
func isBadRequest(err error) bool {
	var httpErr *httpError
	return errors.As(err, &httpErr) && httpErr.status == http.StatusBadRequest
}

// validateBaseURL checks a stored base_url before it is used as an outbound
// target. An empty value is allowed (the provider's public host). A non-empty
// value must be an absolute http(s) URL with no userinfo, and — unless
// allowUnsafe is set — must not be a loopback, link-local, metadata or
// unspecified literal address. Private (RFC1918/ULA) hosts stay allowed because
// self-hosted GitLab/Gitea instances legitimately live there; hostnames are
// left to DNS, matching the notifications outbound guard.
func validateBaseURL(raw string, allowUnsafe bool) error {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil
	}
	parsed, err := url.Parse(trimmed)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return fmt.Errorf("%w: base_url must be an absolute http(s) URL", ErrValidation)
	}
	if parsed.User != nil {
		return fmt.Errorf("%w: base_url must not contain userinfo", ErrValidation)
	}
	if allowUnsafe {
		return nil
	}

	host := strings.Trim(parsed.Hostname(), "[]")
	if addr, err := netip.ParseAddr(host); err == nil {
		if addr.IsLoopback() || addr.IsLinkLocalUnicast() || addr.IsLinkLocalMulticast() ||
			addr.IsMulticast() || addr.IsUnspecified() {
			return fmt.Errorf("%w: base_url must not point at a loopback, link-local or metadata address", ErrValidation)
		}
	}
	switch strings.ToLower(host) {
	case "metadata.google.internal", "metadata":
		return fmt.Errorf("%w: base_url must not point at a metadata service", ErrValidation)
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

// validateHookID accepts only a decimal provider hook ID. The ID is spliced
// into a delete path, so anything that is not digits is rejected instead of
// being allowed to address another API route.
func validateHookID(hookID string) error {
	return validateProviderID(hookID, "hook id")
}

// validateDeployKeyID accepts only a decimal provider deploy-key ID (same rule
// as validateHookID: the ID is spliced into a delete path).
func validateDeployKeyID(keyID string) error {
	return validateProviderID(keyID, "deploy key id")
}

// validateProviderID rejects anything but a short decimal provider identifier.
func validateProviderID(id, what string) error {
	if id == "" || len(id) > 32 {
		return fmt.Errorf("%w: invalid %s", ErrValidation, what)
	}
	for _, r := range id {
		if r < '0' || r > '9' {
			return fmt.Errorf("%w: invalid %s", ErrValidation, what)
		}
	}
	return nil
}

// validateComment rejects a comment the Git host would refuse anyway: a pull
// request number must be positive and the body must not be blank. The number
// is rendered with strconv.Itoa, so it can never escape the intended path.
func validateComment(number int, body string) error {
	if number <= 0 {
		return fmt.Errorf("%w: invalid pull request number", ErrValidation)
	}
	if strings.TrimSpace(body) == "" {
		return fmt.Errorf("%w: comment body is empty", ErrValidation)
	}
	return nil
}

// validateDeployKey rejects a public key the Git host would refuse anyway, so
// the API answers with a clear validation error instead of a provider body.
func validateDeployKey(key DeployKey) error {
	if strings.TrimSpace(key.Title) == "" {
		return fmt.Errorf("%w: deploy key title is empty", ErrValidation)
	}
	if strings.TrimSpace(key.Key) == "" {
		return fmt.Errorf("%w: deploy key is empty", ErrValidation)
	}
	return nil
}

// hookEvents returns the events to subscribe to, defaulting to push so a
// caller that does not care cannot install a hook that never fires.
func hookEvents(events []string, fallback string) []string {
	if len(events) == 0 {
		return []string{fallback}
	}
	return events
}

// wantsEvent reports whether events selects name. An empty event list means
// "the default set", which for every supported provider includes push.
func wantsEvent(events []string, name string) bool {
	if len(events) == 0 {
		return true
	}
	return eventSelected(events, name)
}

// eventSelected reports whether events explicitly selects name. Unlike
// wantsEvent, an empty list selects nothing: optional event families
// (pull_request / merge requests) are opt-in, never part of the push default.
func eventSelected(events []string, name string) bool {
	for _, event := range events {
		if strings.EqualFold(event, name) {
			return true
		}
	}
	return false
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
