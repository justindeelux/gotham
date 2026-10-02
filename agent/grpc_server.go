package agent

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
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
// messages.
const (
	agentMaxConcurrentStreams = 64
	agentMaxRecvMsgSize       = 1 << 20
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
	PullImage(ctx context.Context, image string) error
	CreateContainer(ctx context.Context, req *agentv1.CreateContainerRequest) (string, error)
	RunImage(ctx context.Context, req *agentv1.CreateContainerRequest) (string, error)
	Logs(ctx context.Context, id string, follow bool, tail int64) (<-chan []byte, error)
}

// DockerServer implements agentv1.DockerServiceServer on top of a dockerClient.
type DockerServer struct {
	agentv1.UnimplementedDockerServiceServer
	docker dockerClient
	log    *slog.Logger
}

// NewDockerServer returns a DockerService implementation backed by docker.
func NewDockerServer(docker dockerClient, log *slog.Logger) *DockerServer {
	if log == nil {
		log = slog.Default()
	}
	return &DockerServer{docker: docker, log: log}
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
		case chunk, ok := <-chunks:
			if !ok {
				return nil
			}
			if err := stream.Send(&agentv1.LogChunk{Data: chunk}); err != nil {
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
// failure.
func dockerError(action string, err error) error {
	switch {
	case errors.Is(err, ErrInvalidPortMapping):
		return status.Errorf(codes.InvalidArgument, "%s: %v", action, err)
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
