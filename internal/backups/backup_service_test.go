package backups

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestBackupStoresCompressedArtifact(t *testing.T) {
	fixture := newBackupFixture(t)

	backup := fixture.queueBackup(t)
	if backup.Status != BackupCompleted {
		t.Fatalf("status = %q (%s), want completed", backup.Status, backup.Error)
	}
	if backup.Type != BackupManual {
		t.Errorf("type = %q, want manual", backup.Type)
	}
	if !strings.HasPrefix(backup.Location, locationFilePrefix) {
		t.Errorf("location = %q, want a file:// uri", backup.Location)
	}
	if backup.Size <= 0 {
		t.Errorf("size = %d, want the compressed length", backup.Size)
	}
	if backup.FinishedAt.IsZero() {
		t.Error("finished_at must be set on a terminal run")
	}
	if backup.ContainerID == "" {
		t.Error("the temporary container id must be recorded")
	}
	if backup.ResourceRef != databaseRef(fixture.databaseID) {
		t.Errorf("resource = %s, want the database", backup.ResourceRef)
	}

	if got := string(readArtifact(t, backup.Location)); got != dumpPayload {
		t.Errorf("artifact = %q, want %q", got, dumpPayload)
	}
	info, err := os.Stat(strings.TrimPrefix(backup.Location, locationFilePrefix))
	if err != nil {
		t.Fatalf("stat artifact: %v", err)
	}
	if info.Size() != backup.Size {
		t.Errorf("file is %d bytes, row says %d", info.Size(), backup.Size)
	}
}

func TestBackupFailureIsRecorded(t *testing.T) {
	fixture := newBackupFixture(t)
	fixture.source.captureErr = errors.New("databases: temporary container failed with status 7")

	backup := fixture.queueBackup(t)
	if backup.Status != BackupFailed {
		t.Fatalf("status = %q, want failed", backup.Status)
	}
	if backup.Location != "" {
		t.Errorf("a failed run must not record a location, got %q", backup.Location)
	}
	if !strings.Contains(backup.Error, "status 7") {
		t.Errorf("error = %q, want the driver failure", backup.Error)
	}
}

func TestBackupRejectsSecondJobWhileRunning(t *testing.T) {
	fixture := newBackupFixture(t)
	if !fixture.manager.claim(fixture.databaseID) {
		t.Fatal("could not take the claim")
	}
	defer fixture.manager.release(fixture.databaseID)

	_, err := fixture.manager.CreateBackup(context.Background(), fixture.userID, CreateBackupRequest{
		Resource: databaseRef(fixture.databaseID),
	})
	if !errors.Is(err, ErrBackupInFlight) {
		t.Fatalf("err = %v, want ErrBackupInFlight", err)
	}
}

func TestCreateBackupRejectsUnsupportedResource(t *testing.T) {
	fixture := newBackupFixture(t)
	_, err := fixture.manager.CreateBackup(context.Background(), fixture.userID, CreateBackupRequest{
		Resource: ResourceRef{Type: ResourceService, ID: uuid.New()},
	})
	if !errors.Is(err, ErrUnsupportedResource) {
		t.Fatalf("err = %v, want ErrUnsupportedResource", err)
	}
}

func TestCreateBackupPassesThePauseChoice(t *testing.T) {
	for name, request := range map[string]*bool{
		"default": nil,
		"pause":   boolPointer(true),
		"no stop": boolPointer(false),
	} {
		t.Run(name, func(t *testing.T) {
			fixture := newBackupFixture(t)
			queued, err := fixture.manager.CreateBackup(context.Background(), fixture.userID, CreateBackupRequest{
				Resource:          databaseRef(fixture.databaseID),
				PauseDuringBackup: request,
			})
			if err != nil {
				t.Fatalf("CreateBackup: %v", err)
			}
			want := request == nil || *request
			if queued.PauseDuringBackup != want {
				t.Fatalf("run row pause = %v, want %v", queued.PauseDuringBackup, want)
			}
			fixture.waitBackup(t, queued.ID)
			if got := fixture.source.captures[0].Pause; got != want {
				t.Fatalf("pause = %v, want %v", got, want)
			}
		})
	}
}

