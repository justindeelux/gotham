package updates

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/minio/selfupdate"
)

const (
	// DefaultMaxBytes bounds the release artifact download (a Gotham binary is
	// tens of MiB; the ceiling leaves headroom without allowing a disk-fill).
	DefaultMaxBytes = 200 << 20
	// maxSignatureBytes bounds the detached signature file.
	maxSignatureBytes = 4 << 10
	// maxChecksumsBytes bounds the checksums file.
	maxChecksumsBytes = 1 << 20
	// OldSuffix is the suffix of the retained previous binary.
	OldSuffix = ".old"
)

// Download/verification/apply failures.
var (
	// ErrDownload is returned when the artifact cannot be fetched.
	ErrDownload = errors.New("updates: download failed")
	// ErrTooLarge is returned when a download exceeds its bound.
	ErrTooLarge = errors.New("updates: download too large")
	// ErrChecksum is returned when the checksums file has no entry for the
	// asset or is unusable.
	ErrChecksum = errors.New("updates: checksum unavailable")
	// ErrChecksumMismatch is returned when the artifact SHA-256 differs.
	ErrChecksumMismatch = errors.New("updates: checksum mismatch")
	// ErrApply is returned when the binary swap fails.
	ErrApply = errors.New("updates: apply failed")
	// ErrNoBackup is returned when no <binary>.old exists to roll back to.
	ErrNoBackup = errors.New("updates: no previous binary to roll back to")
	// ErrNoRelease is returned when Apply is called without a release.
	ErrNoUpdate = errors.New("updates: no update available")
)

// Applier downloads and verifies a release, then swaps the running binary.
// Verification always happens before the binary is touched, and the previous
// binary is retained at <binary>.old for Rollback.
type Applier struct {
	// Client is the HTTP client; a bounded default is used when nil.
	Client *http.Client
	// Verifier holds the release public key. When nil, Apply fails closed.
	Verifier *Verifier
	// BinaryPath is the target executable; empty means the running executable.
	BinaryPath string
	// OldPath is where the previous binary is kept; empty means
	// <BinaryPath>.old.
	OldPath string
	// Restart activates the new binary (restart + healthcheck + rollback
	// wrapper). Empty means the swap succeeds without an automatic restart.
	Restart func(ctx context.Context) error
	// Timeout bounds each download when Client is nil.
	Timeout time.Duration
	// MaxBytes bounds the artifact download.
	MaxBytes int64
}

// Apply downloads, verifies and swaps in the release's platform asset. The
// current binary is left untouched unless the artifact verifies; on a restart
// failure the previous binary is restored and an error is returned.
func (a *Applier) Apply(ctx context.Context, rel *Release) error {
	if rel == nil {
		return ErrNoUpdate
	}
	if a.Verifier == nil {
		return ErrNoPublicKey
	}
	if err := validateURL(rel.AssetURL); err != nil {
		return err
	}
	if err := validateURL(rel.SignatureURL); err != nil {
		return err
	}

	binPath, err := a.binaryPath()
	if err != nil {
		return err
	}

	data, err := a.fetch(ctx, rel.AssetURL, a.maxBytes())
	if err != nil {
		return err
	}
	signature, err := a.fetch(ctx, rel.SignatureURL, maxSignatureBytes)
	if err != nil {
		return err
	}
	if err := a.Verifier.Verify(data, signature); err != nil {
		return err
	}
	if strings.TrimSpace(rel.ChecksumURL) != "" {
		checksums, err := a.fetch(ctx, rel.ChecksumURL, maxChecksumsBytes)
		if err != nil {
			return err
		}
		if err := verifyChecksum(checksums, rel.AssetName, data); err != nil {
			return err
		}
	}

	// Verified: swap via selfupdate, retaining the previous binary.
	oldPath := a.oldPath(binPath)
	if err := selfupdate.Apply(bytes.NewReader(data), selfupdate.Options{
		TargetPath:  binPath,
		TargetMode:  0o755,
		OldSavePath: oldPath,
	}); err != nil {
		if rollbackErr := selfupdate.RollbackError(err); rollbackErr != nil {
			return fmt.Errorf("%w: %v (rollback failed: %v)", ErrApply, err, rollbackErr)
		}
		return fmt.Errorf("%w: %v", ErrApply, err)
	}

	if a.Restart != nil {
		if err := a.Restart(ctx); err != nil {
			if rollbackErr := a.rollbackTo(binPath); rollbackErr != nil {
				return fmt.Errorf("%w: restart failed: %v (rollback failed: %v)", ErrApply, err, rollbackErr)
			}
			return fmt.Errorf("%w: restart failed: %v", ErrApply, err)
		}
	}
	return nil
}

// Rollback restores the retained previous binary over the current one.
func (a *Applier) Rollback() error {
	binPath, err := a.binaryPath()
	if err != nil {
		return err
	}
	return a.rollbackTo(binPath)
}

// rollbackTo renames <binPath>.old over binPath.
func (a *Applier) rollbackTo(binPath string) error {
	oldPath := a.oldPath(binPath)
	if _, err := os.Stat(oldPath); err != nil {
		return fmt.Errorf("%w: %s", ErrNoBackup, oldPath)
	}
	if err := os.Rename(oldPath, binPath); err != nil {
		return fmt.Errorf("updates: rollback: %w", err)
	}
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

// verifyChecksum checks the SHA-256 of data against the entry for name in a
// "sha256  filename" checksums file.
func verifyChecksum(checksums []byte, name string, data []byte) error {
	want := ""
	for _, line := range strings.Split(string(checksums), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		filename := strings.TrimPrefix(fields[1], "*")
		if filepath.Base(filename) == filepath.Base(name) {
			want = strings.ToLower(fields[0])
			break
		}
	}
	if want == "" {
		return fmt.Errorf("%w: %s not listed", ErrChecksum, name)
	}
	sum := sha256.Sum256(data)
	if hex.EncodeToString(sum[:]) != want {
		return ErrChecksumMismatch
	}
	return nil
}
