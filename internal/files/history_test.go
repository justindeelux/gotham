package files

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/justindeelux/gotham/internal/teams"
	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
)

// savedVersion is one recorded snapshot the fake store saw.
type savedVersion struct {
	root    string
	path    string
	etag    string
	content string
}

// fakeHistory is a scriptable HistoryStore. It records the canonical path key
// of every call, the context error a save observed, and every deleted revision.
type fakeHistory struct {
	mu sync.Mutex

	saves      []savedVersion
	saveErr    error
	saveID     uuid.UUID
	saveCtxErr error

	versions []Version
	listErr  error

	body   VersionBody
	getErr error

	paths      []string
	deleted    bool
	delErr     error
	deletedIDs []uuid.UUID
}

func (f *fakeHistory) SaveVersion(ctx context.Context, _ uuid.UUID, root, path, etag string, content []byte, _ int, _ int64) (Version, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.saves = append(f.saves, savedVersion{root: root, path: path, etag: etag, content: string(content)})
	f.paths = append(f.paths, path)
	f.saveCtxErr = ctx.Err()
	if f.saveErr != nil {
		return Version{}, f.saveErr
	}
	id := f.saveID
	if id == uuid.Nil {
		id = uuid.New()
	}
	return Version{ID: id, Size: int64(len(content)), Etag: etag}, nil
}

func (f *fakeHistory) ListVersions(_ context.Context, _ uuid.UUID, _, path string) ([]Version, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.paths = append(f.paths, path)
	return f.versions, f.listErr
}

func (f *fakeHistory) GetVersion(_ context.Context, _ uuid.UUID, _, path string, _ uuid.UUID) (VersionBody, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.paths = append(f.paths, path)
	return f.body, f.getErr
}

func (f *fakeHistory) DeleteVersion(_ context.Context, _ uuid.UUID, _, path string, id uuid.UUID) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.paths = append(f.paths, path)
	f.deletedIDs = append(f.deletedIDs, id)
	return f.deleted, f.delErr
}

// savedCount reports how many snapshots were recorded.
func (f *fakeHistory) savedCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.saves)
}

// newHistoryService wires a test service with a history store and returns the
// seeded server and team.
func newHistoryService(t *testing.T, client *fakeFileClient, history HistoryStore) (*Service, uuid.UUID, uuid.UUID) {
	t.Helper()
	teamID := uuid.New()
	registry := newFakeRegistry()
	server := registry.seed(teamID)
	svc := newTestService(t, registry, client)
	if history != nil {
		svc.SetHistoryStore(history)
	}
	return svc, server.ID, teamID
}

// TestSaveRecordsPriorBody proves one save snapshots the body it replaced, with
// the read etag, and only after the write committed.
func TestSaveRecordsPriorBody(t *testing.T) {
	client := &fakeFileClient{
		statResp:  &agentv1.StatResponse{Entry: &agentv1.FileEntry{Name: "a.txt", Kind: agentv1.FileKind_FILE_KIND_FILE, Size: 4, Etag: `"v1"`}},
		readBody:  []byte("old!"),
		writeResp: writeEntry(),
	}
	history := &fakeHistory{}
	svc, serverID, teamID := newHistoryService(t, client, history)

	_, err := svc.Write(scopeCtx(teams.RoleOwner, teamID), serverID, WriteRequest{
		Root: "volumes", Path: "a.txt", ExpectEtag: `"v1"`, Size: 3,
	}, strings.NewReader("new"))
	if err != nil {
		t.Fatalf("write: %v", err)
	}
	if history.savedCount() != 1 {
		t.Fatalf("recorded %d versions, want 1", history.savedCount())
	}
	saved := history.saves[0]
	if saved.root != "volumes" || saved.path != "a.txt" || saved.etag != `"v1"` || saved.content != "old!" {
		t.Fatalf("saved = %+v", saved)
	}
}

