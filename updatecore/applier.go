package updatecore

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	// DefaultMaxBytes bounds the release artifact download (a Gotham binary is
	// tens of MiB; the ceiling leaves headroom without allowing a disk-fill).
	DefaultMaxBytes = 200 << 20
	// maxManifestBytes bounds the signed manifest.
	maxManifestBytes = 64 << 10
	// maxSignatureBytes bounds a detached signature file.
	maxSignatureBytes = 4 << 10
	// OldSuffix is the suffix of the retained previous binary.
	OldSuffix = ".old"
)

// Download/verification/apply failures.
var (
	// ErrDownload is returned when the artifact cannot be fetched.
	ErrDownload = errors.New("updates: download failed")
	// ErrTooLarge is returned when a download exceeds its bound.
	ErrTooLarge = errors.New("updates: download too large")
	// ErrChecksumMismatch is returned when the artifact SHA-256 differs from
	// the signed manifest.
	ErrChecksumMismatch = errors.New("updates: checksum mismatch")
	// ErrApply is returned when the binary swap fails.
	ErrApply = errors.New("updates: apply failed")
	// ErrNoBackup is returned when no previous binary exists to roll back to.
	ErrNoBackup = errors.New("updates: no previous binary to roll back to")
	// ErrNoUpdate is returned when Apply is called without a release.
	ErrNoUpdate = errors.New("updates: no update available")
	// ErrUpdatePending is returned when a previous update is staged and has
	// not been confirmed healthy or rolled back.
	ErrUpdatePending = errors.New("updates: a previous update is still pending")
)

// RestartFunc launches the privileged restart/healthcheck wrapper and returns a
// waiter for its exit. The launch error is for an immediate failure; the waiter
// error is observed asynchronously (the wrapper normally restarts this
// process, so it cannot be awaited inline).
type RestartFunc func(ctx context.Context) (wait func() error, err error)

// ApplyOutcome reports what Apply did.
type ApplyOutcome struct {
	// Version is the version named by the signed manifest.
	Version string
	// Staged is true when a restart/healthcheck wrapper was launched and the
	// new version is not yet proven healthy.
	Staged bool
}

// Applier downloads and verifies a release, then installs the binary.
//
// The signed manifest authenticates the release identity and artifact digest;
// the artifact is never touched before those checks pass. Installation is
// serialized by a process-external lock, uses a unique staging file, keeps the
// previous binary as a hard-linked <binary>.old so the target is never absent,
// gates a second apply while one is staged, and is recoverable after a crash.
type Applier struct {
	// Client is the HTTP client; a bounded default is used when nil.
	Client *http.Client
	// Verifier holds the release public key. When nil, Apply fails closed.
	Verifier *Verifier
	// BinaryPath is the fixed target executable. It is resolved once by the
	// service and must never come from the live inode after a swap.
	BinaryPath string
	// OldPath is where the previous binary is retained; empty means
	// <BinaryPath>.old.
	OldPath string
	// LockPath is the serialization lock; empty means <BinaryPath>.lock.
	LockPath string
	// Pending records the staged/pending marker (control-plane-owned) that
	// gates a second apply.
	Pending *StatusStore
	// Status is the authoritative root-owned status, read only to reconcile a
	// stale pending marker.
	Status *StatusStore
	// Restart launches the restart/healthcheck wrapper.
	Restart RestartFunc
	// Timeout bounds each download when Client is nil.
	Timeout time.Duration
	// MaxBytes bounds the artifact download.
	MaxBytes int64
}

