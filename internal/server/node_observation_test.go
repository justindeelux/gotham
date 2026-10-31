package server

import (
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/justindeelux/gotham/internal/auth"
	"github.com/justindeelux/gotham/internal/servers"
)

type observedRegistry struct {
	*fakeServerService
	versions map[string]servers.AgentVersion
	lookedUp []string
}

func (f *observedRegistry) AgentVersionForNode(id string) (servers.AgentVersion, bool) {
	f.lookedUp = append(f.lookedUp, id)
	entry, ok := f.versions[id]
	return entry, ok
}

func TestAuthorizedNodeVersions(t *testing.T) {
	s, tokens := newScopeTestServer(t)
	registry := &observedRegistry{fakeServerService: newFakeServerService(), versions: map[string]servers.AgentVersion{}}
	s.servers = registry

	now := time.Now().UTC()
	versionedNode, unversionedNode := "agent-a", "agent-unreported"
	versioned, unversioned, noNode := uuid.New(), uuid.New(), uuid.New()
	registry.items[versioned] = servers.Server{ID: versioned, NodeID: &versionedNode, Status: servers.StatusOffline}
	registry.items[unversioned] = servers.Server{ID: unversioned, NodeID: &unversionedNode, Status: servers.StatusOffline}
	registry.items[noNode] = servers.Server{ID: noNode, Status: servers.StatusOffline}
	registry.versions[versionedNode] = servers.AgentVersion{NodeID: versionedNode, Version: "v1.2.3", At: now}
	deleted := uuid.New()
	registry.versions[deleted.String()] = servers.AgentVersion{Version: "deleted-version", At: now}

	token := mustCreateOwnerToken(t, tokens, auth.ScopeAdmin)
	read := func(path string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("GET", path, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set(mcpContractHeader, "1")
		rec := httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, req)
		return rec
	}

	rec := read("/api/v1/servers")
	if rec.Code != 200 {
		t.Fatalf("list=%d %s", rec.Code, rec.Body.String())
	}
	var list serverListEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	if len(list.Servers) != 3 {
		t.Fatalf("visible=%+v, want all three nodes", list)
	}
	for _, node := range list.Servers {
		switch node.ID {
		case versioned.String():
			if node.AgentVersion == nil || node.AgentVersionReportedAt == nil || !node.AgentVersionReportedAt.Equal(now) {
				t.Fatalf("missing heartbeat=%+v", node)
			}
		case unversioned.String(), noNode.String():
			if node.AgentVersion != nil || node.AgentVersionReportedAt != nil {
				t.Fatalf("invented version=%+v", node)
			}
		default:
			t.Fatalf("unexpected node=%+v", node)
		}
	}
	if rec := read("/api/v1/servers/" + deleted.String()); rec.Code != 404 {
		t.Fatalf("missing node=%d %s", rec.Code, rec.Body.String())
	}
	for _, id := range registry.lookedUp {
		if id == deleted.String() {
			t.Fatalf("unauthorized lookup=%s", id)
		}
	}

	registry.versions = map[string]servers.AgentVersion{}
	rec = read("/api/v1/servers/" + versioned.String())
	var detail serverEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &detail); err != nil {
		t.Fatal(err)
	}
	if rec.Code != 200 || detail.Server.AgentVersion != nil || detail.Server.AgentVersionReportedAt != nil {
		t.Fatalf("restart=%s", rec.Body.String())
	}
	var raw map[string]map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &raw)
	if value, ok := raw["server"]["agent_version"]; !ok || value != nil {
		t.Fatal("unknown version must be explicit null")
	}
}
