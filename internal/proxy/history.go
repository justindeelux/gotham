package proxy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/justindeelux/gotham/internal/store"
	"github.com/justindeelux/gotham/internal/store/sqlc"
)

// historyRetention is how long a replaced configuration stays available for
// fast revert. It is measured from the replacement (superseded_at), never
// from the original push (R2).
const historyRetention = 24 * time.Hour

// ConfigVersion is one stored configuration snapshot.
type ConfigVersion struct {
	// ID identifies the stored row (promotion/abort target).
	ID uuid.UUID
	// Files are the documents of the snapshot.
	Files []File
	// ContentHash fingerprints the files.
	ContentHash string
	// Pending marks a version recorded but not yet promoted (the node may or
	// may not already serve it after an ambiguous push).
	Pending bool
	// SupersededAt is when the version was replaced; zero while active.
	SupersededAt time.Time
}

// HistoryStore sequences configuration versions so that the active snapshot
// survives unchanged syncs and its predecessor stays revertable for the
// retention window after replacement. The production implementation is
// *store.Store (through storeHistory); nil disables history and revert.
type HistoryStore interface {
	// PrepareConfigVersion durably records files as the configuration about
	// to be pushed and returns the version to promote. changed=false means
	// the stored active configuration already matches; changed=true means the
	// caller must promote the returned version after a successful push. Any
	// error leaves the node untouched and must fail the sync.
	PrepareConfigVersion(ctx context.Context, serverID uuid.UUID, files []File, contentHash string) (ConfigVersion, bool, error)
	// PromoteConfigVersion activates a prepared version after a confirmed
	// push: pending records are cleared, the version becomes active, the
	// previous active version is superseded and expired predecessors pruned.
	PromoteConfigVersion(ctx context.Context, serverID, versionID uuid.UUID) error
	// AbortConfigVersion drops a pending record. The sync never calls it: every
	// failed push retains the pending record (R2), and only a successful
	// promotion or the restoration path clears it. It remains available for
	// explicit operator cleanup.
	AbortConfigVersion(ctx context.Context, serverID, versionID uuid.UUID) error
	// PreviousConfigVersion returns the configuration to revert to. While a
	// pending push exists the node may already serve it, so the actual prior
	// is the active version, never an older superseded one (R2).
	PreviousConfigVersion(ctx context.Context, serverID uuid.UUID) (ConfigVersion, error)
}

// storeHistory adapts the shared store to the history seam.
type storeHistory struct {
	store *store.Store
}

// PrepareConfigVersion maps the store's transactional preparation.
func (h storeHistory) PrepareConfigVersion(ctx context.Context, serverID uuid.UUID, files []File, contentHash string) (ConfigVersion, bool, error) {
	payload, err := json.Marshal(files)
	if err != nil {
		return ConfigVersion{}, false, fmt.Errorf("encode proxy config: %w", err)
	}
	row, changed, err := h.store.PrepareProxyConfigVersion(ctx, pgUUID(serverID), payload, contentHash)
	if err != nil {
		return ConfigVersion{}, false, err
	}
	version, err := configVersionFromRow(row)
	if err != nil {
		return ConfigVersion{}, false, err
	}
	return version, changed, nil
}

// PromoteConfigVersion maps the store's transactional promotion.
func (h storeHistory) PromoteConfigVersion(ctx context.Context, serverID, versionID uuid.UUID) error {
	return h.store.PromoteProxyConfigVersion(ctx, pgUUID(serverID), pgUUID(versionID),
		pgtype.Timestamptz{Time: time.Now().Add(-historyRetention), Valid: true})
}

// AbortConfigVersion drops a pending record.
func (h storeHistory) AbortConfigVersion(ctx context.Context, serverID, versionID uuid.UUID) error {
	return h.store.AbortProxyConfigVersion(ctx, pgUUID(serverID), pgUUID(versionID))
}

// PreviousConfigVersion resolves the revert target: a pending push means the
// node may already serve it, so the active version is the actual prior;
// otherwise the newest superseded predecessor, then the active version
// (idempotent re-push) and only then ErrVersionNotFound.
func (h storeHistory) PreviousConfigVersion(ctx context.Context, serverID uuid.UUID) (ConfigVersion, error) {
	pending, err := h.store.PendingProxyConfigVersion(ctx, pgUUID(serverID))
	switch {
	case err == nil && pending.Pending:
		active, activeErr := h.store.NewestActiveProxyConfigVersion(ctx, pgUUID(serverID))
		if activeErr == nil {
			return configVersionFromRow(active)
		}
		if !errors.Is(activeErr, pgx.ErrNoRows) {
			return ConfigVersion{}, activeErr
		}
	case err != nil && !errors.Is(err, pgx.ErrNoRows):
		return ConfigVersion{}, err
	}

	previous, err := h.store.PreviousProxyConfigVersion(ctx, pgUUID(serverID))
	if err == nil {
		return configVersionFromRow(previous)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return ConfigVersion{}, err
	}
	active, err := h.store.NewestActiveProxyConfigVersion(ctx, pgUUID(serverID))
	if err == nil {
		return configVersionFromRow(active)
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return ConfigVersion{}, ErrVersionNotFound
	}
	return ConfigVersion{}, err
}

// configVersionFromRow decodes a stored version.
func configVersionFromRow(row sqlc.ProxyConfigVersion) (ConfigVersion, error) {
	var files []File
	if err := json.Unmarshal(row.Files, &files); err != nil {
		return ConfigVersion{}, fmt.Errorf("decode stored proxy config: %w", err)
	}
	version := ConfigVersion{
		ID:          uuidFromPG(row.ID),
		Files:       files,
		ContentHash: row.ContentHash,
		Pending:     row.Pending,
	}
	if row.SupersededAt.Valid {
		version.SupersededAt = row.SupersededAt.Time
	}
	return version, nil
}

// errHistoryNotConfigured guards the optional history seam.
var errHistoryNotConfigured = errors.New("proxy: configuration history is not configured")
