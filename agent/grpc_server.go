package agent

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"strings"
	"time"

	"github.com/justindeelux/gotham/agent/terminal"
	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/peer"
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

// serverConfig collects the service registrations and interceptors a
// ServerOption configures, so interceptors can be passed to grpc.NewServer
// before the services are registered on it.
type serverConfig struct {
	registers []func(*grpc.Server)
	unary     []grpc.UnaryServerInterceptor
	stream    []grpc.StreamServerInterceptor
	// allowAnyPeer opts out of the mandatory control-plane peer check. It is set
	// only by WithoutControlPlanePeer (tests and the explicit dev-insecure mode).
	allowAnyPeer bool
	// terminal is the host terminal service, when registered, so shutdown can
	// wait for in-flight session cleanup.
	terminal *terminal.TerminalServer
}

// ServerOption registers an optional service or interceptor on the agent gRPC
// server.
type ServerOption func(*serverConfig)

// WithBuildService registers build so the control plane can build images on
// this node. Pass it to NewServer.
func WithBuildService(build agentv1.BuildServiceServer) ServerOption {
	return func(c *serverConfig) {
		c.registers = append(c.registers, func(server *grpc.Server) {
			agentv1.RegisterBuildServiceServer(server, build)
		})
	}
}

// WithProxyService registers proxy so the control plane can push generated
// Traefik configuration to this node. Pass it to NewServer.
func WithProxyService(proxy agentv1.ProxyServiceServer) ServerOption {
	return func(c *serverConfig) {
		c.registers = append(c.registers, func(server *grpc.Server) {
			agentv1.RegisterProxyServiceServer(server, proxy)
		})
	}
}

// WithComposeService registers compose so the control plane can run compose
// service projects on this node. Pass it to NewServer.
func WithComposeService(compose agentv1.ComposeServiceServer) ServerOption {
	return func(c *serverConfig) {
		c.registers = append(c.registers, func(server *grpc.Server) {
			agentv1.RegisterComposeServiceServer(server, compose)
		})
	}
}

// WithFileService registers the confined file service so the control plane can
// browse the node's allow-listed roots. It is passed only when
// GOTHAM_AGENT_FILES is not turned off.
func WithFileService(files agentv1.FileServiceServer) ServerOption {
	return func(c *serverConfig) {
		c.registers = append(c.registers, func(server *grpc.Server) {
			agentv1.RegisterFileServiceServer(server, files)
		})
	}
}

// WithTerminalService registers the host terminal service so a platform
// operator can open a PTY-backed shell on this node. It is passed only when the
// file service (and therefore its root policy) is enabled; an agent without it
// answers codes.Unimplemented, which the control plane reports as unsupported.
func WithTerminalService(service agentv1.TerminalServiceServer) ServerOption {
	return func(c *serverConfig) {
		if sessions, ok := service.(*terminal.TerminalServer); ok {
			c.terminal = sessions
		}
		c.registers = append(c.registers, func(server *grpc.Server) {
			agentv1.RegisterTerminalServiceServer(server, service)
		})
	}
}

// WithToolService registers the host tool catalog so the control plane can list
// this node's tools and their detected state. Pass it to NewServer.
func WithToolService(tools agentv1.ToolServiceServer) ServerOption {
	return func(c *serverConfig) {
		c.registers = append(c.registers, func(server *grpc.Server) {
			agentv1.RegisterToolServiceServer(server, tools)
		})
	}
}

// WithoutControlPlanePeer disables the mandatory control-plane peer check that
// NewServer installs for every TLS listener (JUS-194). A production listener
// must never pass it: it exists for tests, and for the explicit dev-insecure
// mode where the listener serves plaintext on loopback and no CA is configured.
func WithoutControlPlanePeer() ServerOption {
	return func(c *serverConfig) {
		c.allowAnyPeer = true
	}
}

