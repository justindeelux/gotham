package agent

import (
	"context"
	"encoding/json"
	"net/http"
	"sort"
	"strconv"
	"testing"
)

func imageIDs(images []appImage) []string {
	ids := make([]string, 0, len(images))
	for _, image := range images {
		ids = append(ids, image.ID)
	}
	sort.Strings(ids)
	return ids
}

// TestSelectStaleImages pins the retention selection: the current deploy and
// every active image are always kept, the newest history is kept, and older
// images are removed.
func TestSelectStaleImages(t *testing.T) {
	app := "gotham/web"
	images := []appImage{
		{ID: "id-5", Ref: app + ":dep-5", Created: 500},
		{ID: "id-4", Ref: app + ":dep-4", Created: 400},
		{ID: "id-3", Ref: app + ":dep-3", Created: 300},
		{ID: "id-2", Ref: app + ":dep-2", Created: 200},
		{ID: "id-1", Ref: app + ":dep-1", Created: 100},
	}

	tests := []struct {
		name    string
		keep    string
		active  map[string]struct{}
		history int
		want    []string
	}{
		{
			name:    "newest kept older removed",
			keep:    app + ":dep-5",
			active:  map[string]struct{}{"id-4": {}},
			history: 2,
			want:    []string{"id-1"},
		},
		{
			name:    "active image never removed even when oldest",
			keep:    app + ":dep-5",
			active:  map[string]struct{}{"id-1": {}},
			history: 1,
			want:    []string{"id-2", "id-3"},
		},
		{
			name:    "current deploy never removed",
			keep:    app + ":dep-1",
			active:  map[string]struct{}{"id-5": {}},
			history: 1,
			want:    []string{"id-2", "id-3"},
		},
		{
			name:    "protected images do not consume the history budget",
			keep:    app + ":dep-5",
			active:  map[string]struct{}{"id-4": {}},
			history: 2,
			want:    []string{"id-1"},
		},
		{
			name:    "zero history keeps only protected images",
			keep:    app + ":dep-5",
			active:  map[string]struct{}{"id-4": {}},
			history: 0,
			want:    []string{"id-1", "id-2", "id-3"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := imageIDs(selectStaleImages(images, tt.keep, tt.active, tt.history))
			if len(got) != len(tt.want) {
				t.Fatalf("removed = %v; want %v", got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Fatalf("removed = %v; want %v", got, tt.want)
				}
			}
		})
	}
}

