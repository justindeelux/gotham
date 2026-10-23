package servers

import (
	"context"
	"crypto/subtle"
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
	defaultGatewayAddr         = ":9442"
	nodeIDMetadataKey          = "node-id"
	enrollmentTokenMetadataKey = "enrollment-token"
	maxMetadataValueLength     = 512
	gatewayGracefulStop        = 5 * time.Second
	maxConcurrentStreams       = 64
	maxRecvMsgSize             = 1 << 20
	heartbeatIdleTimeout       = 2 * time.Minute
)

type GatewayConfig struct {
	Addr                 string
	Authority            *Authority
	Service              *ServerService
	Logger               *slog.Logger
	Hosts                []string
	HeartbeatIdle        time.Duration
	MaxConcurrentStreams uint32
	EnrollmentToken      string
	AllowOpenEnrollment  bool
	LegacyLeafDeadline   time.Time
	Now                  func() time.Time
}

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

	reservedNodeIDs     map[string]bool
	heartbeatIdle       time.Duration
	enrollmentToken     string
	allowOpenEnrollment bool
	legacyLeafDeadline  time.Time
	now                 func() time.Time

	registerLimiter  *peerRateLimiter
	heartbeatLimiter *peerRateLimiter
}

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
		addr:                addr,
		authority:           cfg.Authority,
		service:             cfg.Service,
		logger:              logger,
		reservedNodeIDs:     reservedNodeIDSet(addr, cfg.Hosts),
		heartbeatIdle:       cfg.HeartbeatIdle,
		enrollmentToken:     cfg.EnrollmentToken,
		allowOpenEnrollment: cfg.AllowOpenEnrollment,
		legacyLeafDeadline:  cfg.LegacyLeafDeadline,
		now:                 cfg.Now,
		registerLimiter:     newPeerRateLimiter(registerRateLimit, registerBurst),
		heartbeatLimiter:    newPeerRateLimiter(heartbeatRateLimit, heartbeatBurst),
	}
	if g.heartbeatIdle <= 0 {
		g.heartbeatIdle = heartbeatIdleTimeout
	}
	if g.now == nil {
		g.now = time.Now
	}
	maxStreams := cfg.MaxConcurrentStreams
	if maxStreams == 0 {
		maxStreams = maxConcurrentStreams
	}

	opts := make([]grpc.ServerOption, 0, 3)
	opts = append(opts, grpc.MaxConcurrentStreams(maxStreams), grpc.MaxRecvMsgSize(maxRecvMsgSize))
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

func (g *Gateway) Addr() string {
	return g.addr
}

func (g *Gateway) Register(ctx context.Context, req *agentv1.RegisterRequest) (*agentv1.RegisterResponse, error) {
	if !g.registerLimiter.allow(peerHost(ctx)) {
		g.logger.Warn("servers: registration rate limit exceeded", "peer", peerAddress(ctx))
		return nil, status.Error(codes.ResourceExhausted, "too many registration attempts")
	}

	nodeID := strings.TrimSpace(req.GetNodeId())
	if err := validateNodeID(nodeID); err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	identity, cert, legacyLeaf, err := g.peerNodeIdentity(ctx)
	if err != nil {
		return nil, err
	}
	if identity != "" && identity != nodeID {
		g.logger.Warn("servers: register node_id does not match the authenticated peer",
			"node_id", nodeID, "peer_identity", identity, "peer", peerAddress(ctx))
		return nil, status.Error(codes.PermissionDenied, "node_id does not match the authenticated peer")
	}
	if err := g.admitNodeLeaf(ctx, nodeID, legacyLeaf); err != nil {
		return nil, err
	}
	localPeer := net.ParseIP(peerHost(ctx))
	if g.isReservedNodeID(nodeID) && (nodeID != "localhost" || localPeer == nil || !localPeer.IsLoopback() || g.isReservedNodeID(localhostAgentServerName)) {
		g.logger.Warn("servers: register node_id is reserved for the control plane listener",
			"node_id", nodeID, "peer", peerAddress(ctx))
		return nil, status.Error(codes.PermissionDenied, reservedNodeIDMessage)
	}

	if g.tlsEnabled {
		if identity != "" {
			if err := g.authorizeEstablished(ctx, nodeID, cert, req.GetCsr(), legacyLeaf); err != nil {
				return nil, err
			}
		} else if err := g.authorizeBootstrap(ctx, nodeID, req.GetCsr()); err != nil {
			return nil, err
		}
	}

	var csrCert []byte
	var csrFingerprint string
	if len(req.GetCsr()) > 0 && g.authority != nil {
		issued, err := g.authority.IssueAgentCertFromCSR(req.GetCsr(), nodeID)
		if err != nil {
			return nil, status.Error(codes.InvalidArgument, err.Error())
		}
		csrCert = issued
		if fingerprint, err := CSRKeyFingerprint(req.GetCsr()); err == nil {
			csrFingerprint = fingerprint
		}
	}

	resp, err := g.service.RegisterNode(ctx, req)
	if err != nil {
		return nil, g.registerError(nodeID, err)
	}
	if csrCert != nil {
		resp.Cert = csrCert
		if g.tlsEnabled && csrFingerprint != "" {
			if err := g.nodeKeyError(nodeID, identity, g.service.AuthorizeNodeKey(ctx, nodeID, csrFingerprint, legacyLeaf)); err != nil {
				return nil, err
			}
		}
	}
	return resp, nil
}