// Apply downloads and verifies the release, installs the artifact, and (when
// configured) launches the restart wrapper. The current binary is left
// untouched unless the manifest signature and the artifact digest both verify.
func (a *Applier) Apply(ctx context.Context, rel *Release) (*ApplyOutcome, error) {
	if rel == nil {
		return nil, ErrNoUpdate
	}
	if a.Verifier == nil {
		return nil, ErrNoPublicKey
	}
	for _, rawURL := range []string{rel.ManifestURL, rel.ManifestSignatureURL, rel.AssetURL} {
		if err := validateURL(rawURL); err != nil {
			return nil, err
		}
	}

	manifestBytes, err := a.fetch(ctx, rel.ManifestURL, maxManifestBytes)
	if err != nil {
		return nil, err
	}
	manifestSignature, err := a.fetch(ctx, rel.ManifestSignatureURL, maxSignatureBytes)
	if err != nil {
		return nil, err
	}
	if err := a.Verifier.Verify(manifestBytes, manifestSignature); err != nil {
		return nil, err
	}
	manifest, err := ParseManifest(manifestBytes)
	if err != nil {
		return nil, err
	}
	if err := bindManifest(manifest, rel); err != nil {
		return nil, err
	}

	data, err := a.fetch(ctx, rel.AssetURL, a.maxBytes())
	if err != nil {
		return nil, err
	}
	if err := manifest.Verify(data); err != nil {
		return nil, err
	}

	binPath, err := a.binaryPath()
	if err != nil {
		return nil, err
	}
	if err := withFileLock(a.lockPath(binPath), func() error {
		if err := a.recoverLocked(binPath); err != nil {
			return err
		}
		if a.pendingInFlight() {
			return ErrUpdatePending
		}
		// Mark the update pending before touching the binary so a crash at any
		// point leaves the staged gate set (fail closed) rather than an
		// unconfirmed swap.
		if a.Pending != nil {
			if err := a.Pending.Write(Status{Result: StatusStaged, Version: manifest.Version}); err != nil {
				return fmt.Errorf("%w: record pending: %v", ErrApply, err)
			}
		}
		if err := a.installLocked(binPath, data); err != nil {
			if a.Pending != nil {
				_ = a.Pending.Remove()
			}
			return err
		}
		return nil
	}); err != nil {
		return nil, err
	}

	if a.Restart == nil {
		return &ApplyOutcome{Version: manifest.Version}, nil
	}

	wait, err := a.Restart(ctx)
	if err != nil {
		detail := err.Error()
		result := StatusRolledBack
		if rollbackErr := a.Rollback(); rollbackErr != nil {
			result = StatusRollbackFailed
			detail = fmt.Sprintf("%s (rollback: %v)", detail, rollbackErr)
		}
		if a.Pending != nil {
			_ = a.Pending.Write(Status{Result: result, Version: manifest.Version, Detail: detail})
		}
		return nil, fmt.Errorf("%w: wrapper launch failed: %s", ErrApply, detail)
	}
	go a.monitorRestart(wait, manifest.Version)
	return &ApplyOutcome{Version: manifest.Version, Staged: true}, nil
}

// monitorRestart handles an asynchronous wrapper failure: when the launched
// wrapper exits without recording its own result (for example a denied sudo),
// it restores the known-good binary and records a terminal outcome, so the
// unproven binary is never left armed at ExecStart and the gate can reopen
// without the next apply hardlinking the unproven binary over the last known
// good one.
func (a *Applier) monitorRestart(wait func() error, version string) {
	if wait == nil {
		return
	}
	if err := wait(); err == nil {
		return
	}
	binPath, err := a.binaryPath()
	if err != nil {
		return
	}
	_ = withFileLock(a.lockPath(binPath), func() error {
		pending, err := a.Pending.Read()
		if a.Pending == nil || err != nil || pending == nil || !inFlight(pending.Result) {
			// The wrapper recorded a result (or another path resolved it).
			return nil
		}
		if version == "" {
			version = pending.Version
		}
		result := StatusRolledBack
		detail := "the restart wrapper exited before recording an outcome"
		if rollbackErr := a.restoreLocked(binPath); rollbackErr != nil {
			result = StatusRollbackFailed
			detail = fmt.Sprintf("%s (rollback: %v)", detail, rollbackErr)
		}
		_ = a.Pending.Write(Status{Result: result, Version: version, Detail: detail})
		return nil
	})
}

