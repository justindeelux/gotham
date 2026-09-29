package updates

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

const testAssetName = "gotham-linux-amd64"

// fakeRelease is a local release: the artifact, its detached signature and a
// checksums file, served over httptest.
type fakeRelease struct {
	server  *httptest.Server
	release *Release
}

// newFakeRelease serves payload signed with signer over a local server.
func newFakeRelease(t *testing.T, signer *Signer, payload []byte) *fakeRelease {
	t.Helper()
	sum := sha256.Sum256(payload)
	checksums := hex.EncodeToString(sum[:]) + "  " + testAssetName + "\n"
	signature := signer.SignBase64(payload)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/" + testAssetName:
			_, _ = w.Write(payload)
		case "/" + testAssetName + ".sig":
			_, _ = w.Write([]byte(signature))
		case "/checksums.txt":
			_, _ = w.Write([]byte(checksums))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	return &fakeRelease{
		server: server,
		release: &Release{
			Version:      "v1.2.0",
			AssetName:    testAssetName,
			AssetURL:     server.URL + "/" + testAssetName,
			SignatureURL: server.URL + "/" + testAssetName + ".sig",
			ChecksumURL:  server.URL + "/checksums.txt",
		},
	}
}

// writeTarget creates an "old" binary and returns its path.
func writeTarget(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "gotham")
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatalf("write target: %v", err)
	}
	return path
}

// readFile reads a path, failing the test on error.
func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

// TestApplierApplyAndRollback is the fake-release end to end: download, verify
// and swap, then restore the previous binary.
func TestApplierApplyAndRollback(t *testing.T) {
	publicKey, privateKey, err := GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	signer, err := NewSigner(privateKey)
	if err != nil {
		t.Fatalf("NewSigner: %v", err)
	}
	verifier, err := NewVerifier(publicKey)
	if err != nil {
		t.Fatalf("NewVerifier: %v", err)
	}

	newBinary := []byte("gotham v1.2.0 binary")
	fake := newFakeRelease(t, signer, newBinary)

	target := writeTarget(t, "gotham v1.0.0 binary")
	applier := &Applier{
		Client:     fake.server.Client(),
		Verifier:   verifier,
		BinaryPath: target,
		Restart:    func(context.Context) error { return nil },
	}

	if err := applier.Apply(context.Background(), fake.release); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if got := readFile(t, target); got != string(newBinary) {
		t.Errorf("target = %q, want the new binary", got)
	}
	if got := readFile(t, target+OldSuffix); got != "gotham v1.0.0 binary" {
		t.Errorf("retained old binary = %q", got)
	}

	if err := applier.Rollback(); err != nil {
		t.Fatalf("Rollback: %v", err)
	}
	if got := readFile(t, target); got != "gotham v1.0.0 binary" {
		t.Errorf("target after rollback = %q, want the old binary", got)
	}
	if _, err := os.Stat(target + OldSuffix); !os.IsNotExist(err) {
		t.Errorf("old binary still present after rollback: %v", err)
	}
}

// TestApplierRefusesTamperedSignature proves an artifact whose signature does
// not match is never swapped in.
func TestApplierRefusesTamperedSignature(t *testing.T) {
	publicKey, privateKey, err := GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	signer, err := NewSigner(privateKey)
	if err != nil {
		t.Fatalf("NewSigner: %v", err)
	}
	verifier, err := NewVerifier(publicKey)
	if err != nil {
		t.Fatalf("NewVerifier: %v", err)
	}

	// The server signs one payload but serves a different artifact: the
	// detached signature no longer covers the downloaded bytes.
	tampered := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/" + testAssetName:
			_, _ = w.Write([]byte("tampered payload"))
		case "/" + testAssetName + ".sig":
			_, _ = w.Write([]byte(signer.SignBase64([]byte("signed payload"))))
		case "/checksums.txt":
			sum := sha256.Sum256([]byte("tampered payload"))
			_, _ = w.Write([]byte(hex.EncodeToString(sum[:]) + "  " + testAssetName + "\n"))
		}
	}))
	defer tampered.Close()
	release := Release{
		Version:      "v1.2.0",
		AssetName:    testAssetName,
		AssetURL:     tampered.URL + "/" + testAssetName,
		SignatureURL: tampered.URL + "/" + testAssetName + ".sig",
		ChecksumURL:  tampered.URL + "/checksums.txt",
	}

	target := writeTarget(t, "original binary")
	applier := &Applier{Client: tampered.Client(), Verifier: verifier, BinaryPath: target}

	if err := applier.Apply(context.Background(), &release); !errors.Is(err, ErrBadSignature) {
		t.Fatalf("Apply = %v, want ErrBadSignature", err)
	}
	if got := readFile(t, target); got != "original binary" {
		t.Errorf("target = %q, want the untouched original", got)
	}
}