func TestCreateBackupValidatesTheResourceBeforeQueueing(t *testing.T) {
	fixture := newBackupFixture(t)
	fixture.source.validateErr = fmt.Errorf("%w: this application has no persistent storage", ErrValidation)
	_, err := fixture.manager.CreateBackup(context.Background(), fixture.userID, CreateBackupRequest{
		Resource: databaseRef(fixture.databaseID),
	})
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("err = %v, want ErrValidation", err)
	}
	if fixture.backups.countBackups() != 0 {
		t.Fatal("a rejected run must not leave a backup row")
	}
}

func TestCreateBackupUsesBuiltinProviderByDefault(t *testing.T) {
	fixture := newBackupFixture(t)
	builtin, err := fixture.manager.ListProviders(context.Background(), fixture.userID)
	if err != nil {
		t.Fatalf("ListProviders: %v", err)
	}
	if len(builtin) != 1 || !builtin[0].Builtin || builtin[0].Kind != TargetLocal {
		t.Fatalf("providers = %+v, want the built-in Local disk provider", builtin)
	}

	backup := fixture.queueBackup(t)
	if backup.TargetID != builtin[0].ID {
		t.Errorf("target = %s, want the built-in provider %s", backup.TargetID, builtin[0].ID)
	}
}

func TestBuiltinProviderKeepsItsIdentity(t *testing.T) {
	fixture := newBackupFixture(t)
	ctx := context.Background()
	providers, err := fixture.manager.ListProviders(ctx, fixture.userID)
	if err != nil {
		t.Fatalf("ListProviders: %v", err)
	}
	builtin := providers[0]

	// Renaming it would free "Local disk" for the seeder and grow a duplicate.
	if _, err := fixture.manager.UpdateProvider(ctx, fixture.userID, builtin.ID, ProviderRequest{Name: "Renamed"}); !errors.Is(err, ErrBuiltinProvider) {
		t.Fatalf("rename err = %v, want ErrBuiltinProvider", err)
	}
	// The same name is still allowed and changes nothing.
	if _, err := fixture.manager.UpdateProvider(ctx, fixture.userID, builtin.ID, ProviderRequest{Name: BuiltinProviderName}); err != nil {
		t.Fatalf("same-name update: %v", err)
	}

	for i := 0; i < 3; i++ {
		listed, err := fixture.manager.ListProviders(ctx, fixture.userID)
		if err != nil {
			t.Fatalf("ListProviders %d: %v", i, err)
		}
		if len(listed) != 1 || listed[0].ID != builtin.ID || !listed[0].Builtin {
			t.Fatalf("listing %d = %+v, want exactly the builtin", i, listed)
		}
	}
	// Seeding is keyed on the flag: even a row renamed behind the API cannot
	// make a second builtin appear.
	renamed := builtin
	renamed.Name = "Renamed locally"
	if _, err := fixture.backups.UpdateBackupTarget(ctx, renamed); err != nil {
		t.Fatalf("rename in the repository: %v", err)
	}
	first, err := fixture.backups.EnsureBuiltinTarget(ctx, fixture.userID)
	if err != nil {
		t.Fatalf("EnsureBuiltinTarget: %v", err)
	}
	second, err := fixture.backups.EnsureBuiltinTarget(ctx, fixture.userID)
	if err != nil {
		t.Fatalf("EnsureBuiltinTarget(again): %v", err)
	}
	if first.ID != builtin.ID || second.ID != builtin.ID {
		t.Fatalf("seeded %s then %s, want the existing %s", first.ID, second.ID, builtin.ID)
	}
	if got := fixture.backups.countTargets(); got != 1 {
		t.Fatalf("providers = %d, want 1: seeding duplicated the builtin", got)
	}
}

func TestBackupLabelsAResourceOwnedByAMember(t *testing.T) {
	fixture := newBackupFixture(t)
	fixture.source.mu.Lock()
	fixture.source.owner = uuid.New()
	memberOwner := fixture.source.owner
	fixture.source.mu.Unlock()

	backup := fixture.queueBackup(t)
	if backup.TargetID == uuid.Nil {
		t.Fatal("a member-owned resource produced an unlabelled artifact")
	}
	stored, err := fixture.manager.GetBackup(context.Background(), fixture.userID, backup.ID)
	if err != nil {
		t.Fatalf("GetBackup: %v", err)
	}
	if stored.ProviderName != BuiltinProviderName {
		t.Errorf("provider name = %q, want %q", stored.ProviderName, BuiltinProviderName)
	}
	target, err := fixture.backups.GetBackupTarget(context.Background(), backup.TargetID)
	if err != nil {
		t.Fatalf("GetBackupTarget: %v", err)
	}
	if target.UserID != memberOwner || !target.Builtin || target.Kind != TargetLocal {
		t.Errorf("target = %+v, want the member's own builtin", target)
	}
}

