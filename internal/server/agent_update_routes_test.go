package server

import (
	"context"
	"encoding/json"
	"errors"
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
	versions  []servers.AgentVersion
	rollout   string
	target    string
	targetErr error
}

func (f *fakeAgentUpdateRegistry) KnownAgentVersions() []servers.AgentVersion { return f.versions }
func (f *fakeAgentUpdateRegistry) AgentRolloutVersion() string                { return f.rollout }
func (f *fakeAgentUpdateRegistry) StartAgentRollout(version string)           { f.rollout = version }
func (f *fakeAgentUpdateRegistry) AgentUpdateTarget(context.Context) (string, error) {
	return f.target, f.targetErr
}

// fakeUpdatesService is a minimal updates.Service for handler tests. The agent
// update surface must not depend on it (it reports the control plane's own
// version), so tests set it to prove independence.
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
		target: "v1.2.0",
	}
	s := newServerRoutesTestServer(t, fake)
	// The control plane is already current: its own self-update check finds
	// nothing. H1: update-all must still roll out to the agents behind it.
	s.updates = &fakeUpdatesService{release: nil}

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
	if result.TargetVersion != "v1.2.0" || result.Agents != 2 || result.Pending != 2 {
		t.Fatalf("update-all = %+v", result)
	}
	if fake.rollout != "v1.2.0" {
		t.Fatalf("rollout = %q, want v1.2.0 (CP current + agents behind ⇒ rollout)", fake.rollout)
	}
}

// TestAgentUpdateRoutesUpToDate proves update-all is a no-op when every known
// agent is already on the target.
func TestAgentUpdateRoutesUpToDate(t *testing.T) {
	t.Setenv(PlatformAdminsEnv, "user@example.com")
	fake := &fakeAgentUpdateRegistry{
		fakeServerService: newFakeServerService(),
		versions:          []servers.AgentVersion{{NodeID: "node-a", Version: "v1.2.0", At: time.Now().UTC()}},
		target:            "v1.2.0",
	}
	s := newServerRoutesTestServer(t, fake)

	rec := doRequest(t, s, http.MethodPost, "/api/v1/servers/agents/update-all", "", authHeader)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	var result updateAllAgentsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if result.Pending != 0 || fake.rollout != "" {
		t.Fatalf("result = %+v rollout=%q, want a no-op", result, fake.rollout)
	}
}

// TestAgentUpdateRoutesNoAgentRelease proves update-all reports 503 when the
// agent updater is disabled (no key / FEATURE_UPDATES=false), rather than
// silently no-op'ing.
func TestAgentUpdateRoutesNoAgentRelease(t *testing.T) {
	t.Setenv(PlatformAdminsEnv, "user@example.com")
	fake := &fakeAgentUpdateRegistry{fakeServerService: newFakeServerService(), target: ""}
	s := newServerRoutesTestServer(t, fake)

	rec := doRequest(t, s, http.MethodPost, "/api/v1/servers/agents/update-all", "", authHeader)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503 (body %s)", rec.Code, rec.Body.String())
	}
}

// TestAgentUpdateRoutesTargetError proves a release-server failure is a 502.
func TestAgentUpdateRoutesTargetError(t *testing.T) {
	t.Setenv(PlatformAdminsEnv, "user@example.com")
	fake := &fakeAgentUpdateRegistry{fakeServerService: newFakeServerService(), targetErr: errors.New("releases API down")}
	s := newServerRoutesTestServer(t, fake)

	rec := doRequest(t, s, http.MethodPost, "/api/v1/servers/agents/update-all", "", authHeader)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502 (body %s)", rec.Code, rec.Body.String())
	}
}

// TestAgentUpdateRoutesNormalizesVersions is N4: a bare "1.2.0" and a canonical
// "v1.2.0" are the same version, so a release stamped without the "v" does not
// make every agent look pending.
func TestAgentUpdateRoutesNormalizesVersions(t *testing.T) {
	t.Setenv(PlatformAdminsEnv, "user@example.com")
	fake := &fakeAgentUpdateRegistry{
		fakeServerService: newFakeServerService(),
		versions:          []servers.AgentVersion{{NodeID: "node-a", Version: "1.2.0", At: time.Now().UTC()}},
		target:            "v1.2.0",
	}
	s := newServerRoutesTestServer(t, fake)

	rec := doRequest(t, s, http.MethodPost, "/api/v1/servers/agents/update-all", "", authHeader)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	var result updateAllAgentsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if result.Pending != 0 || fake.rollout != "" {
		t.Fatalf("result = %+v rollout=%q, want a no-op (versions equal)", result, fake.rollout)
	}
}

// TestAgentUpdateRoutesRequireOperator proves a plain session is refused.
func TestAgentUpdateRoutesRequireOperator(t *testing.T) {
	t.Setenv(PlatformAdminsEnv, "")
	fake := &fakeAgentUpdateRegistry{fakeServerService: newFakeServerService(), target: "v1.2.0"}
	s := newServerRoutesTestServer(t, fake)

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
