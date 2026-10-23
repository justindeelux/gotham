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

const historyRetention = 24 * time.Hour

type ConfigVersion struct {
	ID           uuid.UUID
	Files        []File
	ContentHash  string
	Pending      bool
	SupersededAt time.Time
}

type HistoryStore interface {
	PrepareConfigVersion(ctx context.Context, serverID uuid.UUID, files []File, contentHash string) (ConfigVersion, bool, error)
	PromoteConfigVersion(ctx context.Context, serverID, versionID uuid.UUID) error
	AbortConfigVersion(ctx context.Context, serverID, versionID uuid.UUID) error
	PreviousConfigVersion(ctx context.Context, serverID uuid.UUID) (ConfigVersion, error)
	ActiveConfigVersion(ctx context.Context, serverID uuid.UUID) (ConfigVersion, error)
}

type storeHistory struct {
	store *store.Store
}

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

func (h storeHistory) PromoteConfigVersion(ctx context.Context, serverID, versionID uuid.UUID) error {
	return h.store.PromoteProxyConfigVersion(ctx, pgUUID(serverID), pgUUID(versionID),
		pgtype.Timestamptz{Time: time.Now().Add(-historyRetention), Valid: true})
}

func (h storeHistory) AbortConfigVersion(ctx context.Context, serverID, versionID uuid.UUID) error {
	return h.store.AbortProxyConfigVersion(ctx, pgUUID(serverID), pgUUID(versionID))
}

func (h storeHistory) ActiveConfigVersion(ctx context.Context, serverID uuid.UUID) (ConfigVersion, error) {
	pending, err := h.store.PendingProxyConfigVersion(ctx, pgUUID(serverID))
	if err == nil && pending.Pending {
		return configVersionFromRow(pending)
	}
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return ConfigVersion{}, err
	}
	active, err := h.store.NewestActiveProxyConfigVersion(ctx, pgUUID(serverID))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ConfigVersion{}, ErrVersionNotFound
		}
		return ConfigVersion{}, err
	}
	return configVersionFromRow(active)
}

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

var errHistoryNotConfigured = errors.New("proxy: configuration history is not configured")
