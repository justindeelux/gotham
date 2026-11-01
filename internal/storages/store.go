package storages

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"

	"github.com/justindeelux/gotham/internal/netguard"
)

// ObjectStore is the write/read/delete surface a backup run needs from a
// destination. The local and S3 implementations live here with the storage
// domain; backups only consumes the interface.
type ObjectStore interface {
	Kind() string
	Put(ctx context.Context, key string, data io.Reader, size int64) (string, error)
	Get(ctx context.Context, location string) (io.ReadCloser, error)
	// Stat reports the stored object size so an upload can be verified after
	// the fact (a lifecycle rule or a partial write shows up as a mismatch).
	Stat(ctx context.Context, location string) (int64, error)
	Delete(ctx context.Context, location string) error
}

const (
	LocationFilePrefix = "file://"
	LocationS3Prefix   = "s3://"
)

// BackendFactory builds the store for a destination. It is an interface so the
// storage service can run against the real backends in production and against
// a test seam in the backups package.
type BackendFactory interface {
	Open(ctx context.Context, storage Storage, credentials Credentials) (ObjectStore, error)
}

// Credentials are the plaintext keys decrypted for one operation. They never
// leave the process.
type Credentials struct {
	AccessKey string
	SecretKey string
}

type localStore struct {
	dir string
}

// LocalStore is the on-disk destination used by the built-in provider.
type LocalStore = localStore

var _ ObjectStore = (*localStore)(nil)

// FSyncFile and FSyncDir are variables so a test can simulate a failing flush.
var (
	FSyncFile = func(file *os.File) error { return file.Sync() }
	FSyncDir  = func(dir string) error {
		handle, err := os.Open(dir)
		if err != nil {
			return err
		}
		defer func() { _ = handle.Close() }()
		return handle.Sync()
	}
)

func fsyncDirChain(dir, root string) error {
	dir, root = filepath.Clean(dir), filepath.Clean(root)
	for {
		if err := FSyncDir(dir); err != nil {
			return err
		}
		if dir == root || !pathWithin(root, dir) {
			return nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return nil
		}
		dir = parent
	}
}

func pathWithin(root, dir string) bool {
	rel, err := filepath.Rel(root, dir)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// NewLocalStore prepares the directory used for local artifacts.
func NewLocalStore(dir string) (*localStore, error) {
	if strings.TrimSpace(dir) == "" {
		return nil, errors.New("storages: local backup directory is not configured")
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, fmt.Errorf("storages: resolve backup directory: %w", err)
	}
	if err := os.MkdirAll(abs, 0o750); err != nil {
		return nil, fmt.Errorf("storages: create backup directory: %w", err)
	}
	return &localStore{dir: abs}, nil
}

func (s *localStore) Kind() string { return string(KindLocal) }

func (s *localStore) Put(_ context.Context, key string, data io.Reader, size int64) (string, error) {
	target, err := s.pathFor(key)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o750); err != nil {
		return "", fmt.Errorf("storages: create backup directory: %w", err)
	}
	file, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o640)
	if err != nil {
		return "", fmt.Errorf("storages: create backup file: %w", err)
	}
	written, copyErr := io.Copy(file, data)
	var syncErr error
	if copyErr == nil {
		syncErr = FSyncFile(file)
	}
	closeErr := file.Close()
	if copyErr != nil {
		_ = os.Remove(target)
		return "", fmt.Errorf("storages: write backup file: %w", copyErr)
	}
	if syncErr != nil {
		_ = os.Remove(target)
		return "", fmt.Errorf("storages: flush backup file: %w", syncErr)
	}
	if closeErr != nil {
		_ = os.Remove(target)
		return "", fmt.Errorf("storages: write backup file: %w", closeErr)
	}
	if size >= 0 && written != size {
		_ = os.Remove(target)
		return "", fmt.Errorf("storages: wrote %d bytes, expected %d", written, size)
	}
	if err := fsyncDirChain(filepath.Dir(target), s.dir); err != nil {
		_ = os.Remove(target)
		return "", fmt.Errorf("storages: flush backup directory: %w", err)
	}
	return LocationFilePrefix + target, nil
}

func (s *localStore) Get(_ context.Context, location string) (io.ReadCloser, error) {
	file, err := s.resolve(location)
	if err != nil {
		return nil, err
	}
	opened, err := os.Open(file)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("%w: backup file is missing on disk", ErrNotFound)
		}
		return nil, fmt.Errorf("storages: open backup file: %w", err)
	}
	return opened, nil
}

func (s *localStore) Delete(_ context.Context, location string) error {
	file, err := s.resolve(location)
	if err != nil {
		return err
	}
	if err := os.Remove(file); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("storages: remove backup file: %w", err)
	}
	return nil
}

