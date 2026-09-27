package containers

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/justindeelux/gotham/internal/servers"
	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
)

// Timeout defaults bound agent RPCs. Pulls and runs transfer image layers, so
// they get a longer budget than listings and lifecycle actions.
const (
	defaultRPCTimeout  = 30 * time.Second
	defaultPullTimeout = 5 * time.Minute
)

// ContainerService routes Docker commands for one node to that node's agent.
// The control plane never calls Docker directly.
type ContainerService interface {
	List(ctx context.Context, serverID uuid.UUID) ([]Container, error)
	Start(ctx context.Context, serverID uuid.UUID, containerID string) error
	Stop(ctx context.Context, serverID uuid.UUID, containerID string) error
	Restart(ctx context.Context, serverID uuid.UUID, containerID string) error
	Pull(ctx context.Context, serverID uuid.UUID, image string) error
	Run(ctx context.Context, serverID uuid.UUID, opts RunOptions) (string, error)
}

// Config wires a Service. Registry and Cache are required inputs except that
// an empty RedisAddr falls back to the default and a nil Cache disables
// caching; Dial may be nil until the P3-CONN mTLS dialer is plugged in.
type Config struct {
	Registry    Registry
	Dial        DialFunc
	RedisAddr   string
	Cache       Cache
	Logger      *slog.Logger
	RPCTimeout  time.Duration
	PullTimeout time.Duration
}

// Service is the control-plane container domain service.
type Service struct {
	mu          sync.RWMutex
	registry    Registry
	dial        DialFunc
	cache       Cache
	logger      *slog.Logger
	rpcTimeout  time.Duration
	pullTimeout time.Duration
}

// NewService builds a Service. When cfg.Cache is nil a Redis cache over
// cfg.RedisAddr is used; pass NopCache to disable caching explicitly.
func NewService(cfg Config) *Service {
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	cache := cfg.Cache
	if cache == nil {
		cache = NewRedisCache(cfg.RedisAddr)
	}
	rpcTimeout := cfg.RPCTimeout
	if rpcTimeout <= 0 {
		rpcTimeout = defaultRPCTimeout
	}
	pullTimeout := cfg.PullTimeout
	if pullTimeout <= 0 {
		pullTimeout = defaultPullTimeout
	}
	return &Service{
		registry:    cfg.Registry,
		dial:        cfg.Dial,
		cache:       cache,
		logger:      logger,
		rpcTimeout:  rpcTimeout,
		pullTimeout: pullTimeout,
	}
}

// NewDefaultService builds the production Service for the HTTP wiring: node
// registry plus a Redis cache over redisAddr. The agent dialer stays nil until
// the P3-CONN mTLS dial is plugged in via SetDial. It returns nil when
// registry is nil so callers can skip mounting the routes.
func NewDefaultService(registry Registry, redisAddr string) *Service {
	if registry == nil {
		return nil
	}
	return NewService(Config{Registry: registry, RedisAddr: redisAddr})
}

// SetDial installs (or replaces) the agent dialer. It is the merge-time seam
// for the P3-CONN mTLS dial.
func (s *Service) SetDial(dial DialFunc) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.dial = dial
}

// Close releases the cache when it owns resources (Redis). NopCache and test
// fakes are unaffected.
func (s *Service) Close() error {
	if closer, ok := s.cache.(interface{ Close() error }); ok {
		return closer.Close()
	}
	return nil
}

// List returns the containers on a node, serving the Redis cache (10s TTL)
// when it hits and degrading to a direct agent call otherwise.
func (s *Service) List(ctx context.Context, serverID uuid.UUID) ([]Container, error) {
	if _, err := s.resolve(ctx, serverID); err != nil {
		return nil, err
	}

	if cached, ok, err := s.cache.Get(ctx, serverID); err != nil {
		s.logger.Debug("containers: cache lookup failed; calling agent", "server_id", serverID.String(), "error", err)
	} else if ok {
		return cached, nil
	}

	client, cancel, err := s.client(ctx, serverID)
	if err != nil {
		return nil, err
	}
	defer cancel()

	response, err := client.ListContainers(ctx, &agentv1.ListContainersRequest{All: true})
	if err != nil {
		return nil, mapRPCError(err)
	}

	containers := make([]Container, 0, len(response.GetContainers()))
	for _, info := range response.GetContainers() {
		containers = append(containers, newContainer(info))
	}
	if err := s.cache.Set(ctx, serverID, containers); err != nil {
		s.logger.Debug("containers: cache store failed", "server_id", serverID.String(), "error", err)
	}
	return containers, nil
}

// Start starts a container and invalidates the cached list.
func (s *Service) Start(ctx context.Context, serverID uuid.UUID, containerID string) error {
	if strings.TrimSpace(containerID) == "" {
		return fmt.Errorf("%w: container id is required", ErrValidation)
	}
	client, cancel, err := s.client(ctx, serverID)
	if err != nil {
		return err
	}
	defer cancel()

	if _, err := client.StartContainer(ctx, &agentv1.ContainerActionRequest{ContainerId: containerID}); err != nil {
		return mapRPCError(err)
	}
	s.invalidate(ctx, serverID)
	return nil
}

