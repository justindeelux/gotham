package files

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/justindeelux/gotham/internal/store"
	"github.com/justindeelux/gotham/internal/store/sqlc"
)

// File version history (JUS-224). Every successful editor save records the
// body it replaced, so a bad save can be diffed and rolled back. Snapshots are
// stored per (server, root, path) and pruned in the inserting transaction: a
// file never keeps more than historyMaxVersions revisions or historyMaxBytes of
// bodies, so history cannot grow without bound.
const (
	// HistoryMaxVersionsEnv overrides the per-file revision count.
	HistoryMaxVersionsEnv = "FILES_HISTORY_MAX_VERSIONS"
	// HistoryMaxBytesEnv overrides the total snapshot bytes kept per file.
	HistoryMaxBytesEnv = "FILES_HISTORY_MAX_BYTES"
	// DefaultHistoryMaxVersions matches the panel's 100-copy default.
	DefaultHistoryMaxVersions = 100
	// DefaultHistoryMaxBytes keeps at most 32 MiB of bodies per file. At the
	// editor's 2 MiB ceiling that is still 16 full-size revisions.
	DefaultHistoryMaxBytes int64 = 32 << 20
)

// historyMaxVersionsFromEnv parses HistoryMaxVersionsEnv; 0 keeps the default.
func historyMaxVersionsFromEnv() int {
	n, err := strconv.Atoi(strings.TrimSpace(os.Getenv(HistoryMaxVersionsEnv)))
	if err != nil || n <= 0 {
		return 0
	}
	return n
}

// historyMaxBytesFromEnv parses HistoryMaxBytesEnv; 0 keeps the default.
func historyMaxBytesFromEnv() int64 {
	n, err := strconv.ParseInt(strings.TrimSpace(os.Getenv(HistoryMaxBytesEnv)), 10, 64)
	if err != nil || n <= 0 {
		return 0
	}
	return n
}

// Version is one stored snapshot without its body.
type Version struct {
	ID        uuid.UUID `json:"id"`
	CreatedAt time.Time `json:"created_at"`
	Size      int64     `json:"size"`
	Etag      string    `json:"etag"`
}

// VersionBody is one snapshot plus its content.
type VersionBody struct {
	Version Version
	Content string
}

// HistoryStore persists per-file snapshots. The production implementation is
// *store.Store (through storeHistory); nil disables history (the routes answer
// 503 and saves skip snapshots).
type HistoryStore interface {
	// SaveVersion records one pre-save body and applies the retention bounds
	// in the same transaction.
	SaveVersion(ctx context.Context, serverID uuid.UUID, root, path, etag string, content []byte, maxVersions int, maxBytes int64) error
	// ListVersions returns one file's revisions, newest first.
	ListVersions(ctx context.Context, serverID uuid.UUID, root, path string) ([]Version, error)
	// GetVersion reads one revision, scoped to the addressed file.
	GetVersion(ctx context.Context, serverID uuid.UUID, root, path string, id uuid.UUID) (VersionBody, error)
	// DeleteVersion removes one revision, reporting false when it is already
	// gone (or belongs to another file).
	DeleteVersion(ctx context.Context, serverID uuid.UUID, root, path string, id uuid.UUID) (bool, error)
}

// storeHistory adapts the shared store to the history seam.
type storeHistory struct {
	store *store.Store
}

// NewStoreHistory wraps the shared store as a HistoryStore.
func NewStoreHistory(s *store.Store) HistoryStore {
	return storeHistory{store: s}
}

// SaveVersion records one snapshot and prunes the file's older revisions.
func (h storeHistory) SaveVersion(ctx context.Context, serverID uuid.UUID, root, path, etag string, content []byte, maxVersions int, maxBytes int64) error {
	_, err := h.store.SaveFileVersion(ctx, sqlc.InsertFileVersionParams{
		ServerID: historyUUID(serverID),
		Root:     root,
		Path:     path,
		Etag:     etag,
		Size:     int64(len(content)),
		Content:  content,
	}, int64(maxVersions), maxBytes)
	return err
}

