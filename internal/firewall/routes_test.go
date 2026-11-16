package firewall

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
)

type fakeAgent struct {
	agentv1.FirewallServiceClient

	mu      sync.Mutex
	status  *agentv1.FirewallStatusResponse
	rules   []*agentv1.FirewallRule
	addErr  error
	lastAdd *agentv1.FirewallRuleCreateRequest
}

func (f *fakeAgent) GetStatus(context.Context, *agentv1.FirewallStatusRequest, ...grpc.CallOption) (*agentv1.FirewallStatusResponse, error) {
	return f.status, nil
}

func (f *fakeAgent) SetEnabled(_ context.Context, in *agentv1.FirewallEnabledRequest, _ ...grpc.CallOption) (*agentv1.FirewallState, error) {
	next := &agentv1.FirewallStatusResponse{
		Backend:          f.status.GetBackend(),
		Available:        f.status.GetAvailable(),
		Enabled:          in.GetEnabled(),
		Manageable:       f.status.GetManageable(),
		Detail:           f.status.GetDetail(),
		SshPort:          f.status.GetSshPort(),
		AgentPort:        f.status.GetAgentPort(),
		ControlPlanePort: f.status.GetControlPlanePort(),
	}
	return &agentv1.FirewallState{Status: next, Rules: f.rules}, nil
}

func (f *fakeAgent) ListRules(context.Context, *agentv1.FirewallRulesRequest, ...grpc.CallOption) (*agentv1.FirewallRules, error) {
	return &agentv1.FirewallRules{Rules: f.rules}, nil
}

func (f *fakeAgent) AddRule(_ context.Context, in *agentv1.FirewallRuleCreateRequest, _ ...grpc.CallOption) (*agentv1.FirewallRules, error) {
	f.mu.Lock()
	f.lastAdd = in
	f.mu.Unlock()
	if f.addErr != nil {
		return nil, f.addErr
	}
	return &agentv1.FirewallRules{Rules: append(f.rules, in.GetRule())}, nil
}

func (f *fakeAgent) UpdateRule(_ context.Context, in *agentv1.FirewallRuleUpdateRequest, _ ...grpc.CallOption) (*agentv1.FirewallRules, error) {
	return &agentv1.FirewallRules{Rules: []*agentv1.FirewallRule{in.GetRule()}}, nil
}

func (f *fakeAgent) DeleteRule(_ context.Context, _ *agentv1.FirewallRuleDeleteRequest, _ ...grpc.CallOption) (*agentv1.FirewallRules, error) {
	return &agentv1.FirewallRules{Rules: []*agentv1.FirewallRule{}}, nil
}

func (f *fakeAgent) Close() error { return nil }

type fakeDialer struct {
	mu     sync.Mutex
	agents map[uuid.UUID]*fakeAgent
	dialed []uuid.UUID
}

func (d *fakeDialer) Dial(_ context.Context, id uuid.UUID) (AgentConn, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.dialed = append(d.dialed, id)
	agent, ok := d.agents[id]
	if !ok {
		return nil, ErrNotFound
	}
	return agent, nil
}

func testStatus() *agentv1.FirewallStatusResponse {
	return &agentv1.FirewallStatusResponse{
		Backend:          agentv1.FirewallBackend_FIREWALL_BACKEND_UFW,
		Available:        true,
		Enabled:          true,
		Manageable:       true,
		SshPort:          22,
		AgentPort:        9443,
		ControlPlanePort: 8000,
	}
}

func testRule() *agentv1.FirewallRule {
	return &agentv1.FirewallRule{
		Id:        "r-1",
		Action:    agentv1.FirewallAction_FIREWALL_ACTION_ALLOW,
		Direction: agentv1.FirewallDirection_FIREWALL_DIRECTION_IN,
		Protocol:  agentv1.FirewallProtocol_FIREWALL_PROTOCOL_TCP,
		Port:      "8080",
		Comment:   "web",
		Enabled:   true,
	}
}

