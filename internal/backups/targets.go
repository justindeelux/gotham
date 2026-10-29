package backups

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/providers"
)

func (m *BackupManager) ListProviders(ctx context.Context, userID uuid.UUID) ([]BackupTarget, error) {
	if err := m.backupsReady(); err != nil {
		return nil, err
	}
	if userID == uuid.Nil {
		return nil, ErrNotFound
	}
	// The built-in provider is part of the contract, not a row a migration
	// must have created: ensure it before listing so it is always visible.
	if _, err := m.backups.EnsureBuiltinTarget(ctx, userID); err != nil {
		m.logger.Warn("backups: could not ensure the built-in provider",
			"user_id", userID.String(), "error", err)
	}
	targets, err := m.backups.ListBackupTargetsByUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	if targets == nil {
		return []BackupTarget{}, nil
	}
	usage, err := m.backups.BackupUsageByTarget(ctx, userID)
	if err != nil {
		m.logger.Warn("backups: could not read provider usage", "user_id", userID.String(), "error", err)
	}
	free, total, diskErr := diskStats(m.localDir)
	for i := range targets {
		if item, ok := usage[targets[i].ID]; ok {
			targets[i].Usage.BackupCount = item.BackupCount
			targets[i].Usage.TotalSize = item.TotalSize
		}
		if targets[i].Kind == TargetLocal && diskErr == nil {
			targets[i].Usage.FreeSpace = free
			targets[i].Usage.TotalSpace = total
		}
	}
	return targets, nil
}

func (m *BackupManager) CreateProvider(ctx context.Context, userID uuid.UUID, req ProviderRequest) (BackupTarget, error) {
	if err := m.backupsReady(); err != nil {
		return BackupTarget{}, err
	}
	if userID == uuid.Nil {
		return BackupTarget{}, ErrNotFound
	}
	target := BackupTarget{
		ID:     uuid.New(),
		UserID: userID,
	}
	if err := applyTargetRequest(&target, req); err != nil {
		return BackupTarget{}, err
	}
	if target.Kind == TargetS3 && !requestCarriesCredentials(req) {
		return BackupTarget{}, s3CredentialsRequired()
	}
	now := m.now()
	target.CreatedAt, target.UpdatedAt = now, now
	sealed, err := sealTargetSecrets(m.secret, target.ID, req.AccessKey, req.SecretKey)
	if err != nil {
		return BackupTarget{}, err
	}
	return m.backups.CreateBackupTargetWithSecrets(ctx, target, sealed)
}

func (m *BackupManager) UpdateProvider(ctx context.Context, userID, providerID uuid.UUID, req ProviderRequest) (BackupTarget, error) {
	target, err := m.ownedTarget(ctx, userID, providerID)
	if err != nil {
		return BackupTarget{}, err
	}
	if target == nil {
		return BackupTarget{}, ErrNotFound
	}
	if target.Builtin {
		// The built-in keeps its identity: a new name would free "Local disk"
		// for the seeder and grow a second builtin on the next list.
		if name := strings.TrimSpace(req.Name); name != "" && name != target.Name {
			return BackupTarget{}, fmt.Errorf("%w: the built-in Local disk provider keeps its name", ErrBuiltinProvider)
		}
		if kind := strings.TrimSpace(req.Kind); kind != "" &&
			!strings.EqualFold(kind, string(TargetLocal)) {
			return BackupTarget{}, ErrBuiltinProvider
		}
		req.Kind = string(TargetLocal)
		req.Endpoint, req.Bucket, req.AccessKey, req.SecretKey = "", "", "", ""
		req.Region, req.Prefix = nil, nil
	}
	if err := applyTargetRequest(target, req); err != nil {
		return BackupTarget{}, err
	}
	if target.Kind == TargetS3 && !requestCarriesCredentials(req) {
		stored, err := m.targetHasStoredCredentials(ctx, providerID)
		if err != nil {
			return BackupTarget{}, err
		}
		if !stored {
			return BackupTarget{}, s3CredentialsRequired()
		}
	}
	sealed, err := sealTargetSecrets(m.secret, target.ID, req.AccessKey, req.SecretKey)
	if err != nil {
		return BackupTarget{}, err
	}
	target.UpdatedAt = m.now()
	return m.backups.UpdateBackupTargetWithSecrets(ctx, *target, sealed)
}

