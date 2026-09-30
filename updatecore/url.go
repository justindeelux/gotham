package updatecore

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// DefaultHTTPClient builds an HTTP client that bounds redirects, revalidates
// every hop, refuses https downgrades, and blocks link-local/metadata dials.
func DefaultHTTPClient(timeout time.Duration) *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.DialContext = safeDialContext
	return &http.Client{
		Timeout:   timeout,
		Transport: transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return errors.New("updates: too many redirects")
			}
			var previous *url.URL
			if len(via) > 0 {
				previous = via[len(via)-1].URL
			}
			return validateRedirect(previous, req.URL)
		},
	}
}

// ErrBadURL is returned when a release URL is not an acceptable http(s)
// destination.
var ErrBadURL = errors.New("updates: unacceptable URL")

// metadataIPs are IPv6/IPv4 addresses of cloud metadata services that are not
// already covered by the link-local ranges.
var metadataIPs = []net.IP{
	net.ParseIP("fd00:ec2::254"),   // AWS IMDS over IPv6
	net.ParseIP("169.254.169.254"), // AWS/GCP/Azure IMDS (link-local, kept explicit)
	net.ParseIP("100.100.100.200"), // Alibaba Cloud
}

// validateURL accepts an absolute http(s) URL. Plain http is only accepted for
// loopback hosts so tests and local mirrors work without weakening production
// transport. Link-local/metadata destinations are refused.
func validateURL(raw string) error {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Host == "" {
		return ErrBadURL
	}
	switch parsed.Scheme {
	case "https":
	case "http":
		if !isLoopbackHost(parsed.Hostname()) {
			return ErrBadURL
		}
	default:
		return ErrBadURL
	}
	return validateOutboundHost(parsed.Hostname())
}

// ValidateURL reports whether raw is an acceptable release destination.
func ValidateURL(raw string) error { return validateURL(raw) }

// validateRedirect rejects a redirect target and refuses an https upgrade path
// from downgrading to plain http.
func validateRedirect(previous *url.URL, next *url.URL) error {
	if err := validateURL(next.String()); err != nil {
		return err
	}
	if previous != nil && previous.Scheme == "https" && next.Scheme != "https" {
		return fmt.Errorf("%w: refusing https downgrade redirect", ErrBadURL)
	}
	return nil
}

// validateOutboundHost refuses literal link-local/metadata addresses. Private
// ranges stay allowed: a self-hosted mirror on a private network is a
// legitimate configuration.
func validateOutboundHost(host string) error {
	host = strings.TrimSpace(host)
	if split, _, err := net.SplitHostPort(host); err == nil {
		host = split
	}
	host = strings.Trim(host, "[]")
	if zone := strings.IndexByte(host, '%'); zone >= 0 {
		host = host[:zone]
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return nil
	}
	if isBlockedIP(ip) {
		return ErrBadURL
	}
	return nil
}

// isBlockedIP reports whether ip is a link-local/metadata destination.
func isBlockedIP(ip net.IP) bool {
	if ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
		return true
	}
	for _, blocked := range metadataIPs {
		if ip.Equal(blocked) {
			return true
		}
	}
	return false
}

// safeDialContext resolves the host and refuses to connect to a
// link-local/metadata address, so DNS cannot smuggle a request to a cloud
// metadata service.
func safeDialContext(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}
	if ip := net.ParseIP(host); ip != nil {
		if isBlockedIP(ip) {
			return nil, fmt.Errorf("%w: %s", ErrBadURL, host)
		}
		return (&net.Dialer{}).DialContext(ctx, network, address)
	}

	ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, err
	}
	dialer := &net.Dialer{}
	var lastErr error
	for _, resolved := range ips {
		if isBlockedIP(resolved.IP) {
			continue
		}
		conn, err := dialer.DialContext(ctx, network, net.JoinHostPort(resolved.IP.String(), port))
		if err == nil {
			return conn, nil
		}
		lastErr = err
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, fmt.Errorf("%w: no allowed address for %s", ErrBadURL, host)
}

// isLoopbackHost reports whether host is a loopback literal or "localhost".
func isLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(strings.Trim(host, "[]"))
	return ip != nil && ip.IsLoopback()
}
