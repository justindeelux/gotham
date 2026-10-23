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
	DefaultAgentPort = 9443

	controlPlaneClientName = "gotham-control-plane"
)

type DockerClient struct {
	agentv1.DockerServiceClient
	conn *grpc.ClientConn
}

var _ agentv1.DockerServiceClient = (*DockerClient)(nil)

func (c *DockerClient) Close() error {
	if c == nil || c.conn == nil {
		return nil
	}
	return c.conn.Close()
}

func (c *DockerClient) Conn() *grpc.ClientConn {
	if c == nil {
		return nil
	}
	return c.conn
}

type dockerDialConfig struct {
	serverName string
	dialOpts   []grpc.DialOption
}

type DockerDialOption func(*dockerDialConfig)

func WithDockerServerName(name string) DockerDialOption {
	return func(c *dockerDialConfig) {
		c.serverName = name
	}
}

func WithDockerDialOptions(opts ...grpc.DialOption) DockerDialOption {
	return func(c *dockerDialConfig) {
		c.dialOpts = append(c.dialOpts, opts...)
	}
}

func DialDockerClient(ctx context.Context, target string, authority *Authority, opts ...DockerDialOption) (*DockerClient, error) {
	conn, err := dialAgentConn(ctx, target, authority, opts...)
	if err != nil {
		return nil, err
	}
	return &DockerClient{
		DockerServiceClient: agentv1.NewDockerServiceClient(conn),
		conn:                conn,
	}, nil
}

func dialAgentConn(ctx context.Context, target string, authority *Authority, opts ...DockerDialOption) (*grpc.ClientConn, error) {
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
	return conn, nil
}

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

func normalizeDockerTarget(target string) string {
	if strings.Contains(target, "://") {
		return target
	}
	if _, _, err := net.SplitHostPort(target); err == nil {
		return target
	}
	host := strings.Trim(target, "[]")
	return net.JoinHostPort(host, strconv.Itoa(DefaultAgentPort))
}

func serverNameFromTarget(target string) string {
	host := target
	if strings.Contains(target, "://") {
		return ""
	}
	if h, _, err := net.SplitHostPort(target); err == nil {
		host = h
	}
	return strings.Trim(host, "[]")
}

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

func (s *ServerService) DialDockerClient(ctx context.Context, id uuid.UUID, opts ...DockerDialOption) (*DockerClient, error) {
	target, serverName, err := s.AgentTarget(ctx, id)
	if err != nil {
		return nil, err
	}
	conn, err := s.dialResolved(ctx, target, serverName, opts)
	if err != nil {
		return nil, err
	}
	return &DockerClient{
		DockerServiceClient: agentv1.NewDockerServiceClient(conn),
		conn:                conn,
	}, nil
}

func (s *ServerService) DialDockerClientByNodeID(ctx context.Context, nodeID string, opts ...DockerDialOption) (*DockerClient, error) {
	target, serverName, err := s.AgentTargetByNodeID(ctx, nodeID)
	if err != nil {
		return nil, err
	}
	conn, err := s.dialResolved(ctx, target, serverName, opts)
	if err != nil {
		return nil, err
	}
	return &DockerClient{
		DockerServiceClient: agentv1.NewDockerServiceClient(conn),
		conn:                conn,
	}, nil
}

func (s *ServerService) dialResolved(ctx context.Context, target, serverName string, opts []DockerDialOption) (*grpc.ClientConn, error) {
	if s == nil {
		return nil, errors.New("servers: service is nil")
	}
	if !hasServerNameOverride(opts) && serverName != "" {
		opts = append([]DockerDialOption{WithDockerServerName(serverName)}, opts...)
	}
	return dialAgentConn(ctx, target, s.authority, opts...)
}

func hasServerNameOverride(opts []DockerDialOption) bool {
	cfg := &dockerDialConfig{}
	for _, opt := range opts {
		if opt != nil {
			opt(cfg)
		}
	}
	return cfg.serverName != ""
}

func agentTargetFor(server *Server) (target, serverName string, err error) {
	if server == nil {
		return "", "", fmt.Errorf("%w: server is nil", ErrValidation)
	}
	host := strings.TrimSpace(server.IP)
	var nodeID string
	if server.NodeID != nil {
		nodeID = strings.TrimSpace(*server.NodeID)
	}
	if nodeID == "localhost" {
		host = "127.0.0.1"
	}
	if host == "" {
		host = nodeID
	}
	if host == "" {
		return "", "", fmt.Errorf("%w: server has no dialable address", ErrValidation)
	}
	if strings.Contains(host, "://") {
		target = host
	} else if _, _, splitErr := net.SplitHostPort(host); splitErr == nil {
		target = host
	} else {
		target = net.JoinHostPort(strings.Trim(host, "[]"), strconv.Itoa(DefaultAgentPort))
	}

	serverName = nodeID
	if nodeID == "localhost" {
		serverName = localhostAgentServerName
	}
	if serverName == "" {
		serverName = serverNameFromTarget(target)
	}
	return target, serverName, nil
}
