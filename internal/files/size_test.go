package files

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"

	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/teams"
	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
)

func TestDirectorySizeServiceReadScope(t *testing.T) {
	t.Setenv(FeatureEnv, "true")
	teamID := uuid.New()
	registry := newFakeRegistry()
	server := registry.seed(teamID)
	client := &fakeFileClient{
		sizeResp: &agentv1.DirectorySizeResponse{Bytes: 4096, Entries: 12, Skipped: 2, Truncated: true},
	}
	svc := newTestService(t, registry, client)

	size, err := svc.DirectorySize(scopeCtx(teams.RoleReadOnly, teamID), server.ID, "volumes", "apps")
	if err != nil {
		t.Fatalf("DirectorySize: %v", err)
	}
	if size.Bytes != 4096 || size.Entries != 12 || size.Skipped != 2 || !size.Truncated {
		t.Fatalf("size = %+v", size)
	}
	if client.seenSize.GetRoot() != "volumes" || client.seenSize.GetPath() != "apps" {
		t.Fatalf("agent request = %+v", client.seenSize)
	}
}

func TestDirectorySizeServiceForeignTeamNotFound(t *testing.T) {
	t.Setenv(FeatureEnv, "true")
	registry := newFakeRegistry()
	server := registry.seed(uuid.New())
	svc := newTestService(t, registry, &fakeFileClient{})

	_, err := svc.DirectorySize(scopeCtx(teams.RoleOwner, uuid.New()), server.ID, "volumes", "")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestDirectorySizeServiceValidation(t *testing.T) {
	t.Setenv(FeatureEnv, "true")
	registry := newFakeRegistry()
	server := registry.seed(uuid.New())
	svc := newTestService(t, registry, &fakeFileClient{})

	if _, err := svc.DirectorySize(context.Background(), server.ID, "", ""); !errors.Is(err, ErrValidation) {
		t.Fatalf("empty root err = %v, want ErrValidation", err)
	}
	if _, err := svc.DirectorySize(context.Background(), server.ID, "volumes", "a/../b"); !errors.Is(err, ErrValidation) {
		t.Fatalf("parent segment err = %v, want ErrValidation", err)
	}
}

func TestDirectorySizeRoute(t *testing.T) {
	id := uuid.New()
	svc := &fakeService{size: FolderSize{Bytes: 10, Entries: 1, Truncated: true}}
	h := newTestRouter(svc)

	rec := doRequest(t, h, http.MethodGet, "/v1/servers/"+id.String()+"/files/size?root=volumes&path=sub")
	if rec.Code != http.StatusOK {
		t.Fatalf("size status = %d (body %s)", rec.Code, rec.Body.String())
	}
	var got FolderSize
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode size: %v", err)
	}
	if got.Bytes != 10 || !got.Truncated {
		t.Fatalf("size = %+v", got)
	}
	if !svc.called("size") {
		t.Error("service DirectorySize was not called")
	}

	svc.sizeErr = ErrNotFound
	rec = doRequest(t, h, http.MethodGet, "/v1/servers/"+id.String()+"/files/size?root=volumes")
	if rec.Code != http.StatusNotFound {
		t.Errorf("not-found status = %d, want 404", rec.Code)
	}
}
