package updates

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

const testAssetName = "gotham-linux-amd64"

// fakeRelease is a local, manifest-signed release served over httptest.
type fakeRelease struct {
	server  *httptest.Server
	release *Release
}

// newFakeRelease serves artifact plus its signed manifest. The manifest binds
// the given version/channel/arch/file to the artifact digest.
func newFakeRelease(t *testing.T, signer *Signer, artifact []byte, version, channel, arch, file string) *fakeRelease {
	t.Helper()
	manifest := BuildManifest(version, channel, arch, file, artifact)
	manifestBytes := manifest.Marshal()
	manifestSig := signer.SignBase64(manifestBytes)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/" + file:
			_, _ = w.Write(artifact)
		case "/" + ManifestName(arch):
			_, _ = w.Write(manifestBytes)
		case "/" + ManifestName(arch) + ManifestSigSuffix:
			_, _ = w.Write([]byte(manifestSig))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	return &fakeRelease{
		server: server,
		release: &Release{
			Version:              version,
			Tag:                  version,
			Channel:              channel,
			Arch:                 arch,
			AssetName:            file,
			AssetURL:             server.URL + "/" + file,
			ManifestName:         ManifestName(arch),
			ManifestURL:          server.URL + "/" + ManifestName(arch),
			ManifestSignatureURL: server.URL + "/" + ManifestName(arch) + ManifestSigSuffix,
		},
	}
}

// defaultFakeRelease builds the canonical v1.2.0 stable amd64 release.
func defaultFakeRelease(t *testing.T, signer *Signer, artifact []byte) *fakeRelease {
	t.Helper()
	return newFakeRelease(t, signer, artifact, "v1.2.0", "stable", "amd64", testAssetName)
}

// newTestSigner generates a keypair and returns the signer and its verifier.
func newTestSigner(t *testing.T) (*Signer, *Verifier) {
	t.Helper()
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
	return signer, verifier
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

// TestApplierApplyAndRollback is the fake-release end to end: download the
// signed manifest, verify it, install the digest-matching artifact, then
// restore the previous binary.
func TestApplierApplyAndRollback(t *testing.T) {
	signer, verifier := newTestSigner(t)
	newBinary := []byte("gotham v1.2.0 binary")
	fake := defaultFakeRelease(t, signer, newBinary)

	target := writeTarget(t, "gotham v1.0.0 binary")
	applier := &Applier{
		Client:     fake.server.Client(),
		Verifier:   verifier,
		BinaryPath: target,
		Restart:    func(context.Context) error { return nil },
	}

	outcome, err := applier.Apply(context.Background(), fake.release)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if outcome.Version != "v1.2.0" || !outcome.Staged {
		t.Fatalf("outcome = %+v, want v1.2.0 staged (restart configured)", outcome)
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

// TestApplierRefusesWrongManifestKey proves an artifact whose manifest was
// signed by another key is never installed.
func TestApplierRefusesWrongManifestKey(t *testing.T) {
	_, verifier := newTestSigner(t)
	otherSigner, _ := newTestSigner(t)
	fake := defaultFakeRelease(t, otherSigner, []byte("payload"))

	target := writeTarget(t, "original")
	applier := &Applier{Client: fake.server.Client(), Verifier: verifier, BinaryPath: target}

	if _, err := applier.Apply(context.Background(), fake.release); !errors.Is(err, ErrBadSignature) {
		t.Fatalf("Apply = %v, want ErrBadSignature", err)
	}
	if got := readFile(t, target); got != "original" {
		t.Errorf("target = %q, want the untouched original", got)
	}
	if _, err := os.Stat(target + OldSuffix); !os.IsNotExist(err) {
		t.Errorf("a refused update created a backup: %v", err)
	}
}

// TestApplierRejectsDigestMismatch proves the artifact must match the signed
// manifest digest.
func TestApplierRejectsDigestMismatch(t *testing.T) {
	signer, verifier := newTestSigner(t)
	// The manifest describes the signed payload but the server serves other
	// bytes.
	manifest := BuildManifest("v1.2.0", "stable", "amd64", testAssetName, []byte("signed payload"))
	manifestBytes := manifest.Marshal()
	sig := signer.SignBase64(manifestBytes)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/" + testAssetName:
			_, _ = w.Write([]byte("different payload"))
		case "/" + ManifestName("amd64"):
			_, _ = w.Write(manifestBytes)
		case "/" + ManifestName("amd64") + ManifestSigSuffix:
			_, _ = w.Write([]byte(sig))
		}
	}))
	defer server.Close()

	release := &Release{
		Version:              "v1.2.0",
		Channel:              "stable",
		Arch:                 "amd64",
		AssetName:            testAssetName,
		AssetURL:             server.URL + "/" + testAssetName,
		ManifestURL:          server.URL + "/" + ManifestName("amd64"),
		ManifestSignatureURL: server.URL + "/" + ManifestName("amd64") + ManifestSigSuffix,
	}

	target := writeTarget(t, "original")
	applier := &Applier{Client: server.Client(), Verifier: verifier, BinaryPath: target}
	if _, err := applier.Apply(context.Background(), release); !errors.Is(err, ErrChecksumMismatch) {
		t.Fatalf("Apply = %v, want ErrChecksumMismatch", err)
	}
	if got := readFile(t, target); got != "original" {
		t.Errorf("target = %q, want the untouched original", got)
	}
}

