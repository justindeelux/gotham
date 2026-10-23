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

const (
	StatusStaged         = "staged"
	StatusOK             = "ok"
	StatusRolledBack     = "rolled_back"
	StatusRollbackFailed = "rollback_failed"
	StatusNoBackup       = "no_backup"
	StatusWrapperFailed  = "wrapper_failed"
	StatusResuming       = "resuming"
)

type Status struct {
	Result  string    `json:"result"`
	Version string    `json:"version,omitempty"`
	Detail  string    `json:"detail,omitempty"`
	At      time.Time `json:"at,omitempty"`
}

type StatusStore struct {
	Path string
}

func NewStatusStore(path string) *StatusStore {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil
	}
	return &StatusStore{Path: path}
}

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

func (s *StatusStore) Remove() error {
	if s == nil {
		return nil
	}
	if err := os.Remove(s.Path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func singleLine(value string) string {
	return strings.NewReplacer("\n", " ", "\r", " ").Replace(value)
}

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
