package agent

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"regexp"
	"strings"
	"time"

	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/status"
)

// serverDrainTimeout bounds a graceful server stop before it is forced.
const serverDrainTimeout = 5 * time.Second

// Stream and message caps for the agent's gRPC server, matching the control
// plane's gateway: one peer must not open unbounded streams or push oversized
// messages. The receive cap must exceed maxComposeYAML (1 MiB) with room for
// gRPC framing, otherwise a compose document at the application limit would be
// rejected by the transport before its own validation.
const (
	agentMaxConcurrentStreams = 64
	agentMaxRecvMsgSize       = 2 << 20
)

// dockerClient is the subset of DockerClient the gRPC server needs. It is an
// interface so tests can substitute a fake.
type dockerClient interface {
	Version(ctx context.Context) (string, error)
	ListContainers(ctx context.Context, all bool) ([]*agentv1.ContainerInfo, error)
	Start(ctx context.Context, id string) error
	Stop(ctx context.Context, id string) error
	Restart(ctx context.Context, id string) error
	Remove(ctx context.Context, id string) error
	RemoveVolume(ctx context.Context, name string) error
	PullImage(ctx context.Context, image string) error
	CreateContainer(ctx context.Context, req *agentv1.CreateContainerRequest) (string, error)
	RunImage(ctx context.Context, req *agentv1.CreateContainerRequest) (string, error)
	Logs(ctx context.Context, id string, follow bool, tail int64) (<-chan LogMessage, error)
}

// DockerServer implements agentv1.DockerServiceServer on top of a dockerClient.
type DockerServer struct {
	agentv1.UnimplementedDockerServiceServer
	docker dockerClient
	log    *slog.Logger
	// volumeRoot is the parent of every application bind mount the node
	// accepts; a request carrying an application bind outside it is refused.
	volumeRoot string
	// proxyVolumeRoot is the node's own Traefik directory: a proxy container
	// may mount it (the config and ACME directories), unlike an application.
	proxyVolumeRoot string
}

// DockerServerOption tunes a DockerServer.
type DockerServerOption func(*DockerServer)

// WithManagedVolumeRoot confines every application bind DockerServer accepts
// to <root>/<app id>. Pass the configured Config.ManagedVolumeRoot.
func WithManagedVolumeRoot(root string) DockerServerOption {
	return func(s *DockerServer) {
		if strings.TrimSpace(root) != "" {
			s.volumeRoot = root
		}
	}
}

// WithProxyVolumeRoot overrides the directory a proxy container may mount. It
// exists for embedded/test harnesses that run the proxy out of a temporary
// root; the production agent never passes it, so the default stays the fixed
// constant that matches internal/proxy.TraefikDir and no environment or config
// value can widen the allowlist.
func WithProxyVolumeRoot(root string) DockerServerOption {
	return func(s *DockerServer) {
		if strings.TrimSpace(root) != "" {
			s.proxyVolumeRoot = root
		}
	}
}

// NewDockerServer returns a DockerService implementation backed by docker.
func NewDockerServer(docker dockerClient, log *slog.Logger, options ...DockerServerOption) *DockerServer {
	if log == nil {
		log = slog.Default()
	}
	server := &DockerServer{
		docker:          docker,
		log:             log,
		volumeRoot:      defaultManagedVolumeRoot,
		proxyVolumeRoot: defaultTraefikDir,
	}
	for _, option := range options {
		option(server)
	}
	return server
}

// ListContainers returns the node's containers.
func (s *DockerServer) ListContainers(ctx context.Context, req *agentv1.ListContainersRequest) (*agentv1.ListContainersResponse, error) {
	containers, err := s.docker.ListContainers(ctx, req.GetAll())
	if err != nil {
		return nil, dockerError("list containers", err)
	}
	return &agentv1.ListContainersResponse{Containers: containers}, nil
}

// StartContainer starts a container.
func (s *DockerServer) StartContainer(ctx context.Context, req *agentv1.ContainerActionRequest) (*agentv1.ContainerActionResponse, error) {
	return s.containerAction(ctx, req, "start", s.docker.Start)
}

// StopContainer stops a container.
func (s *DockerServer) StopContainer(ctx context.Context, req *agentv1.ContainerActionRequest) (*agentv1.ContainerActionResponse, error) {
	return s.containerAction(ctx, req, "stop", s.docker.Stop)
}

// RestartContainer restarts a container.
func (s *DockerServer) RestartContainer(ctx context.Context, req *agentv1.ContainerActionRequest) (*agentv1.ContainerActionResponse, error) {
	return s.containerAction(ctx, req, "restart", s.docker.Restart)
}

// RemoveContainer deletes a container on the node. Removal is idempotent: a
// container that is already gone is reported as success, so the control plane
// can retry a delete without checking the container first.
func (s *DockerServer) RemoveContainer(ctx context.Context, req *agentv1.ContainerActionRequest) (*agentv1.ContainerActionResponse, error) {
	return s.containerAction(ctx, req, "remove", s.docker.Remove)
}