// TestApplierRejectsTagMismatch proves a signed manifest cannot relabel a
// different version (the applied version always comes from the manifest).
func TestApplierRejectsTagMismatch(t *testing.T) {
	signer, verifier := newTestSigner(t)
	fake := defaultFakeRelease(t, signer, []byte("payload"))
	// The release metadata claims v99.0.0 while the signed manifest says
	// v1.2.0.
	release := *fake.release
	release.Version = "v99.0.0"

	target := writeTarget(t, "original")
	applier := &Applier{Client: fake.server.Client(), Verifier: verifier, BinaryPath: target}
	if _, err := applier.Apply(context.Background(), &release); !errors.Is(err, ErrManifest) {
		t.Fatalf("Apply = %v, want ErrManifest", err)
	}
	if got := readFile(t, target); got != "original" {
		t.Errorf("target = %q, want the untouched original", got)
	}
}

// TestApplierRejectsMissingManifest proves an update without a manifest URL is
// refused.
func TestApplierRejectsMissingManifest(t *testing.T) {
	signer, verifier := newTestSigner(t)
	fake := defaultFakeRelease(t, signer, []byte("payload"))
	release := *fake.release
	release.ManifestURL = ""
	release.ManifestSignatureURL = ""

	target := writeTarget(t, "original")
	applier := &Applier{Client: fake.server.Client(), Verifier: verifier, BinaryPath: target}
	if _, err := applier.Apply(context.Background(), &release); err == nil {
		t.Fatal("Apply without a manifest = nil error, want failure")
	}
	if got := readFile(t, target); got != "original" {
		t.Errorf("target = %q, want the untouched original", got)
	}
}

// TestApplierRestartFailureRollsBack simulates a wrapper that reports an
// immediate failure: the previous binary must be restored.
func TestApplierRestartFailureRollsBack(t *testing.T) {
	signer, verifier := newTestSigner(t)
	fake := defaultFakeRelease(t, signer, []byte("v1.2.0 binary"))

	target := writeTarget(t, "v1.0.0 binary")
	applier := &Applier{
		Client:     fake.server.Client(),
		Verifier:   verifier,
		BinaryPath: target,
		Restart:    func(context.Context) error { return errors.New("wrapper failed") },
	}

	if _, err := applier.Apply(context.Background(), fake.release); !errors.Is(err, ErrApply) {
		t.Fatalf("Apply = %v, want ErrApply", err)
	}
	if got := readFile(t, target); got != "v1.0.0 binary" {
		t.Errorf("target = %q, want the restored old binary", got)
	}
}

