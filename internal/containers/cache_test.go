package containers

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"

	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
)

// TestCacheCodecPreservesInternalEvidence is the U1 regression: the Redis cache
// must round-trip the fields Container's API tags hide (labels, mounts,
// restart policy, ports_reported) as well as Health, or a cache hit serves
// containers without them and every consumer reading them silently sees
// nothing.
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
		Health:        HealthHealthy,
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
	if got.Health != HealthHealthy {
		t.Errorf("Health = %q, want %q (readiness would report early)", got.Health, HealthHealthy)
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

// TestContainerJSONRoundTripKeepsHealth is the cheap guard for the readiness
// cache bug: the Docker health status must survive the encoding/json round
// trip RedisCache performs, otherwise a cached "starting" is seen as "no
// healthcheck" and readiness is reported early.
func TestContainerJSONRoundTripKeepsHealth(t *testing.T) {
	raw, err := json.Marshal([]Container{{ID: "c1", State: "running", Health: HealthStarting}})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var got []Container
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if len(got) != 1 || got[0].Health != HealthStarting {
		t.Fatalf("health through JSON = %q, want %q (json tag dropped it)", got[0].Health, HealthStarting)
	}
}

// newTestRedisCache builds a RedisCache for the dev Redis. It skips only when
// GOTHAM_TEST_REDIS is unset; an explicit address makes an unreachable Redis
// fatal, mirroring the GOTHAM_TEST_DSN fail-not-skip rule, so the db-test
// job's provisioned Redis is actually gated.
func newTestRedisCache(t *testing.T) *RedisCache {
	t.Helper()
	addr := os.Getenv("GOTHAM_TEST_REDIS")
	explicit := addr != ""
	if !explicit {
		addr = defaultRedisAddr
	}
	cache := NewRedisCache(addr)
	t.Cleanup(func() { _ = cache.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	if err := cache.client.Ping(ctx).Err(); err != nil {
		if explicit {
			t.Fatalf("GOTHAM_TEST_REDIS is set but Redis is unavailable at %s: %v", addr, err)
		}
		t.Skipf("Redis not available at %s: %v", addr, err)
	}
	return cache
}

// TestRedisCachePreservesHealth exercises the real cache round-trip the
// readiness gate depends on.
func TestRedisCachePreservesHealth(t *testing.T) {
	cache := newTestRedisCache(t)
	ctx := context.Background()
	serverID := uuid.New()
	list := []Container{{ID: "c1", State: "running", Status: "Up 2s (health: starting)", Health: HealthStarting}}

	if err := cache.Set(ctx, serverID, list); err != nil {
		t.Fatalf("Set: %v", err)
	}
	got, ok, err := cache.Get(ctx, serverID)
	if err != nil || !ok {
		t.Fatalf("Get = %v, %v, want a hit", ok, err)
	}
	if len(got) != 1 || got[0].Health != HealthStarting {
		t.Fatalf("cached health = %q, want %q", got[0].Health, HealthStarting)
	}
}

// TestListPreservesHealthThroughRedisCache is the reviewer's scenario: a fake
// agent reports "starting", the service caches it, and the second (cache-hit)
// read must still report "starting" rather than dropping Health to "".
func TestListPreservesHealthThroughRedisCache(t *testing.T) {
	cache := newTestRedisCache(t)
	registry := newFakeRegistry()
	server := registry.seed()
	mock := &mockDockerClient{listResp: &agentv1.ListContainersResponse{Containers: []*agentv1.ContainerInfo{{
		Id:     "c1",
		State:  "running",
		Status: "Up 2 seconds (health: starting)",
		Health: HealthStarting,
	}}}}
	svc := fixture(registry, mock, cache)

	first, err := svc.List(context.Background(), server.ID)
	if err != nil {
		t.Fatalf("first List: %v", err)
	}
	if first[0].Health != HealthStarting {
		t.Fatalf("first health = %q, want %q", first[0].Health, HealthStarting)
	}

	second, err := svc.List(context.Background(), server.ID)
	if err != nil {
		t.Fatalf("second List: %v", err)
	}
	if lists, _, _, _ := mock.counts(); lists != 1 {
		t.Fatalf("agent lists = %d, want 1 (second read must be a cache hit)", lists)
	}
	if second[0].Health != HealthStarting {
		t.Fatalf("cache-hit health = %q, want %q (the readiness gate would report ready early)", second[0].Health, HealthStarting)
	}
}
