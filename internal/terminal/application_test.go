package terminal

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"golang.org/x/net/websocket"

	"github.com/justindeelux/gotham/agent"
)

// fakeApplicationTargets is a controllable resolver for the application routes.
type fakeApplicationTargets struct {
	mu         sync.Mutex
	set        ContainerTargetSet
	target     ContainerTarget
	listErr    error
	resolveErr error
	listed     int
	resolved   int
	lastUser   uuid.UUID
	lastApp    uuid.UUID
	lastSvc    string
	lastID     string
}

func (f *fakeApplicationTargets) ListContainers(_ context.Context, userID, appID uuid.UUID) (ContainerTargetSet, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.listed++
	f.lastUser, f.lastApp = userID, appID
	if f.listErr != nil {
		return ContainerTargetSet{}, f.listErr
	}
	return f.set, nil
}

func (f *fakeApplicationTargets) ResolveContainer(_ context.Context, userID, appID uuid.UUID, service, containerID string) (ContainerTargetSet, ContainerTarget, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.resolved++
	f.lastUser, f.lastApp, f.lastSvc, f.lastID = userID, appID, service, containerID
	if f.resolveErr != nil {
		return ContainerTargetSet{}, ContainerTarget{}, f.resolveErr
	}
	return f.set, f.target, nil
}

func (f *fakeApplicationTargets) snapshot() (resolved int, user, app uuid.UUID, service, container string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.resolved, f.lastUser, f.lastApp, f.lastSvc, f.lastID
}

// appFakeExec is a container exec that echoes input back as output.
type appFakeExec struct {
	sessions chan *appFakeSession
}

func (f *appFakeExec) Exec(_ context.Context, containerID string, opts agent.ExecOptions) (agent.ExecSession, error) {
	reader, writer := io.Pipe()
	session := &appFakeSession{
		containerID: containerID,
		reader:      reader,
		output:      writer,
		cols:        opts.Cols,
		rows:        opts.Rows,
		closed:      make(chan struct{}),
	}
	f.sessions <- session
	return session, nil
}

type appFakeSession struct {
	containerID string
	reader      *io.PipeReader
	output      *io.PipeWriter

	mu        sync.Mutex
	input     strings.Builder
	cols      uint16
	rows      uint16
	closed    chan struct{}
	closeOnce sync.Once
}

func (s *appFakeSession) Read(p []byte) (int, error) { return s.reader.Read(p) }
func (s *appFakeSession) Write(p []byte) (int, error) {
	s.mu.Lock()
	n, err := s.input.Write(p)
	exiting := strings.Contains(s.input.String(), "exit")
	s.mu.Unlock()
	if exiting {
		// Simulate the shell exiting after the command: the reader ends, the
		// pump reports the exit status on the same stream.
		go func() { _ = s.reader.Close() }()
	}
	return n, err
}
func (s *appFakeSession) Close() error {
	s.closeOnce.Do(func() { close(s.closed) })
	return s.reader.Close()
}
func (s *appFakeSession) Resize(_ context.Context, cols, rows uint16) error {
	s.mu.Lock()
	s.cols, s.rows = cols, rows
	s.mu.Unlock()
	return nil
}
func (s *appFakeSession) ExitCode(context.Context) (int32, string, error) {
	return 0, "", nil
}
func (s *appFakeSession) Shell() string { return "/bin/sh" }
func (s *appFakeSession) User() string  { return "app" }

func (s *appFakeSession) inputString() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.input.String()
}

func (s *appFakeSession) dimensions() (uint16, uint16) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cols, s.rows
}

func newApplicationFixture(t *testing.T, targets *fakeApplicationTargets, exec agent.ContainerExec) (*fixture, uuid.UUID) {
	t.Helper()
	f := newFixture(t, fixtureOptions{operator: true, applications: targets, containers: exec})
	appID := targets.set.ApplicationID
	if appID == uuid.Nil {
		appID = uuid.New()
		targets.set.ApplicationID = appID
	}
	targets.set.ServerID = f.serverID
	for i := range targets.set.Containers {
		targets.set.Containers[i].ApplicationID = appID
		targets.set.Containers[i].ServerID = f.serverID
	}
	targets.target.ApplicationID = appID
	targets.target.ServerID = f.serverID
	return f, appID
}

func defaultApplicationTargets(appID uuid.UUID) *fakeApplicationTargets {
	container := ContainerTarget{
		Service:       "web",
		ContainerID:   "web-1",
		Name:          "shop-web-1",
		Image:         "nginx:1",
		State:         "running",
		Replica:       1,
		Label:         "web · shop-web-1",
		ApplicationID: appID,
	}
	return &fakeApplicationTargets{
		set: ContainerTargetSet{
			ApplicationID:   appID,
			ApplicationName: "shop",
			SourceType:      "compose",
			Containers:      []ContainerTarget{container},
		},
		target: container,
	}
}

