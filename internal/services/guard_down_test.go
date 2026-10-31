package services

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"

	"github.com/justindeelux/gotham/agent"
	"github.com/justindeelux/gotham/internal/servers"
	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
)

func installNoopComposeCLI(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "docker"), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write fake docker: %v", err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func TestLegacyUnconfinedServiceCanBeStoppedAndDeleted(t *testing.T) {
	installNoopComposeCLI(t)
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("canonical temp dir: %v", err)
	}
	composeServer := agent.NewComposeServer(agent.ComposeServerConfig{Root: root, Logger: discardLogger()})

	serviceID := uuid.New()
	userID := uuid.New()
	legacy := "services:\n  web:\n    image: nginx:1.27\n    privileged: true\n    volumes:\n      - /etc/passwd:/etc/passwd:ro\n"
	if _, err := composeServer.ComposeDown(context.Background(), &agentv1.ComposeDownRequest{
		ProjectName: ProjectName(serviceID),
		ComposeYaml: []byte(legacy),
	}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("confined teardown of a legacy document = %v, want InvalidArgument", err)
	}

	listener := bufconn.Listen(1 << 20)
	grpcServer := grpc.NewServer()
	agentv1.RegisterComposeServiceServer(grpcServer, composeServer)
	go func() { _ = grpcServer.Serve(listener) }()
	t.Cleanup(grpcServer.Stop)
	dialer := func(ctx context.Context, _ string) (net.Conn, error) {
		return listener.DialContext(ctx)
	}

	repo := newFakeRepository()
	if _, err := repo.CreateService(context.Background(), Service{
		ID: serviceID, UserID: userID, ServerID: uuid.New(), Name: "legacy", Status: StatusRunning,
		ComposeYAML: legacy, Env: map[string]string{},
	}); err != nil {
		t.Fatalf("CreateService: %v", err)
	}
	svc := NewService(Config{
		Repository: repo,
		Logger:     discardLogger(),
		Dial: func(dialCtx context.Context, _ uuid.UUID) (ComposeAgent, error) {
			client, err := servers.DialComposeClient(dialCtx, "passthrough:///bufnet", nil,
				servers.WithDockerDialOptions(grpc.WithContextDialer(dialer)))
			if err != nil {
				return nil, err
			}
			return NewGRPCComposeAgent(client), nil
		},
	})
	t.Cleanup(func() { closeTestService(t, svc) })

	stopped, err := svc.Stop(context.Background(), userID, serviceID)
	if err != nil {
		t.Fatalf("Stop of a legacy document: %v", err)
	}
	if stopped.Status != StatusStopped {
		t.Fatalf("status after Stop = %q, want %q", stopped.Status, StatusStopped)
	}
	if _, err := svc.Delete(context.Background(), userID, serviceID); err != nil {
		t.Fatalf("Delete of a legacy document: %v", err)
	}
	if _, err := repo.GetService(context.Background(), serviceID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetService after Delete = %v, want ErrNotFound", err)
	}
}
