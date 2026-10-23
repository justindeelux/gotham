package agent

import (
	"context"
	"testing"
	"time"

	"github.com/justindeelux/gotham/agent/docker"
	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
)

func TestServerServeAndShutdown(t *testing.T) {
	fake := &fakeDockerClient{containers: []*agentv1.ContainerInfo{{Id: "abc"}}}
	creds, err := ServerCredentials(nil, nil, "", true)
	if err != nil {
		t.Fatalf("ServerCredentials: %v", err)
	}
	server, err := NewServer("127.0.0.1:0", creds, NewDockerServer(fake, discardLogger()), discardLogger())
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	serveErr := make(chan error, 1)
	go func() { serveErr <- server.Serve(ctx) }()

	conn, err := grpc.NewClient(server.Addr().String(),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = conn.Close() }()

	client := agentv1.NewDockerServiceClient(conn)
	callCtx, callCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer callCancel()
	response, err := client.ListContainers(callCtx, &agentv1.ListContainersRequest{})
	if err != nil {
		t.Fatalf("ListContainers: %v", err)
	}
	if len(response.GetContainers()) != 1 {
		t.Errorf("containers = %v; want one", response.GetContainers())
	}

	cancel()
	select {
	case err := <-serveErr:
		if err != nil {
			t.Fatalf("Serve: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("server did not shut down")
	}
}

func TestNewServerRefusesPlaintextOffLoopback(t *testing.T) {
	fake := &fakeDockerClient{}

	for _, addr := range []string{"0.0.0.0:0", ":0", "[::]:0"} {
		if _, err := NewServer(addr, nil, NewDockerServer(fake, discardLogger()), discardLogger()); err == nil {
			t.Errorf("NewServer(plaintext, %q) = nil error, want loopback refusal", addr)
		}
	}

	server, err := NewServer("127.0.0.1:0", nil, NewDockerServer(fake, discardLogger()), discardLogger())
	if err != nil {
		t.Fatalf("NewServer(plaintext, loopback) = %v; want success", err)
	}
	t.Cleanup(func() { _ = server.ln.Close() })

	certPEM, keyPEM, err := generateSelfSigned()
	if err != nil {
		t.Fatalf("generateSelfSigned: %v", err)
	}
	creds, err := ServerCredentials(certPEM, keyPEM, "", false)
	if err != nil {
		t.Fatalf("ServerCredentials: %v", err)
	}
	tlsServer, err := NewServer("0.0.0.0:0", creds, NewDockerServer(fake, discardLogger()), discardLogger())
	if err != nil {
		t.Fatalf("NewServer(TLS, wildcard) = %v; want success", err)
	}
	t.Cleanup(func() { _ = tlsServer.ln.Close() })
}

func TestAgentRecvCapExceedsComposeLimit(t *testing.T) {
	const maxComposeYAML = 1 << 20
	const composeTestProject = "gotham-11111111-2222-4333-8444-555555555555"
	if agentMaxRecvMsgSize <= maxComposeYAML {
		t.Fatalf("agentMaxRecvMsgSize = %d, must exceed maxComposeYAML = %d", agentMaxRecvMsgSize, maxComposeYAML)
	}

	fake := &fakeDockerClient{}
	creds, err := ServerCredentials(nil, nil, "", true)
	if err != nil {
		t.Fatalf("ServerCredentials: %v", err)
	}
	server, err := NewServer("127.0.0.1:0", creds, NewDockerServer(fake, discardLogger()), discardLogger(),
		WithComposeService(docker.NewComposeServer(docker.ComposeServerConfig{Root: t.TempDir()})))
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = server.Serve(ctx) }()
	t.Cleanup(cancel)

	conn, err := grpc.NewClient(server.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = conn.Close() }()

	callCtx, callCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer callCancel()
	client := agentv1.NewComposeServiceClient(conn)

	justOver := make([]byte, maxComposeYAML+1)
	for i := range justOver {
		justOver[i] = 'x'
	}
	if _, err := client.ComposeValidate(callCtx, &agentv1.ComposeValidateRequest{
		ProjectName: composeTestProject,
		ComposeYaml: justOver,
	}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("just-over-limit document = %v, want InvalidArgument (not a transport cap)", err)
	}

	overCap := make([]byte, agentMaxRecvMsgSize+1)
	for i := range overCap {
		overCap[i] = 'x'
	}
	if _, err := client.ComposeValidate(callCtx, &agentv1.ComposeValidateRequest{
		ProjectName: composeTestProject,
		ComposeYaml: overCap,
	}); status.Code(err) != codes.ResourceExhausted {
		t.Fatalf("over-cap payload = %v, want ResourceExhausted", err)
	}

	exact := make([]byte, maxComposeYAML)
	copy(exact, "services:\n  web:\n    image: nginx\n")
	for i := len("services:\n  web:\n    image: nginx\n"); i < len(exact); i++ {
		exact[i] = ' '
	}
	if _, err := client.ComposeValidate(callCtx, &agentv1.ComposeValidateRequest{
		ProjectName: composeTestProject,
		ComposeYaml: exact,
	}); status.Code(err) == codes.ResourceExhausted {
		t.Fatalf("exact-limit document = ResourceExhausted, want it to reach validation")
	}
}
