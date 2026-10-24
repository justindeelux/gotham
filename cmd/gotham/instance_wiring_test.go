package main

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/peer"

	"github.com/justindeelux/gotham/internal/servers"
	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
)

type instanceAgentFixture struct {
	node      *servers.Server
	authority *servers.Authority
	target    string
}

func (f *instanceAgentFixture) Get(_ context.Context, id uuid.UUID) (*servers.Server, error) {
	if id != f.node.ID {
		return nil, servers.ErrNotFound
	}
	return f.node, nil
}

func (f *instanceAgentFixture) DialDockerClient(ctx context.Context, id uuid.UUID, opts ...servers.DockerDialOption) (*servers.DockerClient, error) {
	if id != f.node.ID {
		return nil, servers.ErrNotFound
	}
	return servers.DialDockerClient(ctx, f.target, f.authority, append(opts, servers.WithDockerServerName("localhost.agent.gotham.invalid"))...)
}

type instanceDockerAgent struct {
	agentv1.UnimplementedDockerServiceServer
}

func (instanceDockerAgent) ListContainers(ctx context.Context, _ *agentv1.ListContainersRequest) (*agentv1.ListContainersResponse, error) {
	p, ok := peer.FromContext(ctx)
	if !ok {
		return nil, fmt.Errorf("missing TLS peer")
	}
	info, ok := p.AuthInfo.(credentials.TLSInfo)
	if !ok || len(info.State.PeerCertificates) == 0 || info.State.PeerCertificates[0].Subject.CommonName != "gotham-control-plane" {
		return nil, fmt.Errorf("expected authenticated control-plane client")
	}
	return &agentv1.ListContainersResponse{Containers: []*agentv1.ContainerInfo{{Id: "traefik", Name: "gotham-traefik", State: "running"}}}, nil
}

func TestInstanceContainerAgentWiringMTLS(t *testing.T) {
	authority, err := servers.LoadOrCreateAuthority(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	cert, key, err := authority.IssueServerCert([]string{"localhost.agent.gotham.invalid"})
	if err != nil {
		t.Fatal(err)
	}
	pair, err := tls.X509KeyPair(cert, key)
	if err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	rpc := grpc.NewServer(grpc.Creds(credentials.NewTLS(&tls.Config{
		Certificates: []tls.Certificate{pair}, ClientAuth: tls.RequireAndVerifyClientCert, ClientCAs: authority.Pool(), MinVersion: tls.VersionTLS12,
	})))
	agentv1.RegisterDockerServiceServer(rpc, instanceDockerAgent{})
	go func() { _ = rpc.Serve(listener) }()
	defer rpc.Stop()
	fixture := &instanceAgentFixture{node: &servers.Server{ID: uuid.New(), Name: "localhost", Status: servers.StatusReady}, authority: authority, target: listener.Addr().String()}
	service := newInstanceContainers(fixture, "127.0.0.1:1")
	defer func() { _ = service.Close() }()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	listed, err := service.ListFresh(ctx, fixture.node.ID)
	if err != nil {
		t.Fatalf("CLI container service must reach the mTLS agent: %v", err)
	}
	if len(listed) != 1 || listed[0].ID != "traefik" {
		t.Fatalf("containers = %+v", listed)
	}
}
