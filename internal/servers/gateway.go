package servers

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
)

const (
	// defaultGatewayAddr is used when no gRPC address is configured.
	defaultGatewayAddr = ":9442"
	// updateLatestVersion is reported by the UpdateService skeleton.
	updateLatestVersion = "0.0.0"
	// nodeIDMetadataKey identifies a node on the dev (non-mTLS) listener.
	nodeIDMetadataKey = "node-id"
	// gatewayGracefulStop bounds graceful shutdown before forcing a stop.
	gatewayGracefulStop = 5 * time.Second
)

// GatewayConfig wires a Gateway.
type GatewayConfig struct {
	Addr      string
	Authority *Authority
	Service   *ServerService
	Logger    *slog.Logger
}

// Gateway is the control-plane gRPC server. It implements AgentService (node
// registration and heartbeats) and the UpdateService skeleton.
type Gateway struct {
	agentv1.UnimplementedAgentServiceServer
	agentv1.UnimplementedUpdateServiceServer

	server     *grpc.Server
	listener   net.Listener
	addr       string
	authority  *Authority
	service    *ServerService
	logger     *slog.Logger
	tlsEnabled bool
	stopOnce   sync.Once
}

// NewGateway builds the gRPC server and its transport credentials. When an
// authority is configured the listener presents a CP server certificate and
// verifies agent client certificates if the agent sends one; without one the
// listener runs insecurely for development.
func NewGateway(cfg GatewayConfig) (*Gateway, error) {
	if cfg.Service == nil {
		return nil, errors.New("servers: gateway service is nil")
	}

	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	addr := cfg.Addr
	if addr == "" {
		addr = defaultGatewayAddr
	}

	g := &Gateway{
		addr:      addr,
		authority: cfg.Authority,
		service:   cfg.Service,
		logger:    logger,
	}

	opts := make([]grpc.ServerOption, 0, 1)
	if cfg.Authority != nil {
		creds, err := cfg.Authority.serverCredentials(addr)
		if err != nil {
			return nil, err
		}
		opts = append(opts, grpc.Creds(creds))
		g.tlsEnabled = true
	} else {
		logger.Warn("servers: no CA configured; gRPC gateway starting without TLS (development only)")
	}

	g.server = grpc.NewServer(opts...)
	agentv1.RegisterAgentServiceServer(g.server, g)
	agentv1.RegisterUpdateServiceServer(g.server, g)
	return g, nil
}

// Start binds the listener and serves in a background goroutine. It returns an
// error when the address cannot be bound. The gateway stops when ctx is
// cancelled or Stop is called.
func (g *Gateway) Start(ctx context.Context) error {
	listener, err := net.Listen("tcp", g.addr)
	if err != nil {
		return fmt.Errorf("grpc listen %s: %w", g.addr, err)
	}
	g.listener = listener

	g.logger.Info("grpc gateway listening", "addr", listener.Addr().String(), "tls", g.tlsEnabled)

	go func() {
		if err := g.server.Serve(listener); err != nil && !errors.Is(err, grpc.ErrServerStopped) {
			g.logger.Error("grpc gateway stopped with error", "error", err)
		}
	}()

	go func() {
		<-ctx.Done()
		g.Stop()
	}()

	return nil
}

// Stop gracefully shuts the server down, forcing a stop after a short grace
// period. It is idempotent.
func (g *Gateway) Stop() {
	g.stopOnce.Do(func() {
		if g.server == nil {
			return
		}
		done := make(chan struct{})
		go func() {
			g.server.GracefulStop()
			close(done)
		}()
		select {
		case <-done:
		case <-time.After(gatewayGracefulStop):
			g.logger.Warn("grpc gateway graceful stop timed out; forcing stop")
			g.server.Stop()
		}
	})
}

// Addr returns the configured listen address.
func (g *Gateway) Addr() string {
	return g.addr
}

// Register handles an agent registration.
func (g *Gateway) Register(ctx context.Context, req *agentv1.RegisterRequest) (*agentv1.RegisterResponse, error) {
	resp, err := g.service.RegisterNode(ctx, req)
	if err != nil {
		return nil, toGRPCError(err)
	}
	return resp, nil
}