// TestForceSaveDoesNotSnapshot proves a write with no read etag (a force-save
// or a create) has no known prior and records nothing.
func TestForceSaveDoesNotSnapshot(t *testing.T) {
	client := &fakeFileClient{writeResp: writeEntry()}
	history := &fakeHistory{}
	svc, serverID, teamID := newHistoryService(t, client, history)

	if _, err := svc.Write(scopeCtx(teams.RoleOwner, teamID), serverID, WriteRequest{
		Root: "volumes", Path: "a.txt", Overwrite: true, Size: 3,
	}, strings.NewReader("new")); err != nil {
		t.Fatalf("write: %v", err)
	}
	if history.savedCount() != 0 {
		t.Fatalf("recorded %d versions for a force-save, want 0", history.savedCount())
	}
}

// TestRestoreSnapshotsCurrentBody proves a restore snapshots what it replaces,
// writes the version under the current etag, and records the prior body.
func TestRestoreSnapshotsCurrentBody(t *testing.T) {
	client := &fakeFileClient{
		statResp:  &agentv1.StatResponse{Entry: &agentv1.FileEntry{Name: "a.txt", Kind: agentv1.FileKind_FILE_KIND_FILE, Size: 7, Etag: `"v1"`}},
		readBody:  []byte("current"),
		writeResp: writeEntry(),
	}
	history := &fakeHistory{body: VersionBody{Version: Version{ID: uuid.New(), Size: 12}, Content: "version-body"}}
	svc, serverID, teamID := newHistoryService(t, client, history)

	if _, err := svc.RestoreVersion(scopeCtx(teams.RoleOwner, teamID), serverID, "volumes", "a.txt", history.body.Version.ID); err != nil {
		t.Fatalf("restore: %v", err)
	}
	header := client.lastWriteHeader()
	if header.GetExpectEtag() != `"v1"` {
		t.Errorf("restore expect etag = %q, want the current etag", header.GetExpectEtag())
	}
	if got := string(client.lastWriteBody()); got != "version-body" {
		t.Errorf("restore body = %q, want version-body", got)
	}
	if history.savedCount() != 1 || history.saves[0].content != "current" || history.saves[0].etag != `"v1"` {
		t.Fatalf("snapshots = %+v, want the current body", history.saves)
	}
}

// TestRestoreRecreatesDeletedFile proves restoring a deleted path creates it
// instead of failing on a missing etag.
func TestRestoreRecreatesDeletedFile(t *testing.T) {
	client := &fakeFileClient{
		statErr:   status.Error(codes.NotFound, "gone"),
		writeResp: &agentv1.WriteResponse{Entry: &agentv1.FileEntry{Name: "a.txt", Etag: `"v2"`}, Created: true},
	}
	history := &fakeHistory{body: VersionBody{Version: Version{ID: uuid.New(), Size: 4}, Content: "body"}}
	svc, serverID, teamID := newHistoryService(t, client, history)

	if _, err := svc.RestoreVersion(scopeCtx(teams.RoleOwner, teamID), serverID, "volumes", "a.txt", history.body.Version.ID); err != nil {
		t.Fatalf("restore: %v", err)
	}
	if header := client.lastWriteHeader(); header.GetExpectEtag() != "" {
		t.Errorf("restore expect etag = %q, want empty for a recreate", header.GetExpectEtag())
	}
	if history.savedCount() != 0 {
		t.Errorf("recorded %d versions for a missing file, want 0", history.savedCount())
	}
}

// TestRestoreRefusesUnreadableCurrent proves a restore of a file that cannot be
// read as text is refused before anything is written, so an over-limit body
// cannot be replaced without a snapshot.
func TestRestoreRefusesUnreadableCurrent(t *testing.T) {
	client := &fakeFileClient{
		statResp: &agentv1.StatResponse{Entry: &agentv1.FileEntry{Name: "a.txt", Kind: agentv1.FileKind_FILE_KIND_FILE, Size: 9, Etag: `"v1"`}},
	}
	history := &fakeHistory{body: VersionBody{Version: Version{ID: uuid.New(), Size: 4}, Content: "body"}}
	svc, serverID, teamID := newHistoryService(t, client, history)

	_, err := svc.RestoreVersion(scopeCtx(teams.RoleOwner, teamID), serverID, "volumes", "a.txt", history.body.Version.ID)
	if !errors.Is(err, ErrTooLarge) {
		t.Fatalf("err = %v, want ErrTooLarge", err)
	}
	if client.lastWrite != nil {
		t.Fatal("an unreadable current file must not be overwritten")
	}
}

