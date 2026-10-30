package cleanup

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/justindeelux/gotham/internal/backups"
	"github.com/justindeelux/gotham/internal/store"
)

// LeftoverStore is the control-plane persistence the Gotham leftovers need.
// The scan lists what a backups row still owns and how much history is older
// than the retention window; the clean deletes history in bounded batches. The
// retention windows are read from the settings row on every call, so a saved
// value applies without a restart.
type LeftoverStore interface {
	ListBackupLocations(ctx context.Context) ([]string, error)
	CleanupRetentionDays(ctx context.Context) (historyDays, backupFileMinAgeDays int32, err error)
	HistoryStats(ctx context.Context, cutoff time.Time) ([]store.HistoryStat, error)
	DeleteHistoryBatch(ctx context.Context, name string, cutoff time.Time, batch int) (int64, error)
}

// LeftoversConfig wires the control-plane leftover categories.
type LeftoversConfig struct {
	Store     LeftoverStore
	BackupDir string
	Logger    *slog.Logger
	Now       func() time.Time
}

// LeftoversSource owns Gotham's own leftovers on the control-plane node: local
// backup files no row references and history rows past the retention window.
// The cleanup service only asks it about the local node.
type LeftoversSource struct {
	store     LeftoverStore
	backupDir string
	logger    *slog.Logger
	now       func() time.Time
}

// NewLeftoversSource builds the control-plane leftover source. An empty
// BackupDir is resolved like the backup service does (GOTHAM_BACKUP_DIR, then
// data/backups).
func NewLeftoversSource(cfg LeftoversConfig) *LeftoversSource {
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	return &LeftoversSource{
		store:     cfg.Store,
		backupDir: backups.LocalBackupDir(cfg.BackupDir),
		logger:    logger,
		now:       now,
	}
}

// Scan lists the orphaned backup files and the history rows older than the
// owner's retention windows. update_files is not offered: the only gotham-owned
// updater paths hold the live binary, its rollback copy and live state, and
// every downloaded payload lives in a root-owned staging directory.
func (l *LeftoversSource) Scan(ctx context.Context) ([]Item, error) {
	retention, err := l.retention(ctx)
	if err != nil {
		return nil, err
	}
	files, err := l.scanBackupFiles(ctx, retention)
	if err != nil {
		return nil, err
	}
	history, err := l.scanHistory(ctx, retention)
	if err != nil {
		return nil, err
	}
	return append(files, history...), nil
}

type retentionWindow struct {
	historyDays          int
	backupFileMinAgeDays int
}

func (l *LeftoversSource) retention(ctx context.Context) (retentionWindow, error) {
	historyDays, backupAge, err := l.store.CleanupRetentionDays(ctx)
	if err != nil {
		return retentionWindow{}, fmt.Errorf("cleanup: read retention settings: %w", err)
	}
	return retentionWindow{historyDays: int(historyDays), backupFileMinAgeDays: int(backupAge)}, nil
}

func (l *LeftoversSource) scanBackupFiles(ctx context.Context, retention retentionWindow) ([]Item, error) {
	root, err := l.backupRoot()
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	referenced, err := l.referencedFiles(ctx, root)
	if err != nil {
		return nil, err
	}
	cutoff := l.now().Add(-time.Duration(retention.backupFileMinAgeDays) * 24 * time.Hour)
	var items []Item
	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			l.logger.Debug("cleanup: skip unreadable backup entry", "path", path, "error", walkErr)
			return nil
		}
		if entry.IsDir() || entry.Type()&fs.ModeSymlink != 0 || !entry.Type().IsRegular() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			l.logger.Debug("cleanup: skip unreadable backup file", "path", path, "error", err)
			return nil
		}
		if info.ModTime().After(cutoff) {
			return nil
		}
		if referenced[l.canonical(path)] {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return nil
		}
		items = append(items, Item{
			ID:          idGothamPrefix + "backup_file:" + filepath.ToSlash(rel),
			Category:    CategoryBackupFiles,
			Name:        filepath.ToSlash(rel),
			SizeBytes:   info.Size(),
			CreatedUnix: info.ModTime().Unix(),
			Reason:      "no backups row references this file",
		})
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("cleanup: list backup files: %w", err)
	}
	return items, nil
}

