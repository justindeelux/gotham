package cleanup

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/servers"
	"github.com/justindeelux/gotham/internal/store"
)

type fakeLeftoverStore struct {
	historyDays int32
	backupDays  int32
	locations   []string
	stats       []store.HistoryStat
	statsErr    error
	deleted     []string
	deleteRows  int64
	deleteSeq   []int64
	deleteCalls int
	deleteErr   error
}

func (f *fakeLeftoverStore) ListBackupLocations(context.Context) ([]string, error) {
	return f.locations, nil
}

func (f *fakeLeftoverStore) CleanupRetentionDays(context.Context) (int32, int32, error) {
	return f.historyDays, f.backupDays, nil
}

func (f *fakeLeftoverStore) HistoryStats(context.Context, time.Time) ([]store.HistoryStat, error) {
	return f.stats, f.statsErr
}

func (f *fakeLeftoverStore) DeleteHistoryBatch(_ context.Context, name string, _ time.Time, _ int) (int64, error) {
	if f.deleteErr != nil {
		return 0, f.deleteErr
	}
	f.deleted = append(f.deleted, name)
	rows := f.deleteRows
	if f.deleteCalls < len(f.deleteSeq) {
		rows = f.deleteSeq[f.deleteCalls]
	}
	f.deleteCalls++
	return rows, nil
}

func testLeftovers(t *testing.T, st LeftoverStore, dir string) *LeftoversSource {
	t.Helper()
	if fake, ok := st.(*fakeLeftoverStore); ok {
		fake.historyDays, fake.backupDays = 30, 7
	}
	return NewLeftoversSource(LeftoversConfig{
		Store:     st,
		BackupDir: dir,
		Now:       func() time.Time { return time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC) },
	})
}

func writeBackupFile(t *testing.T, dir, name string, age time.Duration) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("payload"), 0o640); err != nil {
		t.Fatal(err)
	}
	mtime := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC).Add(-age)
	if err := os.Chtimes(path, mtime, mtime); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestScanOffersOnlyOldUnreferencedBackupFiles(t *testing.T) {
	dir := t.TempDir()
	orphan := writeBackupFile(t, dir, "old/orphan.gz", 10*24*time.Hour)
	referenced := writeBackupFile(t, dir, "referenced.gz", 10*24*time.Hour)
	fresh := writeBackupFile(t, dir, "fresh.gz", 24*time.Hour)

	st := &fakeLeftoverStore{locations: []string{"file://" + referenced}}
	source := testLeftovers(t, st, dir)

	items, err := source.Scan(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("items = %+v; want only the old orphan", items)
	}
	item := items[0]
	if item.ID != "gotham:backup_file:old/orphan.gz" || item.Category != CategoryBackupFiles ||
		item.Name != "old/orphan.gz" || item.SizeBytes != int64(len("payload")) {
		t.Fatalf("item = %+v; want the relative orphan id with its size", item)
	}

	result := source.Clean(context.Background(), item.ID, 0)
	if result.Status != statusRemoved || result.ReclaimedBytes != int64(len("payload")) {
		t.Fatalf("clean orphan = %+v; want removed with reclaimed bytes", result)
	}
	if _, err := os.Stat(orphan); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("orphan still exists: %v", err)
	}
	for _, path := range []string{referenced, fresh} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("kept file %s was touched: %v", path, err)
		}
	}
	if again := source.Clean(context.Background(), item.ID, 0); again.Status != StatusSkipped {
		t.Fatalf("second clean = %+v; want skipped no longer present", again)
	}
}

func TestCleanSkipsReferencedAndTooNewFiles(t *testing.T) {
	dir := t.TempDir()
	referenced := writeBackupFile(t, dir, "referenced.gz", 10*24*time.Hour)
	fresh := writeBackupFile(t, dir, "fresh.gz", 24*time.Hour)

	st := &fakeLeftoverStore{locations: []string{"file://" + referenced}}
	source := testLeftovers(t, st, dir)

	for id, want := range map[string]string{
		"gotham:backup_file:referenced.gz": "referenced by a backups row",
		"gotham:backup_file:fresh.gz":      "too new",
	} {
		result := source.Clean(context.Background(), id, 0)
		if result.Status != StatusSkipped || result.Message != want {
			t.Fatalf("%s = %+v; want skipped %q", id, result, want)
		}
	}
	for _, path := range []string{referenced, fresh} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("file %s was touched: %v", path, err)
		}
	}
}

func TestCleanSkipsFileReferencedAfterScan(t *testing.T) {
	dir := t.TempDir()
	path := writeBackupFile(t, dir, "orphan.gz", 10*24*time.Hour)
	st := &fakeLeftoverStore{}
	source := testLeftovers(t, st, dir)

	items, err := source.Scan(context.Background())
	if err != nil || len(items) != 1 {
		t.Fatalf("scan = %+v err=%v; want the orphan before it is referenced", items, err)
	}
	st.locations = []string{"file://" + path}
	result := source.Clean(context.Background(), items[0].ID, 0)
	if result.Status != StatusSkipped || result.Message != "referenced by a backups row" {
		t.Fatalf("clean = %+v; want skipped because a row now references the file", result)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("referenced file was removed: %v", err)
	}
}

