package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"syscall"
	"time"

	"golang.org/x/oauth2"
)

// maxResponseBytes bounds how much of a provider response is decoded.
const maxResponseBytes = 4 << 20 // 4 MiB

// maxRepoPages bounds how many pages a repository/branch listing walks. A
// provider that never returns a short page — a hostile or misconfigured
// self-hosted instance — must not make the control plane allocate without
// limit. At the largest provider page size this caps a listing at 50k items;
// GitLab additionally stops itself at its 50k offset ceiling. A listing that
// hits this bound is reported as truncated so the caller does not overwrite a
// complete cache with a partial list.
const maxRepoPages = 500

// providerHTTPTimeout bounds one provider HTTP call (including an oauth2 token
// refresh). It is a variable so a test can shorten it; production keeps the
// sane default.
var providerHTTPTimeout = 15 * time.Second

// providerListingTimeout bounds one whole paginated listing. Without it a
// hostile instance could drip-feed full pages and pin the goroutine for
// maxRepoPages × providerHTTPTimeout. It is a variable so a test can shorten it.
var providerListingTimeout = 2 * time.Minute

// providerRedirectLimit bounds followed redirects; the stdlib default is 10.
const providerRedirectLimit = 10

// metadataV4 and metadataV6 are the known cloud metadata endpoints that live
// inside otherwise-legitimate private ranges (Alibaba 100.100.100.200 in
// CGNAT, AWS fd00:ec2::254 in ULA). The ranges themselves stay allowed so
// self-hosted IPv6-ULA/Tailscale providers keep working; only these two
// addresses are refused.
var (
	metadataV4 = netip.MustParseAddr("100.100.100.200")
	metadataV6 = netip.MustParseAddr("fd00:ec2::254")
)

// withProviderTimeout bounds a provider call with the shared timeout while
// preserving any shorter deadline the caller already set on ctx.
func withProviderTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, providerHTTPTimeout)
}

// withListingTimeout bounds one whole paginated listing.
func withListingTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, providerListingTimeout)
}

// unsafeAddr reports whether a resolved address must not be dialed: loopback,
// link-local (including the IPv4 169.254.169.254 and IPv6 fe80::/10 metadata
// services), multicast, unspecified, or one of the two known cloud metadata
// endpoints that sit inside otherwise-allowed private ranges.
func unsafeAddr(addr netip.Addr) bool {
	addr = addr.Unmap()
	switch {
	case addr.IsLoopback(), addr.IsLinkLocalUnicast(), addr.IsLinkLocalMulticast(),
		addr.IsMulticast(), addr.IsUnspecified():
		return true
	case addr == metadataV4 || addr == metadataV6:
		return true
	default:
		return false
	}
}

// validateOutboundHost refuses a host that resolves to an unsafe address. Only
// literal addresses are checked here; a hostname is left to the dial-time guard
// (providerDialControl), which sees the resolved IP and so also covers DNS
// rebinding and non-canonical numeric forms (2130706433, 0x7f.0.0.1, 127.1).
func validateOutboundHost(host string, allowUnsafe bool) error {
	if allowUnsafe {
		return nil
	}
	host = strings.Trim(strings.TrimSpace(host), "[]")
	if zone := strings.IndexByte(host, '%'); zone >= 0 {
		host = host[:zone]
	}
	if addr, err := netip.ParseAddr(host); err == nil && unsafeAddr(addr) {
		return fmt.Errorf("%w: refusing unsafe outbound address", ErrValidation)
	}
	return nil
}

// checkProviderRedirect refuses a redirect that leaves the provider origin or
// that targets an unsafe address, mirroring the notifications outbound guard.
// Same-origin redirects (for example a trailing-slash normalization) keep
// working. The guard is method-agnostic, so it also stops a 307/308 from
// replaying a request body to another origin.
func checkProviderRedirect(allowUnsafe bool) func(*http.Request, []*http.Request) error {
	return func(req *http.Request, via []*http.Request) error {
		if len(via) >= providerRedirectLimit {
			return fmt.Errorf("%w: too many redirects", ErrValidation)
		}
		if len(via) > 0 {
			origin := via[0].URL
			if req.URL.Scheme != origin.Scheme || req.URL.Host != origin.Host {
				return fmt.Errorf("%w: cross-origin redirect", ErrValidation)
			}
		}
		return validateOutboundHost(req.URL.Hostname(), allowUnsafe)
	}
}

// providerDialControl rejects a dial whose resolved address is unsafe, so a
// hostname that resolves to loopback/link-local/metadata (including
// non-canonical numeric forms) cannot be reached even when validation ran on
// the name.
func providerDialControl(allowUnsafe bool) func(network, address string, _ syscall.RawConn) error {
	return func(_, address string, _ syscall.RawConn) error {
		if allowUnsafe {
			return nil
		}
		host, _, err := net.SplitHostPort(address)
		if err != nil {
			host = address
		}
		if addr, err := netip.ParseAddr(host); err == nil && unsafeAddr(addr) {
			return fmt.Errorf("%w: refusing to dial unsafe address", ErrValidation)
		}
		return nil
	}
}

// newProviderTransport clones the default transport with the dial guard.
func newProviderTransport(allowUnsafe bool) *http.Transport {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.DialContext = (&net.Dialer{
		Timeout:   30 * time.Second,
		KeepAlive: 30 * time.Second,
		Control:   providerDialControl(allowUnsafe),
	}).DialContext
	return transport
}

// providerHTTPContext returns ctx carrying the guarded, timeout-bounded base
// client that oauth2 uses for both token refreshes and API calls. Putting it in
// the context via oauth2.HTTPClient is what bounds a refresh performed inside
// the transport, which the per-request context wrap cannot reach.
func providerHTTPContext(ctx context.Context, allowUnsafe bool) context.Context {
	base := &http.Client{
		Transport:     newProviderTransport(allowUnsafe),
		Timeout:       providerHTTPTimeout,
		CheckRedirect: checkProviderRedirect(allowUnsafe),
	}
	return context.WithValue(ctx, oauth2.HTTPClient, base)
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

// validateBaseURL checks a stored base_url before it is used as an outbound
// target. An empty value is allowed (the provider's public host). A non-empty
// value must be an absolute http(s) URL with no userinfo, and — unless
// allowUnsafe is set — must not name localhost, a metadata hostname, or a
// loopback/link-local/metadata/unspecified literal address. Private IPv4
// (RFC1918), CGNAT and IPv6 ULA hosts stay allowed because self-hosted
// GitLab/Gitea (and Tailscale) instances legitimately live there. A hostname
// that is not a literal is left to the dial-time guard, which sees the
// resolved IP.
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

	host := strings.ToLower(strings.Trim(parsed.Hostname(), "[]"))
	host = strings.TrimSuffix(host, ".")
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return fmt.Errorf("%w: base_url must not point at localhost", ErrValidation)
	}
	switch host {
	case "metadata.google.internal", "metadata":
		return fmt.Errorf("%w: base_url must not point at a metadata service", ErrValidation)
	}
	return validateOutboundHost(host, allowUnsafe)
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
