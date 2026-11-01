package storages

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/netguard"
	"github.com/justindeelux/gotham/internal/providers"
)

const probePayload = "gotham-backup-probe"

// Config wires the storage domain.
type Config struct {
	Repository Repository
	// Secret seals credentials at rest; an empty key is refused by the crypto
	// helper rather than storing plaintext.
	Secret string
	// LocalDir is the control-plane directory the built-in local storage uses.
	LocalDir string
	Logger   *slog.Logger
	// Backends overrides the real local/S3 factories in tests.
	Backends BackendFactory
}

// Manager owns storage destination CRUD, usage and the connection test.
type Manager struct {
	repo     Repository
	backends BackendFactory
	secret   string
	localDir string
	logger   *slog.Logger
	now      func() time.Time
}

// New builds the storage service.
func New(cfg Config) *Manager {
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	backends := cfg.Backends
	if backends == nil {
		backends = DefaultBackends{LocalDir: cfg.LocalDir}
	}
	return &Manager{
		repo:     cfg.Repository,
		backends: backends,
		secret:   cfg.Secret,
		localDir: cfg.LocalDir,
		logger:   logger,
		now:      func() time.Time { return time.Now().UTC() },
	}
}

func (m *Manager) ready() error {
	if m == nil || m.repo == nil {
		return ErrNotReady
	}
	return nil
}

// List returns every storage of the account, ensuring the built-in Local disk
// destination exists first so a fresh install always has one.
func (m *Manager) List(ctx context.Context, userID uuid.UUID) ([]Storage, error) {
	if err := m.ready(); err != nil {
		return nil, err
	}
	if userID == uuid.Nil {
		return nil, ErrNotFound
	}
	if _, err := m.repo.EnsureBuiltinTarget(ctx, userID); err != nil {
		m.logger.Warn("storages: could not ensure the built-in storage",
			"user_id", userID.String(), "error", err)
	}
	targets, err := m.repo.ListBackupTargetsByUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	if targets == nil {
		return []Storage{}, nil
	}
	usage, err := m.repo.BackupUsageByTarget(ctx, userID)
	if err != nil {
		m.logger.Warn("storages: could not read storage usage", "user_id", userID.String(), "error", err)
	}
	free, total, diskErr := DiskStats(m.localDir)
	for i := range targets {
		if item, ok := usage[targets[i].ID]; ok {
			targets[i].Usage.BackupCount = item.BackupCount
			targets[i].Usage.TotalSize = item.TotalSize
		}
		if targets[i].Kind == KindLocal && diskErr == nil {
			targets[i].Usage.FreeSpace = free
			targets[i].Usage.TotalSpace = total
		}
	}
	return targets, nil
}

// Create validates and seals a new destination.
func (m *Manager) Create(ctx context.Context, userID uuid.UUID, req Request) (Storage, error) {
	if err := m.ready(); err != nil {
		return Storage{}, err
	}
	if userID == uuid.Nil {
		return Storage{}, ErrNotFound
	}
	target := Storage{
		ID:     uuid.New(),
		UserID: userID,
	}
	if err := applyRequest(&target, req); err != nil {
		return Storage{}, err
	}
	if target.Kind == KindS3 {
		if err := validateEndpoint(ctx, target.Endpoint); err != nil {
			return Storage{}, err
		}
		if !requestCarriesCredentials(req) {
			return Storage{}, s3CredentialsRequired()
		}
	}
	now := m.now()
	target.CreatedAt, target.UpdatedAt = now, now
	sealed, err := SealSecrets(m.secret, target.ID, req.AccessKey, req.SecretKey)
	if err != nil {
		return Storage{}, err
	}
	return m.repo.CreateBackupTargetWithSecrets(ctx, target, sealed)
}