func (m *BackupManager) DeleteProvider(ctx context.Context, userID, providerID uuid.UUID, confirm bool) error {
	target, err := m.ownedTarget(ctx, userID, providerID)
	if err != nil {
		return err
	}
	if target == nil {
		return ErrNotFound
	}
	if target.Builtin {
		return fmt.Errorf("%w: the built-in Local disk provider cannot be deleted", ErrBuiltinProvider)
	}
	schedules, err := m.backups.ListBackupScheduleIDsForTarget(ctx, providerID)
	if err != nil {
		return err
	}
	if len(schedules) > 0 {
		ids := make([]string, 0, len(schedules))
		for _, id := range schedules {
			ids = append(ids, id.String())
		}
		return fmt.Errorf("%w: %d schedule(s) still use this provider (%s); move or delete them first",
			ErrProviderInUse, len(schedules), strings.Join(ids, ", "))
	}
	backups, err := m.backups.CountBackupsForTarget(ctx, providerID)
	if err != nil {
		return err
	}
	if backups > 0 && !confirm {
		return fmt.Errorf("%w: %d backup(s) use this provider and become unreadable; confirm to delete it",
			ErrProviderHasBackups, backups)
	}
	if backups > 0 {
		if err := m.backups.MarkBackupsProviderRemoved(ctx, providerID); err != nil {
			return err
		}
	}
	if _, err := m.backups.DeleteBackupTarget(ctx, providerID, userID); err != nil {
		return err
	}
	return nil
}

func (m *BackupManager) TestProvider(ctx context.Context, userID, providerID uuid.UUID) (TargetCheck, error) {
	target, err := m.ownedTarget(ctx, userID, providerID)
	if err != nil {
		return TargetCheck{}, err
	}
	if target == nil {
		return TargetCheck{}, ErrNotFound
	}
	store, err := m.storeForTarget(ctx, target)
	if err != nil {
		return TargetCheck{OK: false, Message: boundedDiag(err.Error())}, nil
	}
	if s3, ok := store.(*s3Store); ok {
		exists, err := s3.bucketExists(ctx)
		if err != nil {
			return TargetCheck{OK: false, Message: boundedDiag(err.Error())}, nil
		}
		if !exists {
			return TargetCheck{OK: false, Message: "bucket " + target.Bucket + " does not exist or is not accessible"}, nil
		}
	}
	// A read-only or lifecycle-restricted destination must be reported now,
	// not after the first real backup has already failed.
	if err := probeStore(ctx, store); err != nil {
		return TargetCheck{OK: false, Message: boundedDiag(err.Error())}, nil
	}
	if store.Kind() == string(TargetS3) {
		return TargetCheck{OK: true, Message: "connected to " + target.Bucket}, nil
	}
	return TargetCheck{OK: true, Message: "local backup directory is ready"}, nil
}

const probePayload = "gotham-backup-probe"

// probeStore writes, reads back and deletes a small object so the check proves
// the provider can hold and return a backup, not just accept a connection.
func probeStore(ctx context.Context, store ObjectStore) error {
	key := ".gotham-probe-" + uuid.NewString()
	location, err := store.Put(ctx, key, strings.NewReader(probePayload), int64(len(probePayload)))
	if err != nil {
		return fmt.Errorf("%w: the provider is not writable: %v", ErrValidation, err)
	}
	readErr := func() error {
		reader, err := store.Get(ctx, location)
		if err != nil {
			return err
		}
		defer func() { _ = reader.Close() }()
		got, err := io.ReadAll(io.LimitReader(reader, int64(len(probePayload))+1))
		if err != nil {
			return err
		}
		if string(got) != probePayload {
			return errors.New("the probe object came back different")
		}
		return nil
	}()
	deleteErr := store.Delete(ctx, location)
	if readErr != nil {
		return fmt.Errorf("%w: the provider cannot read a backup back: %v", ErrValidation, readErr)
	}
	if deleteErr != nil {
		return fmt.Errorf("%w: the provider does not allow deleting a backup: %v", ErrValidation, deleteErr)
	}
	return nil
}

