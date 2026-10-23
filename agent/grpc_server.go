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

const serverDrainTimeout = 5 * time.Second

const (
	agentMaxConcurrentStreams = 64
	agentMaxRecvMsgSize       = 2 << 20
)

type serverConfig struct {
	registers    []func(*grpc.Server)
	unary        []grpc.UnaryServerInterceptor
	stream       []grpc.StreamServerInterceptor
	allowAnyPeer bool
	terminal     *terminal.TerminalServer
}

type ServerOption func(*serverConfig)

func WithBuildService(build agentv1.BuildServiceServer) ServerOption {
	return func(c *serverConfig) {
		c.registers = append(c.registers, func(server *grpc.Server) {
			agentv1.RegisterBuildServiceServer(server, build)
		})
	}
}

func WithProxyService(proxy agentv1.ProxyServiceServer) ServerOption {
	return func(c *serverConfig) {
		c.registers = append(c.registers, func(server *grpc.Server) {
			agentv1.RegisterProxyServiceServer(server, proxy)
		})
	}
}

func WithComposeService(compose agentv1.ComposeServiceServer) ServerOption {
	return func(c *serverConfig) {
		c.registers = append(c.registers, func(server *grpc.Server) {
			agentv1.RegisterComposeServiceServer(server, compose)
		})
	}
}

func WithFileService(files agentv1.FileServiceServer) ServerOption {
	return func(c *serverConfig) {
		c.registers = append(c.registers, func(server *grpc.Server) {
			agentv1.RegisterFileServiceServer(server, files)
		})
	}
}

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

func WithToolService(tools agentv1.ToolServiceServer) ServerOption {
	return func(c *serverConfig) {
		c.registers = append(c.registers, func(server *grpc.Server) {
			agentv1.RegisterToolServiceServer(server, tools)
		})
	}
}

func WithoutControlPlanePeer() ServerOption {
	return func(c *serverConfig) {
		c.allowAnyPeer = true
	}
}

func requireControlPlaneUnary(log *slog.Logger) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, _ *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		if err := requireControlPlanePeerFromContext(ctx, log); err != nil {
			return nil, err
		}
		return handler(ctx, req)
	}
}

func requireControlPlaneStream(log *slog.Logger) grpc.StreamServerInterceptor {
	return func(srv any, stream grpc.ServerStream, _ *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		if err := requireControlPlanePeerFromContext(stream.Context(), log); err != nil {
			return err
		}
		return handler(srv, stream)
	}
}

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

type Server struct {
	grpc     *grpc.Server
	ln       net.Listener
	log      *slog.Logger
	terminal *terminal.TerminalServer
}

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
	if creds != nil && !config.allowAnyPeer {
		config.unary = append(config.unary, requireControlPlaneUnary(log))
		config.stream = append(config.stream, requireControlPlaneStream(log))
	}
	var serverOptions []grpc.ServerOption
	if creds != nil {
		serverOptions = append(serverOptions, grpc.Creds(creds))
	}
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

func (s *Server) Addr() net.Addr { return s.ln.Addr() }

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
