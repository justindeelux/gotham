package instance

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/justindeelux/gotham/internal/store"
	"github.com/justindeelux/gotham/internal/store/sqlc"
)

// NodeRepository keeps the only per-node state the control plane stores: the
// unconfirmed network change. Everything else is read from the host itself.
type NodeRepository interface {
	Pending(ctx context.Context, serverID uuid.UUID) (*Pending, error)
	// BeginPending reports false when the node already has a pending change.
	BeginPending(ctx context.Context, serverID uuid.UUID, pending Pending) (bool, error)
	ClearPending(ctx context.Context, serverID uuid.UUID) error
	Audit(ctx context.Context, actor uuid.UUID, section string, changes any) error
}

type storeNodeRepository struct{ store *store.Store }

var _ NodeRepository = (*storeNodeRepository)(nil)

func NewStoreNodeRepository(st *store.Store) NodeRepository { return &storeNodeRepository{store: st} }

func (r *storeNodeRepository) Pending(ctx context.Context, serverID uuid.UUID) (*Pending, error) {
	row, err := r.store.GetServerHostPending(ctx, serverID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("instance: load pending network change: %w", err)
	}
	var pending Pending
	if err := json.Unmarshal(row.Pending, &pending); err != nil {
		return nil, fmt.Errorf("instance: decode pending network change: %w", err)
	}
	if row.Deadline.Valid {
		pending.Deadline = row.Deadline.Time
	}
	return &pending, nil
}

func (r *storeNodeRepository) BeginPending(ctx context.Context, serverID uuid.UUID, pending Pending) (bool, error) {
	payload, err := json.Marshal(pending)
	if err != nil {
		return false, fmt.Errorf("instance: encode pending network change: %w", err)
	}
	ok, err := r.store.BeginServerHostPending(ctx, serverID, payload, pgtype.Timestamptz{Time: pending.Deadline, Valid: true})
	if err != nil {
		return false, fmt.Errorf("instance: save pending network change: %w", err)
	}
	return ok, nil
}

func (r *storeNodeRepository) ClearPending(ctx context.Context, serverID uuid.UUID) error {
	if err := r.store.DeleteServerHostPending(ctx, serverID); err != nil {
		return fmt.Errorf("instance: clear pending network change: %w", err)
	}
	return nil
}

func (r *storeNodeRepository) Audit(ctx context.Context, actor uuid.UUID, section string, changes any) error {
	payload, err := json.Marshal(changes)
	if err != nil {
		return fmt.Errorf("instance: encode audit: %w", err)
	}
	audit := sqlc.InsertInstanceAuditParams{Section: section, Changes: payload}
	if actor != uuid.Nil {
		audit.ActorID = pgtype.UUID{Bytes: actor, Valid: true}
	}
	return r.store.InsertInstanceSettingsAudit(ctx, audit)
}
