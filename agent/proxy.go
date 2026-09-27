package agent

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Node-side proxy layout defaults. The control plane's internal/proxy package
// owns the canonical values; they are duplicated rather than imported because
// agent/ must not import internal/ (cross-package scope rule). Changing one
// side without the other leaves Traefik reading an empty mount.
const (
	// defaultTraefikDir is the host directory the agent writes generated
	// Traefik configuration into (internal/proxy.TraefikDir).
	defaultTraefikDir = "/var/lib/gotham-agent/traefik"
	// defaultTraefikPingURL is the loopback-only Traefik ping endpoint
	// (internal/proxy.PingURL).
	defaultTraefikPingURL = "http://127.0.0.1:8080/ping"
)

// WriteProxyConfig bounds. The control plane is a trusted mTLS peer, but a
// compromised control plane must not be able to fill the node's disk or write
// outside the proxy directory, so paths and sizes are validated here too.
const (
	defaultProxyPingTimeout = 5 * time.Second
	maxProxyFileSize        = 1 << 20 // 1 MiB per document
	maxProxyFiles           = 16
)

// ProxyServerConfig wires a ProxyServer. Every field falls back to the node
// default when empty, so production passes a bare configuration and tests
// override Root and PingURL.
type ProxyServerConfig struct {
	// Root is the directory Traefik configuration is written under.
	Root string
	// PingURL is the Traefik ping endpoint a write is verified against.
	PingURL string
	// Client overrides the HTTP client used for pings (tests).
	Client *http.Client
	// PingTimeout bounds one ping; default 5 seconds.
	PingTimeout time.Duration
	// Logger defaults to slog.Default.
	Logger *slog.Logger
}

// ProxyServer implements agentv1.ProxyServiceServer: it writes the
// control-plane generated Traefik files on the node and, when asked, verifies
// the local Traefik instance answered its ping after the write.
type ProxyServer struct {
	agentv1.UnimplementedProxyServiceServer

	root        string
	pingURL     string
	client      *http.Client
	pingTimeout time.Duration
	log         *slog.Logger
}

// Compile-time guarantee that ProxyServer satisfies the agent service.
var _ agentv1.ProxyServiceServer = (*ProxyServer)(nil)

// NewProxyServer returns a ProxyServer with the configured (or default)
// layout.
func NewProxyServer(cfg ProxyServerConfig) *ProxyServer {
	root := strings.TrimSpace(cfg.Root)
	if root == "" {
		root = defaultTraefikDir
	}
	pingURL := strings.TrimSpace(cfg.PingURL)
	if pingURL == "" {
		pingURL = defaultTraefikPingURL
	}
	client := cfg.Client
	if client == nil {
		client = &http.Client{}
	}
	pingTimeout := cfg.PingTimeout
	if pingTimeout <= 0 {
		pingTimeout = defaultProxyPingTimeout
	}
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &ProxyServer{
		root:        root,
		pingURL:     pingURL,
		client:      client,
		pingTimeout: pingTimeout,
		log:         logger,
	}
}

// WriteProxyConfig writes the given Traefik configuration files under the
// node's proxy directory. Each write is atomic (temp file + rename), so a
// watcher never reads a half-written document. With verify set, the local
// Traefik ping is called after the writes; a ping failure is reported in the
// response (ping_error) rather than as an RPC error, because the files were
// written successfully and the control plane decides whether that is fatal.
func (s *ProxyServer) WriteProxyConfig(ctx context.Context, req *agentv1.WriteProxyConfigRequest) (*agentv1.WriteProxyConfigResponse, error) {
	files := req.GetFiles()
	if len(files) == 0 {
		return nil, status.Error(codes.InvalidArgument, "files are required")
	}
	if len(files) > maxProxyFiles {
		return nil, status.Errorf(codes.InvalidArgument, "too many files: %d (max %d)", len(files), maxProxyFiles)
	}

	written := make([]string, 0, len(files))
	for _, file := range files {
		rel, err := sanitizeProxyPath(file.GetPath())
		if err != nil {
			return nil, status.Error(codes.InvalidArgument, err.Error())
		}
		content := file.GetContent()
		if len(content) > maxProxyFileSize {
			return nil, status.Errorf(codes.InvalidArgument, "file %q exceeds %d bytes", rel, maxProxyFileSize)
		}
		if err := writeFileAtomic(filepath.Join(s.root, rel), content); err != nil {
			return nil, status.Errorf(codes.Internal, "write %s: %v", rel, err)
		}
		written = append(written, rel)
	}

	response := &agentv1.WriteProxyConfigResponse{Written: written}
	if req.GetVerify() {
		if err := s.ping(ctx); err != nil {
			response.PingError = err.Error()
			s.log.Warn("proxy: traefik ping failed after config write", "error", err)
		} else {
			response.Reloaded = true
		}
	}
	s.log.Info("proxy: configuration written", "files", written, "reloaded", response.GetReloaded())
	return response, nil
}

// ping calls the Traefik ping endpoint. A 200 response means the proxy is up
// and has loaded its configuration (Traefik serves /ping from its own
// process, not from the file provider, so it confirms the process, while the
// file provider's watch mode reloads the dynamic document without a restart).
func (s *ProxyServer) ping(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, s.pingTimeout)
	defer cancel()

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, s.pingURL, nil)
	if err != nil {
		return fmt.Errorf("traefik ping: %w", err)
	}
	response, err := s.client.Do(request)
	if err != nil {
		return fmt.Errorf("traefik ping: %w", err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("traefik ping: status %d", response.StatusCode)
	}
	return nil
}

// sanitizeProxyPath validates a document path: it must be relative, must not
// escape the proxy directory (no "..", no absolute path) and must not contain
// a backslash (a Windows separator that is a plain filename character on
// Linux and would make the path ambiguous between the two sides).
func sanitizeProxyPath(path string) (string, error) {
	raw := strings.TrimSpace(path)
	if raw == "" {
		return "", errors.New("path is required")
	}
	if filepath.IsAbs(raw) || strings.HasPrefix(raw, "/") {
		return "", fmt.Errorf("path %q must be relative", path)
	}
	cleaned := filepath.Clean(raw)
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path %q escapes the proxy directory", path)
	}
	if strings.Contains(cleaned, `\`) {
		return "", fmt.Errorf("path %q must not contain a backslash", path)
	}
	return cleaned, nil
}

// writeFileAtomic writes content to path through a temporary file in the same
// directory and renames it into place, so a watching Traefik never observes a
// partially written document.
func writeFileAtomic(path string, content []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".gotham-proxy-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()

	if _, err := tmp.Write(content); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o644); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}
