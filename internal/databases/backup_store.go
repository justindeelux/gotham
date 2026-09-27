package databases

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// ObjectStore is where backup artifacts live. Two implementations exist: the
// control plane's own disk (the default, and the target of a "local" entry)
// and any S3-compatible endpoint — AWS S3, Cloudflare R2, MinIO. It is an
// interface so tests can fake the network without a live bucket.
type ObjectStore interface {
	// Kind reports the store family ("local" or "s3").
	Kind() string
	// Put stores data under key and returns the canonical location of the
	// object, which is what the backups row records.
	Put(ctx context.Context, key string, data io.Reader, size int64) (string, error)
	// Get opens the object at location for reading.
	Get(ctx context.Context, location string) (io.ReadCloser, error)
	// Delete removes the object at location. A missing object is success.
	Delete(ctx context.Context, location string) error
}

// Store schemes recorded in backups.location.
const (
	locationFilePrefix = "file://"
	locationS3Prefix   = "s3://"
)

// localStore keeps artifacts on the control plane's disk under a fixed root
// directory. Keys are relative paths inside that root; anything trying to
// escape it (a key with "..", an absolute path) is rejected.
type localStore struct {
	dir string
}

// Compile-time guarantee.
var _ ObjectStore = (*localStore)(nil)

// newLocalStore returns a store rooted at dir, creating it if needed.
func newLocalStore(dir string) (*localStore, error) {
	if strings.TrimSpace(dir) == "" {
		return nil, errors.New("databases: local backup directory is not configured")
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, fmt.Errorf("databases: resolve backup directory: %w", err)
	}
	if err := os.MkdirAll(abs, 0o750); err != nil {
		return nil, fmt.Errorf("databases: create backup directory: %w", err)
	}
	return &localStore{dir: abs}, nil
}

// Kind implements ObjectStore.
func (s *localStore) Kind() string { return string(TargetLocal) }

// Put implements ObjectStore: the bytes are copied into the root directory
// and the location is a file:// URI an operator can open directly.
func (s *localStore) Put(_ context.Context, key string, data io.Reader, size int64) (string, error) {
	target, err := s.pathFor(key)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o750); err != nil {
		return "", fmt.Errorf("databases: create backup directory: %w", err)
	}
	file, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o640)
	if err != nil {
		return "", fmt.Errorf("databases: create backup file: %w", err)
	}
	written, copyErr := io.Copy(file, data)
	closeErr := file.Close()
	if copyErr != nil {
		_ = os.Remove(target)
		return "", fmt.Errorf("databases: write backup file: %w", copyErr)
	}
	if closeErr != nil {
		_ = os.Remove(target)
		return "", fmt.Errorf("databases: write backup file: %w", closeErr)
	}
	if size >= 0 && written != size {
		_ = os.Remove(target)
		return "", fmt.Errorf("databases: wrote %d bytes, expected %d", written, size)
	}
	return locationFilePrefix + target, nil
}

// Get implements ObjectStore.
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
		return nil, fmt.Errorf("databases: open backup file: %w", err)
	}
	return opened, nil
}

// Delete implements ObjectStore: removing a file that is already gone is
// success, matching the idempotency of the container removals.
func (s *localStore) Delete(_ context.Context, location string) error {
	file, err := s.resolve(location)
	if err != nil {
		return err
	}
	if err := os.Remove(file); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("databases: remove backup file: %w", err)
	}
	return nil
}

// resolve turns a recorded location back into a path: relative keys are
// resolved inside the root, absolute file:// paths must already live there.
// Refusing anything outside the root keeps a crafted location from deleting
// a file elsewhere on the control plane.
func (s *localStore) resolve(location string) (string, error) {
	target := strings.TrimPrefix(location, locationFilePrefix)
	if !filepath.IsAbs(target) {
		return s.pathFor(target)
	}
	rel, err := filepath.Rel(s.dir, filepath.Clean(target))
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("%w: backup location escapes the backup directory", ErrValidation)
	}
	return filepath.Join(s.dir, rel), nil
}

// pathFor resolves a key inside the root directory, refusing anything that
// would escape it.
func (s *localStore) pathFor(key string) (string, error) {
	cleaned := path.Clean("/" + strings.TrimSpace(key))
	target := filepath.Join(s.dir, filepath.FromSlash(cleaned))
	rel, err := filepath.Rel(s.dir, target)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("%w: backup key escapes the backup directory", ErrValidation)
	}
	return target, nil
}

// s3Store talks to an S3-compatible endpoint through the minio-go SDK. The
// credentials are opened from sealed rows immediately before the client is
// built and never leave this struct — they are not logged, not returned by
// the API and not written anywhere.
type s3Store struct {
	client *minio.Client
	bucket string
	prefix string
}

// Compile-time guarantee.
var _ ObjectStore = (*s3Store)(nil)

// s3Config is the plaintext configuration of one target, short-lived: it
// exists only while a store is built.
type s3Config struct {
	endpoint  string
	region    string
	bucket    string
	prefix    string
	accessKey string
	secretKey string
}

