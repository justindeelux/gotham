package clientip

import (
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"strings"
)

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
			prefix, err = unmapPrefix(prefix)
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

func ClientIP(r *http.Request, trusted []netip.Prefix) string {
	peer := peerHost(r.RemoteAddr)
	peerAddr, err := netip.ParseAddr(peer)
	if err != nil {
		return peer
	}
	peerAddr = peerAddr.Unmap()
	if !isTrusted(peerAddr, trusted) {
		return peer
	}

	hops := forwardedFor(r.Header.Values("X-Forwarded-For"))
	for i := len(hops) - 1; i >= 0; i-- {
		addr, ok := parseHop(hops[i])
		if !ok {
			return peer
		}
		if !isTrusted(addr, trusted) {
			return addr.String()
		}
	}
	return peer
}

func IsSecure(r *http.Request, trusted []netip.Prefix) bool {
	if r.TLS != nil {
		return true
	}
	peerAddr, err := netip.ParseAddr(peerHost(r.RemoteAddr))
	if err != nil || !isTrusted(peerAddr.Unmap(), trusted) {
		return false
	}
	return strings.EqualFold(lastForwardedProto(r.Header.Values("X-Forwarded-Proto")), "https")
}

func peerHost(remoteAddr string) string {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		return remoteAddr
	}
	return host
}

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

func lastForwardedProto(values []string) string {
	last := ""
	for _, value := range values {
		for _, part := range strings.Split(value, ",") {
			if part = strings.TrimSpace(part); part != "" {
				last = part
			}
		}
	}
	return last
}

func parseHop(raw string) (netip.Addr, bool) {
	entry := strings.TrimSpace(raw)
	if entry == "" {
		return netip.Addr{}, false
	}
	if host, _, err := net.SplitHostPort(entry); err == nil {
		entry = host
	} else {
		entry = strings.Trim(entry, "[]")
	}
	addr, err := netip.ParseAddr(entry)
	if err != nil {
		return netip.Addr{}, false
	}
	return addr.Unmap(), true
}

func unmapPrefix(prefix netip.Prefix) (netip.Prefix, error) {
	if !prefix.Addr().Is4In6() {
		return prefix, nil
	}
	bits := prefix.Bits() - 96
	if bits < 0 {
		return netip.Prefix{}, fmt.Errorf("invalid mapped prefix length %d", prefix.Bits())
	}
	return netip.PrefixFrom(prefix.Addr().Unmap(), bits), nil
}

func isTrusted(addr netip.Addr, prefixes []netip.Prefix) bool {
	for _, prefix := range prefixes {
		if prefix.Contains(addr) {
			return true
		}
	}
	return false
}