// Stop stops a container and invalidates the cached list.
func (s *Service) Stop(ctx context.Context, serverID uuid.UUID, containerID string) error {
	if strings.TrimSpace(containerID) == "" {
		return fmt.Errorf("%w: container id is required", ErrValidation)
	}
	client, cancel, err := s.client(ctx, serverID)
	if err != nil {
		return err
	}
	defer cancel()

	if _, err := client.StopContainer(ctx, &agentv1.ContainerActionRequest{ContainerId: containerID}); err != nil {
		return mapRPCError(err)
	}
	s.invalidate(ctx, serverID)
	return nil
}

// Restart restarts a container and invalidates the cached list.
func (s *Service) Restart(ctx context.Context, serverID uuid.UUID, containerID string) error {
	if strings.TrimSpace(containerID) == "" {
		return fmt.Errorf("%w: container id is required", ErrValidation)
	}
	client, cancel, err := s.client(ctx, serverID)
	if err != nil {
		return err
	}
	defer cancel()

	if _, err := client.RestartContainer(ctx, &agentv1.ContainerActionRequest{ContainerId: containerID}); err != nil {
		return mapRPCError(err)
	}
	s.invalidate(ctx, serverID)
	return nil
}

// Pull pulls an image on the node and invalidates the cached list.
func (s *Service) Pull(ctx context.Context, serverID uuid.UUID, image string) error {
	if strings.TrimSpace(image) == "" {
		return fmt.Errorf("%w: image is required", ErrValidation)
	}
	client, cancel, err := s.pullClient(ctx, serverID)
	if err != nil {
		return err
	}
	defer cancel()

	if _, err := client.PullImage(ctx, &agentv1.PullImageRequest{Image: strings.TrimSpace(image)}); err != nil {
		return mapRPCError(err)
	}
	s.invalidate(ctx, serverID)
	return nil
}

// Run creates and starts a container from opts, returning the new container
// ID, and invalidates the cached list.
func (s *Service) Run(ctx context.Context, serverID uuid.UUID, opts RunOptions) (string, error) {
	if err := opts.validate(); err != nil {
		return "", err
	}
	client, cancel, err := s.pullClient(ctx, serverID)
	if err != nil {
		return "", err
	}
	defer cancel()

	response, err := client.RunImage(ctx, opts.toProto())
	if err != nil {
		return "", mapRPCError(err)
	}
	s.invalidate(ctx, serverID)
	return response.GetContainerId(), nil
}

// resolve returns the registry entry for a server, mapping a missing row to
// ErrServerNotFound.
func (s *Service) resolve(ctx context.Context, serverID uuid.UUID) (*servers.Server, error) {
	if s.registry == nil {
		return nil, errors.New("containers: server registry is not configured")
	}
	server, err := s.registry.Get(ctx, serverID)
	if err != nil {
		if errors.Is(err, servers.ErrNotFound) {
			return nil, ErrServerNotFound
		}
		return nil, fmt.Errorf("containers: resolve server: %w", err)
	}
	return server, nil
}

// client resolves the server and dials its agent with the standard RPC
// timeout.
func (s *Service) client(ctx context.Context, serverID uuid.UUID) (DockerClient, context.CancelFunc, error) {
	ctx, cancel := context.WithTimeout(ctx, s.rpcTimeout)
	server, err := s.resolve(ctx, serverID)
	if err != nil {
		cancel()
		return nil, nil, err
	}
	client, err := s.dialClient(ctx, server)
	if err != nil {
		cancel()
		return nil, nil, err
	}
	return client, cancel, nil
}

// pullClient is client with the longer image-transfer timeout.
func (s *Service) pullClient(ctx context.Context, serverID uuid.UUID) (DockerClient, context.CancelFunc, error) {
	ctx, cancel := context.WithTimeout(ctx, s.pullTimeout)
	server, err := s.resolve(ctx, serverID)
	if err != nil {
		cancel()
		return nil, nil, err
	}
	client, err := s.dialClient(ctx, server)
	if err != nil {
		cancel()
		return nil, nil, err
	}
	return client, cancel, nil
}

// dialClient opens the agent client, reporting a clear error while no dialer
// is configured.
func (s *Service) dialClient(ctx context.Context, server *servers.Server) (DockerClient, error) {
	s.mu.RLock()
	dial := s.dial
	s.mu.RUnlock()
	if dial == nil {
		return nil, fmt.Errorf("%w: agent dialer is not configured", ErrAgentUnavailable)
	}
	client, err := dial(ctx, server)
	if err != nil {
		return nil, mapRPCError(err)
	}
	if client == nil {
		return nil, fmt.Errorf("%w: agent dial returned no client", ErrAgentUnavailable)
	}
	return client, nil
}

// invalidate drops the cached list; failures only degrade to a stale read
// until the TTL expires, so they are logged and ignored.
func (s *Service) invalidate(ctx context.Context, serverID uuid.UUID) {
	if err := s.cache.Invalidate(ctx, serverID); err != nil {
		s.logger.Debug("containers: cache invalidate failed", "server_id", serverID.String(), "error", err)
	}
}

// mapRPCError translates agent transport failures to service sentinels;
// unrecognised errors pass through unchanged.
func mapRPCError(err error) error {
	if err == nil {
		return nil
	}
	switch status.Code(err) {
	case codes.NotFound:
		return fmt.Errorf("%w: %v", ErrContainerNotFound, err)
	case codes.InvalidArgument:
		return fmt.Errorf("%w: %v", ErrValidation, err)
	case codes.Unavailable, codes.DeadlineExceeded, codes.Canceled:
		return fmt.Errorf("%w: %v", ErrAgentUnavailable, err)
	default:
		return err
	}
}
