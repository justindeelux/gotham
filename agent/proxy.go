package agent

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"golang.org/x/sys/unix"

	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Node-side proxy layout defaults. The control plane's internal/proxy package
// owns the canonical values; they are duplicated rather than imported because
// agent/ must not import internal/ (cross-package scope rule). Changing one
// side without the other leaves Traefik reading an empty mount.
const (
	// defaultTraefikDir is the host directory the agent writes generated
	// Traefik configuration into (internal/proxy.TraefikDir). The ACME
	// storage defaults to its "acme" subdirectory
	// (internal/proxy.TraefikAcmeDir).
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
	// maxACMEStorageSize bounds the ACME storage file the agent is willing to
	// read into memory. acme.json holds every certificate and key the node
	// ever stored; a node with thousands of certificates still stays far
	// below this.
	maxACMEStorageSize = 16 << 20 // 16 MiB
	// maxACMECertificates bounds one read so a hostile or corrupt file cannot
	// turn the response into an unbounded message.
	maxACMECertificates = 4096
)

// ProxyServerConfig wires a ProxyServer. Every field falls back to the node
// default when empty, so production passes a bare configuration and tests
// override Root, AcmeDir and PingURL.
type ProxyServerConfig struct {
	// Root is the directory Traefik configuration is written under.
	Root string
	// AcmeDir is the directory holding the ACME storage (acme.json) that
	// ReadACMEStorage reads; it defaults to Root + "/acme" (the production
	// layout where internal/proxy.AcmeDir sits under TraefikDir).
	AcmeDir string
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
// the local Traefik instance answered its ping after the write. It also reads
// the node's ACME storage and reports certificate metadata only (BE-6.3).
type ProxyServer struct {
	agentv1.UnimplementedProxyServiceServer

	root        string
	acmeDir     string
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
	acmeDir := strings.TrimSpace(cfg.AcmeDir)
	if acmeDir == "" {
		// The production layout keeps the ACME storage at TraefikDir/acme, so
		// a relocated root keeps its ACME state under the relocated root
		// (BE-6.3); an explicit AcmeDir overrides the derivation.
		acmeDir = filepath.Join(root, "acme")
	}
	if absolute, err := filepath.Abs(acmeDir); err == nil {
		acmeDir = absolute
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
		acmeDir:     acmeDir,
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
		dir, err := openTrustedDir(filepath.Dir(filepath.Join(s.root, rel)), true)
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

	// The ACME storage file is prepared before Traefik can ever start (the
	// static configuration must exist first), so the agent owns a readable
	// storage file from the beginning. Best effort: a node whose ACME
	// directory is not writable still gets its configuration, and the status
	// read reports unknown instead of fabricating a value.
	if err := s.ensureACMEStorage(); err != nil {
		s.log.Warn("proxy: cannot prepare acme storage", "error", err)
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

// ReadACMEStorage reads the node's Traefik ACME storage and reports one entry
// per stored certificate: the resolver it belongs to, the primary domain, the
// subject alternative names and the leaf's expiry.
//
// Security boundary: acme.json holds private certificate keys and the ACME
// account key. The file is parsed with decode structs that structurally omit
// every key field, so key material never enters the response, a log line or an
// error string; only the certificate's expiry is decoded from the stored
// certificate bytes, and those bytes never leave this function. The read
// walks every path component from "/" with O_NOFOLLOW (creating nothing) and
// opens the file itself with O_NOFOLLOW, so a symlink swapped in on the node
// cannot redirect the read outside the ACME directory. A missing file is
// present=false, not an error; an oversized file, a symlinked path or a
// malformed entry is an error (the control plane then reports "unknown"
// instead of fabricating a status).
func (s *ProxyServer) ReadACMEStorage(ctx context.Context, _ *agentv1.ReadACMEStorageRequest) (*agentv1.ReadACMEStorageResponse, error) {
	if err := ctx.Err(); err != nil {
		return nil, status.FromContextError(err).Err()
	}
	dir, err := openTrustedDir(s.acmeDir, false)
	if err != nil {
		if errors.Is(err, unix.ENOENT) {
			return &agentv1.ReadACMEStorageResponse{}, nil
		}
		return nil, status.Errorf(codes.Internal, "read acme storage: %v", err)
	}
	defer func() { _ = dir.Close() }()

	fd, err := unix.Openat(int(dir.Fd()), "acme.json", unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		if errors.Is(err, unix.ENOENT) {
			return &agentv1.ReadACMEStorageResponse{}, nil
		}
		return nil, status.Errorf(codes.Internal, "read acme storage: %v", err)
	}
	file := os.NewFile(uintptr(fd), "acme.json")
	defer func() { _ = file.Close() }()

	// The size cap is enforced while reading, so a hostile file cannot make
	// the agent allocate without bound.
	content, err := io.ReadAll(io.LimitReader(file, maxACMEStorageSize+1))
	if err != nil {
		return nil, status.Errorf(codes.Internal, "read acme storage: %v", err)
	}
	if len(content) > maxACMEStorageSize {
		return nil, status.Errorf(codes.Internal, "read acme storage: file exceeds %d bytes", maxACMEStorageSize)
	}
	// Traefik creates the storage file empty at boot and fills it on the
	// first issuance; an empty (or whitespace-only) file means no storage
	// content yet, not a malformed one.
	if len(bytes.TrimSpace(content)) == 0 {
		return &agentv1.ReadACMEStorageResponse{}, nil
	}

	storage := acmeStorage{}
	if err := json.Unmarshal(content, &storage); err != nil {
		// The decoder reports offsets and type names only; the content is
		// never echoed.
		return nil, status.Errorf(codes.Internal, "read acme storage: malformed storage: %v", err)
	}
	response := &agentv1.ReadACMEStorageResponse{Present: true}
	resolvers := make([]string, 0, len(storage))
	for resolver := range storage {
		resolvers = append(resolvers, resolver)
	}
	sort.Strings(resolvers)
	for _, resolver := range resolvers {
		for _, stored := range storage[resolver].Certificates {
			if len(response.Certificates) >= maxACMECertificates {
				return nil, status.Errorf(codes.Internal, "read acme storage: more than %d certificates", maxACMECertificates)
			}
			info, err := certificateInfo(resolver, stored)
			if err != nil {
				return nil, status.Errorf(codes.Internal, "read acme storage: %v", err)
			}
			response.Certificates = append(response.Certificates, info)
		}
	}
	s.log.Info("proxy: acme storage read",
		"present", true, "certificates", len(response.Certificates))
	return response, nil
}

// acmeStorage is the subset of Traefik's acme.json the agent decodes. Key
// fields (the per-certificate "key" and the resolver's "Account" including
// its private key) are deliberately absent from these structs: encoding/json
// ignores unknown fields, so key material is never decoded into a Go value
// here, let alone returned or logged.
type acmeStorage map[string]acmeResolver

// acmeResolver is one resolver section of the storage file.
type acmeResolver struct {
	Certificates []acmeStoredCertificate `json:"Certificates"`
}

// acmeStoredCertificate is one stored certificate entry reduced to the
// non-secret fields: the recorded names and the certificate chain bytes.
type acmeStoredCertificate struct {
	Domain struct {
		Main string   `json:"main"`
		SANs []string `json:"sans"`
	} `json:"domain"`
	// Certificate is the base64-encoded PEM leaf chain Traefik stored.
	Certificate []byte `json:"certificate"`
}

// certificateInfo reduces one stored certificate to its non-secret metadata:
// the expiry comes from parsing the leaf, the names from the stored domain
// record.
func certificateInfo(resolver string, stored acmeStoredCertificate) (*agentv1.ACMECertificateInfo, error) {
	leaf, err := parseLeafCertificate(stored.Certificate)
	if err != nil {
		return nil, err
	}
	return &agentv1.ACMECertificateInfo{
		Resolver: resolver,
		Main:     stored.Domain.Main,
		Sans:     append([]string{}, stored.Domain.SANs...),
		NotAfter: timestamppb.New(leaf.NotAfter),
	}, nil
}

// parseLeafCertificate decodes the stored certificate bytes (base64-PEM as
// Traefik writes them, raw DER tolerated) and parses the leaf. The error text
// never contains certificate bytes.
func parseLeafCertificate(stored []byte) (*x509.Certificate, error) {
	der := stored
	if block, _ := pem.Decode(stored); block != nil {
		der = block.Bytes
	}
	leaf, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, fmt.Errorf("stored certificate is not parseable: %w", err)
	}
	return leaf, nil
}

// openTrustedDir opens the absolute directory dir by walking every component
// from the trusted root descriptor "/" with O_NOFOLLOW. With create set,
// missing components are created with mkdirat relative to the held parent
// descriptor (the write path); without it the walk is strictly read-only and
// a missing component is an ENOENT error (the ACME read path). A symlink
// anywhere between / and dir (including dir itself) is rejected, so a
// concurrently swapped ancestor cannot redirect a later write or read. The
// caller owns the returned descriptor.
func openTrustedDir(dir string, create bool) (*os.File, error) {
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
		if create && errors.Is(openErr, unix.ENOENT) {
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

// ensureACMEStorage creates the node's ACME storage file when it does not
// exist yet, before the Traefik container is ever started. Traefik writes the
// file with mode 0600 and os.WriteFile keeps an existing file's mode and
// ownership, so a storage file the agent created stays readable by the agent
// after Traefik writes certificates into it; without it, a root-owned 0600
// file would make every status read fail (unknown, never fabricated). The
// creation is confined by the same no-follow traversal as the writes, never
// truncates or replaces an existing file (O_EXCL), and keeps the tight 0600
// mode: the storage file holds private keys, so no other local user may read
// it.
func (s *ProxyServer) ensureACMEStorage() error {
	dir, err := openTrustedDir(s.acmeDir, true)
	if err != nil {
		return err
	}
	defer func() { _ = dir.Close() }()

	fd, err := unix.Openat(int(dir.Fd()), "acme.json",
		unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0o600)
	if errors.Is(err, unix.EEXIST) {
		// An existing storage file is never touched: it may already hold
		// certificates (and, on a legacy node, may be owned by another user).
		return nil
	}
	if err != nil {
		return fmt.Errorf("create %s/acme.json: %w", s.acmeDir, err)
	}
	return unix.Close(fd)
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
// escape the proxy directory (no "..", no absolute path), must not contain a
// backslash (a Windows separator that is a plain filename character on Linux
// and would make the path ambiguous between the two sides) and must not
// contain a NUL byte (the kernel rejects NUL in a filename, which would fail
// the write pass only after earlier documents of the batch were replaced).
func sanitizeProxyPath(path string) (string, error) {
	raw := strings.TrimSpace(path)
	if raw == "" {
		return "", errors.New("path is required")
	}
	if strings.ContainsRune(raw, 0) {
		return "", fmt.Errorf("path %q must not contain a NUL byte", path)
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
