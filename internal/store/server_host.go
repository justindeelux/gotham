package store

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/justindeelux/gotham/internal/store/sqlc"
)

func (s *Store) GetServerHostPending(ctx context.Context, serverID uuid.UUID) (sqlc.ServerHostPending, error) {
	return s.queries.GetServerHostPending(ctx, pgtype.UUID{Bytes: serverID, Valid: true})
}

// BeginServerHostPending stores the pending change unless the node already has
// one, in which case it reports false and leaves the existing record alone.
func (s *Store) BeginServerHostPending(ctx context.Context, serverID uuid.UUID, pending []byte, deadline pgtype.Timestamptz) (bool, error) {
	rows, err := s.queries.BeginServerHostPending(ctx, sqlc.BeginServerHostPendingParams{
		ServerID: pgtype.UUID{Bytes: serverID, Valid: true},
		Pending:  pending,
		Deadline: deadline,
	})
	if err != nil {
		return false, err
	}
	return rows > 0, nil
}

func (s *Store) DeleteServerHostPending(ctx context.Context, serverID uuid.UUID) error {
	return s.queries.DeleteServerHostPending(ctx, pgtype.UUID{Bytes: serverID, Valid: true})
}

func (s *Store) InsertInstanceSettingsAudit(ctx context.Context, params sqlc.InsertInstanceAuditParams) error {
	if err := s.queries.InsertInstanceAudit(ctx, params); err != nil {
		return fmt.Errorf("store: insert settings audit: %w", err)
	}
	return nil
}
