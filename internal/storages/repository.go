package storages

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/justindeelux/gotham/internal/store"
	"github.com/justindeelux/gotham/internal/store/sqlc"
)

// Repository is the persistence surface the storage domain needs. The rows
// still live in backup_targets/backup_target_secrets so existing backups,
// schedules and restores keep working without a migration.
type Repository interface {
	EnsureBuiltinTarget(ctx context.Context, userID uuid.UUID) (Storage, error)
	GetBuiltinTarget(ctx context.Context, userID uuid.UUID) (Storage, error)
	GetBackupTarget(ctx context.Context, id uuid.UUID) (Storage, error)
	ListBackupTargetsByUser(ctx context.Context, userID uuid.UUID) ([]Storage, error)
	CreateBackupTargetWithSecrets(ctx context.Context, target Storage, secrets []Secret) (Storage, error)
	UpdateBackupTargetWithSecrets(ctx context.Context, target Storage, secrets []Secret) (Storage, error)
	DeleteBackupTarget(ctx context.Context, id, userID uuid.UUID) (Storage, error)
	CountBackupsForTarget(ctx context.Context, id uuid.UUID) (int64, error)
	ListBackupScheduleIDsForTarget(ctx context.Context, id uuid.UUID) ([]uuid.UUID, error)
	MarkBackupsProviderRemoved(ctx context.Context, id uuid.UUID) error
	BackupUsageByTarget(ctx context.Context, userID uuid.UUID) (map[uuid.UUID]Usage, error)
	ListTargetSecrets(ctx context.Context, id uuid.UUID) ([]Secret, error)
}

type storeRepository struct {
	store *store.Store
}

var _ Repository = (*storeRepository)(nil)

// NewStoreRepository adapts the shared persistence layer.
func NewStoreRepository(st *store.Store) Repository {
	return &storeRepository{store: st}
}

func (r *storeRepository) EnsureBuiltinTarget(ctx context.Context, userID uuid.UUID) (Storage, error) {
	// Resolve an existing builtin first: the seeding is keyed on the flag, not
	// on the name, so a renamed or differently named row can never grow a
	// second "Local disk" storage.
	if existing, err := r.GetBuiltinTarget(ctx, userID); err == nil {
		return existing, nil
	} else if !errors.Is(err, ErrNotFound) {
		return Storage{}, err
	}
	row, err := r.store.EnsureBuiltinBackupTarget(ctx, pgUUID(userID))
	if err != nil {
		// A user-created storage already holds the name; the account still has
		// no builtin, so surface the cause instead of inserting a duplicate.
		return Storage{}, fmt.Errorf("storages: create the built-in storage: %w", err)
	}
	return storageFromRow(row), nil
}

func (r *storeRepository) GetBuiltinTarget(ctx context.Context, userID uuid.UUID) (Storage, error) {
	row, err := r.store.GetBuiltinBackupTarget(ctx, pgUUID(userID))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Storage{}, ErrNotFound
		}
		return Storage{}, fmt.Errorf("storages: get the built-in storage: %w", err)
	}
	return storageFromRow(row), nil
}

func (r *storeRepository) GetBackupTarget(ctx context.Context, id uuid.UUID) (Storage, error) {
	row, err := r.store.GetBackupTarget(ctx, pgUUID(id))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Storage{}, ErrNotFound
		}
		return Storage{}, fmt.Errorf("storages: get storage: %w", err)
	}
	return storageFromRow(row), nil
}

func (r *storeRepository) ListBackupTargetsByUser(ctx context.Context, userID uuid.UUID) ([]Storage, error) {
	rows, err := r.store.ListBackupTargetsByUser(ctx, pgUUID(userID))
	if err != nil {
		return nil, fmt.Errorf("storages: list storages: %w", err)
	}
	targets := make([]Storage, 0, len(rows))
	for _, row := range rows {
		targets = append(targets, storageFromRow(row))
	}
	return targets, nil
}

func (r *storeRepository) CreateBackupTargetWithSecrets(ctx context.Context, target Storage, secrets []Secret) (Storage, error) {
	row, err := r.store.CreateBackupTargetWithSecrets(ctx, createTargetParams(target), secretParams(secrets))
	if err != nil {
		if isUniqueViolation(err) {
			return Storage{}, ErrConflict
		}
		return Storage{}, fmt.Errorf("storages: create storage: %w", err)
	}
	return storageFromRow(row), nil
}

func (r *storeRepository) UpdateBackupTargetWithSecrets(ctx context.Context, target Storage, secrets []Secret) (Storage, error) {
	row, stranded, err := r.store.UpdateBackupTargetWithSecrets(ctx, updateTargetParams(target), secretParams(secrets))
	if err != nil {
		if isUniqueViolation(err) {
			return Storage{}, ErrConflict
		}
		if errors.Is(err, pgx.ErrNoRows) {
			return Storage{}, ErrNotFound
		}
		return Storage{}, fmt.Errorf("storages: update storage: %w", err)
	}
	if stranded {
		return Storage{}, ErrStranded
	}
	return storageFromRow(row), nil
}

