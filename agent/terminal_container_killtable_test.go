package agent

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
)

// The round-2 review table, run against real alpine:3, busybox:1.36 and
// debian:bookworm-slim containers. Each case opens a terminal, starts the job,
// closes, and then scans /proc from a fresh exec (not the kill helper) for the
// session's unique environment marker.
var killTableCases = []struct {
	name  string
	input string
}{
	{"foreground", "sleep 7101\n"},
	{"trap-ignores-hup-term-int", "trap '' HUP TERM INT; sleep 7102\n"},
	{"nohup-background", "nohup sleep 7103 &\n"},
	{"plain-background", "sleep 7104 &\n"},
	{"setsid-background", "(setsid sleep 7105 &); sleep 7106\n"},
	{"nested-background", "( (sleep 7107 &) & ); sleep 7108\n"},
	{"shell-exit-before-close", "exit 0\n"},
}

func TestDockerExecRealKillTable(t *testing.T) {
	client := e2eContainerClient(t)
	for _, image := range []string{"alpine:3", "busybox:1.36", "debian:bookworm-slim"} {
		t.Run(image, func(t *testing.T) {
			containerID := e2eContainerImage(t, client, image)
			for _, testCase := range killTableCases {
				t.Run(testCase.name, func(t *testing.T) {
					ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
					defer cancel()
					session, err := client.Exec(ctx, containerID, ExecOptions{Cols: 100, Rows: 30})
					if err != nil {
						t.Fatalf("Exec: %v", err)
					}
					dockerSession, ok := session.(*dockerExecSession)
					if !ok {
						t.Fatalf("session type = %T", session)
					}
					if _, err := session.Write([]byte(testCase.input)); err != nil {
						t.Fatalf("write: %v", err)
					}
					time.Sleep(450 * time.Millisecond)
					if err := session.Close(); err != nil {
						t.Fatalf("Close: %v", err)
					}
					running, err := client.execRunning(context.Background(), dockerSession.id)
					if err != nil {
						t.Fatalf("inspect exec: %v", err)
					}
					if running {
						t.Fatal("Docker still reports the exec as Running after Close")
					}
					waitNoMarker(t, client, containerID, dockerSession.marker)
					if !containerRunning(t, client, containerID) {
						t.Fatal("closing the terminal stopped the application container")
					}
				})
			}
		})
	}
}

// TestDockerExecRealTransportLossTable repeats the hardest rows through the real
// terminal server and a cancelled stream, then scans by command line because the
// server owns the generated marker.
func TestDockerExecRealTransportLossTable(t *testing.T) {
	client := e2eContainerClient(t)
	cases := []struct {
		name    string
		input   string
		needles []string
	}{
		{"trap", "trap '' HUP TERM INT; sleep 7301\n", []string{"sleep 7301"}},
		{"nohup", "nohup sleep 7302 &\n", []string{"sleep 7302"}},
		{"setsid", "(setsid sleep 7303 &); sleep 7304\n", []string{"sleep 7303", "sleep 7304"}},
		{"nested", "( (sleep 7305 &) & ); sleep 7306\n", []string{"sleep 7305", "sleep 7306"}},
	}
	for _, image := range []string{"alpine:3", "debian:bookworm-slim"} {
		t.Run(image, func(t *testing.T) {
			containerID := e2eContainerImage(t, client, image)
			terminal := NewTerminalServer(TerminalConfig{Logger: discardLogger(), Containers: client})
			grpcClient := startTerminalServer(t, terminal)
			for _, testCase := range cases {
				t.Run(testCase.name, func(t *testing.T) {
					ctx, cancel := context.WithCancel(context.Background())
					stream, err := grpcClient.OpenTerminal(ctx)
					if err != nil {
						t.Fatalf("OpenTerminal: %v", err)
					}
					if err := stream.Send(&agentv1.TerminalRequest{Part: &agentv1.TerminalRequest_Open{Open: &agentv1.TerminalOpen{
						Target: "container", ContainerId: containerID, Cols: 100, Rows: 30,
					}}}); err != nil {
						t.Fatalf("send open: %v", err)
					}
					if _, err := stream.Recv(); err != nil {
						t.Fatalf("recv opened: %v", err)
					}
					if err := stream.Send(&agentv1.TerminalRequest{Part: &agentv1.TerminalRequest_Input{Input: []byte(testCase.input)}}); err != nil {
						t.Fatalf("send job: %v", err)
					}
					time.Sleep(450 * time.Millisecond)
					cancel()
					for _, needle := range testCase.needles {
						waitNoCmdline(t, client, containerID, needle)
					}
					if !containerRunning(t, client, containerID) {
						t.Fatal("transport loss stopped the application container")
					}
				})
			}
		})
	}
}