// Rollback restores the retained previous binary over the current one and
// clears the pending marker. It is serialized with Apply.
func (a *Applier) Rollback() error {
	binPath, err := a.binaryPath()
	if err != nil {
		return err
	}
	return withFileLock(a.lockPath(binPath), func() error {
		if err := a.restoreLocked(binPath); err != nil {
			return err
		}
		if a.Pending != nil {
			_ = a.Pending.Remove()
		}
		return nil
	})
}

// Recover repairs a crash-interrupted swap, cleans stale staging files and
// reconciles a stale pending marker. It reports whether it restored a missing
// target from its backup. It is safe to call at startup and skips while the
// privileged wrapper holds the lock.
func (a *Applier) Recover() (bool, error) {
	binPath, err := a.binaryPath()
	if err != nil {
		return false, err
	}
	restored := false
	acquired, err := tryFileLock(a.lockPath(binPath), func() error {
		before := fileExists(binPath)
		if err := a.recoverLocked(binPath); err != nil {
			return err
		}
		restored = !before && fileExists(binPath)
		return nil
	})
	if err != nil {
		return false, err
	}
	if !acquired {
		// The wrapper owns the transaction; it will finish recovery.
		return false, nil
	}
	return restored, nil
}

// recoverLocked cleans stale staging files, restores the backup when the target
// is missing, and clears a pending marker the wrapper already superseded. The
// caller must hold the lock.
func (a *Applier) recoverLocked(binPath string) error {
	dir := filepath.Dir(binPath)
	pattern := filepath.Join(dir, "."+filepath.Base(binPath)+".new.*")
	if matches, err := filepath.Glob(pattern); err == nil {
		for _, match := range matches {
			_ = os.Remove(match)
		}
	}
	if !fileExists(binPath) {
		backup := a.oldPath(binPath)
		if fileExists(backup) {
			if err := os.Rename(backup, binPath); err != nil {
				return fmt.Errorf("updates: recover: %w", err)
			}
			syncDir(dir)
		}
	}
	a.reconcilePending(binPath)
	return nil
}

// reconcilePending clears a staged pending marker when the crash left nothing
// to activate (target and backup are the same inode) or when the authoritative
// status is at least as new (the wrapper finished but could not remove the
// marker).
func (a *Applier) reconcilePending(binPath string) {
	if a.Pending == nil {
		return
	}
	pending, err := a.Pending.Read()
	if err != nil || pending == nil || pending.Result != StatusStaged {
		return
	}
	// Crash before commit: the target was never replaced, so target and backup
	// still reference the same inode. Nothing was staged; reopen the gate.
	if targetInfo, err := os.Stat(binPath); err == nil {
		if backupInfo, err := os.Stat(a.oldPath(binPath)); err == nil && os.SameFile(targetInfo, backupInfo) {
			_ = a.Pending.Remove()
			return
		}
	}
	if a.Status == nil {
		return
	}
	status, err := a.Status.Read()
	if err != nil || status == nil {
		return
	}
	if !status.At.Before(pending.At) {
		_ = a.Pending.Remove()
	}
}