// Update keeps the built-in Local disk identity fixed and preserves stored
// credentials when the request leaves the key fields blank.
func (m *Manager) Update(ctx context.Context, userID, storageID uuid.UUID, req Request) (Storage, error) {
	if err := m.ready(); err != nil {
		return Storage{}, err
	}
	target, err := m.owned(ctx, userID, storageID)
	if err != nil {
		return Storage{}, err
	}
	if target == nil {
		return Storage{}, ErrNotFound
	}
	if target.Builtin {
		// The built-in keeps its identity: a new name would free "Local disk"
		// for the seeder and grow a second builtin on the next list.
		if name := strings.TrimSpace(req.Name); name != "" && name != target.Name {
			return Storage{}, fmt.Errorf("%w: the built-in Local disk storage keeps its name", ErrBuiltin)
		}
		if kind := strings.TrimSpace(req.Kind); kind != "" &&
			!strings.EqualFold(kind, string(KindLocal)) {
			return Storage{}, ErrBuiltin
		}
		req.Kind = string(KindLocal)
		req.Endpoint, req.Bucket, req.AccessKey, req.SecretKey = "", "", "", ""
		req.Region, req.Prefix = nil, nil
	}
	if err := applyRequest(target, req); err != nil {
		return Storage{}, err
	}
	if target.Kind == KindS3 {
		if err := validateEndpoint(ctx, target.Endpoint); err != nil {
			return Storage{}, err
		}
	}
	if target.Kind == KindS3 && !requestCarriesCredentials(req) {
		stored, err := m.hasStoredCredentials(ctx, storageID)
		if err != nil {
			return Storage{}, err
		}
		if !stored {
			return Storage{}, s3CredentialsRequired()
		}
	}
	sealed, err := SealSecrets(m.secret, target.ID, req.AccessKey, req.SecretKey)
	if err != nil {
		return Storage{}, err
	}
	target.UpdatedAt = m.now()
	return m.repo.UpdateBackupTargetWithSecrets(ctx, *target, sealed)
}

// Delete refuses a destination still used by a schedule, and a destination
// that still holds backups unless the caller explicitly confirmed losing them.
func (m *Manager) Delete(ctx context.Context, userID, storageID uuid.UUID, confirm bool) error {
	if err := m.ready(); err != nil {
		return err
	}
	target, schedules, backups, deleted, err := m.repo.DeleteGuarded(ctx, storageID, userID, confirm)
	if err != nil {
		return err
	}
	if target.Builtin {
		return fmt.Errorf("%w: the built-in Local disk storage cannot be deleted", ErrBuiltin)
	}
	if len(schedules) > 0 {
		ids := make([]string, 0, len(schedules))
		for _, id := range schedules {
			ids = append(ids, id.String())
		}
		return fmt.Errorf("%w: %d schedule(s) still use this storage (%s); move or delete them first",
			ErrInUse, len(schedules), strings.Join(ids, ", "))
	}
	if backups > 0 && !confirm {
		return fmt.Errorf("%w: %d backup(s) use this storage and become unreadable; confirm to delete it",
			ErrHasBackups, backups)
	}
	if !deleted {
		return ErrNotFound
	}
	return nil
}

// Test proves the destination can hold and return an artifact, not just accept
// a connection: it writes, reads back and deletes a small probe object.
func (m *Manager) Test(ctx context.Context, userID, storageID uuid.UUID) (Check, error) {
	if err := m.ready(); err != nil {
		return Check{}, err
	}
	target, err := m.owned(ctx, userID, storageID)
	if err != nil {
		return Check{}, err
	}
	if target == nil {
		return Check{}, ErrNotFound
	}
	store, err := m.Open(ctx, *target)
	if err != nil {
		return m.connectionFailure(storageID, err), nil
	}
	if s3, ok := store.(interface {
		BucketExists(context.Context) (bool, error)
	}); ok {
		exists, err := s3.BucketExists(ctx)
		if err != nil {
			return m.connectionFailure(storageID, err), nil
		}
		if !exists {
			return Check{OK: false, Message: "bucket " + target.Bucket + " does not exist or is not accessible"}, nil
		}
	}
	// A read-only or lifecycle-restricted destination must be reported now,
	// not after the first real backup has already failed.
	if err := probe(ctx, store); err != nil {
		return m.connectionFailure(storageID, err), nil
	}
	if store.Kind() == string(KindS3) {
		return Check{OK: true, Message: "connected to " + target.Bucket}, nil
	}
	return Check{OK: true, Message: "local backup directory is ready"}, nil
}

