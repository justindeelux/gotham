package updatecore

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// Status results recorded by the restart/healthcheck wrapper and the control
// plane's pending marker.
const (
	// StatusStaged means the swap succeeded and the restart is pending.
	StatusStaged = "staged"
	// StatusOK means the new binary restarted and passed its health check.
	StatusOK = "ok"
	// StatusRolledBack means the new binary failed and the previous one was
	// restored.
	StatusRolledBack = "rolled_back"
	// StatusRollbackFailed means the new binary failed and the previous one
	// could not be restored.
	StatusRollbackFailed = "rollback_failed"
	// StatusNoBackup means activation failed and no previous binary existed.
	StatusNoBackup = "no_backup"
	// StatusWrapperFailed means the privileged wrapper could not acquire the
	// update lock (or otherwise failed closed before it could act); the control
	// plane rolls back and records rolled_back/rollback_failed. An asynchronous
	// wrapper exit is also handled by the control plane's rollback, not by this
	// status.
	StatusWrapperFailed = "wrapper_failed"
	// StatusResuming means a staged update was re-launched at startup (crash
	// or reboot during the health window) and its outcome is still pending.
	StatusResuming = "resuming"
)

// Status is the durable outcome of the most recent update attempt.
type Status struct {
	Result  string    `json:"result"`
	Version string    `json:"version,omitempty"`
	Detail  string    `json:"detail,omitempty"`
	At      time.Time `json:"at,omitempty"`
}

// StatusStore reads and writes the update status file. The privileged wrapper
// writes it as root; the control plane only reads it.
type StatusStore struct {
	Path string
}

// NewStatusStore returns a store at path, or nil when path is empty (status
// tracking disabled).
func NewStatusStore(path string) *StatusStore {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil
	}
	return &StatusStore{Path: path}
}

// Read loads the recorded status. A missing file returns (nil, nil): no update
// has run yet. A non-regular file (FIFO, device, directory or symlink) is
// ignored: both the pending marker and the status file live in a directory the
// service user can write, and opening a planted FIFO would block.
func (s *StatusStore) Read() (*Status, error) {
	if s == nil {
		return nil, nil
	}
	data, err := readRegularFile(s.Path)
	if err != nil {
		return nil, err
	}
	if data == nil {
		return nil, nil
	}
	status := &Status{}
	for _, line := range strings.Split(string(data), "\n") {
		key, value, ok := strings.Cut(strings.TrimSpace(line), "=")
		if !ok {
			continue
		}
		switch strings.TrimSpace(key) {
		case "result":
			status.Result = strings.TrimSpace(value)
		case "version":
			status.Version = strings.TrimSpace(value)
		case "detail":
			status.Detail = strings.TrimSpace(value)
		case "at":
			if secs, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64); err == nil {
				status.At = time.Unix(secs, 0).UTC()
			}
		}
	}
	if status.Result == "" {
		return nil, fmt.Errorf("%w: status has no result", ErrManifest)
	}
	return status, nil
}

// Write records status atomically (temp file + rename).
func (s *StatusStore) Write(status Status) error {
	if s == nil {
		return nil
	}
	if status.At.IsZero() {
		status.At = time.Now().UTC()
	}
	var b strings.Builder
	fmt.Fprintf(&b, "result=%s\n", status.Result)
	fmt.Fprintf(&b, "version=%s\n", status.Version)
	fmt.Fprintf(&b, "detail=%s\n", singleLine(status.Detail))
	fmt.Fprintf(&b, "at=%d\n", status.At.Unix())

	dir := filepath.Dir(s.Path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".update.status.*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.WriteString(b.String()); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, s.Path)
}

// Remove deletes the store file, ignoring a missing file. It is used to clear
// the pending marker once an update is confirmed.
func (s *StatusStore) Remove() error {
	if s == nil {
		return nil
	}
	if err := os.Remove(s.Path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// singleLine keeps a detail value on one line in the status file.
func singleLine(value string) string {
	return strings.NewReplacer("\n", " ", "\r", " ").Replace(value)
}

// readRegularFile reads path only when it is a regular file. Symlinks and
// non-regular files (FIFO, device, directory) return (nil, nil) instead of
// following a link or blocking; the open uses O_NONBLOCK so a FIFO swapped in
// after the Lstat cannot block either. A missing file also returns (nil, nil).
func readRegularFile(path string) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, nil
	}
	file, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !opened.Mode().IsRegular() {
		return nil, nil
	}
	return io.ReadAll(file)
}
