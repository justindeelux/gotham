package docker

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
)

type cleanupFake struct {
	mu sync.Mutex

	buildCache []map[string]any
	images     []map[string]any
	containers []map[string]any
	dangling   []map[string]any

	removedImages     []string
	removedContainers []string
	containerQueries  []string
	pruneUntil        string
	pruneCalls        int
	pruneReclaimed    int64
}

func (f *cleanupFake) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()

	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/containers/json":
		f.containerQueries = append(f.containerQueries, r.URL.RawQuery)
		_ = json.NewEncoder(w).Encode(f.containers)
	case r.Method == http.MethodGet && r.URL.Path == "/system/df":
		_ = json.NewEncoder(w).Encode(map[string]any{"BuildCache": f.buildCache})
	case r.Method == http.MethodGet && r.URL.Path == "/images/json":
		if r.URL.Query().Get("filters") != "" {
			_ = json.NewEncoder(w).Encode(f.dangling)
			return
		}
		_ = json.NewEncoder(w).Encode(f.images)
	case r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, "/images/"):
		f.removedImages = append(f.removedImages, strings.TrimPrefix(r.URL.Path, "/images/"))
		_, _ = w.Write([]byte(`[{"Deleted":"sha256:removed"}]`))
	case r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, "/containers/"):
		f.removedContainers = append(f.removedContainers, r.URL.Path)
		if r.URL.Query().Get("v") == "true" {
			http.Error(w, "cleanup must not remove volumes", http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	case r.Method == http.MethodPost && r.URL.Path == "/build/prune":
		f.pruneCalls++
		f.pruneUntil = r.URL.Query().Get("until")
		_ = json.NewEncoder(w).Encode(map[string]any{"SpaceReclaimed": f.pruneReclaimed})
	default:
		http.NotFound(w, r)
	}
}

func imageSummary(id string, tags []string, created, size int64) map[string]any {
	return map[string]any{
		"Id":       id,
		"RepoTags": tags,
		"Created":  created,
		"Size":     size,
	}
}

func containerSummary(id, name, state string, created, size int64, labels map[string]string, imageID string) map[string]any {
	if labels == nil {
		labels = map[string]string{}
	}
	return map[string]any{
		"Id":      id,
		"Names":   []string{"/" + name},
		"ImageID": imageID,
		"State":   state,
		"Created": created,
		"SizeRw":  size,
		"Labels":  labels,
	}
}

func cleanupFixture(t *testing.T, fake *cleanupFake) *DockerClient {
	t.Helper()
	return newTestDockerClient(t, fake)
}

func itemsByCategory(items []*agentv1.CleanupItem) map[string][]*agentv1.CleanupItem {
	grouped := map[string][]*agentv1.CleanupItem{}
	for _, item := range items {
		grouped[item.GetCategory()] = append(grouped[item.GetCategory()], item)
	}
	return grouped
}

