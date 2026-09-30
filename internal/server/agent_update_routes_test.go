package server

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/justindeelux/gotham/internal/servers"
	"github.com/justindeelux/gotham/internal/updates"
)

// fakeAgentUpdateRegistry is a ServerService that also implements the optional
// agent-update surface (BE-9.2).
type fakeAgentUpdateRegistry struct {
	*fakeServerService
	versions []servers.AgentVersion
	rollout  string
}

func (f *fakeAgentUpdateRegistry) KnownAgentVersions() []servers.AgentVersion { return f.versions }
func (f *fakeAgentUpdateRegistry) AgentRolloutVersion() string                { return f.rollout }
func (f *fakeAgentUpdateRegistry) StartAgentRollout(version string)           { f.rollout = version }

// fakeUpdatesService is a minimal updates.Service for handler tests.
type fakeUpdatesService struct {
	release *updates.Release
	err     error
}

func (f *fakeUpdatesService) Current() string { return "v1.0.0" }
func (f *fakeUpdatesService) Check(context.Context) (*updates.Release, error) {
	return f.release, f.err
}
func (f *fakeUpdatesService) Apply(context.Context, updates.Channel) (*updates.ApplyResult, error) {
	return nil, nil
}
func (f *fakeUpdatesService) Rollback() error                      { return nil }
func (f *fakeUpdatesService) Reset() error                         { return nil }
func (f *fakeUpdatesService) Resume(context.Context) error         { return nil }
func (f *fakeUpdatesService) LastStatus() (*updates.Status, error) { return nil, nil }
func (f *fakeUpdatesService) StartAuto(context.Context)            {}

// TestAgentUpdateRoutes covers the fleet view and the update-all trigger.
func TestAgentUpdateRoutes(t *testing.T) {
	t.Setenv(PlatformAdminsEnv, "user@example.com")

	fake := &fakeAgentUpdateRegistry{
		fakeServerService: newFakeServerService(),
		versions: []servers.AgentVersion{
			{NodeID: "node-a", Version: "v1.0.0", At: time.Now().UTC()},
			{NodeID: "node-b", Version: "v1.1.0", At: time.Now().UTC()},
		},
	}
	s := newServerRoutesTestServer(t, fake)
	s.updates = &fakeUpdatesService{release: &updates.Release{Version: "v1.2.0"}}

	rec := doRequest(t, s, http.MethodGet, "/api/v1/servers/agents", "", authHeader)
	if rec.Code != http.StatusOK {
		t.Fatalf("list status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	var list agentVersionsEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(list.Agents) != 2 || list.LatestVersion != "v1.2.0" {
		t.Fatalf("list = %+v", list)
	}

	rec = doRequest(t, s, http.MethodPost, "/api/v1/servers/agents/update-all", "", authHeader)
	if rec.Code != http.StatusOK {
		t.Fatalf("update-all status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	var result updateAllAgentsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if result.TargetVersion != "v1.2.0" || result.Agents != 2 {
		t.Fatalf("update-all = %+v", result)
	}
	if fake.rollout != "v1.2.0" {
		t.Fatalf("rollout = %q, want v1.2.0", fake.rollout)
	}
}

// TestAgentUpdateRoutesUpToDate proves update-all reports a no-op when nothing
// is newer.
func TestAgentUpdateRoutesUpToDate(t *testing.T) {
	t.Setenv(PlatformAdminsEnv, "user@example.com")
	fake := &fakeAgentUpdateRegistry{fakeServerService: newFakeServerService()}
	s := newServerRoutesTestServer(t, fake)
	s.updates = &fakeUpdatesService{release: nil}

	rec := doRequest(t, s, http.MethodPost, "/api/v1/servers/agents/update-all", "", authHeader)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	var result updateAllAgentsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if result.TargetVersion != "" || fake.rollout != "" {
		t.Fatalf("result = %+v rollout=%q, want a no-op", result, fake.rollout)
	}
}

// TestAgentUpdateRoutesRequireOperator proves a plain session is refused.
func TestAgentUpdateRoutesRequireOperator(t *testing.T) {
	t.Setenv(PlatformAdminsEnv, "")
	fake := &fakeAgentUpdateRegistry{fakeServerService: newFakeServerService()}
	s := newServerRoutesTestServer(t, fake)
	s.updates = &fakeUpdatesService{release: &updates.Release{Version: "v1.2.0"}}

	if rec := doRequest(t, s, http.MethodGet, "/api/v1/servers/agents", "", authHeader); rec.Code != http.StatusForbidden {
		t.Errorf("list status = %d, want 403", rec.Code)
	}
	if rec := doRequest(t, s, http.MethodPost, "/api/v1/servers/agents/update-all", "", authHeader); rec.Code != http.StatusForbidden {
		t.Errorf("update-all status = %d, want 403", rec.Code)
	}
}

// TestAgentUpdateRoutesUnavailable proves the routes are not mounted when the
// registry does not implement the surface: the request falls through to the
// /v1/servers/{id} route, which rejects "agents" as a bad id.
func TestAgentUpdateRoutesUnavailable(t *testing.T) {
	fake := newFakeServerService()
	s := newServerRoutesTestServer(t, fake)

	if rec := doRequest(t, s, http.MethodGet, "/api/v1/servers/agents", "", authHeader); rec.Code != http.StatusBadRequest {
		t.Errorf("list status = %d, want 400 (falls through to the id route)", rec.Code)
	}
}
