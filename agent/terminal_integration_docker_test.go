package agent

import (
	"context"
	"io"
	"strings"
	"sync"
	"testing"
	"time"
)

// The tests below are the required real-container proof for JUS-263 (gated by
// GOTHAM_E2E=1 like the other live Docker suites). They never touch a
// pre-existing container: every case starts its own bounded throwaway
// container and removes it in cleanup.

// execBuffer is a concurrency-safe sink for one interactive exec.
type execBuffer struct {
	mu     sync.Mutex
	buffer strings.Builder
}

func (b *execBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buffer.Write(p)
}

func (b *execBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buffer.String()
}

// readExec starts draining an exec session in the background.
func readExec(session ExecSession) (*execBuffer, <-chan struct{}) {
	buffer := &execBuffer{}
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = io.Copy(buffer, session)
	}()
	return buffer, done
}

// waitExecOutput waits until output contains marker.
func waitExecOutput(t *testing.T, buffer *execBuffer, marker string) string {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if text := buffer.String(); strings.Contains(text, marker) {
			return text
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %q; output %q", marker, buffer.String())
	return ""
}

// closeExec ends the session and waits for the reader, bounded.
func closeExec(t *testing.T, session ExecSession, done <-chan struct{}) {
	t.Helper()
	_ = session.Close()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Error("exec reader did not stop after Close")
	}
}

// TestDockerExecRealBashIntegration proves a bash container session gets the
// prompt marks and terminal description with no image change.
func TestDockerExecRealBashIntegration(t *testing.T) {
	client := e2eContainerClient(t)
	containerID := e2eContainerImage(t, client, "debian:bookworm-slim")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	session, err := client.Exec(ctx, containerID, ExecOptions{Integration: true, Cols: 200, Rows: 40})
	if err != nil {
		t.Fatalf("Exec: %v", err)
	}
	buffer, done := readExec(session)
	defer closeExec(t, session, done)
	if shell := session.Shell(); !strings.HasSuffix(shell, "/bash") {
		t.Fatalf("shell = %q; want the probed bash", shell)
	}
	waitExecOutput(t, buffer, "\x1b]133;B")

	if _, err := session.Write([]byte("echo \"ENV:$TERM:$COLORTERM:$LANG\"\n")); err != nil {
		t.Fatalf("write env: %v", err)
	}
	text := waitExecOutput(t, buffer, "ENV:")
	if !strings.Contains(text, "ENV:xterm-256color:truecolor:") {
		t.Fatalf("shell environment %q; want TERM=xterm-256color COLORTERM=truecolor", text)
	}
	if _, err := session.Write([]byte("false\n")); err != nil {
		t.Fatalf("write false: %v", err)
	}
	if _, err := session.Write([]byte("echo AFTER-\"\"FALSE\n")); err != nil {
		t.Fatalf("write after: %v", err)
	}
	waitExecOutput(t, buffer, "AFTER-FALSE")

	marks := scanPromptMarks(buffer.String())
	kinds := markKinds(marks)
	if !strings.Contains(kinds, "DAB") {
		t.Fatalf("marks %q; want ordered D A B", kinds)
	}
	if !strings.Contains(kinds, "ABC") {
		t.Fatalf("marks %q; bash 5 must emit C after B", kinds)
	}
	statusOne := false
	for _, mark := range marks {
		if mark.kind == 'D' && mark.status == "1" {
			statusOne = true
		}
	}
	if !statusOne {
		t.Fatalf("marks %q; want a D;1 after the failed command", kinds)
	}
}

// TestDockerExecRealAlpineWithoutBash proves an image with no bash keeps the
// plain shell and never emits marks.
func TestDockerExecRealAlpineWithoutBash(t *testing.T) {
	client := e2eContainerClient(t)
	containerID := e2eContainer(t, client) // alpine:3
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	session, err := client.Exec(ctx, containerID, ExecOptions{Integration: true, Cols: 200, Rows: 40})
	if err != nil {
		t.Fatalf("Exec: %v", err)
	}
	buffer, done := readExec(session)
	defer closeExec(t, session, done)
	if shell := session.Shell(); shell != "/bin/sh" {
		t.Fatalf("shell = %q; alpine has no bash and must keep /bin/sh", shell)
	}
	if _, err := session.Write([]byte("echo ALPINE-\"\"OK\n")); err != nil {
		t.Fatalf("write: %v", err)
	}
	waitExecOutput(t, buffer, "ALPINE-OK")
	if strings.Contains(buffer.String(), "\x1b]133;") {
		t.Fatalf("a shell without bash emitted OSC 133 marks: %q", buffer.String())
	}
}

// TestDockerExecRealBash32 proves the integration works on bash 3.2 (A/B/D, no
// PS0 C mark) using the official bash:3.2 image. It skips when the image cannot
// be pulled.
func TestDockerExecRealBash32(t *testing.T) {
	client := e2eContainerClient(t)
	containerID := e2eContainerImage(t, client, "bash:3.2")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	session, err := client.Exec(ctx, containerID, ExecOptions{Integration: true, Cols: 200, Rows: 40})
	if err != nil {
		t.Fatalf("Exec: %v", err)
	}
	buffer, done := readExec(session)
	defer closeExec(t, session, done)
	waitExecOutput(t, buffer, "\x1b]133;B")
	if _, err := session.Write([]byte("echo \"V:$BASH_VERSION:$TERM\"\n")); err != nil {
		t.Fatalf("write: %v", err)
	}
	text := waitExecOutput(t, buffer, "V:3.2")
	if !strings.Contains(text, "V:3.2.57(1)-release:xterm-256color") {
		t.Fatalf("bash 3.2 environment %q", text)
	}
	if _, err := session.Write([]byte("false\n")); err != nil {
		t.Fatalf("write false: %v", err)
	}
	if _, err := session.Write([]byte("echo AFTER-\"\"32\n")); err != nil {
		t.Fatalf("write after: %v", err)
	}
	waitExecOutput(t, buffer, "AFTER-32")

	kinds := markKinds(scanPromptMarks(buffer.String()))
	if !strings.Contains(kinds, "DAB") {
		t.Fatalf("marks %q; bash 3.2 must emit ordered D A B", kinds)
	}
	statusOne := false
	for _, mark := range scanPromptMarks(buffer.String()) {
		if mark.kind == 'D' && mark.status == "1" {
			statusOne = true
		}
	}
	if !statusOne {
		t.Fatalf("marks %q; want a D;1 after the failed command", kinds)
	}
}
