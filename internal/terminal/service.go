// Package terminal provides control-plane host terminal sessions: an
// authenticated operator creates a session bound to one user, node and target,
// then attaches exactly one private WebSocket stream to it. The stream is
// relayed to the node agent's TerminalService over the existing CP→agent mTLS
// gRPC channel. Terminal bytes never touch Redis or the log feed.
//
// See docs/terminal-session-api.md for the full contract consumed by later work
// packages (container exec, the shared terminal view).
package terminal

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"google.golang.org/grpc"

	"github.com/justindeelux/gotham/internal/servers"
	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
)

// TargetHost runs a host shell on a platform operator's node. TargetContainer
// runs an interactive exec inside one application container resolved
// server-side; the two targets are separate routes, separate permissions and
// never fall back to each other.
const (
	TargetHost      = "host"
	TargetContainer = "container"
)

// Session limits and lifetimes.
const (
	// DefaultPendingTTL is how long a created session may wait for its WebSocket
	// before it expires. No PTY exists during this window.
	DefaultPendingTTL = 60 * time.Second
	// DefaultMaxDuration bounds an attached session. The agent enforces its own
	// cap independently.
	DefaultMaxDuration = 8 * time.Hour
	// DefaultMaxSessionsPerUser and DefaultMaxSessions bound one operator and
	// the whole control plane, so a script cannot open shells without limit.
	DefaultMaxSessionsPerUser = 4
	DefaultMaxSessions        = 64

	MaxRootBytes = 128
	MaxCwdBytes  = 4096
	MinDimension = 1
	MaxDimension = 1000
)

// Sentinel errors. Handlers map them onto HTTP/gRPC statuses.
var (
	// ErrNotFound covers both an unknown session and a session that belongs to
	// another user or node; callers must not distinguish the two.
	ErrNotFound = errors.New("terminal: session not found")
	// ErrAlreadyAttached is returned when a second stream tries to attach to a
	// session. A session is single-use: reconnecting always means a new shell.
	ErrAlreadyAttached = errors.New("terminal: session is already attached")
	// ErrTooManySessions is returned when a quota is reached.
	ErrTooManySessions = errors.New("terminal: too many sessions")
	// ErrUnsupportedTarget is returned for a target this package does not serve.
	ErrUnsupportedTarget = errors.New("terminal: unsupported target")
	// ErrInvalid is wrapped by every caller-input validation failure.
	ErrInvalid = errors.New("terminal: invalid request")
	// ErrForbidden is returned when the caller may read an application but may
	// not run a shell in it (a read-only team role).
	ErrForbidden = errors.New("terminal: application write access is required")
	// ErrNoRunningDeployment is returned when the application has no running
	// deployment with a container to attach to.
	ErrNoRunningDeployment = errors.New("terminal: application has no running deployment")
	// ErrContainerNotFound is returned when the selected container is gone,
	// replaced, or not part of this application's running deployment.
	ErrContainerNotFound = errors.New("terminal: the selected container is no longer available")
	// ErrContainerNotRunning is returned when the container exists but is
	// stopped or restarting.
	ErrContainerNotRunning = errors.New("terminal: the selected container is not running")
	// ErrDeployInProgress is returned while a deployment is replacing the
	// application's containers.
	ErrDeployInProgress = errors.New("terminal: a deployment is in progress")
	// ErrApplicationsDisabled is returned when the applications feature is off.
	ErrApplicationsDisabled = errors.New("terminal: application terminals are disabled")
	// ErrAgentUnavailable is returned when the node agent cannot be reached
	// while resolving a container target.
	ErrAgentUnavailable = errors.New("terminal: the node agent is unavailable")
)

// ContainerTarget is one running container an application terminal may attach
// to, as resolved from the application's deployment and node metadata. The
// container id is produced server-side and re-validated on create.
type ContainerTarget struct {
	Service       string
	ContainerID   string
	Name          string
	Image         string
	State         string
	Replica       int
	Label         string
	ServerID      uuid.UUID
	ApplicationID uuid.UUID
}

// ContainerTargetSet is the resolved terminal surface of one application.
type ContainerTargetSet struct {
	ApplicationID   uuid.UUID
	ApplicationName string
	ServerID        uuid.UUID
	SourceType      string
	Containers      []ContainerTarget
}

