package deploy

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/justindeelux/gotham/internal/providers"
	"github.com/justindeelux/gotham/internal/services"
	"github.com/justindeelux/gotham/internal/teams"
	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type snapshotStream struct {
	grpc.ClientStream
	ctx    context.Context
	chunks [][]byte
	err    error
	block  bool
	closed bool
}

func (s *snapshotStream) Recv() (*agentv1.LogChunk, error) {
	if len(s.chunks) > 0 {
		data := s.chunks[0]
		s.chunks = s.chunks[1:]
		return &agentv1.LogChunk{Data: data}, nil
	}
	if s.block {
		<-s.ctx.Done()
		return nil, s.ctx.Err()
	}
	if s.err != nil {
		return nil, s.err
	}
	return nil, io.EOF
}
func (s *snapshotStream) CloseSend() error { s.closed = true; return nil }

type snapshotNode struct {
	*runtimeTestNode
	streams  []*snapshotStream
	requests []*agentv1.StreamLogsRequest
	contexts []context.Context
	closed   bool
}

func (n *snapshotNode) StreamLogs(ctx context.Context, req *agentv1.StreamLogsRequest, _ ...grpc.CallOption) (grpc.ServerStreamingClient[agentv1.LogChunk], error) {
	n.requests = append(n.requests, req)
	n.contexts = append(n.contexts, ctx)
	if len(n.streams) == 0 {
		return nil, status.Error(codes.NotFound, "sensitive missing detail")
	}
	stream := n.streams[0]
	n.streams = n.streams[1:]
	stream.ctx = ctx
	return stream, nil
}
func (n *snapshotNode) Close() error { n.closed = true; return nil }
func snapshotService(t *testing.T, repo *fakeRepository, n *snapshotNode) *Service {
	t.Helper()
	s := NewService(Config{Repository: repo, Secret: testSecretKey, Logger: discardLogger(), Dial: func(context.Context, uuid.UUID) (Node, error) { return n, nil }})
	t.Cleanup(func() { _ = s.Close() })
	return s
}
func TestSnapshotPrefixBounds(t *testing.T) {
	for _, tc := range []struct {
		name, raw, want string
		bytes, lines    int
		complete        bool
		truncated       *bool
	}{
		{"empty", "", "", 10, 10, true, new(false)},
		{"exact-byte", "abc", "abc", 3, 10, false, nil},
		{"extra-byte", "abcd", "abc", 3, 10, false, new(true)},
		{"exact-line", "a\n", "a\n", 10, 1, false, nil},
		{"extra-line", "a\nb\n", "a\n", 10, 1, false, new(true)},
		{"long-line-prefix", strings.Repeat("x", 10000) + "\nfinal\n", "xxxxx", 5, 2, false, new(true)},
		{"rune", "a世b", "a", 3, 10, false, new(true)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, _ := readLogPrefix(strings.NewReader(tc.raw), tc.bytes, tc.lines)
			if got.Log != tc.want || !utf8.ValidString(got.Log) || got.Complete != tc.complete || (got.Truncated == nil) != (tc.truncated == nil) || got.Truncated != nil && *got.Truncated != *tc.truncated {
				t.Fatalf("read=%+v want=%+v", got, tc)
			}
		})
	}
	stream := &snapshotStream{chunks: [][]byte{{0xe4}, {0xb8}, {0x96, '\n'}}}
	got, _ := readLogPrefix(&chunkReader{stream: stream}, 10, 10)
	if got.Log != "世\n" || !got.Complete {
		t.Fatalf("split UTF-8: %+v", got)
	}
	stream = &snapshotStream{chunks: [][]byte{[]byte(strings.Repeat("x", 8193))}}
	got, _ = readLogPrefix(&chunkReader{stream: stream}, 8192, 10)
	if got.Truncated == nil || !*got.Truncated {
		t.Fatal("lost chunk remainder proof")
	}
}
func TestSnapshotAggregateAndStableSelection(t *testing.T) {
	user := uuid.New()
	app := testApplication(user)
	node := &snapshotNode{runtimeTestNode: &runtimeTestNode{}}
	for i := 12; i > 0; i-- {
		node.inventory = append(node.inventory, runtimeFixture(app, fmt.Sprintf("%064x", i), "exited", "unhealthy"))
	}
	first := &snapshotStream{chunks: [][]byte{[]byte("first\n")}}
	second := &snapshotStream{chunks: [][]byte{[]byte("second\n")}}
	node.streams = []*snapshotStream{first, second}
	svc := snapshotService(t, &fakeRepository{app: app}, node)
	out, err := svc.GetApplicationLogs(context.Background(), user, app.ID, LogOptions{MaxBytes: 9})
	if err != nil || len(out.Containers) != 10 || !out.SelectionTruncated || out.Complete || !out.Partial {
		t.Fatalf("snapshot=%+v %v", out, err)
	}
	if out.Containers[0].ID != fmt.Sprintf("%064x", 1) || out.Containers[0].Log != "first\n" || out.Containers[1].Log != "sec" || len(node.requests) != 2 {
		t.Fatalf("unstable/unbounded: %+v", out)
	}
	for i, req := range node.requests {
		if req.Follow || req.Tail != 100 || len(req.ContainerId) != 64 || node.contexts[i].Err() == nil {
			t.Fatalf("unsafe/unclosed request: %+v", req)
		}
	}
	if !first.closed || !second.closed || !node.closed {
		t.Fatal("resources not closed at cap")
	}
}
func TestSnapshotComposeSelectorsAndReadAuthorization(t *testing.T) {
	user := uuid.New()
	app := testApplication(user)
	app.SourceType = SourceCompose
	app.TeamID = uuid.New()
	owned := runtimeFixture(app, strings.Repeat("a", 64), "exited", "unhealthy")
	owned.Labels = map[string]string{composeProjectLabel: services.ProjectName(app.ID), composeServiceLabel: "web", "com.docker.compose.container-number": "2"}
	foreign := runtimeFixture(testApplication(user), strings.Repeat("b", 64), "running", "healthy")
	node := &snapshotNode{runtimeTestNode: &runtimeTestNode{inventory: []*agentv1.ContainerInfo{foreign, owned}}, streams: []*snapshotStream{{chunks: [][]byte{[]byte("ok\n")}}}}
	svc := snapshotService(t, &fakeRepository{app: app, deployments: []Deployment{{ID: uuid.New(), ApplicationID: app.ID, State: StateQueued}}}, node)
	ctx := teams.WithScope(context.Background(), teams.Scope{UserID: user, TeamID: app.TeamID, Role: teams.RoleReadOnly})
	out, err := svc.GetApplicationLogs(ctx, user, app.ID, LogOptions{Service: "web", Replica: "2"})
	if err != nil || len(out.Containers) != 1 || out.Containers[0].ID != owned.Id || out.Containers[0].DeploymentID != nil || out.Containers[0].Log != "ok\n" {
		t.Fatalf("read-only stopped compose=%+v %v", out, err)
	}
	for _, options := range []LogOptions{{Service: "foreign"}, {Service: "web", Replica: "1"}} {
		if _, err = svc.GetApplicationLogs(ctx, user, app.ID, options); !errors.Is(err, ErrNotFound) {
			t.Fatalf("foreign selector: %v", err)
		}
	}
	foreignCtx := teams.WithScope(context.Background(), teams.Scope{UserID: user, TeamID: uuid.New(), Role: teams.RoleOwner})
	if _, err = svc.GetApplicationLogs(foreignCtx, user, app.ID, LogOptions{}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("foreign team: %v", err)
	}
	if _, err = svc.GetApplicationLogs(ctx, user, uuid.New(), LogOptions{}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("foreign app: %v", err)
	}
}
func TestSnapshotFailuresCloseStreams(t *testing.T) {
	for _, tc := range []struct {
		name, code string
		err        error
		block      bool
	}{
		{"missing", "container_missing_or_stale", status.Error(codes.NotFound, "sensitive"), false},
		{"oversize", "upstream_too_large", status.Error(codes.ResourceExhausted, "sensitive"), false},
		{"disconnect", "agent_unavailable", status.Error(codes.Unavailable, "sensitive"), false},
		{"deadline", "deadline_exceeded", nil, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			user := uuid.New()
			app := testApplication(user)
			stream := &snapshotStream{chunks: [][]byte{[]byte("partial\n")}, err: tc.err, block: tc.block}
			node := &snapshotNode{runtimeTestNode: &runtimeTestNode{inventory: []*agentv1.ContainerInfo{runtimeFixture(app, strings.Repeat("a", 64), "running", "")}}, streams: []*snapshotStream{stream}}
			svc := snapshotService(t, &fakeRepository{app: app}, node)
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
			defer cancel()
			out, err := svc.GetApplicationLogs(ctx, user, app.ID, LogOptions{})
			if err != nil || out.Complete || !out.Partial || out.Containers[0].Error != tc.code || !stream.closed || !node.closed || node.contexts[0].Err() == nil {
				t.Fatalf("failure=%+v %v", out, err)
			}
		})
	}
}
func TestSnapshotMaskingAndClipping(t *testing.T) {
	env := map[string]string{"SECRET": "configured-value", "ESCAPED": "x$y", "SHORT": "a-secret"}
	for _, raw := range []string{"prefix configured-value suffix", "x$$y", "token=private-value", "Authorization: Bearer private-value", "https://user:pass@example.com/repo", "configured-va"} {
		got := maskLog(raw, env, true, 256)
		for _, secret := range []string{"configured-value", "private-value", "user:pass", "x$$y", "configured-va"} {
			if strings.Contains(got, secret) {
				t.Fatalf("secret in %q", got)
			}
		}
	}
	user := uuid.New()
	app := testApplication(user)
	sealed, err := providers.SealSecret(testSecretKey, "super-secret-value")
	if err != nil {
		t.Fatal(err)
	}
	repo := &fakeRepository{app: app, secrets: []Secret{{Key: "PASSWORD", Ciphertext: sealed}}}
	node := &snapshotNode{runtimeTestNode: &runtimeTestNode{inventory: []*agentv1.ContainerInfo{runtimeFixture(app, strings.Repeat("a", 64), "exited", "")}}, streams: []*snapshotStream{{chunks: [][]byte{[]byte("super-"), []byte("secret-value\n")}}}}
	out, err := snapshotService(t, repo, node).GetApplicationLogs(context.Background(), user, app.ID, LogOptions{})
	if err != nil || strings.Contains(out.Containers[0].Log, "super-secret-value") {
		t.Fatalf("configured secret=%+v %v", out, err)
	}
}
func TestSnapshotBuildAvailabilityAndPairing(t *testing.T) {
	user := uuid.New()
	app := testApplication(user)
	id := uuid.New()
	for _, state := range []State{StateQueued, StateBuilding, StateRunning, StateFailed} {
		repo := &fakeRepository{app: app, deployments: []Deployment{{ID: id, ApplicationID: app.ID, State: state}}}
		svc := snapshotService(t, repo, &snapshotNode{runtimeTestNode: &runtimeTestNode{}})
		repo.deployments[0].State = state
		out, err := svc.GetDeploymentLogs(context.Background(), user, app.ID, id, LogOptions{})
		want := "not_yet_persisted"
		if state == StateRunning || state == StateFailed {
			want = "no_persisted_output_available"
		}
		if err != nil || out.Availability != want || out.Source != "persisted_build" || out.PriorTruncation != "unknown" {
			t.Fatalf("build=%+v %v", out, err)
		}
		if _, err = svc.GetDeploymentLogs(context.Background(), user, app.ID, uuid.New(), LogOptions{}); !errors.Is(err, ErrNotFound) {
			t.Fatalf("wrong deployment: %v", err)
		}
	}
}
func TestSnapshotRoutesRejectUnboundedOptions(t *testing.T) {
	router := chi.NewRouter()
	user, app, dep := uuid.New(), uuid.New(), uuid.New()
	Mount(router, func(h http.Handler) http.Handler { return h }, func(context.Context) (uuid.UUID, bool) { return user, true }, &fakeDeployService{})
	for _, suffix := range []string{"?tail=0", "?tail=-1", "?tail=1001", "?max_bytes=262145", "?max_bytes=0", "?tail=1&tail=2", "?container_id=foreign", "?replica=2", "?service=web&replica=-1"} {
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, httptest.NewRequest("GET", "/v1/applications/"+app.String()+"/logs"+suffix, nil))
		if rr.Code != 400 {
			t.Fatalf("%s: %d %s", suffix, rr.Code, rr.Body.String())
		}
	}
	for _, suffix := range []string{"", "?tail=10"} {
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, httptest.NewRequest("GET", "/v1/applications/"+app.String()+"/deployments/"+dep.String()+"/logs"+suffix, nil))
		if rr.Code != 200 {
			t.Fatalf("build route %s: %d %s", suffix, rr.Code, rr.Body.String())
		}
		if suffix == "" && rr.Body.String() != "{\"log\":\"\"}\n" {
			t.Fatalf("legacy shape=%s", rr.Body.String())
		}
	}
}