// Open decrypts the credentials and builds the backend. Callers must not log
// the returned store's configuration.
func (m *Manager) Open(ctx context.Context, storage Storage) (ObjectStore, error) {
	if err := m.ready(); err != nil {
		return nil, err
	}
	if storage.Kind == KindS3 {
		if err := validateEndpoint(ctx, storage.Endpoint); err != nil {
			return nil, err
		}
	}
	secrets, err := m.repo.ListTargetSecrets(ctx, storage.ID)
	if err != nil {
		return nil, err
	}
	accessKey, secretKey, err := OpenSecrets(m.secret, secrets)
	if err != nil {
		return nil, err
	}
	return m.backends.Open(ctx, storage, Credentials{AccessKey: accessKey, SecretKey: secretKey})
}

// EnsureBuiltin returns the account's built-in Local disk storage, creating it
// when the first start did not.
func (m *Manager) EnsureBuiltin(ctx context.Context, userID uuid.UUID) (Storage, error) {
	if err := m.ready(); err != nil {
		return Storage{}, err
	}
	return m.repo.EnsureBuiltinTarget(ctx, userID)
}

// Builtin returns the account's built-in storage, or nil when it has none.
func (m *Manager) Builtin(ctx context.Context, userID uuid.UUID) (*Storage, error) {
	if err := m.ready(); err != nil {
		return nil, err
	}
	target, err := m.repo.GetBuiltinTarget(ctx, userID)
	if errors.Is(err, ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &target, nil
}

// Resolve looks a storage up for an owning account. No owner means no access:
// internal callers that already resolved the owner use ResolveAny instead.
func (m *Manager) Resolve(ctx context.Context, ownerID, storageID uuid.UUID) (*Storage, error) {
	if ownerID == uuid.Nil {
		return nil, ErrNotFound
	}
	return m.owned(ctx, ownerID, storageID)
}

// ResolveAny looks a storage up by id without an ownership check. It exists
// only for trusted internal callers (scheduler, cleanup, run labelling) that
// already hold the row id from another owned record.
func (m *Manager) ResolveAny(ctx context.Context, storageID uuid.UUID) (*Storage, error) {
	if err := m.ready(); err != nil {
		return nil, err
	}
	if storageID == uuid.Nil {
		return nil, nil
	}
	target, err := m.repo.GetBackupTarget(ctx, storageID)
	if err != nil {
		return nil, err
	}
	return &target, nil
}

func (m *Manager) owned(ctx context.Context, userID, storageID uuid.UUID) (*Storage, error) {
	if storageID == uuid.Nil {
		return nil, nil
	}
	target, err := m.repo.GetBackupTarget(ctx, storageID)
	if err != nil {
		return nil, err
	}
	if target.UserID != userID {
		return nil, ErrNotFound
	}
	return &target, nil
}

func (m *Manager) hasStoredCredentials(ctx context.Context, storageID uuid.UUID) (bool, error) {
	secrets, err := m.repo.ListTargetSecrets(ctx, storageID)
	if err != nil {
		return false, err
	}
	accessKey, secretKey, err := OpenSecrets(m.secret, secrets)
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(accessKey) != "" && strings.TrimSpace(secretKey) != "", nil
}

// SealSecrets encrypts the non-empty credentials of a destination.
func SealSecrets(secret string, storageID uuid.UUID, accessKey, secretKey string) ([]Secret, error) {
	pairs := [][2]string{
		{secretKeyAccessKey, accessKey},
		{secretKeySecretKey, secretKey},
	}
	sealed := make([]Secret, 0, len(pairs))
	for _, pair := range pairs {
		if strings.TrimSpace(pair[1]) == "" {
			continue
		}
		ciphertext, err := providers.SealSecret(secret, pair[1])
		if err != nil {
			return nil, fmt.Errorf("storages: seal %s: %w", pair[0], err)
		}
		sealed = append(sealed, Secret{
			TargetID:   storageID,
			Key:        pair[0],
			Ciphertext: ciphertext,
		})
	}
	return sealed, nil
}

// OpenSecrets decrypts the stored credentials of a destination.
func OpenSecrets(secret string, secrets []Secret) (string, string, error) {
	var accessKey, secretKey string
	for _, item := range secrets {
		value, err := providers.OpenSecret(secret, item.Ciphertext)
		if err != nil {
			return "", "", fmt.Errorf("storages: open %s: %w", item.Key, err)
		}
		switch item.Key {
		case secretKeyAccessKey:
			accessKey = value
		case secretKeySecretKey:
			secretKey = value
		}
	}
	return accessKey, secretKey, nil
}

func applyRequest(target *Storage, req Request) error {
	if name := strings.TrimSpace(req.Name); name != "" {
		target.Name = name
	}
	if kind := strings.ToLower(strings.TrimSpace(req.Kind)); kind != "" {
		switch Kind(kind) {
		case KindS3, KindLocal:
			target.Kind = Kind(kind)
		default:
			return fmt.Errorf("%w: unsupported storage kind %q (supported: s3, local)", ErrValidation, req.Kind)
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
	if target.Kind == KindS3 {
		if strings.TrimSpace(target.Endpoint) == "" {
			return fmt.Errorf("%w: endpoint is required for an s3 storage", ErrValidation)
		}
		if strings.TrimSpace(target.Bucket) == "" {
			return fmt.Errorf("%w: bucket is required for an s3 storage", ErrValidation)
		}
	}
	return nil
}

func requestCarriesCredentials(req Request) bool {
	return strings.TrimSpace(req.AccessKey) != "" && strings.TrimSpace(req.SecretKey) != ""
}

func s3CredentialsRequired() error {
	return fmt.Errorf("%w: s3 storages need both an access key and a secret key", ErrValidation)
}

// probe writes, reads back and deletes a small object so the check proves the
// destination can hold and return a backup.
func probe(ctx context.Context, store ObjectStore) error {
	key := ".gotham-probe-" + uuid.NewString()
	location, err := store.Put(ctx, key, strings.NewReader(probePayload), int64(len(probePayload)))
	if err != nil {
		return fmt.Errorf("%w: the storage is not writable: %v", ErrValidation, err)
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
		return fmt.Errorf("%w: the storage cannot read a backup back: %v", ErrValidation, readErr)
	}
	if deleteErr != nil {
		return fmt.Errorf("%w: the storage does not allow deleting a backup: %v", ErrValidation, deleteErr)
	}
	return nil
}

// connectionFailure keeps our own actionable probe/validation messages but
// never echoes a raw dial or DNS error back to the caller; the full error goes
// to the log instead.
func (m *Manager) connectionFailure(storageID uuid.UUID, err error) Check {
	switch {
	case errors.Is(err, netguard.ErrDenied):
		return Check{OK: false, Message: "storage endpoint is not allowed"}
	case errors.Is(err, ErrValidation):
		return Check{OK: false, Message: boundedDiag(err.Error())}
	default:
		m.logger.Warn("storages: connection test failed",
			"storage_id", storageID.String(), "error", err)
		return Check{OK: false, Message: "could not connect to the storage endpoint"}
	}
}

const diagLimit = 600

func boundedDiag(diag string) string {
	diag = strings.TrimSpace(strings.ReplaceAll(diag, "\x00", ""))
	if len(diag) <= diagLimit {
		return diag
	}
	return "…" + diag[len(diag)-diagLimit:]
}
