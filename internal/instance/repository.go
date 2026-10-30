package instance

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/justindeelux/gotham/internal/store"
	"github.com/justindeelux/gotham/internal/store/sqlc"
)

const SectionGeneral = "general"

// SectionCleanup labels the retention changes in the settings audit trail.
const SectionCleanup = "cleanup"

// NodeSectionSystem and NodeSectionNetwork label the per-node host changes in
// the settings audit trail.
const (
	NodeSectionSystem  = "node-system"
	NodeSectionNetwork = "node-network"
)

type Repository interface {
	Load(ctx context.Context) (Stored, error)
	Save(ctx context.Context, actor uuid.UUID, section string, rec Stored, changes any) error
	SaveCleanup(ctx context.Context, actor uuid.UUID, rec Stored, changes any) error
}

type storeRepository struct{ store *store.Store }

var _ Repository = (*storeRepository)(nil)

func NewStoreRepository(st *store.Store) Repository { return &storeRepository{store: st} }

func (r *storeRepository) Load(ctx context.Context) (Stored, error) {
	row, err := r.store.GetInstanceSettings(ctx)
	if err != nil {
		return Stored{}, fmt.Errorf("instance: load: %w", err)
	}
	rec := Stored{
		ControlPlaneURL: row.ControlPlaneUrl,
		InstanceName:    row.InstanceName,
		Timezone:        row.Timezone,
		Retention:       retentionFrom(row.CleanupHistoryRetentionDays, row.CleanupBackupFileMinAgeDays),
	}
	if len(row.PendingControlPlaneUrl) > 0 {
		var pending PendingControlPlaneURL
		if err := json.Unmarshal(row.PendingControlPlaneUrl, &pending); err != nil {
			return Stored{}, fmt.Errorf("instance: decode pending control-plane URL: %w", err)
		}
		rec.PendingControlPlaneURL = &pending
	}
	return rec, nil
}

func (r *storeRepository) Save(ctx context.Context, actor uuid.UUID, section string, rec Stored, changes any) error {
	if section != SectionGeneral {
		return fmt.Errorf("instance: unknown section %q", section)
	}
	audit, err := r.audit(actor, section, changes)
	if err != nil {
		return err
	}
	var pending []byte
	if rec.PendingControlPlaneURL != nil {
		pending, err = json.Marshal(rec.PendingControlPlaneURL)
		if err != nil {
			return fmt.Errorf("instance: encode pending control-plane URL: %w", err)
		}
	}
	err = r.store.UpdateInstanceGeneral(ctx, sqlc.UpdateInstanceGeneralParams{
		ControlPlaneUrl: rec.ControlPlaneURL, InstanceName: rec.InstanceName, Timezone: rec.Timezone, PendingControlPlaneUrl: pending,
	}, audit)
	if err != nil {
		return fmt.Errorf("instance: save %s: %w", section, err)
	}
	return nil
}

func (r *storeRepository) SaveCleanup(ctx context.Context, actor uuid.UUID, rec Stored, changes any) error {
	audit, err := r.audit(actor, SectionCleanup, changes)
	if err != nil {
		return err
	}
	err = r.store.UpdateInstanceCleanup(ctx, sqlc.UpdateInstanceCleanupParams{
		CleanupHistoryRetentionDays: int32(rec.Retention.HistoryDays),
		CleanupBackupFileMinAgeDays: int32(rec.Retention.BackupFileMinAgeDays),
	}, audit)
	if err != nil {
		return fmt.Errorf("instance: save cleanup: %w", err)
	}
	return nil
}

func (r *storeRepository) audit(actor uuid.UUID, section string, changes any) (sqlc.InsertInstanceAuditParams, error) {
	payload, err := json.Marshal(changes)
	if err != nil {
		return sqlc.InsertInstanceAuditParams{}, fmt.Errorf("instance: encode audit: %w", err)
	}
	audit := sqlc.InsertInstanceAuditParams{Section: section, Changes: payload}
	if actor != uuid.Nil {
		audit.ActorID = pgtype.UUID{Bytes: actor, Valid: true}
	}
	return audit, nil
}

func nonNil(in []string) []string {
	if in == nil {
		return []string{}
	}
	return in
}