func TestSnapshotLineBudgetEmptyAndCancellation(t *testing.T) {
	for _, tc := range []struct {
		name   string
		chunks [][]byte
		tail   int
		cancel bool
		want   string
	}{
		{"aggregate-lines", [][]byte{[]byte("a\nb\n")}, 1, false, "a\n"},
		{"empty-data-and-eof", [][]byte{nil, {}}, 100, false, ""},
		{"cancelled", nil, 100, true, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			user := uuid.New()
			app := testApplication(user)
			stream := &snapshotStream{chunks: tc.chunks}
			node := &snapshotNode{runtimeTestNode: &runtimeTestNode{inventory: []*agentv1.ContainerInfo{runtimeFixture(app, strings.Repeat("a", 64), "exited", ""), runtimeFixture(app, strings.Repeat("b", 64), "exited", "")}}, streams: []*snapshotStream{stream, {}}}
			svc := snapshotService(t, &fakeRepository{app: app}, node)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if tc.cancel {
				stream.block = true
				timer := time.AfterFunc(10*time.Millisecond, cancel)
				defer timer.Stop()
			}
			out, err := svc.GetApplicationLogs(ctx, user, app.ID, LogOptions{Tail: tc.tail})
			if err != nil || out.Containers[0].Log != tc.want || !stream.closed || !node.closed {
				t.Fatalf("snapshot=%+v %v", out, err)
			}
			if tc.name == "aggregate-lines" && (len(node.requests) != 1 || out.Containers[1].Error != "aggregate_cap") {
				t.Fatalf("per-container line budget: %+v", out)
			}
			if tc.cancel && out.Containers[0].Error != "cancelled" {
				t.Fatalf("cancellation=%+v", out)
			}
			if tc.name == "empty-data-and-eof" && !out.Complete {
				t.Fatalf("empty EOF=%+v", out)
			}
		})
	}
}
func TestSnapshotInventoryRaceAndAgentUnavailable(t *testing.T) {
	user := uuid.New()
	app := testApplication(user)
	for _, missing := range []bool{false, true} {
		repo := &fakeRepository{app: app}
		node := &snapshotNode{runtimeTestNode: &runtimeTestNode{inventory: []*agentv1.ContainerInfo{runtimeFixture(app, strings.Repeat("a", 64), "running", "")}}}
		node.inspect = func(map[string]string, bool, bool) {
			if missing {
				repo.app = Application{}
			} else {
				repo.app.ServerID = uuid.New()
			}
		}
		svc := snapshotService(t, repo, node)
		_, err := svc.GetApplicationLogs(context.Background(), user, app.ID, LogOptions{})
		want := ErrConflict
		if missing {
			want = ErrNotFound
		}
		if !errors.Is(err, want) || len(node.requests) != 0 || !node.closed {
			t.Fatalf("snapshot race: %v", err)
		}
	}
	repo := &fakeRepository{app: app}
	node := &snapshotNode{runtimeTestNode: &runtimeTestNode{err: status.Error(codes.Unavailable, "sensitive")}}
	out, err := snapshotService(t, repo, node).GetApplicationLogs(context.Background(), user, app.ID, LogOptions{})
	if err != nil || out.Error != "agent_unavailable" || !out.Partial || !node.closed {
		t.Fatalf("unavailable=%+v %v", out, err)
	}
}

func TestSnapshotRuntimeIgnoresPipelineState(t *testing.T) {
	for _, state := range []State{StateQueued, StateBuilding, StateFailed} {
		user := uuid.New()
		app := testApplication(user)
		repo := &fakeRepository{app: app, deployments: []Deployment{{ID: uuid.New(), ApplicationID: app.ID}}}
		node := &snapshotNode{runtimeTestNode: &runtimeTestNode{inventory: []*agentv1.ContainerInfo{runtimeFixture(app, strings.Repeat("a", 64), "exited", "")}}, streams: []*snapshotStream{{chunks: [][]byte{[]byte("old container output\n")}}}}
		svc := snapshotService(t, repo, node)
		repo.deployments[0].State = state
		out, err := svc.GetApplicationLogs(context.Background(), user, app.ID, LogOptions{})
		if err != nil || !out.Complete || out.Containers[0].Log != "old container output\n" || out.Containers[0].DeploymentID != nil {
			t.Fatalf("pipeline %s changed runtime evidence: %+v %v", state, out, err)
		}
	}
}