// TestDockerClientPruneAppImages exercises the removal against the Engine API:
// only this application's stale images are removed, and the active image and
// current deploy survive.
func TestDockerClientPruneAppImages(t *testing.T) {
	type image struct {
		ID       string   `json:"Id"`
		RepoTags []string `json:"RepoTags"`
		Created  int64    `json:"Created"`
	}
	images := []image{
		{ID: "sha256:new", RepoTags: []string{"gotham/web:dep-7", "127.0.0.1:5000/gotham/web:dep-7"}, Created: 700},
		{ID: "sha256:h2", RepoTags: []string{"gotham/web:dep-6"}, Created: 600},
		{ID: "sha256:h3", RepoTags: []string{"gotham/web:dep-5"}, Created: 500},
		{ID: "sha256:active", RepoTags: []string{"gotham/web:dep-4"}, Created: 400},
		{ID: "sha256:h5", RepoTags: []string{"gotham/web:dep-3"}, Created: 300},
		{ID: "sha256:old", RepoTags: []string{"gotham/web:dep-2"}, Created: 200},
		{ID: "sha256:older", RepoTags: []string{"gotham/web:dep-1"}, Created: 100},
		{ID: "sha256:oldest", RepoTags: []string{"gotham/web:dep-0"}, Created: 50},
		{ID: "sha256:other", RepoTags: []string{"gotham/api:dep-9"}, Created: 25},
	}

	var removed []string
	client := newTestDockerClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/images/json":
			_ = json.NewEncoder(w).Encode(images)
		case r.Method == http.MethodGet && r.URL.Path == "/containers/json":
			_ = json.NewEncoder(w).Encode([]map[string]string{{"Id": "c1", "ImageID": "sha256:active"}})
		case r.Method == http.MethodDelete && len(r.URL.Path) > len("/images/"):
			removed = append(removed, r.URL.Path[len("/images/"):])
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"Deleted":"ok"}`))
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
		}
	}))

	if err := client.PruneAppImages(context.Background(), "web", "dep-7"); err != nil {
		t.Fatalf("PruneAppImages: %v", err)
	}
	sort.Strings(removed)
	want := []string{"sha256:oldest"}
	if len(removed) != len(want) {
		t.Fatalf("removed = %v; want %v", removed, want)
	}
	for i := range want {
		if removed[i] != want[i] {
			t.Fatalf("removed = %v; want %v", removed, want)
		}
	}
}

// TestPruneAppImagesContinuesAfterRemovalError is the U9 regression: one
// failing removal must not prevent the older images from being reclaimed, and
// the failure is still reported.
func TestPruneAppImagesContinuesAfterRemovalError(t *testing.T) {
	type image struct {
		ID       string   `json:"Id"`
		RepoTags []string `json:"RepoTags"`
		Created  int64    `json:"Created"`
	}
	images := []image{
		{ID: "sha256:new", RepoTags: []string{"gotham/web:dep-9"}, Created: 900},
	}
	// Seven non-protected images; the newest five are kept, so dep-3, dep-2 and
	// dep-1 are removed. dep-3 fails; dep-2 and dep-1 must still be removed.
	for i := 8; i >= 1; i-- {
		name := "dep-" + strconv.Itoa(i)
		images = append(images, image{
			ID:       "sha256:" + name,
			RepoTags: []string{"gotham/web:" + name},
			Created:  int64(i * 100),
		})
	}

	var deleted []string
	client := newTestDockerClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/images/json":
			_ = json.NewEncoder(w).Encode(images)
		case r.Method == http.MethodGet && r.URL.Path == "/containers/json":
			_ = json.NewEncoder(w).Encode([]map[string]string{})
		case r.Method == http.MethodDelete && r.URL.Path == "/images/sha256:dep-3":
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"message":"daemon busy"}`))
		case r.Method == http.MethodDelete:
			deleted = append(deleted, r.URL.Path[len("/images/"):])
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"Deleted":"ok"}`))
		default:
			http.NotFound(w, r)
		}
	}))

	err := client.PruneAppImages(context.Background(), "web", "dep-9")
	if err == nil {
		t.Fatal("PruneAppImages should report the failed removal")
	}
	sort.Strings(deleted)
	want := []string{"sha256:dep-1", "sha256:dep-2"}
	if len(deleted) != len(want) || deleted[0] != want[0] || deleted[1] != want[1] {
		t.Fatalf("deleted = %v; want the two removals after the failure: %v", deleted, want)
	}
}

// TestDockerClientRemoveImageIdempotent pins that a missing image is not an
// error, so a retried cleanup cannot fail a deploy.
func TestDockerClientRemoveImageIdempotent(t *testing.T) {
	client := newTestDockerClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"message":"No such image"}`))
	}))
	if err := client.removeImage(context.Background(), "sha256:missing"); err != nil {
		t.Fatalf("RemoveImage(missing) = %v; want nil", err)
	}
}

// TestAppImageTagIgnoresSharedPrefix guards the repo filter: an application
// whose id prefixes another's must not adopt its images.
func TestAppImageTagIgnoresSharedPrefix(t *testing.T) {
	if _, ok := appImageTag([]string{"gotham/web2:dep"}, "gotham/web"); ok {
		t.Error("gotham/web2 must not match gotham/web")
	}
	if tag, ok := appImageTag([]string{"127.0.0.1:5000/gotham/web:dep"}, "gotham/web"); !ok || tag != "127.0.0.1:5000/gotham/web:dep" {
		t.Errorf("registry-qualified tag = %q, ok=%v", tag, ok)
	}
}