func (r *storeRepository) DeleteBackupTarget(ctx context.Context, id, userID uuid.UUID) (Storage, error) {
	if err := r.store.MarkBackupSchedulesTargetChanged(ctx, pgUUID(id)); err != nil {
		return Storage{}, fmt.Errorf("storages: record storage change: %w", err)
	}
	row, err := r.store.DeleteBackupTarget(ctx, sqlc.DeleteBackupTargetParams{
		ID:     pgUUID(id),
		UserID: pgUUID(userID),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Storage{}, ErrNotFound
		}
		return Storage{}, fmt.Errorf("storages: delete storage: %w", err)
	}
	return storageFromRow(row), nil
}

func (r *storeRepository) CountBackupsForTarget(ctx context.Context, id uuid.UUID) (int64, error) {
	count, err := r.store.CountBackupsForTarget(ctx, pgUUID(id))
	if err != nil {
		return 0, fmt.Errorf("storages: count storage backups: %w", err)
	}
	return count, nil
}

func (r *storeRepository) ListBackupScheduleIDsForTarget(ctx context.Context, id uuid.UUID) ([]uuid.UUID, error) {
	rows, err := r.store.ListBackupScheduleIDsForTarget(ctx, pgUUID(id))
	if err != nil {
		return nil, fmt.Errorf("storages: list schedules using the storage: %w", err)
	}
	ids := make([]uuid.UUID, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, uuidFromPG(row))
	}
	return ids, nil
}

func (r *storeRepository) MarkBackupsProviderRemoved(ctx context.Context, id uuid.UUID) error {
	if err := r.store.MarkBackupsProviderRemoved(ctx, pgUUID(id)); err != nil {
		return fmt.Errorf("storages: mark backups without storage: %w", err)
	}
	return nil
}

func (r *storeRepository) BackupUsageByTarget(ctx context.Context, userID uuid.UUID) (map[uuid.UUID]Usage, error) {
	rows, err := r.store.BackupUsageByTarget(ctx, pgUUID(userID))
	if err != nil {
		return nil, fmt.Errorf("storages: storage usage: %w", err)
	}
	usage := make(map[uuid.UUID]Usage, len(rows))
	for _, row := range rows {
		usage[uuidFromPG(row.TargetID)] = Usage{
			BackupCount: row.BackupCount,
			TotalSize:   row.TotalSize,
		}
	}
	return usage, nil
}

func (r *storeRepository) ListTargetSecrets(ctx context.Context, id uuid.UUID) ([]Secret, error) {
	rows, err := r.store.ListBackupTargetSecrets(ctx, pgUUID(id))
	if err != nil {
		return nil, fmt.Errorf("storages: list storage secrets: %w", err)
	}
	secrets := make([]Secret, 0, len(rows))
	for _, row := range rows {
		secrets = append(secrets, Secret{
			ID:         uuidFromPG(row.ID),
			TargetID:   uuidFromPG(row.TargetID),
			Key:        row.Key,
			Ciphertext: row.Ciphertext,
			CreatedAt:  timeFromPG(row.CreatedAt),
		})
	}
	return secrets, nil
}

func createTargetParams(target Storage) sqlc.CreateBackupTargetParams {
	return sqlc.CreateBackupTargetParams{
		ID:       pgUUID(target.ID),
		UserID:   pgUUID(target.UserID),
		Name:     target.Name,
		Kind:     string(target.Kind),
		Endpoint: target.Endpoint,
		Region:   target.Region,
		Bucket:   target.Bucket,
		Prefix:   target.Prefix,
	}
}

func updateTargetParams(target Storage) sqlc.UpdateBackupTargetParams {
	return sqlc.UpdateBackupTargetParams{
		ID:       pgUUID(target.ID),
		Name:     target.Name,
		Kind:     string(target.Kind),
		Endpoint: target.Endpoint,
		Region:   target.Region,
		Bucket:   target.Bucket,
		Prefix:   target.Prefix,
	}
}

func secretParams(secrets []Secret) []sqlc.UpsertBackupTargetSecretParams {
	params := make([]sqlc.UpsertBackupTargetSecretParams, 0, len(secrets))
	for _, secret := range secrets {
		params = append(params, sqlc.UpsertBackupTargetSecretParams{
			TargetID:   pgUUID(secret.TargetID),
			Key:        secret.Key,
			Ciphertext: secret.Ciphertext,
		})
	}
	return params
}

func storageFromRow(row sqlc.BackupTarget) Storage {
	return Storage{
		ID:        uuidFromPG(row.ID),
		UserID:    uuidFromPG(row.UserID),
		Name:      row.Name,
		Kind:      Kind(row.Kind),
		Endpoint:  row.Endpoint,
		Region:    row.Region,
		Bucket:    row.Bucket,
		Prefix:    row.Prefix,
		Builtin:   row.Builtin,
		CreatedAt: timeFromPG(row.CreatedAt),
		UpdatedAt: timeFromPG(row.UpdatedAt),
	}
}

func pgUUID(id uuid.UUID) pgtype.UUID {
	return pgtype.UUID{Bytes: id, Valid: id != uuid.Nil}
}

func uuidFromPG(v pgtype.UUID) uuid.UUID {
	if !v.Valid {
		return uuid.Nil
	}
	return uuid.UUID(v.Bytes)
}

func timeFromPG(v pgtype.Timestamptz) time.Time {
	if !v.Valid {
		return time.Time{}
	}
	return v.Time
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}
