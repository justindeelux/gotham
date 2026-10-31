package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/auth"
	"github.com/justindeelux/gotham/internal/toolchain"
)

func TestToolchainUninstallRefusalReturnsBadRequestWithImpact(t *testing.T) {
	for _, id := range []string{"git", "docker"} {
		s, _ := newToolchainTestServer(t, &fakeToolchainService{})
		service := toolchain.NewService(toolchain.Config{})
		s.toolchain = service
		t.Cleanup(func() { _ = service.Close() })
		rec := doRequest(t, s, http.MethodPost, "/api/v1/servers/"+uuid.NewString()+"/toolchain/jobs",
			`{"tools":["`+id+`"],"action":"uninstall"}`, "Bearer valid-token")
		if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "required by the platform") {
			t.Fatalf("uninstall %s = %d %s; want 400 with platform impact", id, rec.Code, rec.Body.String())
		}
	}
}

type fakeToolchainService struct {
	tools   []toolchain.Tool
	listErr error

	enqueueResult toolchain.EnqueueResult
	enqueueErr    error
	lastAction    toolchain.Action
	lastTools     []string

	job       toolchain.Job
	jobErr    error
	log       string
	nextSeq   int64
	lastAfter int64
}

func (f *fakeToolchainService) ListTools(context.Context, uuid.UUID) ([]toolchain.Tool, error) {
	return f.tools, f.listErr
}

func (f *fakeToolchainService) Enqueue(_ context.Context, _ uuid.UUID, action toolchain.Action, toolIDs []string) (toolchain.EnqueueResult, error) {
	f.lastAction, f.lastTools = action, toolIDs
	return f.enqueueResult, f.enqueueErr
}

func (f *fakeToolchainService) Cancel(context.Context, uuid.UUID, uuid.UUID) (toolchain.Job, error) {
	return f.job, f.jobErr
}

func (f *fakeToolchainService) Get(_ context.Context, _, _ uuid.UUID, after int64) (toolchain.JobDetail, error) {
	f.lastAfter = after
	return toolchain.JobDetail{Job: f.job, Log: f.log, NextSeq: f.nextSeq}, f.jobErr
}

func (f *fakeToolchainService) List(context.Context, uuid.UUID, int) ([]toolchain.Job, error) {
	return nil, f.jobErr
}

func newToolchainTestServer(t *testing.T, fake *fakeToolchainService) (*Server, *fakeTokenService) {
	t.Helper()
	s, tokens := newScopeTestServer(t)
	s.auth.(*fakeAuthService).user.Role = "owner"
	s.toolchain = fake
	router, err := s.routes()
	if err != nil {
		t.Fatalf("routes: %v", err)
	}
	s.router = router
	return s, tokens
}

func TestToolchainRoutesScopeContainment(t *testing.T) {
	fake := &fakeToolchainService{
		tools:         []toolchain.Tool{{ID: "docker", Name: "Docker", Installable: true}},
		enqueueResult: toolchain.EnqueueResult{Jobs: []toolchain.Job{}, Skipped: []string{}},
	}
	s, tokens := newToolchainTestServer(t, fake)

	readToken := mustCreateToken(t, tokens, auth.ScopeRead)
	deployToken := mustCreateToken(t, tokens, auth.ScopeDeploy)
	adminToken := mustCreateToken(t, tokens, auth.ScopeAdmin)
	const session = "Bearer valid-token"
	serverID := uuid.New().String()
	toolsPath := "/api/v1/servers/" + serverID + "/tools"
	jobsPath := "/api/v1/servers/" + serverID + "/toolchain/jobs"
	jobPath := jobsPath + "/" + uuid.New().String()
	body := `{"tools":["docker"],"action":"install"}`

	tests := []struct {
		name   string
		method string
		path   string
		body   string
		bearer string
		want   int
	}{
		{"read token lists tools", http.MethodGet, toolsPath, "", "Bearer " + readToken, http.StatusOK},
		{"deploy token lists tools", http.MethodGet, toolsPath, "", "Bearer " + deployToken, http.StatusOK},
		{"read token reads job", http.MethodGet, jobPath, "", "Bearer " + readToken, http.StatusOK},
		{"read token cannot enqueue", http.MethodPost, jobsPath, body, "Bearer " + readToken, http.StatusForbidden},
		{"deploy token cannot enqueue", http.MethodPost, jobsPath, body, "Bearer " + deployToken, http.StatusForbidden},
		{"deploy token cannot cancel", http.MethodPost, jobPath + "/cancel", "", "Bearer " + deployToken, http.StatusForbidden},
		{"admin token lists tools", http.MethodGet, toolsPath, "", "Bearer " + adminToken, http.StatusOK},
		{"admin token enqueues", http.MethodPost, jobsPath, body, "Bearer " + adminToken, http.StatusCreated},
		{"jwt session lists tools", http.MethodGet, toolsPath, "", session, http.StatusOK},
		{"jwt session enqueues", http.MethodPost, jobsPath, body, session, http.StatusCreated},
		{"no token is unauthorized", http.MethodGet, toolsPath, "", "", http.StatusUnauthorized},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rec := doRequest(t, s, tc.method, tc.path, tc.body, tc.bearer)
			if rec.Code != tc.want {
				t.Fatalf("%s %s (%s) = %d, want %d (body %s)", tc.method, tc.path, tc.bearer, rec.Code, tc.want, rec.Body.String())
			}
		})
	}
}

