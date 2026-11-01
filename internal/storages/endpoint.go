package storages

import (
	"context"
	"fmt"
	"net"
	"strings"

	"github.com/justindeelux/gotham/internal/netguard"
)

// lookupHost is a variable so tests can exercise a DNS answer without network.
var lookupHost = net.DefaultResolver.LookupIPAddr

// validateEndpoint applies the outbound-host policy to an S3 endpoint. Private
// and loopback targets stay allowed for self-hosted MinIO; metadata, CGNAT,
// link-local and other reserved ranges are refused. A resolver failure is not
// fatal here: the dial-time control is authoritative and still refuses a
// rebinding answer, while an unreachable host is reported by the test button.
func validateEndpoint(ctx context.Context, endpoint string) error {
	raw, _, err := SplitEndpoint(endpoint)
	if err != nil {
		return err
	}
	host := raw
	if withoutPort, _, splitErr := net.SplitHostPort(raw); splitErr == nil {
		host = withoutPort
	}
	host = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(host, "["), "]"))
	if host == "" {
		return fmt.Errorf("%w: s3 endpoint has no host", ErrValidation)
	}
	if strings.Contains(host, "%") {
		return fmt.Errorf("%w: s3 endpoint host must not carry an IPv6 zone id", ErrValidation)
	}
	if ip := net.ParseIP(host); ip != nil {
		if netguard.Denied(ip, true) {
			return fmt.Errorf("%w: s3 endpoint host is not allowed", ErrValidation)
		}
		return nil
	}
	addresses, err := lookupHost(ctx, host)
	if err != nil {
		return nil
	}
	for _, address := range addresses {
		if netguard.Denied(address.IP, true) {
			return fmt.Errorf("%w: s3 endpoint host is not allowed", ErrValidation)
		}
	}
	return nil
}