// installLocked writes data to a unique staging file, fsyncs it, and swaps it
// into place. The current binary is first hard-linked to the backup, so the
// target path is never absent at any instant. The caller must hold the lock.
func (a *Applier) installLocked(binPath string, data []byte) error {
	dir := filepath.Dir(binPath)
	base := filepath.Base(binPath)

	if isSymlink(binPath) {
		return fmt.Errorf("%w: refusing symlinked target %s", ErrApply, binPath)
	}
	backup := a.oldPath(binPath)
	if isSymlink(backup) {
		return fmt.Errorf("%w: refusing symlinked backup %s", ErrApply, backup)
	}

	staging, err := os.CreateTemp(dir, "."+base+".new.*")
	if err != nil {
		return fmt.Errorf("%w: stage: %v", ErrApply, err)
	}
	stagingName := staging.Name()
	defer func() {
		if stagingName != "" {
			_ = os.Remove(stagingName)
		}
	}()

	if _, err := staging.Write(data); err != nil {
		staging.Close()
		return fmt.Errorf("%w: write: %v", ErrApply, err)
	}
	if err := staging.Sync(); err != nil {
		staging.Close()
		return fmt.Errorf("%w: sync: %v", ErrApply, err)
	}
	if err := staging.Close(); err != nil {
		return fmt.Errorf("%w: close: %v", ErrApply, err)
	}
	if err := os.Chmod(stagingName, 0o755); err != nil {
		return fmt.Errorf("%w: chmod: %v", ErrApply, err)
	}

	// Replace the previous backup and hardlink the current (known-good) binary
	// to it. os.Link never removes the target, so a crash here leaves both the
	// target and the backup in place.
	if err := os.Remove(backup); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("%w: replace backup: %v", ErrApply, err)
	}
	if err := os.Link(binPath, backup); err != nil {
		return fmt.Errorf("%w: hardlink backup: %v", ErrApply, err)
	}
	if err := os.Rename(stagingName, binPath); err != nil {
		_ = os.Remove(backup)
		return fmt.Errorf("%w: activate: %v", ErrApply, err)
	}
	stagingName = ""
	syncDir(dir)
	return nil
}

// restoreLocked renames the backup over the target. The rename is atomic, so
// the target is never absent. The caller must hold the lock.
func (a *Applier) restoreLocked(binPath string) error {
	backup := a.oldPath(binPath)
	if isSymlink(backup) {
		return fmt.Errorf("%w: refusing symlinked backup %s", ErrNoBackup, backup)
	}
	if _, err := os.Stat(backup); err != nil {
		return fmt.Errorf("%w: %s", ErrNoBackup, backup)
	}
	if err := os.Rename(backup, binPath); err != nil {
		return fmt.Errorf("updates: rollback: %w", err)
	}
	syncDir(filepath.Dir(binPath))
	return nil
}

// inFlight reports whether a pending result means an update is still awaiting
// its outcome (staged, or resumed after a crash).
func inFlight(result string) bool {
	return result == StatusStaged || result == StatusResuming
}

// pendingInFlight reports whether an update is awaiting its outcome; a second
// apply is refused while it is.
func (a *Applier) pendingInFlight() bool {
	if a.Pending == nil {
		return false
	}
	pending, err := a.Pending.Read()
	return err == nil && pending != nil && inFlight(pending.Result)
}

// ResumeStaged relaunches the wrapper when a staged update is still pending at
// startup (a crash, reboot or OOM during the health window), so the new binary
// is health-checked or rolled back instead of running unproven behind a closed
// gate. It rewrites the marker to resuming first, so a second startup cannot
// relaunch again; it never loops.
//
// It uses a non-blocking lock: on a normal update the wrapper holds the lock
// through the restart and the whole health window, and the restarted binary
// must not wait for it (waiting would deadlock the update against itself). A
// held lock means the wrapper already owns this update's outcome, so the resume
// is skipped.
func (a *Applier) ResumeStaged(ctx context.Context) error {
	if a.Restart == nil {
		return nil
	}
	binPath, err := a.binaryPath()
	if err != nil {
		return err
	}
	var version string
	resume := false
	acquired, err := tryFileLock(a.lockPath(binPath), func() error {
		if err := a.recoverLocked(binPath); err != nil {
			return err
		}
		pending, err := a.Pending.Read()
		if a.Pending == nil || err != nil || pending == nil || pending.Result != StatusStaged {
			return nil
		}
		version = pending.Version
		resume = true
		return a.Pending.Write(Status{Result: StatusResuming, Version: pending.Version})
	})
	if err != nil {
		return err
	}
	if !acquired {
		// The wrapper is already handling this update; never wait for it.
		return nil
	}
	if !resume {
		return nil
	}

	wait, err := a.Restart(ctx)
	if err != nil {
		result := StatusRolledBack
		detail := fmt.Sprintf("resume: %v", err)
		if rollbackErr := a.Rollback(); rollbackErr != nil {
			result = StatusRollbackFailed
			detail = fmt.Sprintf("%s (rollback: %v)", detail, rollbackErr)
		}
		_ = a.Pending.Write(Status{Result: result, Version: version, Detail: detail})
		return err
	}
	go a.monitorRestart(wait, version)
	return nil
}