// TestHistoryPathConfinement proves the history surface refuses traversal-shaped
// paths and a missing version id before touching the store or the agent.
func TestHistoryPathConfinement(t *testing.T) {
	history := &fakeHistory{}
	svc := NewService(Config{Registry: newFakeRegistry(), HistoryMaxVersions: 10, HistoryMaxBytes: 1024})
	svc.SetHistoryStore(history)
	ctx := context.Background()
	serverID := uuid.New()

	cases := []struct {
		name string
		run  func() error
	}{
		{"list parent segment", func() error {
			_, err := svc.History(ctx, serverID, "volumes", "app/../etc/passwd")
			return err
		}},
		{"read parent segment", func() error {
			_, err := svc.HistoryVersion(ctx, serverID, "volumes", "../a.txt", uuid.New())
			return err
		}},
		{"delete parent segment", func() error {
			return svc.DeleteVersion(ctx, serverID, "volumes", "a/../../b", uuid.New())
		}},
		{"restore missing version id", func() error {
			_, err := svc.RestoreVersion(ctx, serverID, "volumes", "a.txt", uuid.Nil)
			return err
		}},
		{"empty root", func() error {
			_, err := svc.History(ctx, serverID, "", "a.txt")
			return err
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.run(); !errors.Is(err, ErrValidation) {
				t.Fatalf("err = %v, want ErrValidation", err)
			}
		})
	}
	if history.savedCount() != 0 {
		t.Errorf("a refused history call reached the store")
	}
}

// TestHistoryUnavailable proves a service with no store answers 503-shaped
// ErrHistoryUnavailable instead of an empty list.
func TestHistoryUnavailable(t *testing.T) {
	teamID := uuid.New()
	registry := newFakeRegistry()
	server := registry.seed(teamID)
	svc := newTestService(t, registry, &fakeFileClient{})
	if _, err := svc.History(scopeCtx(teams.RoleOwner, teamID), server.ID, "volumes", "a.txt"); !errors.Is(err, ErrHistoryUnavailable) {
		t.Fatalf("err = %v, want ErrHistoryUnavailable", err)
	}
}

// TestHistoryListsNewestFirst proves the service passes the store order through
// unchanged (the store query is newest-first).
func TestHistoryListsNewestFirst(t *testing.T) {
	teamID := uuid.New()
	registry := newFakeRegistry()
	server := registry.seed(teamID)
	now := time.Now()
	history := &fakeHistory{versions: []Version{
		{ID: uuid.New(), CreatedAt: now, Size: 2},
		{ID: uuid.New(), CreatedAt: now.Add(-time.Minute), Size: 2},
	}}
	svc := newTestService(t, registry, &fakeFileClient{})
	svc.SetHistoryStore(history)

	got, err := svc.History(scopeCtx(teams.RoleOwner, teamID), server.ID, "volumes", "a.txt")
	if err != nil {
		t.Fatalf("history: %v", err)
	}
	if len(got) != 2 || !got[0].CreatedAt.After(got[1].CreatedAt) {
		t.Fatalf("versions = %+v, want newest first", got)
	}
}