// requireControlPlaneUnary rejects a unary RPC whose peer is not the control
// plane.
func requireControlPlaneUnary(log *slog.Logger) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, _ *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		if err := requireControlPlanePeerFromContext(ctx, log); err != nil {
			return nil, err
		}
		return handler(ctx, req)
	}
}

// requireControlPlaneStream rejects a streaming RPC whose peer is not the
// control plane.
func requireControlPlaneStream(log *slog.Logger) grpc.StreamServerInterceptor {
	return func(srv any, stream grpc.ServerStream, _ *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		if err := requireControlPlanePeerFromContext(stream.Context(), log); err != nil {
			return err
		}
		return handler(srv, stream)
	}
}

// requireControlPlanePeerFromContext re-checks the peer certificate's
// control-plane identity for one RPC.
func requireControlPlanePeerFromContext(ctx context.Context, log *slog.Logger) error {
	p, ok := peer.FromContext(ctx)
	if !ok || p.AuthInfo == nil {
		return status.Error(codes.Unauthenticated, "control-plane client certificate is required")
	}
	tlsInfo, ok := p.AuthInfo.(credentials.TLSInfo)
	if !ok || len(tlsInfo.State.PeerCertificates) == 0 {
		return status.Error(codes.Unauthenticated, "control-plane client certificate is required")
	}
	legacy, err := verifyControlPlaneCert(tlsInfo.State.PeerCertificates[0])
	if err != nil {
		return status.Error(codes.PermissionDenied, "peer is not the control plane")
	}
	if legacy && log != nil {
		log.Warn("agent: accepted a legacy control-plane credential (no URI SAN); it should be upgraded")
	}
	return nil
}

// Server wraps a gRPC server exposing DockerService over TLS.
type Server struct {
	grpc     *grpc.Server
	ln       net.Listener
	log      *slog.Logger
	terminal *terminal.TerminalServer
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
	if log == nil {
		log = slog.Default()
	}
	config := &serverConfig{}
	for _, option := range options {
		option(config)
	}
	// The control-plane peer check is mandatory for a TLS listener (JUS-194): the
	// F1 fix relies on it, so it is installed here rather than by each caller.
	if creds != nil && !config.allowAnyPeer {
		config.unary = append(config.unary, requireControlPlaneUnary(log))
		config.stream = append(config.stream, requireControlPlaneStream(log))
	}
	var serverOptions []grpc.ServerOption
	if creds != nil {
		serverOptions = append(serverOptions, grpc.Creds(creds))
	}
	// Bound streams and message size for the same reason as the control-plane
	// gateway: the listener is reachable by the CP, and a compromised or buggy
	// peer must not be able to exhaust the agent.
	serverOptions = append(serverOptions, grpc.MaxConcurrentStreams(agentMaxConcurrentStreams), grpc.MaxRecvMsgSize(agentMaxRecvMsgSize))
	if len(config.unary) > 0 {
		serverOptions = append(serverOptions, grpc.ChainUnaryInterceptor(config.unary...))
	}
	if len(config.stream) > 0 {
		serverOptions = append(serverOptions, grpc.ChainStreamInterceptor(config.stream...))
	}
	server := grpc.NewServer(serverOptions...)
	agentv1.RegisterDockerServiceServer(server, impl)
	for _, register := range config.registers {
		register(server)
	}
	if creds == nil {
		log.Warn("agent: serving plaintext on loopback (development only)", "addr", listener.Addr().String())
	}
	return &Server{grpc: server, ln: listener, log: log, terminal: config.terminal}, nil
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
		// Handlers are drained; finish the session cleanup walks (SIGTERM then
		// SIGKILL) within a bound so a graceful stop does not strand a job.
		if s.terminal != nil {
			s.terminal.WaitTerminations(terminal.ShutdownWait)
		}
	}()

	if err := s.grpc.Serve(s.ln); err != nil && !errors.Is(err, grpc.ErrServerStopped) {
		return fmt.Errorf("agent: serve docker service: %w", err)
	}
	<-shutdownDone
	return nil
}
