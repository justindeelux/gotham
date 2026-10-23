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

const maxResponseBytes = 4 << 20

const maxRepoPages = 500

var providerHTTPTimeout = 15 * time.Second

var providerListingTimeout = 2 * time.Minute

const providerRedirectLimit = 10

var (
	metadataV4 = netip.MustParseAddr("100.100.100.200")
	metadataV6 = netip.MustParseAddr("fd00:ec2::254")
)

func withProviderTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, providerHTTPTimeout)
}

func withListingTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, providerListingTimeout)
}

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

func newProviderTransport(allowUnsafe bool) *http.Transport {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.DialContext = (&net.Dialer{
		Timeout:   30 * time.Second,
		KeepAlive: 30 * time.Second,
		Control:   providerDialControl(allowUnsafe),
	}).DialContext
	return transport
}

func providerHTTPContext(ctx context.Context, allowUnsafe bool) context.Context {
	base := &http.Client{
		Transport:     newProviderTransport(allowUnsafe),
		Timeout:       providerHTTPTimeout,
		CheckRedirect: checkProviderRedirect(allowUnsafe),
	}
	return context.WithValue(ctx, oauth2.HTTPClient, base)
}

type httpError struct {
	provider string
	url      string
	status   int
}

func (e *httpError) Error() string {
	return fmt.Sprintf("providers: %s %s: unexpected status %d", e.provider, e.url, e.status)
}

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

func isNotFound(err error) bool {
	var httpErr *httpError
	return errors.As(err, &httpErr) && httpErr.status == http.StatusNotFound
}

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

func escapeProjectPath(repo string) string {
	return url.PathEscape(strings.TrimSpace(repo))
}

func validateHookID(hookID string) error {
	return validateProviderID(hookID, "hook id")
}

func validateDeployKeyID(keyID string) error {
	return validateProviderID(keyID, "deploy key id")
}

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

func validateComment(number int, body string) error {
	if number <= 0 {
		return fmt.Errorf("%w: invalid pull request number", ErrValidation)
	}
	if strings.TrimSpace(body) == "" {
		return fmt.Errorf("%w: comment body is empty", ErrValidation)
	}
	return nil
}

func validateDeployKey(key DeployKey) error {
	if strings.TrimSpace(key.Title) == "" {
		return fmt.Errorf("%w: deploy key title is empty", ErrValidation)
	}
	if strings.TrimSpace(key.Key) == "" {
		return fmt.Errorf("%w: deploy key is empty", ErrValidation)
	}
	return nil
}

func hookEvents(events []string, fallback string) []string {
	if len(events) == 0 {
		return []string{fallback}
	}
	return events
}

func wantsEvent(events []string, name string) bool {
	if len(events) == 0 {
		return true
	}
	return eventSelected(events, name)
}

func eventSelected(events []string, name string) bool {
	for _, event := range events {
		if strings.EqualFold(event, name) {
			return true
		}
	}
	return false
}

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
