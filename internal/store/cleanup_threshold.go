package store

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/justindeelux/gotham/internal/store/sqlc"
)

// SectionCleanupThreshold labels threshold changes in the settings audit trail.
const SectionCleanupThreshold = "cleanup-threshold"

// CleanupThreshold is the disk-pressure rule of the cleanup center: when a
// node's disk usage stays at or above Percent, Gotham cleans Categories once
// and waits CooldownHours before trying again.
type CleanupThreshold struct {
	Enabled       bool
	Percent       int
	Categories    []string
	MinAgeHours   int
	CooldownHours int
}

func cleanupThresholdFromRow(row sqlc.InstanceSetting) CleanupThreshold {
	return CleanupThreshold{
		Enabled:       row.CleanupThresholdEnabled,
		Percent:       int(row.CleanupThresholdPercent),
		Categories:    append([]string(nil), row.CleanupThresholdCategories...),
		MinAgeHours:   int(row.CleanupThresholdMinAgeHours),
		CooldownHours: int(row.CleanupThresholdCooldownHours),
	}
}

// GetCleanupThreshold reads the threshold rule; a fresh instance holds the
// migration defaults (disabled, 85%, safe categories).
func (s *Store) GetCleanupThreshold(ctx context.Context) (CleanupThreshold, error) {
	row, err := s.queries.GetInstanceSettings(ctx)
	if err != nil {
		return CleanupThreshold{}, err
	}
	return cleanupThresholdFromRow(row), nil
}

// SaveCleanupThreshold writes the rule and its audit entry in one transaction.
func (s *Store) SaveCleanupThreshold(
	ctx context.Context, actor uuid.UUID, threshold CleanupThreshold, changes any,
) error {
	payload, err := json.Marshal(changes)
	if err != nil {
		return fmt.Errorf("store: encode threshold audit: %w", err)
	}
	audit := sqlc.InsertInstanceAuditParams{Section: SectionCleanupThreshold, Changes: payload}
	if actor != uuid.Nil {
		audit.ActorID = pgtype.UUID{Bytes: actor, Valid: true}
	}
	categories := threshold.Categories
	if categories == nil {
		categories = []string{}
	}
	return s.InTx(ctx, func(tx *Store) error {
		if _, err := tx.queries.UpdateInstanceThreshold(ctx, sqlc.UpdateInstanceThresholdParams{
			CleanupThresholdEnabled:       threshold.Enabled,
			CleanupThresholdPercent:       int32(threshold.Percent),
			CleanupThresholdCategories:    categories,
			CleanupThresholdMinAgeHours:   int32(threshold.MinAgeHours),
			CleanupThresholdCooldownHours: int32(threshold.CooldownHours),
		}); err != nil {
			return err
		}
		return tx.queries.InsertInstanceAudit(ctx, audit)
	})
}

// LatestCleanupThresholdRun returns the newest threshold run of one node, so a
// restart keeps honouring the cool-down stored in history.
func (s *Store) LatestCleanupThresholdRun(ctx context.Context, serverID uuid.UUID) (CleanupRun, error) {
	row, err := s.queries.GetLatestCleanupThresholdRun(ctx, uuidToPG(serverID))
	if err != nil {
		return CleanupRun{}, err
	}
	return cleanupRunFromRow(row), nil
}