func TestCleanRejectsTraversalAndEscapingSymlinks(t *testing.T) {
	dir := t.TempDir()
	outsideDir := t.TempDir()
	outside := writeBackupFile(t, outsideDir, "secret.gz", 10*24*time.Hour)
	if err := os.Symlink(outside, filepath.Join(dir, "link.gz")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outsideDir, filepath.Join(dir, "escape")); err != nil {
		t.Fatal(err)
	}
	source := testLeftovers(t, &fakeLeftoverStore{}, dir)

	for _, id := range []string{
		"gotham:backup_file:../secret.gz",
		"gotham:backup_file:" + outside,
		"gotham:backup_file:link.gz",
		"gotham:backup_file:escape/secret.gz",
	} {
		result := source.Clean(context.Background(), id, 0)
		if result.Status != StatusSkipped {
			t.Fatalf("%s = %+v; want skipped", id, result)
		}
	}
	if _, err := os.Stat(outside); err != nil {
		t.Fatalf("file outside the backup directory was touched: %v", err)
	}
}

func TestScanHistoryUsesRetentionWindow(t *testing.T) {
	st := &fakeLeftoverStore{stats: []store.HistoryStat{
		{Name: store.HistoryDeploymentLogs, Rows: 4, Bytes: 400},
		{Name: store.HistoryWebhookEvents, Rows: 0, Bytes: 0},
	}}
	source := testLeftovers(t, st, t.TempDir())

	items, err := source.Scan(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].ID != "gotham:history:"+store.HistoryDeploymentLogs ||
		items[0].Category != CategoryHistory || items[0].Name != "Deployment logs" ||
		items[0].SizeBytes != 400 {
		t.Fatalf("history items = %+v; want only the non-empty deployment log item", items)
	}
}

func TestCleanHistoryDeletesUntilEmpty(t *testing.T) {
	st := &fakeLeftoverStore{
		stats:     []store.HistoryStat{{Name: store.HistoryRequestReceipts, Rows: 3, Bytes: 90}},
		deleteSeq: []int64{2, 1, 0},
	}
	source := testLeftovers(t, st, t.TempDir())
	result := source.Clean(context.Background(), "gotham:history:"+store.HistoryRequestReceipts, 0)
	if result.Status != statusRemoved || result.ReclaimedBytes != 90 || result.Message != "3 rows" {
		t.Fatalf("clean = %+v; want removed with the before-scan bytes", result)
	}
	if len(st.deleted) != 3 {
		t.Fatalf("delete calls = %d; want a batch per non-empty result plus the final empty one", len(st.deleted))
	}
}

func TestLeftoversOnlyScannedOnControlPlaneNode(t *testing.T) {
	serverID := uuid.New()
	remote := &servers.Server{ID: serverID, Name: "node"}
	svc := NewService(Config{
		Registry:  &fakeRegistry{server: remote},
		Dial:      func(context.Context, *servers.Server) (DockerClient, error) { return &fakeAgent{}, nil },
		Leftovers: &fakeLeftovers{},
	})
	scans := svc.Scan(context.Background(), []string{serverID.String()}, 0)
	if len(scans) != 1 || len(scans[0].Items) != 0 {
		t.Fatalf("remote scan = %+v; want no gotham items", scans)
	}

	label := "localhost"
	remote.NodeID = &label
	scans = svc.Scan(context.Background(), []string{serverID.String()}, 0)
	if len(scans) != 1 || len(scans[0].Items) != 1 ||
		scans[0].Items[0].Category != CategoryHistory {
		t.Fatalf("control-plane scan = %+v; want the gotham item", scans)
	}
}

type fakeLeftovers struct {
	scanned int
	cleaned []string
}

func (f *fakeLeftovers) Scan(context.Context) ([]Item, error) {
	f.scanned++
	return []Item{{ID: "gotham:history:webhook_events", Category: CategoryHistory, Name: "Webhook events"}}, nil
}

func (f *fakeLeftovers) Clean(_ context.Context, id string, _ time.Duration) ItemResult {
	f.cleaned = append(f.cleaned, id)
	return ItemResult{ID: id, Category: CategoryHistory, Name: "Webhook events", Status: statusRemoved}
}

func TestCleanLeftoversSkipsOnAnotherNode(t *testing.T) {
	serverID := uuid.New()
	left := &fakeLeftovers{}
	svc := NewService(Config{
		Registry:  &fakeRegistry{server: &servers.Server{ID: serverID, Name: "node"}},
		Leftovers: left,
	})
	results := svc.cleanLeftovers(context.Background(), serverID, []string{"gotham:history:webhook_events"}, 0)
	if len(results) != 1 || results[0].Status != StatusSkipped ||
		results[0].Message != "control-plane node only" || len(left.cleaned) != 0 {
		t.Fatalf("results = %+v cleaned = %v; want a skip on a remote node", results, left.cleaned)
	}

	label := "localhost"
	svc.registry.(*fakeRegistry).server.NodeID = &label
	results = svc.cleanLeftovers(context.Background(), serverID, []string{"gotham:history:webhook_events"}, 0)
	if len(results) != 1 || results[0].Status != statusRemoved || len(left.cleaned) != 1 {
		t.Fatalf("results = %+v cleaned = %v; want the control-plane clean", results, left.cleaned)
	}
}
