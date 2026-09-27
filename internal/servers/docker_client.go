package servers

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"

	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
)

const (
	// DefaultAgentPort is the agent DockerService gRPC listen port. It matches
	// the agent defaultListenAddr (:9443). The servers table port column
	// carries the SSH port, so agent dial targets always use this port.
	DefaultAgentPort = 9443

	// controlPlaneClientName is the common name of the ephemeral client
	// certificate the control plane presents when dialing an agent.
	controlPlaneClientName = "gotham-control-plane"
)

// DockerClient is a DockerService client bound to its connection. It embeds
// agentv1.DockerServiceClient, so all eight RPCs are exposed directly:
// ListContainers, StartContainer, StopContainer, RestartContainer, PullImage,
// CreateContainer, RunImage, and StreamLogs. Close releases the connection
// when the caller is done; Conn exposes the raw connection for health checks.
type DockerClient struct {
	agentv1.DockerServiceClient
	conn *grpc.ClientConn
}

// Compile-time guarantee that DockerClient exposes the full DockerService
// surface, including the StreamLogs server-streaming RPC.
var _ agentv1.DockerServiceClient = (*DockerClient)(nil)

// Close releases the underlying gRPC connection. It is safe to call twice;
// the second call returns the connection's close error.
func (c *DockerClient) Close() error {
	if c == nil || c.conn == nil {
		return nil
	}
	return c.conn.Close()
}

// Conn returns the underlying gRPC connection, for health or state checks.
func (c *DockerClient) Conn() *grpc.ClientConn {
	if c == nil {
		return nil
	}
	return c.conn
}

// dockerDialConfig carries the optional dial overrides.
type dockerDialConfig struct {
	serverName string
	dialOpts   []grpc.DialOption
}

// DockerDialOption customizes DialDockerClient.
type DockerDialOption func(*dockerDialConfig)

// WithDockerServerName overrides the TLS server name used to verify the
// agent's certificate. By default the server name is derived from the dial
// target's host, which fails when dialing by IP a certificate issued for a
// node id: pass the node id explicitly in that case. The service-level dial
// helpers set this from the registry automatically.
func WithDockerServerName(name string) DockerDialOption {
	return func(c *dockerDialConfig) {
		c.serverName = name
	}
}

// WithDockerDialOptions appends raw gRPC dial options, for example a bufconn
// context dialer in tests.
func WithDockerDialOptions(opts ...grpc.DialOption) DockerDialOption {
	return func(c *dockerDialConfig) {
		c.dialOpts = append(c.dialOpts, opts...)
	}
}

// DialDockerClient dials the agent DockerService at target (host or
// host:port) over mTLS and returns a client exposing all eight DockerService
// RPCs. The caller owns the returned client and must Close it.
//
// Credentials reuse the Phase 2 authority: the client presents an ephemeral
// certificate issued via Authority.IssueClientCert and verifies the agent's
// server certificate against Authority.Pool. No cert logic is duplicated here.
// A nil authority selects insecure transport for local development only.
func DialDockerClient(ctx context.Context, target string, authority *Authority, opts ...DockerDialOption) (*DockerClient, error) {
	if ctx == nil {
		return nil, errors.New("servers: context is nil")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	target = strings.TrimSpace(target)
	if target == "" {
		return nil, fmt.Errorf("%w: dial target is empty", ErrValidation)
	}
	target = normalizeDockerTarget(target)

	cfg := &dockerDialConfig{}
	for _, opt := range opts {
		if opt != nil {
			opt(cfg)
		}
	}
	serverName := cfg.serverName
	if serverName == "" {
		serverName = serverNameFromTarget(target)
	}

	transport, err := dockerTransportCredentials(authority, serverName)
	if err != nil {
		return nil, err
	}

	dialOpts := make([]grpc.DialOption, 0, len(cfg.dialOpts)+1)
	dialOpts = append(dialOpts, grpc.WithTransportCredentials(transport))
	dialOpts = append(dialOpts, cfg.dialOpts...)

	conn, err := grpc.NewClient(target, dialOpts...)
	if err != nil {
		return nil, fmt.Errorf("servers: dial agent %s: %w", target, err)
	}
	return &DockerClient{
		DockerServiceClient: agentv1.NewDockerServiceClient(conn),
		conn:                conn,
	}, nil
}

// dockerTransportCredentials builds the dial credentials from the Phase 2
// authority: an ephemeral client certificate plus CA-pool server verification.
// A nil authority returns insecure credentials for development.
func dockerTransportCredentials(authority *Authority, serverName string) (credentials.TransportCredentials, error) {
	if authority == nil {
		return insecure.NewCredentials(), nil
	}
	certPEM, keyPEM, err := authority.IssueClientCert(controlPlaneClientName)
	if err != nil {
		return nil, fmt.Errorf("servers: issue control-plane client certificate: %w", err)
	}
	keyPair, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return nil, fmt.Errorf("servers: load control-plane keypair: %w", err)
	}
	return credentials.NewTLS(&tls.Config{
		Certificates: []tls.Certificate{keyPair},
		RootCAs:      authority.Pool(),
		ServerName:   serverName,
		MinVersion:   tls.VersionTLS12,
	}), nil
}