// dbVolumePattern pins the only named-volume namespace RemoveVolume may touch:
// a database volume is "gotham-db-{database id}". The control plane names it,
// so a compromised control plane cannot use this call as a generic node
// storage deletion primitive.
var dbVolumePattern = regexp.MustCompile(`^gotham-db-[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// RemoveVolume deletes one managed database volume. The name is validated
// against the gotham-db-{uuid} shape before Docker is reached, so the call
// cannot delete an arbitrary volume even if the control plane is compromised.
// Removal is idempotent: a volume that is already gone is success.
func (s *DockerServer) RemoveVolume(ctx context.Context, req *agentv1.VolumeActionRequest) (*agentv1.VolumeActionResponse, error) {
	name := req.GetName()
	if strings.TrimSpace(name) == "" {
		return nil, status.Error(codes.InvalidArgument, "volume name is required")
	}
	// Match the raw name: the pattern is anchored, so a padded or
	// suffix-injected name ("x-gotham-db-…", "…\n") is rejected rather than
	// normalised into something Docker would accept.
	if !dbVolumePattern.MatchString(name) {
		return nil, status.Errorf(codes.InvalidArgument, "remove volume: %q is not a managed database volume", name)
	}
	if err := s.docker.RemoveVolume(ctx, name); err != nil {
		return nil, dockerError("remove volume", err)
	}
	return &agentv1.VolumeActionResponse{}, nil
}

// PullImage pulls an image onto the node.
func (s *DockerServer) PullImage(ctx context.Context, req *agentv1.PullImageRequest) (*agentv1.PullImageResponse, error) {
	if req.GetImage() == "" {
		return nil, status.Error(codes.InvalidArgument, "image is required")
	}
	if err := s.docker.PullImage(ctx, req.GetImage()); err != nil {
		return nil, dockerError("pull image", err)
	}
	return &agentv1.PullImageResponse{}, nil
}

// CreateContainer creates a container without starting it.
func (s *DockerServer) CreateContainer(ctx context.Context, req *agentv1.CreateContainerRequest) (*agentv1.ContainerActionResponse, error) {
	if req.GetImage() == "" {
		return nil, status.Error(codes.InvalidArgument, "image is required")
	}
	if err := validateContainerVolumes(s.volumeRoot, s.proxyVolumeRoot, req); err != nil {
		return nil, dockerError("create container", err)
	}
	id, err := s.docker.CreateContainer(ctx, req)
	if err != nil {
		return nil, dockerError("create container", err)
	}
	return &agentv1.ContainerActionResponse{ContainerId: id}, nil
}

// RunImage creates and starts a container.
func (s *DockerServer) RunImage(ctx context.Context, req *agentv1.CreateContainerRequest) (*agentv1.ContainerActionResponse, error) {
	if req.GetImage() == "" {
		return nil, status.Error(codes.InvalidArgument, "image is required")
	}
	if err := validateContainerVolumes(s.volumeRoot, s.proxyVolumeRoot, req); err != nil {
		return nil, dockerError("run image", err)
	}
	id, err := s.docker.RunImage(ctx, req)
	if err != nil {
		return nil, dockerError("run image", err)
	}
	return &agentv1.ContainerActionResponse{ContainerId: id}, nil
}

// StreamLogs streams container log chunks until the container's log stream ends
// or the client cancels the RPC.
func (s *DockerServer) StreamLogs(req *agentv1.StreamLogsRequest, stream grpc.ServerStreamingServer[agentv1.LogChunk]) error {
	if req.GetContainerId() == "" {
		return status.Error(codes.InvalidArgument, "container_id is required")
	}
	ctx := stream.Context()
	chunks, err := s.docker.Logs(ctx, req.GetContainerId(), req.GetFollow(), req.GetTail())
	if err != nil {
		return dockerError("stream logs", err)
	}
	for {
		select {
		case <-ctx.Done():
			return status.FromContextError(ctx.Err()).Err()
		case message, ok := <-chunks:
			if !ok {
				return nil
			}
			if message.Err != nil {
				// A decode error is a failed stream, not a clean end: report
				// it so the control plane never treats a truncated stream as
				// success.
				return dockerError("stream logs", message.Err)
			}
			if err := stream.Send(&agentv1.LogChunk{Data: message.Data}); err != nil {
				return err
			}
		}
	}
}

// containerAction runs a lifecycle method after validating the container id.
func (s *DockerServer) containerAction(
	ctx context.Context,
	req *agentv1.ContainerActionRequest,
	verb string,
	action func(context.Context, string) error,
) (*agentv1.ContainerActionResponse, error) {
	id := req.GetContainerId()
	if id == "" {
		return nil, status.Error(codes.InvalidArgument, "container_id is required")
	}
	if err := action(ctx, id); err != nil {
		return nil, dockerError(verb+" container", err)
	}
	return &agentv1.ContainerActionResponse{ContainerId: id}, nil
}

// dockerError maps a Docker client error onto a gRPC status error. A
// malformed port mapping is invalid input for every caller, not an internal
// failure; a Docker 404 is NotFound and an unreachable daemon is Unavailable,
// so the control plane can answer 404/502 instead of 500.
func dockerError(action string, err error) error {
	switch {
	case errors.Is(err, ErrInvalidPortMapping), errors.Is(err, ErrInvalidVolumeBind):
		return status.Errorf(codes.InvalidArgument, "%s: %v", action, err)
	case errors.Is(err, ErrDockerNotFound):
		return status.Errorf(codes.NotFound, "%s: %v", action, err)
	case errors.Is(err, ErrDockerImageNotFound):
		// A missing image is a bad image reference, not a missing resource:
		// the control plane answers 400 rather than 404.
		return status.Errorf(codes.InvalidArgument, "%s: %v", action, err)
	case errors.Is(err, ErrDockerUnavailable):
		return status.Errorf(codes.Unavailable, "%s: %v", action, err)
	case errors.Is(err, context.Canceled):
		return status.Error(codes.Canceled, action+": context canceled")
	case errors.Is(err, context.DeadlineExceeded):
		return status.Error(codes.DeadlineExceeded, action+": deadline exceeded")
	default:
		return status.Errorf(codes.Internal, "%s: %v", action, err)
	}
}

// ServerOption registers an optional service on the agent gRPC server.
type ServerOption func(*grpc.Server)

// WithBuildService registers build so the control plane can build images on
// this node. Pass it to NewServer.
func WithBuildService(build agentv1.BuildServiceServer) ServerOption {
	return func(server *grpc.Server) {
		agentv1.RegisterBuildServiceServer(server, build)
	}
}

// WithProxyService registers proxy so the control plane can push generated
// Traefik configuration to this node. Pass it to NewServer.
func WithProxyService(proxy agentv1.ProxyServiceServer) ServerOption {
	return func(server *grpc.Server) {
		agentv1.RegisterProxyServiceServer(server, proxy)
	}
}

// WithComposeService registers compose so the control plane can run compose
// service projects on this node. Pass it to NewServer.
func WithComposeService(compose agentv1.ComposeServiceServer) ServerOption {
	return func(server *grpc.Server) {
		agentv1.RegisterComposeServiceServer(server, compose)
	}
}

// Server wraps a gRPC server exposing DockerService over TLS.
type Server struct {
	grpc *grpc.Server
	ln   net.Listener
	log  *slog.Logger
}

// NewServer binds addr and registers the DockerService implementation plus any
// optional services. When creds is nil the listener runs in development
// plaintext and is confined to loopback: a plaintext Docker control channel
// must never be reachable off-host. The caller must call Serve to begin
// accepting connections.
func NewServer(addr string, creds credentials.TransportCredentials, impl agentv1.DockerServiceServer, log *slog.Logger, options ...ServerOption) (*Server, error) {
	if creds == nil && !isLoopbackListenAddr(addr) {
		return nil, fmt.Errorf("agent: refusing to serve plaintext on non-loopback address %q", addr)
	}
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("agent: listen %s: %w", addr, err)
	}
	var serverOptions []grpc.ServerOption
	if creds != nil {
		serverOptions = append(serverOptions, grpc.Creds(creds))
	}
	// Bound streams and message size for the same reason as the control-plane
	// gateway: the listener is reachable by the CP, and a compromised or buggy
	// peer must not be able to exhaust the agent.
	serverOptions = append(serverOptions, grpc.MaxConcurrentStreams(agentMaxConcurrentStreams), grpc.MaxRecvMsgSize(agentMaxRecvMsgSize))
	server := grpc.NewServer(serverOptions...)
	agentv1.RegisterDockerServiceServer(server, impl)
	for _, option := range options {
		option(server)
	}
	if log == nil {
		log = slog.Default()
	}
	if creds == nil {
		log.Warn("agent: serving plaintext on loopback (development only)", "addr", listener.Addr().String())
	}
	return &Server{grpc: server, ln: listener, log: log}, nil
}

// isLoopbackListenAddr reports whether a listen address is confined to the
// loopback interface ("127.0.0.1:9443", "[::1]:9443", "localhost:9443"). An
// empty or wildcard host is not loopback.
func isLoopbackListenAddr(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		host = addr
	}
	host = strings.Trim(host, "[]")
	if host == "" {
		return false
	}
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// Addr returns the bound listener address.
func (s *Server) Addr() net.Addr { return s.ln.Addr() }

// Serve blocks serving requests until ctx is canceled or the server stops. It
// returns nil after a clean shutdown.
func (s *Server) Serve(ctx context.Context) error {
	shutdownDone := make(chan struct{})
	go func() {
		defer close(shutdownDone)
		<-ctx.Done()
		stopped := make(chan struct{})
		go func() {
			s.grpc.GracefulStop()
			close(stopped)
		}()
		select {
		case <-stopped:
		case <-time.After(serverDrainTimeout):
			s.grpc.Stop()
		}
	}()

	if err := s.grpc.Serve(s.ln); err != nil && !errors.Is(err, grpc.ErrServerStopped) {
		return fmt.Errorf("agent: serve docker service: %w", err)
	}
	<-shutdownDone
	return nil
}
