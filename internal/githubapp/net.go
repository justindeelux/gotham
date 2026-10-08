package githubapp

import (
	"fmt"
	"net"
)

// guardHost refuses non-HTTPS-friendly outbound hosts: production GitHub traffic
// must never be redirected at link-local, loopback or private addresses.
// Tests pass allowUnsafe to reach their httptest fakes.
func guardHost(host string) error {
	ip := net.ParseIP(host)
	if ip == nil {
		return nil
	}
	if ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsPrivate() {
		return fmt.Errorf("%w: refusing non-public github host", ErrValidation)
	}
	return nil
}