// TestApplierRestartFailureRollsBack simulates a healthcheck failure driven by
// the restart hook: the previous binary must be restored.
func TestApplierRestartFailureRollsBack(t *testing.T) {
	publicKey, privateKey, err := GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	signer, err := NewSigner(privateKey)
	if err != nil {
		t.Fatalf("NewSigner: %v", err)
	}
	verifier, err := NewVerifier(publicKey)
	if err != nil {
		t.Fatalf("NewVerifier: %v", err)
	}
	fake := newFakeRelease(t, signer, []byte("v1.2.0 binary"))

	target := writeTarget(t, "v1.0.0 binary")
	applier := &Applier{
		Client:     fake.server.Client(),
		Verifier:   verifier,
		BinaryPath: target,
		Restart:    func(context.Context) error { return errors.New("healthcheck failed") },
	}

	if err := applier.Apply(context.Background(), fake.release); !errors.Is(err, ErrApply) {
		t.Fatalf("Apply = %v, want ErrApply", err)
	}
	if got := readFile(t, target); got != "v1.0.0 binary" {
		t.Errorf("target = %q, want the restored old binary", got)
	}
}

// TestApplierFailClosed covers the missing key, checksum mismatch and rollback
// without a backup.
func TestApplierFailClosed(t *testing.T) {
	publicKey, privateKey, err := GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	signer, err := NewSigner(privateKey)
	if err != nil {
		t.Fatalf("NewSigner: %v", err)
	}
	fake := newFakeRelease(t, signer, []byte("binary"))

	target := writeTarget(t, "original")
	if err := (&Applier{Client: fake.server.Client(), BinaryPath: target}).Apply(context.Background(), fake.release); !errors.Is(err, ErrNoPublicKey) {
		t.Fatalf("Apply = %v, want ErrNoPublicKey", err)
	}
	if _, err := os.Stat(target + OldSuffix); !os.IsNotExist(err) {
		t.Errorf("no update should have produced a backup: %v", err)
	}

	verifier, err := NewVerifier(publicKey)
	if err != nil {
		t.Fatalf("NewVerifier: %v", err)
	}
	// A valid signature over bytes the checksums file disagrees with fails.
	badChecksums := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("deadbeef  " + testAssetName + "\n"))
	}))
	defer badChecksums.Close()
	release := *fake.release
	release.ChecksumURL = badChecksums.URL

	applier := &Applier{Client: badChecksums.Client(), Verifier: verifier, BinaryPath: target}
	if err := applier.Apply(context.Background(), &release); !errors.Is(err, ErrChecksumMismatch) {
		t.Fatalf("Apply = %v, want ErrChecksumMismatch", err)
	}

	if err := applier.Rollback(); !errors.Is(err, ErrNoBackup) {
		t.Fatalf("Rollback = %v, want ErrNoBackup", err)
	}
}

// TestDecodeSignatureFormats proves raw and base64 signatures both decode.
func TestDecodeSignatureFormats(t *testing.T) {
	sig := make([]byte, SignatureSize)
	for i := range sig {
		sig[i] = byte(i)
	}
	if got, err := decodeSignature(sig); err != nil || len(got) != SignatureSize {
		t.Fatalf("decode raw signature: %v", err)
	}
	if got, err := decodeSignature([]byte(base64.StdEncoding.EncodeToString(sig))); err != nil || len(got) != SignatureSize {
		t.Fatalf("decode base64 signature: %v", err)
	}
	if _, err := decodeSignature([]byte("short")); !errors.Is(err, ErrBadSignature) {
		t.Fatalf("decode short signature = %v, want ErrBadSignature", err)
	}
}