// TestSaveSkipsSnapshotOnEtagMismatch proves a pre-save read whose etag does
// not match the If-Match the agent validates never records a body: the file
// moved between the caller's read and ours, so the read body is not provably
// the body the commit replaces.
func TestSaveSkipsSnapshotOnEtagMismatch(t *testing.T) {
	client := &fakeFileClient{
		statResp:  &agentv1.StatResponse{Entry: &agentv1.FileEntry{Name: "a.txt", Kind: agentv1.FileKind_FILE_KIND_FILE, Size: 4, Etag: `"v1"`}},
		readBody:  []byte("old!"),
		writeResp: writeEntry(),
	}
	history := &fakeHistory{}
	svc, serverID, teamID := newHistoryService(t, client, history)

	if _, err := svc.Write(scopeCtx(teams.RoleOwner, teamID), serverID, WriteRequest{
		Root: "volumes", Path: "a.txt", ExpectEtag: `"v2"`, Size: 3,
	}, strings.NewReader("new")); err != nil {
		t.Fatalf("write: %v", err)
	}
	if history.savedCount() != 0 {
		t.Fatalf("recorded %d versions for a mismatched etag, want 0", history.savedCount())
	}
}

// TestSnapshotFailureDoesNotFailSave proves a history store error is logged and
// ignored after a committed save (the save result already succeeded).
func TestSnapshotFailureDoesNotFailSave(t *testing.T) {
	client := &fakeFileClient{
		statResp:  &agentv1.StatResponse{Entry: &agentv1.FileEntry{Name: "a.txt", Kind: agentv1.FileKind_FILE_KIND_FILE, Size: 4, Etag: `"v1"`}},
		readBody:  []byte("old!"),
		writeResp: writeEntry(),
	}
	history := &fakeHistory{saveErr: errors.New("database down")}
	svc, serverID, teamID := newHistoryService(t, client, history)

	if _, err := svc.Write(scopeCtx(teams.RoleOwner, teamID), serverID, WriteRequest{
		Root: "volumes", Path: "a.txt", ExpectEtag: `"v1"`, Size: 3,
	}, strings.NewReader("new")); err != nil {
		t.Fatalf("write = %v, want the committed save to succeed", err)
	}
	if history.savedCount() != 1 {
		t.Fatalf("snapshot attempts = %d, want 1", history.savedCount())
	}
}

// TestSnapshotInsertSurvivesRequestCancellation proves the snapshot insert runs
// on a detached context: the client disconnecting right after the agent commit
// must not lose the revision.
func TestSnapshotInsertSurvivesRequestCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(scopeCtx(teams.RoleOwner, uuid.New()))
	defer cancel()
	client := &fakeFileClient{
		statResp:     &agentv1.StatResponse{Entry: &agentv1.FileEntry{Name: "a.txt", Kind: agentv1.FileKind_FILE_KIND_FILE, Size: 4, Etag: `"v1"`}},
		readBody:     []byte("old!"),
		writeResp:    writeEntry(),
		onWriteClose: cancel,
	}
	history := &fakeHistory{}
	teamID := uuid.New()
	registry := newFakeRegistry()
	server := registry.seed(teamID)
	svc := newTestService(t, registry, client)
	svc.SetHistoryStore(history)

	// Rebind the scope to the seeded team, then cancel the request from the
	// fake stream's commit.
	ctx = teams.WithScope(ctx, teams.Scope{UserID: uuid.New(), TeamID: teamID, Role: teams.RoleOwner})
	if _, err := svc.Write(ctx, server.ID, WriteRequest{
		Root: "volumes", Path: "a.txt", ExpectEtag: `"v1"`, Size: 3,
	}, strings.NewReader("new")); err != nil {
		t.Fatalf("write: %v", err)
	}
	if history.savedCount() != 1 {
		t.Fatalf("snapshot attempts = %d, want 1", history.savedCount())
	}
	if history.saveCtxErr != nil {
		t.Fatalf("snapshot context err = %v, want a detached live context", history.saveCtxErr)
	}
}

