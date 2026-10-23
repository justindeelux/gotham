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

const (
	HistoryMaxVersionsEnv           = "FILES_HISTORY_MAX_VERSIONS"
	HistoryMaxBytesEnv              = "FILES_HISTORY_MAX_BYTES"
	DefaultHistoryMaxVersions       = 100
	DefaultHistoryMaxBytes    int64 = 32 << 20

	historyWriteTimeout = 10 * time.Second
)

func historyMaxVersionsFromEnv() int {
	n, err := strconv.Atoi(strings.TrimSpace(os.Getenv(HistoryMaxVersionsEnv)))
	if err != nil || n <= 0 {
		return 0
	}
	return n
}

func historyMaxBytesFromEnv() int64 {
	n, err := strconv.ParseInt(strings.TrimSpace(os.Getenv(HistoryMaxBytesEnv)), 10, 64)
	if err != nil || n <= 0 {
		return 0
	}
	return n
}

type Version struct {
	ID        uuid.UUID `json:"id"`
	CreatedAt time.Time `json:"created_at"`
	Size      int64     `json:"size"`
	Etag      string    `json:"etag"`
}

type VersionBody struct {
	Version Version
	Content string
	raw     []byte
}

type HistoryStore interface {
	SaveVersion(ctx context.Context, serverID uuid.UUID, root, path, etag string, content []byte, maxVersions int, maxBytes int64) (Version, error)
	ListVersions(ctx context.Context, serverID uuid.UUID, root, path string) ([]Version, error)
	GetVersion(ctx context.Context, serverID uuid.UUID, root, path string, id uuid.UUID) (VersionBody, error)
	DeleteVersion(ctx context.Context, serverID uuid.UUID, root, path string, id uuid.UUID) (bool, error)
}

type storeHistory struct {
	store *store.Store
}

func NewStoreHistory(s *store.Store) HistoryStore {
	return storeHistory{store: s}
}

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

func historyText(body []byte) string {
	if _, text, err := detectEncoding(body); err == nil {
		return text
	}
	return string(body)
}

func (h storeHistory) DeleteVersion(ctx context.Context, serverID uuid.UUID, root, path string, id uuid.UUID) (bool, error) {
	return h.store.DeleteFileVersion(ctx, sqlc.DeleteFileVersionParams{
		ID:       historyUUID(id),
		ServerID: historyUUID(serverID),
		Root:     root,
		Path:     path,
	})
}

func historyUUID(id uuid.UUID) pgtype.UUID {
	return pgtype.UUID{Bytes: id, Valid: true}
}

func canonicalHistoryPath(p string) string {
	if p == "" {
		return ""
	}
	return path.Clean(p)
}

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
	prior, priorErr := s.readContent(ctx, serverID, root, path)
	expectEtag := ""
	switch {
	case priorErr == nil:
		expectEtag = prior.Etag
	case errors.Is(priorErr, ErrNotFound):
	default:
		return WriteResult{}, priorErr
	}
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
		if priorErr == nil && (errors.Is(err, ErrConflict) || errors.Is(err, ErrValidation)) {
			s.clearVersion(ctx, serverID, root, path, snapshot.ID)
		}
		return WriteResult{}, err
	}
	return result, nil
}

func (s *Service) saveVersionDetached(ctx context.Context, serverID uuid.UUID, root, path string, prior Content) (Version, error) {
	history := s.historyStore()
	if history == nil {
		return Version{}, ErrHistoryUnavailable
	}
	if prior.Truncated {
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

func versionBytes(version VersionBody) []byte {
	if version.raw != nil {
		return version.raw
	}
	return []byte(version.Content)
}

func (s *Service) recordVersion(ctx context.Context, serverID uuid.UUID, root, path string, prior Content) {
	if _, err := s.saveVersionDetached(ctx, serverID, root, path, prior); err != nil {
		s.logger.Warn("files: could not record file version",
			"server_id", serverID.String(), "root", root, "relpath", path, "error", err)
	}
}

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
