package storages

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

type fakeRepository struct {
	byID        map[uuid.UUID]Storage
	secrets     map[uuid.UUID][]Secret
	usage       map[uuid.UUID]Usage
	scheduleIDs []uuid.UUID
	backupCount int64
}

func newFakeRepository() *fakeRepository {
	return &fakeRepository{
		byID:    make(map[uuid.UUID]Storage),
		secrets: make(map[uuid.UUID][]Secret),
		usage:   make(map[uuid.UUID]Usage),
	}
}

var _ Repository = (*fakeRepository)(nil)

func (r *fakeRepository) EnsureBuiltinTarget(_ context.Context, userID uuid.UUID) (Storage, error) {
	for _, item := range r.byID {
		if item.UserID == userID && item.Builtin {
			return item, nil
		}
	}
	item := Storage{
		ID:      uuid.New(),
		UserID:  userID,
		Name:    BuiltinName,
		Kind:    KindLocal,
		Builtin: true,
	}
	r.byID[item.ID] = item
	return item, nil
}

func (r *fakeRepository) GetBuiltinTarget(ctx context.Context, userID uuid.UUID) (Storage, error) {
	for _, item := range r.byID {
		if item.UserID == userID && item.Builtin {
			return item, nil
		}
	}
	return Storage{}, ErrNotFound
}

func (r *fakeRepository) GetBackupTarget(_ context.Context, id uuid.UUID) (Storage, error) {
	item, ok := r.byID[id]
	if !ok {
		return Storage{}, ErrNotFound
	}
	return item, nil
}

func (r *fakeRepository) ListBackupTargetsByUser(_ context.Context, userID uuid.UUID) ([]Storage, error) {
	items := make([]Storage, 0, len(r.byID))
	for _, item := range r.byID {
		if item.UserID == userID {
			items = append(items, item)
		}
	}
	return items, nil
}

func (r *fakeRepository) CreateBackupTargetWithSecrets(_ context.Context, target Storage, secrets []Secret) (Storage, error) {
	r.byID[target.ID] = target
	r.secrets[target.ID] = secrets
	return target, nil
}

func (r *fakeRepository) UpdateBackupTargetWithSecrets(_ context.Context, target Storage, secrets []Secret) (Storage, error) {
	if _, ok := r.byID[target.ID]; !ok {
		return Storage{}, ErrNotFound
	}
	r.byID[target.ID] = target
	if len(secrets) > 0 {
		r.secrets[target.ID] = secrets
	}
	return target, nil
}

func (r *fakeRepository) DeleteGuarded(_ context.Context, id, userID uuid.UUID, confirm bool) (Storage, []uuid.UUID, int64, bool, error) {
	item, ok := r.byID[id]
	if !ok || item.UserID != userID {
		return Storage{}, nil, 0, false, ErrNotFound
	}
	if item.Builtin {
		return item, nil, 0, false, nil
	}
	if len(r.scheduleIDs) > 0 {
		return item, r.scheduleIDs, 0, false, nil
	}
	if r.backupCount > 0 && !confirm {
		return item, nil, r.backupCount, false, nil
	}
	delete(r.byID, id)
	delete(r.secrets, id)
	return item, nil, r.backupCount, true, nil
}

func (r *fakeRepository) DeleteBackupTarget(_ context.Context, id, userID uuid.UUID) (Storage, error) {
	item, ok := r.byID[id]
	if !ok || item.UserID != userID {
		return Storage{}, ErrNotFound
	}
	delete(r.byID, id)
	delete(r.secrets, id)
	return item, nil
}

func (r *fakeRepository) CountBackupsForTarget(context.Context, uuid.UUID) (int64, error) {
	return r.backupCount, nil
}

func (r *fakeRepository) ListBackupScheduleIDsForTarget(context.Context, uuid.UUID) ([]uuid.UUID, error) {
	return r.scheduleIDs, nil
}

func (r *fakeRepository) MarkBackupsProviderRemoved(context.Context, uuid.UUID) error { return nil }

func (r *fakeRepository) BackupUsageByTarget(context.Context, uuid.UUID) (map[uuid.UUID]Usage, error) {
	return r.usage, nil
}

func (r *fakeRepository) ListTargetSecrets(_ context.Context, id uuid.UUID) ([]Secret, error) {
	return r.secrets[id], nil
}

type fakeBackend struct {
	puts, gets, deletes int
	putErr, getErr      error
	deleteErr           error
	kind                string
}

func (b *fakeBackend) Kind() string { return b.kind }

func (b *fakeBackend) Put(_ context.Context, key string, data io.Reader, size int64) (string, error) {
	if b.putErr != nil {
		return "", b.putErr
	}
	if _, err := io.Copy(io.Discard, data); err != nil {
		return "", err
	}
	b.puts++
	return "mem://" + key, nil
}