// ListVersions returns one file's revisions, newest first.
func (h storeHistory) ListVersions(ctx context.Context, serverID uuid.UUID, root, path string) ([]Version, error) {
	rows, err := h.store.ListFileVersions(ctx, sqlc.ListFileVersionsParams{
		ServerID: historyUUID(serverID),
		Root:     root,
		Path:     path,
	})
	if err != nil {
		return nil, err
	}
	versions := make([]Version, 0, len(rows))
	for _, row := range rows {
		versions = append(versions, Version{
			ID:        uuid.UUID(row.ID.Bytes),
			CreatedAt: row.CreatedAt.Time,
			Size:      row.Size,
			Etag:      row.Etag,
		})
	}
	return versions, nil
}

// GetVersion reads one revision, mapping a missing row to ErrNotFound.
func (h storeHistory) GetVersion(ctx context.Context, serverID uuid.UUID, root, path string, id uuid.UUID) (VersionBody, error) {
	row, err := h.store.GetFileVersion(ctx, sqlc.GetFileVersionParams{
		ID:       historyUUID(id),
		ServerID: historyUUID(serverID),
		Root:     root,
		Path:     path,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return VersionBody{}, ErrNotFound
	}
	if err != nil {
		return VersionBody{}, err
	}
	return VersionBody{
		Version: Version{
			ID:        uuid.UUID(row.ID.Bytes),
			CreatedAt: row.CreatedAt.Time,
			Size:      row.Size,
			Etag:      row.Etag,
		},
		Content: string(row.Content),
	}, nil
}

// DeleteVersion removes one revision.
func (h storeHistory) DeleteVersion(ctx context.Context, serverID uuid.UUID, root, path string, id uuid.UUID) (bool, error) {
	return h.store.DeleteFileVersion(ctx, sqlc.DeleteFileVersionParams{
		ID:       historyUUID(id),
		ServerID: historyUUID(serverID),
		Root:     root,
		Path:     path,
	})
}

// historyUUID renders a UUID for the generated queries.
func historyUUID(id uuid.UUID) pgtype.UUID {
	return pgtype.UUID{Bytes: id, Valid: true}
}

// History lists one file's snapshots, newest first. Owner/admin only: a
// snapshot holds the file body. It never dials the agent, so the history of a
// deleted or renamed file stays readable.
func (s *Service) History(ctx context.Context, serverID uuid.UUID, root, path string) ([]Version, error) {
	if !Enabled() {
		return nil, ErrDisabled
	}
	if err := validateRoot(root); err != nil {
		return nil, err
	}
	if err := validatePathArg("path", path); err != nil {
		return nil, err
	}
	if _, err := s.resolve(ctx, serverID, true); err != nil {
		return nil, err
	}
	history := s.historyStore()
	if history == nil {
		return nil, ErrHistoryUnavailable
	}
	versions, err := history.ListVersions(ctx, serverID, root, path)
	if err != nil {
		return nil, err
	}
	if versions == nil {
		versions = []Version{}
	}
	return versions, nil
}

// HistoryVersion reads one snapshot body, scoped to (server, root, path).
func (s *Service) HistoryVersion(ctx context.Context, serverID uuid.UUID, root, path string, versionID uuid.UUID) (body VersionBody, err error) {
	defer func() { s.audit(ctx, serverID, "read_history", root, path, err, "version_id", versionID.String()) }()
	if !Enabled() {
		return VersionBody{}, ErrDisabled
	}
	if err := validateRoot(root); err != nil {
		return VersionBody{}, err
	}
	if err := validatePathArg("path", path); err != nil {
		return VersionBody{}, err
	}
	if versionID == uuid.Nil {
		return VersionBody{}, fmt.Errorf("%w: version id is required", ErrValidation)
	}
	if _, err := s.resolve(ctx, serverID, true); err != nil {
		return VersionBody{}, err
	}
	history := s.historyStore()
	if history == nil {
		return VersionBody{}, ErrHistoryUnavailable
	}
	return history.GetVersion(ctx, serverID, root, path, versionID)
}

// DeleteVersion permanently removes one snapshot. The current file is never
// touched.
func (s *Service) DeleteVersion(ctx context.Context, serverID uuid.UUID, root, path string, versionID uuid.UUID) (err error) {
	defer func() { s.audit(ctx, serverID, "delete_history", root, path, err, "version_id", versionID.String()) }()
	if !Enabled() {
		return ErrDisabled
	}
	if err := validateRoot(root); err != nil {
		return err
	}
	if err := validatePathArg("path", path); err != nil {
		return err
	}
	if versionID == uuid.Nil {
		return fmt.Errorf("%w: version id is required", ErrValidation)
	}
	if _, err := s.resolve(ctx, serverID, true); err != nil {
		return err
	}
	history := s.historyStore()
	if history == nil {
		return ErrHistoryUnavailable
	}
	removed, err := history.DeleteVersion(ctx, serverID, root, path, versionID)
	if err != nil {
		return err
	}
	if !removed {
		return ErrNotFound
	}
	return nil
}

// RestoreVersion writes one snapshot back over the file. The body the restore
// replaces is snapshotted first (so a restore is itself undoable), then the
// version is streamed through the normal conflict-checked write path: a file
// that changed since it was read answers 409 and is never clobbered. A restore
// into a deleted path recreates the file. The restore is audit-logged with the
// version id.
func (s *Service) RestoreVersion(ctx context.Context, serverID uuid.UUID, root, path string, versionID uuid.UUID) (result WriteResult, err error) {
	defer func() { s.audit(ctx, serverID, "restore_history", root, path, err, "version_id", versionID.String()) }()
	if !Enabled() {
		return WriteResult{}, ErrDisabled
	}
	if err := validateRoot(root); err != nil {
		return WriteResult{}, err
	}
	if err := validatePathArg("path", path); err != nil {
		return WriteResult{}, err
	}
	if versionID == uuid.Nil {
		return WriteResult{}, fmt.Errorf("%w: version id is required", ErrValidation)
	}
	if _, err := s.resolve(ctx, serverID, true); err != nil {
		return WriteResult{}, err
	}
	history := s.historyStore()
	if history == nil {
		return WriteResult{}, ErrHistoryUnavailable
	}
	version, err := history.GetVersion(ctx, serverID, root, path, versionID)
	if err != nil {
		return WriteResult{}, err
	}
	// The current body must be readable before it is replaced: a restore that
	// cannot snapshot what it overwrites would silently drop the only copy.
	prior, priorErr := s.readContent(ctx, serverID, root, path)
	expectEtag := ""
	switch {
	case priorErr == nil:
		expectEtag = prior.Etag
	case errors.Is(priorErr, ErrNotFound):
		// The file was deleted or renamed: restoring recreates it.
	default:
		return WriteResult{}, priorErr
	}
	result, err = s.streamWrite(ctx, serverID, WriteRequest{
		Root:       root,
		Path:       path,
		ExpectEtag: expectEtag,
		Size:       int64(len(version.Content)),
	}, strings.NewReader(version.Content))
	if err != nil {
		return WriteResult{}, err
	}
	if priorErr == nil {
		s.recordVersion(ctx, serverID, root, path, prior)
	}
	return result, nil
}

// recordVersion stores one pre-save body. A history failure never fails the
// save that already committed: it is logged (never with the body) so an
// operator can see the missing revision.
func (s *Service) recordVersion(ctx context.Context, serverID uuid.UUID, root, path string, prior Content) {
	history := s.historyStore()
	if history == nil {
		return
	}
	if err := history.SaveVersion(ctx, serverID, root, path, prior.Etag, []byte(prior.Content), s.historyMaxVersions, s.historyMaxBytes); err != nil {
		s.logger.Warn("files: could not record file version",
			"server_id", serverID.String(), "root", root, "relpath", path, "error", err)
	}
}