// TestRestoreSnapshotsBeforeWriteAndAbortsOnFailure proves a restore whose
// pre-snapshot cannot be stored aborts without touching the file.
func TestRestoreSnapshotsBeforeWriteAndAbortsOnFailure(t *testing.T) {
	client := &fakeFileClient{
		statResp:  &agentv1.StatResponse{Entry: &agentv1.FileEntry{Name: "a.txt", Kind: agentv1.FileKind_FILE_KIND_FILE, Size: 7, Etag: `"v1"`}},
		readBody:  []byte("current"),
		writeResp: writeEntry(),
	}
	history := &fakeHistory{
		body:    VersionBody{Version: Version{ID: uuid.New(), Size: 4}, Content: "body"},
		saveErr: errors.New("database down"),
	}
	svc, serverID, teamID := newHistoryService(t, client, history)

	_, err := svc.RestoreVersion(scopeCtx(teams.RoleOwner, teamID), serverID, "volumes", "a.txt", history.body.Version.ID)
	if err == nil {
		t.Fatal("restore succeeded without storing the pre-restore snapshot")
	}
	if client.lastWrite != nil {
		t.Fatal("an unstorable pre-restore snapshot must not overwrite the file")
	}
}

// TestRestoreDropsPreSnapshotOnConflict proves a refused restore removes the
// revision it recorded first, so no phantom history entry remains.
func TestRestoreDropsPreSnapshotOnConflict(t *testing.T) {
	saveID := uuid.New()
	client := &fakeFileClient{
		statResp: &agentv1.StatResponse{Entry: &agentv1.FileEntry{Name: "a.txt", Kind: agentv1.FileKind_FILE_KIND_FILE, Size: 7, Etag: `"v1"`}},
		readBody: []byte("current"),
		writeErr: status.Error(codes.FailedPrecondition, "write: the file changed since it was read"),
	}
	history := &fakeHistory{
		body:   VersionBody{Version: Version{ID: uuid.New(), Size: 4}, Content: "body"},
		saveID: saveID,
	}
	svc, serverID, teamID := newHistoryService(t, client, history)

	_, err := svc.RestoreVersion(scopeCtx(teams.RoleOwner, teamID), serverID, "volumes", "a.txt", history.body.Version.ID)
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("err = %v, want ErrConflict", err)
	}
	if len(history.deletedIDs) != 1 || history.deletedIDs[0] != saveID {
		t.Fatalf("deleted versions = %v, want the pre-restore snapshot %s", history.deletedIDs, saveID)
	}
}

// TestHistoryCanonicalizesPath proves one file has one history key regardless
// of "." / duplicate / trailing separators, for list and restore.
func TestHistoryCanonicalizesPath(t *testing.T) {
	history := &fakeHistory{
		versions: []Version{{ID: uuid.New(), Size: 3}},
		body:     VersionBody{Version: Version{ID: uuid.New(), Size: 4}, Content: "body"},
	}
	client := &fakeFileClient{
		statResp:  &agentv1.StatResponse{Entry: &agentv1.FileEntry{Name: "a.txt", Kind: agentv1.FileKind_FILE_KIND_FILE, Size: 7, Etag: `"v1"`}},
		readBody:  []byte("current"),
		writeResp: writeEntry(),
	}
	svc, serverID, teamID := newHistoryService(t, client, history)
	ctx := scopeCtx(teams.RoleOwner, teamID)
	spelling := "app//./compose.yml/"

	if _, err := svc.History(ctx, serverID, "volumes", spelling); err != nil {
		t.Fatalf("history: %v", err)
	}
	if len(history.paths) != 1 || history.paths[0] != "app/compose.yml" {
		t.Fatalf("list key = %v, want app/compose.yml", history.paths)
	}

	if _, err := svc.RestoreVersion(ctx, serverID, "volumes", spelling, history.body.Version.ID); err != nil {
		t.Fatalf("restore: %v", err)
	}
	if got := client.lastWriteHeader().GetPath(); got != "app/compose.yml" {
		t.Errorf("restore wrote %q, want the canonical path", got)
	}
	if history.paths[len(history.paths)-1] != "app/compose.yml" {
		t.Errorf("snapshot key = %q, want the canonical path", history.paths[len(history.paths)-1])
	}
}