func newTestService(t *testing.T, dialer *fakeDialer) *chi.Mux {
	t.Helper()
	svc := NewService(Config{
		Dial:             dialer.Dial,
		ControlPlanePort: 8000,
		Logger:           slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	router := chi.NewRouter()
	Mount(router, func(next http.Handler) http.Handler { return next },
		func(context.Context) (uuid.UUID, bool) { return uuid.New(), true }, svc)
	return router
}

func do(t *testing.T, router http.Handler, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func decodeBody[T any](t *testing.T, rec *httptest.ResponseRecorder) T {
	t.Helper()
	var out T
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode %q: %v", rec.Body.String(), err)
	}
	return out
}

func TestRoutesStatusAndRules(t *testing.T) {
	serverID := uuid.New()
	dialer := &fakeDialer{agents: map[uuid.UUID]*fakeAgent{
		serverID: {status: testStatus(), rules: []*agentv1.FirewallRule{testRule()}},
	}}
	router := newTestService(t, dialer)

	rec := do(t, router, http.MethodGet, "/v1/servers/"+serverID.String()+"/firewall", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d %s", rec.Code, rec.Body.String())
	}
	statusBody := decodeBody[statusEnvelope](t, rec)
	if statusBody.Status.Backend != BackendUFW || !statusBody.Status.Enabled {
		t.Fatalf("status = %#v", statusBody.Status)
	}

	rec = do(t, router, http.MethodGet, "/v1/servers/"+serverID.String()+"/firewall/rules", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("rules = %d %s", rec.Code, rec.Body.String())
	}
	rulesBody := decodeBody[rulesEnvelope](t, rec)
	if len(rulesBody.Rules) != 1 || rulesBody.Rules[0].Port != "8080" {
		t.Fatalf("rules = %#v", rulesBody.Rules)
	}
}

func TestRoutesToggleAddUpdateDelete(t *testing.T) {
	serverID := uuid.New()
	dialer := &fakeDialer{agents: map[uuid.UUID]*fakeAgent{
		serverID: {status: testStatus(), rules: []*agentv1.FirewallRule{testRule()}},
	}}
	router := newTestService(t, dialer)

	rec := do(t, router, http.MethodPut, "/v1/servers/"+serverID.String()+"/firewall", `{"enabled":false}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("toggle = %d %s", rec.Code, rec.Body.String())
	}
	state := decodeBody[stateEnvelope](t, rec)
	if state.Status.Enabled {
		t.Fatalf("state = %#v", state)
	}

	rec = do(t, router, http.MethodPost, "/v1/servers/"+serverID.String()+"/firewall/rules",
		`{"rule":{"action":"allow","direction":"in","protocol":"tcp","port":"8080","source":"","comment":"web","enabled":true}}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("add = %d %s", rec.Code, rec.Body.String())
	}

	rec = do(t, router, http.MethodPut, "/v1/servers/"+serverID.String()+"/firewall/rules/r-1",
		`{"rule":{"action":"deny","direction":"in","protocol":"udp","port":"5000-5010","source":"10.0.0.0/8","comment":"","enabled":false}}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("update = %d %s", rec.Code, rec.Body.String())
	}

	rec = do(t, router, http.MethodDelete, "/v1/servers/"+serverID.String()+"/firewall/rules/r-1", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("delete = %d %s", rec.Code, rec.Body.String())
	}
}

func TestRoutesValidation(t *testing.T) {
	serverID := uuid.New()
	dialer := &fakeDialer{agents: map[uuid.UUID]*fakeAgent{
		serverID: {status: testStatus()},
	}}
	router := newTestService(t, dialer)

	cases := []string{
		`{"rule":{"action":"block","direction":"in","protocol":"tcp","port":"22","enabled":true}}`,
		`{"rule":{"action":"allow","direction":"sideways","protocol":"tcp","port":"22","enabled":true}}`,
		`{"rule":{"action":"allow","direction":"in","protocol":"icmp","port":"22","enabled":true}}`,
		`{"rule":{"action":"allow","direction":"in","protocol":"tcp","port":"0","enabled":true}}`,
		`{"rule":{"action":"allow","direction":"in","protocol":"tcp","port":"9000-8000","enabled":true}}`,
		`{"rule":{"action":"allow","direction":"in","protocol":"tcp","port":"22","source":"not-an-ip","enabled":true}}`,
		`{"rule":{"action":"allow","direction":"in","protocol":"tcp","port":"22","comment":"bad; comment","enabled":true}}`,
		`{"rule":`,
	}
	for _, body := range cases {
		rec := do(t, router, http.MethodPost, "/v1/servers/"+serverID.String()+"/firewall/rules", body)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("body %s = %d %s", body, rec.Code, rec.Body.String())
		}
	}
}

func TestRoutesNodeRefusalIsAConflict(t *testing.T) {
	serverID := uuid.New()
	agent := &fakeAgent{
		status: testStatus(),
		addErr: status.Error(codes.FailedPrecondition,
			"this rule set denies port 22, which would block SSH access"),
	}
	dialer := &fakeDialer{agents: map[uuid.UUID]*fakeAgent{serverID: agent}}
	router := newTestService(t, dialer)

	rec := do(t, router, http.MethodPost, "/v1/servers/"+serverID.String()+"/firewall/rules",
		`{"rule":{"action":"deny","direction":"in","protocol":"tcp","port":"22","enabled":true}}`)
	if rec.Code != http.StatusConflict {
		t.Fatalf("code = %d %s", rec.Code, rec.Body.String())
	}
	body := decodeBody[errorBody](t, rec)
	if body.Message == "" {
		t.Fatal("a lockout refusal needs a message")
	}
}

func TestRoutesAgentUnavailable(t *testing.T) {
	serverID := uuid.New()
	dialer := &fakeDialer{agents: map[uuid.UUID]*fakeAgent{}}
	router := newTestService(t, dialer)
	rec := do(t, router, http.MethodGet, "/v1/servers/"+serverID.String()+"/firewall", "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("code = %d %s", rec.Code, rec.Body.String())
	}
}

func TestChangingOneNodeNeverTouchesAnother(t *testing.T) {
	firstID := uuid.New()
	secondID := uuid.New()
	firstRule := testRule()
	secondRule := testRule()
	secondRule.Id = "r-2"
	secondRule.Port = "9090"
	dialer := &fakeDialer{agents: map[uuid.UUID]*fakeAgent{
		firstID:  {status: testStatus(), rules: []*agentv1.FirewallRule{firstRule}},
		secondID: {status: testStatus(), rules: []*agentv1.FirewallRule{secondRule}},
	}}
	router := newTestService(t, dialer)

	rec := do(t, router, http.MethodDelete, "/v1/servers/"+firstID.String()+"/firewall/rules/r-1", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("delete = %d %s", rec.Code, rec.Body.String())
	}
	rec = do(t, router, http.MethodGet, "/v1/servers/"+secondID.String()+"/firewall/rules", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("second rules = %d %s", rec.Code, rec.Body.String())
	}
	rules := decodeBody[rulesEnvelope](t, rec)
	if len(rules.Rules) != 1 || rules.Rules[0].Port != "9090" {
		t.Fatalf("second node rules = %#v", rules.Rules)
	}
	for _, id := range dialer.dialed {
		if id != firstID && id != secondID {
			t.Fatalf("dialed unexpected server %s", id)
		}
	}
}

func TestNodeErrorMapping(t *testing.T) {
	cases := []struct {
		err  error
		want error
	}{
		{status.Error(codes.NotFound, "gone"), ErrNotFound},
		{status.Error(codes.Unimplemented, "old agent"), ErrUnsupported},
		{status.Error(codes.Unavailable, "down"), ErrUnavailable},
		{errors.New("plain"), ErrUnavailable},
	}
	for _, tc := range cases {
		if got := nodeError(tc.err); !errors.Is(got, tc.want) {
			t.Errorf("nodeError(%v) = %v, want %v", tc.err, got, tc.want)
		}
	}
}
