package agent

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"strings"
)

// newDockerTransport resolves a Docker endpoint into an HTTP transport and base
// URL. Accepted forms:
//
//   - ""                     → unix:///var/run/docker.sock
//   - "unix:///path.sock"    → unix socket
//   - "/path.sock"           → unix socket
//   - "tcp://host:port"      → TCP
//   - "http(s)://host:port"  → TCP
func newDockerTransport(target string) (*http.Transport, string, error) {
	target = strings.TrimSpace(target)
	if target == "" {
		target = "unix://" + defaultDockerSock
	}

	switch {
	case strings.HasPrefix(target, "unix://"):
		return unixTransport(strings.TrimPrefix(target, "unix://"))
	case strings.HasPrefix(target, "/"):
		return unixTransport(target)
	case strings.HasPrefix(target, "tcp://"):
		address := strings.TrimPrefix(target, "tcp://")
		if address == "" {
			return nil, "", fmt.Errorf("docker: empty tcp address")
		}
		return &http.Transport{DisableCompression: true}, "http://" + address, nil
	case strings.HasPrefix(target, "http://"), strings.HasPrefix(target, "https://"):
		return &http.Transport{DisableCompression: true}, target, nil
	default:
		return nil, "", fmt.Errorf("docker: unsupported endpoint %q", target)
	}
}

// unixTransport builds a transport that dials the Docker unix socket at path.
func unixTransport(path string) (*http.Transport, string, error) {
	if strings.TrimSpace(path) == "" {
		return nil, "", fmt.Errorf("docker: empty unix socket path")
	}
	transport := &http.Transport{
		DisableCompression: true,
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			var dialer net.Dialer
			return dialer.DialContext(ctx, "unix", path)
		},
	}
	return transport, "http://docker", nil
}
