package files

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path"
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

	// historyWriteTimeout bounds one snapshot insert or cleanup. It runs on a
	// detached context, so a slow database still cannot stall a request
	// forever while a client disconnect cannot lose the revision.
	historyWriteTimeout = 10 * time.Second
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

// VersionBody is one snapshot plus its content. Content is the body decoded to
// UTF-8 for the diff view; raw keeps the stored bytes so a legacy-encoded
// snapshot restores byte-for-byte.
type VersionBody struct {
	Version Version
	Content string
	raw     []byte
}

// HistoryStore persists per-file snapshots. The production implementation is
// *store.Store (through storeHistory); nil disables history (the routes answer
// 503 and saves skip snapshots).
type HistoryStore interface {
	// SaveVersion records one pre-save body and applies the retention bounds
	// in the same transaction. It returns the stored revision, which a restore
	// deletes again when its write is refused.
	SaveVersion(ctx context.Context, serverID uuid.UUID, root, path, etag string, content []byte, maxVersions int, maxBytes int64) (Version, error)
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
func (h storeHistory) SaveVersion(ctx context.Context, serverID uuid.UUID, root, path, etag string, content []byte, maxVersions int, maxBytes int64) (Version, error) {
	row, err := h.store.SaveFileVersion(ctx, sqlc.InsertFileVersionParams{
		ServerID: historyUUID(serverID),
		Root:     root,
		Path:     path,
		Etag:     etag,
		Size:     int64(len(content)),
		Content:  content,
	}, int64(maxVersions), maxBytes)
	if err != nil {
		return Version{}, err
	}
	return Version{
		ID:        uuid.UUID(row.ID.Bytes),
		CreatedAt: row.CreatedAt.Time,
		Size:      row.Size,
		Etag:      row.Etag,
	}, nil
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
		Content: historyText(row.Content),
		raw:     row.Content,
	}, nil
}

// historyText decodes a stored snapshot body for the diff view. A body the
// editor detector does not recognise falls back to a lossy string rather than
// failing the history read; the restore path uses the raw bytes, so a fallback
// never changes what a restore writes.
func historyText(body []byte) string {
	if _, text, err := detectEncoding(body); err == nil {
		return text
	}
	return string(body)
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

// canonicalHistoryPath normalizes a root-relative path into the one key a
// file's history lives under. It mirrors the agent's own cleanRel for relative
// paths (path.Clean drops ".", duplicate and trailing separators), so
// "a//f", "a/./f" and "a/f/" all address the same single history. An absolute
// path is left absolute: the agent refuses it, so no revision is ever stored
// under it.
func canonicalHistoryPath(p string) string {
	if p == "" {
		return ""
	}
	return path.Clean(p)
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
	path = canonicalHistoryPath(path)
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
	path = canonicalHistoryPath(path)
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
	path = canonicalHistoryPath(path)
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
	path = canonicalHistoryPath(path)
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
	// Snapshot what the restore replaces BEFORE the write, and abort when it
	// cannot be stored: an overwrite whose prior body is unknown is exactly
	// the data loss this feature exists to prevent (aaPanel's FE-HIS-08 gap).
	var snapshot Version
	if priorErr == nil {
		snapshot, err = s.saveVersionDetached(ctx, serverID, root, path, prior)
		if err != nil {
			return WriteResult{}, fmt.Errorf("files: snapshot the current body before restore: %w", err)
		}
	}
	body := versionBytes(version)
	result, err = s.streamWrite(ctx, serverID, WriteRequest{
		Root:       root,
		Path:       path,
		ExpectEtag: expectEtag,
		Size:       int64(len(body)),
	}, bytes.NewReader(body))
	if err != nil {
		// A refused write (conflict or bad request) replaced nothing, so drop
		// the pre-snapshot instead of leaving a phantom revision. Any other
		// error may have committed on the node, so its snapshot is kept.
		if priorErr == nil && (errors.Is(err, ErrConflict) || errors.Is(err, ErrValidation)) {
			s.clearVersion(ctx, serverID, root, path, snapshot.ID)
		}
		return WriteResult{}, err
	}
	return result, nil
}

// saveVersionDetached stores one pre-save body on a context detached from the
// request, so a client disconnect or request deadline after the agent commit
// cannot lose the revision. The key is canonical, so every spelling of one path
// shares a single history. The body is stored exactly as read (see
// Content.raw), so a GBK/BIG5 file is versioned and restored byte-for-byte.
func (s *Service) saveVersionDetached(ctx context.Context, serverID uuid.UUID, root, path string, prior Content) (Version, error) {
	history := s.historyStore()
	if history == nil {
		return Version{}, ErrHistoryUnavailable
	}
	if prior.Truncated {
		// A tail view is not the file: snapshotting it would make a later
		// restore replace the whole file with its last lines.
		return Version{}, ErrTooLarge
	}
	saveCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), historyWriteTimeout)
	defer cancel()
	body := prior.raw
	if body == nil {
		body = []byte(prior.Content)
	}
	return history.SaveVersion(saveCtx, serverID, root, canonicalHistoryPath(path), prior.Etag, body, s.historyMaxVersions, s.historyMaxBytes)
}

// versionBytes returns the bytes to write for a restore: the raw snapshot when
// the store kept them, else the decoded text (a store that predates raw
// snapshots).
func versionBytes(version VersionBody) []byte {
	if version.raw != nil {
		return version.raw
	}
	return []byte(version.Content)
}

// recordVersion stores one pre-save body after a successful save. A history
// failure never fails the save that already committed: it is logged (never with
// the body) so an operator can see the missing revision.
func (s *Service) recordVersion(ctx context.Context, serverID uuid.UUID, root, path string, prior Content) {
	if _, err := s.saveVersionDetached(ctx, serverID, root, path, prior); err != nil {
		s.logger.Warn("files: could not record file version",
			"server_id", serverID.String(), "root", root, "relpath", path, "error", err)
	}
}

// clearVersion removes the pre-restore snapshot when the restore write was
// refused. It uses a detached context for the same reason as the insert; a
// failed cleanup is logged and leaves at worst one extra revision.
func (s *Service) clearVersion(ctx context.Context, serverID uuid.UUID, root, path string, versionID uuid.UUID) {
	history := s.historyStore()
	if history == nil {
		return
	}
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), historyWriteTimeout)
	defer cancel()
	if _, err := history.DeleteVersion(cleanupCtx, serverID, root, canonicalHistoryPath(path), versionID); err != nil {
		s.logger.Warn("files: could not remove the pre-restore version",
			"server_id", serverID.String(), "root", root, "relpath", path, "error", err)
	}
}
