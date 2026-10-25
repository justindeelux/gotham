package files

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
)

func TestTrashServiceMapsEntries(t *testing.T) {
	registry := newFakeRegistry()
	server := registry.seed(uuid.New())
	client := &fakeFileClient{trashResp: &agentv1.ListTrashResponse{Entries: []*agentv1.TrashEntry{
		{
			Id:           "0123456789abcdef0123456789abcdef",
			OriginalPath: "dir/a.txt",
			Name:         "a.txt",
			Size:         12,
			Kind:         agentv1.FileKind_FILE_KIND_FILE,
			DeletedAt:    timestamppb.New(testTime()),
		},
		{
			Id:           "fedcba9876543210fedcba9876543210",
			OriginalPath: "sub",
			Name:         "sub",
			Kind:         agentv1.FileKind_FILE_KIND_DIRECTORY,
		},
	}}}
	svc := newTestService(t, registry, client)

	listing, err := svc.Trash(context.Background(), server.ID, "volumes")
	if err != nil {
		t.Fatalf("Trash: %v", err)
	}
	entries := listing.Entries
	if len(entries) != 2 {
		t.Fatalf("got %d entries, want 2", len(entries))
	}
	if listing.Truncated {
		t.Fatal("an uncapped listing reported truncation")
	}
	if entries[0].OriginalPath != "dir/a.txt" || entries[0].Kind != "file" || entries[0].Size != 12 || entries[0].DeletedAt.IsZero() {
		t.Fatalf("entry[0] = %+v", entries[0])
	}
	if entries[1].Kind != "directory" {
		t.Fatalf("entry[1] kind = %q, want directory", entries[1].Kind)
	}

	if _, err := svc.Trash(context.Background(), server.ID, ""); !errors.Is(err, ErrValidation) {
		t.Fatalf("Trash with an empty root = %v, want ErrValidation", err)
	}

	client.trashResp = &agentv1.ListTrashResponse{Truncated: true}
	listing, err = svc.Trash(context.Background(), server.ID, "volumes")
	if err != nil {
		t.Fatalf("Trash: %v", err)
	}
	if !listing.Truncated {
		t.Fatal("a truncated agent listing was reported as complete")
	}
}

func TestTrashRestoreService(t *testing.T) {
	registry := newFakeRegistry()
	server := registry.seed(uuid.New())
	client := &fakeFileClient{restoreResp: &agentv1.RestoreResponse{Entry: &agentv1.FileEntry{Name: "a.txt", Kind: agentv1.FileKind_FILE_KIND_FILE}}}
	svc := newTestService(t, registry, client)

	entry, err := svc.Restore(context.Background(), server.ID, "volumes", "0123456789abcdef0123456789abcdef", "b.txt", true)
	if err != nil {
		t.Fatalf("Restore: %v", err)
	}
	if entry.Name != "a.txt" {
		t.Fatalf("entry = %+v", entry)
	}
	if client.seenRestore.GetId() != "0123456789abcdef0123456789abcdef" || client.seenRestore.GetDestination() != "b.txt" || !client.seenRestore.GetOverwrite() {
		t.Fatalf("restore request = %+v", client.seenRestore)
	}

	client.restoreErr = status.Error(codes.AlreadyExists, "restore: exists")
	if _, err := svc.Restore(context.Background(), server.ID, "volumes", "0123456789abcdef0123456789abcdef", "", false); !errors.Is(err, ErrConflict) {
		t.Fatalf("Restore conflict = %v, want ErrConflict", err)
	}
}

func TestTrashPurgeAndEmptyService(t *testing.T) {
	registry := newFakeRegistry()
	server := registry.seed(uuid.New())
	client := &fakeFileClient{emptyTrashResp: &agentv1.EmptyTrashResponse{Removed: 3}}
	svc := newTestService(t, registry, client)

	if err := svc.Purge(context.Background(), server.ID, "volumes", "0123456789abcdef0123456789abcdef"); err != nil {
		t.Fatalf("Purge: %v", err)
	}
	if client.seenPurge.GetId() != "0123456789abcdef0123456789abcdef" {
		t.Fatalf("purge request = %+v", client.seenPurge)
	}
	if err := svc.Purge(context.Background(), server.ID, "volumes", "../x"); !errors.Is(err, ErrValidation) {
		t.Fatalf("Purge with a bad id = %v, want ErrValidation", err)
	}

	removed, err := svc.EmptyTrash(context.Background(), server.ID, "volumes")
	if err != nil {
		t.Fatalf("EmptyTrash: %v", err)
	}
	if removed != 3 {
		t.Fatalf("removed = %d, want 3", removed)
	}
	if client.seenEmpty.GetRoot() != "volumes" {
		t.Fatalf("empty request = %+v", client.seenEmpty)
	}
}

