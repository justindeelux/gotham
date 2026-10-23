package agent

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/justindeelux/gotham/agent/docker"
)

func TestShouldSendEnrollmentToken(t *testing.T) {
	cases := []struct {
		name    string
		token   string
		ca      string
		hasCert bool
		want    bool
	}{
		{name: "tls and certificate-less", token: "t", ca: "/etc/gotham/ca.crt", want: true},
		{name: "plaintext is refused", token: "t", ca: "", want: false},
		{name: "no token configured", token: "", ca: "/etc/gotham/ca.crt", want: false},
		{name: "already enrolled", token: "t", ca: "/etc/gotham/ca.crt", hasCert: true, want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			if tc.hasCert {
				if err := os.WriteFile(filepath.Join(dir, certFileName), []byte("x"), 0o600); err != nil {
					t.Fatalf("write cert: %v", err)
				}
			}
			a := NewAgent(Config{CertDir: dir, CA: tc.ca, EnrollmentToken: tc.token}, discardLogger(), nil)
			if got := a.shouldSendEnrollmentToken(); got != tc.want {
				t.Fatalf("shouldSendEnrollmentToken() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestEnrollmentTokenNotSentWithoutCA(t *testing.T) {
	fake := &fakeAgentService{cpVersion: "test"}
	dialOptions := startFakeCP(t, fake)

	runner := NewAgent(Config{
		CPAddr:          "passthrough:///bufnet",
		NodeID:          "node-plaintext",
		CertDir:         t.TempDir(),
		DockerSock:      docker.DefaultDockerSocket,
		LogLevel:        "error",
		EnrollmentToken: "s3cret-enroll",
	}, discardLogger(), &fakeDockerClient{}, WithDialOptions(dialOptions...), WithHeartbeatInterval(time.Hour))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runErr := make(chan error, 1)
	go func() { runErr <- runner.Run(ctx, nil) }()

	deadline := time.Now().Add(5 * time.Second)
	for fake.registerCount() == 0 {
		if time.Now().After(deadline) {
			t.Fatal("the agent did not register with the fake control plane")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if token := fake.firstRegisterToken(); token != "" {
		t.Fatalf("the enrollment token crossed a plaintext channel: %q", token)
	}
	cancel()
	select {
	case <-runErr:
	case <-time.After(5 * time.Second):
		t.Fatal("agent.Run did not return after cancel")
	}
}