// newS3Store builds a client for cfg. The endpoint may carry an http(s)
// scheme, which decides whether the transport is TLS; without a scheme the
// endpoint is assumed to be plain HTTP (the usual self-hosted MinIO setup).
func newS3Store(cfg s3Config) (*s3Store, error) {
	if strings.TrimSpace(cfg.bucket) == "" {
		return nil, fmt.Errorf("%w: s3 target needs a bucket", ErrValidation)
	}
	endpoint, secure, err := splitEndpoint(cfg.endpoint)
	if err != nil {
		return nil, err
	}
	client, err := minio.New(endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.accessKey, cfg.secretKey, ""),
		Secure: secure,
		Region: cfg.region,
	})
	if err != nil {
		return nil, fmt.Errorf("databases: build s3 client: %w", err)
	}
	return &s3Store{client: client, bucket: cfg.bucket, prefix: normalizePrefix(cfg.prefix)}, nil
}

// Kind implements ObjectStore.
func (s *s3Store) Kind() string { return string(TargetS3) }

// Put implements ObjectStore.
func (s *s3Store) Put(ctx context.Context, key string, data io.Reader, size int64) (string, error) {
	object := s.objectName(key)
	info, err := s.client.PutObject(ctx, s.bucket, object, data, size, minio.PutObjectOptions{
		ContentType: "application/gzip",
	})
	if err != nil {
		return "", fmt.Errorf("databases: upload backup to s3: %w", err)
	}
	if size >= 0 && info.Size != size {
		return "", fmt.Errorf("databases: uploaded %d bytes, expected %d", info.Size, size)
	}
	return locationS3Prefix + s.bucket + "/" + object, nil
}

// Get implements ObjectStore. StatObject first so a missing key is reported
// as ErrNotFound instead of surfacing from the first Read.
func (s *s3Store) Get(ctx context.Context, location string) (io.ReadCloser, error) {
	object, err := s.objectFromLocation(location)
	if err != nil {
		return nil, err
	}
	if _, err := s.client.StatObject(ctx, s.bucket, object, minio.StatObjectOptions{}); err != nil {
		if isS3NotFound(err) {
			return nil, fmt.Errorf("%w: backup object is missing in %s", ErrNotFound, s.bucket)
		}
		return nil, fmt.Errorf("databases: stat backup in s3: %w", err)
	}
	reader, err := s.client.GetObject(ctx, s.bucket, object, minio.GetObjectOptions{})
	if err != nil {
		return nil, fmt.Errorf("databases: download backup from s3: %w", err)
	}
	return reader, nil
}

// Delete implements ObjectStore.
func (s *s3Store) Delete(ctx context.Context, location string) error {
	object, err := s.objectFromLocation(location)
	if err != nil {
		return err
	}
	// RemoveObject is idempotent on S3: removing a key that does not exist
	// reports success, which is exactly the delete contract.
	if err := s.client.RemoveObject(ctx, s.bucket, object, minio.RemoveObjectOptions{}); err != nil {
		return fmt.Errorf("databases: delete backup from s3: %w", err)
	}
	return nil
}

// bucketExists backs the "test connection" action of a target.
func (s *s3Store) bucketExists(ctx context.Context) (bool, error) {
	ok, err := s.client.BucketExists(ctx, s.bucket)
	if err != nil {
		return false, fmt.Errorf("databases: check s3 bucket: %w", err)
	}
	return ok, nil
}

// objectName joins the target prefix with a key.
func (s *s3Store) objectName(key string) string {
	key = strings.TrimLeft(strings.TrimSpace(key), "/")
	if s.prefix == "" {
		return key
	}
	return s.prefix + "/" + key
}

// objectFromLocation extracts the object key from an s3://bucket/key
// location, refusing a location that belongs to another bucket.
func (s *s3Store) objectFromLocation(location string) (string, error) {
	rest, ok := strings.CutPrefix(location, locationS3Prefix)
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

// splitEndpoint parses "https://host:9000" (or a bare "host:9000") into the
// host:port minio-go expects and the TLS flag.
func splitEndpoint(endpoint string) (string, bool, error) {
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
		// No scheme means TLS: an operator pointing at a plain-HTTP
		// endpoint (usually self-hosted MinIO) must say so explicitly, so a
		// forgotten scheme can never downgrade credentials to plaintext.
		secure = true
	}
	raw = strings.TrimRight(raw, "/")
	if raw == "" {
		return "", false, fmt.Errorf("%w: s3 endpoint has no host", ErrValidation)
	}
	return raw, secure, nil
}

// normalizePrefix trims a target prefix down to the path segments that can
// appear in an object key.
func normalizePrefix(prefix string) string {
	cleaned := strings.Trim(path.Clean("/"+strings.TrimSpace(prefix)), "/")
	if cleaned == "." || cleaned == "/" {
		return ""
	}
	return cleaned
}

// isS3NotFound reports whether err is a 404 from the endpoint: minio-go
// surfaces both a typed ErrorResponse and plain text for proxies that do not
// speak the XML error body, so both spellings count.
func isS3NotFound(err error) bool {
	var resp minio.ErrorResponse
	if errors.As(err, &resp) && (resp.Code == "NoSuchKey" || resp.Code == "NoSuchBucket") {
		return true
	}
	message := err.Error()
	return strings.Contains(message, "NoSuchKey") || strings.Contains(message, "NoSuchBucket")
}
