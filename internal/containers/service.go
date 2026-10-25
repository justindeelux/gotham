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
	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
)

const (
	defaultRPCTimeout  = 30 * time.Second
	defaultPullTimeout = 5 * time.Minute
	defaultLogTimeout  = 30 * time.Minute
)

type ContainerService interface {
	List(ctx context.Context, serverID uuid.UUID) ([]Container, error)
	Start(ctx context.Context, serverID uuid.UUID, containerID string) error
	Stop(ctx context.Context, serverID uuid.UUID, containerID string) error
	Restart(ctx context.Context, serverID uuid.UUID, containerID string) error
	Remove(ctx context.Context, serverID uuid.UUID, containerID string) error
	RemoveVolume(ctx context.Context, serverID uuid.UUID, volumeName string) error
	Pull(ctx context.Context, serverID uuid.UUID, image string) error
	Run(ctx context.Context, serverID uuid.UUID, opts RunOptions) (string, error)
	Logs(ctx context.Context, serverID uuid.UUID, containerID string, follow bool) (<-chan []byte, <-chan error, error)
}

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

func NewDefaultService(registry Registry, redisAddr string) *Service {
	if registry == nil {
		return nil
	}
	return NewService(Config{Registry: registry, RedisAddr: redisAddr})
}

func (s *Service) SetDial(dial DialFunc) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.dial = dial
}

func (s *Service) Close() error {
	if closer, ok := s.cache.(interface{ Close() error }); ok {
		return closer.Close()
	}
	return nil
}

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

func (s *Service) RemoveVolume(ctx context.Context, serverID uuid.UUID, volumeName string) error {
	if strings.TrimSpace(volumeName) == "" {
		return fmt.Errorf("%w: volume name is required", ErrValidation)
	}
	ctx, client, cancel, err := s.client(ctx, serverID, true)
	if err != nil {
		return err
	}
	defer cancel()
	defer s.closeClient(client)

	if _, err := client.RemoveVolume(ctx, &agentv1.VolumeActionRequest{Name: volumeName}); err != nil {
		return mapRPCError(err)
	}
	return nil
}

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
	streamErr := make(chan error, 1)
	go func() {
		defer close(out)
		defer close(streamErr)
		defer cancel()
		defer s.closeClient(client)
		for {
			chunk, err := stream.Recv()
			if err != nil {
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

func (s *Service) resolve(ctx context.Context, serverID uuid.UUID, _ bool) (*servers.Server, error) {
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

func (s *Service) closeClient(client DockerClient) {
	closer, ok := client.(io.Closer)
	if !ok {
		return
	}
	if err := closer.Close(); err != nil {
		s.logger.Debug("containers: closing agent client failed", "error", err)
	}
}

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

func (s *Service) invalidate(ctx context.Context, serverID uuid.UUID) {
	if err := s.cache.Invalidate(ctx, serverID); err != nil {
		s.logger.Debug("containers: cache invalidate failed", "server_id", serverID.String(), "error", err)
	}
}

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
		if isPortConflictMessage(status.Convert(err).Message()) {
			return fmt.Errorf("%w: %v", ErrPortConflict, err)
		}
		return err
	default:
		return err
	}
}

var portConflictMarkers = []string{
	"port is already allocated",
	"address already in use",
}

func isPortConflictMessage(message string) bool {
	message = strings.ToLower(message)
	for _, marker := range portConflictMarkers {
		if strings.Contains(message, marker) {
			return true
		}
	}
	return false
}
