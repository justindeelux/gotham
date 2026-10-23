package proxy

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

const (
	DefaultRoot    = "/var/lib/gotham-agent/traefik"
	ContainerName  = "gotham-traefik"
	DefaultPingURL = "http://127.0.0.1:8080/ping"
)

const (
	defaultProxyPingTimeout = 5 * time.Second
	maxProxyFileSize        = 1 << 20
	maxProxyFiles           = 16
	maxACMEStorageSize      = 16 << 20
	maxACMECertificates     = 4096
)

type Config struct {
	Root        string
	AcmeDir     string
	PingURL     string
	Client      *http.Client
	PingTimeout time.Duration
	Logger      *slog.Logger
}

type Server struct {
	agentv1.UnimplementedProxyServiceServer

	root        string
	acmeDir     string
	pingURL     string
	client      *http.Client
	pingTimeout time.Duration
	log         *slog.Logger
}

var _ agentv1.ProxyServiceServer = (*Server)(nil)

func New(cfg Config) *Server {
	root := strings.TrimSpace(cfg.Root)
	if root == "" {
		root = DefaultRoot
	}
	if absolute, err := filepath.Abs(root); err == nil {
		root = absolute
	}
	acmeDir := strings.TrimSpace(cfg.AcmeDir)
	if acmeDir == "" {
		acmeDir = filepath.Join(root, "acme")
	}
	if absolute, err := filepath.Abs(acmeDir); err == nil {
		acmeDir = absolute
	}
	pingURL := strings.TrimSpace(cfg.PingURL)
	if pingURL == "" {
		pingURL = DefaultPingURL
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
	return &Server{
		root:        root,
		acmeDir:     acmeDir,
		pingURL:     pingURL,
		client:      client,
		pingTimeout: pingTimeout,
		log:         logger,
	}
}

func (s *Server) WriteProxyConfig(ctx context.Context, req *agentv1.WriteProxyConfigRequest) (*agentv1.WriteProxyConfigResponse, error) {
	files := req.GetFiles()
	if len(files) == 0 {
		return nil, status.Error(codes.InvalidArgument, "files are required")
	}
	if len(files) > maxProxyFiles {
		return nil, status.Errorf(codes.InvalidArgument, "too many files: %d (max %d)", len(files), maxProxyFiles)
	}

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
		dir, err := OpenTrustedDir(filepath.Dir(filepath.Join(s.root, rel)), true)
		if err != nil {
			closeDocuments()
			return nil, status.Errorf(codes.InvalidArgument, "confine %s: %v", rel, err)
		}
		documents = append(documents, document{rel: rel, name: filepath.Base(rel), content: content, dir: dir})
	}
	defer closeDocuments()

	written := make([]string, 0, len(documents))
	for _, document := range documents {
		if err := WriteFileInDir(document.dir, document.name, document.content, 0o644); err != nil {
			return nil, status.Errorf(codes.Internal, "write %s: %v", document.rel, err)
		}
		written = append(written, document.rel)
	}

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

func (s *Server) ReadACMEStorage(ctx context.Context, _ *agentv1.ReadACMEStorageRequest) (*agentv1.ReadACMEStorageResponse, error) {
	if err := ctx.Err(); err != nil {
		return nil, status.FromContextError(err).Err()
	}
	dir, err := OpenTrustedDir(s.acmeDir, false)
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

	content, err := io.ReadAll(io.LimitReader(file, maxACMEStorageSize+1))
	if err != nil {
		return nil, status.Errorf(codes.Internal, "read acme storage: %v", err)
	}
	if len(content) > maxACMEStorageSize {
		return nil, status.Errorf(codes.Internal, "read acme storage: file exceeds %d bytes", maxACMEStorageSize)
	}
	if len(bytes.TrimSpace(content)) == 0 {
		return &agentv1.ReadACMEStorageResponse{}, nil
	}

	storage := acmeStorage{}
	if err := json.Unmarshal(content, &storage); err != nil {
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

type acmeStorage map[string]acmeResolver

type acmeResolver struct {
	Certificates []acmeStoredCertificate `json:"Certificates"`
}

type acmeStoredCertificate struct {
	Domain struct {
		Main string   `json:"main"`
		SANs []string `json:"sans"`
	} `json:"domain"`
	Certificate []byte `json:"certificate"`
}

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

func OpenTrustedDir(dir string, create bool) (*os.File, error) {
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

func WriteFileInDir(dir *os.File, name string, content []byte, mode os.FileMode) error {
	dirFD := int(dir.Fd())
	suffix := make([]byte, 8)
	if _, err := rand.Read(suffix); err != nil {
		return err
	}
	tmpName := ".gotham-proxy-" + hex.EncodeToString(suffix)

	fd, err := unix.Openat(dirFD, tmpName, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW|unix.O_CLOEXEC, uint32(mode))
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

func (s *Server) ensureACMEStorage() error {
	dir, err := OpenTrustedDir(s.acmeDir, true)
	if err != nil {
		return err
	}
	defer func() { _ = dir.Close() }()

	fd, err := unix.Openat(int(dir.Fd()), "acme.json",
		unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0o600)
	if errors.Is(err, unix.EEXIST) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("create %s/acme.json: %w", s.acmeDir, err)
	}
	return unix.Close(fd)
}

func (s *Server) ping(ctx context.Context) error {
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
