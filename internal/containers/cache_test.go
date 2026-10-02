package containers

import (
	"testing"
	"time"
)

// TestCacheCodecPreservesInternalEvidence is the U1 regression: the Redis cache
// must round-trip the fields Container's API tags hide (labels, mounts,
// restart policy), or a cache hit serves containers with no labels and any
// consumer reading them silently sees nothing.
func TestCacheCodecPreservesInternalEvidence(t *testing.T) {
	created := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	original := []Container{{
		ID:            "abc",
		Name:          "gotham-backup-1",
		Image:         "postgres:16",
		State:         "exited",
		Status:        "Exited (0)",
		Ports:         []string{"5432:5432"},
		Created:       &created,
		PortsReported: true,
		Labels: map[string]string{
			"gotham.managed":   "true",
			"gotham.role":      "backup",
			"gotham.db_id":     "db-1",
			"gotham.backup_id": "run-1",
		},
		Mounts:        []ContainerMount{{Source: "/var/lib/x", Destination: "/data", ReadOnly: true}},
		RestartPolicy: "no",
	}}

	raw, err := encodeContainers(original)
	if err != nil {
		t.Fatalf("encodeContainers: %v", err)
	}
	decoded, err := decodeContainers(raw)
	if err != nil {
		t.Fatalf("decodeContainers: %v", err)
	}
	if len(decoded) != 1 {
		t.Fatalf("decoded %d containers, want 1", len(decoded))
	}
	got := decoded[0]
	if got.Labels["gotham.role"] != "backup" || got.Labels["gotham.backup_id"] != "run-1" {
		t.Errorf("labels did not survive the cache: %+v", got.Labels)
	}
	if !got.PortsReported {
		t.Error("PortsReported did not survive the cache")
	}
	if got.RestartPolicy != "no" {
		t.Errorf("RestartPolicy = %q, want no", got.RestartPolicy)
	}
	if len(got.Mounts) != 1 || got.Mounts[0].Source != "/var/lib/x" || !got.Mounts[0].ReadOnly {
		t.Errorf("mounts did not survive the cache: %+v", got.Mounts)
	}
	if got.Created == nil || !got.Created.Equal(created) {
		t.Errorf("created = %v, want %v", got.Created, created)
	}
}