func TestApplicationTargetsAndSessionRoutes(t *testing.T) {
	targets := defaultApplicationTargets(uuid.New())
	f, appID := newApplicationFixture(t, targets, &appFakeExec{sessions: make(chan *appFakeSession, 1)})

	response, err := http.Get(f.server.URL + "/v1/applications/" + appID.String() + "/terminal/targets")
	if err != nil {
		t.Fatalf("list targets: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("targets status = %d, want 200", response.StatusCode)
	}
	var listed targetsResponse
	if err := json.NewDecoder(response.Body).Decode(&listed); err != nil {
		t.Fatalf("decode targets: %v", err)
	}
	if listed.ApplicationID != appID.String() || listed.ServerID != f.serverID.String() ||
		len(listed.Containers) != 1 || listed.Containers[0].Label != "web · shop-web-1" {
		t.Fatalf("targets = %+v", listed)
	}
	if _, user, app, _, _ := targets.snapshot(); user != f.userID || app != appID {
		t.Fatalf("resolver saw user=%s app=%s", user, app)
	}

	body := strings.NewReader(`{"service":"web","container_id":"web-1","cols":100,"rows":30}`)
	created, err := http.Post(f.server.URL+"/v1/applications/"+appID.String()+"/terminal/sessions", "application/json", body)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	defer created.Body.Close()
	if created.StatusCode != http.StatusCreated {
		t.Fatalf("create status = %d, want 201", created.StatusCode)
	}
	var session createResponse
	if err := json.NewDecoder(created.Body).Decode(&session); err != nil {
		t.Fatalf("decode create: %v", err)
	}
	if session.Target != TargetContainer || session.ApplicationID != appID.String() ||
		session.ContainerID != "web-1" || session.TargetLabel != "web · shop-web-1" {
		t.Fatalf("session = %+v", session)
	}

	request, _ := http.NewRequest(http.MethodDelete,
		f.server.URL+"/v1/applications/"+appID.String()+"/terminal/sessions/"+session.ID, nil)
	deleted, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("delete: %v", err)
	}
	defer deleted.Body.Close()
	if deleted.StatusCode != http.StatusNoContent {
		t.Fatalf("delete status = %d, want 204", deleted.StatusCode)
	}
}

func TestApplicationCreateRejectsForgedAndForbiddenTargets(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want int
	}{
		{"forged container", ErrContainerNotFound, http.StatusNotFound},
		{"read-only role", ErrForbidden, http.StatusForbidden},
		{"no running deployment", ErrNoRunningDeployment, http.StatusConflict},
		{"container stopped", ErrContainerNotRunning, http.StatusConflict},
		{"deploy in progress", ErrDeployInProgress, http.StatusConflict},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			targets := defaultApplicationTargets(uuid.New())
			targets.resolveErr = testCase.err
			f, appID := newApplicationFixture(t, targets, &appFakeExec{sessions: make(chan *appFakeSession, 1)})
			body := strings.NewReader(`{"service":"web","container_id":"someone-elses","cols":100,"rows":30}`)
			response, err := http.Post(f.server.URL+"/v1/applications/"+appID.String()+"/terminal/sessions", "application/json", body)
			if err != nil {
				t.Fatalf("create: %v", err)
			}
			defer response.Body.Close()
			if response.StatusCode != testCase.want {
				t.Fatalf("status = %d, want %d", response.StatusCode, testCase.want)
			}
			resolved, _, _, service, container := targets.snapshot()
			if resolved != 1 || service != "web" || container != "someone-elses" {
				t.Fatalf("resolver saw (%d, %q, %q), want the requested forged target", resolved, service, container)
			}
		})
	}
}