func (l *LeftoversSource) scanHistory(ctx context.Context, retention retentionWindow) ([]Item, error) {
	cutoff := l.now().Add(-time.Duration(retention.historyDays) * 24 * time.Hour)
	stats, err := l.store.HistoryStats(ctx, cutoff)
	if err != nil {
		return nil, err
	}
	items := make([]Item, 0, len(stats))
	for _, stat := range stats {
		if stat.Rows == 0 {
			continue
		}
		items = append(items, Item{
			ID:        idGothamPrefix + "history:" + stat.Name,
			Category:  CategoryHistory,
			Name:      historyLabel(stat.Name),
			SizeBytes: stat.Bytes,
			Reason:    fmt.Sprintf("%d rows older than %d days", stat.Rows, retention.historyDays),
		})
	}
	return items, nil
}

// Clean removes one Gotham leftover. Every branch re-checks the current world:
// a file may have become referenced since the scan, and the retention windows
// may have changed.
func (l *LeftoversSource) Clean(ctx context.Context, id string, minAge time.Duration) ItemResult {
	result := ItemResult{ID: id}
	switch {
	case strings.HasPrefix(id, idGothamPrefix+"backup_file:"):
		return l.cleanBackupFile(ctx, result, strings.TrimPrefix(id, idGothamPrefix+"backup_file:"), minAge)
	case strings.HasPrefix(id, idGothamPrefix+"history:"):
		return l.cleanHistory(ctx, result, strings.TrimPrefix(id, idGothamPrefix+"history:"))
	default:
		result.Status = StatusSkipped
		result.Message = "unknown gotham leftover"
		return result
	}
}

func (l *LeftoversSource) cleanBackupFile(
	ctx context.Context, result ItemResult, rel string, minAge time.Duration,
) ItemResult {
	result.Category = CategoryBackupFiles
	result.Name = rel
	retention, err := l.retention(ctx)
	if err != nil {
		result.Status, result.Message = StatusFailed, err.Error()
		return result
	}
	root, path, err := l.safeBackupPath(rel)
	if err != nil {
		result.Status, result.Message = StatusSkipped, err.Error()
		return result
	}
	info, err := os.Lstat(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		result.Status, result.Message = StatusSkipped, "no longer present"
		return result
	case err != nil:
		result.Status, result.Message = StatusFailed, err.Error()
		return result
	case info.Mode()&os.ModeSymlink != 0:
		result.Status, result.Message = StatusSkipped, "symlink"
		return result
	case !info.Mode().IsRegular():
		result.Status, result.Message = StatusSkipped, "not a regular file"
		return result
	}
	requiredAge := time.Duration(retention.backupFileMinAgeDays) * 24 * time.Hour
	if minAge > requiredAge {
		requiredAge = minAge
	}
	if info.ModTime().After(l.now().Add(-requiredAge)) {
		result.Status, result.Message = StatusSkipped, "too new"
		return result
	}
	referenced, err := l.referencedFiles(ctx, root)
	if err != nil {
		result.Status, result.Message = StatusFailed, err.Error()
		return result
	}
	if referenced[l.canonical(path)] {
		result.Status, result.Message = StatusSkipped, "referenced by a backups row"
		return result
	}
	if err := os.Remove(path); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			result.Status, result.Message = StatusSkipped, "no longer present"
		} else {
			result.Status, result.Message = StatusFailed, err.Error()
		}
		return result
	}
	result.Status = statusRemoved
	result.ReclaimedBytes = info.Size()
	return result
}

