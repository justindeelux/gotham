package databases

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLocalStoreRoundTrip(t *testing.T) {
	store, err := newLocalStore(t.TempDir())
	if err != nil {
		t.Fatalf("newLocalStore: %v", err)
	}
	if store.Kind() != string(TargetLocal) {
		t.Errorf("kind = %q, want %q", store.Kind(), TargetLocal)
	}

	payload := []byte{0x1f, 0x8b, 0x00, 0xff, '\n', 'P', 'K'}
	location, err := store.Put(context.Background(), "databases/db1/backup1.dump.gz", bytes.NewReader(payload), int64(len(payload)))
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	if !strings.HasPrefix(location, locationFilePrefix) {
		t.Errorf("location = %q, want a file:// uri", location)
	}

	reader, err := store.Get(context.Background(), location)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	stored, err := io.ReadAll(reader)
	_ = reader.Close()
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !bytes.Equal(stored, payload) {
		t.Errorf("round trip changed the bytes")
	}

	if err := store.Delete(context.Background(), location); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := store.Get(context.Background(), location); !errors.Is(err, ErrNotFound) {
		t.Errorf("Get after delete err = %v, want ErrNotFound", err)
	}
	// Deleting again is still success.
	if err := store.Delete(context.Background(), location); err != nil {
		t.Errorf("second Delete err = %v, want nil", err)
	}
}

func TestLocalStoreRejectsEscapingKeysAndLocations(t *testing.T) {
	root := t.TempDir()
	store, err := newLocalStore(root)
	if err != nil {
		t.Fatalf("newLocalStore: %v", err)
	}

	// A key with traversal is neutralised rather than followed: the bytes
	// land inside the root directory instead of outside it.
	location, err := store.Put(context.Background(), "../../etc/passwd", bytes.NewReader([]byte("x")), 1)
	if err != nil {
		t.Fatalf("Put with a traversal key: %v", err)
	}
	if !strings.HasPrefix(location, locationFilePrefix+root) {
		t.Errorf("location = %q, want it inside %s", location, root)
	}
	if _, err := os.Stat(filepath.Join(root, "etc", "passwd")); err != nil {
		t.Errorf("file was not written under the root: %v", err)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(root), "etc", "passwd")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("file escaped the root: %v", err)
	}
	outside := locationFilePrefix + filepath.Join(root, "..", "escape.txt")
	if _, err := store.Get(context.Background(), outside); !errors.Is(err, ErrValidation) {
		t.Errorf("escaping location (Get) err = %v, want ErrValidation", err)
	}
	if err := store.Delete(context.Background(), outside); !errors.Is(err, ErrValidation) {
		t.Errorf("escaping location (Delete) err = %v, want ErrValidation", err)
	}
	// A system file must be unreachable through a crafted location.
	if err := store.Delete(context.Background(), locationFilePrefix+"/etc/hosts"); !errors.Is(err, ErrValidation) {
		t.Errorf("/etc/hosts err = %v, want ErrValidation", err)
	}
}

func TestLocalStorePutChecksSize(t *testing.T) {
	store, err := newLocalStore(t.TempDir())
	if err != nil {
		t.Fatalf("newLocalStore: %v", err)
	}
	if _, err := store.Put(context.Background(), "k", bytes.NewReader([]byte("abc")), 99); err == nil {
		t.Error("expected a size mismatch error")
	}
	// A mismatch must not leave a partial file behind.
	if _, statErr := os.Stat(filepath.Join(store.dir, "k")); !errors.Is(statErr, os.ErrNotExist) {
		t.Errorf("partial file left behind: %v", statErr)
	}
}