func (s *localStore) Stat(_ context.Context, location string) (int64, error) {
	file, err := s.resolve(location)
	if err != nil {
		return 0, err
	}
	info, err := os.Stat(file)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return 0, fmt.Errorf("%w: backup file is missing on disk", ErrNotFound)
		}
		return 0, fmt.Errorf("storages: stat backup file: %w", err)
	}
	return info.Size(), nil
}

func (s *localStore) resolve(location string) (string, error) {
	target := strings.TrimPrefix(location, LocationFilePrefix)
	if !filepath.IsAbs(target) {
		return s.pathFor(target)
	}
	rel, err := filepath.Rel(s.dir, filepath.Clean(target))
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("%w: backup location escapes the backup directory", ErrValidation)
	}
	return filepath.Join(s.dir, rel), nil
}

func (s *localStore) pathFor(key string) (string, error) {
	cleaned := path.Clean("/" + strings.TrimSpace(key))
	target := filepath.Join(s.dir, filepath.FromSlash(cleaned))
	rel, err := filepath.Rel(s.dir, target)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("%w: backup key escapes the backup directory", ErrValidation)
	}
	return target, nil
}

// S3Store is the S3-compatible destination.
type S3Store struct {
	client *minio.Client
	bucket string
	prefix string
}

var _ ObjectStore = (*S3Store)(nil)

// S3Config is the resolved configuration of an S3-compatible destination.
type S3Config struct {
	Endpoint  string
	Region    string
	Bucket    string
	Prefix    string
	AccessKey string
	SecretKey string
}

// NewS3Store builds the S3-compatible backend.
func NewS3Store(cfg S3Config) (*S3Store, error) {
	if strings.TrimSpace(cfg.Bucket) == "" {
		return nil, fmt.Errorf("%w: s3 target needs a bucket", ErrValidation)
	}
	endpoint, secure, err := SplitEndpoint(cfg.Endpoint)
	if err != nil {
		return nil, err
	}
	client, err := minio.New(endpoint, &minio.Options{
		Creds:     credentials.NewStaticV4(cfg.AccessKey, cfg.SecretKey, ""),
		Secure:    secure,
		Region:    cfg.Region,
		Transport: s3Transport(),
	})
	if err != nil {
		return nil, fmt.Errorf("storages: build s3 client: %w", err)
	}
	return &S3Store{client: client, bucket: cfg.Bucket, prefix: NormalizePrefix(cfg.Prefix)}, nil
}

func (s *S3Store) Kind() string { return string(KindS3) }

func (s *S3Store) Put(ctx context.Context, key string, data io.Reader, size int64) (string, error) {
	object := s.objectName(key)
	info, err := s.client.PutObject(ctx, s.bucket, object, data, size, minio.PutObjectOptions{
		ContentType: "application/gzip",
	})
	if err != nil {
		return "", fmt.Errorf("storages: upload backup to s3: %w", err)
	}
	if size >= 0 && info.Size != size {
		return "", fmt.Errorf("storages: uploaded %d bytes, expected %d", info.Size, size)
	}
	return LocationS3Prefix + s.bucket + "/" + object, nil
}

func (s *S3Store) Get(ctx context.Context, location string) (io.ReadCloser, error) {
	object, err := s.objectFromLocation(location)
	if err != nil {
		return nil, err
	}
	if _, err := s.client.StatObject(ctx, s.bucket, object, minio.StatObjectOptions{}); err != nil {
		if IsS3NotFound(err) {
			return nil, fmt.Errorf("%w: backup object is missing in %s", ErrNotFound, s.bucket)
		}
		return nil, fmt.Errorf("storages: stat backup in s3: %w", err)
	}
	reader, err := s.client.GetObject(ctx, s.bucket, object, minio.GetObjectOptions{})
	if err != nil {
		return nil, fmt.Errorf("storages: download backup from s3: %w", err)
	}
	return reader, nil
}

func (s *S3Store) Delete(ctx context.Context, location string) error {
	object, err := s.objectFromLocation(location)
	if err != nil {
		return err
	}
	if err := s.client.RemoveObject(ctx, s.bucket, object, minio.RemoveObjectOptions{}); err != nil {
		return fmt.Errorf("storages: delete backup from s3: %w", err)
	}
	return nil
}

func (s *S3Store) Stat(ctx context.Context, location string) (int64, error) {
	object, err := s.objectFromLocation(location)
	if err != nil {
		return 0, err
	}
	info, err := s.client.StatObject(ctx, s.bucket, object, minio.StatObjectOptions{})
	if err != nil {
		if IsS3NotFound(err) {
			return 0, fmt.Errorf("%w: backup object is missing in %s", ErrNotFound, s.bucket)
		}
		return 0, fmt.Errorf("storages: stat backup in s3: %w", err)
	}
	return info.Size, nil
}

