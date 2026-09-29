package updates

import (
	"errors"
	"net"
	"net/url"
	"strings"
)

// ErrBadURL is returned when a release URL is not an acceptable http(s)
// destination.
var ErrBadURL = errors.New("updates: unacceptable URL")

// validateURL accepts an absolute http(s) URL. Plain http is only accepted for
// loopback hosts so tests and local mirrors work without weakening production
// transport. Link-local destinations (cloud metadata, fe80::/10) are refused.
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

// validateOutboundHost refuses literal link-local addresses, which cover the
// cloud metadata services (169.254.169.254) and IPv6 fe80::/10. Private ranges
// stay allowed: a self-hosted mirror on a private network is a legitimate
// configuration.
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
	if ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
		return ErrBadURL
	}
	return nil
}

// isLoopbackHost reports whether host is a loopback literal or "localhost".
func isLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(strings.Trim(host, "[]"))
	return ip != nil && ip.IsLoopback()
}