func (b *fakeBackend) Get(_ context.Context, location string) (io.ReadCloser, error) {
	if b.getErr != nil {
		return nil, b.getErr
	}
	b.gets++
	return io.NopCloser(strings.NewReader(probePayload)), nil
}

func (b *fakeBackend) Stat(context.Context, string) (int64, error) {
	return int64(len(probePayload)), nil
}

func (b *fakeBackend) Delete(context.Context, string) error {
	if b.deleteErr != nil {
		return b.deleteErr
	}
	b.deletes++
	return nil
}

type fakeBackends struct {
	backend *fakeBackend
	openErr error
}

func (f fakeBackends) Open(context.Context, Storage, Credentials) (ObjectStore, error) {
	if f.openErr != nil {
		return nil, f.openErr
	}
	return f.backend, nil
}

func newTestManager(t *testing.T, repo *fakeRepository) (*Manager, *fakeBackend) {
	t.Helper()
	backend := &fakeBackend{kind: string(KindS3)}
	return New(Config{
		Repository: repo,
		Secret:     "0123456789abcdef0123456789abcdef",
		LocalDir:   t.TempDir(),
		Backends:   fakeBackends{backend: backend},
	}), backend
}

func TestListEnsuresBuiltinAndAppliesUsage(t *testing.T) {
	repo := newFakeRepository()
	manager, _ := newTestManager(t, repo)
	userID := uuid.New()

	items, err := manager.List(context.Background(), userID)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(items) != 1 || !items[0].Builtin || items[0].Kind != KindLocal {
		t.Fatalf("items = %+v, want one built-in local storage", items)
	}
	repo.usage[items[0].ID] = Usage{BackupCount: 2, TotalSize: 42}
	items, err = manager.List(context.Background(), userID)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if items[0].Usage.BackupCount != 2 || items[0].Usage.TotalSize != 42 {
		t.Errorf("usage = %+v, want 2 backups / 42 bytes", items[0].Usage)
	}
	if items[0].Usage.TotalSpace == 0 {
		t.Error("local storage should report the disk size")
	}
}