// TestApplierConcurrentApply proves two overlapping applies cannot interleave
// their staging: the lock serializes installs and the result is always one of
// the two verified payloads with no leftover staging files.
func TestApplierConcurrentApply(t *testing.T) {
	signer, verifier := newTestSigner(t)
	payloadA := []byte("payload A")
	payloadB := []byte("payload B")
	fakeA := defaultFakeRelease(t, signer, payloadA)
	fakeB := defaultFakeRelease(t, signer, payloadB)

	target := writeTarget(t, "original")
	applierA := &Applier{Client: fakeA.server.Client(), Verifier: verifier, BinaryPath: target, LockPath: filepath.Join(filepath.Dir(target), "update.lock")}
	applierB := &Applier{Client: fakeB.server.Client(), Verifier: verifier, BinaryPath: target, LockPath: filepath.Join(filepath.Dir(target), "update.lock")}

	var wg sync.WaitGroup
	errs := make([]error, 2)
	wg.Add(2)
	go func() { defer wg.Done(); _, errs[0] = applierA.Apply(context.Background(), fakeA.release) }()
	go func() { defer wg.Done(); _, errs[1] = applierB.Apply(context.Background(), fakeB.release) }()
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("apply %d: %v", i, err)
		}
	}
	final := readFile(t, target)
	if final != string(payloadA) && final != string(payloadB) {
		t.Fatalf("final binary = %q, want one of the verified payloads", final)
	}
	backup := readFile(t, target+OldSuffix)
	if backup != string(payloadA) && backup != string(payloadB) && backup != "original" {
		t.Fatalf("backup = %q, want a verified payload or the original", backup)
	}
	if matches, _ := filepath.Glob(filepath.Join(filepath.Dir(target), ".gotham.new.*")); len(matches) != 0 {
		t.Fatalf("leftover staging files: %v", matches)
	}
}

// TestApplierRecoverRestoresMissingTarget proves startup recovery restores the
// backup when a crash left the target missing.
func TestApplierRecoverRestoresMissingTarget(t *testing.T) {
	target := filepath.Join(t.TempDir(), "gotham")
	if err := os.WriteFile(target+OldSuffix, []byte("last known good"), 0o755); err != nil {
		t.Fatalf("write backup: %v", err)
	}
	if err := os.WriteFile(filepath.Join(filepath.Dir(target), ".gotham.new.123"), []byte("stale"), 0o644); err != nil {
		t.Fatalf("write stale staging: %v", err)
	}

	applier := &Applier{BinaryPath: target}
	restored, err := applier.Recover()
	if err != nil {
		t.Fatalf("Recover: %v", err)
	}
	if !restored {
		t.Fatal("Recover did not report a restore")
	}
	if got := readFile(t, target); got != "last known good" {
		t.Errorf("target after recovery = %q", got)
	}
	if _, err := os.Stat(target + OldSuffix); !os.IsNotExist(err) {
		t.Errorf("backup still present after recovery: %v", err)
	}
	if matches, _ := filepath.Glob(filepath.Join(filepath.Dir(target), ".gotham.new.*")); len(matches) != 0 {
		t.Errorf("stale staging not cleaned: %v", matches)
	}
}

// TestApplierFailClosed covers the missing key and no-backup rollback.
func TestApplierFailClosed(t *testing.T) {
	signer, _ := newTestSigner(t)
	fake := defaultFakeRelease(t, signer, []byte("binary"))

	target := writeTarget(t, "original")
	if _, err := (&Applier{Client: fake.server.Client(), BinaryPath: target}).Apply(context.Background(), fake.release); !errors.Is(err, ErrNoPublicKey) {
		t.Fatalf("Apply = %v, want ErrNoPublicKey", err)
	}
	if err := (&Applier{BinaryPath: target}).Rollback(); !errors.Is(err, ErrNoBackup) {
		t.Fatalf("Rollback = %v, want ErrNoBackup", err)
	}
}
