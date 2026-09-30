package updatecore

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
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

// noopRestart is a RestartFunc that reports the wrapper exited cleanly.
func noopRestart(context.Context) (func() error, error) {
	return func() error { return nil }, nil
}

// newTestApplier builds an Applier whose pending/status/lock live in dir.
func newTestApplier(t *testing.T, dir, target string, verifier *Verifier, restart RestartFunc) *Applier {
	t.Helper()
	return &Applier{
		Verifier:   verifier,
		BinaryPath: target,
		LockPath:   filepath.Join(dir, "update.lock"),
		Pending:    NewStatusStore(filepath.Join(dir, "update.pending")),
		Status:     NewStatusStore(filepath.Join(dir, "update.status")),
		Restart:    restart,
	}
}

// writeTarget creates an "old" binary and returns its path and dir.
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
	dir := filepath.Dir(target)
	applier := newTestApplier(t, dir, target, verifier, noopRestart)
	applier.Client = fake.server.Client()

	outcome, err := applier.Apply(context.Background(), fake.release)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if outcome.Version != "v1.2.0" || !outcome.Staged {
		t.Fatalf("outcome = %+v, want v1.2.0 staged", outcome)
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
	if pending, _ := applier.Pending.Read(); pending != nil {
		t.Errorf("pending marker not cleared after rollback: %+v", pending)
	}
}

// TestApplierRefusesWrongManifestKey proves an artifact whose manifest was
// signed by another key is never installed.
func TestApplierRefusesWrongManifestKey(t *testing.T) {
	_, verifier := newTestSigner(t)
	otherSigner, _ := newTestSigner(t)
	fake := defaultFakeRelease(t, otherSigner, []byte("payload"))

	target := writeTarget(t, "original")
	applier := newTestApplier(t, filepath.Dir(target), target, verifier, nil)
	applier.Client = fake.server.Client()

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
	applier := newTestApplier(t, filepath.Dir(target), target, verifier, nil)
	applier.Client = server.Client()
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
	release := *fake.release
	release.Version = "v99.0.0"

	target := writeTarget(t, "original")
	applier := newTestApplier(t, filepath.Dir(target), target, verifier, nil)
	applier.Client = fake.server.Client()
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
	applier := newTestApplier(t, filepath.Dir(target), target, verifier, nil)
	applier.Client = fake.server.Client()
	if _, err := applier.Apply(context.Background(), &release); err == nil {
		t.Fatal("Apply without a manifest = nil error, want failure")
	}
	if got := readFile(t, target); got != "original" {
		t.Errorf("target = %q, want the untouched original", got)
	}
}

// TestApplierRefusesSecondApplyWhileStaged is the reviewer's two-apply
// interleave: once an update is staged, a second apply is refused and the last
// known good backup is preserved.
func TestApplierRefusesSecondApplyWhileStaged(t *testing.T) {
	signer, verifier := newTestSigner(t)
	payloadA := []byte("payload A")
	payloadB := []byte("payload B")
	fakeA := defaultFakeRelease(t, signer, payloadA)
	fakeB := defaultFakeRelease(t, signer, payloadB)

	target := writeTarget(t, "original")
	dir := filepath.Dir(target)
	applierA := newTestApplier(t, dir, target, verifier, noopRestart)
	applierA.Client = fakeA.server.Client()
	applierB := newTestApplier(t, dir, target, verifier, noopRestart)
	applierB.Client = fakeB.server.Client()

	if _, err := applierA.Apply(context.Background(), fakeA.release); err != nil {
		t.Fatalf("first Apply: %v", err)
	}
	if got := readFile(t, target); got != string(payloadA) {
		t.Fatalf("target = %q, want payload A", got)
	}
	if got := readFile(t, target+OldSuffix); got != "original" {
		t.Fatalf("backup = %q, want the original known-good", got)
	}

	if _, err := applierB.Apply(context.Background(), fakeB.release); !errors.Is(err, ErrUpdatePending) {
		t.Fatalf("second Apply = %v, want ErrUpdatePending", err)
	}
	if got := readFile(t, target); got != string(payloadA) {
		t.Errorf("target = %q, want payload A unchanged", got)
	}
	if got := readFile(t, target+OldSuffix); got != "original" {
		t.Errorf("backup = %q, want the original known-good preserved", got)
	}
}

