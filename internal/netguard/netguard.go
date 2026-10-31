// Package netguard holds the address classes the control plane refuses to
// contact on behalf of an untrusted target. Resolution happens at dial time so
// a DNS answer that changes after validation cannot redirect the connection.
package netguard

import (
	"errors"
	"net"
	"strings"
	"syscall"
)

// ErrDenied is returned when a dial target falls in a denied address class.
var ErrDenied = errors.New("outbound destination is not allowed")

var alwaysDeny = mustCIDRs(
	"0.0.0.0/8",         // this-network and unspecified
	"100.64.0.0/10",     // carrier-grade NAT, Tailscale
	"169.254.0.0/16",    // link-local and cloud metadata
	"192.0.0.0/24",      // IETF protocol assignments
	"198.18.0.0/15",     // benchmarking
	"224.0.0.0/4",       // multicast
	"240.0.0.0/4",       // reserved
	"::/96",             // IPv4-compatible
	"64:ff9b::/96",      // NAT64
	"64:ff9b:1::/48",    // local-use NAT64
	"2002::/16",         // 6to4
	"fd00:ec2::254/128", // AWS instance metadata over IPv6
	"fe80::/10",         // link-local
	"ff00::/8",          // multicast
)

var privateNets = mustCIDRs(
	"127.0.0.0/8",
	"10.0.0.0/8",
	"172.16.0.0/12",
	"192.168.0.0/16",
	"::1/128",
	"fc00::/7",
)

func mustCIDRs(cidrs ...string) []*net.IPNet {
	nets := make([]*net.IPNet, 0, len(cidrs))
	for _, cidr := range cidrs {
		_, parsed, err := net.ParseCIDR(cidr)
		if err != nil {
			panic("netguard: invalid CIDR " + cidr)
		}
		nets = append(nets, parsed)
	}
	return nets
}

// Denied reports whether ip must not be contacted. allowPrivate exempts
// loopback, RFC1918 and unique-local addresses for owner-configured targets;
// metadata, CGNAT, reserved and multicast ranges stay denied either way.
func Denied(ip net.IP, allowPrivate bool) bool {
	if ip == nil || ip.IsUnspecified() {
		return true
	}
	// IPv6 loopback shares ::/96 with the always-denied IPv4-compatible range.
	if ip.IsLoopback() && allowPrivate {
		return false
	}
	if v4 := ip.To4(); v4 != nil {
		ip = v4
	}
	for _, denied := range alwaysDeny {
		if denied.Contains(ip) {
			return true
		}
	}
	if allowPrivate {
		return false
	}
	for _, denied := range privateNets {
		if denied.Contains(ip) {
			return true
		}
	}
	return false
}

// Control is a net.Dialer.Control that refuses denied addresses after the
// dialer has resolved a hostname, so a short-TTL answer cannot bypass the
// policy applied during validation.
func Control(allowPrivate bool) func(network, address string, _ syscall.RawConn) error {
	return func(_, address string, _ syscall.RawConn) error {
		host, _, err := net.SplitHostPort(address)
		if err != nil {
			host = address
		}
		host = strings.TrimSuffix(strings.TrimPrefix(host, "["), "]")
		if zone := strings.IndexByte(host, '%'); zone >= 0 {
			host = host[:zone]
		}
		ip := net.ParseIP(host)
		if ip != nil && Denied(ip, allowPrivate) {
			return ErrDenied
		}
		return nil
	}
}
