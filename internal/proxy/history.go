package proxy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/justindeelux/gotham/internal/store"
)

// historyRetention is how long a pushed configuration version stays available
// for fast revert (the phase rollback plan pins it at one day).
const historyRetention = 24 * time.Hour

// ConfigVersion is one successfully pushed configuration snapshot.
type ConfigVersion struct {
	// Files are the documents that were written to the node.
	Files []File
	// ContentHash identifies the snapshot (used to skip duplicate writes).
	ContentHash string
	// CreatedAt is when the snapshot was pushed.
	CreatedAt time.Time
}

// HistoryStore persists pushed configuration versions for fast revert. The
// production implementation is *store.Store (through storeHistory); nil
// disables history and revert.
type HistoryStore interface {
	// LatestConfigVersions returns the newest versions, newest first.
	LatestConfigVersions(ctx context.Context, serverID uuid.UUID, limit int32) ([]ConfigVersion, error)
	// RecordConfigVersion stores one pushed version and prunes expired ones.
	RecordConfigVersion(ctx context.Context, serverID uuid.UUID, files []File, contentHash string) error
}

// storeHistory adapts the shared store to the history seam.
type storeHistory struct {
	store *store.Store
}

// LatestConfigVersions reads the newest stored versions for a node.
func (h storeHistory) LatestConfigVersions(ctx context.Context, serverID uuid.UUID, limit int32) ([]ConfigVersion, error) {
	rows, err := h.store.LatestProxyConfigVersions(ctx, pgUUID(serverID), limit)
	if err != nil {
		return nil, err
	}
	versions := make([]ConfigVersion, 0, len(rows))
	for _, row := range rows {
		var files []File
		if err := json.Unmarshal(row.Files, &files); err != nil {
			return nil, fmt.Errorf("decode stored proxy config: %w", err)
		}
		versions = append(versions, ConfigVersion{
			Files:       files,
			ContentHash: row.ContentHash,
			CreatedAt:   row.CreatedAt.Time,
		})
	}
	return versions, nil
}

// RecordConfigVersion stores one pushed version and prunes versions older
// than the retention window.
func (h storeHistory) RecordConfigVersion(ctx context.Context, serverID uuid.UUID, files []File, contentHash string) error {
	payload, err := json.Marshal(files)
	if err != nil {
		return fmt.Errorf("encode proxy config: %w", err)
	}
	if _, err := h.store.InsertProxyConfigVersion(ctx, pgUUID(serverID), payload, contentHash); err != nil {
		return err
	}
	return h.store.PruneProxyConfigVersions(ctx, pgUUID(serverID),
		pgtype.Timestamptz{Time: time.Now().Add(-historyRetention), Valid: true})
}

// errHistoryNotConfigured guards the optional history seam.
var errHistoryNotConfigured = errors.New("proxy: configuration history is not configured")