// TestApplierConcurrentApplySerialized proves two overlapping applies cannot
// both install: exactly one wins and the other is refused as pending, while the
// known-good backup is preserved.
func TestApplierConcurrentApplySerialized(t *testing.T) {
	signer, verifier := newTestSigner(t)
	payloadA := []byte("payload A")
	payloadB := []byte("payload B")
	fakeA := defaultFakeRelease(t, signer, payloadA)
	fakeB := defaultFakeRelease(t, signer, payloadB)

	target := writeTarget(t, "original")
	dir := filepath.Dir(target)
	applierA := newTestApplier(t, dir, target, verifier, noopRestart)
	applierA.Client = fakeA.server.Client()
	applierB := newTestApplier(t, dir, target, verifier, noopRestart)
	applierB.Client = fakeB.server.Client()

	var wg sync.WaitGroup
	errs := make([]error, 2)
	wg.Add(2)
	go func() { defer wg.Done(); _, errs[0] = applierA.Apply(context.Background(), fakeA.release) }()
	go func() { defer wg.Done(); _, errs[1] = applierB.Apply(context.Background(), fakeB.release) }()
	wg.Wait()

	successes, pending := 0, 0
	for _, err := range errs {
		switch {
		case err == nil:
			successes++
		case errors.Is(err, ErrUpdatePending):
			pending++
		default:
			t.Fatalf("unexpected apply error: %v", err)
		}
	}
	if successes != 1 || pending != 1 {
		t.Fatalf("successes=%d pending=%d, want exactly one of each", successes, pending)
	}
	final := readFile(t, target)
	if final != string(payloadA) && final != string(payloadB) {
		t.Fatalf("final binary = %q, want one verified payload", final)
	}
	if backup := readFile(t, target+OldSuffix); backup != "original" {
		t.Fatalf("backup = %q, want the original preserved", backup)
	}
}

// TestApplierCrashLayout proves the activation layout keeps the target present
// on both sides of the commit rename.
func TestApplierCrashLayout(t *testing.T) {
	t.Run("before commit", func(t *testing.T) {
		target := writeTarget(t, "old")
		if err := os.Link(target, target+OldSuffix); err != nil {
			t.Fatalf("hardlink: %v", err)
		}
		applier := newTestApplier(t, filepath.Dir(target), target, nil, nil)
		restored, err := applier.Recover()
		if err != nil {
			t.Fatalf("Recover: %v", err)
		}
		if restored {
			t.Error("Recover restored although the target was present")
		}
		if got := readFile(t, target); got != "old" {
			t.Errorf("target = %q, want old", got)
		}
	})
	t.Run("after commit", func(t *testing.T) {
		target := writeTarget(t, "new")
		if err := os.WriteFile(target+OldSuffix, []byte("old"), 0o755); err != nil {
			t.Fatalf("write backup: %v", err)
		}
		applier := newTestApplier(t, filepath.Dir(target), target, nil, nil)
		if _, err := applier.Recover(); err != nil {
			t.Fatalf("Recover: %v", err)
		}
		if got := readFile(t, target); got != "new" {
			t.Errorf("target = %q, want new", got)
		}
	})
}

