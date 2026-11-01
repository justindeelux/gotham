package backups

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/storages"
)

// Storage destinations are owned by internal/storages. The provider methods
// below stay on the backup service so the deprecated /backups/providers alias
// keeps working; they are a thin delegation, not a second implementation.

func (m *BackupManager) ListProviders(ctx context.Context, userID uuid.UUID) ([]BackupTarget, error) {
	if m == nil || m.storages == nil {
		return nil, storages.ErrNotReady
	}
	return m.storages.List(ctx, userID)
}

func (m *BackupManager) CreateProvider(ctx context.Context, userID uuid.UUID, req ProviderRequest) (BackupTarget, error) {
	if m == nil || m.storages == nil {
		return BackupTarget{}, storages.ErrNotReady
	}
	return m.storages.Create(ctx, userID, req)
}

func (m *BackupManager) UpdateProvider(ctx context.Context, userID, providerID uuid.UUID, req ProviderRequest) (BackupTarget, error) {
	if m == nil || m.storages == nil {
		return BackupTarget{}, storages.ErrNotReady
	}
	return m.storages.Update(ctx, userID, providerID, req)
}

func (m *BackupManager) DeleteProvider(ctx context.Context, userID, providerID uuid.UUID, confirm bool) error {
	if m == nil || m.storages == nil {
		return storages.ErrNotReady
	}
	return m.storages.Delete(ctx, userID, providerID, confirm)
}

func (m *BackupManager) TestProvider(ctx context.Context, userID, providerID uuid.UUID) (TargetCheck, error) {
	if m == nil || m.storages == nil {
		return TargetCheck{}, storages.ErrNotReady
	}
	return m.storages.Test(ctx, userID, providerID)
}

func (m *BackupManager) storeAndKey(
	ctx context.Context,
	target *BackupTarget,
	resource Resource,
	runID uuid.UUID,
	extension string,
) (ObjectStore, string, error) {
	extension = strings.TrimSpace(extension)
	if extension == "" {
		return nil, "", fmt.Errorf("%w: the backup driver reported no artifact extension", ErrValidation)
	}
	// Volume captures already report "tar.gz"; suffixing it again produces
	// "<run>.tar.gz.gz". The core gzips every capture, so the stored bytes stay
	// gzip over that archive and the location keeps the driver's extension.
	suffix := ".gz"
	if strings.HasSuffix(extension, ".gz") {
		suffix = ""
	}
	key := fmt.Sprintf("%ss/%s/%s.%s%s", resource.Type, resource.ID, runID, extension, suffix)
	if m.objects != nil {
		return m.objects, key, nil
	}
	store, err := m.storeForTarget(ctx, target)
	if err != nil {
		return nil, "", err
	}
	return store, key, nil
}

// defaultTarget is the resource owner's built-in Local disk storage. Every
// account gets one, including a member who created a resource, so those runs
// are labelled the same way as the owner's. If seeding is impossible the run
// still falls back to the local store instead of failing.
func (m *BackupManager) defaultTarget(ctx context.Context, ownerID uuid.UUID) (*BackupTarget, error) {
	if ownerID == uuid.Nil || m.storages == nil {
		return nil, nil
	}
	target, err := m.storages.Builtin(ctx, ownerID)
	if err != nil {
		return nil, err
	}
	if target != nil {
		return target, nil
	}
	seeded, err := m.storages.EnsureBuiltin(ctx, ownerID)
	if err != nil {
		m.logger.Warn("backups: could not seed the built-in storage",
			"user_id", ownerID.String(), "error", err)
		return nil, nil
	}
	return &seeded, nil
}

func (m *BackupManager) storeForTarget(ctx context.Context, target *BackupTarget) (ObjectStore, error) {
	if m.objects != nil {
		return m.objects, nil
	}
	if m.storages == nil {
		return nil, storages.ErrNotReady
	}
	storage := BackupTarget{Kind: TargetLocal}
	if target != nil {
		storage = *target
	}
	return m.storages.Open(ctx, storage)
}