// fetch downloads rawURL into memory, bounded by maxBytes.
func (a *Applier) fetch(ctx context.Context, rawURL string, maxBytes int64) ([]byte, error) {
	if err := validateURL(rawURL); err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "gotham-selfupdate")

	client := a.Client
	if client == nil {
		client = DefaultHTTPClient(defaultDuration(a.Timeout, defaultTimeout))
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrDownload, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%w: %s", ErrDownload, resp.Status)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes+1))
	if err != nil {
		return nil, fmt.Errorf("%w: read: %v", ErrDownload, err)
	}
	if int64(len(data)) > maxBytes {
		return nil, fmt.Errorf("%w: %s exceeds %d bytes", ErrTooLarge, rawURL, maxBytes)
	}
	return data, nil
}

// bindManifest rejects a release whose signed manifest does not describe the
// selected asset (a metadata compromise cannot relabel an older artifact).
func bindManifest(manifest Manifest, rel *Release) error {
	switch {
	case manifest.Version != rel.Version:
		return fmt.Errorf("%w: manifest version %q disagrees with tag %q", ErrManifest, manifest.Version, rel.Version)
	case rel.Arch != "" && manifest.Arch != rel.Arch:
		return fmt.Errorf("%w: manifest arch %q disagrees with %q", ErrManifest, manifest.Arch, rel.Arch)
	case manifest.File != rel.AssetName:
		return fmt.Errorf("%w: manifest file %q disagrees with %q", ErrManifest, manifest.File, rel.AssetName)
	case rel.Channel != "" && manifest.Channel != rel.Channel:
		return fmt.Errorf("%w: manifest channel %q disagrees with %q", ErrManifest, manifest.Channel, rel.Channel)
	case rel.SHA256 != "" && !strings.EqualFold(manifest.SHA256, rel.SHA256):
		return fmt.Errorf("%w: manifest sha256 %q disagrees with %q", ErrManifest, manifest.SHA256, rel.SHA256)
	}
	return nil
}

// binaryPath returns the fixed target path resolved at construction. It never
// consults the live inode, so a rename cannot drift it.
func (a *Applier) binaryPath() (string, error) {
	path := strings.TrimSpace(a.BinaryPath)
	if path == "" {
		return "", errors.New("updates: binary path is not configured")
	}
	if !filepath.IsAbs(path) {
		return "", fmt.Errorf("updates: binary path must be absolute: %s", path)
	}
	return path, nil
}

// lockPath resolves the serialization lock file.
func (a *Applier) lockPath(binPath string) string {
	if strings.TrimSpace(a.LockPath) != "" {
		return a.LockPath
	}
	return binPath + ".lock"
}

// oldPath resolves where the previous binary is retained.
func (a *Applier) oldPath(binPath string) string {
	if strings.TrimSpace(a.OldPath) != "" {
		return a.OldPath
	}
	return binPath + OldSuffix
}

// maxBytes returns the download bound.
func (a *Applier) maxBytes() int64 {
	if a.MaxBytes > 0 {
		return a.MaxBytes
	}
	return DefaultMaxBytes
}

// fileExists reports whether path exists.
func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// isSymlink reports whether path is a symbolic link.
func isSymlink(path string) bool {
	info, err := os.Lstat(path)
	return err == nil && info.Mode()&os.ModeSymlink != 0
}

// syncDir best-effort fsyncs a directory so a rename is durable. Some
// filesystems reject fsync on a directory, so the error is ignored.
func syncDir(dir string) {
	if handle, err := os.Open(dir); err == nil {
		_ = handle.Sync()
		_ = handle.Close()
	}
}
