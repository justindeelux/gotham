package backups

import (
	"context"
	"testing"

	"github.com/google/uuid"
)

func TestStoreAndKeyDoesNotRepeatTheGzipSuffix(t *testing.T) {
	manager := &BackupManager{objects: newFakeObjectStore()}
	resource := Resource{Type: ResourceApplication, ID: uuid.New()}
	runID := uuid.New()

	cases := []struct {
		extension string
		want      string
	}{
		{"tar.gz", "applications/" + resource.ID.String() + "/" + runID.String() + ".tar.gz"},
		{"dump", "applications/" + resource.ID.String() + "/" + runID.String() + ".dump.gz"},
	}
	for _, tc := range cases {
		_, key, err := manager.storeAndKey(context.Background(), nil, resource, runID, tc.extension)
		if err != nil {
			t.Fatalf("storeAndKey(%q): %v", tc.extension, err)
		}
		if key != tc.want {
			t.Errorf("storeAndKey(%q) = %q, want %q", tc.extension, key, tc.want)
		}
	}
}