// EnsureBucket creates the bucket when missing. Integration tests use it to
// prepare the throwaway object store; production never calls it.
func (s *S3Store) EnsureBucket(ctx context.Context) error {
	err := s.client.MakeBucket(ctx, s.bucket, minio.MakeBucketOptions{Region: ""})
	if err != nil && minio.ToErrorResponse(err).Code != "BucketAlreadyOwnedByYou" {
		return fmt.Errorf("storages: create s3 bucket: %w", err)
	}
	return nil
}

// s3Transport refuses reserved outbound addresses at dial time (private and
// loopback hosts stay allowed for self-hosted MinIO) and lets minio keep its
// own no-redirect client.
func s3Transport() *http.Transport {
	base, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		base = &http.Transport{Proxy: http.ProxyFromEnvironment}
	}
	transport := base.Clone()
	dialer := &net.Dialer{
		Timeout:   10 * time.Second,
		KeepAlive: 30 * time.Second,
		Control:   netguard.Control(true),
	}
	transport.DialContext = dialer.DialContext
	return transport
}

// BucketExists proves the bucket is reachable before the probe writes to it.
func (s *S3Store) BucketExists(ctx context.Context) (bool, error) {
	ok, err := s.client.BucketExists(ctx, s.bucket)
	if err != nil {
		return false, fmt.Errorf("storages: check s3 bucket: %w", err)
	}
	return ok, nil
}

func (s *S3Store) objectName(key string) string {
	key = strings.TrimLeft(strings.TrimSpace(key), "/")
	if s.prefix == "" {
		return key
	}
	return s.prefix + "/" + key
}

func (s *S3Store) objectFromLocation(location string) (string, error) {
	rest, ok := strings.CutPrefix(location, LocationS3Prefix)
	if !ok {
		return "", fmt.Errorf("%w: backup location %q is not an s3 uri", ErrValidation, location)
	}
	bucket, object, found := strings.Cut(rest, "/")
	if !found || bucket != s.bucket || strings.TrimSpace(object) == "" {
		return "", fmt.Errorf("%w: backup location %q does not belong to bucket %q",
			ErrValidation, location, s.bucket)
	}
	return object, nil
}

// SplitEndpoint normalises an endpoint into the minio host and TLS choice.
func SplitEndpoint(endpoint string) (string, bool, error) {
	raw := strings.TrimSpace(endpoint)
	if raw == "" {
		return "", false, fmt.Errorf("%w: s3 target needs an endpoint", ErrValidation)
	}
	secure := true
	switch {
	case strings.HasPrefix(raw, "https://"):
		raw = strings.TrimPrefix(raw, "https://")
	case strings.HasPrefix(raw, "http://"):
		secure = false
		raw = strings.TrimPrefix(raw, "http://")
	default:
		secure = true
	}
	raw = strings.TrimRight(raw, "/")
	if raw == "" {
		return "", false, fmt.Errorf("%w: s3 endpoint has no host", ErrValidation)
	}
	return raw, secure, nil
}

// NormalizePrefix cleans a key prefix so object names stay stable.
func NormalizePrefix(prefix string) string {
	cleaned := strings.Trim(path.Clean("/"+strings.TrimSpace(prefix)), "/")
	if cleaned == "." || cleaned == "/" {
		return ""
	}
	return cleaned
}

func IsS3NotFound(err error) bool {
	var resp minio.ErrorResponse
	if errors.As(err, &resp) && (resp.Code == "NoSuchKey" || resp.Code == "NoSuchBucket") {
		return true
	}
	message := err.Error()
	return strings.Contains(message, "NoSuchKey") || strings.Contains(message, "NoSuchBucket")
}

// DefaultBackends builds the real local and S3 stores.
type DefaultBackends struct {
	LocalDir string
}

var _ BackendFactory = DefaultBackends{}

func (b DefaultBackends) Open(_ context.Context, storage Storage, creds Credentials) (ObjectStore, error) {
	if storage.Kind == KindLocal {
		return NewLocalStore(b.LocalDir)
	}
	if storage.Kind != KindS3 {
		return nil, fmt.Errorf("%w: %q", ErrUnsupported, storage.Kind)
	}
	return NewS3Store(S3Config{
		Endpoint:  storage.Endpoint,
		Region:    storage.Region,
		Bucket:    storage.Bucket,
		Prefix:    storage.Prefix,
		AccessKey: creds.AccessKey,
		SecretKey: creds.SecretKey,
	})
}
