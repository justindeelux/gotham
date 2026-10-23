package docker

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"strings"
)

const DefaultDockerSocket = "/var/run/docker.sock"

func newDockerTransport(target string) (*http.Transport, string, error) {
	target = strings.TrimSpace(target)
	if target == "" {
		target = "unix://" + DefaultDockerSocket
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
