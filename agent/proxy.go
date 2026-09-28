package agent

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/sys/unix"

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
	if absolute, err := filepath.Abs(root); err == nil {
		root = absolute
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
// node's proxy directory. The whole batch is validated before the first
// write: every path and size is checked, and every target parent directory is
// opened relative to the trusted filesystem root with O_NOFOLLOW on each
// component (creating missing components with mkdirat), holding the directory
// descriptors until all writes are done. A filesystem-invalid later entry can
// therefore never replace an earlier document.
//
// The trust anchor is "/", not the configured root: only an absolute root is
// accepted, no component between / and a target may be a symlink, and each
// document is written atomically (temp file + rename) through its held
// directory descriptor. Limits: absolute roots only; no cross-input-directory
// rename; the root itself and every ancestor must be real directories.
//
// With verify set, the local Traefik ping is called after the writes. A ping
// proves the proxy process answered; it does not prove Traefik accepted the
// document (the file provider reloads asynchronously and validates on its
// own), so the response is a liveness signal, not a configuration-acceptance
// claim.
func (s *ProxyServer) WriteProxyConfig(ctx context.Context, req *agentv1.WriteProxyConfigRequest) (*agentv1.WriteProxyConfigResponse, error) {
	files := req.GetFiles()
	if len(files) == 0 {
		return nil, status.Error(codes.InvalidArgument, "files are required")
	}
	if len(files) > maxProxyFiles {
		return nil, status.Errorf(codes.InvalidArgument, "too many files: %d (max %d)", len(files), maxProxyFiles)
	}

	// Pass 1: validate every path and size, then open (and hold) every target
	// parent directory relative to the trusted root. Nothing is written yet.
	type document struct {
		rel     string
		name    string
		content []byte
		dir     *os.File
	}
	documents := make([]document, 0, len(files))
	closeDocuments := func() {
		for _, document := range documents {
			if document.dir != nil {
				_ = document.dir.Close()
			}
		}
	}
	for _, file := range files {
		rel, err := sanitizeProxyPath(file.GetPath())
		if err != nil {
			closeDocuments()
			return nil, status.Error(codes.InvalidArgument, err.Error())
		}
		content := file.GetContent()
		if len(content) > maxProxyFileSize {
			closeDocuments()
			return nil, status.Errorf(codes.InvalidArgument, "file %q exceeds %d bytes", rel, maxProxyFileSize)
		}
		dir, err := openTrustedDir(filepath.Dir(filepath.Join(s.root, rel)))
		if err != nil {
			closeDocuments()
			return nil, status.Errorf(codes.InvalidArgument, "confine %s: %v", rel, err)
		}
		documents = append(documents, document{rel: rel, name: filepath.Base(rel), content: content, dir: dir})
	}
	defer closeDocuments()

	// Pass 2: replace content only after every target parent is validated and
	// held open.
	written := make([]string, 0, len(documents))
	for _, document := range documents {
		if err := writeFileInDir(document.dir, document.name, document.content); err != nil {
			return nil, status.Errorf(codes.Internal, "write %s: %v", document.rel, err)
		}
		written = append(written, document.rel)
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
	s.log.Info("proxy: configuration written",
		"files", written, "ping_ok", response.GetReloaded())
	return response, nil
}

// openTrustedDir opens the absolute directory dir by walking every component
// from the trusted root descriptor "/" with O_NOFOLLOW, creating missing
// components with mkdirat relative to the held parent descriptor. A symlink
// anywhere between / and dir (including dir itself) is rejected, so a
// concurrently swapped ancestor cannot redirect a later write. The caller owns
// the returned descriptor.
func openTrustedDir(dir string) (*os.File, error) {
	if !filepath.IsAbs(dir) {
		return nil, fmt.Errorf("%s must be an absolute path", dir)
	}
	fd, err := unix.Open("/", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, fmt.Errorf("open trust anchor: %w", err)
	}
	current := "/"
	for _, component := range strings.Split(strings.TrimPrefix(filepath.Clean(dir), "/"), "/") {
		if component == "" || component == "." {
			continue
		}
		current = filepath.Join(current, component)
		next, openErr := unix.Openat(fd, component, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		if errors.Is(openErr, unix.ENOENT) {
			if mkErr := unix.Mkdirat(fd, component, 0o755); mkErr != nil && !errors.Is(mkErr, unix.EEXIST) {
				_ = unix.Close(fd)
				return nil, fmt.Errorf("create %s: %w", current, mkErr)
			}
			next, openErr = unix.Openat(fd, component, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		}
		if openErr != nil {
			_ = unix.Close(fd)
			return nil, fmt.Errorf("%s must be a real directory: %w", current, openErr)
		}
		_ = unix.Close(fd)
		fd = next
	}
	return os.NewFile(uintptr(fd), dir), nil
}

// writeFileInDir writes content as name inside the held directory descriptor
// through a fresh temporary file and renameat, so a watcher never reads a
// half-written document and the final entry is replaced, never followed.
func writeFileInDir(dir *os.File, name string, content []byte) error {
	dirFD := int(dir.Fd())
	suffix := make([]byte, 8)
	if _, err := rand.Read(suffix); err != nil {
		return err
	}
	tmpName := ".gotham-proxy-" + hex.EncodeToString(suffix)

	fd, err := unix.Openat(dirFD, tmpName, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0o644)
	if err != nil {
		return err
	}
	file := os.NewFile(uintptr(fd), filepath.Join(dir.Name(), tmpName))
	if _, err := file.Write(content); err != nil {
		_ = file.Close()
		_ = unix.Unlinkat(dirFD, tmpName, 0)
		return err
	}
	if err := file.Close(); err != nil {
		_ = unix.Unlinkat(dirFD, tmpName, 0)
		return err
	}
	if err := unix.Renameat(dirFD, tmpName, dirFD, name); err != nil {
		_ = unix.Unlinkat(dirFD, tmpName, 0)
		return err
	}
	return nil
}

// ping calls the Traefik ping endpoint. A 200 response means the proxy
// process is up and answering; Traefik documents /ping as process liveness,
// and the file provider reloads and validates configuration asynchronously,
// so this is never a claim that a particular document was accepted.
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