// TestApplierRecoverRestoresMissingTarget proves recovery restores the backup
// when a crash left the target missing.
func TestApplierRecoverRestoresMissingTarget(t *testing.T) {
	target := filepath.Join(t.TempDir(), "gotham")
	if err := os.WriteFile(target+OldSuffix, []byte("last known good"), 0o755); err != nil {
		t.Fatalf("write backup: %v", err)
	}
	if err := os.WriteFile(filepath.Join(filepath.Dir(target), ".gotham.new.123"), []byte("stale"), 0o644); err != nil {
		t.Fatalf("write stale staging: %v", err)
	}

	applier := newTestApplier(t, filepath.Dir(target), target, nil, nil)
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

// TestApplierWrapperLaunchErrorRollsBack proves an immediate wrapper launch
// failure restores the previous binary and records the outcome.
func TestApplierWrapperLaunchErrorRollsBack(t *testing.T) {
	signer, verifier := newTestSigner(t)
	fake := defaultFakeRelease(t, signer, []byte("v1.2.0 binary"))

	target := writeTarget(t, "v1.0.0 binary")
	applier := newTestApplier(t, filepath.Dir(target), target, verifier, func(context.Context) (func() error, error) {
		return nil, errors.New("sudo: a password is required")
	})
	applier.Client = fake.server.Client()

	if _, err := applier.Apply(context.Background(), fake.release); !errors.Is(err, ErrApply) {
		t.Fatalf("Apply = %v, want ErrApply", err)
	}
	if got := readFile(t, target); got != "v1.0.0 binary" {
		t.Errorf("target = %q, want the restored old binary", got)
	}
	pending, err := applier.Pending.Read()
	if err != nil {
		t.Fatalf("read pending: %v", err)
	}
	if pending == nil || pending.Result != StatusRolledBack {
		t.Fatalf("pending = %+v, want rolled_back", pending)
	}
}

// TestMonitorRestartRollsBack proves N2: a wrapper that exits without recording
// a result restores the known-good binary and records a terminal outcome
// instead of leaving the unproven binary armed behind a reopened gate.
func TestMonitorRestartRollsBack(t *testing.T) {
	target := writeTarget(t, "unproven A")
	if err := os.WriteFile(target+OldSuffix, []byte("original"), 0o755); err != nil {
		t.Fatalf("write backup: %v", err)
	}
	applier := newTestApplier(t, filepath.Dir(target), target, nil, nil)
	if err := applier.Pending.Write(Status{Result: StatusStaged, Version: "v1.2.0"}); err != nil {
		t.Fatalf("write pending: %v", err)
	}

	applier.monitorRestart(func() error { return errors.New("wrapper exited 1") }, "v1.2.0")

	if got := readFile(t, target); got != "original" {
		t.Fatalf("target = %q, want the known-good restored", got)
	}
	pending, err := applier.Pending.Read()
	if err != nil {
		t.Fatalf("read pending: %v", err)
	}
	if pending == nil || pending.Result != StatusRolledBack {
		t.Fatalf("pending = %+v, want rolled_back", pending)
	}
}

// TestWrapperFailedPreservesKnownGood is the promoted round-3 repro: a denied
// sudo leaves the wrapper failed, the control plane rolls back, and a following
// apply can no longer overwrite the last known good with the unproven binary.
func TestWrapperFailedPreservesKnownGood(t *testing.T) {
	signer, verifier := newTestSigner(t)
	payloadA := []byte("payload A")
	payloadB := []byte("payload B")
	fakeA := defaultFakeRelease(t, signer, payloadA)
	fakeB := defaultFakeRelease(t, signer, payloadB)

	target := writeTarget(t, "original")
	dir := filepath.Dir(target)

	applierA := newTestApplier(t, dir, target, verifier, func(context.Context) (func() error, error) {
		return func() error { return errors.New("exit status 1 (sudo: a password is required)") }, nil
	})
	applierA.Client = fakeA.server.Client()
	if _, err := applierA.Apply(context.Background(), fakeA.release); err != nil {
		t.Fatalf("apply A: %v", err)
	}

	// The monitor rolls back asynchronously; wait for a terminal marker.
	waitForSettledPending(t, applierA.Pending)
	if got := readFile(t, target); got != "original" {
		t.Fatalf("target after denied sudo = %q, want the known-good original", got)
	}

	// A following apply must not lose the original known-good.
	applierB := newTestApplier(t, dir, target, verifier, noopRestart)
	applierB.Client = fakeB.server.Client()
	if _, err := applierB.Apply(context.Background(), fakeB.release); err != nil {
		t.Fatalf("apply B: %v", err)
	}
	if got := readFile(t, target); got != string(payloadB) {
		t.Errorf("target = %q, want payload B", got)
	}
	if got := readFile(t, target+OldSuffix); got != "original" {
		t.Fatalf("backup = %q, want the original known-good preserved", got)
	}
}

// waitForSettledPending waits until the pending marker is no longer in flight.
func waitForSettledPending(t *testing.T, store *StatusStore) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		pending, err := store.Read()
		if err == nil && (pending == nil || !inFlight(pending.Result)) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	pending, _ := store.Read()
	t.Fatalf("pending never settled: %+v", pending)
}