func TestApplicationSessionOwnershipAndSingleUse(t *testing.T) {
	targets := defaultApplicationTargets(uuid.New())
	f, appID := newApplicationFixture(t, targets, &appFakeExec{sessions: make(chan *appFakeSession, 1)})
	session, err := f.svc.CreateApplication(context.Background(), f.userID, appID,
		AppCreateRequest{Service: "web", ContainerID: "web-1", Cols: 80, Rows: 24})
	if err != nil {
		t.Fatalf("CreateApplication: %v", err)
	}
	if session.Target != TargetContainer || session.ApplicationID != appID {
		t.Fatalf("session = %+v", session)
	}
	if _, err := f.svc.ValidateApplication(context.Background(), f.userID, uuid.New(), session.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-application validate = %v, want ErrNotFound", err)
	}
	if _, err := f.svc.ValidateApplication(context.Background(), uuid.New(), appID, session.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-user validate = %v, want ErrNotFound", err)
	}
	if _, err := f.svc.AttachApplication(context.Background(), f.userID, appID, session.ID); err != nil {
		t.Fatalf("attach: %v", err)
	}
	if _, err := f.svc.ValidateApplication(context.Background(), f.userID, appID, session.ID); !errors.Is(err, ErrAlreadyAttached) {
		t.Fatalf("second validate = %v, want ErrAlreadyAttached", err)
	}
	if err := f.svc.CloseApplication(context.Background(), f.userID, uuid.New(), session.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-application close = %v, want ErrNotFound", err)
	}
	// The host route must not see or close a container session: the two
	// targets never imply each other.
	if err := f.svc.Validate(context.Background(), f.userID, f.serverID, session.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("host validate of a container session = %v, want ErrNotFound", err)
	}
	if err := f.svc.Close(context.Background(), f.userID, f.serverID, session.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("host close of a container session = %v, want ErrNotFound", err)
	}
	host, err := f.svc.Create(context.Background(), f.userID, f.serverID, CreateRequest{Root: "system", Cols: 80, Rows: 24})
	if err != nil {
		t.Fatalf("Create host: %v", err)
	}
	if _, err := f.svc.ValidateApplication(context.Background(), f.userID, appID, host.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("application validate of a host session = %v, want ErrNotFound", err)
	}
}

func TestApplicationRelayStreamsContainerSession(t *testing.T) {
	exec := &appFakeExec{sessions: make(chan *appFakeSession, 1)}
	targets := defaultApplicationTargets(uuid.New())
	f, appID := newApplicationFixture(t, targets, exec)
	session, err := f.svc.CreateApplication(context.Background(), f.userID, appID,
		AppCreateRequest{Service: "web", ContainerID: "web-1", Cols: 80, Rows: 24})
	if err != nil {
		t.Fatalf("CreateApplication: %v", err)
	}

	wsURL := "ws" + strings.TrimPrefix(f.server.URL, "http") +
		"/v1/applications/" + appID.String() + "/terminal/sessions/" + session.ID.String() + "/ws"
	config, err := websocket.NewConfig(wsURL, f.server.URL)
	if err != nil {
		t.Fatalf("websocket config: %v", err)
	}
	config.Protocol = []string{Subprotocol, TokenProtocolPrefix + "good-token"}
	conn, err := websocket.DialConfig(config)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = conn.Close() }()

	opcode, payload := readFrame(t, conn)
	if opcode != opOpened {
		t.Fatalf("first opcode = %#x, want opened", opcode)
	}
	var opened openedEvent
	if err := json.Unmarshal(payload, &opened); err != nil {
		t.Fatalf("decode opened: %v", err)
	}
	if opened.Target != TargetContainer || opened.ContainerID != "web-1" ||
		opened.TargetLabel != "web · shop-web-1" || opened.Shell != "/bin/sh" || opened.ServiceUser != "app" {
		t.Fatalf("opened = %+v", opened)
	}
	f.authorizer.mu.Lock()
	target := f.authorizer.target
	f.authorizer.mu.Unlock()
	if target.Kind != TargetContainer || target.ApplicationID != appID || target.ServerID != f.serverID {
		t.Fatalf("authorizer target = %+v", target)
	}

	execSession := <-exec.sessions
	if _, err := execSession.output.Write([]byte("container-hello\r\n")); err != nil {
		t.Fatalf("output: %v", err)
	}
	readUntilOutput(t, conn, "container-hello")

	sendFrameTest(t, conn, opInput, []byte("echo hi\r"))
	waitForTest(t, 5*time.Second, func() bool {
		return strings.Contains(execSession.inputString(), "echo hi\r")
	}, "input did not reach the container exec")

	sendFrameTest(t, conn, opResize, []byte{0, 132, 0, 43})
	waitForTest(t, 5*time.Second, func() bool {
		cols, rows := execSession.dimensions()
		return cols == 132 && rows == 43
	}, "resize did not reach the container exec")

	// Running `exit` ends the exec process; the server reports the exit event
	// on the same stream. Closing the browser socket afterwards must not stop
	// any application container (the fake has no container to stop).
	sendFrameTest(t, conn, opInput, []byte("exit\r"))
	opcode, payload = readFrame(t, conn)
	if opcode != opExited {
		t.Fatalf("opcode after exit = %#x, want exited (%s)", opcode, payload)
	}
	var exited exitedEvent
	if err := json.Unmarshal(payload, &exited); err != nil {
		t.Fatalf("decode exited: %v", err)
	}
	if exited.ExitCode != 0 {
		t.Fatalf("exit code = %d, want 0", exited.ExitCode)
	}
}

// waitForTest polls cond until true or the deadline expires.
func waitForTest(t *testing.T, timeout time.Duration, cond func() bool, message string) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal(message)
}
