package backups

import (
	"context"
	"testing"

	"github.com/justindeelux/gotham/internal/storages"
)

// TestProviderAliasAndStorageServiceShareRows proves the deprecated provider
// methods on the backup service and the standalone storage service operate on
// the same rows through the same repository.
func TestProviderAliasAndStorageServiceShareRows(t *testing.T) {
	fixture := newBackupFixture(t)
	ctx := context.Background()

	created, err := fixture.manager.CreateProvider(ctx, fixture.userID, ProviderRequest{
		Name: "bucket", Kind: "s3", Endpoint: "https://s3.example.test", Bucket: "b",
		AccessKey: "ak", SecretKey: "sk",
	})
	if err != nil {
		t.Fatalf("CreateProvider: %v", err)
	}

	storagesSvc := storages.New(storages.Config{
		Repository: fixture.backups,
		Secret:     testSecret,
		LocalDir:   fixture.dir,
	})
	items, err := storagesSvc.List(ctx, fixture.userID)
	if err != nil {
		t.Fatalf("storages List: %v", err)
	}
	found := false
	for _, item := range items {
		if item.ID == created.ID {
			found = true
		}
	}
	if !found {
		t.Fatalf("storages List did not see the provider-created row: %+v", items)
	}

	if _, err := storagesSvc.Update(ctx, fixture.userID, created.ID, storages.Request{Name: "renamed"}); err != nil {
		t.Fatalf("storages Update: %v", err)
	}
	providers, err := fixture.manager.ListProviders(ctx, fixture.userID)
	if err != nil {
		t.Fatalf("ListProviders: %v", err)
	}
	renamed := false
	for _, provider := range providers {
		if provider.ID == created.ID && provider.Name == "renamed" {
			renamed = true
		}
	}
	if !renamed {
		t.Fatalf("provider alias did not see the storage-service update: %+v", providers)
	}
}
