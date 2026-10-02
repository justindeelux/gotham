package databases

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/providers"
)

// Storage target surfaces: CRUD, credential sealing and store resolution.

// ListTargets implements BackupService.
func (m *BackupManager) ListTargets(ctx context.Context, userID uuid.UUID) ([]BackupTarget, error) {
	if err := m.backupsReady(); err != nil {
		return nil, err
	}
	if userID == uuid.Nil {
		return nil, ErrNotFound
	}
	targets, err := m.backups.ListBackupTargetsByUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	if targets == nil {
		return []BackupTarget{}, nil
	}
	return targets, nil
}

// CreateTarget implements BackupService: configuration in the clear,
// credentials sealed.
func (m *BackupManager) CreateTarget(ctx context.Context, userID uuid.UUID, req TargetRequest) (BackupTarget, error) {
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
	created, err := m.backups.CreateBackupTargetWithSecrets(ctx, target, sealed)
	if err != nil {
		return BackupTarget{}, err
	}
	return created, nil
}

// UpdateTarget implements BackupService. Credentials are replaced only when
// the request carries new ones, so a UI that resends the masked value cannot
// wipe a stored key. Switching the target to s3 has the same requirement as
// CreateTarget: the request must carry a complete pair or the target must
// already hold one, so an s3 target can never end up without keys.
func (m *BackupManager) UpdateTarget(ctx context.Context, userID, targetID uuid.UUID, req TargetRequest) (BackupTarget, error) {
	target, err := m.ownedTarget(ctx, userID, targetID)
	if err != nil {
		return BackupTarget{}, err
	}
	if err := applyTargetRequest(target, req); err != nil {
		return BackupTarget{}, err
	}
	if target.Kind == TargetS3 && !requestCarriesCredentials(req) {
		stored, err := m.targetHasStoredCredentials(ctx, targetID)
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
	// The write locks the target row and refuses a destination change while a
	// run references it, in the same transaction. A recorded location names the
	// endpoint and bucket it was written to, so moving the target would strand
	// every completed backup and every run still in flight; a new target is the
	// escape hatch.
	updated, err := m.backups.UpdateBackupTargetWithSecrets(ctx, *target, sealed)
	if err != nil {
		return BackupTarget{}, err
	}
	return updated, nil
}

// DeleteTarget implements BackupService. Backups that used the target keep
// their location; only their ability to be read back from S3 goes with the
// credentials, which the API surface documents.
func (m *BackupManager) DeleteTarget(ctx context.Context, userID, targetID uuid.UUID) error {
	if _, err := m.ownedTarget(ctx, userID, targetID); err != nil {
		return err
	}
	if _, err := m.backups.DeleteBackupTarget(ctx, targetID, userID); err != nil {
		return err
	}
	return nil
}

// TestTarget implements BackupService: it builds the real client and asks
// whether the bucket is reachable. The answer never includes credentials.
func (m *BackupManager) TestTarget(ctx context.Context, userID, targetID uuid.UUID) (TargetCheck, error) {
	target, err := m.ownedTarget(ctx, userID, targetID)
	if err != nil {
		return TargetCheck{}, err
	}
	store, err := m.storeForTarget(ctx, target)
	if err != nil {
		return TargetCheck{OK: false, Message: boundedDiag(err.Error())}, nil
	}
	s3, ok := store.(*s3Store)
	if !ok {
		return TargetCheck{OK: true, Message: "local backup directory is ready"}, nil
	}
	exists, err := s3.bucketExists(ctx)
	if err != nil {
		return TargetCheck{OK: false, Message: boundedDiag(err.Error())}, nil
	}
	if !exists {
		return TargetCheck{OK: false, Message: "bucket " + target.Bucket + " does not exist or is not accessible"}, nil
	}
	return TargetCheck{OK: true, Message: "connected to " + target.Bucket}, nil
}

// storeAndKey resolves the store a new artifact goes to and the key it is
// written under. The key is identical for every target — "s3://" locations
// carry the bucket and "file://" locations the absolute path, and a target
// prefix only ever applies inside S3.
func (m *BackupManager) storeAndKey(
	ctx context.Context,
	target *BackupTarget,
	database Database,
	runID uuid.UUID,
) (ObjectStore, string, error) {
	engine, ok := LookupBackupEngine(database.Engine)
	if !ok {
		return nil, "", fmt.Errorf("%w: engine %q has no backup engine", ErrValidation, database.Engine)
	}
	key := fmt.Sprintf("databases/%s/%s.%s.gz", database.ID, runID, engine.DumpExtension())
	if m.objects != nil {
		return m.objects, key, nil
	}
	store, err := m.storeForTarget(ctx, target)
	if err != nil {
		return nil, "", err
	}
	return store, key, nil
}

// storeForTarget resolves the store of one target; a nil target (or a local
// one) is the control plane's own backup directory.
func (m *BackupManager) storeForTarget(ctx context.Context, target *BackupTarget) (ObjectStore, error) {
	if m.objects != nil {
		return m.objects, nil
	}
	if target == nil || target.Kind == TargetLocal {
		return newLocalStore(m.localDir)
	}
	if target.Kind != TargetS3 {
		return nil, fmt.Errorf("%w: unsupported target kind %q", ErrValidation, target.Kind)
	}
	config, err := m.openTarget(ctx, *target)
	if err != nil {
		return nil, err
	}
	return newS3Store(config)
}

// storeForLocation resolves the store that can read a recorded location. The
// scheme decides: local files need no target, an s3:// location needs the
// target its credentials were sealed with.
func (m *BackupManager) storeForLocation(ctx context.Context, backup Backup, database *Database) (ObjectStore, error) {
	if m.objects != nil {
		return m.objects, nil
	}
	switch {
	case strings.HasPrefix(backup.Location, locationFilePrefix):
		return newLocalStore(m.localDir)
	case strings.HasPrefix(backup.Location, locationS3Prefix):
		if backup.TargetID == uuid.Nil {
			return nil, fmt.Errorf("%w: the storage target of this backup no longer exists", ErrValidation)
		}
		target, err := m.backups.GetBackupTarget(ctx, backup.TargetID)
		if err != nil {
			return nil, err
		}
		if database != nil && target.UserID != database.UserID {
			return nil, ErrNotFound
		}
		return m.storeForTarget(ctx, &target)
	default:
		return nil, fmt.Errorf("%w: unknown backup location %q", ErrValidation, backup.Location)
	}
}

// openTarget opens a target's sealed credentials and assembles the plaintext
// configuration of its client. The values returned here are never logged and
// never leave this call chain.
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

// requestCarriesCredentials reports whether the request carries a complete
// non-blank credential pair.
func requestCarriesCredentials(req TargetRequest) bool {
	return strings.TrimSpace(req.AccessKey) != "" && strings.TrimSpace(req.SecretKey) != ""
}

// s3CredentialsRequired is the shared create/update rejection: an s3 target
// must always hold a complete access/secret pair.
func s3CredentialsRequired() error {
	return fmt.Errorf("%w: s3 targets need both an access key and a secret key", ErrValidation)
}

// targetHasStoredCredentials reports whether a target already holds an
// openable, non-blank access/secret pair.
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

// sealTargetSecrets seals the halves of an S3 login with AES-256-GCM
// (providers.SealSecret). Blank values are skipped so a partial update
// replaces only what it carries.
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
			return nil, fmt.Errorf("databases: seal %s: %w", pair[0], err)
		}
		sealed = append(sealed, TargetSecret{
			TargetID:   targetID,
			Key:        pair[0],
			Ciphertext: ciphertext,
		})
	}
	return sealed, nil
}