func (l *LeftoversSource) cleanHistory(ctx context.Context, result ItemResult, name string) ItemResult {
	result.Category = CategoryHistory
	result.Name = historyLabel(name)
	if !knownHistory(name) {
		result.Status, result.Message = StatusSkipped, "unknown history category"
		return result
	}
	retention, err := l.retention(ctx)
	if err != nil {
		result.Status, result.Message = StatusFailed, err.Error()
		return result
	}
	cutoff := l.now().Add(-time.Duration(retention.historyDays) * 24 * time.Hour)
	stats, err := l.store.HistoryStats(ctx, cutoff)
	if err != nil {
		result.Status, result.Message = StatusFailed, err.Error()
		return result
	}
	var before store.HistoryStat
	for _, stat := range stats {
		if stat.Name == name {
			before = stat
			break
		}
	}
	if before.Rows == 0 {
		result.Status, result.Message = StatusSkipped, "no longer present"
		return result
	}
	for {
		deleted, err := l.store.DeleteHistoryBatch(ctx, name, cutoff, store.HistoryDeleteBatch)
		if err != nil {
			result.Status, result.Message = StatusFailed, err.Error()
			return result
		}
		if deleted == 0 {
			break
		}
	}
	result.Status = statusRemoved
	result.ReclaimedBytes = before.Bytes
	result.Message = fmt.Sprintf("%d rows", before.Rows)
	return result
}

// safeBackupPath resolves one relative id to a regular file inside the backup
// directory, rejecting absolute paths, parent traversal and symlinks. It
// returns the resolved root so the caller can compare referenced locations.
func (l *LeftoversSource) safeBackupPath(rel string) (string, string, error) {
	root, err := l.backupRoot()
	if errors.Is(err, fs.ErrNotExist) {
		return "", "", errors.New("backup directory is missing")
	}
	if err != nil {
		return "", "", err
	}
	if strings.TrimSpace(rel) == "" || filepath.IsAbs(rel) {
		return "", "", errors.New("invalid backup file path")
	}
	clean := filepath.Clean(filepath.FromSlash(rel))
	if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", "", errors.New("invalid backup file path")
	}
	target := filepath.Join(root, clean)
	if !pathWithin(root, target) {
		return "", "", errors.New("invalid backup file path")
	}
	resolved, err := filepath.EvalSymlinks(target)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return root, target, nil
		}
		return "", "", err
	}
	if !pathWithin(root, resolved) {
		return "", "", errors.New("invalid backup file path")
	}
	return root, resolved, nil
}

func (l *LeftoversSource) referencedFiles(ctx context.Context, root string) (map[string]bool, error) {
	locations, err := l.store.ListBackupLocations(ctx)
	if err != nil {
		return nil, fmt.Errorf("cleanup: list backup rows: %w", err)
	}
	referenced := make(map[string]bool, len(locations))
	for _, location := range locations {
		if path, ok := localArtifactPath(location, root); ok {
			referenced[l.canonical(path)] = true
		}
	}
	return referenced, nil
}

func (l *LeftoversSource) backupRoot() (string, error) {
	abs, err := filepath.Abs(l.backupDir)
	if err != nil {
		return "", fmt.Errorf("cleanup: resolve backup directory: %w", err)
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", err
	}
	return resolved, nil
}

// canonical resolves a path that exists, so a referenced location compares
// equal to the file the walk found even when the backup directory is a symlink.
func (l *LeftoversSource) canonical(path string) string {
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		return filepath.Clean(resolved)
	}
	return filepath.Clean(path)
}

// localArtifactPath turns a backups location into an absolute path. Locations
// outside this backup directory stay comparable by absolute path.
func localArtifactPath(location, root string) (string, bool) {
	const prefix = "file://"
	if !strings.HasPrefix(location, prefix) {
		return "", false
	}
	path := strings.TrimPrefix(location, prefix)
	if !filepath.IsAbs(path) {
		path = filepath.Join(root, path)
	}
	return filepath.Clean(path), true
}

func pathWithin(root, target string) bool {
	rel, err := filepath.Rel(root, target)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func knownHistory(name string) bool {
	switch name {
	case store.HistoryDeploymentLogs, store.HistoryToolchainJobLogs,
		store.HistoryRequestReceipts, store.HistoryWebhookEvents:
		return true
	}
	return false
}

func historyLabel(name string) string {
	switch name {
	case store.HistoryDeploymentLogs:
		return "Deployment logs"
	case store.HistoryToolchainJobLogs:
		return "Toolchain job logs"
	case store.HistoryRequestReceipts:
		return "Request receipts"
	case store.HistoryWebhookEvents:
		return "Webhook events"
	default:
		return name
	}
}
