package store

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/justindeelux/gotham/internal/store/sqlc"
)

// ListProxiedApplications returns every application that declares a base
// domain, ordered by creation time so the proxy generator's input and its
// duplicate-domain disposition are deterministic. Each row carries the
// container of the newest running deployment (empty when none is running)
// and the disabled flag set by the domain-uniqueness migration.
func (s *Store) ListProxiedApplications(ctx context.Context) ([]sqlc.ListProxiedApplicationsRow, error) {
	return s.queries.ListProxiedApplications(ctx)
}

// PrepareProxyConfigVersion durably records the intent to make files the
// active configuration of a node, before the node is touched (R2). It is
// transactional: unchanged active content records nothing, a same-hash
// pending row is reused for a promotion retry, and any other pending row is
// replaced by a new one. It reports whether the node content differs from the
// stored active version (changed=false means the active snapshot already
// matches and must not be superseded).
func (s *Store) PrepareProxyConfigVersion(ctx context.Context, serverID pgtype.UUID, files []byte, contentHash string) (sqlc.ProxyConfigVersion, bool, error) {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return sqlc.ProxyConfigVersion{}, false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := s.queries.WithTx(tx)

	active, activeErr := queries.NewestActiveProxyConfigVersion(ctx, serverID)
	if activeErr != nil && !errors.Is(activeErr, pgx.ErrNoRows) {
		return sqlc.ProxyConfigVersion{}, false, activeErr
	}
	hasActive := activeErr == nil

	pending, pendingErr := queries.PendingProxyConfigVersion(ctx, serverID)
	if pendingErr != nil && !errors.Is(pendingErr, pgx.ErrNoRows) {
		return sqlc.ProxyConfigVersion{}, false, pendingErr
	}
	hasPending := pendingErr == nil

	switch {
	case hasActive && active.ContentHash == contentHash && !hasPending:
		// Unchanged: the active snapshot survives and nothing is recorded.
		if err := tx.Commit(ctx); err != nil {
			return sqlc.ProxyConfigVersion{}, false, err
		}
		return active, false, nil
	case hasActive && active.ContentHash == contentHash:
		// Restoration: the node may serve the pending content, so the active
		// row is pushed again and promoted (promotion clears the stale
		// pending row only after the push succeeds).
		if err := tx.Commit(ctx); err != nil {
			return sqlc.ProxyConfigVersion{}, false, err
		}
		return active, true, nil
	case hasPending && pending.ContentHash == contentHash:
		// Promotion retry of an ambiguous earlier push.
		if err := tx.Commit(ctx); err != nil {
			return sqlc.ProxyConfigVersion{}, false, err
		}
		return pending, true, nil
	}

	if err := queries.ClearPendingProxyConfigVersions(ctx, serverID); err != nil {
		return sqlc.ProxyConfigVersion{}, false, err
	}
	row, err := queries.InsertPendingProxyConfigVersion(ctx, sqlc.InsertPendingProxyConfigVersionParams{
		ServerID:    serverID,
		Files:       files,
		ContentHash: contentHash,
	})
	if err != nil {
		return sqlc.ProxyConfigVersion{}, false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return sqlc.ProxyConfigVersion{}, false, err
	}
	return row, true, nil
}

// PromoteProxyConfigVersion activates a prepared version: the pending mark is
// cleared, the version becomes the active one, the previous active version is
// superseded (starting its retention window) and expired predecessors are
// pruned. Any failure rolls the whole promotion back.
func (s *Store) PromoteProxyConfigVersion(ctx context.Context, serverID, versionID pgtype.UUID, before pgtype.Timestamptz) error {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := s.queries.WithTx(tx)

	if err := queries.PromoteProxyConfigVersion(ctx, versionID); err != nil {
		return err
	}
	if err := queries.ClearPendingProxyConfigVersions(ctx, serverID); err != nil {
		return err
	}
	if err := queries.SupersedeActiveProxyConfigVersions(ctx, sqlc.SupersedeActiveProxyConfigVersionsParams{
		ServerID: serverID,
		ID:       versionID,
	}); err != nil {
		return err
	}
	if err := queries.PruneSupersededProxyConfigVersions(ctx, sqlc.PruneSupersededProxyConfigVersionsParams{
		ServerID:     serverID,
		SupersededAt: before,
	}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// AbortProxyConfigVersion drops a pending record after a push that provably
// never touched the node.
func (s *Store) AbortProxyConfigVersion(ctx context.Context, serverID, versionID pgtype.UUID) error {
	return s.queries.DeleteProxyConfigVersion(ctx, sqlc.DeleteProxyConfigVersionParams{
		ID:       versionID,
		ServerID: serverID,
	})
}

// NewestActiveProxyConfigVersion returns the version the node is believed to
// serve.
func (s *Store) NewestActiveProxyConfigVersion(ctx context.Context, serverID pgtype.UUID) (sqlc.ProxyConfigVersion, error) {
	return s.queries.NewestActiveProxyConfigVersion(ctx, serverID)
}

// PendingProxyConfigVersion returns the newest recorded-but-unpromoted
// version.
func (s *Store) PendingProxyConfigVersion(ctx context.Context, serverID pgtype.UUID) (sqlc.ProxyConfigVersion, error) {
	return s.queries.PendingProxyConfigVersion(ctx, serverID)
}

// PreviousProxyConfigVersion returns the newest replaced predecessor.
func (s *Store) PreviousProxyConfigVersion(ctx context.Context, serverID pgtype.UUID) (sqlc.ProxyConfigVersion, error) {
	return s.queries.PreviousProxyConfigVersion(ctx, serverID)
}