// TestDockerExecRealJobControl proves Ctrl-Z, jobs, bg and fg still behave
// normally through the pid-recording wrapper on sh and bash.
func TestDockerExecRealJobControl(t *testing.T) {
	client := e2eContainerClient(t)
	for _, image := range []string{"alpine:3", "debian:bookworm-slim"} {
		t.Run(image, func(t *testing.T) {
			containerID := e2eContainerImage(t, client, image)
			terminal := NewTerminalServer(TerminalConfig{Logger: discardLogger(), Containers: client})
			grpcClient := startTerminalServer(t, terminal)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			stream, err := grpcClient.OpenTerminal(ctx)
			if err != nil {
				t.Fatalf("OpenTerminal: %v", err)
			}
			if err := stream.Send(&agentv1.TerminalRequest{Part: &agentv1.TerminalRequest_Open{Open: &agentv1.TerminalOpen{
				Target: "container", ContainerId: containerID, Cols: 100, Rows: 30,
			}}}); err != nil {
				t.Fatalf("send open: %v", err)
			}
			if _, err := stream.Recv(); err != nil {
				t.Fatalf("recv opened: %v", err)
			}
			send := func(data string) {
				t.Helper()
				if err := stream.Send(&agentv1.TerminalRequest{Part: &agentv1.TerminalRequest_Input{Input: []byte(data)}}); err != nil {
					t.Fatalf("send %q: %v", data, err)
				}
			}
			send("sleep 7400\n")
			time.Sleep(300 * time.Millisecond)
			send(string([]byte{0x1a})) // Ctrl-Z
			time.Sleep(200 * time.Millisecond)
			send("jobs\n")
			waitForOutput(t, stream, "Stopped")
			send("bg\n")
			time.Sleep(200 * time.Millisecond)
			send("jobs\n")
			waitForOutput(t, stream, "Running")
			send("fg\n")
			time.Sleep(300 * time.Millisecond)
			send(string([]byte{0x03})) // Ctrl-C interrupts the resumed job
			time.Sleep(300 * time.Millisecond)
			// The output marker is not the input text, so this proves the
			// command actually ran after the interrupted fg.
			send("echo JOB-$((6*7))\n")
			waitForOutput(t, stream, "JOB-42")
			send("exit\n")
			_ = stream.CloseSend()
			waitNoCmdline(t, client, containerID, "sleep 7400")
			if !containerRunning(t, client, containerID) {
				t.Fatal("job control stopped the application container")
			}
		})
	}
}

