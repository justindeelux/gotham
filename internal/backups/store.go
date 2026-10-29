package backups

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

type ObjectStore interface {
	Kind() string
	Put(ctx context.Context, key string, data io.Reader, size int64) (string, error)
	Get(ctx context.Context, location string) (io.ReadCloser, error)
	Delete(ctx context.Context, location string) error
}

const (
	locationFilePrefix = "file://"
	locationS3Prefix   = "s3://"
)

type localStore struct {
	dir string
}

var _ ObjectStore = (*localStore)(nil)

var (
	fsyncFile = func(file *os.File) error { return file.Sync() }
	fsyncDir  = func(dir string) error {
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
		if err := fsyncDir(dir); err != nil {
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

func newLocalStore(dir string) (*localStore, error) {
	if strings.TrimSpace(dir) == "" {
		return nil, errors.New("backups: local backup directory is not configured")
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, fmt.Errorf("backups: resolve backup directory: %w", err)
	}
	if err := os.MkdirAll(abs, 0o750); err != nil {
		return nil, fmt.Errorf("backups: create backup directory: %w", err)
	}
	return &localStore{dir: abs}, nil
}

func (s *localStore) Kind() string { return string(TargetLocal) }

func (s *localStore) Put(_ context.Context, key string, data io.Reader, size int64) (string, error) {
	target, err := s.pathFor(key)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o750); err != nil {
		return "", fmt.Errorf("backups: create backup directory: %w", err)
	}
	file, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o640)
	if err != nil {
		return "", fmt.Errorf("backups: create backup file: %w", err)
	}
	written, copyErr := io.Copy(file, data)
	var syncErr error
	if copyErr == nil {
		syncErr = fsyncFile(file)
	}
	closeErr := file.Close()
	if copyErr != nil {
		_ = os.Remove(target)
		return "", fmt.Errorf("backups: write backup file: %w", copyErr)
	}
	if syncErr != nil {
		_ = os.Remove(target)
		return "", fmt.Errorf("backups: flush backup file: %w", syncErr)
	}
	if closeErr != nil {
		_ = os.Remove(target)
		return "", fmt.Errorf("backups: write backup file: %w", closeErr)
	}
	if size >= 0 && written != size {
		_ = os.Remove(target)
		return "", fmt.Errorf("backups: wrote %d bytes, expected %d", written, size)
	}
	if err := fsyncDirChain(filepath.Dir(target), s.dir); err != nil {
		_ = os.Remove(target)
		return "", fmt.Errorf("backups: flush backup directory: %w", err)
	}
	return locationFilePrefix + target, nil
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
		return nil, fmt.Errorf("backups: open backup file: %w", err)
	}
	return opened, nil
}

func (s *localStore) Delete(_ context.Context, location string) error {
	file, err := s.resolve(location)
	if err != nil {
		return err
	}
	if err := os.Remove(file); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("backups: remove backup file: %w", err)
	}
	return nil
}

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

func (s *localStore) pathFor(key string) (string, error) {
	cleaned := path.Clean("/" + strings.TrimSpace(key))
	target := filepath.Join(s.dir, filepath.FromSlash(cleaned))
	rel, err := filepath.Rel(s.dir, target)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("%w: backup key escapes the backup directory", ErrValidation)
	}
	return target, nil
}

type s3Store struct {
	client *minio.Client
	bucket string
	prefix string
}

var _ ObjectStore = (*s3Store)(nil)

type s3Config struct {
	endpoint  string
	region    string
	bucket    string
	prefix    string
	accessKey string
	secretKey string
}

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
		return nil, fmt.Errorf("backups: build s3 client: %w", err)
	}
	return &s3Store{client: client, bucket: cfg.bucket, prefix: normalizePrefix(cfg.prefix)}, nil
}

func (s *s3Store) Kind() string { return string(TargetS3) }

func (s *s3Store) Put(ctx context.Context, key string, data io.Reader, size int64) (string, error) {
	object := s.objectName(key)
	info, err := s.client.PutObject(ctx, s.bucket, object, data, size, minio.PutObjectOptions{
		ContentType: "application/gzip",
	})
	if err != nil {
		return "", fmt.Errorf("backups: upload backup to s3: %w", err)
	}
	if size >= 0 && info.Size != size {
		return "", fmt.Errorf("backups: uploaded %d bytes, expected %d", info.Size, size)
	}
	return locationS3Prefix + s.bucket + "/" + object, nil
}

func (s *s3Store) Get(ctx context.Context, location string) (io.ReadCloser, error) {
	object, err := s.objectFromLocation(location)
	if err != nil {
		return nil, err
	}
	if _, err := s.client.StatObject(ctx, s.bucket, object, minio.StatObjectOptions{}); err != nil {
		if isS3NotFound(err) {
			return nil, fmt.Errorf("%w: backup object is missing in %s", ErrNotFound, s.bucket)
		}
		return nil, fmt.Errorf("backups: stat backup in s3: %w", err)
	}
	reader, err := s.client.GetObject(ctx, s.bucket, object, minio.GetObjectOptions{})
	if err != nil {
		return nil, fmt.Errorf("backups: download backup from s3: %w", err)
	}
	return reader, nil
}

func (s *s3Store) Delete(ctx context.Context, location string) error {
	object, err := s.objectFromLocation(location)
	if err != nil {
		return err
	}
	if err := s.client.RemoveObject(ctx, s.bucket, object, minio.RemoveObjectOptions{}); err != nil {
		return fmt.Errorf("backups: delete backup from s3: %w", err)
	}
	return nil
}

func (s *s3Store) bucketExists(ctx context.Context) (bool, error) {
	ok, err := s.client.BucketExists(ctx, s.bucket)
	if err != nil {
		return false, fmt.Errorf("backups: check s3 bucket: %w", err)
	}
	return ok, nil
}

func (s *s3Store) objectName(key string) string {
	key = strings.TrimLeft(strings.TrimSpace(key), "/")
	if s.prefix == "" {
		return key
	}
	return s.prefix + "/" + key
}

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
		secure = true
	}
	raw = strings.TrimRight(raw, "/")
	if raw == "" {
		return "", false, fmt.Errorf("%w: s3 endpoint has no host", ErrValidation)
	}
	return raw, secure, nil
}

func normalizePrefix(prefix string) string {
	cleaned := strings.Trim(path.Clean("/"+strings.TrimSpace(prefix)), "/")
	if cleaned == "." || cleaned == "/" {
		return ""
	}
	return cleaned
}

func isS3NotFound(err error) bool {
	var resp minio.ErrorResponse
	if errors.As(err, &resp) && (resp.Code == "NoSuchKey" || resp.Code == "NoSuchBucket") {
		return true
	}
	message := err.Error()
	return strings.Contains(message, "NoSuchKey") || strings.Contains(message, "NoSuchBucket")
}