// Heartbeat consumes the client stream, recording a heartbeat per message, and
// acknowledges once the stream closes.
func (g *Gateway) Heartbeat(stream grpc.ClientStreamingServer[agentv1.HeartbeatRequest, agentv1.HeartbeatResponse]) error {
	ctx := stream.Context()
	nodeID := nodeIDFromContext(ctx)
	if nodeID == "" {
		g.logger.Warn("servers: heartbeat from unidentifiable peer", "peer", peerAddress(ctx))
	}

	for {
		req, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			return stream.SendAndClose(&agentv1.HeartbeatResponse{ReceivedAt: timestamppb.Now()})
		}
		if err != nil {
			return err
		}
		if nodeID == "" {
			continue
		}
		if err := g.service.RecordHeartbeat(ctx, nodeID, req); err != nil {
			if errors.Is(err, ErrNotFound) {
				g.logger.Warn("servers: heartbeat for unknown node", "node_id", nodeID)
				continue
			}
			g.logger.Warn("servers: heartbeat", "node_id", nodeID, "error", err)
		}
	}
}

// RequestUpdate is the UpdateService skeleton. It always reports no update.
func (g *Gateway) RequestUpdate(context.Context, *agentv1.UpdateRequest) (*agentv1.UpdateResponse, error) {
	return &agentv1.UpdateResponse{UpdateAvailable: false, LatestVersion: updateLatestVersion}, nil
}

// serverCredentials builds the mTLS transport credentials for the listener.
//
// Client certificates are verified only when presented (VerifyClientCertIfGiven)
// rather than required: the current RegisterResponse contract carries no
// registration token or CSR, so an agent's very first Register cannot present a
// client certificate. Requiring one is possible once the contract gains a
// bootstrap credential.
func (a *Authority) serverCredentials(addr string) (credentials.TransportCredentials, error) {
	certPEM, keyPEM, err := a.IssueServerCert(serverHosts(addr))
	if err != nil {
		return nil, fmt.Errorf("issue server certificate: %w", err)
	}
	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return nil, fmt.Errorf("load server keypair: %w", err)
	}

	return credentials.NewTLS(&tls.Config{
		Certificates: []tls.Certificate{cert},
		ClientAuth:   tls.VerifyClientCertIfGiven,
		ClientCAs:    a.Pool(),
		MinVersion:   tls.VersionTLS12,
	}), nil
}

// nodeIDFromContext identifies the calling node: mTLS peer certificate first
// (SAN/CN), falling back to the node-id metadata header used by the insecure
// development listener.
func nodeIDFromContext(ctx context.Context) string {
	if p, ok := peer.FromContext(ctx); ok && p.AuthInfo != nil {
		if tlsInfo, ok := p.AuthInfo.(credentials.TLSInfo); ok {
			certs := tlsInfo.State.PeerCertificates
			if len(certs) > 0 {
				cert := certs[0]
				switch {
				case len(cert.DNSNames) > 0:
					return cert.DNSNames[0]
				case len(cert.IPAddresses) > 0:
					return cert.IPAddresses[0].String()
				case cert.Subject.CommonName != "":
					return cert.Subject.CommonName
				}
			}
		}
	}

	if md, ok := metadata.FromIncomingContext(ctx); ok {
		if values := md.Get(nodeIDMetadataKey); len(values) > 0 {
			return values[0]
		}
	}
	return ""
}

// peerAddress renders the caller address for diagnostics.
func peerAddress(ctx context.Context) string {
	if p, ok := peer.FromContext(ctx); ok && p.Addr != nil {
		return p.Addr.String()
	}
	return "unknown"
}

// serverHosts derives the SAN host list for the listener certificate.
func serverHosts(addr string) []string {
	hosts := []string{"localhost", "127.0.0.1", "::1"}
	if host, _, err := net.SplitHostPort(addr); err == nil && host != "" && host != "0.0.0.0" && host != "::" {
		hosts = append([]string{host}, hosts...)
	}
	return hosts
}

// toGRPCError maps domain sentinels to gRPC status codes.
func toGRPCError(err error) error {
	switch {
	case errors.Is(err, ErrValidation), errors.Is(err, ErrNoCredentials):
		return status.Error(codes.InvalidArgument, err.Error())
	case errors.Is(err, ErrNotFound):
		return status.Error(codes.NotFound, err.Error())
	case errors.Is(err, ErrConflict):
		return status.Error(codes.AlreadyExists, err.Error())
	default:
		return status.Error(codes.Internal, "internal error")
	}
}
