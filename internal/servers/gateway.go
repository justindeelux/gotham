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
	// defaultGatewayAddr is used when no gRPC address is configured.
	defaultGatewayAddr = ":9442"
	// nodeIDMetadataKey identifies a node on the dev (non-mTLS) listener.
	nodeIDMetadataKey = "node-id"
	// enrollmentTokenMetadataKey carries the operator-configured bootstrap
	// credential on a node's first-ever Register (JUS-194).
	enrollmentTokenMetadataKey = "enrollment-token"
	// maxMetadataValueLength caps a metadata value before it is compared, so a
	// hostile peer cannot make the gateway work on an unbounded string.
	maxMetadataValueLength = 512
	// gatewayGracefulStop bounds graceful shutdown before forcing a stop.
	gatewayGracefulStop = 5 * time.Second
	// maxConcurrentStreams caps streams per connection; an unauthenticated peer
	// must not be able to open unbounded Heartbeat streams.
	maxConcurrentStreams = 64
	// maxRecvMsgSize caps a single inbound gRPC message (1 MiB): registration
	// and heartbeat payloads are tiny.
	maxRecvMsgSize = 1 << 20
	// heartbeatIdleTimeout closes a Heartbeat stream that stops sending. The
	// agent heartbeats every 10s, so a stream quiet for this long is dead or
	// hostile.
	heartbeatIdleTimeout = 2 * time.Minute
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
	// HeartbeatIdle bounds how long a Heartbeat stream may stay quiet before the
	// gateway closes it. Zero selects the production default; tests set a
	// smaller value.
	HeartbeatIdle time.Duration
	// MaxConcurrentStreams overrides the per-connection stream cap. Zero selects
	// the production default; tests set a small value to exercise the cap.
	MaxConcurrentStreams uint32
	// EnrollmentToken, when set, is the bootstrap credential a node must present
	// (metadata "enrollment-token") on its first-ever Register, before it holds
	// a client certificate. Set from GOTHAM_GRPC_ENROLLMENT_TOKEN.
	EnrollmentToken string
	// AllowOpenEnrollment opts into trust-on-first-use first enrollment when no
	// EnrollmentToken is configured. It must be an explicit, loudly-logged
	// operator choice: with neither, first enrollment is refused (fail closed).
	// Set from GOTHAM_GRPC_ALLOW_OPEN_ENROLLMENT=true.
	AllowOpenEnrollment bool
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

	// reservedNodeIDs are the control plane's own listener hosts; enrolling one
	// as a node would mint a CP-impersonation certificate (LOW-4 short-term
	// guard). heartbeatIdle bounds an idle Heartbeat stream.
	reservedNodeIDs map[string]bool
	heartbeatIdle   time.Duration
	// enrollmentToken gates first-ever enrollment on the TLS listener.
	enrollmentToken string
	// allowOpenEnrollment permits certificate-less first enrollment with no
	// token configured (explicit operator opt-in).
	allowOpenEnrollment bool

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
		addr:                addr,
		authority:           cfg.Authority,
		service:             cfg.Service,
		logger:              logger,
		reservedNodeIDs:     reservedNodeIDSet(addr, cfg.Hosts),
		heartbeatIdle:       cfg.HeartbeatIdle,
		enrollmentToken:     cfg.EnrollmentToken,
		allowOpenEnrollment: cfg.AllowOpenEnrollment,
		registerLimiter:     newPeerRateLimiter(registerRateLimit, registerBurst),
		heartbeatLimiter:    newPeerRateLimiter(heartbeatRateLimit, heartbeatBurst),
	}
	if g.heartbeatIdle <= 0 {
		g.heartbeatIdle = heartbeatIdleTimeout
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
// On the TLS listener the caller must authenticate: a caller presenting a client
// certificate must claim the identity that certificate asserts and must prove
// the key its node enrolled with; the one certificate-less case, a node's very
// first enrollment, is the gated bootstrap (a CSR plus, when configured, the
// enrollment token) and is refused for a node that is already key-bound. The dev
// (non-mTLS) listener has no authority to verify against and keeps the
// metadata-only identity.
//
// The node identity recorded in the registry is always req.NodeId. When the
// agent also supplies a CSR, the certificate is issued for req.NodeId's own
// public key (the CSR's SANs must match it; see Authority.IssueAgentCertFromCSR)
// rather than the CSR's self-asserted SAN list, and a renewal must sign for the
// same key the node already authenticated with. The CSR is validated before the
// registry is touched, so a malformed request fails without side effects.
func (g *Gateway) Register(ctx context.Context, req *agentv1.RegisterRequest) (*agentv1.RegisterResponse, error) {
	if !g.registerLimiter.allow(peerHost(ctx)) {
		g.logger.Warn("servers: registration rate limit exceeded", "peer", peerAddress(ctx))
		return nil, status.Error(codes.ResourceExhausted, "too many registration attempts")
	}

	nodeID := strings.TrimSpace(req.GetNodeId())
	if err := validateNodeID(nodeID); err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	identity, cert, err := g.peerNodeIdentity(ctx)
	if err != nil {
		return nil, err
	}
	if identity != "" && identity != nodeID {
		g.logger.Warn("servers: register node_id does not match the authenticated peer",
			"node_id", nodeID, "peer_identity", identity, "peer", peerAddress(ctx))
		return nil, status.Error(codes.PermissionDenied, "node_id does not match the authenticated peer")
	}
	// Only the co-located agent may enroll the mandatory localhost identity.
	localPeer := net.ParseIP(peerHost(ctx))
	if g.isReservedNodeID(nodeID) && (nodeID != "localhost" || localPeer == nil || !localPeer.IsLoopback() || g.isReservedNodeID(localhostAgentServerName)) {
		g.logger.Warn("servers: register node_id is reserved for the control plane listener",
			"node_id", nodeID, "peer", peerAddress(ctx))
		return nil, status.Error(codes.PermissionDenied, reservedNodeIDMessage)
	}

	// Authenticate before any signing: an established node proves its key and
	// that a renewal signs for the same key; a certificate-less caller must pass
	// the bootstrap gate.
	if g.tlsEnabled {
		if identity != "" {
			if err := g.authorizeEstablished(ctx, nodeID, cert, req.GetCsr()); err != nil {
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
		// Bind the key the certificate was issued for. An established node was
		// already authorized above (this is a no-op re-read); a bootstrap node
		// is bound here, once RegisterNode has created its row.
		if g.tlsEnabled && csrFingerprint != "" {
			if err := g.nodeKeyError(nodeID, identity, g.service.AuthorizeNodeKey(ctx, nodeID, csrFingerprint)); err != nil {
				return nil, err
			}
		}
	}
	return resp, nil
}

// authorizeEstablished authenticates an agent that presented a client
// certificate: its node must exist, the presented key must be the key the node
// is bound to (bound on first authenticated use), and a renewal CSR must sign
// for that same key.
func (g *Gateway) authorizeEstablished(ctx context.Context, nodeID string, cert *x509.Certificate, csr []byte) error {
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
	return g.nodeKeyError(nodeID, "", g.service.AuthorizeNodeKey(ctx, nodeID, fingerprint))
}

// authorizeBootstrap gates a first-ever enrollment. The enrollment token is
// checked FIRST, so a caller without a valid token learns nothing about which
// node ids exist (uniform refusal, no existence oracle). Only then does it
// require a CSR (the identity is pinned to its key) and refuse a node that
// already holds a bound key.
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

// checkEnrollmentToken gates first-ever enrollment. When the operator configured
// an enrollment token, the request must present it (constant-time compared).
// With no token configured, enrollment fails closed unless the operator
// explicitly opted into open enrollment (GOTHAM_GRPC_ALLOW_OPEN_ENROLLMENT),
// in which case it is trust-on-first-use and logged loudly. The error is the
// same whether the token is missing or wrong, so it is not an oracle.
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

// nodeKeyError maps a node-key authorization failure to a gRPC status. Every
// failure is a refusal with the SAME message: an unknown node, a key mismatch
// and a revoked node are indistinguishable to the caller, so a certificate
// holder cannot use the error to probe the registry (the distinct reason is
// logged server-side).
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
// acknowledges once the stream closes. On the TLS listener the node identity
// comes from the verified peer certificate; the dev listener falls back to the
// node-id metadata it carries. When both are present and differ, the stream is
// refused. The certificate is re-checked against the node on every message, so
// removing or rotating a node closes its live stream at the next heartbeat.
func (g *Gateway) Heartbeat(stream grpc.ClientStreamingServer[agentv1.HeartbeatRequest, agentv1.HeartbeatResponse]) error {
	ctx := stream.Context()
	peer := peerAddress(ctx)
	limiterKey := peerHost(ctx)

	nodeID, cert, err := g.heartbeatIdentity(ctx, peer)
	if err != nil {
		return err
	}
	if nodeID == "" {
		g.logger.Warn("servers: heartbeat from unidentifiable peer", "peer", peer)
	}
	// The node's bound key, resolved once per message below; a certificate-less
	// (dev) identity has none.
	var keyFingerprint string
	if cert != nil {
		fingerprint, err := NodeKeyFingerprint(cert.PublicKey)
		if err != nil {
			return status.Error(codes.Internal, "internal error")
		}
		keyFingerprint = fingerprint
	}

	// Receive on a helper goroutine so the handler can apply an idle deadline;
	// the goroutine exits when the stream's context is canceled after return.
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
			// Revocation: a node removed or re-keyed after the stream opened
			// fails this check and the stream closes.
			if keyFingerprint != "" {
				if err := g.nodeKeyError(nodeID, "", g.service.AuthorizeNodeKey(ctx, nodeID, keyFingerprint)); err != nil {
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

// heartbeatIdentity resolves the node id for a heartbeat stream and rejects a
// metadata identity that contradicts the authenticated peer certificate. On the
// TLS listener a client certificate is required; the dev listener has none and
// falls back to the node-id metadata.
func (g *Gateway) heartbeatIdentity(ctx context.Context, peer string) (string, *x509.Certificate, error) {
	authenticated, cert, err := g.peerNodeIdentity(ctx)
	if err != nil {
		return "", nil, err
	}
	claimed := metadataNodeID(ctx)
	if authenticated != "" && claimed != "" && authenticated != claimed {
		g.logger.Warn("servers: heartbeat node_id does not match the authenticated peer",
			"node_id", claimed, "peer_identity", authenticated, "peer", peer)
		return "", nil, status.Error(codes.PermissionDenied, "node_id does not match the authenticated peer")
	}
	if authenticated != "" {
		return authenticated, cert, nil
	}
	if g.tlsEnabled {
		g.logger.Warn("servers: heartbeat from a peer without a client certificate", "peer", peer)
		return "", nil, status.Error(codes.Unauthenticated, "client certificate is required")
	}
	return claimed, nil, nil
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
// On the TLS listener the caller must authenticate like any other RPC: a node
// presenting no client certificate (or one for a node it is not) is refused. It
// does not record the reported version: the version map is fed exclusively by
// heartbeats, which resolve the node in the registry first.
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

// serverCredentials builds the mTLS transport credentials for the listener.
//
// The listener *requests* a client certificate but does not verify it in the TLS
// handshake (tls.RequestClientCert): the gateway verifies it per RPC against the
// authority with Authority.VerifyNodeClientCert, which accepts both a leaf issued
// before mutual TLS (serverAuth only) and a dual-EKU leaf. Verifying in the
// handshake (RequireAndVerifyClientCert) would reject every already-enrolled
// node's old leaf and would make the certificate-less bootstrap Register
// impossible.
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

// authorizeRequest authenticates a unary RPC that carries no node id of its own
// (RequestUpdate): on the TLS listener it requires a verified client certificate
// bound to an enrolled node, and on the dev listener it falls back to the node-id
// metadata. The returned id is the authenticated node id ("" on the dev
// listener when the peer sent no metadata).
func (g *Gateway) authorizeRequest(ctx context.Context) (string, error) {
	identity, cert, err := g.peerNodeIdentity(ctx)
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
	fingerprint, err := NodeKeyFingerprint(cert.PublicKey)
	if err != nil {
		return "", status.Error(codes.Internal, "internal error")
	}
	if err := g.nodeKeyError(identity, "", g.service.AuthorizeNodeKey(ctx, identity, fingerprint)); err != nil {
		return "", err
	}
	return identity, nil
}

// peerClientCert returns the certificate a peer presented, or nil. Verification
// is the caller's job: the listener only requests the certificate.
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

// peerNodeIdentity returns the node identity proven by a verified client
// certificate the peer presented, or "" when it presented none. A certificate
// that does not verify against the authority is a hard refusal: the peer is
// unauthenticated rather than anonymous.
func (g *Gateway) peerNodeIdentity(ctx context.Context) (string, *x509.Certificate, error) {
	cert := peerClientCert(ctx)
	if cert == nil || g.authority == nil {
		return "", nil, nil
	}
	if err := g.authority.VerifyNodeClientCert(cert); err != nil {
		g.logger.Warn("servers: rejecting a peer client certificate", "peer", peerAddress(ctx), "error", err)
		return "", nil, status.Error(codes.Unauthenticated, "client certificate is not valid for this control plane")
	}
	return nodeIDFromCertIdentity(certIdentity(cert)), cert, nil
}

// nodeIDFromCertIdentity maps a certificate identity to the node id it proves.
// The localhost agent's leaf deliberately carries a dedicated SAN (it must not
// be valid for the control plane's loopback listener), so it maps back to the
// mandatory localhost node id.
func nodeIDFromCertIdentity(identity string) string {
	if identity == localhostAgentServerName {
		return "localhost"
	}
	return identity
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

// metadataValue returns the first value of key, or "" when it is absent or
// over-long. An over-long value is ignored rather than trusted, so a hostile
// peer cannot make the caller work on an unbounded string.
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

// reservedNodeIDSet builds the set of CP listener identities that must never be
// enrollable as nodes: registration would otherwise mint a CP-impersonation
// certificate. Each identity is canonicalized so equivalent spellings cannot
// slip past the lookup, and the literal bind address is added so a wildcard bind
// still protects its loopback form.
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

// reservedNodeIDMessage is the error for an enrollable id that names the CP's
// own listener. It names the fix so a co-located agent (whose default node id is
// the hostname) is actionable rather than a bare "reserved".
const reservedNodeIDMessage = "node_id is reserved for the control plane listener; " +
	"set a distinct GOTHAM_AGENT_NODE_ID for this node (it must not match the control plane's own hostname or address)"

// canonicalNodeID normalizes a node id / host for identity comparison: it
// lowercases, trims all trailing dots (a fully qualified name), strips one IPv6
// bracket pair, and, when the result parses as an IP address, uses the canonical
// form. Without this, "::ffff:127.0.0.1" and "0:0:0:0:0:0:0:1" (both equal
// 127.0.0.1 / ::1) would enroll as distinct node ids and mint a control-plane
// certificate.
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

// isReservedNodeID reports whether nodeID names the CP's own listener. Both the
// candidate and the set are canonicalized first. The list is a short-term guard;
// a role-separated CA/bootstrap credential is the real fix (LOW-4).
func (g *Gateway) isReservedNodeID(nodeID string) bool {
	return g.reservedNodeIDs[canonicalNodeID(nodeID)]
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