func (g *Gateway) authorizeEstablished(ctx context.Context, nodeID string, cert *x509.Certificate, csr []byte, legacyLeaf bool) error {
	fingerprint, err := NodeKeyFingerprint(cert.PublicKey)
	if err != nil {
		return status.Error(codes.Internal, "internal error")
	}
	if len(csr) > 0 {
		csrFingerprint, err := CSRKeyFingerprint(csr)
		if err != nil {
			return status.Error(codes.InvalidArgument, err.Error())
		}
		if csrFingerprint != fingerprint {
			g.logger.Warn("servers: registration CSR key differs from the authenticated certificate",
				"node_id", nodeID, "peer", peerAddress(ctx))
			return status.Error(codes.PermissionDenied, "certificate signing request key does not match the authenticated certificate")
		}
	}
	return g.nodeKeyError(nodeID, "", g.service.AuthorizeNodeKey(ctx, nodeID, fingerprint, legacyLeaf))
}

func (g *Gateway) authorizeBootstrap(ctx context.Context, nodeID string, csr []byte) error {
	if err := g.checkEnrollmentToken(ctx, nodeID); err != nil {
		return err
	}
	if len(csr) == 0 {
		g.logger.Warn("servers: registration without a client certificate or a certificate signing request",
			"node_id", nodeID, "peer", peerAddress(ctx))
		return status.Error(codes.Unauthenticated, "a client certificate is required")
	}
	if _, enrolled, err := g.service.NodeKeyFingerprint(ctx, nodeID); err != nil {
		g.logger.Error("servers: node key lookup failed", "node_id", nodeID, "error", err)
		return status.Error(codes.Internal, "internal error")
	} else if enrolled {
		g.logger.Warn("servers: enrollment without a client certificate for an already enrolled node",
			"node_id", nodeID, "peer", peerAddress(ctx))
		return status.Error(codes.PermissionDenied, "the node is already enrolled; present its client certificate")
	}
	return nil
}

func (g *Gateway) checkEnrollmentToken(ctx context.Context, nodeID string) error {
	presented := metadataValue(ctx, enrollmentTokenMetadataKey)
	if g.enrollmentToken == "" {
		if !g.allowOpenEnrollment {
			g.logger.Warn("servers: first enrollment refused; no GOTHAM_GRPC_ENROLLMENT_TOKEN is configured and open enrollment is not enabled",
				"node_id", nodeID, "peer", peerAddress(ctx))
			return status.Error(codes.PermissionDenied, "enrollment is closed")
		}
		g.logger.Warn("servers: enrolling a node with open enrollment enabled (GOTHAM_GRPC_ALLOW_OPEN_ENROLLMENT); first enrollment is trust-on-first-use",
			"node_id", nodeID, "peer", peerAddress(ctx))
		return nil
	}
	if subtle.ConstantTimeCompare([]byte(presented), []byte(g.enrollmentToken)) != 1 {
		g.logger.Warn("servers: enrollment token rejected", "node_id", nodeID, "peer", peerAddress(ctx))
		return status.Error(codes.PermissionDenied, "enrollment token is missing or invalid")
	}
	return nil
}

func (g *Gateway) nodeKeyError(nodeID, peerIdentity string, err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, ErrNotFound):
		g.logger.Warn("servers: agent certificate for an unknown node", "node_id", nodeID, "peer_identity", peerIdentity)
		return status.Error(codes.PermissionDenied, "agent certificate is not valid for this node")
	case errors.Is(err, ErrConflict):
		g.logger.Warn("servers: agent certificate key does not match the enrolled node",
			"node_id", nodeID, "peer_identity", peerIdentity)
		return status.Error(codes.PermissionDenied, "agent certificate is not valid for this node")
	default:
		g.logger.Error("servers: node key authorization failed", "node_id", nodeID, "error", err)
		return status.Error(codes.Internal, "internal error")
	}
}

