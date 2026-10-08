package githubapp

import (
	"context"
	"fmt"
	"net"
	"regexp"
	"time"
)

// errRedirect is returned when the GitHub API redirects: credentials must
// never follow it to another host.
var errRedirect = fmt.Errorf("%w: redirects are not followed", ErrValidation)

// manifestCodePattern matches the manifest conversion path segment carrying
// the single-use code, so it can be redacted from errors and logs.
var manifestCodePattern = regexp.MustCompile(`/app-manifests/[^/]+/conversions`)

// resolveTimeout bounds DNS resolution for the SSRF guard.
const resolveTimeout = 5 * time.Second

// isPublicIP reports whether ip is a routable public address. Loopback,
// link-local (including the 169.254.169.254 metadata endpoint), private,
// multicast and unspecified addresses are never valid GitHub hosts.
func isPublicIP(ip net.IP) bool {
	if ip == nil {
		return false
	}
	return !ip.IsLoopback() && !ip.IsLinkLocalUnicast() && !ip.IsLinkLocalMulticast() &&
		!ip.IsMulticast() && !ip.IsUnspecified() && !ip.IsPrivate()
}

// guardHost resolves hostname and rejects it unless every address is public.
// Literal IPs are checked directly; DNS names resolve first, so a name
// pointing at private space fails. allowUnsafe bypasses the check for the
// loopback fakes tests use; production leaves it false.
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

// resolveHost returns the IPs hostname resolves to: a literal IP directly,
// otherwise a bounded DNS lookup.
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

// guardedDialer dials only public IPs, so even a DNS change between the
// resolve-time check and the connection cannot reach private space.
// allowUnsafe bypasses it for test fakes.
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