func TestDeleteProviderRejectsBuiltin(t *testing.T) {
	fixture := newBackupFixture(t)
	providers, err := fixture.manager.ListProviders(context.Background(), fixture.userID)
	if err != nil {
		t.Fatalf("ListProviders: %v", err)
	}
	if err := fixture.manager.DeleteProvider(context.Background(), fixture.userID, providers[0].ID, false); !errors.Is(err, ErrBuiltinProvider) {
		t.Fatalf("err = %v, want ErrBuiltinProvider", err)
	}
	if _, err := fixture.manager.UpdateProvider(context.Background(), fixture.userID, providers[0].ID, ProviderRequest{
		Kind: "s3", Endpoint: "https://s3.example.com", Bucket: "b",
	}); !errors.Is(err, ErrBuiltinProvider) {
		t.Fatalf("kind switch err = %v, want ErrBuiltinProvider", err)
	}
}

func TestListRestoresReturnsDurableHistory(t *testing.T) {
	fixture := newBackupFixture(t)
	ref := databaseRef(fixture.databaseID)
	older := fixture.backups.seedRestore(Restore{
		ResourceRef: ref,
		BackupID:    uuid.New(),
		Status:      RestoreFailed,
		Error:       "interrupted",
		CreatedAt:   time.Now().UTC().Add(-time.Hour),
	})
	newer := fixture.backups.seedRestore(Restore{
		ResourceRef: ref,
		BackupID:    uuid.New(),
		Status:      RestoreCompleted,
		CreatedAt:   time.Now().UTC(),
	})
	fixture.backups.seedRestore(Restore{
		ResourceRef: databaseRef(uuid.New()),
		BackupID:    uuid.New(),
		Status:      RestoreCompleted,
		CreatedAt:   time.Now().UTC(),
	})

	all, err := fixture.manager.ListRestores(context.Background(), fixture.userID, RestoreFilter{Resource: &ref})
	if err != nil {
		t.Fatalf("ListRestores: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("restores = %d, want 2", len(all))
	}
	if all[0].ID != newer.ID || all[1].ID != older.ID {
		t.Errorf("order = %s, %s; want %s, %s", all[0].ID, all[1].ID, newer.ID, older.ID)
	}

	limited, err := fixture.manager.ListRestores(context.Background(), fixture.userID, RestoreFilter{Resource: &ref, Limit: 1})
	if err != nil {
		t.Fatalf("ListRestores limited: %v", err)
	}
	if len(limited) != 1 || limited[0].ID != newer.ID {
		t.Errorf("limited = %+v, want the newest row only", limited)
	}
}

func TestBackupUsesObjectStoreSeam(t *testing.T) {
	fixture := newBackupFixture(t)
	objects := newFakeObjectStore()
	fixture.manager.objects = objects

	backup := fixture.queueBackup(t)
	if backup.Status != BackupCompleted {
		t.Fatalf("status = %q (%s)", backup.Status, backup.Error)
	}
	if !strings.HasPrefix(backup.Location, locationS3Prefix) {
		t.Errorf("location = %q, want an s3 uri", backup.Location)
	}
	wantKey := "databases/" + fixture.databaseID.String() + "/" + backup.ID.String() + ".dump.gz"
	if len(objects.puts) != 1 || objects.puts[0] != wantKey {
		t.Errorf("puts = %v, want [%s]", objects.puts, wantKey)
	}
}

func TestRestoreAppliesArtifactThroughTheSource(t *testing.T) {
	fixture := newBackupFixture(t)
	backup := fixture.queueBackup(t)

	result, err := fixture.manager.RestoreBackup(context.Background(), fixture.userID, backup.ID)
	if err != nil {
		t.Fatalf("RestoreBackup: %v", err)
	}
	if result.Status != BackupRunning || result.BackupID != backup.ID {
		t.Errorf("result = %+v, want the queued restore", result)
	}
	waitFor(t, "the restore to finish", func() bool {
		restore, ok := fixture.backups.getRestore(result.RestoreID)
		return ok && restore.Status != RestoreRunning
	})
	restore, _ := fixture.backups.getRestore(result.RestoreID)
	if restore.Status != RestoreCompleted {
		t.Errorf("restore status = %q (%s), want completed", restore.Status, restore.Error)
	}
	if restore.ResourceRef != databaseRef(fixture.databaseID) {
		t.Errorf("restore resource = %s, want the database", restore.ResourceRef)
	}

	fixture.source.mu.Lock()
	restores := append([]RestoreRequest(nil), fixture.source.restores...)
	fixture.source.mu.Unlock()
	if len(restores) != 1 || restores[0].BackupID != backup.ID {
		t.Errorf("source restores = %+v, want one for the backup", restores)
	}
}

func TestRestoreFailureIsRecorded(t *testing.T) {
	fixture := newBackupFixture(t)
	backup := fixture.queueBackup(t)
	fixture.source.restoreErr = errors.New("databases: restore job failed")

	result, err := fixture.manager.RestoreBackup(context.Background(), fixture.userID, backup.ID)
	if err != nil {
		t.Fatalf("RestoreBackup: %v", err)
	}
	waitFor(t, "the restore to fail", func() bool {
		restore, ok := fixture.backups.getRestore(result.RestoreID)
		return ok && restore.Status != RestoreRunning
	})
	restore, _ := fixture.backups.getRestore(result.RestoreID)
	if restore.Status != RestoreFailed || restore.Error == "" {
		t.Errorf("restore = %+v, want a recorded failure", restore)
	}
}

func TestRestoreRejectsIncompleteBackup(t *testing.T) {
	fixture := newBackupFixture(t)
	backup := fixture.backups.seedBackup(Backup{
		ResourceRef: databaseRef(fixture.databaseID),
		Status:      BackupFailed,
		Location:    locationFilePrefix + "/tmp/missing.gz",
	})
	_, err := fixture.manager.RestoreBackup(context.Background(), fixture.userID, backup.ID)
	if !errors.Is(err, ErrBackupNotCompleted) {
		t.Fatalf("err = %v, want ErrBackupNotCompleted", err)
	}
}

func TestDeleteBackupRemovesArtifactAndRow(t *testing.T) {
	fixture := newBackupFixture(t)
	backup := fixture.queueBackup(t)
	path := strings.TrimPrefix(backup.Location, locationFilePrefix)

	if err := fixture.manager.DeleteBackup(context.Background(), fixture.userID, backup.ID); err != nil {
		t.Fatalf("DeleteBackup: %v", err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("artifact still on disk: %v", err)
	}
	if _, ok := fixture.backups.getBackup(backup.ID); ok {
		t.Error("row still present after delete")
	}
	if err := fixture.manager.DeleteBackup(context.Background(), fixture.userID, backup.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("second delete err = %v, want ErrNotFound", err)
	}
}

func TestProviderCredentialsAreSealed(t *testing.T) {
	fixture := newBackupFixture(t)

	target, err := fixture.manager.CreateProvider(context.Background(), fixture.userID, ProviderRequest{
		Name:      "r2",
		Kind:      "s3",
		Endpoint:  "https://account.r2.cloudflarestorage.com",
		Region:    strPtr("auto"),
		Bucket:    "gotham-backups",
		Prefix:    strPtr("pg-orders/"),
		AccessKey: "R2AK7f3c9a2b51de84",
		SecretKey: "b7d41e90c3f5a28e6d0194bc7a3e52f0",
	})
	if err != nil {
		t.Fatalf("CreateProvider: %v", err)
	}

	secrets := fixture.backups.getSecrets(target.ID)
	if len(secrets) != 2 {
		t.Fatalf("stored %d secrets, want access and secret key", len(secrets))
	}
	for _, secret := range secrets {
		if secret.Ciphertext == "" {
			t.Errorf("%s stored empty", secret.Key)
		}
		if strings.Contains(secret.Ciphertext, "R2AK7f3c9a2b51de84") ||
			strings.Contains(secret.Ciphertext, "b7d41e90c3f5a28e6d0194bc7a3e52f0") {
			t.Errorf("%s is stored in the clear", secret.Key)
		}
	}

	accessKey, secretKey, err := openTargetSecrets(testSecret, secrets)
	if err != nil {
		t.Fatalf("openTargetSecrets: %v", err)
	}
	if accessKey != "R2AK7f3c9a2b51de84" || secretKey != "b7d41e90c3f5a28e6d0194bc7a3e52f0" {
		t.Errorf("opened %q / %q", accessKey, secretKey)
	}

	if _, err := fixture.manager.UpdateProvider(context.Background(), fixture.userID, target.ID, ProviderRequest{
		Name: "r2-renamed",
	}); err != nil {
		t.Fatalf("UpdateProvider: %v", err)
	}
	accessKey, _, err = openTargetSecrets(testSecret, fixture.backups.getSecrets(target.ID))
	if err != nil || accessKey != "R2AK7f3c9a2b51de84" {
		t.Errorf("stored access key was lost: %q, %v", accessKey, err)
	}
}

func TestProviderOwnershipAndValidation(t *testing.T) {
	fixture := newBackupFixture(t)
	stranger := uuid.New()

	if _, err := fixture.manager.CreateProvider(context.Background(), uuid.Nil, ProviderRequest{Name: "x", Kind: "local"}); !errors.Is(err, ErrNotFound) {
		t.Errorf("nil user err = %v, want ErrNotFound", err)
	}
	if _, err := fixture.manager.CreateProvider(context.Background(), fixture.userID, ProviderRequest{Name: "", Kind: "s3"}); !errors.Is(err, ErrValidation) {
		t.Errorf("missing name err = %v, want ErrValidation", err)
	}
	if _, err := fixture.manager.CreateProvider(context.Background(), fixture.userID, ProviderRequest{
		Name: "s3", Kind: "s3", Endpoint: "", Bucket: "",
	}); !errors.Is(err, ErrValidation) {
		t.Errorf("missing endpoint err = %v, want ErrValidation", err)
	}
	if _, err := fixture.manager.CreateProvider(context.Background(), fixture.userID, ProviderRequest{
		Name: "weird", Kind: "ftp",
	}); !errors.Is(err, ErrValidation) {
		t.Errorf("bad kind err = %v, want ErrValidation", err)
	}

	target := fixture.backups.seedTarget(BackupTarget{UserID: fixture.userID, Name: "mine", Kind: TargetS3,
		Endpoint: "http://minio:9000", Bucket: "b"})
	if _, err := fixture.manager.UpdateProvider(context.Background(), stranger, target.ID, ProviderRequest{Name: "stolen"}); !errors.Is(err, ErrNotFound) {
		t.Errorf("foreign update err = %v, want ErrNotFound", err)
	}
	if err := fixture.manager.DeleteProvider(context.Background(), stranger, target.ID, false); !errors.Is(err, ErrNotFound) {
		t.Errorf("foreign delete err = %v, want ErrNotFound", err)
	}
}

func TestUpdateProviderClearsOptionalFields(t *testing.T) {
	fixture := newBackupFixture(t)
	ctx := context.Background()
	target, err := fixture.manager.CreateProvider(ctx, fixture.userID, ProviderRequest{
		Name: "r2", Kind: "s3", Endpoint: "https://s3.example.com",
		Region: strPtr("auto"), Bucket: "b", Prefix: strPtr("pg/"),
		AccessKey: "ak", SecretKey: "sk",
	})
	if err != nil {
		t.Fatalf("CreateProvider: %v", err)
	}

	kept, err := fixture.manager.UpdateProvider(ctx, fixture.userID, target.ID, ProviderRequest{Name: "renamed"})
	if err != nil {
		t.Fatalf("UpdateProvider(keep): %v", err)
	}
	if kept.Region != "auto" || kept.Prefix != "pg/" {
		t.Errorf("region/prefix = %q/%q, want the stored values kept", kept.Region, kept.Prefix)
	}

	cleared, err := fixture.manager.UpdateProvider(ctx, fixture.userID, target.ID, ProviderRequest{
		Region: strPtr(""), Prefix: strPtr(""),
	})
	if err != nil {
		t.Fatalf("UpdateProvider(clear): %v", err)
	}
	if cleared.Region != "" || cleared.Prefix != "" {
		t.Errorf("region/prefix = %q/%q, want both cleared", cleared.Region, cleared.Prefix)
	}
}

func TestSchedulesLifecycle(t *testing.T) {
	fixture := newBackupFixture(t)
	ctx := context.Background()
	ref := databaseRef(fixture.databaseID)

	if _, err := fixture.manager.CreateSchedule(ctx, fixture.userID, ScheduleRequest{
		Resource: ref, Cron: "not a cron",
	}); !errors.Is(err, ErrValidation) {
		t.Errorf("invalid cron err = %v, want ErrValidation", err)
	}

	enabled := false
	schedule, err := fixture.manager.CreateSchedule(ctx, fixture.userID, ScheduleRequest{
		Resource: ref,
		Cron:     "0 2 * * *",
		Enabled:  &enabled,
	})
	if err != nil {
		t.Fatalf("CreateSchedule: %v", err)
	}
	if schedule.NextRunAt.IsZero() || schedule.NextRunAt.Before(time.Now()) {
		t.Errorf("next_run_at = %v, want a future instant", schedule.NextRunAt)
	}
	if schedule.Enabled {
		t.Error("enabled = true, want the requested false")
	}
	if schedule.ResourceRef != ref {
		t.Errorf("resource = %s, want the database", schedule.ResourceRef)
	}

	enabled = true
	updated, err := fixture.manager.UpdateSchedule(ctx, fixture.userID, schedule.ID, ScheduleRequest{
		Cron:    "0 */6 * * *",
		Enabled: &enabled,
	})
	if err != nil {
		t.Fatalf("UpdateSchedule: %v", err)
	}
	if updated.Cron != "0 */6 * * *" || !updated.Enabled {
		t.Errorf("schedule = %+v, want the new cron and enabled", updated)
	}

	list, err := fixture.manager.ListSchedules(ctx, fixture.userID, ref)
	if err != nil {
		t.Fatalf("ListSchedules: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("listed %d schedules, want 1", len(list))
	}

	if err := fixture.manager.DeleteSchedule(ctx, fixture.userID, schedule.ID); err != nil {
		t.Fatalf("DeleteSchedule: %v", err)
	}
	if _, err := fixture.manager.ListSchedules(ctx, fixture.userID, ref); err != nil {
		t.Fatalf("ListSchedules after delete: %v", err)
	}
}

func TestScheduleUpdateTargetClearSemantics(t *testing.T) {
	fixture := newBackupFixture(t)
	ctx := context.Background()
	ref := databaseRef(fixture.databaseID)
	s3 := fixture.backups.seedTarget(BackupTarget{UserID: fixture.userID, Name: "s3", Kind: TargetS3,
		Endpoint: "https://s3.example.com", Bucket: "b"})
	local := fixture.backups.seedTarget(BackupTarget{UserID: fixture.userID, Name: "local", Kind: TargetLocal})

	s3ID := s3.ID.String()
	schedule, err := fixture.manager.CreateSchedule(ctx, fixture.userID, ScheduleRequest{
		Resource: ref,
		Cron:     "0 2 * * *",
		TargetID: &s3ID,
	})
	if err != nil {
		t.Fatalf("CreateSchedule: %v", err)
	}
	if schedule.TargetID != s3.ID {
		t.Fatalf("created target = %s, want %s", schedule.TargetID, s3.ID)
	}

	kept, err := fixture.manager.UpdateSchedule(ctx, fixture.userID, schedule.ID, ScheduleRequest{
		Cron: "0 3 * * *",
	})
	if err != nil {
		t.Fatalf("update without target_id: %v", err)
	}
	if kept.TargetID != s3.ID {
		t.Errorf("target = %s, want the stored %s (absent must not clear)", kept.TargetID, s3.ID)
	}

	clear := ""
	cleared, err := fixture.manager.UpdateSchedule(ctx, fixture.userID, schedule.ID, ScheduleRequest{
		Cron:     "0 4 * * *",
		TargetID: &clear,
	})
	if err != nil {
		t.Fatalf("clear update: %v", err)
	}
	if cleared.TargetID != uuid.Nil {
		t.Errorf("target = %s, want cleared to no target", cleared.TargetID)
	}

	localID := local.ID.String()
	replaced, err := fixture.manager.UpdateSchedule(ctx, fixture.userID, schedule.ID, ScheduleRequest{
		Cron:     "0 5 * * *",
		TargetID: &localID,
	})
	if err != nil {
		t.Fatalf("replace update: %v", err)
	}
	if replaced.TargetID != local.ID {
		t.Errorf("target = %s, want %s", replaced.TargetID, local.ID)
	}

	foreign := fixture.backups.seedTarget(BackupTarget{UserID: uuid.New(), Name: "foreign", Kind: TargetLocal})
	foreignID := foreign.ID.String()
	if _, err := fixture.manager.UpdateSchedule(ctx, fixture.userID, schedule.ID, ScheduleRequest{
		Cron:     "0 6 * * *",
		TargetID: &foreignID,
	}); !errors.Is(err, ErrNotFound) {
		t.Errorf("foreign target err = %v, want ErrNotFound", err)
	}
}

func TestBackupManagerWithoutRepositoryFailsClearly(t *testing.T) {
	manager := NewBackupService(BackupConfig{Logger: discardLogger()})
	if _, err := manager.ListProviders(context.Background(), uuid.New()); err == nil {
		t.Error("expected an error without a repository")
	}
	if _, err := manager.ListBackups(context.Background(), uuid.New(), BackupFilter{}); err == nil {
		t.Error("expected an error without a repository")
	}
	if _, err := manager.GetBackup(context.Background(), uuid.New(), uuid.New()); err == nil {
		t.Error("expected an error without a repository")
	}
}

func TestReconcileStaleBackups(t *testing.T) {
	fixture := newBackupFixture(t)
	stale, err := fixture.backups.CreateBackup(context.Background(), Backup{
		ResourceRef: databaseRef(fixture.databaseID),
		Type:        BackupManual,
		Status:      BackupRunning,
		WasRunning:  true,
		CreatedAt:   time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("seed running backup: %v", err)
	}

	fixture.manager.reconcileStaleBackups()

	got, err := fixture.backups.GetBackup(context.Background(), stale.ID)
	if err != nil {
		t.Fatalf("GetBackup: %v", err)
	}
	if got.Status != BackupFailed {
		t.Errorf("status = %q, want %q", got.Status, BackupFailed)
	}
	if got.Error == "" {
		t.Error("swept backup carries no error")
	}
	if got.FinishedAt.IsZero() {
		t.Error("swept backup has no finished_at")
	}

	fixture.source.mu.Lock()
	recovers := append([]RecoverRequest(nil), fixture.source.recovers...)
	fixture.source.mu.Unlock()
	if len(recovers) != 1 || !recovers[0].WasRunning || recovers[0].WasRestore {
		t.Errorf("recovers = %+v, want one capture recovery that resumes the resource", recovers)
	}
}

func TestReconcileSweepsInterruptedRestore(t *testing.T) {
	fixture := newBackupFixture(t)
	interrupted := fixture.backups.seedRestore(Restore{
		ResourceRef: databaseRef(fixture.databaseID),
		BackupID:    uuid.New(),
		Status:      RestoreRunning,
	})

	fixture.manager.reconcileStaleBackups()

	restore, _ := fixture.backups.getRestore(interrupted.ID)
	if restore.Status != RestoreFailed || restore.Error == "" {
		t.Errorf("restore = %+v, want a recorded sweep failure", restore)
	}
	fixture.source.mu.Lock()
	recovers := append([]RecoverRequest(nil), fixture.source.recovers...)
	fixture.source.mu.Unlock()
	if len(recovers) != 1 || !recovers[0].WasRestore {
		t.Errorf("recovers = %+v, want one restore recovery", recovers)
	}
}

func TestReconcileSkipsResourceWithLiveLease(t *testing.T) {
	fixture := newBackupFixture(t)
	live, err := fixture.backups.CreateBackup(context.Background(), Backup{
		ResourceRef: databaseRef(fixture.databaseID),
		Type:        BackupManual,
		Status:      BackupRunning,
	})
	if err != nil {
		t.Fatalf("seed running backup: %v", err)
	}
	if !fixture.manager.claim(fixture.databaseID) {
		t.Fatal("could not claim the live lease")
	}
	defer fixture.manager.release(fixture.databaseID)

	fixture.manager.reconcileStaleBackups()

	if got, _ := fixture.backups.getBackup(live.ID); got.Status != BackupRunning {
		t.Errorf("live backup status = %q, want %q: the sweep clobbered a live job", got.Status, BackupRunning)
	}
	if len(fixture.source.recovers) != 0 {
		t.Errorf("recovers = %v, want none: a live job's resources were cleaned", fixture.source.recovers)
	}
}

func TestCreateProviderS3RequiresCredentials(t *testing.T) {
	fixture := newBackupFixture(t)
	for _, req := range []ProviderRequest{
		{Name: "no-keys", Kind: "s3", Endpoint: "https://s3.example.com", Bucket: "b"},
		{Name: "access-only", Kind: "s3", Endpoint: "https://s3.example.com", Bucket: "b", AccessKey: "ak"},
		{Name: "secret-only", Kind: "s3", Endpoint: "https://s3.example.com", Bucket: "b", SecretKey: "sk"},
	} {
		if _, err := fixture.manager.CreateProvider(context.Background(), fixture.userID, req); !errors.Is(err, ErrValidation) {
			t.Errorf("CreateProvider(%s) err = %v, want ErrValidation", req.Name, err)
		}
	}
}

func TestUpdateProviderS3SwitchRequiresCredentials(t *testing.T) {
	fixture := newBackupFixture(t)
	ctx := context.Background()
	target := fixture.backups.seedTarget(BackupTarget{UserID: fixture.userID, Name: "local", Kind: TargetLocal})

	rejected := []ProviderRequest{
		{Kind: "s3", Endpoint: "https://s3.example.com", Bucket: "b"},
		{Kind: "s3", Endpoint: "https://s3.example.com", Bucket: "b", AccessKey: "ak"},
		{Kind: "s3", Endpoint: "https://s3.example.com", Bucket: "b", SecretKey: "sk"},
		{Kind: "s3", Endpoint: "https://s3.example.com", Bucket: "b", AccessKey: "ak", SecretKey: "   "},
	}
	for _, req := range rejected {
		if _, err := fixture.manager.UpdateProvider(ctx, fixture.userID, target.ID, req); !errors.Is(err, ErrValidation) {
			t.Errorf("UpdateProvider(%+v) err = %v, want ErrValidation", req, err)
		}
	}
	stored, err := fixture.backups.GetBackupTarget(ctx, target.ID)
	if err != nil {
		t.Fatalf("GetBackupTarget: %v", err)
	}
	if stored.Kind != TargetLocal {
		t.Fatalf("kind = %q, want the rejected switch to leave the target local", stored.Kind)
	}

	switched, err := fixture.manager.UpdateProvider(ctx, fixture.userID, target.ID, ProviderRequest{
		Kind: "s3", Endpoint: "https://s3.example.com", Bucket: "b",
		AccessKey: "ak", SecretKey: "sk",
	})
	if err != nil {
		t.Fatalf("UpdateProvider with credentials: %v", err)
	}
	if switched.Kind != TargetS3 {
		t.Fatalf("kind = %q, want s3", switched.Kind)
	}
	if secrets := fixture.backups.getSecrets(target.ID); len(secrets) != 2 {
		t.Fatalf("stored %d secrets, want the supplied pair", len(secrets))
	}
}

func TestUpdateProviderS3FieldsWithoutCredentialsKeepsStoredKeys(t *testing.T) {
	fixture := newBackupFixture(t)
	ctx := context.Background()
	created, err := fixture.manager.CreateProvider(ctx, fixture.userID, ProviderRequest{
		Name: "r2", Kind: "s3", Endpoint: "https://account.r2.cloudflarestorage.com",
		Bucket: "gotham-backups", AccessKey: "R2AK7f3c9a2b51de84", SecretKey: "b7d41e90c3f5a28e6d0194bc7a3e52f0",
	})
	if err != nil {
		t.Fatalf("CreateProvider: %v", err)
	}

	updated, err := fixture.manager.UpdateProvider(ctx, fixture.userID, created.ID, ProviderRequest{
		Endpoint: "https://account2.r2.cloudflarestorage.com",
		Bucket:   "gotham-backups-v2",
		Prefix:   strPtr("pg-orders/"),
	})
	if err != nil {
		t.Fatalf("credential-less s3 update: %v", err)
	}
	if updated.Endpoint != "https://account2.r2.cloudflarestorage.com" ||
		updated.Bucket != "gotham-backups-v2" || updated.Prefix != "pg-orders/" {
		t.Errorf("updated = %+v, want the new endpoint, bucket and prefix", updated)
	}
	accessKey, secretKey, err := openTargetSecrets(testSecret, fixture.backups.getSecrets(created.ID))
	if err != nil {
		t.Fatalf("openTargetSecrets: %v", err)
	}
	if accessKey != "R2AK7f3c9a2b51de84" || secretKey != "b7d41e90c3f5a28e6d0194bc7a3e52f0" {
		t.Errorf("stored keys changed: %q / %q", accessKey, secretKey)
	}
}