func TestSplitEndpoint(t *testing.T) {
	tests := []struct {
		endpoint string
		host     string
		secure   bool
		wantErr  bool
	}{
		{endpoint: "https://s3.example.com", host: "s3.example.com", secure: true},
		{endpoint: "https://account.r2.cloudflarestorage.com", host: "account.r2.cloudflarestorage.com", secure: true},
		{endpoint: "http://minio.gotham.internal:9000", host: "minio.gotham.internal:9000", secure: false},
		// A bare endpoint defaults to TLS so a forgotten scheme cannot send
		// credentials over plaintext.
		{endpoint: "minio.gotham.internal:9000", host: "minio.gotham.internal:9000", secure: true},
		{endpoint: " https://s3.example.com/ ", host: "s3.example.com", secure: true},
		{endpoint: "", wantErr: true},
		{endpoint: "https://", wantErr: true},
	}
	for _, test := range tests {
		host, secure, err := splitEndpoint(test.endpoint)
		if test.wantErr {
			if err == nil {
				t.Errorf("splitEndpoint(%q) = nil error, want failure", test.endpoint)
			}
			continue
		}
		if err != nil {
			t.Errorf("splitEndpoint(%q): %v", test.endpoint, err)
			continue
		}
		if host != test.host || secure != test.secure {
			t.Errorf("splitEndpoint(%q) = (%q, %v), want (%q, %v)", test.endpoint, host, secure, test.host, test.secure)
		}
	}
}

func TestNormalizePrefix(t *testing.T) {
	tests := map[string]string{
		"":            "",
		"/":           "",
		".":           "",
		"pg-orders/":  "pg-orders",
		"/backups/26": "backups/26",
		"a//b":        "a/b",
	}
	for input, want := range tests {
		if got := normalizePrefix(input); got != want {
			t.Errorf("normalizePrefix(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestNewS3StoreValidation(t *testing.T) {
	if _, err := newS3Store(s3Config{}); !errors.Is(err, ErrValidation) {
		t.Errorf("empty config err = %v, want ErrValidation", err)
	}
	if _, err := newS3Store(s3Config{endpoint: "http://minio:9000"}); !errors.Is(err, ErrValidation) {
		t.Errorf("missing bucket err = %v, want ErrValidation", err)
	}
	store, err := newS3Store(s3Config{
		endpoint: "http://minio.gotham.internal:9000",
		bucket:   "gotham-backups",
		prefix:   "/pg-orders/",
	})
	if err != nil {
		t.Fatalf("newS3Store: %v", err)
	}
	if store.Kind() != string(TargetS3) {
		t.Errorf("kind = %q", store.Kind())
	}
	if store.prefix != "pg-orders" {
		t.Errorf("prefix = %q, want pg-orders", store.prefix)
	}
	if store.objectName("databases/db1/b.dump.gz") != "pg-orders/databases/db1/b.dump.gz" {
		t.Errorf("objectName = %q", store.objectName("databases/db1/b.dump.gz"))
	}
}

func TestS3LocationHandling(t *testing.T) {
	store := &s3Store{bucket: "gotham-backups", prefix: "pg"}

	object, err := store.objectFromLocation("s3://gotham-backups/pg/databases/x.dump.gz")
	if err != nil {
		t.Fatalf("objectFromLocation: %v", err)
	}
	if object != "pg/databases/x.dump.gz" {
		t.Errorf("object = %q", object)
	}
	if _, err := store.objectFromLocation("s3://other-bucket/pg/x"); !errors.Is(err, ErrValidation) {
		t.Errorf("foreign bucket err = %v, want ErrValidation", err)
	}
	if _, err := store.objectFromLocation("file:///tmp/x"); !errors.Is(err, ErrValidation) {
		t.Errorf("file location err = %v, want ErrValidation", err)
	}
	if _, err := store.objectFromLocation("s3://gotham-backups/"); !errors.Is(err, ErrValidation) {
		t.Errorf("empty object err = %v, want ErrValidation", err)
	}
}

func TestIsS3NotFound(t *testing.T) {
	if !isS3NotFound(errors.New("NoSuchKey: object does not exist")) {
		t.Error("text NoSuchKey must count as not found")
	}
	if isS3NotFound(errors.New("connection refused")) {
		t.Error("a transport error is not a not-found")
	}
}