func (m *BackupManager) storeForLocation(ctx context.Context, backup Backup, ownerID uuid.UUID) (ObjectStore, error) {
	if m.objects != nil {
		return m.objects, nil
	}
	switch {
	case strings.HasPrefix(backup.Location, locationFilePrefix):
		return newLocalStore(m.localDir)
	case strings.HasPrefix(backup.Location, locationS3Prefix):
		var target *BackupTarget
		var err error
		if ownerID == uuid.Nil {
			target, err = m.storages.ResolveAny(ctx, backup.TargetID)
		} else {
			target, err = m.storages.Resolve(ctx, ownerID, backup.TargetID)
		}
		if err != nil {
			return nil, err
		}
		if target == nil {
			return nil, fmt.Errorf("%w: the storage of this backup no longer exists", ErrValidation)
		}
		return m.storeForTarget(ctx, target)
	default:
		return nil, fmt.Errorf("%w: unknown backup location %q", ErrValidation, backup.Location)
	}
}

func (m *BackupManager) ownedTarget(ctx context.Context, userID, targetID uuid.UUID) (*BackupTarget, error) {
	if m.storages == nil {
		return nil, storages.ErrNotReady
	}
	return m.storages.Resolve(ctx, userID, targetID)
}

func (m *BackupManager) ownedTargetID(ctx context.Context, userID uuid.UUID, raw string) (uuid.UUID, error) {
	if strings.TrimSpace(raw) == "" {
		return uuid.Nil, nil
	}
	targetID, err := uuid.Parse(raw)
	if err != nil {
		return uuid.Nil, fmt.Errorf("%w: invalid provider id", ErrValidation)
	}
	if _, err := m.ownedTarget(ctx, userID, targetID); err != nil {
		return uuid.Nil, err
	}
	return targetID, nil
}

func (m *BackupManager) targetOwnedByResource(ctx context.Context, targetID, resourceOwner uuid.UUID) error {
	if targetID == uuid.Nil {
		return nil
	}
	if m.storages == nil {
		return storages.ErrNotReady
	}
	target, err := m.storages.ResolveAny(ctx, targetID)
	if err != nil {
		return err
	}
	if target == nil {
		return ErrNotFound
	}
	if target.Kind == TargetS3 && target.UserID != resourceOwner {
		return ErrNotFound
	}
	return nil
}

// newStorageManager builds the storage service for a manager created without a
// shared one (tests and embedders). The backend factory keeps the object-store
// test seam on the manager.
func newStorageManager(cfg BackupConfig, repo BackupRepository, manager *BackupManager, localDir string) *storages.Manager {
	var storageRepo storages.Repository
	switch {
	case cfg.Store != nil:
		storageRepo = storages.NewStoreRepository(cfg.Store)
	default:
		typed, ok := repo.(storages.Repository)
		if !ok {
			return nil
		}
		storageRepo = typed
	}
	return storages.New(storages.Config{
		Repository: storageRepo,
		Secret:     cfg.Secret,
		LocalDir:   localDir,
		Backends:   storageBackends{manager: manager},
	})
}

type storageBackends struct {
	manager *BackupManager
}

func (b storageBackends) Open(_ context.Context, storage storages.Storage, creds storages.Credentials) (storages.ObjectStore, error) {
	if b.manager.objects != nil {
		return b.manager.objects, nil
	}
	if storage.Kind == storages.KindLocal {
		return newLocalStore(b.manager.localDir)
	}
	if storage.Kind != storages.KindS3 {
		return nil, fmt.Errorf("%w: %q", storages.ErrUnsupported, storage.Kind)
	}
	return newS3Store(s3Config{
		endpoint:  storage.Endpoint,
		region:    storage.Region,
		bucket:    storage.Bucket,
		prefix:    storage.Prefix,
		accessKey: creds.AccessKey,
		secretKey: creds.SecretKey,
	})
}

func sealTargetSecrets(secret string, targetID uuid.UUID, accessKey, secretKey string) ([]TargetSecret, error) {
	return storages.SealSecrets(secret, targetID, accessKey, secretKey)
}

func openTargetSecrets(secret string, secrets []TargetSecret) (string, string, error) {
	return storages.OpenSecrets(secret, secrets)
}
