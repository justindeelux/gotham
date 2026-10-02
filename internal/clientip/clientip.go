// Package clientip resolves the effective client address and request scheme
// behind a trusted reverse proxy, without trusting forwarded headers from an
// untrusted peer. An empty trusted set means no peer is trusted: forwarded
// headers are ignored and only the direct connection is reported.
package clientip

import (
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"strings"
)

// Parse turns configured entries into prefixes. An entry is an IP address or a
// CIDR; a bare address becomes a single-host prefix. Blank entries are skipped.
// A malformed entry is an error so a typo cannot silently widen trust.
func Parse(entries []string) ([]netip.Prefix, error) {
	var prefixes []netip.Prefix
	for _, entry := range entries {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		if strings.Contains(entry, "/") {
			prefix, err := netip.ParsePrefix(entry)
			if err != nil {
				return nil, fmt.Errorf("invalid trusted proxy %q: %w", entry, err)
			}
			prefixes = append(prefixes, prefix.Masked())
			continue
		}
		addr, err := netip.ParseAddr(entry)
		if err != nil {
			return nil, fmt.Errorf("invalid trusted proxy %q: %w", entry, err)
		}
		addr = addr.Unmap()
		prefixes = append(prefixes, netip.PrefixFrom(addr, addr.BitLen()))
	}
	return prefixes, nil
}

// ClientIP returns the effective client IP for r.
//
// When the direct peer is not trusted, X-Forwarded-For is ignored and the peer
// address is returned (the default, unchanged behavior with no proxies
// configured). When the peer is trusted, the header is walked right to left —
// nearest proxy first — and the first address that is not itself a trusted
// proxy is the client; a spoofed leftmost entry is therefore ignored because
// the trusted proxy's appended entry sits to its right. Malformed entries are
// skipped. When the header is absent, malformed, or entirely trusted, the peer
// address is the fallback.
func ClientIP(r *http.Request, trusted []netip.Prefix) string {
	peer := peerHost(r.RemoteAddr)
	peerAddr, err := netip.ParseAddr(peer)
	if err != nil {
		// Not an IP (unix socket, test double): forwarded headers cannot be
		// attributed, report the peer verbatim.
		return peer
	}
	peerAddr = peerAddr.Unmap()
	if !isTrusted(peerAddr, trusted) {
		return peer
	}

	hops := forwardedFor(r.Header.Values("X-Forwarded-For"))
	for i := len(hops) - 1; i >= 0; i-- {
		addr, err := netip.ParseAddr(hops[i])
		if err != nil {
			continue
		}
		addr = addr.Unmap()
		if !isTrusted(addr, trusted) {
			return addr.String()
		}
	}
	return peer
}

// IsSecure reports whether the request reached the server over HTTPS. A direct
// TLS connection always counts. X-Forwarded-Proto: https is honored only when
// the direct peer is a trusted proxy, so a client cannot claim HTTPS by sending
// the header itself.
func IsSecure(r *http.Request, trusted []netip.Prefix) bool {
	if r.TLS != nil {
		return true
	}
	peerAddr, err := netip.ParseAddr(peerHost(r.RemoteAddr))
	if err != nil || !isTrusted(peerAddr.Unmap(), trusted) {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(r.Header.Get("X-Forwarded-Proto")), "https")
}

// peerHost extracts the host from RemoteAddr, falling back to the raw value
// when it is not in host:port form. It mirrors the pre-proxy behavior so an
// untrusted peer keys exactly as before.
func peerHost(remoteAddr string) string {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		return remoteAddr
	}
	return host
}

// forwardedFor flattens every X-Forwarded-For header (which may repeat) into
// the ordered list of hop addresses, leftmost — the original client — first.
func forwardedFor(values []string) []string {
	var hops []string
	for _, value := range values {
		for _, part := range strings.Split(value, ",") {
			if part = strings.TrimSpace(part); part != "" {
				hops = append(hops, part)
			}
		}
	}
	return hops
}

// isTrusted reports whether addr falls inside any trusted prefix.
func isTrusted(addr netip.Addr, prefixes []netip.Prefix) bool {
	for _, prefix := range prefixes {
		if prefix.Contains(addr) {
			return true
		}
	}
	return false
}
