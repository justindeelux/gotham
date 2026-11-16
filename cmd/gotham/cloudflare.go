package main

import (
	"net"
	"net/netip"
)

// cloudflareProxyPrefixes is Cloudflare's published proxy address space
// (https://www.cloudflare.com/ips/). A panel domain that still resolves into
// it is proxied at the edge, so the HTTP-01 challenge never reaches this host:
// the domain step refuses it with the DNS-only instruction instead of leaving
// HTTPS broken after the install.
// shortcut: static snapshot, refresh it when Cloudflare publishes new ranges.
var cloudflareProxyPrefixes = []netip.Prefix{
	netip.MustParsePrefix("103.21.244.0/22"),
	netip.MustParsePrefix("103.22.200.0/22"),
	netip.MustParsePrefix("103.31.4.0/22"),
	netip.MustParsePrefix("104.16.0.0/13"),
	netip.MustParsePrefix("104.24.0.0/14"),
	netip.MustParsePrefix("108.162.192.0/18"),
	netip.MustParsePrefix("131.0.72.0/22"),
	netip.MustParsePrefix("141.101.64.0/18"),
	netip.MustParsePrefix("162.158.0.0/15"),
	netip.MustParsePrefix("172.64.0.0/13"),
	netip.MustParsePrefix("173.245.48.0/20"),
	netip.MustParsePrefix("188.114.96.0/20"),
	netip.MustParsePrefix("190.93.240.0/20"),
	netip.MustParsePrefix("197.234.240.0/22"),
	netip.MustParsePrefix("198.41.128.0/17"),
	netip.MustParsePrefix("2400:cb00::/32"),
	netip.MustParsePrefix("2405:b500::/32"),
	netip.MustParsePrefix("2405:8100::/32"),
	netip.MustParsePrefix("2606:4700::/32"),
	netip.MustParsePrefix("2803:f800::/32"),
	netip.MustParsePrefix("2a06:98c0::/29"),
	netip.MustParsePrefix("2c0f:f248::/32"),
}

// cloudflareProxyAddrs returns the resolved addresses that belong to Cloudflare.
func cloudflareProxyAddrs(addrs []net.IPAddr) []string {
	var proxied []string
	for _, addr := range addrs {
		ip, ok := netip.AddrFromSlice(addr.IP)
		if !ok || !isCloudflareProxyAddr(ip.Unmap()) {
			continue
		}
		proxied = append(proxied, addr.IP.String())
	}
	return proxied
}

func isCloudflareProxyAddr(addr netip.Addr) bool {
	for _, prefix := range cloudflareProxyPrefixes {
		if prefix.Contains(addr) {
			return true
		}
	}
	return false
}