func TestToolchainRouteErrorMapping(t *testing.T) {
	serverID := uuid.New().String()
	jobsPath := "/api/v1/servers/" + serverID + "/toolchain/jobs"
	jobPath := jobsPath + "/" + uuid.New().String()
	toolsPath := "/api/v1/servers/" + serverID + "/tools"
	body := `{"tools":["docker"],"action":"install"}`

	tests := []struct {
		name string
		path string
		do   func(f *fakeToolchainService)
		want int
	}{
		{"validation", jobsPath, func(f *fakeToolchainService) { f.enqueueErr = toolchain.ErrValidation }, http.StatusBadRequest},
		{"not found", jobPath, func(f *fakeToolchainService) { f.jobErr = toolchain.ErrNotFound }, http.StatusNotFound},
		{"forbidden", jobsPath, func(f *fakeToolchainService) { f.enqueueErr = toolchain.ErrForbidden }, http.StatusForbidden},
		{"conflict", jobsPath, func(f *fakeToolchainService) { f.enqueueErr = toolchain.ErrConflict }, http.StatusConflict},
		{"agent unavailable", toolsPath, func(f *fakeToolchainService) { f.listErr = toolchain.ErrAgentUnavailable }, http.StatusBadGateway},
		{"internal", toolsPath, func(f *fakeToolchainService) { f.listErr = errors.New("boom") }, http.StatusInternalServerError},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fake := &fakeToolchainService{}
			tc.do(fake)
			s, _ := newToolchainTestServer(t, fake)
			method := http.MethodGet
			if tc.path == jobsPath {
				method = http.MethodPost
			}
			rec := doRequest(t, s, method, tc.path, body, "Bearer valid-token")
			if rec.Code != tc.want {
				t.Fatalf("%s = %d, want %d (body %s)", tc.path, rec.Code, tc.want, rec.Body.String())
			}
		})
	}
}

func TestToolchainEnqueueParsesActionAndTools(t *testing.T) {
	fake := &fakeToolchainService{enqueueResult: toolchain.EnqueueResult{Jobs: []toolchain.Job{}, Skipped: []string{}}}
	s, _ := newToolchainTestServer(t, fake)

	serverID := uuid.New().String()
	rec := doRequest(t, s, http.MethodPost, "/api/v1/servers/"+serverID+"/toolchain/jobs",
		`{"tools":["pack","railpack"],"action":"uninstall"}`, "Bearer valid-token")
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201 (body %s)", rec.Code, rec.Body.String())
	}
	if fake.lastAction != toolchain.ActionUninstall {
		t.Fatalf("action = %q, want uninstall", fake.lastAction)
	}
	if len(fake.lastTools) != 2 || fake.lastTools[0] != "pack" || fake.lastTools[1] != "railpack" {
		t.Fatalf("tools = %v, want [pack railpack]", fake.lastTools)
	}
}

func TestToolchainInvalidJobIDIsBadRequest(t *testing.T) {
	fake := &fakeToolchainService{}
	s, _ := newToolchainTestServer(t, fake)
	rec := doRequest(t, s, http.MethodGet, "/api/v1/servers/"+uuid.New().String()+"/toolchain/jobs/not-a-uuid", "", "Bearer valid-token")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (body %s)", rec.Code, rec.Body.String())
	}
}

func TestToolchainGetJobReturnsLogAndCursor(t *testing.T) {
	fake := &fakeToolchainService{
		job:     toolchain.Job{ID: uuid.New(), ToolID: "docker", Status: toolchain.StatusRunning},
		log:     "two\nthree\n",
		nextSeq: 3,
	}
	s, _ := newToolchainTestServer(t, fake)

	serverID := uuid.New().String()
	jobID := uuid.New().String()
	rec := doRequest(t, s, http.MethodGet, "/api/v1/servers/"+serverID+"/toolchain/jobs/"+jobID+"?after=1", "", "Bearer valid-token")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	if fake.lastAfter != 1 {
		t.Fatalf("after passed to service = %d, want 1", fake.lastAfter)
	}
	var body struct {
		Job     toolchain.Job `json:"job"`
		Log     string        `json:"log"`
		NextSeq int64         `json:"next_seq"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Log != "two\nthree\n" || body.NextSeq != 3 || body.Job.ToolID != "docker" {
		t.Fatalf("detail = %+v, want log/next_seq/tool", body)
	}
}

func TestToolchainInvalidAfterIsBadRequest(t *testing.T) {
	fake := &fakeToolchainService{}
	s, _ := newToolchainTestServer(t, fake)
	rec := doRequest(t, s, http.MethodGet, "/api/v1/servers/"+uuid.New().String()+"/toolchain/jobs/"+uuid.New().String()+"?after=-3", "", "Bearer valid-token")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (body %s)", rec.Code, rec.Body.String())
	}
}