func TestDeletePermanentFlagService(t *testing.T) {
	registry := newFakeRegistry()
	server := registry.seed(uuid.New())
	client := &fakeFileClient{}
	svc := newTestService(t, registry, client)

	if err := svc.Delete(context.Background(), server.ID, "volumes", "a", false, false); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if client.seenDelete.GetPermanent() {
		t.Fatal("a plain Delete asked for a permanent delete")
	}
	if err := svc.Delete(context.Background(), server.ID, "volumes", "a", true, true); err != nil {
		t.Fatalf("permanent Delete: %v", err)
	}
	if !client.seenDelete.GetPermanent() {
		t.Fatal("a permanent Delete did not set the flag")
	}
}

func TestTrashRoutes(t *testing.T) {
	client := &fakeFileClient{
		trashResp:      &agentv1.ListTrashResponse{Entries: []*agentv1.TrashEntry{{Id: "0123456789abcdef0123456789abcdef", OriginalPath: "a.txt", Name: "a.txt", Kind: agentv1.FileKind_FILE_KIND_FILE}}},
		restoreResp:    &agentv1.RestoreResponse{Entry: &agentv1.FileEntry{Name: "a.txt"}},
		emptyTrashResp: &agentv1.EmptyTrashResponse{Removed: 2},
	}
	h, server := newRealRouter(t, "", uuid.New(), client)
	base := "/v1/servers/" + server.ID.String() + "/files/trash"

	rec := doWrite(t, h, http.MethodGet, base+"?root=volumes", "", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("list trash status = %d (body %s)", rec.Code, rec.Body.String())
	}
	if !contains(rec.Body.String(), `"original_path":"a.txt"`) {
		t.Fatalf("list body = %s", rec.Body.String())
	}

	rec = doWrite(t, h, http.MethodPost, base+"/restore", `{"root":"volumes","id":"0123456789abcdef0123456789abcdef","overwrite":false}`, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("restore status = %d (body %s)", rec.Code, rec.Body.String())
	}

	rec = doWrite(t, h, http.MethodPost, base+"/purge", `{"root":"volumes","id":"0123456789abcdef0123456789abcdef"}`, nil)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("purge status = %d (body %s)", rec.Code, rec.Body.String())
	}

	rec = doWrite(t, h, http.MethodPost, base+"/empty", `{"root":"volumes"}`, nil)
	if rec.Code != http.StatusOK || !contains(rec.Body.String(), `"removed":2`) {
		t.Fatalf("empty status = %d (body %s)", rec.Code, rec.Body.String())
	}
}

func TestRestoreConflictRoute(t *testing.T) {
	client := &fakeFileClient{restoreErr: status.Error(codes.AlreadyExists, "restore: exists")}
	h, server := newRealRouter(t, "", uuid.New(), client)
	rec := doWrite(t, h, http.MethodPost, "/v1/servers/"+server.ID.String()+"/files/trash/restore",
		`{"root":"volumes","id":"0123456789abcdef0123456789abcdef"}`, nil)
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409 (body %s)", rec.Code, rec.Body.String())
	}
	if !contains(rec.Body.String(), `"code":"conflict"`) {
		t.Fatalf("body = %s, want a conflict code", rec.Body.String())
	}
}

func TestDeleteRoutePermanentFlag(t *testing.T) {
	client := &fakeFileClient{}
	h, server := newRealRouter(t, "", uuid.New(), client)
	rec := doWrite(t, h, http.MethodDelete, "/v1/servers/"+server.ID.String()+"/files/entry?root=volumes&path=a&permanent=true", "", nil)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d (body %s)", rec.Code, rec.Body.String())
	}
	if !client.seenDelete.GetPermanent() {
		t.Fatal("the permanent query flag did not reach the agent")
	}

	rec = doWrite(t, h, http.MethodDelete, "/v1/servers/"+server.ID.String()+"/files/entry?root=volumes&path=a", "", nil)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d (body %s)", rec.Code, rec.Body.String())
	}
	if client.seenDelete.GetPermanent() {
		t.Fatal("a plain delete asked for a permanent delete")
	}
}

func contains(haystack, needle string) bool {
	return strings.Contains(haystack, needle)
}

func testTime() time.Time {
	return time.Unix(1_700_000_000, 0).UTC()
}