// openTargetSecrets reverses sealTargetSecrets. Unknown keys are ignored so a
// future credential can travel through the same table.
func openTargetSecrets(secret string, secrets []TargetSecret) (string, string, error) {
	var accessKey, secretKey string
	for _, item := range secrets {
		value, err := providers.OpenSecret(secret, item.Ciphertext)
		if err != nil {
			return "", "", fmt.Errorf("databases: open %s: %w", item.Key, err)
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

// applyTargetRequest copies the request onto the target, keeping stored
// values for fields the request leaves empty, and validates the resulting
// configuration. Credentials are handled separately by the callers.
func applyTargetRequest(target *BackupTarget, req TargetRequest) error {
	if name := strings.TrimSpace(req.Name); name != "" {
		target.Name = name
	}
	if kind := strings.ToLower(strings.TrimSpace(req.Kind)); kind != "" {
		switch TargetKind(kind) {
		case TargetS3, TargetLocal:
			target.Kind = TargetKind(kind)
		default:
			return fmt.Errorf("%w: unsupported target kind %q (supported: s3, local)", ErrValidation, req.Kind)
		}
	}
	if endpoint := strings.TrimSpace(req.Endpoint); endpoint != "" {
		target.Endpoint = endpoint
	}
	if region := strings.TrimSpace(req.Region); region != "" {
		target.Region = region
	}
	if bucket := strings.TrimSpace(req.Bucket); bucket != "" {
		target.Bucket = bucket
	}
	if prefix := strings.TrimSpace(req.Prefix); prefix != "" {
		target.Prefix = prefix
	}

	if strings.TrimSpace(target.Name) == "" {
		return fmt.Errorf("%w: name is required", ErrValidation)
	}
	if target.Kind == "" {
		return fmt.Errorf("%w: kind is required (s3 or local)", ErrValidation)
	}
	if target.Kind == TargetS3 {
		if strings.TrimSpace(target.Endpoint) == "" {
			return fmt.Errorf("%w: endpoint is required for an s3 target", ErrValidation)
		}
		if strings.TrimSpace(target.Bucket) == "" {
			return fmt.Errorf("%w: bucket is required for an s3 target", ErrValidation)
		}
	}
	return nil
}

// ownedTarget loads a target the caller owns. A zero target id means "the
// default local store" and resolves to a nil target instead of an error.
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

// ownedTargetID is ownedTarget for the string ids the API carries.
func (m *BackupManager) ownedTargetID(ctx context.Context, userID uuid.UUID, raw string) (uuid.UUID, error) {
	if strings.TrimSpace(raw) == "" {
		return uuid.Nil, nil
	}
	targetID, err := uuid.Parse(raw)
	if err != nil {
		return uuid.Nil, fmt.Errorf("%w: invalid target id", ErrValidation)
	}
	if _, err := m.ownedTarget(ctx, userID, targetID); err != nil {
		return uuid.Nil, err
	}
	return targetID, nil
}

// targetOwnedByDatabase reports whether a target may store a database's
// backups. An S3 target is resolved through the database owner on the read
// path, so it must belong to that owner; a local target records a file://
// location and is readable regardless of who owns it. It returns ErrNotFound
// for an inconsistent pair, so the caller cannot probe a target it could never
// use.
func (m *BackupManager) targetOwnedByDatabase(ctx context.Context, targetID, databaseOwner uuid.UUID) error {
	if targetID == uuid.Nil {
		return nil
	}
	target, err := m.backups.GetBackupTarget(ctx, targetID)
	if err != nil {
		return err
	}
	if target.Kind == TargetS3 && target.UserID != databaseOwner {
		return ErrNotFound
	}
	return nil
}