func (g *Gateway) registerError(nodeID string, err error) error {
	if !isDomainError(err) {
		g.logger.Error("servers: register failed", "node_id", nodeID, "error", err)
	}
	return toGRPCError(err)
}

func isDomainError(err error) bool {
	return errors.Is(err, ErrValidation) ||
		errors.Is(err, ErrNoCredentials) ||
		errors.Is(err, ErrNotFound) ||
		errors.Is(err, ErrConflict)
}

func (g *Gateway) Heartbeat(stream grpc.ClientStreamingServer[agentv1.HeartbeatRequest, agentv1.HeartbeatResponse]) error {
	ctx := stream.Context()
	peer := peerAddress(ctx)
	limiterKey := peerHost(ctx)

	nodeID, cert, legacyLeaf, err := g.heartbeatIdentity(ctx, peer)
	if err != nil {
		return err
	}
	if nodeID == "" {
		g.logger.Warn("servers: heartbeat from unidentifiable peer", "peer", peer)
	}
	var keyFingerprint string
	if cert != nil {
		fingerprint, err := NodeKeyFingerprint(cert.PublicKey)
		if err != nil {
			return status.Error(codes.Internal, "internal error")
		}
		keyFingerprint = fingerprint
	}

	type recvResult struct {
		req *agentv1.HeartbeatRequest
		err error
	}
	received := make(chan recvResult, 1)
	go func() {
		for {
			req, err := stream.Recv()
			select {
			case received <- recvResult{req: req, err: err}:
			case <-ctx.Done():
				return
			}
			if err != nil {
				return
			}
		}
	}()

	idleFor := g.heartbeatIdle
	idle := time.NewTimer(idleFor)
	defer idle.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-idle.C:
			return status.Error(codes.DeadlineExceeded, "heartbeat stream idle")
		case result := <-received:
			if errors.Is(result.err, io.EOF) {
				return stream.SendAndClose(&agentv1.HeartbeatResponse{ReceivedAt: timestamppb.Now()})
			}
			if result.err != nil {
				return result.err
			}
			if !idle.Stop() {
				select {
				case <-idle.C:
				default:
				}
			}
			idle.Reset(idleFor)
			if !g.heartbeatLimiter.allow(limiterKey) {
				return status.Error(codes.ResourceExhausted, "heartbeat rate limit exceeded")
			}
			if nodeID == "" {
				continue
			}
			if keyFingerprint != "" {
				if err := g.nodeKeyError(nodeID, "", g.service.AuthorizeNodeKey(ctx, nodeID, keyFingerprint, legacyLeaf)); err != nil {
					return err
				}
			}
			if err := g.service.RecordHeartbeat(ctx, nodeID, result.req); err != nil {
				if errors.Is(err, ErrNotFound) {
					g.logger.Warn("servers: heartbeat for unknown node", "node_id", nodeID)
					continue
				}
				g.logger.Warn("servers: heartbeat", "node_id", nodeID, "error", err)
			}
		}
	}
}

func (g *Gateway) heartbeatIdentity(ctx context.Context, peer string) (string, *x509.Certificate, bool, error) {
	authenticated, cert, legacyLeaf, err := g.peerNodeIdentity(ctx)
	if err != nil {
		return "", nil, false, err
	}
	claimed := metadataNodeID(ctx)
	if authenticated != "" && claimed != "" && authenticated != claimed {
		g.logger.Warn("servers: heartbeat node_id does not match the authenticated peer",
			"node_id", claimed, "peer_identity", authenticated, "peer", peer)
		return "", nil, false, status.Error(codes.PermissionDenied, "node_id does not match the authenticated peer")
	}
	if authenticated != "" {
		if err := g.admitNodeLeaf(ctx, authenticated, legacyLeaf); err != nil {
			return "", nil, false, err
		}
		return authenticated, cert, legacyLeaf, nil
	}
	if g.tlsEnabled {
		g.logger.Warn("servers: heartbeat from a peer without a client certificate", "peer", peer)
		return "", nil, false, status.Error(codes.Unauthenticated, "client certificate is required")
	}
	return claimed, nil, false, nil
}

func (g *Gateway) RequestUpdate(ctx context.Context, req *agentv1.UpdateRequest) (*agentv1.UpdateResponse, error) {
	nodeID, err := g.authorizeRequest(ctx)
	if err != nil {
		return nil, err
	}

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
		ClientAuth:   tls.RequestClientCert,
		ClientCAs:    a.Pool(),
		MinVersion:   tls.VersionTLS12,
	}), nil
}