func TestScanCleanupCategoriesAndMinAge(t *testing.T) {
	now := time.Now().Unix()
	old := now - 10*24*3600
	recent := now - 3600
	appID := "11111111-1111-1111-1111-111111111111"

	fake := &cleanupFake{
		buildCache: []map[string]any{
			{"ID": "bc-old", "InUse": false, "Size": 100, "CreatedAt": old},
			{"ID": "bc-new", "InUse": false, "Size": 50, "CreatedAt": recent},
			{"ID": "bc-in-use", "InUse": true, "Size": 999, "CreatedAt": old},
		},
		dangling: []map[string]any{
			imageSummary("sha256:dang-old", nil, old, 200),
			imageSummary("sha256:dang-new", nil, recent, 5),
		},
		images: []map[string]any{
			imageSummary("sha256:ext", []string{"library/alpine:3.20"}, old, 10),
			imageSummary("sha256:used", []string{"library/redis:7"}, old, 20),
		},
		containers: []map[string]any{
			containerSummary("c-old", "old-job", "exited", old, 300, nil, ""),
			containerSummary("c-new", "new-job", "exited", recent, 10, nil, ""),
			containerSummary("c-running", "web", "running", old, 50, nil, "sha256:used"),
			containerSummary("c-managed", "stopped-app", "exited", old, 70,
				map[string]string{"gotham.managed": "true"}, ""),
		},
	}
	for i := 5; i >= 1; i-- {
		created := old - int64(6-i)*3600
		fake.images = append(fake.images, imageSummary(
			fmt.Sprintf("sha256:dep-%d", i),
			[]string{fmt.Sprintf("gotham/%s:dep-%d", appID, i)},
			created, 1000,
		))
	}
	client := cleanupFixture(t, fake)

	items, err := client.ScanCleanup(context.Background(), 7*24*time.Hour)
	if err != nil {
		t.Fatalf("ScanCleanup: %v", err)
	}
	grouped := itemsByCategory(items)

	if got := grouped[cleanupCategoryBuildCache]; len(got) != 1 || got[0].GetSizeBytes() != 100 {
		t.Fatalf("build cache = %+v; want one item of 100 bytes (in-use and too-new records excluded)", got)
	}
	if got := grouped[cleanupCategoryDanglingImages]; len(got) != 1 || got[0].GetId() != "image:sha256:dang-old" {
		t.Fatalf("dangling images = %+v; want only the old dangling image", got)
	}
	stopped := grouped[cleanupCategoryStoppedContainers]
	if len(stopped) != 1 || stopped[0].GetId() != "container:c-old" || stopped[0].GetSizeBytes() != 300 {
		t.Fatalf("stopped containers = %+v; want only the old unmanaged exited container", stopped)
	}
	unused := grouped[cleanupCategoryUnusedImages]
	names := make([]string, 0, len(unused))
	for _, item := range unused {
		names = append(names, item.GetName())
	}
	want := []string{"library/alpine:3.20", fmt.Sprintf("gotham/%s:dep-2", appID), fmt.Sprintf("gotham/%s:dep-1", appID)}
	sort.Strings(names)
	sort.Strings(want)
	if len(names) != len(want) {
		t.Fatalf("unused images = %v; want %v", names, want)
	}
	for i := range want {
		if names[i] != want[i] {
			t.Fatalf("unused images = %v; want %v", names, want)
		}
	}
	for _, item := range items {
		if strings.Contains(item.GetId(), "sha256:used") {
			t.Fatalf("active image %s was listed", item.GetId())
		}
	}
	if len(fake.removedImages) != 0 || len(fake.removedContainers) != 0 || fake.pruneCalls != 0 {
		t.Fatalf("scan removed objects: images %v containers %v prunes %d",
			fake.removedImages, fake.removedContainers, fake.pruneCalls)
	}
}

func TestScanCleanupKeepsThreeNewestAppImages(t *testing.T) {
	now := time.Now().Unix()
	appID := "22222222-2222-2222-2222-222222222222"
	fake := &cleanupFake{}
	for i := 5; i >= 1; i-- {
		fake.images = append(fake.images, imageSummary(
			fmt.Sprintf("sha256:img-%d", i),
			[]string{fmt.Sprintf("gotham/%s:dep-%d", appID, i)},
			now-int64(6-i)*100, 500,
		))
	}
	client := cleanupFixture(t, fake)

	items, err := client.ScanCleanup(context.Background(), 0)
	if err != nil {
		t.Fatalf("ScanCleanup: %v", err)
	}
	unused := itemsByCategory(items)[cleanupCategoryUnusedImages]
	if len(unused) != 2 {
		t.Fatalf("unused = %d items; want the 2 older than the 3 newest", len(unused))
	}
	got := map[string]bool{}
	for _, item := range unused {
		got[item.GetName()] = true
	}
	for _, name := range []string{
		fmt.Sprintf("gotham/%s:dep-2", appID),
		fmt.Sprintf("gotham/%s:dep-1", appID),
	} {
		if !got[name] {
			t.Errorf("missing stale image %s", name)
		}
	}
	for _, keep := range []string{"dep-5", "dep-4", "dep-3"} {
		if got[fmt.Sprintf("gotham/%s:%s", appID, keep)] {
			t.Errorf("listed protected image %s", keep)
		}
	}
}

