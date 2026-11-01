package storages

import (
	"errors"
	"testing"
)

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
		if got := NormalizePrefix(input); got != want {
			t.Errorf("NormalizePrefix(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestNewS3StoreValidation(t *testing.T) {
	if _, err := NewS3Store(S3Config{}); !errors.Is(err, ErrValidation) {
		t.Errorf("empty config err = %v, want ErrValidation", err)
	}
	if _, err := NewS3Store(S3Config{Endpoint: "http://minio:9000"}); !errors.Is(err, ErrValidation) {
		t.Errorf("missing bucket err = %v, want ErrValidation", err)
	}
	store, err := NewS3Store(S3Config{
		Endpoint: "http://minio.gotham.internal:9000",
		Bucket:   "gotham-backups",
		Prefix:   "/pg-orders/",
	})
	if err != nil {
		t.Fatalf("NewS3Store: %v", err)
	}
	if store.Kind() != string(KindS3) {
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
	store := &S3Store{bucket: "gotham-backups", prefix: "pg"}

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
	if !IsS3NotFound(errors.New("NoSuchKey: object does not exist")) {
		t.Error("text NoSuchKey must count as not found")
	}
	if IsS3NotFound(errors.New("connection refused")) {
		t.Error("a transport error is not a not-found")
	}
}
