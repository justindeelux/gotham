package servers

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"strings"
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
	// Hosts are extra DNS names and IPs added to the listener certificate SANs,
	// for example the control plane's public hostname. They are additive: the
	// bind-address host, the loopback names and the machine hostname are always
	// present.
	Hosts []string
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

	// Per-peer rate limits bound the unauthenticated Register/heartbeat
	// surface. Tests may replace them with tighter limits.
	registerLimiter  *peerRateLimiter
	heartbeatLimiter *peerRateLimiter
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
		addr:             addr,
		authority:        cfg.Authority,
		service:          cfg.Service,
		logger:           logger,
		registerLimiter:  newPeerRateLimiter(registerRateLimit, registerBurst),
		heartbeatLimiter: newPeerRateLimiter(heartbeatRateLimit, heartbeatBurst),
	}

	opts := make([]grpc.ServerOption, 0, 1)
	if cfg.Authority != nil {
		creds, err := cfg.Authority.serverCredentials(addr, cfg.Hosts)
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
//
// The node identity recorded in the registry is always req.NodeId, but it is
// bound to the authenticated peer when one is available: a caller presenting a
// client certificate whose identity differs is refused. When the agent also
// supplies a CSR, the certificate is issued for req.NodeId's own public key
// (the CSR's SANs must match it; see Authority.IssueAgentCertFromCSR) rather
// than the CSR's self-asserted SAN list. The CSR is validated before the
// registry is touched, so a malformed request fails without side effects.
//
// The bootstrap case carries no client certificate yet, so this is the
// verifiable subset of the identity binding; full mutual identity waits on the
// LOW-4 registration bootstrap credential (docs/TODO.md).
func (g *Gateway) Register(ctx context.Context, req *agentv1.RegisterRequest) (*agentv1.RegisterResponse, error) {
	if !g.registerLimiter.allow(peerHost(ctx)) {
		g.logger.Warn("servers: registration rate limit exceeded", "peer", peerAddress(ctx))
		return nil, status.Error(codes.ResourceExhausted, "too many registration attempts")
	}

	nodeID := strings.TrimSpace(req.GetNodeId())
	if err := validateNodeID(nodeID); err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	if identity := authenticatedNodeID(ctx); identity != "" && identity != nodeID {
		g.logger.Warn("servers: register node_id does not match the authenticated peer",
			"node_id", nodeID, "peer_identity", identity, "peer", peerAddress(ctx))
		return nil, status.Error(codes.PermissionDenied, "node_id does not match the authenticated peer")
	}

	var csrCert []byte
	if len(req.GetCsr()) > 0 && g.authority != nil {
		cert, err := g.authority.IssueAgentCertFromCSR(req.GetCsr(), nodeID)
		if err != nil {
			return nil, status.Error(codes.InvalidArgument, err.Error())
		}
		csrCert = cert
	}

	resp, err := g.service.RegisterNode(ctx, req)
	if err != nil {
		return nil, g.registerError(nodeID, err)
	}
	if csrCert != nil {
		resp.Cert = csrCert
	}
	return resp, nil
}

// registerError logs an unexpected registry failure before it is collapsed to
// Internal, then maps the domain sentinels to gRPC status codes.
func (g *Gateway) registerError(nodeID string, err error) error {
	if !isDomainError(err) {
		g.logger.Error("servers: register failed", "node_id", nodeID, "error", err)
	}
	return toGRPCError(err)
}

// isDomainError reports whether err maps to a caller-visible status rather than
// the opaque Internal error.
func isDomainError(err error) bool {
	return errors.Is(err, ErrValidation) ||
		errors.Is(err, ErrNoCredentials) ||
		errors.Is(err, ErrNotFound) ||
		errors.Is(err, ErrConflict)
}

// Heartbeat consumes the client stream, recording a heartbeat per message, and
// acknowledges once the stream closes. The node identity comes from the
// authenticated peer certificate when present; otherwise from the node-id
// metadata the bootstrap connection carries. When both are present and differ,
// the stream is refused.
func (g *Gateway) Heartbeat(stream grpc.ClientStreamingServer[agentv1.HeartbeatRequest, agentv1.HeartbeatResponse]) error {
	ctx := stream.Context()
	peer := peerAddress(ctx)
	limiterKey := peerHost(ctx)

	nodeID, err := g.heartbeatIdentity(ctx, peer)
	if err != nil {
		return err
	}
	if nodeID == "" {
		g.logger.Warn("servers: heartbeat from unidentifiable peer", "peer", peer)
	}

	for {
		req, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			return stream.SendAndClose(&agentv1.HeartbeatResponse{ReceivedAt: timestamppb.Now()})
		}
		if err != nil {
			return err
		}
		if !g.heartbeatLimiter.allow(limiterKey) {
			return status.Error(codes.ResourceExhausted, "heartbeat rate limit exceeded")
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

// heartbeatIdentity resolves the node id for a heartbeat stream and rejects a
// metadata identity that contradicts the authenticated peer certificate.
func (g *Gateway) heartbeatIdentity(ctx context.Context, peer string) (string, error) {
	authenticated := authenticatedNodeID(ctx)
	claimed := metadataNodeID(ctx)
	if authenticated != "" && claimed != "" && authenticated != claimed {
		g.logger.Warn("servers: heartbeat node_id does not match the authenticated peer",
			"node_id", claimed, "peer_identity", authenticated, "peer", peer)
		return "", status.Error(codes.PermissionDenied, "node_id does not match the authenticated peer")
	}
	if authenticated != "" {
		return authenticated, nil
	}
	return claimed, nil
}

// RequestUpdate answers an agent's update query. It compares the reported
// version and platform against the release the control plane knows, and, when a
// newer one exists, returns the signed release material (asset, manifest and
// detached-signature URLs plus the artifact digest and channel). The control
// plane verifies the manifest signature itself before offering, so an offer is
// never unsigned or unverified. It never fails the RPC for a release-server or
// configuration problem: the agent is told "no update" and the detail is
// logged.
//
// It does not record the reported version: this RPC carries no authenticated
// node identity (the listener verifies a client certificate only when one is
// presented, and agents present none), so the version map is fed exclusively by
// heartbeats, which resolve the node in the registry first.
func (g *Gateway) RequestUpdate(ctx context.Context, req *agentv1.UpdateRequest) (*agentv1.UpdateResponse, error) {
	nodeID := nodeIDFromContext(ctx)

	release, rollout, err := g.service.OfferAgentUpdate(ctx, req.GetAgentVersion(), req.GetOs(), req.GetArch())
	switch {
	case err != nil:
		g.logger.Warn("servers: agent update offer failed",
			"node_id", nodeID, "arch", req.GetArch(), "error", err)
		return &agentv1.UpdateResponse{UpdateAvailable: false}, nil
	case release == nil:
		return &agentv1.UpdateResponse{UpdateAvailable: false}, nil
	}

	g.logger.Info("servers: offering agent update",
		"node_id", nodeID, "from", req.GetAgentVersion(), "to", release.Version, "rollout", rollout)
	return &agentv1.UpdateResponse{
		UpdateAvailable:      true,
		LatestVersion:        release.Version,
		AssetUrl:             release.AssetURL,
		ManifestUrl:          release.ManifestURL,
		ManifestSignatureUrl: release.ManifestSignatureURL,
		Sha256:               release.SHA256,
		Channel:              release.Channel,
		Rollout:              rollout,
	}, nil
}

// serverCredentials builds the mTLS transport credentials for the listener.
//
// Client certificates are verified only when presented (VerifyClientCertIfGiven)
// rather than required: the current RegisterResponse contract carries no
// registration token or CSR, so an agent's very first Register cannot present a
// client certificate. Requiring one is possible once the contract gains a
// bootstrap credential.
func (a *Authority) serverCredentials(addr string, extraHosts []string) (credentials.TransportCredentials, error) {
	certPEM, keyPEM, err := a.IssueServerCert(serverHosts(addr, extraHosts))
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

// authenticatedNodeID returns the node identity proven by a presented mTLS
// client certificate, or "" when the peer presented none. It is the only
// identity the gateway can trust without the LOW-4 bootstrap credential.
func authenticatedNodeID(ctx context.Context) string {
	p, ok := peer.FromContext(ctx)
	if !ok || p.AuthInfo == nil {
		return ""
	}
	tlsInfo, ok := p.AuthInfo.(credentials.TLSInfo)
	if !ok || len(tlsInfo.State.PeerCertificates) == 0 {
		return ""
	}
	return certIdentity(tlsInfo.State.PeerCertificates[0])
}

// certIdentity derives the node identity a client certificate asserts: the
// first DNS SAN, then the first IP SAN, then the subject common name.
func certIdentity(cert *x509.Certificate) string {
	switch {
	case len(cert.DNSNames) > 0:
		return cert.DNSNames[0]
	case len(cert.IPAddresses) > 0:
		return cert.IPAddresses[0].String()
	default:
		return cert.Subject.CommonName
	}
}

// metadataNodeID returns the node-id metadata header used by the bootstrap
// connection. An over-long value is ignored rather than trusted.
func metadataNodeID(ctx context.Context) string {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return ""
	}
	values := md.Get(nodeIDMetadataKey)
	if len(values) == 0 {
		return ""
	}
	nodeID := strings.TrimSpace(values[0])
	if len(nodeID) > maxNodeIDLength {
		return ""
	}
	return nodeID
}

// nodeIDFromContext identifies the calling node: the authenticated mTLS peer
// certificate first, falling back to the node-id metadata header used by the
// insecure development listener.
func nodeIDFromContext(ctx context.Context) string {
	if identity := authenticatedNodeID(ctx); identity != "" {
		return identity
	}
	return metadataNodeID(ctx)
}

// peerAddress renders the caller address for diagnostics.
func peerAddress(ctx context.Context) string {
	if p, ok := peer.FromContext(ctx); ok && p.Addr != nil {
		return p.Addr.String()
	}
	return "unknown"
}

// peerHost returns the calling peer's host (without the ephemeral port), so a
// peer cannot rotate ports to escape the per-peer rate limits. The address is
// kept whole (not folded to a prefix): the limiter's maxPeerBuckets cap already
// bounds memory, whereas folding an IPv6 /64 would let one busy neighbour
// starve every agent in that prefix. A non-host:port address (for example the
// in-memory bufconn transport) is returned unchanged.
func peerHost(ctx context.Context) string {
	addr := peerAddress(ctx)
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		host = addr
	}
	host = strings.Trim(host, "[]")
	if ip := net.ParseIP(host); ip != nil {
		return ip.String()
	}
	return host
}

// serverHosts derives the SAN host list for the listener certificate: the
// operator-configured extra hosts first, then the bind-address host, the
// loopback names and the machine hostname so a remote agent dialing the control
// plane by its configured name or IP verifies.
func serverHosts(addr string, extra []string) []string {
	hosts := make([]string, 0, len(extra)+5)
	hosts = append(hosts, extra...)
	if host, _, err := net.SplitHostPort(addr); err == nil && host != "" && host != "0.0.0.0" && host != "::" {
		hosts = append(hosts, host)
	}
	hosts = append(hosts, "localhost", "127.0.0.1", "::1")
	if name, err := os.Hostname(); err == nil {
		hosts = append(hosts, name)
	}
	return uniqueStrings(hosts)
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
