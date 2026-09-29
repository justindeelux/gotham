package updates

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"
)

// TestApplierPathHelpers covers the default/custom path and bound resolution.
func TestApplierPathHelpers(t *testing.T) {
	applier := &Applier{}
	if _, err := applier.binaryPath(); err != nil {
		t.Fatalf("binaryPath (executable): %v", err)
	}
	applier.BinaryPath = "/opt/gotham/gotham"
	if got, _ := applier.binaryPath(); got != "/opt/gotham/gotham" {
		t.Errorf("binaryPath = %q", got)
	}
	if got := applier.lockPath("/opt/gotham/gotham"); got != "/opt/gotham/gotham.lock" {
		t.Errorf("lockPath = %q", got)
	}
	if got := applier.oldPath("/opt/gotham/gotham"); got != "/opt/gotham/gotham"+OldSuffix {
		t.Errorf("oldPath = %q", got)
	}
	applier.LockPath = "/run/lock"
	applier.OldPath = "/run/backup"
	if applier.lockPath("/x") != "/run/lock" || applier.oldPath("/x") != "/run/backup" {
		t.Error("custom lock/backup paths not honoured")
	}
	if applier.maxBytes() != DefaultMaxBytes {
		t.Errorf("maxBytes default = %d", applier.maxBytes())
	}
	applier.MaxBytes = 5
	if applier.maxBytes() != 5 {
		t.Errorf("maxBytes custom = %d", applier.maxBytes())
	}
}

// TestApplierFetchErrors covers a non-2xx response and an over-size body.
func TestApplierFetchErrors(t *testing.T) {
	applier := &Applier{}

	notFound := httptest.NewServer(http.NotFoundHandler())
	defer notFound.Close()
	applier.Client = notFound.Client()
	if _, err := applier.fetch(context.Background(), notFound.URL+"/missing", 64); !errors.Is(err, ErrDownload) {
		t.Fatalf("fetch(404) = %v, want ErrDownload", err)
	}

	big := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("0123456789"))
	}))
	defer big.Close()
	applier.Client = big.Client()
	if _, err := applier.fetch(context.Background(), big.URL, 4); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("fetch(too large) = %v, want ErrTooLarge", err)
	}
}

// TestValidateRedirect proves an https downgrade to loopback http is refused.
func TestValidateRedirect(t *testing.T) {
	httpsURL, _ := url.Parse("https://releases.example.com/asset")
	loopbackHTTP, _ := url.Parse("http://127.0.0.1/asset")
	httpsNext, _ := url.Parse("https://objects.example.com/asset")

	if err := validateRedirect(httpsURL, httpsNext); err != nil {
		t.Fatalf("https redirect = %v, want nil", err)
	}
	if err := validateRedirect(httpsURL, loopbackHTTP); !errors.Is(err, ErrBadURL) {
		t.Fatalf("https downgrade = %v, want ErrBadURL", err)
	}
	if err := validateRedirect(nil, httpsNext); err != nil {
		t.Fatalf("first redirect = %v, want nil", err)
	}
}

// TestIsBlockedIP covers the dial-time destination filter.
func TestIsBlockedIP(t *testing.T) {
	for _, raw := range []string{"169.254.169.254", "fe80::1", "fd00:ec2::254", "100.100.100.200"} {
		ip := net.ParseIP(raw)
		if ip == nil || !isBlockedIP(ip) {
			t.Errorf("isBlockedIP(%s) = false, want true", raw)
		}
	}
	for _, raw := range []string{"127.0.0.1", "10.0.0.1", "192.168.1.1", "2606:4700::1"} {
		if isBlockedIP(net.ParseIP(raw)) {
			t.Errorf("isBlockedIP(%s) = true, want false", raw)
		}
	}
}

// TestInstallLockedFailsWithoutDirectory covers the staging error path.
func TestInstallLockedFailsWithoutDirectory(t *testing.T) {
	applier := &Applier{}
	if err := applier.installLocked(filepath.Join(t.TempDir(), "missing", "gotham"), []byte("x")); !errors.Is(err, ErrApply) {
		t.Fatalf("installLocked = %v, want ErrApply", err)
	}
}

// TestServiceLastStatus proves the durable outcome is surfaced through the
// service.
func TestServiceLastStatus(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "update.status")
	store := NewStatusStore(path)
	if err := store.Write(Status{Result: StatusOK, Version: "v1.2.0"}); err != nil {
		t.Fatalf("write status: %v", err)
	}

	svc, err := NewService(Config{
		Current:    "v1.2.0",
		BinaryPath: filepath.Join(dir, "gotham"),
		StatusPath: path,
		Restart:    func(context.Context) error { return nil },
	})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	status, err := svc.LastStatus()
	if err != nil {
		t.Fatalf("LastStatus: %v", err)
	}
	if status == nil || status.Result != StatusOK {
		t.Fatalf("LastStatus = %+v, want ok", status)
	}
}

// TestSafeDialContext proves blocked literals are refused and normal targets
// dial.
func TestSafeDialContext(t *testing.T) {
	if _, err := safeDialContext(context.Background(), "tcp", "169.254.169.254:80"); !errors.Is(err, ErrBadURL) {
		t.Fatalf("safeDialContext(metadata) = %v, want ErrBadURL", err)
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer listener.Close()
	conn, err := safeDialContext(context.Background(), "tcp", listener.Addr().String())
	if err != nil {
		t.Fatalf("safeDialContext(loopback) = %v", err)
	}
	_ = conn.Close()

	if _, err := safeDialContext(context.Background(), "tcp", "localhost:9"); err != nil && errors.Is(err, ErrBadURL) {
		t.Fatalf("safeDialContext(localhost) refused: %v", err)
	}
}

// TestDefaultRestartSuccess proves the wrapper is launched (detached) when it
// exists.
func TestDefaultRestartSuccess(t *testing.T) {
	dir := t.TempDir()
	fakeBin := filepath.Join(dir, "fakebin")
	if err := os.MkdirAll(fakeBin, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(fakeBin, "sudo"), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write sudo shim: %v", err)
	}
	script := filepath.Join(dir, "gotham-update")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write wrapper: %v", err)
	}
	t.Setenv("PATH", fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"))

	if err := defaultRestart(script)(context.Background()); err != nil {
		t.Fatalf("defaultRestart = %v", err)
	}
}