func TestCleanSkipsImageThatBecameUsed(t *testing.T) {
	now := time.Now().Unix()
	fake := &cleanupFake{
		images: []map[string]any{
			imageSummary("sha256:ext", []string{"library/alpine:3.20"}, now-10*24*3600, 10),
		},
	}
	client := cleanupFixture(t, fake)

	items, err := client.ScanCleanup(context.Background(), 0)
	if err != nil || len(items) != 1 {
		t.Fatalf("ScanCleanup = %v, %v; want the unused image", items, err)
	}

	fake.mu.Lock()
	fake.containers = append(fake.containers,
		containerSummary("c1", "app", "running", now, 20, nil, "sha256:ext"))
	fake.mu.Unlock()

	results, err := client.Clean(context.Background(), []string{"image:sha256:ext"}, 0)
	if err != nil {
		t.Fatalf("Clean: %v", err)
	}
	if len(results) != 1 || results[0].GetStatus() != cleanupStatusSkipped || results[0].GetMessage() != "in use" {
		t.Fatalf("result = %+v; want skipped in use", results)
	}
	if len(fake.removedImages) != 0 {
		t.Fatalf("removed images = %v; an in-use image must not be deleted", fake.removedImages)
	}
}

func TestCleanRejectsUnknownItem(t *testing.T) {
	client := cleanupFixture(t, &cleanupFake{})

	results, err := client.Clean(context.Background(), []string{"volume:data", "nonsense"}, 0)
	if err != nil {
		t.Fatalf("Clean: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("results = %+v; want one per requested id", results)
	}
	for _, result := range results {
		if result.GetStatus() != cleanupStatusFailed || result.GetMessage() != "unknown item" {
			t.Errorf("result = %+v; want failed unknown item", result)
		}
	}
}

func TestCleanRemovesOnlySelectedItems(t *testing.T) {
	now := time.Now().Unix()
	old := now - 10*24*3600
	fake := &cleanupFake{
		buildCache: []map[string]any{
			{"ID": "bc", "InUse": false, "Size": 100, "CreatedAt": old},
		},
		images: []map[string]any{
			imageSummary("sha256:ext", []string{"library/alpine:3.20"}, old, 10),
		},
		containers: []map[string]any{
			containerSummary("c-old", "old-job", "exited", old, 300, nil, ""),
		},
		pruneReclaimed: 111,
	}
	client := cleanupFixture(t, fake)

	results, err := client.Clean(context.Background(), []string{
		"image:sha256:ext", "container:c-old", cleanupIDBuildCache,
	}, 0)
	if err != nil {
		t.Fatalf("Clean: %v", err)
	}
	if len(results) != 3 {
		t.Fatalf("results = %d; want 3", len(results))
	}
	for _, result := range results {
		if result.GetStatus() != cleanupStatusRemoved {
			t.Fatalf("result = %+v; want removed", result)
		}
	}
	if len(fake.removedImages) != 1 || fake.removedImages[0] != "sha256:ext" {
		t.Errorf("removed images = %v; want only the selected image", fake.removedImages)
	}
	if len(fake.removedContainers) != 1 {
		t.Fatalf("removed containers = %v; want one", fake.removedContainers)
	}
	if strings.Contains(fake.removedContainers[0], "v=true") {
		t.Errorf("container removal %q must not remove volumes", fake.removedContainers[0])
	}
	if fake.pruneCalls != 1 || fake.pruneUntil != "" {
		t.Errorf("prune calls = %d until = %q; want one prune without an age bound", fake.pruneCalls, fake.pruneUntil)
	}
	if results[2].GetReclaimedBytes() != 111 {
		t.Errorf("build cache reclaimed = %d; want the engine's SpaceReclaimed", results[2].GetReclaimedBytes())
	}
}

func TestCleanBuildCacheUsesMinAge(t *testing.T) {
	now := time.Now().Unix()
	fake := &cleanupFake{
		buildCache: []map[string]any{
			{"ID": "bc", "InUse": false, "Size": 100, "CreatedAt": now - 10*24*3600},
		},
	}
	client := cleanupFixture(t, fake)

	minAge := 7 * 24 * time.Hour
	if _, err := client.Clean(context.Background(), []string{cleanupIDBuildCache}, minAge); err != nil {
		t.Fatalf("Clean: %v", err)
	}
	cutoff := time.Now().Add(-minAge).Unix()
	got, err := strconv.ParseInt(fake.pruneUntil, 10, 64)
	if err != nil {
		t.Fatalf("until = %q: %v", fake.pruneUntil, err)
	}
	if diff := got - cutoff; diff < -60 || diff > 60 {
		t.Errorf("until = %d; want about the min-age cutoff %d", got, cutoff)
	}
}
