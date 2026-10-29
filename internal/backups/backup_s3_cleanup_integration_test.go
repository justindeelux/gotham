package backups

import (
	"bytes"
	"context"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/minio/minio-go/v7"
)

// TestS3ArtifactCleanupRemovesTheObject is the throwaway-container check for
// GOTHAM-131: a run whose artifact lives in an S3-compatible bucket is deleted
// from the bucket as well as from the database. It is skipped unless
// GOTHAM_TEST_S3_ENDPOINT points at a disposable MinIO (or compatible) server.
func TestS3ArtifactCleanupRemovesTheObject(t *testing.T) {
	endpoint := strings.TrimSpace(os.Getenv("GOTHAM_TEST_S3_ENDPOINT"))
	if endpoint == "" {
		t.Skip("GOTHAM_TEST_S3_ENDPOINT is not set")
	}
	access := envOr("GOTHAM_TEST_S3_ACCESS_KEY", "gotham")
	secret := envOr("GOTHAM_TEST_S3_SECRET_KEY", "gotham-secret")
	bucket := envOr("GOTHAM_TEST_S3_BUCKET", "gotham-test")

	ctx := context.Background()
	store, err := newS3Store(s3Config{
		endpoint: endpoint, region: "us-east-1", bucket: bucket,
		accessKey: access, secretKey: secret,
	})
	if err != nil {
		t.Fatalf("newS3Store: %v", err)
	}
	if err := store.client.MakeBucket(ctx, bucket, minio.MakeBucketOptions{Region: "us-east-1"}); err != nil {
		if minio.ToErrorResponse(err).Code != "BucketAlreadyOwnedByYou" {
			t.Fatalf("create bucket: %v", err)
		}
	}
	payload := []byte("dump")
	location, err := store.Put(ctx, "retention/"+uuid.NewString()+".gz", bytes.NewReader(payload), int64(len(payload)))
	if err != nil {
		t.Fatalf("put artifact: %v", err)
	}

	fixture := newBackupFixture(t)
	target := fixture.backups.seedTarget(BackupTarget{
		UserID: fixture.userID, Name: "minio", Kind: TargetS3,
		Endpoint: endpoint, Region: "us-east-1", Bucket: bucket,
	})
	sealed, err := sealTargetSecrets(testSecret, target.ID, access, secret)
	if err != nil {
		t.Fatalf("seal secrets: %v", err)
	}
	for _, item := range sealed {
		if _, err := fixture.backups.UpsertTargetSecret(ctx, item); err != nil {
			t.Fatalf("store secret: %v", err)
		}
	}
	backup := fixture.backups.seedBackup(Backup{
		ResourceRef: databaseRef(fixture.databaseID),
		Type:        BackupManual,
		Status:      BackupCompleted,
		TargetID:    target.ID,
		Location:    location,
	})

	if err := fixture.manager.DeleteBackup(ctx, fixture.userID, backup.ID); err != nil {
		t.Fatalf("DeleteBackup: %v", err)
	}
	if _, ok := fixture.backups.getBackup(backup.ID); ok {
		t.Fatal("the run row must be removed after the artifact")
	}
	key := strings.TrimPrefix(location, locationS3Prefix+bucket+"/")
	if _, err := store.client.StatObject(ctx, bucket, key, minio.StatObjectOptions{}); err == nil {
		t.Fatal("the artifact is still in the bucket")
	}
}

func envOr(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}