// ApplicationTargets resolves application container targets for a caller and
// proves application write access. It is implemented by an adapter over the
// deploy service; the transport package never imports the deploy domain.
type ApplicationTargets interface {
	// ListContainers returns the application's running containers.
	ListContainers(ctx context.Context, userID, appID uuid.UUID) (ContainerTargetSet, error)
	// ResolveContainer re-resolves the set and validates the selected service
	// and container id against it.
	ResolveContainer(ctx context.Context, userID, appID uuid.UUID, service, containerID string) (ContainerTargetSet, ContainerTarget, error)
}

// Target identifies what an authorized session may access. It is passed to the
// Authorizer at the handshake and on every revalidation tick so a host
// authorizer can never authorize a container stream (and vice versa).
type Target struct {
	Kind          string
	ServerID      uuid.UUID
	ApplicationID uuid.UUID
}

// AppCreateRequest is one application container terminal request.
type AppCreateRequest struct {
	Service     string `json:"service"`
	ContainerID string `json:"container_id"`
	Cols        uint32 `json:"cols"`
	Rows        uint32 `json:"rows"`
}

// rootPattern keeps a root name to the shape the agent generates.
var rootPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,127}$`)

// Registry resolves a managed node. *servers.ServerService implements it.
type Registry interface {
	Get(ctx context.Context, id uuid.UUID) (*servers.Server, error)
}

// TerminalClient is the agent-side stream client the relay consumes.
type TerminalClient interface {
	OpenTerminal(ctx context.Context, opts ...grpc.CallOption) (agentv1.TerminalService_OpenTerminalClient, error)
	Close() error
}

// DialFunc opens a TerminalClient for one node. The returned closer is closed
// when the relay ends.
type DialFunc func(ctx context.Context, server *servers.Server) (TerminalClient, error)

// CreateRequest is one operator's request to start a session.
type CreateRequest struct {
	Target string `json:"target"`
	// Root is an optional FileService root name. When it is empty the agent
	// starts the shell in its own default directory, so a host session does not
	// depend on the node having file roots configured.
	Root string `json:"root"`
	// Cwd is relative to Root; it is only valid together with a Root.
	Cwd  string `json:"cwd"`
	Cols uint32 `json:"cols"`
	Rows uint32 `json:"rows"`
}

// Session is a snapshot of a created session.
type Session struct {
	ID        uuid.UUID
	UserID    uuid.UUID
	ServerID  uuid.UUID
	Target    string
	Root      string
	Cwd       string
	Cols      uint32
	Rows      uint32
	CreatedAt time.Time
	ExpiresAt time.Time

	// Container target fields. They are empty for a host session.
	ApplicationID uuid.UUID
	Service       string
	ContainerID   string
	ContainerName string
	TargetLabel   string

	done <-chan struct{}
}

// Done closes when the service expires or releases the session.
func (s *Session) Done() <-chan struct{} { return s.done }

// Config configures a Service.
type Config struct {
	Registry Registry
	Dial     DialFunc
	// Applications resolves application container targets. nil leaves the
	// application routes unmounted/unavailable.
	Applications ApplicationTargets
	Logger       *slog.Logger

	// Now is a test seam; nil means time.Now.
	Now func() time.Time
	// PendingTTL, MaxDuration and the quotas default when zero.
	PendingTTL         time.Duration
	MaxDuration        time.Duration
	MaxSessionsPerUser int
	MaxSessions        int
}

// Service owns the control plane's terminal session registry. The registry is
// in-memory: a control-plane restart drops pending sessions, and an attached
// session's stream ends (the agent then tears its PTY down). Multi-instance
// control planes would need a shared registry; the transport deliberately does
// not use Redis for terminal I/O.
type Service struct {
	registry     Registry
	dial         DialFunc
	applications ApplicationTargets
	log          *slog.Logger

	now                func() time.Time
	pendingTTL         time.Duration
	maxDuration        time.Duration
	maxSessionsPerUser int
	maxSessions        int

	mu       sync.Mutex
	sessions map[uuid.UUID]*sessionState
	closed   bool
}

// sessionState is the mutable half of a session.
type sessionState struct {
	snapshot  *Session
	attached  bool
	timer     *time.Timer
	done      chan struct{}
	closeOnce sync.Once
}

// NewService builds a session registry. A nil registry or dialer makes Create
// fail with ErrNotFound / the relay fail as unavailable; Mount still works so
// tests can exercise validation and authentication.
func NewService(cfg Config) *Service {
	log := cfg.Logger
	if log == nil {
		log = slog.Default()
	}
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	pendingTTL := cfg.PendingTTL
	if pendingTTL <= 0 {
		pendingTTL = DefaultPendingTTL
	}
	maxDuration := cfg.MaxDuration
	if maxDuration <= 0 {
		maxDuration = DefaultMaxDuration
	}
	maxPerUser := cfg.MaxSessionsPerUser
	if maxPerUser <= 0 {
		maxPerUser = DefaultMaxSessionsPerUser
	}
	maxSessions := cfg.MaxSessions
	if maxSessions <= 0 {
		maxSessions = DefaultMaxSessions
	}
	return &Service{
		registry:           cfg.Registry,
		dial:               cfg.Dial,
		applications:       cfg.Applications,
		log:                log,
		now:                now,
		pendingTTL:         pendingTTL,
		maxDuration:        maxDuration,
		maxSessionsPerUser: maxPerUser,
		maxSessions:        maxSessions,
		sessions:           make(map[uuid.UUID]*sessionState),
	}
}

// Create validates a request, resolves the node and records a pending session.
func (s *Service) Create(ctx context.Context, userID, serverID uuid.UUID, req CreateRequest) (*Session, error) {
	if userID == uuid.Nil || serverID == uuid.Nil {
		return nil, fmt.Errorf("%w: user and server are required", ErrInvalid)
	}
	target, err := normalizeTarget(req.Target)
	if err != nil {
		return nil, err
	}
	if err := validateRoot(req.Root); err != nil {
		return nil, err
	}
	if err := validateCwd(req.Cwd); err != nil {
		return nil, err
	}
	if req.Cwd != "" && req.Root == "" {
		return nil, fmt.Errorf("%w: a working directory requires a file root", ErrInvalid)
	}
	if err := validateDimensions(req.Cols, req.Rows); err != nil {
		return nil, err
	}
	if s.registry == nil {
		return nil, fmt.Errorf("%w: no server registry", ErrNotFound)
	}
	server, err := s.registry.Get(ctx, serverID)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrNotFound, err)
	}
	if server == nil {
		return nil, ErrNotFound
	}

	now := s.now()
	session := &sessionState{
		snapshot: &Session{
			ID:        uuid.New(),
			UserID:    userID,
			ServerID:  serverID,
			Target:    target,
			Root:      req.Root,
			Cwd:       req.Cwd,
			Cols:      req.Cols,
			Rows:      req.Rows,
			CreatedAt: now,
			ExpiresAt: now.Add(s.pendingTTL),
		},
		done: make(chan struct{}),
	}
	session.snapshot.done = session.done
	return s.register(userID, session)
}

// CreateApplication resolves an application's running containers, validates the
// selected target server-side and records a pending container session. The
// caller's application write access is proven by the resolver before any node
// call.
func (s *Service) CreateApplication(ctx context.Context, userID, appID uuid.UUID, req AppCreateRequest) (*Session, error) {
	if userID == uuid.Nil || appID == uuid.Nil {
		return nil, fmt.Errorf("%w: user and application are required", ErrInvalid)
	}
	if err := validateDimensions(req.Cols, req.Rows); err != nil {
		return nil, err
	}
	if err := validateContainerSelection(req.Service, req.ContainerID); err != nil {
		return nil, err
	}
	if s.applications == nil {
		return nil, fmt.Errorf("%w: application terminals are not configured", ErrUnsupportedTarget)
	}
	_, target, err := s.applications.ResolveContainer(ctx, userID, appID, req.Service, req.ContainerID)
	if err != nil {
		return nil, err
	}
	if target.ServerID == uuid.Nil || target.ContainerID == "" {
		return nil, fmt.Errorf("%w: the application has no resolvable container", ErrContainerNotFound)
	}
	if s.registry != nil {
		if server, err := s.registry.Get(ctx, target.ServerID); err != nil || server == nil {
			return nil, fmt.Errorf("%w: the node is unavailable", ErrNotFound)
		}
	}
	now := s.now()
	session := &sessionState{
		snapshot: &Session{
			ID:            uuid.New(),
			UserID:        userID,
			ServerID:      target.ServerID,
			Target:        TargetContainer,
			Cols:          req.Cols,
			Rows:          req.Rows,
			CreatedAt:     now,
			ExpiresAt:     now.Add(s.pendingTTL),
			ApplicationID: appID,
			Service:       target.Service,
			ContainerID:   target.ContainerID,
			ContainerName: target.Name,
			TargetLabel:   target.Label,
		},
		done: make(chan struct{}),
	}
	session.snapshot.done = session.done
	return s.register(userID, session)
}

// register applies the global and per-user quotas and installs the pending
// expiry timer.
func (s *Service) register(userID uuid.UUID, session *sessionState) (*Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, fmt.Errorf("%w: the control plane is shutting down", ErrInvalid)
	}
	if len(s.sessions) >= s.maxSessions {
		return nil, ErrTooManySessions
	}
	perUser := 0
	for _, existing := range s.sessions {
		if existing.snapshot.UserID == userID {
			perUser++
		}
	}
	if perUser >= s.maxSessionsPerUser {
		return nil, ErrTooManySessions
	}
	s.sessions[session.snapshot.ID] = session
	session.timer = time.AfterFunc(s.pendingTTL, func() { s.expire(session.snapshot.ID) })
	return cloneSession(session.snapshot, session.done), nil
}

// validateContainerSelection bounds the browser-supplied service and container
// id. The value is not trusted: the resolver re-resolves it against the
// application's running set; this only rejects malformed input early.
func validateContainerSelection(service, containerID string) error {
	if len(service) > 128 || strings.IndexByte(service, 0) >= 0 {
		return fmt.Errorf("%w: invalid service name", ErrInvalid)
	}
	containerID = strings.TrimSpace(containerID)
	if containerID == "" || len(containerID) > 128 {
		return fmt.Errorf("%w: a container must be selected", ErrInvalid)
	}
	for i := 0; i < len(containerID); i++ {
		c := containerID[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		case c == '_', c == '.', c == ':', c == '-':
		default:
			return fmt.Errorf("%w: invalid container id", ErrInvalid)
		}
	}
	return nil
}

// Validate reports whether a session exists, is owned by the caller and node,
// and is still attachable, without marking it attached. The WebSocket handler
// runs it before the upgrade so a non-upgrade request cannot burn the session;
// Attach then marks it in the handshake itself.
func (s *Service) Validate(_ context.Context, userID, serverID, sessionID uuid.UUID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	session, ok := s.sessions[sessionID]
	if !ok || session.snapshot.Target != TargetHost {
		return ErrNotFound
	}
	if session.snapshot.UserID != userID || session.snapshot.ServerID != serverID {
		return ErrNotFound
	}
	if session.attached {
		return ErrAlreadyAttached
	}
	return nil
}

// ValidateApplication is the container-session mirror of Validate. It answers
// the snapshot so the WebSocket handler can pass the session's target to the
// authorizer before the upgrade.
func (s *Service) ValidateApplication(_ context.Context, userID, appID, sessionID uuid.UUID) (*Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	session, ok := s.sessions[sessionID]
	if !ok || session.snapshot.UserID != userID || session.snapshot.ApplicationID != appID ||
		session.snapshot.Target != TargetContainer {
		return nil, ErrNotFound
	}
	if session.attached {
		return nil, ErrAlreadyAttached
	}
	return cloneSession(session.snapshot, session.done), nil
}

// AttachApplication marks a pending container session attached. It mirrors
// Attach's single-use rule and ownership checks.
func (s *Service) AttachApplication(_ context.Context, userID, appID, sessionID uuid.UUID) (*Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	session, ok := s.sessions[sessionID]
	if !ok || session.snapshot.UserID != userID || session.snapshot.ApplicationID != appID ||
		session.snapshot.Target != TargetContainer {
		return nil, ErrNotFound
	}
	if session.attached {
		return nil, ErrAlreadyAttached
	}
	session.attached = true
	if session.timer != nil {
		session.timer.Stop()
	}
	session.timer = time.AfterFunc(s.pendingTTL, func() { s.expire(sessionID) })
	return cloneSession(session.snapshot, session.done), nil
}

// RevalidateApplicationTarget re-resolves a pending session's container through
// the application resolver before the WebSocket upgrade. The create-time
// resolution is not trusted for the 60 s pending window: a replaced container,
// a stopped target or a deployment that started in between answers the same
// actionable error as create, before any agent stream is opened.
func (s *Service) RevalidateApplicationTarget(ctx context.Context, userID, appID uuid.UUID, session *Session) error {
	if session == nil || session.Target != TargetContainer || session.ApplicationID != appID {
		return ErrNotFound
	}
	if s.applications == nil {
		return fmt.Errorf("%w: application terminals are not configured", ErrUnsupportedTarget)
	}
	_, target, err := s.applications.ResolveContainer(ctx, userID, appID, session.Service, session.ContainerID)
	if err != nil {
		return err
	}
	if target.ServerID != session.ServerID || target.Service != session.Service ||
		target.ContainerID != session.ContainerID {
		return fmt.Errorf("%w: the selected container changed since the session was created; refresh the targets", ErrContainerNotFound)
	}
	return nil
}

// CloseApplication releases a container session on behalf of its owner.
func (s *Service) CloseApplication(_ context.Context, userID, appID, sessionID uuid.UUID) error {
	s.mu.Lock()
	session, ok := s.sessions[sessionID]
	if !ok || session.snapshot.UserID != userID || session.snapshot.ApplicationID != appID ||
		session.snapshot.Target != TargetContainer {
		s.mu.Unlock()
		return ErrNotFound
	}
	delete(s.sessions, sessionID)
	s.mu.Unlock()
	s.finish(session)
	return nil
}

// Attach marks a pending session attached and returns its snapshot. A session
// can be attached once; a second call is ErrAlreadyAttached. A session that
// belongs to another user or node answers ErrNotFound.
func (s *Service) Attach(_ context.Context, userID, serverID, sessionID uuid.UUID) (*Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	session, ok := s.sessions[sessionID]
	if !ok || session.snapshot.Target != TargetHost {
		return nil, ErrNotFound
	}
	if session.snapshot.UserID != userID || session.snapshot.ServerID != serverID {
		return nil, ErrNotFound
	}
	if session.attached {
		return nil, ErrAlreadyAttached
	}
	session.attached = true
	if session.timer != nil {
		session.timer.Stop()
	}
	// The upgrade may still fail; keep the short pending lifetime until the
	// relay confirms it owns a live socket.
	session.timer = time.AfterFunc(s.pendingTTL, func() { s.expire(sessionID) })
	return cloneSession(session.snapshot, session.done), nil
}

// Confirm records that an attached session's WebSocket is live and switches its
// lifetime from the short handshake window to the full session duration. It is
// idempotent and a no-op for a session that already ended.
func (s *Service) Confirm(sessionID uuid.UUID) {
	s.mu.Lock()
	defer s.mu.Unlock()
	session, ok := s.sessions[sessionID]
	if !ok || !session.attached {
		return
	}
	if session.timer != nil {
		session.timer.Stop()
	}
	session.snapshot.ExpiresAt = s.now().Add(s.maxDuration)
	session.timer = time.AfterFunc(s.maxDuration, func() { s.expire(sessionID) })
}

// Release removes a session and signals its Done channel. It is idempotent.
func (s *Service) Release(sessionID uuid.UUID) {
	s.mu.Lock()
	session, ok := s.sessions[sessionID]
	if ok {
		delete(s.sessions, sessionID)
	}
	s.mu.Unlock()
	if ok {
		s.finish(session)
	}
}

// Close releases a session on behalf of its owner. A session owned by another
// user or node answers ErrNotFound.
func (s *Service) Close(_ context.Context, userID, serverID, sessionID uuid.UUID) error {
	s.mu.Lock()
	session, ok := s.sessions[sessionID]
	if !ok || session.snapshot.Target != TargetHost ||
		session.snapshot.UserID != userID || session.snapshot.ServerID != serverID {
		s.mu.Unlock()
		return ErrNotFound
	}
	delete(s.sessions, sessionID)
	s.mu.Unlock()
	s.finish(session)
	return nil
}

// CloseAll tears down every session. It is called on control-plane shutdown.
func (s *Service) CloseAll() {
	s.mu.Lock()
	s.closed = true
	sessions := make([]*sessionState, 0, len(s.sessions))
	for id, session := range s.sessions {
		delete(s.sessions, id)
		sessions = append(sessions, session)
	}
	s.mu.Unlock()
	for _, session := range sessions {
		s.finish(session)
	}
}

// Dial opens the agent client for a session's node.
func (s *Service) Dial(ctx context.Context, server *servers.Server) (TerminalClient, error) {
	if s.dial == nil {
		return nil, errors.New("terminal: the agent dialer is not configured")
	}
	return s.dial(ctx, server)
}

// Registry exposes the node resolver to the relay.
func (s *Service) Registry() Registry { return s.registry }

// Applications exposes the application target resolver to the HTTP handlers.
func (s *Service) Applications() ApplicationTargets { return s.applications }

// SetApplications plugs the application target resolver after construction. The
// server builds the deploy service after the terminal registry, so the wiring
// cannot be part of Config there.
func (s *Service) SetApplications(apps ApplicationTargets) { s.applications = apps }

// Logger exposes the service logger.
func (s *Service) Logger() *slog.Logger { return s.log }

// TargetFor returns the authorization target of one session.
func TargetFor(session *Session) Target {
	if session == nil {
		return Target{}
	}
	return Target{
		Kind:          session.Target,
		ServerID:      session.ServerID,
		ApplicationID: session.ApplicationID,
	}
}

// expire removes a session and closes its Done channel.
func (s *Service) expire(sessionID uuid.UUID) {
	s.mu.Lock()
	session, ok := s.sessions[sessionID]
	if ok {
		delete(s.sessions, sessionID)
	}
	s.mu.Unlock()
	if ok {
		s.finish(session)
	}
}

func (s *Service) finish(session *sessionState) {
	if session.timer != nil {
		session.timer.Stop()
	}
	session.closeOnce.Do(func() { close(session.done) })
}

// cloneSession copies the snapshot so a caller cannot mutate registry state.
func cloneSession(session *Session, done <-chan struct{}) *Session {
	clone := *session
	clone.done = done
	return &clone
}

// normalizeTarget maps the empty target to host and refuses anything else.
func normalizeTarget(target string) (string, error) {
	switch strings.TrimSpace(target) {
	case "", TargetHost:
		return TargetHost, nil
	default:
		return "", fmt.Errorf("%w: %q", ErrUnsupportedTarget, target)
	}
}

// validateRoot accepts an omitted root: a host session without a File manager
// root starts in a default directory the agent chooses. A provided root keeps
// the exact bounded-name shape the agent generates.
func validateRoot(root string) error {
	if root == "" {
		return nil
	}
	if !rootPattern.MatchString(root) {
		return fmt.Errorf("%w: root must match %s", ErrInvalid, rootPattern)
	}
	return nil
}

func validateCwd(cwd string) error {
	if len(cwd) > MaxCwdBytes || strings.IndexByte(cwd, 0) >= 0 {
		return fmt.Errorf("%w: invalid working directory", ErrInvalid)
	}
	if strings.HasPrefix(cwd, "/") || strings.HasPrefix(cwd, `\`) {
		return fmt.Errorf("%w: working directory must be relative to the root", ErrInvalid)
	}
	for _, segment := range strings.FieldsFunc(cwd, func(r rune) bool { return r == '/' || r == '\\' }) {
		if segment == ".." {
			return fmt.Errorf("%w: working directory must not contain a parent segment", ErrInvalid)
		}
	}
	return nil
}

func validateDimensions(cols, rows uint32) error {
	if cols < MinDimension || cols > MaxDimension || rows < MinDimension || rows > MaxDimension {
		return fmt.Errorf("%w: dimensions must be between %d and %d", ErrInvalid, MinDimension, MaxDimension)
	}
	return nil
}

// ValidateDimensions is the shared dimension check for resize frames.
func ValidateDimensions(cols, rows uint32) error { return validateDimensions(cols, rows) }
