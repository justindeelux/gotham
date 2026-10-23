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
	DefaultMaxBytes   = 200 << 20
	maxManifestBytes  = 64 << 10
	maxSignatureBytes = 4 << 10
	OldSuffix         = ".old"
)

var (
	ErrDownload         = errors.New("updates: download failed")
	ErrTooLarge         = errors.New("updates: download too large")
	ErrChecksumMismatch = errors.New("updates: checksum mismatch")
	ErrApply            = errors.New("updates: apply failed")
	ErrNoBackup         = errors.New("updates: no previous binary to roll back to")
	ErrNoUpdate         = errors.New("updates: no update available")
	ErrUpdatePending    = errors.New("updates: a previous update is still pending")
)

type RestartFunc func(ctx context.Context) (wait func() error, err error)

type ApplyOutcome struct {
	Version string
	Staged  bool
}

type Applier struct {
	Client          *http.Client
	Verifier        *Verifier
	BinaryPath      string
	OldPath         string
	LockPath        string
	Pending         *StatusStore
	Status          *StatusStore
	Restart         RestartFunc
	Timeout         time.Duration
	ArtifactTimeout time.Duration
	MaxBytes        int64
}

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

	data, err := a.fetchArtifact(ctx, rel.AssetURL, a.maxBytes())
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
		return false, nil
	}
	return restored, nil
}

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

func (a *Applier) reconcilePending(binPath string) {
	if a.Pending == nil {
		return
	}
	pending, err := a.Pending.Read()
	if err != nil || pending == nil || pending.Result != StatusStaged {
		return
	}
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

func inFlight(result string) bool {
	return result == StatusStaged || result == StatusResuming
}

func (a *Applier) pendingInFlight() bool {
	if a.Pending == nil {
		return false
	}
	pending, err := a.Pending.Read()
	return err == nil && pending != nil && inFlight(pending.Result)
}

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

func (a *Applier) fetch(ctx context.Context, rawURL string, maxBytes int64) ([]byte, error) {
	return a.fetchBounded(ctx, rawURL, maxBytes, defaultDuration(a.Timeout, defaultTimeout))
}

func (a *Applier) fetchArtifact(ctx context.Context, rawURL string, maxBytes int64) ([]byte, error) {
	return a.fetchBounded(ctx, rawURL, maxBytes, defaultDuration(a.ArtifactTimeout, defaultArtifactTimeout))
}

func (a *Applier) fetchBounded(ctx context.Context, rawURL string, maxBytes int64, timeout time.Duration) ([]byte, error) {
	if err := validateURL(rawURL); err != nil {
		return nil, err
	}
	client := a.Client
	if client == nil {
		client = DefaultHTTPClient(0)
	}
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "gotham-selfupdate")

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

func (a *Applier) lockPath(binPath string) string {
	if strings.TrimSpace(a.LockPath) != "" {
		return a.LockPath
	}
	return binPath + ".lock"
}

func (a *Applier) oldPath(binPath string) string {
	if strings.TrimSpace(a.OldPath) != "" {
		return a.OldPath
	}
	return binPath + OldSuffix
}

func (a *Applier) maxBytes() int64 {
	if a.MaxBytes > 0 {
		return a.MaxBytes
	}
	return DefaultMaxBytes
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func isSymlink(path string) bool {
	info, err := os.Lstat(path)
	return err == nil && info.Mode()&os.ModeSymlink != 0
}

func syncDir(dir string) {
	if handle, err := os.Open(dir); err == nil {
		_ = handle.Sync()
		_ = handle.Close()
	}
}
