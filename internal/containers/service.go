package containers

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/justindeelux/gotham/internal/servers"
	"github.com/justindeelux/gotham/internal/teams"
	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
)

// Timeout defaults bound agent RPCs. Pulls and runs transfer image layers, so
// they get a longer budget than listings and lifecycle actions. Log streams
// are long-lived by nature — a backup job tails its temporary container until
// the dump finishes — so they get their own, much larger budget.
const (
	defaultRPCTimeout  = 30 * time.Second
	defaultPullTimeout = 5 * time.Minute
	defaultLogTimeout  = 30 * time.Minute
)

// ContainerService routes Docker commands for one node to that node's agent.
// The control plane never calls Docker directly.
type ContainerService interface {
	List(ctx context.Context, serverID uuid.UUID) ([]Container, error)
	Start(ctx context.Context, serverID uuid.UUID, containerID string) error
	Stop(ctx context.Context, serverID uuid.UUID, containerID string) error
	Restart(ctx context.Context, serverID uuid.UUID, containerID string) error
	// Remove force-removes a container (idempotent: an already-gone container
	// is not an error). Named volumes are kept, so removing a container never
	// deletes its data.
	Remove(ctx context.Context, serverID uuid.UUID, containerID string) error
	Pull(ctx context.Context, serverID uuid.UUID, image string) error
	Run(ctx context.Context, serverID uuid.UUID, opts RunOptions) (string, error)
	// Logs streams a container's stdout and stderr payloads (the agent merges
	// both) until the stream ends — with follow, Docker closes it when the
	// container stops — or ctx is cancelled. chunks is closed on both. streamErr
	// receives a non-nil terminal error when the agent stream failed before a
	// clean end, so a caller never mistakes a truncated stream for success.
	Logs(ctx context.Context, serverID uuid.UUID, containerID string, follow bool) (<-chan []byte, <-chan error, error)
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
	LogTimeout  time.Duration
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
	logTimeout  time.Duration
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
	logTimeout := cfg.LogTimeout
	if logTimeout <= 0 {
		logTimeout = defaultLogTimeout
	}
	return &Service{
		registry:    cfg.Registry,
		dial:        cfg.Dial,
		cache:       cache,
		logger:      logger,
		rpcTimeout:  rpcTimeout,
		pullTimeout: pullTimeout,
		logTimeout:  logTimeout,
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
	if _, err := s.resolve(ctx, serverID, false); err != nil {
		return nil, err
	}

	if cached, ok, err := s.cache.Get(ctx, serverID); err != nil {
		s.logger.Debug("containers: cache lookup failed; calling agent", "server_id", serverID.String(), "error", err)
	} else if ok {
		return cached, nil
	}

	ctx, client, cancel, err := s.client(ctx, serverID, false)
	if err != nil {
		return nil, err
	}
	defer cancel()
	defer s.closeClient(client)

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

// ListFresh returns the node's containers straight from the agent, bypassing
// the List cache. Routing decisions use it: a container started by the deploy
// orchestrator never touches this cache, so a stale UI list must not hide a
// just-started application's published port.
func (s *Service) ListFresh(ctx context.Context, serverID uuid.UUID) ([]Container, error) {
	if _, err := s.resolve(ctx, serverID, false); err != nil {
		return nil, err
	}
	ctx, client, cancel, err := s.client(ctx, serverID, true)
	if err != nil {
		return nil, err
	}
	defer cancel()
	defer s.closeClient(client)

	response, err := client.ListContainers(ctx, &agentv1.ListContainersRequest{All: true})
	if err != nil {
		return nil, mapRPCError(err)
	}
	containers := make([]Container, 0, len(response.GetContainers()))
	for _, info := range response.GetContainers() {
		containers = append(containers, newContainer(info))
	}
	return containers, nil
}

// Start starts a container and invalidates the cached list.
func (s *Service) Start(ctx context.Context, serverID uuid.UUID, containerID string) error {
	if strings.TrimSpace(containerID) == "" {
		return fmt.Errorf("%w: container id is required", ErrValidation)
	}
	ctx, client, cancel, err := s.client(ctx, serverID, true)
	if err != nil {
		return err
	}
	defer cancel()
	defer s.closeClient(client)

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
	ctx, client, cancel, err := s.client(ctx, serverID, true)
	if err != nil {
		return err
	}
	defer cancel()
	defer s.closeClient(client)

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
	ctx, client, cancel, err := s.client(ctx, serverID, true)
	if err != nil {
		return err
	}
	defer cancel()
	defer s.closeClient(client)

	if _, err := client.RestartContainer(ctx, &agentv1.ContainerActionRequest{ContainerId: containerID}); err != nil {
		return mapRPCError(err)
	}
	s.invalidate(ctx, serverID)
	return nil
}

// Remove force-removes a container and invalidates the cached list. The agent
// treats an already-gone container as success, so a retry of a delete stays
// idempotent; named volumes survive the removal.
func (s *Service) Remove(ctx context.Context, serverID uuid.UUID, containerID string) error {
	if strings.TrimSpace(containerID) == "" {
		return fmt.Errorf("%w: container id is required", ErrValidation)
	}
	ctx, client, cancel, err := s.client(ctx, serverID, true)
	if err != nil {
		return err
	}
	defer cancel()
	defer s.closeClient(client)

	if _, err := client.RemoveContainer(ctx, &agentv1.ContainerActionRequest{ContainerId: containerID}); err != nil {
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
	ctx, client, cancel, err := s.pullClient(ctx, serverID)
	if err != nil {
		return err
	}
	defer cancel()
	defer s.closeClient(client)

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
	ctx, client, cancel, err := s.pullClient(ctx, serverID)
	if err != nil {
		return "", err
	}
	defer cancel()
	defer s.closeClient(client)

	response, err := client.RunImage(ctx, opts.toProto())
	if err != nil {
		return "", mapRPCError(err)
	}
	s.invalidate(ctx, serverID)
	return response.GetContainerId(), nil
}

// Logs streams a container's stdout and stderr payloads until the stream ends
// or ctx is cancelled. With follow the agent tails the container, and Docker
// closes the stream when it stops — which is how a job waits for its
// temporary container to finish without an Exec or Wait RPC.
//
// The stream lives in its own context bounded by Config.LogTimeout rather than
// the short RPC timeout: a dump of a large database runs for minutes.
func (s *Service) Logs(ctx context.Context, serverID uuid.UUID, containerID string, follow bool) (<-chan []byte, <-chan error, error) {
	if strings.TrimSpace(containerID) == "" {
		return nil, nil, fmt.Errorf("%w: container id is required", ErrValidation)
	}
	ctx, cancel := context.WithTimeout(ctx, s.logTimeout)
	server, err := s.resolve(ctx, serverID, false)
	if err != nil {
		cancel()
		return nil, nil, err
	}
	client, err := s.dialClient(ctx, server)
	if err != nil {
		cancel()
		return nil, nil, err
	}
	stream, err := client.StreamLogs(ctx, &agentv1.StreamLogsRequest{ContainerId: containerID, Follow: follow})
	if err != nil {
		s.closeClient(client)
		cancel()
		return nil, nil, mapRPCError(err)
	}

	out := make(chan []byte)
	// Buffered so the producer never blocks if the caller stops reading after
	// a cancellation.
	streamErr := make(chan error, 1)
	go func() {
		defer close(out)
		defer close(streamErr)
		defer cancel()
		defer s.closeClient(client)
		for {
			chunk, err := stream.Recv()
			if err != nil {
				// io.EOF is the normal end of a log stream. Any other error
				// while the caller context is still live is a failed stream
				// and is surfaced so the caller never reports success.
				if !errors.Is(err, io.EOF) && ctx.Err() == nil {
					streamErr <- mapRPCError(err)
					s.logger.Debug("containers: log stream ended early",
						"container_id", containerID, "error", err)
				}
				return
			}
			data := chunk.GetData()
			if len(data) == 0 {
				continue
			}
			select {
			case out <- data:
			case <-ctx.Done():
				return
			}
		}
	}()
	return out, streamErr, nil
}

// resolve returns the registry entry for a server, mapping a missing row to
// ErrServerNotFound and a node outside the caller's active team to the same
// not-found (so server IDs cannot be probed). write additionally requires an
// owner/admin role: the container routes are mounted through the team chain,
// and this is the authoritative per-node check behind it. A request without a
// team scope (background jobs, tests) keeps the pre-teams behavior, exactly
// like every other team-scoped resource.
func (s *Service) resolve(ctx context.Context, serverID uuid.UUID, write bool) (*servers.Server, error) {
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
	if err := teams.ScopeFor(ctx, uuid.Nil).AuthorizeOptionalTeam(server.TeamID, write); err != nil {
		if errors.Is(err, teams.ErrForbidden) {
			return nil, ErrForbidden
		}
		return nil, ErrServerNotFound
	}
	return server, nil
}

// client resolves the server and dials its agent with the standard RPC
// timeout, returning the bounded context the caller must pass to the RPC.
// write selects the authorization: read methods pass false, every mutation
// passes true.
func (s *Service) client(ctx context.Context, serverID uuid.UUID, write bool) (context.Context, DockerClient, context.CancelFunc, error) {
	ctx, cancel := context.WithTimeout(ctx, s.rpcTimeout)
	server, err := s.resolve(ctx, serverID, write)
	if err != nil {
		cancel()
		return nil, nil, nil, err
	}
	client, err := s.dialClient(ctx, server)
	if err != nil {
		cancel()
		return nil, nil, nil, err
	}
	return ctx, client, cancel, nil
}

// pullClient is client with the longer image-transfer timeout. Pull and run
// are mutations.
func (s *Service) pullClient(ctx context.Context, serverID uuid.UUID) (context.Context, DockerClient, context.CancelFunc, error) {
	ctx, cancel := context.WithTimeout(ctx, s.pullTimeout)
	server, err := s.resolve(ctx, serverID, true)
	if err != nil {
		cancel()
		return nil, nil, nil, err
	}
	client, err := s.dialClient(ctx, server)
	if err != nil {
		cancel()
		return nil, nil, nil, err
	}
	return ctx, client, cancel, nil
}

// closeClient releases a dialed agent client. The production client owns a
// gRPC connection, so every operation closes what it dialed; a test fake that
// does not implement io.Closer is left untouched.
func (s *Service) closeClient(client DockerClient) {
	closer, ok := client.(io.Closer)
	if !ok {
		return
	}
	if err := closer.Close(); err != nil {
		s.logger.Debug("containers: closing agent client failed", "error", err)
	}
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
	case codes.Internal, codes.Unknown:
		// Docker reports a host-port collision through a generic 500 whose
		// message carries the daemon text (the agent has no typed error for
		// it), so the text is the only signal left. Classify it as a conflict
		// so callers answer 409 instead of 500.
		if isPortConflictMessage(status.Convert(err).Message()) {
			return fmt.Errorf("%w: %v", ErrPortConflict, err)
		}
		return err
	default:
		return err
	}
}

// portConflictMarkers are the Docker daemon messages for a host port that is
// already bound.
var portConflictMarkers = []string{
	"port is already allocated",
	"address already in use",
}

// isPortConflictMessage reports whether a Docker (or agent) error message
// describes a host-port bind collision.
//
// Scope: the heuristic applies to every container RPC mapped through
// mapRPCError, not just Run. The current agent proto carries no typed
// port-conflict error, so the daemon text is the only signal across gRPC; a
// false positive can only reclassify an Internal failure as a conflict, and
// it is replaced by a typed status the moment the proto grows one.
func isPortConflictMessage(message string) bool {
	message = strings.ToLower(message)
	for _, marker := range portConflictMarkers {
		if strings.Contains(message, marker) {
			return true
		}
	}
	return false
}
