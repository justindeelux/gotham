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

const (
	TargetHost      = "host"
	TargetContainer = "container"
)

const (
	DefaultPendingTTL         = 60 * time.Second
	DefaultMaxDuration        = 8 * time.Hour
	DefaultMaxSessionsPerUser = 4
	DefaultMaxSessions        = 64

	MaxRootBytes = 128
	MaxCwdBytes  = 4096
	MinDimension = 1
	MaxDimension = 1000
)

var (
	ErrNotFound             = errors.New("terminal: session not found")
	ErrAlreadyAttached      = errors.New("terminal: session is already attached")
	ErrTooManySessions      = errors.New("terminal: too many sessions")
	ErrUnsupportedTarget    = errors.New("terminal: unsupported target")
	ErrInvalid              = errors.New("terminal: invalid request")
	ErrForbidden            = errors.New("terminal: application write access is required")
	ErrNoRunningDeployment  = errors.New("terminal: application has no running deployment")
	ErrContainerNotFound    = errors.New("terminal: the selected container is no longer available")
	ErrContainerNotRunning  = errors.New("terminal: the selected container is not running")
	ErrDeployInProgress     = errors.New("terminal: a deployment is in progress")
	ErrApplicationsDisabled = errors.New("terminal: application terminals are disabled")
	ErrAgentUnavailable     = errors.New("terminal: the node agent is unavailable")
)

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

type ContainerTargetSet struct {
	ApplicationID   uuid.UUID
	ApplicationName string
	ServerID        uuid.UUID
	SourceType      string
	Containers      []ContainerTarget
}

type ApplicationTargets interface {
	ListContainers(ctx context.Context, userID, appID uuid.UUID) (ContainerTargetSet, error)
	ResolveContainer(ctx context.Context, userID, appID uuid.UUID, service, containerID string) (ContainerTargetSet, ContainerTarget, error)
}

type Target struct {
	Kind          string
	ServerID      uuid.UUID
	ApplicationID uuid.UUID
}

type AppCreateRequest struct {
	Service     string `json:"service"`
	ContainerID string `json:"container_id"`
	Cols        uint32 `json:"cols"`
	Rows        uint32 `json:"rows"`
}

var rootPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,127}$`)

type Registry interface {
	Get(ctx context.Context, id uuid.UUID) (*servers.Server, error)
}

type TerminalClient interface {
	OpenTerminal(ctx context.Context, opts ...grpc.CallOption) (agentv1.TerminalService_OpenTerminalClient, error)
	Close() error
}

type DialFunc func(ctx context.Context, server *servers.Server) (TerminalClient, error)

type CreateRequest struct {
	Target string `json:"target"`
	Root   string `json:"root"`
	Cwd    string `json:"cwd"`
	Cols   uint32 `json:"cols"`
	Rows   uint32 `json:"rows"`
}

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

	ApplicationID uuid.UUID
	Service       string
	ContainerID   string
	ContainerName string
	TargetLabel   string

	done <-chan struct{}
}

func (s *Session) Done() <-chan struct{} { return s.done }

type Config struct {
	Registry     Registry
	Dial         DialFunc
	Applications ApplicationTargets
	Logger       *slog.Logger

	Now                func() time.Time
	PendingTTL         time.Duration
	MaxDuration        time.Duration
	MaxSessionsPerUser int
	MaxSessions        int
}

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

type sessionState struct {
	snapshot  *Session
	attached  bool
	timer     *time.Timer
	done      chan struct{}
	closeOnce sync.Once
}

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
	session.timer = time.AfterFunc(s.pendingTTL, func() { s.expire(sessionID) })
	return cloneSession(session.snapshot, session.done), nil
}

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

func (s *Service) Dial(ctx context.Context, server *servers.Server) (TerminalClient, error) {
	if s.dial == nil {
		return nil, errors.New("terminal: the agent dialer is not configured")
	}
	return s.dial(ctx, server)
}

func (s *Service) Registry() Registry { return s.registry }

func (s *Service) Applications() ApplicationTargets { return s.applications }

func (s *Service) SetApplications(apps ApplicationTargets) { s.applications = apps }

func (s *Service) Logger() *slog.Logger { return s.log }

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

func cloneSession(session *Session, done <-chan struct{}) *Session {
	clone := *session
	clone.done = done
	return &clone
}

func normalizeTarget(target string) (string, error) {
	switch strings.TrimSpace(target) {
	case "", TargetHost:
		return TargetHost, nil
	default:
		return "", fmt.Errorf("%w: %q", ErrUnsupportedTarget, target)
	}
}

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

func ValidateDimensions(cols, rows uint32) error { return validateDimensions(cols, rows) }