// normalizeDockerTarget appends the default agent port to a bare host.
// Targets that already carry a port or a URL scheme (passthrough:///bufnet
// for bufconn tests) are returned unchanged.
func normalizeDockerTarget(target string) string {
	if strings.Contains(target, "://") {
		return target
	}
	if _, _, err := net.SplitHostPort(target); err == nil {
		return target
	}
	// A bare IPv6 literal without brackets still joins correctly.
	host := strings.Trim(target, "[]")
	return net.JoinHostPort(host, strconv.Itoa(DefaultAgentPort))
}

// serverNameFromTarget derives the TLS server name from a dial target,
// stripping any scheme, port, and IPv6 brackets.
func serverNameFromTarget(target string) string {
	host := target
	if strings.Contains(target, "://") {
		// passthrough:///bufnet and similar: no meaningful host.
		return ""
	}
	if h, _, err := net.SplitHostPort(target); err == nil {
		host = h
	}
	return strings.Trim(host, "[]")
}

// AgentTarget resolves a server id to its agent dial target (host:port) and
// the TLS server name the agent certificate verifies against. It reuses the
// Phase 2 registry via ServerService.Get and shares agentTargetFor with the
// node-id variant so address logic lives in one place.
func (s *ServerService) AgentTarget(ctx context.Context, id uuid.UUID) (target, serverName string, err error) {
	if s == nil || s.store == nil {
		return "", "", errors.New("servers: store is not configured")
	}
	server, err := s.Get(ctx, id)
	if err != nil {
		return "", "", err
	}
	return agentTargetFor(server)
}

// AgentTargetByNodeID resolves a node id to its agent dial target and TLS
// server name, reusing the Phase 2 heartbeat/register lookup.
func (s *ServerService) AgentTargetByNodeID(ctx context.Context, nodeID string) (target, serverName string, err error) {
	if s == nil || s.store == nil {
		return "", "", errors.New("servers: store is not configured")
	}
	nodeID = strings.TrimSpace(nodeID)
	if nodeID == "" {
		return "", "", fmt.Errorf("%w: node id is required", ErrValidation)
	}
	row, err := s.store.GetServerByNodeID(ctx, &nodeID)
	if err != nil {
		return "", "", fmt.Errorf("lookup server by node id: %w", err)
	}
	return agentTargetFor(serverFromRow(row))
}

// DialDockerClient dials the agent for the server id, resolving the target
// and server name from the registry and presenting the Phase 2 client
// credentials. An explicit WithDockerServerName option wins over the resolved
// name. The caller must Close the returned client.
func (s *ServerService) DialDockerClient(ctx context.Context, id uuid.UUID, opts ...DockerDialOption) (*DockerClient, error) {
	target, serverName, err := s.AgentTarget(ctx, id)
	if err != nil {
		return nil, err
	}
	return s.dialResolved(ctx, target, serverName, opts)
}

// DialDockerClientByNodeID dials the agent for the node id. The caller must
// Close the returned client.
func (s *ServerService) DialDockerClientByNodeID(ctx context.Context, nodeID string, opts ...DockerDialOption) (*DockerClient, error) {
	target, serverName, err := s.AgentTargetByNodeID(ctx, nodeID)
	if err != nil {
		return nil, err
	}
	return s.dialResolved(ctx, target, serverName, opts)
}

// dialResolved dials a registry-resolved target, injecting the resolved
// server name unless the caller overrode it explicitly.
func (s *ServerService) dialResolved(ctx context.Context, target, serverName string, opts []DockerDialOption) (*DockerClient, error) {
	if s == nil {
		return nil, errors.New("servers: service is nil")
	}
	if !hasServerNameOverride(opts) && serverName != "" {
		opts = append([]DockerDialOption{WithDockerServerName(serverName)}, opts...)
	}
	return DialDockerClient(ctx, target, s.authority, opts...)
}

// hasServerNameOverride reports whether opts carry an explicit server name.
func hasServerNameOverride(opts []DockerDialOption) bool {
	cfg := &dockerDialConfig{}
	for _, opt := range opts {
		if opt != nil {
			opt(cfg)
		}
	}
	return cfg.serverName != ""
}

// agentTargetFor maps a registry server to its dial target and TLS server
// name. The servers port column is the SSH port, so the agent port is always
// DefaultAgentPort. The server name prefers the node id (the SAN agents
// request at registration) and falls back to the dial host. When the stored
// IP is empty the node id is used as the host, which covers nodes that
// registered before an operator recorded their address.
func agentTargetFor(server *Server) (target, serverName string, err error) {
	if server == nil {
		return "", "", fmt.Errorf("%w: server is nil", ErrValidation)
	}
	host := strings.TrimSpace(server.IP)
	var nodeID string
	if server.NodeID != nil {
		nodeID = strings.TrimSpace(*server.NodeID)
	}
	if host == "" {
		host = nodeID
	}
	if host == "" {
		return "", "", fmt.Errorf("%w: server has no dialable address", ErrValidation)
	}
	// A stored value that already carries a port (or scheme) is honored.
	if strings.Contains(host, "://") {
		target = host
	} else if _, _, splitErr := net.SplitHostPort(host); splitErr == nil {
		target = host
	} else {
		target = net.JoinHostPort(strings.Trim(host, "[]"), strconv.Itoa(DefaultAgentPort))
	}

	serverName = nodeID
	if serverName == "" {
		serverName = serverNameFromTarget(target)
	}
	return target, serverName, nil
}
