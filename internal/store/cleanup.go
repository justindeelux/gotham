package store

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/justindeelux/gotham/internal/store/sqlc"
)

// CleanupRun is one completed cleanup attempt on a node.
type CleanupRun struct {
	ID             uuid.UUID
	ServerID       uuid.UUID
	ActorUserID    *uuid.UUID
	Trigger        string
	StartedAt      time.Time
	FinishedAt     time.Time
	ReclaimedBytes int64
}

// CleanupRunItem is one requested object's outcome within a run.
type CleanupRunItem struct {
	ItemID         string
	Category       string
	Name           string
	Status         string
	Message        string
	ReclaimedBytes int64
}

// CountActiveDeploymentsByServer reports how many of a node's applications are
// mid-deployment.
func (s *Store) CountActiveDeploymentsByServer(ctx context.Context, serverID uuid.UUID) (int64, error) {
	return s.queries.CountActiveDeploymentsByServer(ctx, uuidToPG(serverID))
}

// RecordCleanupRun writes a run and its items in one transaction, so history
// never shows a run without the outcomes it recorded.
func (s *Store) RecordCleanupRun(ctx context.Context, run CleanupRun, items []CleanupRunItem) error {
	return s.InTx(ctx, func(tx *Store) error {
		actor := pgtype.UUID{}
		if run.ActorUserID != nil {
			actor = uuidToPG(*run.ActorUserID)
		}
		if err := tx.queries.InsertCleanupRun(ctx, sqlc.InsertCleanupRunParams{
			ID:             uuidToPG(run.ID),
			ServerID:       uuidToPG(run.ServerID),
			ActorUserID:    actor,
			Trigger:        run.Trigger,
			StartedAt:      pgtype.Timestamptz{Time: run.StartedAt, Valid: true},
			FinishedAt:     pgtype.Timestamptz{Time: run.FinishedAt, Valid: true},
			ReclaimedBytes: run.ReclaimedBytes,
		}); err != nil {
			return err
		}
		for _, item := range items {
			if err := tx.queries.InsertCleanupRunItem(ctx, sqlc.InsertCleanupRunItemParams{
				RunID:          uuidToPG(run.ID),
				ItemID:         item.ItemID,
				Category:       item.Category,
				Name:           item.Name,
				Status:         item.Status,
				Message:        item.Message,
				ReclaimedBytes: item.ReclaimedBytes,
			}); err != nil {
				return err
			}
		}
		return nil
	})
}

func uuidToPG(id uuid.UUID) pgtype.UUID {
	return pgtype.UUID{Bytes: id, Valid: true}
}
