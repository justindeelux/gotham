package updates

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
)

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
// previous binary as <binary>.old until the new one is proven healthy, and is
// recoverable after a crash (Recover).
type Applier struct {
	// Client is the HTTP client; a bounded default is used when nil.
	Client *http.Client
	// Verifier holds the release public key. When nil, Apply fails closed.
	Verifier *Verifier
	// BinaryPath is the target executable; empty means the running executable.
	BinaryPath string
	// OldPath is where the previous binary is retained; empty means
	// <BinaryPath>.old.
	OldPath string
	// LockPath is the serialization lock; empty means <BinaryPath>.lock.
	LockPath string
	// Restart activates the new binary (restart + healthcheck + rollback
	// wrapper). Empty means the swap succeeds without an automatic restart.
	Restart func(ctx context.Context) error
	// Status records the durable outcome. Optional.
	Status *StatusStore
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
		return a.installLocked(binPath, data)
	}); err != nil {
		return nil, err
	}

	if a.Status != nil {
		_ = a.Status.Write(Status{Result: StatusStaged, Version: manifest.Version})
	}

	staged := a.Restart != nil
	if a.Restart != nil {
		if err := a.Restart(ctx); err != nil {
			rollbackErr := a.Rollback()
			detail := err.Error()
			if rollbackErr != nil {
				detail = fmt.Sprintf("%s (rollback: %v)", detail, rollbackErr)
				if a.Status != nil {
					_ = a.Status.Write(Status{Result: StatusRollbackFailed, Version: manifest.Version, Detail: detail})
				}
				return nil, fmt.Errorf("%w: restart failed and rollback failed: %s", ErrApply, detail)
			}
			if a.Status != nil {
				_ = a.Status.Write(Status{Result: StatusRolledBack, Version: manifest.Version, Detail: detail})
			}
			return nil, fmt.Errorf("%w: restart failed: %v", ErrApply, err)
		}
	}
	return &ApplyOutcome{Version: manifest.Version, Staged: staged}, nil
}

// Rollback restores the retained previous binary over the current one. It is
// serialized with Apply.
func (a *Applier) Rollback() error {
	binPath, err := a.binaryPath()
	if err != nil {
		return err
	}
	return withFileLock(a.lockPath(binPath), func() error {
		backup := a.oldPath(binPath)
		if _, err := os.Stat(backup); err != nil {
			return fmt.Errorf("%w: %s", ErrNoBackup, backup)
		}
		if err := os.Rename(backup, binPath); err != nil {
			return fmt.Errorf("updates: rollback: %w", err)
		}
		syncDir(filepath.Dir(binPath))
		return nil
	})
}

// Recover repairs a crash-interrupted swap and removes stale staging files. It
// reports whether it restored a missing target from its backup. It is safe to
// call at startup.
func (a *Applier) Recover() (bool, error) {
	binPath, err := a.binaryPath()
	if err != nil {
		return false, err
	}
	restored := false
	err = withFileLock(a.lockPath(binPath), func() error {
		before := fileExists(binPath)
		if err := a.recoverLocked(binPath); err != nil {
			return err
		}
		restored = !before && fileExists(binPath)
		return nil
	})
	return restored, err
}

// recoverLocked cleans stale staging files and restores the backup when the
// target is missing. The caller must hold the lock.
func (a *Applier) recoverLocked(binPath string) error {
	dir := filepath.Dir(binPath)
	pattern := filepath.Join(dir, "."+filepath.Base(binPath)+".new.*")
	if matches, err := filepath.Glob(pattern); err == nil {
		for _, match := range matches {
			_ = os.Remove(match)
		}
	}
	if fileExists(binPath) {
		return nil
	}
	backup := a.oldPath(binPath)
	if !fileExists(backup) {
		return nil
	}
	if err := os.Rename(backup, binPath); err != nil {
		return fmt.Errorf("updates: recover: %w", err)
	}
	syncDir(dir)
	return nil
}

// installLocked writes data to a unique staging file, fsyncs it, and swaps it
// into place, moving the current binary to the backup. The caller must hold the
// lock.
func (a *Applier) installLocked(binPath string, data []byte) error {
	dir := filepath.Dir(binPath)
	base := filepath.Base(binPath)

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

	backup := a.oldPath(binPath)
	if err := os.Rename(binPath, backup); err != nil {
		return fmt.Errorf("%w: move current binary: %v", ErrApply, err)
	}
	if err := os.Rename(stagingName, binPath); err != nil {
		_ = os.Rename(backup, binPath)
		return fmt.Errorf("%w: activate: %v", ErrApply, err)
	}
	stagingName = ""
	syncDir(dir)
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
		client = defaultHTTPClient(defaultDuration(a.Timeout, defaultTimeout))
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
	}
	return nil
}

// binaryPath resolves the target executable.
func (a *Applier) binaryPath() (string, error) {
	if strings.TrimSpace(a.BinaryPath) != "" {
		return a.BinaryPath, nil
	}
	path, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("updates: resolve executable: %w", err)
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

// syncDir best-effort fsyncs a directory so a rename is durable. Some
// filesystems reject fsync on a directory, so the error is ignored.
func syncDir(dir string) {
	if handle, err := os.Open(dir); err == nil {
		_ = handle.Sync()
		_ = handle.Close()
	}
}