// TestDockerExecRealConcurrentCleanupPaths races CloseSend and cancellation on
// the same session, then exercises the 8 h cap through a short MaxDuration.
func TestDockerExecRealConcurrentCleanupPaths(t *testing.T) {
	client := e2eContainerClient(t)
	containerID := e2eContainer(t, client)

	terminal := NewTerminalServer(TerminalConfig{Logger: discardLogger(), Containers: client})
	grpcClient := startTerminalServer(t, terminal)
	for i := 0; i < 5; i++ {
		ctx, cancel := context.WithCancel(context.Background())
		stream, err := grpcClient.OpenTerminal(ctx)
		if err != nil {
			t.Fatalf("OpenTerminal: %v", err)
		}
		if err := stream.Send(&agentv1.TerminalRequest{Part: &agentv1.TerminalRequest_Open{Open: &agentv1.TerminalOpen{
			Target: "container", ContainerId: containerID, Cols: 100, Rows: 30,
		}}}); err != nil {
			t.Fatalf("send open: %v", err)
		}
		if _, err := stream.Recv(); err != nil {
			t.Fatalf("recv opened: %v", err)
		}
		if err := stream.Send(&agentv1.TerminalRequest{Part: &agentv1.TerminalRequest_Input{Input: []byte("sleep 7500\n")}}); err != nil {
			t.Fatalf("send job: %v", err)
		}
		time.Sleep(200 * time.Millisecond)
		var wait sync.WaitGroup
		wait.Add(2)
		go func() { defer wait.Done(); _ = stream.CloseSend() }()
		go func() { defer wait.Done(); cancel() }()
		wait.Wait()
		waitNoCmdline(t, client, containerID, "sleep 7500")
	}
	if !containerRunning(t, client, containerID) {
		t.Fatal("concurrent close/cancel stopped the application container")
	}

	// The lifetime cap uses the same terminate path.
	capped := NewTerminalServer(TerminalConfig{
		Logger:      discardLogger(),
		Containers:  client,
		MaxDuration: 1500 * time.Millisecond,
	})
	cappedClient := startTerminalServer(t, capped)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stream, err := cappedClient.OpenTerminal(ctx)
	if err != nil {
		t.Fatalf("OpenTerminal (cap): %v", err)
	}
	if err := stream.Send(&agentv1.TerminalRequest{Part: &agentv1.TerminalRequest_Open{Open: &agentv1.TerminalOpen{
		Target: "container", ContainerId: containerID, Cols: 100, Rows: 30,
	}}}); err != nil {
		t.Fatalf("send open (cap): %v", err)
	}
	if _, err := stream.Recv(); err != nil {
		t.Fatalf("recv opened (cap): %v", err)
	}
	if err := stream.Send(&agentv1.TerminalRequest{Part: &agentv1.TerminalRequest_Input{Input: []byte("sleep 7501\n")}}); err != nil {
		t.Fatalf("send job (cap): %v", err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := stream.Recv(); err != nil {
			break
		}
	}
	waitNoCmdline(t, client, containerID, "sleep 7501")
	if !containerRunning(t, client, containerID) {
		t.Fatal("the lifetime cap stopped the application container")
	}
}

// TestDockerExecRealCleanupFailureIsSurfaced removes the helper's /bin/sh after
// the terminal is open. Close must return an error and must not pretend the exec
// is gone; the container is removed by cleanup.
func TestDockerExecRealCleanupFailureIsSurfaced(t *testing.T) {
	client := e2eContainerClient(t)
	containerID := e2eContainer(t, client)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	session, err := client.Exec(ctx, containerID, ExecOptions{Cols: 100, Rows: 30})
	if err != nil {
		t.Fatalf("Exec: %v", err)
	}
	if _, err := session.Write([]byte("chmod -x /bin/sh; sleep 7600\n")); err != nil {
		t.Fatalf("write: %v", err)
	}
	time.Sleep(400 * time.Millisecond)
	closeErr := session.Close()
	if closeErr == nil || (!strings.Contains(closeErr.Error(), "cleanup incomplete") && !strings.Contains(closeErr.Error(), "still running")) {
		t.Fatalf("Close = %v, want a surfaced cleanup failure", closeErr)
	}
	running, inspectErr := client.execRunning(context.Background(), session.(*dockerExecSession).id)
	if inspectErr != nil {
		t.Fatalf("inspect exec: %v", inspectErr)
	}
	if !running {
		t.Fatal("cleanup failure was reported but the exec is already gone")
	}
}