func (g *Gateway) authorizeRequest(ctx context.Context) (string, error) {
	identity, cert, legacyLeaf, err := g.peerNodeIdentity(ctx)
	if err != nil {
		return "", err
	}
	if identity == "" {
		if g.tlsEnabled {
			g.logger.Warn("servers: unauthenticated request", "peer", peerAddress(ctx))
			return "", status.Error(codes.Unauthenticated, "client certificate is required")
		}
		return metadataNodeID(ctx), nil
	}
	if claimed := metadataNodeID(ctx); claimed != "" && claimed != identity {
		g.logger.Warn("servers: request node_id does not match the authenticated peer",
			"node_id", claimed, "peer_identity", identity, "peer", peerAddress(ctx))
		return "", status.Error(codes.PermissionDenied, "node_id does not match the authenticated peer")
	}
	if err := g.admitNodeLeaf(ctx, identity, legacyLeaf); err != nil {
		return "", err
	}
	fingerprint, err := NodeKeyFingerprint(cert.PublicKey)
	if err != nil {
		return "", status.Error(codes.Internal, "internal error")
	}
	if err := g.nodeKeyError(identity, "", g.service.AuthorizeNodeKey(ctx, identity, fingerprint, legacyLeaf)); err != nil {
		return "", err
	}
	return identity, nil
}

func peerClientCert(ctx context.Context) *x509.Certificate {
	p, ok := peer.FromContext(ctx)
	if !ok || p.AuthInfo == nil {
		return nil
	}
	tlsInfo, ok := p.AuthInfo.(credentials.TLSInfo)
	if !ok || len(tlsInfo.State.PeerCertificates) == 0 {
		return nil
	}
	return tlsInfo.State.PeerCertificates[0]
}

func (g *Gateway) peerNodeIdentity(ctx context.Context) (string, *x509.Certificate, bool, error) {
	cert := peerClientCert(ctx)
	if cert == nil || g.authority == nil {
		return "", nil, false, nil
	}
	legacy, err := g.authority.VerifyNodeClientCert(cert)
	if err != nil {
		g.logger.Warn("servers: rejecting a peer client certificate", "peer", peerAddress(ctx), "error", err)
		return "", nil, false, status.Error(codes.Unauthenticated, "client certificate is not valid for this control plane")
	}
	return nodeIDFromCertIdentity(certIdentity(cert)), cert, legacy, nil
}

func (g *Gateway) admitNodeLeaf(ctx context.Context, nodeID string, legacy bool) error {
	if !legacy {
		return nil
	}
	if !g.legacyLeafDeadline.IsZero() && !g.now().Before(g.legacyLeafDeadline) {
		g.logger.Error("servers: rejecting a legacy serverAuth-only node certificate after the sunset",
			"node_id", nodeID, "deadline", g.legacyLeafDeadline.Format(time.RFC3339), "peer", peerAddress(ctx))
		return status.Error(codes.Unauthenticated, "node certificate predates mutual TLS; re-enroll the node")
	}
	g.logger.Warn("servers: accepted a legacy serverAuth-only node certificate; it stops working after the sunset",
		"node_id", nodeID, "deadline", g.legacyLeafDeadline.Format(time.RFC3339), "peer", peerAddress(ctx))
	return nil
}

func nodeIDFromCertIdentity(identity string) string {
	if identity == localhostAgentServerName {
		return "localhost"
	}
	return identity
}

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

func metadataValue(ctx context.Context, key string) string {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return ""
	}
	values := md.Get(key)
	if len(values) == 0 {
		return ""
	}
	value := strings.TrimSpace(values[0])
	if len(value) > maxMetadataValueLength {
		return ""
	}
	return value
}

func peerAddress(ctx context.Context) string {
	if p, ok := peer.FromContext(ctx); ok && p.Addr != nil {
		return p.Addr.String()
	}
	return "unknown"
}

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

func reservedNodeIDSet(addr string, extraHosts []string) map[string]bool {
	ids := map[string]bool{}
	for _, host := range serverHosts(addr, extraHosts) {
		ids[canonicalNodeID(host)] = true
	}
	if _, _, err := net.SplitHostPort(addr); err == nil {
		ids[canonicalNodeID(addr)] = true
	}
	return ids
}

const reservedNodeIDMessage = "node_id is reserved for the control plane listener; " +
	"set a distinct GOTHAM_AGENT_NODE_ID for this node (it must not match the control plane's own hostname or address)"

func canonicalNodeID(id string) string {
	id = strings.ToLower(strings.TrimSpace(id))
	id = strings.TrimRight(id, ".")
	if len(id) >= 2 && strings.HasPrefix(id, "[") && strings.HasSuffix(id, "]") {
		id = id[1 : len(id)-1]
	}
	if ip := net.ParseIP(id); ip != nil {
		return ip.String()
	}
	return id
}

func (g *Gateway) isReservedNodeID(nodeID string) bool {
	return g.reservedNodeIDs[canonicalNodeID(nodeID)]
}

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