// TestCrashBeforeCommitReopensGate proves L3: a crash before the commit rename
// leaves target and backup as the same inode, so the staged gate is cleared and
// a new apply can proceed.
func TestCrashBeforeCommitReopensGate(t *testing.T) {
	signer, verifier := newTestSigner(t)
	fake := defaultFakeRelease(t, signer, []byte("payload A"))

	target := writeTarget(t, "original")
	if err := os.Link(target, target+OldSuffix); err != nil {
		t.Fatalf("hardlink: %v", err)
	}
	applier := newTestApplier(t, filepath.Dir(target), target, verifier, noopRestart)
	applier.Client = fake.server.Client()
	if err := applier.Pending.Write(Status{Result: StatusStaged, Version: "v1.2.0"}); err != nil {
		t.Fatalf("write pending: %v", err)
	}

	if _, err := applier.Recover(); err != nil {
		t.Fatalf("Recover: %v", err)
	}
	if pending, _ := applier.Pending.Read(); pending != nil && pending.Result == StatusStaged {
		t.Fatalf("gate still closed after a crash before commit: %+v", pending)
	}
	if _, err := applier.Apply(context.Background(), fake.release); err != nil {
		t.Fatalf("Apply after crash-before-commit: %v", err)
	}
}

// TestResumeStagedRelaunches proves M2: a staged marker left by a crash makes
// the next startup relaunch the wrapper exactly once (no loop).
func TestResumeStagedRelaunches(t *testing.T) {
	signer, verifier := newTestSigner(t)
	fake := defaultFakeRelease(t, signer, []byte("payload A"))

	target := writeTarget(t, "new-unproven")
	if err := os.WriteFile(target+OldSuffix, []byte("original"), 0o755); err != nil {
		t.Fatalf("write backup: %v", err)
	}
	launched := 0
	applier := newTestApplier(t, filepath.Dir(target), target, verifier, func(context.Context) (func() error, error) {
		launched++
		return func() error { return nil }, nil
	})
	applier.Client = fake.server.Client()
	if err := applier.Pending.Write(Status{Result: StatusStaged, Version: "v1.2.0"}); err != nil {
		t.Fatalf("write pending: %v", err)
	}

	if err := applier.ResumeStaged(context.Background()); err != nil {
		t.Fatalf("ResumeStaged: %v", err)
	}
	if launched != 1 {
		t.Fatalf("wrapper launches = %d, want 1", launched)
	}
	pending, err := applier.Pending.Read()
	if err != nil {
		t.Fatalf("read pending: %v", err)
	}
	if pending == nil || pending.Result != StatusResuming {
		t.Fatalf("pending = %+v, want resuming", pending)
	}

	// A second startup must not relaunch (no loop).
	if err := applier.ResumeStaged(context.Background()); err != nil {
		t.Fatalf("ResumeStaged (second): %v", err)
	}
	if launched != 1 {
		t.Fatalf("wrapper relaunched on a resuming marker (loop): %d", launched)
	}
}