func (m *BackupManager) storeAndKey(
	ctx context.Context,
	target *BackupTarget,
	resource Resource,
	runID uuid.UUID,
	extension string,
) (ObjectStore, string, error) {
	if strings.TrimSpace(extension) == "" {
		return nil, "", fmt.Errorf("%w: the backup driver reported no artifact extension", ErrValidation)
	}
	key := fmt.Sprintf("%ss/%s/%s.%s.gz", resource.Type, resource.ID, runID, extension)
	if m.objects != nil {
		return m.objects, key, nil
	}
	store, err := m.storeForTarget(ctx, target)
	if err != nil {
		return nil, "", err
	}
	return store, key, nil
}

// defaultTarget is the resource owner's built-in Local disk provider. Every
// account gets one, including a member who created a resource, so those runs
// are labelled the same way as the owner's. If seeding is impossible the run
// still falls back to the local store instead of failing.
func (m *BackupManager) defaultTarget(ctx context.Context, ownerID uuid.UUID) (*BackupTarget, error) {
	if ownerID == uuid.Nil || m.backups == nil {
		return nil, nil
	}
	target, err := m.backups.GetBuiltinTarget(ctx, ownerID)
	if err == nil {
		return &target, nil
	}
	if !errors.Is(err, ErrNotFound) {
		return nil, err
	}
	target, err = m.backups.EnsureBuiltinTarget(ctx, ownerID)
	if err != nil {
		m.logger.Warn("backups: could not seed the built-in provider",
			"user_id", ownerID.String(), "error", err)
		return nil, nil
	}
	return &target, nil
}

func (m *BackupManager) storeForTarget(ctx context.Context, target *BackupTarget) (ObjectStore, error) {
	if m.objects != nil {
		return m.objects, nil
	}
	if target == nil || target.Kind == TargetLocal {
		return newLocalStore(m.localDir)
	}
	if target.Kind != TargetS3 {
		return nil, fmt.Errorf("%w: unsupported provider kind %q", ErrValidation, target.Kind)
	}
	config, err := m.openTarget(ctx, *target)
	if err != nil {
		return nil, err
	}
	return newS3Store(config)
}

func (m *BackupManager) storeForLocation(ctx context.Context, backup Backup, ownerID uuid.UUID) (ObjectStore, error) {
	if m.objects != nil {
		return m.objects, nil
	}
	switch {
	case strings.HasPrefix(backup.Location, locationFilePrefix):
		return newLocalStore(m.localDir)
	case strings.HasPrefix(backup.Location, locationS3Prefix):
		if backup.TargetID == uuid.Nil {
			return nil, fmt.Errorf("%w: the storage provider of this backup no longer exists", ErrValidation)
		}
		target, err := m.backups.GetBackupTarget(ctx, backup.TargetID)
		if err != nil {
			return nil, err
		}
		if ownerID != uuid.Nil && target.UserID != ownerID {
			return nil, ErrNotFound
		}
		return m.storeForTarget(ctx, &target)
	default:
		return nil, fmt.Errorf("%w: unknown backup location %q", ErrValidation, backup.Location)
	}
}

func (m *BackupManager) openTarget(ctx context.Context, target BackupTarget) (s3Config, error) {
	secrets, err := m.backups.ListTargetSecrets(ctx, target.ID)
	if err != nil {
		return s3Config{}, err
	}
	accessKey, secretKey, err := openTargetSecrets(m.secret, secrets)
	if err != nil {
		return s3Config{}, err
	}
	return s3Config{
		endpoint:  target.Endpoint,
		region:    target.Region,
		bucket:    target.Bucket,
		prefix:    target.Prefix,
		accessKey: accessKey,
		secretKey: secretKey,
	}, nil
}

func requestCarriesCredentials(req ProviderRequest) bool {
	return strings.TrimSpace(req.AccessKey) != "" && strings.TrimSpace(req.SecretKey) != ""
}

