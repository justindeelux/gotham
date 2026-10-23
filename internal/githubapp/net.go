package githubapp

import (
	"context"
	"fmt"
	"net"
	"regexp"
	"time"
)

var errRedirect = fmt.Errorf("%w: redirects are not followed", ErrValidation)

var manifestCodePattern = regexp.MustCompile(`/app-manifests/[^/]+/conversions`)

const resolveTimeout = 5 * time.Second

var deniedRanges = []string{
	"100.64.0.0/10",
	"192.0.0.0/24",
	"198.18.0.0/15",
	"64:ff9b::/96",
	"2002::/16",
}

func deniedNets() []*net.IPNet {
	nets := make([]*net.IPNet, 0, len(deniedRanges))
	for _, cidr := range deniedRanges {
		if _, net, err := net.ParseCIDR(cidr); err == nil {
			nets = append(nets, net)
		}
	}
	return nets
}

func isPublicIP(ip net.IP) bool {
	if ip == nil {
		return false
	}
	if unwrapped := ip.To4(); unwrapped != nil {
		ip = unwrapped
	}
	if !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() ||
		ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsMulticast() || ip.IsUnspecified() {
		return false
	}
	for _, denied := range deniedNets() {
		if denied.Contains(ip) {
			return false
		}
	}
	return true
}

func guardHost(hostname string) error {
	return guardHostAllow(hostname, false)
}

func guardHostAllow(hostname string, allowUnsafe bool) error {
	if allowUnsafe {
		return nil
	}
	ips, err := resolveHost(hostname)
	if err != nil {
		return err
	}
	for _, ip := range ips {
		if !isPublicIP(ip) {
			return fmt.Errorf("%w: refusing non-public github host", ErrValidation)
		}
	}
	return nil
}

func resolveHost(hostname string) ([]net.IP, error) {
	if ip := net.ParseIP(hostname); ip != nil {
		return []net.IP{ip}, nil
	}
	if hostname == "" {
		return nil, fmt.Errorf("%w: github host is empty", ErrValidation)
	}
	ctx, cancel := context.WithTimeout(context.Background(), resolveTimeout)
	defer cancel()
	ips, err := net.DefaultResolver.LookupIP(ctx, "ip", hostname)
	if err != nil {
		return nil, fmt.Errorf("%w: resolve github host: %v", ErrValidation, err)
	}
	if len(ips) == 0 {
		return nil, fmt.Errorf("%w: github host resolves to nothing", ErrValidation)
	}
	return ips, nil
}

func guardedDialer(allowUnsafe bool) func(ctx context.Context, network, addr string) (net.Conn, error) {
	dialer := &net.Dialer{Timeout: 10 * time.Second}
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		if !allowUnsafe {
			host, _, err := net.SplitHostPort(addr)
			if err != nil {
				return nil, fmt.Errorf("%w: split dial address: %v", ErrValidation, err)
			}
			if err := guardHostAllow(host, false); err != nil {
				return nil, err
			}
		}
		return dialer.DialContext(ctx, network, addr)
	}
}