func TestCreateSealsSecretsAndRejectsMissingKeys(t *testing.T) {
	repo := newFakeRepository()
	manager, _ := newTestManager(t, repo)
	userID := uuid.New()

	if _, err := manager.Create(context.Background(), userID, Request{
		Name: "bucket", Kind: "s3", Endpoint: "https://s3.example.test", Bucket: "b",
	}); !errors.Is(err, ErrValidation) {
		t.Fatalf("missing keys err = %v, want ErrValidation", err)
	}

	created, err := manager.Create(context.Background(), userID, Request{
		Name: "bucket", Kind: "s3", Endpoint: "https://s3.example.test", Bucket: "b",
		AccessKey: "ak", SecretKey: "sk",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	secrets := repo.secrets[created.ID]
	if len(secrets) != 2 {
		t.Fatalf("sealed secrets = %d, want 2", len(secrets))
	}
	for _, secret := range secrets {
		if secret.Ciphertext == "ak" || secret.Ciphertext == "sk" {
			t.Error("credentials were stored in plaintext")
		}
	}
	accessKey, secretKey, err := OpenSecrets("0123456789abcdef0123456789abcdef", secrets)
	if err != nil || accessKey != "ak" || secretKey != "sk" {
		t.Fatalf("OpenSecrets = %q/%q (%v)", accessKey, secretKey, err)
	}
}

func TestUpdateKeepsStoredCredentialsWhenKeysAreBlank(t *testing.T) {
	repo := newFakeRepository()
	manager, _ := newTestManager(t, repo)
	userID := uuid.New()

	created, err := manager.Create(context.Background(), userID, Request{
		Name: "bucket", Kind: "s3", Endpoint: "https://s3.example.test", Bucket: "b",
		AccessKey: "ak", SecretKey: "sk",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	updated, err := manager.Update(context.Background(), userID, created.ID, Request{Name: "renamed"})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.Name != "renamed" {
		t.Errorf("name = %q, want renamed", updated.Name)
	}
	if len(repo.secrets[created.ID]) != 2 {
		t.Error("a blank-key update dropped the stored credentials")
	}
}

func TestDeleteRejectsSchedulesAndUnconfirmedBackups(t *testing.T) {
	repo := newFakeRepository()
	manager, _ := newTestManager(t, repo)
	userID := uuid.New()

	created, err := manager.Create(context.Background(), userID, Request{
		Name: "bucket", Kind: "s3", Endpoint: "https://s3.example.test", Bucket: "b",
		AccessKey: "ak", SecretKey: "sk",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	repo.scheduleIDs = []uuid.UUID{uuid.New()}
	if err := manager.Delete(context.Background(), userID, created.ID, true); !errors.Is(err, ErrInUse) {
		t.Fatalf("schedule guard err = %v, want ErrInUse", err)
	}
	repo.scheduleIDs = nil
	repo.backupCount = 1
	if err := manager.Delete(context.Background(), userID, created.ID, false); !errors.Is(err, ErrHasBackups) {
		t.Fatalf("backup guard err = %v, want ErrHasBackups", err)
	}
	if err := manager.Delete(context.Background(), userID, created.ID, true); err != nil {
		t.Fatalf("confirmed delete: %v", err)
	}
}

func TestTestProbesOneRoundTrip(t *testing.T) {
	repo := newFakeRepository()
	manager, backend := newTestManager(t, repo)
	userID := uuid.New()

	created, err := manager.Create(context.Background(), userID, Request{
		Name: "bucket", Kind: "s3", Endpoint: "https://s3.example.test", Bucket: "b",
		AccessKey: "ak", SecretKey: "sk",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	check, err := manager.Test(context.Background(), userID, created.ID)
	if err != nil {
		t.Fatalf("Test: %v", err)
	}
	if !check.OK {
		t.Fatalf("check = %+v, want ok", check)
	}
	if backend.puts != 1 || backend.gets != 1 || backend.deletes != 1 {
		t.Errorf("probe = %d/%d/%d, want one each", backend.puts, backend.gets, backend.deletes)
	}

	backend.putErr = errors.New("access denied")
	check, err = manager.Test(context.Background(), userID, created.ID)
	if err != nil {
		t.Fatalf("Test: %v", err)
	}
	if check.OK || !strings.Contains(check.Message, "not writable") {
		t.Errorf("check = %+v, want a not-writable failure", check)
	}
}

func TestRoutesExposeStoragesWithoutSecrets(t *testing.T) {
	repo := newFakeRepository()
	manager, _ := newTestManager(t, repo)
	userID := uuid.New()

	r := chi.NewRouter()
	Mount(r, func(next http.Handler) http.Handler { return next }, func(context.Context) (uuid.UUID, bool) {
		return userID, true
	}, manager)

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/storages",
		strings.NewReader(`{"name":"bucket","kind":"s3","endpoint":"https://s3.example.test","bucket":"b","access_key":"ak","secret_key":"sk"}`)))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create = %d, want 201 (body %s)", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, forbidden := range []string{"access_key\":", "secret_key\":", "ciphertext"} {
		if strings.Contains(body, forbidden) {
			t.Errorf("response leaked %q: %s", forbidden, body)
		}
	}
	var envelope struct {
		Storage struct {
			ID             string `json:"id"`
			HasCredentials bool   `json:"has_credentials"`
		} `json:"storage"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !envelope.Storage.HasCredentials {
		t.Error("has_credentials should be true for an s3 storage")
	}

	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/storages", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"storages"`) {
		t.Fatalf("list = %d (body %s)", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, "/v1/storages/"+envelope.Storage.ID, nil))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete = %d, want 204 (body %s)", rec.Code, rec.Body.String())
	}
}

func TestStorageAccessIsOwnerScoped(t *testing.T) {
	repo := newFakeRepository()
	manager, _ := newTestManager(t, repo)
	ownerID, stranger := uuid.New(), uuid.New()

	created, err := manager.Create(context.Background(), ownerID, Request{
		Name: "bucket", Kind: "s3", Endpoint: "https://s3.example.test", Bucket: "b",
		AccessKey: "ak", SecretKey: "sk",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if _, err := manager.Update(context.Background(), stranger, created.ID, Request{Name: "stolen"}); !errors.Is(err, ErrNotFound) {
		t.Errorf("cross-owner update err = %v, want ErrNotFound", err)
	}
	if err := manager.Delete(context.Background(), stranger, created.ID, true); !errors.Is(err, ErrNotFound) {
		t.Errorf("cross-owner delete err = %v, want ErrNotFound", err)
	}
	if _, err := manager.Test(context.Background(), stranger, created.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("cross-owner test err = %v, want ErrNotFound", err)
	}
	if _, err := manager.Resolve(context.Background(), stranger, created.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("cross-owner resolve err = %v, want ErrNotFound", err)
	}
	// A zero owner must not fall back to an unchecked lookup.
	if _, err := manager.Resolve(context.Background(), uuid.Nil, created.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("zero-owner resolve err = %v, want ErrNotFound", err)
	}
	if got, err := manager.ResolveAny(context.Background(), created.ID); err != nil || got.ID != created.ID {
		t.Errorf("ResolveAny = %+v, %v, want the row", got, err)
	}
}

func withLookup(t *testing.T, fn func(context.Context, string) ([]net.IPAddr, error)) {
	t.Helper()
	original := lookupHost
	lookupHost = fn
	t.Cleanup(func() { lookupHost = original })
}

func TestCreateRejectsReservedEndpoints(t *testing.T) {
	repo := newFakeRepository()
	manager, _ := newTestManager(t, repo)
	userID := uuid.New()
	withLookup(t, func(context.Context, string) ([]net.IPAddr, error) {
		return nil, errors.New("no such host")
	})

	for _, endpoint := range []string{
		"http://169.254.169.254",
		"http://[fe80::1]:9000",
		"http://100.64.0.1",
	} {
		if _, err := manager.Create(context.Background(), userID, Request{
			Name: "bad", Kind: "s3", Endpoint: endpoint, Bucket: "b",
			AccessKey: "ak", SecretKey: "sk",
		}); !errors.Is(err, ErrValidation) {
			t.Errorf("Create(%q) err = %v, want ErrValidation", endpoint, err)
		}
	}
}

func TestCreateChecksTheResolvedAddress(t *testing.T) {
	repo := newFakeRepository()
	manager, _ := newTestManager(t, repo)
	userID := uuid.New()
	withLookup(t, func(context.Context, string) ([]net.IPAddr, error) {
		return []net.IPAddr{{IP: net.ParseIP("169.254.169.254")}}, nil
	})

	if _, err := manager.Create(context.Background(), userID, Request{
		Name: "rebind", Kind: "s3", Endpoint: "https://metadata.example.test", Bucket: "b",
		AccessKey: "ak", SecretKey: "sk",
	}); !errors.Is(err, ErrValidation) {
		t.Fatalf("Create with a reserved DNS answer err = %v, want ErrValidation", err)
	}
}

func TestPrivateMinIOEndpointIsAllowed(t *testing.T) {
	repo := newFakeRepository()
	manager, _ := newTestManager(t, repo)
	withLookup(t, func(context.Context, string) ([]net.IPAddr, error) {
		return []net.IPAddr{{IP: net.ParseIP("10.0.0.12")}}, nil
	})

	if _, err := manager.Create(context.Background(), uuid.New(), Request{
		Name: "minio", Kind: "s3", Endpoint: "http://minio.gotham.internal:9000", Bucket: "b",
		AccessKey: "ak", SecretKey: "sk",
	}); err != nil {
		t.Fatalf("Create with a private endpoint: %v", err)
	}
}

func TestTestHidesRawDialErrors(t *testing.T) {
	repo := newFakeRepository()
	backend := &fakeBackend{kind: string(KindS3)}
	manager := New(Config{
		Repository: repo,
		Secret:     "0123456789abcdef0123456789abcdef",
		LocalDir:   t.TempDir(),
		Backends:   fakeBackends{backend: backend, openErr: errors.New("dial tcp 10.0.0.9:9000: connect: connection refused")},
	})
	userID := uuid.New()
	created, err := manager.Create(context.Background(), userID, Request{
		Name: "bucket", Kind: "s3", Endpoint: "http://127.0.0.1:9000", Bucket: "b",
		AccessKey: "ak", SecretKey: "sk",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	check, err := manager.Test(context.Background(), userID, created.ID)
	if err != nil {
		t.Fatalf("Test: %v", err)
	}
	if check.OK {
		t.Fatalf("check = %+v, want failure", check)
	}
	if strings.Contains(check.Message, "dial tcp") || strings.Contains(check.Message, "10.0.0.9") {
		t.Fatalf("probe leaked raw dial text: %s", check.Message)
	}
	if check.Message != "could not connect to the storage endpoint" {
		t.Errorf("message = %q, want the generic connection failure", check.Message)
	}
}

func TestProbeFailureMessageNeverCarriesTheSecret(t *testing.T) {
	repo := newFakeRepository()
	manager, backend := newTestManager(t, repo)
	userID := uuid.New()
	withLookup(t, func(context.Context, string) ([]net.IPAddr, error) {
		return nil, errors.New("no such host")
	})

	created, err := manager.Create(context.Background(), userID, Request{
		Name: "bucket", Kind: "s3", Endpoint: "https://s3.example.test", Bucket: "b",
		AccessKey: "ak", SecretKey: "super-secret-value",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	backend.putErr = errors.New("access denied")
	check, err := manager.Test(context.Background(), userID, created.ID)
	if err != nil {
		t.Fatalf("Test: %v", err)
	}
	if check.OK {
		t.Fatalf("check = %+v, want failure", check)
	}
	if strings.Contains(check.Message, "super-secret-value") {
		t.Fatalf("probe message leaked the secret: %s", check.Message)
	}
}