// TestResumeStagedLaunchErrorRollsBack proves a resume that cannot launch the
// wrapper restores the known-good binary.
func TestResumeStagedLaunchErrorRollsBack(t *testing.T) {
	signer, verifier := newTestSigner(t)
	fake := defaultFakeRelease(t, signer, []byte("payload A"))

	target := writeTarget(t, "new-unproven")
	if err := os.WriteFile(target+OldSuffix, []byte("original"), 0o755); err != nil {
		t.Fatalf("write backup: %v", err)
	}
	applier := newTestApplier(t, filepath.Dir(target), target, verifier, func(context.Context) (func() error, error) {
		return nil, errors.New("sudo: a password is required")
	})
	applier.Client = fake.server.Client()
	if err := applier.Pending.Write(Status{Result: StatusStaged, Version: "v1.2.0"}); err != nil {
		t.Fatalf("write pending: %v", err)
	}

	if err := applier.ResumeStaged(context.Background()); err == nil {
		t.Fatal("ResumeStaged with a failed launch = nil error")
	}
	if got := readFile(t, target); got != "original" {
		t.Fatalf("target = %q, want the known-good restored", got)
	}
	pending, err := applier.Pending.Read()
	if err != nil {
		t.Fatalf("read pending: %v", err)
	}
	if pending == nil || pending.Result != StatusRolledBack {
		t.Fatalf("pending = %+v, want rolled_back", pending)
	}
}

// TestApplierFailClosed covers the missing key and no-backup rollback.
func TestApplierFailClosed(t *testing.T) {
	signer, _ := newTestSigner(t)
	fake := defaultFakeRelease(t, signer, []byte("binary"))

	target := writeTarget(t, "original")
	applier := newTestApplier(t, filepath.Dir(target), target, nil, nil)
	applier.Client = fake.server.Client()
	if _, err := applier.Apply(context.Background(), fake.release); !errors.Is(err, ErrNoPublicKey) {
		t.Fatalf("Apply = %v, want ErrNoPublicKey", err)
	}
	if err := applier.Rollback(); !errors.Is(err, ErrNoBackup) {
		t.Fatalf("Rollback = %v, want ErrNoBackup", err)
	}
}

// TestBinaryPathStableAcrossRename proves the target comes from fixed
// configuration and does not drift to <binary>.old after a rename (the
// /proc/self/exe problem).
func TestBinaryPathStableAcrossRename(t *testing.T) {
	target := writeTarget(t, "v1")
	applier := newTestApplier(t, filepath.Dir(target), target, nil, nil)
	before, err := applier.binaryPath()
	if err != nil {
		t.Fatalf("binaryPath: %v", err)
	}
	if err := os.Rename(target, target+OldSuffix); err != nil {
		t.Fatalf("rename: %v", err)
	}
	after, err := applier.binaryPath()
	if err != nil {
		t.Fatalf("binaryPath after rename: %v", err)
	}
	if before != after || after != target {
		t.Fatalf("binary path drifted: %q -> %q", before, after)
	}
}

// TestApplierRefusesSymlinkedPaths proves a planted symlink at the target or
// backup cannot redirect a swap.
func TestApplierRefusesSymlinkedPaths(t *testing.T) {
	signer, verifier := newTestSigner(t)
	fake := defaultFakeRelease(t, signer, []byte("payload"))

	dir := t.TempDir()
	victim := filepath.Join(dir, "victim")
	if err := os.WriteFile(victim, []byte("victim"), 0o644); err != nil {
		t.Fatalf("write victim: %v", err)
	}
	target := filepath.Join(dir, "gotham")
	if err := os.Symlink(victim, target); err != nil {
		t.Fatalf("symlink target: %v", err)
	}
	applier := newTestApplier(t, dir, target, verifier, noopRestart)
	applier.Client = fake.server.Client()
	if _, err := applier.Apply(context.Background(), fake.release); !errors.Is(err, ErrApply) {
		t.Fatalf("Apply with symlinked target = %v, want ErrApply", err)
	}
	if got := readFile(t, victim); got != "victim" {
		t.Errorf("victim = %q, want it untouched", got)
	}
}