func s3CredentialsRequired() error {
	return fmt.Errorf("%w: s3 providers need both an access key and a secret key", ErrValidation)
}

func (m *BackupManager) targetHasStoredCredentials(ctx context.Context, targetID uuid.UUID) (bool, error) {
	secrets, err := m.backups.ListTargetSecrets(ctx, targetID)
	if err != nil {
		return false, err
	}
	accessKey, secretKey, err := openTargetSecrets(m.secret, secrets)
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(accessKey) != "" && strings.TrimSpace(secretKey) != "", nil
}

func sealTargetSecrets(secret string, targetID uuid.UUID, accessKey, secretKey string) ([]TargetSecret, error) {
	pairs := [][2]string{
		{targetSecretAccessKey, accessKey},
		{targetSecretSecretKey, secretKey},
	}
	sealed := make([]TargetSecret, 0, len(pairs))
	for _, pair := range pairs {
		if strings.TrimSpace(pair[1]) == "" {
			continue
		}
		ciphertext, err := providers.SealSecret(secret, pair[1])
		if err != nil {
			return nil, fmt.Errorf("backups: seal %s: %w", pair[0], err)
		}
		sealed = append(sealed, TargetSecret{
			TargetID:   targetID,
			Key:        pair[0],
			Ciphertext: ciphertext,
		})
	}
	return sealed, nil
}

func openTargetSecrets(secret string, secrets []TargetSecret) (string, string, error) {
	var accessKey, secretKey string
	for _, item := range secrets {
		value, err := providers.OpenSecret(secret, item.Ciphertext)
		if err != nil {
			return "", "", fmt.Errorf("backups: open %s: %w", item.Key, err)
		}
		switch item.Key {
		case targetSecretAccessKey:
			accessKey = value
		case targetSecretSecretKey:
			secretKey = value
		}
	}
	return accessKey, secretKey, nil
}

func applyTargetRequest(target *BackupTarget, req ProviderRequest) error {
	if name := strings.TrimSpace(req.Name); name != "" {
		target.Name = name
	}
	if kind := strings.ToLower(strings.TrimSpace(req.Kind)); kind != "" {
		switch TargetKind(kind) {
		case TargetS3, TargetLocal:
			target.Kind = TargetKind(kind)
		default:
			return fmt.Errorf("%w: unsupported provider kind %q (supported: s3, local)", ErrValidation, req.Kind)
		}
	}
	if endpoint := strings.TrimSpace(req.Endpoint); endpoint != "" {
		target.Endpoint = endpoint
	}
	if req.Region != nil {
		target.Region = strings.TrimSpace(*req.Region)
	}
	if bucket := strings.TrimSpace(req.Bucket); bucket != "" {
		target.Bucket = bucket
	}
	if req.Prefix != nil {
		target.Prefix = strings.TrimSpace(*req.Prefix)
	}

	if strings.TrimSpace(target.Name) == "" {
		return fmt.Errorf("%w: name is required", ErrValidation)
	}
	if target.Kind == "" {
		return fmt.Errorf("%w: kind is required (s3 or local)", ErrValidation)
	}
	if target.Kind == TargetS3 {
		if strings.TrimSpace(target.Endpoint) == "" {
			return fmt.Errorf("%w: endpoint is required for an s3 provider", ErrValidation)
		}
		if strings.TrimSpace(target.Bucket) == "" {
			return fmt.Errorf("%w: bucket is required for an s3 provider", ErrValidation)
		}
	}
	return nil
}

func (m *BackupManager) ownedTarget(ctx context.Context, userID, targetID uuid.UUID) (*BackupTarget, error) {
	if targetID == uuid.Nil {
		return nil, nil
	}
	target, err := m.backups.GetBackupTarget(ctx, targetID)
	if err != nil {
		return nil, err
	}
	if target.UserID != userID {
		return nil, ErrNotFound
	}
	return &target, nil
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
	target, err := m.backups.GetBackupTarget(ctx, targetID)
	if err != nil {
		return err
	}
	if target.Kind == TargetS3 && target.UserID != resourceOwner {
		return ErrNotFound
	}
	return nil
}
