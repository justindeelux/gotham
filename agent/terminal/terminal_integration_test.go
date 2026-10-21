package terminal

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/creack/pty"

	"github.com/justindeelux/gotham/agent/docker"
	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
)

// This file is the JUS-263 proof: bash host sessions get a real PTY with
// TERM/COLORTERM, colour defaults, the operator's own rc and OSC 133 marks;
// the integration is idempotent, optional, and cannot be driven by the caller.

// terminalTestBash returns an installed bash or skips the test.
func terminalTestBash(t *testing.T) string {
	t.Helper()
	for _, candidate := range []string{"/bin/bash", "/usr/bin/bash"} {
		if info, err := os.Stat(candidate); err == nil && info.Mode().IsRegular() && info.Mode().Perm()&0o111 != 0 {
			return candidate
		}
	}
	if path, err := exec.LookPath("bash"); err == nil {
		return path
	}
	t.Skip("bash is not installed")
	return ""
}

// bashMajorMinor reports the running bash version.
func bashMajorMinor(t *testing.T, bash string) (int, int) {
	t.Helper()
	out, err := exec.Command(bash, "-c", `printf '%s %s' "${BASH_VERSINFO[0]}" "${BASH_VERSINFO[1]}"`).Output()
	if err != nil {
		t.Fatalf("bash version: %v", err)
	}
	fields := strings.Fields(string(out))
	if len(fields) != 2 {
		t.Fatalf("bash version output %q is not major minor", out)
	}
	major, err := strconv.Atoi(fields[0])
	if err != nil {
		t.Fatalf("bash major %q: %v", fields[0], err)
	}
	minor, err := strconv.Atoi(fields[1])
	if err != nil {
		t.Fatalf("bash minor %q: %v", fields[1], err)
	}
	return major, minor
}

// promptMark is one OSC 133 mark captured from a real PTY.
type promptMark struct {
	kind   byte
	status string
}

// scanPromptMarks parses the marks in output with the same narrow rules the web
// scanner uses: OSC 133 or 633, BEL or ST terminated, A/B/C/D only. Anything
// else is ignored.
func scanPromptMarks(output string) []promptMark {
	var marks []promptMark
	for i := 0; i < len(output); i++ {
		if output[i] != 0x1b || i+1 >= len(output) || output[i+1] != ']' {
			continue
		}
		end := -1
		terminator := 0
		for j := i + 2; j < len(output); j++ {
			if output[j] == 0x07 {
				end, terminator = j, 1
				break
			}
			if output[j] == 0x1b && j+1 < len(output) && output[j+1] == '\\' {
				end, terminator = j, 2
				break
			}
		}
		if end < 0 {
			break
		}
		body := output[i+2 : end]
		i = end + terminator - 1
		parts := strings.Split(body, ";")
		if len(parts) < 2 || (parts[0] != "133" && parts[0] != "633") || parts[1] == "" {
			continue
		}
		kind := parts[1][0]
		if kind < 'A' || kind > 'D' {
			continue
		}
		mark := promptMark{kind: kind}
		if kind == 'D' && len(parts) > 2 {
			mark.status = parts[2]
		}
		marks = append(marks, mark)
	}
	return marks
}

// markKinds renders the marks in order, e.g. "DAB".
func markKinds(marks []promptMark) string {
	var kinds strings.Builder
	for _, mark := range marks {
		kinds.WriteByte(mark.kind)
	}
	return kinds.String()
}

// terminalTestPTY is a real PTY running bash with the agent's environment.
type terminalTestPTY struct {
	write   func(string)
	stop    func()
	waitFor func(string) string
	settle  func()
	all     func() string
}

// startTerminalTestBash starts bash on a real PTY with the same environment the
// agent gives a host session, with HOME pointing at home.
func startTerminalTestBash(t *testing.T, bash string, args []string, home string) *terminalTestPTY {
	t.Helper()
	env := terminalEnv()
	env = docker.TerminalEnvSet(env, "HOME", home)
	return startTerminalTestShell(t, bash, args, env)
}

// startTerminalTestShell starts shell on a real PTY with the given environment.
func startTerminalTestShell(t *testing.T, shell string, args []string, env []string) *terminalTestPTY {
	t.Helper()
	cmd := exec.Command(shell, args...)
	cmd.Env = env
	master, err := pty.StartWithSize(cmd, &pty.Winsize{Cols: 200, Rows: 40})
	if err != nil {
		t.Fatalf("start %s pty: %v", shell, err)
	}

	var mu sync.Mutex
	var output strings.Builder
	snapshot := func() string {
		mu.Lock()
		defer mu.Unlock()
		return output.String()
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		buffer := make([]byte, 32<<10)
		for {
			n, err := master.Read(buffer)
			if n > 0 {
				mu.Lock()
				output.Write(buffer[:n])
				mu.Unlock()
			}
			if err != nil {
				return
			}
		}
	}()

	return &terminalTestPTY{
		write: func(data string) {
			if _, err := master.Write([]byte(data)); err != nil {
				t.Errorf("write to pty: %v", err)
			}
		},
		all: snapshot,
		settle: func() {
			// Let the line editor take over the terminal before typing: bytes
			// written while the shell is still in canonical mode are echoed and
			// redrawn.
			last := ""
			for stable := 0; stable < 10; stable++ {
				time.Sleep(15 * time.Millisecond)
				now := snapshot()
				if now == last {
					continue
				}
				last, stable = now, 0
			}
		},
		waitFor: func(marker string) string {
			deadline := time.Now().Add(10 * time.Second)
			for time.Now().Before(deadline) {
				if text := snapshot(); strings.Contains(text, marker) {
					return text
				}
				time.Sleep(10 * time.Millisecond)
			}
			t.Fatalf("timed out waiting for %q; output %q", marker, snapshot())
			return ""
		},
		stop: func() {
			// Closing the master does not interrupt a blocking read on darwin
			// (no poller registration), so end the shell first and bound the
			// wait for the read goroutine.
			if cmd.Process != nil {
				_ = cmd.Process.Signal(syscall.SIGHUP)
			}
			_ = master.Close()
			select {
			case <-done:
			case <-time.After(2 * time.Second):
				if cmd.Process != nil {
					_ = cmd.Process.Kill()
				}
				select {
				case <-done:
				case <-time.After(2 * time.Second):
					t.Errorf("pty reader did not stop after the shell was killed")
				}
			}
			if cmd.Process != nil {
				_, _ = cmd.Process.Wait()
			}
		},
	}
}

// writeTempBashrc writes a temporary HOME with a .bashrc.
func writeTempBashrc(t *testing.T, content string) string {
	t.Helper()
	home := t.TempDir()
	if content != "" {
		if err := os.WriteFile(filepath.Join(home, ".bashrc"), []byte(content), 0o600); err != nil {
			t.Fatalf("write .bashrc: %v", err)
		}
	}
	return home
}

// TestResolveTerminalShellPreference pins the integrated shell order ($SHELL
// when bash/zsh, then bash, then zsh) and the pre-JUS-263 fallback when the
// integration is disabled.
func TestResolveTerminalShellPreference(t *testing.T) {
	statWith := func(existing map[string]os.FileMode) func(string) (os.FileInfo, error) {
		return func(path string) (os.FileInfo, error) {
			if mode, ok := existing[path]; ok {
				return fakeFileInfo{mode: mode}, nil
			}
			return nil, os.ErrNotExist
		}
	}
	lookPathMissing := func(string) (string, error) { return "", exec.ErrNotFound }
	lookPathAt := func(path string) func(string) (string, error) {
		return func(name string) (string, error) {
			if name == "bash" {
				return path, nil
			}
			return "", exec.ErrNotFound
		}
	}

	t.Run("integration honors a bash or zsh $SHELL", func(t *testing.T) {
		t.Setenv("SHELL", "/bin/zsh")
		shell, err := resolveTerminalShellWith("", true, lookPathMissing, statWith(map[string]os.FileMode{
			"/bin/bash": 0o755, "/bin/zsh": 0o755, "/bin/sh": 0o755,
		}))
		if err != nil || shell != "/bin/zsh" {
			t.Fatalf("shell = %q, err = %v; want $SHELL (/bin/zsh)", shell, err)
		}
	})
	t.Run("integration prefers bash over an unsupported $SHELL", func(t *testing.T) {
		t.Setenv("SHELL", "/usr/bin/fish")
		shell, err := resolveTerminalShellWith("", true, lookPathMissing, statWith(map[string]os.FileMode{
			"/bin/bash": 0o755, "/usr/bin/fish": 0o755, "/bin/sh": 0o755,
		}))
		if err != nil || shell != "/bin/bash" {
			t.Fatalf("shell = %q, err = %v; want /bin/bash", shell, err)
		}
	})
	t.Run("integration uses /usr/bin/bash when /bin/bash is absent", func(t *testing.T) {
		t.Setenv("SHELL", "/bin/sh")
		shell, err := resolveTerminalShellWith("", true, lookPathMissing, statWith(map[string]os.FileMode{
			"/usr/bin/bash": 0o755, "/bin/sh": 0o755,
		}))
		if err != nil || shell != "/usr/bin/bash" {
			t.Fatalf("shell = %q, err = %v; want /usr/bin/bash", shell, err)
		}
	})
	t.Run("integration uses zsh when bash is absent", func(t *testing.T) {
		t.Setenv("SHELL", "/bin/sh")
		shell, err := resolveTerminalShellWith("", true, lookPathMissing, statWith(map[string]os.FileMode{
			"/bin/zsh": 0o755, "/bin/sh": 0o755,
		}))
		if err != nil || shell != "/bin/zsh" {
			t.Fatalf("shell = %q, err = %v; want /bin/zsh", shell, err)
		}
	})
	t.Run("integration finds bash on PATH", func(t *testing.T) {
		t.Setenv("SHELL", "/bin/sh")
		shell, err := resolveTerminalShellWith("", true, lookPathAt("/nix/store/bash/bin/bash"), statWith(map[string]os.FileMode{
			"/nix/store/bash/bin/bash": 0o755, "/bin/sh": 0o755,
		}))
		if err != nil || shell != "/nix/store/bash/bin/bash" {
			t.Fatalf("shell = %q, err = %v; want the PATH bash", shell, err)
		}
	})
	t.Run("missing bash falls back to $SHELL then /bin/sh", func(t *testing.T) {
		t.Setenv("SHELL", "/bin/dash")
		shell, err := resolveTerminalShellWith("", true, lookPathMissing, statWith(map[string]os.FileMode{
			"/bin/dash": 0o755, "/bin/sh": 0o755,
		}))
		if err != nil || shell != "/bin/dash" {
			t.Fatalf("shell = %q, err = %v; want /bin/dash", shell, err)
		}
		t.Setenv("SHELL", "/bin/zsh")
		shell, err = resolveTerminalShellWith("", true, lookPathMissing, statWith(map[string]os.FileMode{
			"/bin/sh": 0o755,
		}))
		if err != nil || shell != "/bin/sh" {
			t.Fatalf("shell = %q, err = %v; want /bin/sh", shell, err)
		}
	})
	t.Run("disabled integration keeps the old order", func(t *testing.T) {
		t.Setenv("SHELL", "/bin/zsh")
		shell, err := resolveTerminalShellWith("", false, lookPathMissing, statWith(map[string]os.FileMode{
			"/bin/bash": 0o755, "/bin/zsh": 0o755, "/bin/sh": 0o755,
		}))
		if err != nil || shell != "/bin/zsh" {
			t.Fatalf("shell = %q, err = %v; want $SHELL (/bin/zsh)", shell, err)
		}
	})
	t.Run("configured shell wins and must be usable", func(t *testing.T) {
		t.Setenv("SHELL", "/bin/bash")
		shell, err := resolveTerminalShellWith("/usr/bin/zsh", true, lookPathMissing, statWith(map[string]os.FileMode{
			"/usr/bin/zsh": 0o755, "/bin/bash": 0o755,
		}))
		if err != nil || shell != "/usr/bin/zsh" {
			t.Fatalf("shell = %q, err = %v; want the configured /usr/bin/zsh", shell, err)
		}
		if _, err := resolveTerminalShellWith("/missing/shell", true, lookPathMissing, statWith(nil)); err == nil {
			t.Fatal("an unusable configured shell must not silently fall back")
		}
	})
	t.Run("no usable shell", func(t *testing.T) {
		t.Setenv("SHELL", "relative-shell")
		if _, err := resolveTerminalShellWith("", true, lookPathMissing, statWith(nil)); err == nil {
			t.Fatal("expected an error when no absolute shell exists")
		}
	})
}

// fakeFileInfo is the minimal os.FileInfo firstUsableShell needs.
type fakeFileInfo struct{ mode os.FileMode }

func (f fakeFileInfo) Name() string       { return "shell" }
func (f fakeFileInfo) Size() int64        { return 1 }
func (f fakeFileInfo) Mode() os.FileMode  { return f.mode }
func (f fakeFileInfo) ModTime() time.Time { return time.Time{} }
func (f fakeFileInfo) IsDir() bool        { return false }
func (f fakeFileInfo) Sys() any           { return nil }

// TestTerminalEnvDefaults pins the child environment: terminal description,
// a UTF-8 default only when no locale is set, correct identity variables, and
// no agent secret inherited.
func TestTerminalEnvDefaults(t *testing.T) {
	t.Setenv("LANG", "")
	t.Setenv("LC_ALL", "")
	t.Setenv("LC_CTYPE", "")
	t.Setenv("TERM", "dumb")
	t.Setenv("COLORTERM", "")
	t.Setenv("GOTHAM_AGENT_ENROLLMENT_TOKEN", "super-secret-token")

	env := terminalEnv()
	check := func(name, want string) {
		t.Helper()
		got, ok := lookupTerminalEnv(env, name)
		if !ok {
			t.Fatalf("%s missing from %v", name, env)
		}
		if got != want {
			t.Fatalf("%s = %q; want %q", name, got, want)
		}
	}
	check("TERM", "xterm-256color")
	check("COLORTERM", "truecolor")
	check("LANG", "C.UTF-8")
	for _, name := range []string{"PATH", "HOME", "USER", "LOGNAME"} {
		if value, ok := lookupTerminalEnv(env, name); !ok || value == "" {
			t.Fatalf("%s = %q (present %v); want a non-empty value", name, value, ok)
		}
	}
	if user, _ := lookupTerminalEnv(env, "USER"); user != currentUsername() {
		t.Fatalf("USER = %q; want the service user %q", user, currentUsername())
	}
	for _, entry := range env {
		if strings.Contains(entry, "super-secret-token") {
			t.Fatalf("the agent's enrollment token leaked into the shell environment: %q", entry)
		}
	}
	if count := countTerminalEnv(env, "TERM"); count != 1 {
		t.Fatalf("TERM appears %d times; want exactly one", count)
	}
}

// TestTerminalEnvKeepsExplicitLocale proves an operator-set locale is not
// replaced, including a non-UTF-8 one, and a container image locale is not
// overridden by the agent default.
func TestTerminalEnvKeepsExplicitLocale(t *testing.T) {
	t.Setenv("LANG", "en_GB.UTF-8")
	t.Setenv("LC_ALL", "")
	deleteEnv(t, "LC_CTYPE")
	env := terminalEnv()
	if value, _ := lookupTerminalEnv(env, "LANG"); value != "en_GB.UTF-8" {
		t.Fatalf("LANG = %q; want the operator's en_GB.UTF-8", value)
	}

	value, _ := lookupTerminalEnv(docker.TerminalEnvDescription(nil), "LANG")
	if value != "C.UTF-8" {
		t.Fatalf("LANG = %q; want the C.UTF-8 default", value)
	}
	value, _ = lookupTerminalEnv(docker.TerminalEnvDescription(nil, []string{"LANG=ja_JP.UTF-8"}), "LANG")
	if value != "" {
		t.Fatalf("LANG = %q; a container image locale must not be replaced", value)
	}
	value, _ = lookupTerminalEnv(docker.TerminalEnvDescription(nil, []string{"LANG=C"}), "LANG")
	if value != "" {
		t.Fatalf("LANG = %q; an explicit non-UTF-8 locale must be kept", value)
	}
}

// deleteEnv removes a variable for the duration of the test.
func deleteEnv(t *testing.T, name string) {
	t.Helper()
	value, present := os.LookupEnv(name)
	if err := os.Unsetenv(name); err != nil {
		t.Fatalf("unset %s: %v", name, err)
	}
	t.Cleanup(func() {
		if present {
			_ = os.Setenv(name, value)
			return
		}
		_ = os.Unsetenv(name)
	})
}

// lookupTerminalEnv returns the first value of name.
func lookupTerminalEnv(env []string, name string) (string, bool) {
	for _, entry := range env {
		key, value, ok := strings.Cut(entry, "=")
		if ok && key == name {
			return value, true
		}
	}
	return "", false
}

func countTerminalEnv(env []string, name string) int {
	count := 0
	for _, entry := range env {
		if key, _, ok := strings.Cut(entry, "="); ok && key == name {
			count++
		}
	}
	return count
}

// TestTerminalBashRcfileIsPrivateAndConstant proves the rcfile's location,
// permissions, cleanup and that its content never carries request data.
func TestTerminalBashRcfileIsPrivateAndConstant(t *testing.T) {
	args, cleanup, err := terminalBashRcArgs("/bin/bash", true)
	if err != nil {
		t.Fatalf("terminalBashRcArgs: %v", err)
	}
	if len(args) != 2 || args[0] != "--rcfile" {
		t.Fatalf("args = %v; want --rcfile <path>", args)
	}
	path := args[1]
	info, err := os.Stat(filepath.Dir(path))
	if err != nil {
		t.Fatalf("stat rcfile dir: %v", err)
	}
	if info.Mode().Perm() != 0o700 {
		t.Fatalf("rcfile directory mode = %o; want 700", info.Mode().Perm())
	}
	fileInfo, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat rcfile: %v", err)
	}
	if fileInfo.Mode().Perm() != 0o600 {
		t.Fatalf("rcfile mode = %o; want 600", fileInfo.Mode().Perm())
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read rcfile: %v", err)
	}
	if string(content) != docker.BashIntegrationScript {
		t.Fatal("rcfile content differs from the agent constant")
	}

	// A second session gets its own file with byte-identical content.
	secondArgs, secondCleanup, err := terminalBashRcArgs("/bin/bash", true)
	if err != nil {
		t.Fatalf("terminalBashRcArgs (second): %v", err)
	}
	if secondArgs[1] == path {
		t.Fatal("two sessions share an rcfile path; want a per-session file")
	}
	secondContent, err := os.ReadFile(secondArgs[1])
	if err != nil {
		t.Fatalf("read second rcfile: %v", err)
	}
	if string(secondContent) != string(content) {
		t.Fatal("rcfile content depends on session data; it must be a constant")
	}
	for _, value := range []string{"gotham-request-cwd", "gotham-service", "/tmp/request-root"} {
		if strings.Contains(string(content), value) {
			t.Fatalf("rcfile contains request-shaped data %q", value)
		}
	}

	cleanup()
	secondCleanup()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("rcfile still exists after cleanup: %v", err)
	}
}

func TestTerminalBashRcArgsSkipsNonBashAndDisabledIntegration(t *testing.T) {
	for _, shell := range []string{"/bin/sh", "/bin/zsh", "/usr/bin/dash", "/bin/ash"} {
		args, cleanup, err := terminalBashRcArgs(shell, true)
		if err != nil || args != nil || cleanup != nil {
			t.Fatalf("%s: args = %v, cleanup set = %v, err = %v; want no integration", shell, args, cleanup != nil, err)
		}
	}
	args, cleanup, err := terminalBashRcArgs("/bin/bash", false)
	if err != nil || args != nil || cleanup != nil {
		t.Fatalf("disabled integration: args = %v, cleanup set = %v, err = %v", args, cleanup != nil, err)
	}
}

func TestContainerBashCommand(t *testing.T) {
	wrapped := docker.ContainerBashCommand([]string{"/bin/bash"}, true)
	if len(wrapped) != 3 || wrapped[0] != "/bin/bash" || wrapped[1] != "-c" || wrapped[2] != docker.ContainerBashRcCommand() {
		t.Fatalf("wrapped = %v; want the fixed bash wrapper", wrapped)
	}
	if !strings.Contains(wrapped[2], docker.BashIntegrationScript) {
		t.Fatal("the wrapper must embed the constant integration script")
	}
	if !strings.HasPrefix(wrapped[2], docker.ContainerBashRcPrelude) || !strings.HasSuffix(wrapped[2], docker.ContainerBashRcEpilogue) {
		t.Fatal("the wrapper must use the fixed here-document prelude and fallback")
	}
	for _, command := range [][]string{{"/bin/sh"}, {"/bin/ash"}, {"/usr/bin/zsh"}} {
		original := command[0]
		if got := docker.ContainerBashCommand(command, true); len(got) != 1 || got[0] != original {
			t.Fatalf("docker.ContainerBashCommand(%v) = %v; want it unchanged", command, got)
		}
	}
	if got := docker.ContainerBashCommand([]string{"/bin/bash"}, false); len(got) != 1 || got[0] != "/bin/bash" {
		t.Fatalf("disabled integration wrapped the command: %v", got)
	}
	if got := docker.ContainerBashCommand([]string{"/bin/bash", "-l"}, true); len(got) != 2 {
		t.Fatalf("a bash command with extra args must not be wrapped: %v", got)
	}
}

// TestTerminalBashIntegrationScript runs the exact rcfile the agent writes on a
// real PTY: colours, marks, the operator's rc, status marks and idempotency.
func TestTerminalBashIntegrationScript(t *testing.T) {
	bash := terminalTestBash(t)
	major, minor := bashMajorMinor(t, bash)
	args, cleanup, err := terminalBashRcArgs(bash, true)
	if err != nil {
		t.Fatalf("terminalBashRcArgs: %v", err)
	}
	t.Cleanup(cleanup)
	rcfile := args[1]

	home := writeTempBashrc(t, strings.Join([]string{
		"export GOTHAM_TEST_USER_RC=1",
		"PS1='GOTHAMCUSTOM> '",
		"alias ls='echo GOTHAM_USER_LS'",
		"alias gotham-user-alias='echo GOTHAM_USER_ALIAS'",
		"PROMPT_COMMAND='printf \"GOTHAM_USER_PC\\n\"'",
	}, "\n"))

	shell := startTerminalTestBash(t, bash, []string{"--rcfile", rcfile}, home)
	defer shell.stop()
	shell.waitFor("GOTHAMCUSTOM> ")
	shell.settle()

	// The operator's rc is sourced, its prompt survives, and its aliases win.
	shell.write("echo \"userrc=$GOTHAM_TEST_USER_RC\"\r")
	shell.waitFor("userrc=1")
	shell.write("ls\r")
	shell.waitFor("GOTHAM_USER_LS")
	shell.write("gotham-user-alias\r")
	shell.waitFor("GOTHAM_USER_ALIAS")
	if strings.Contains(shell.all(), "\x1b[01;32m") {
		t.Fatal("the coloured default prompt overrode the operator's custom PS1")
	}

	// TERM/COLORTERM and a UTF-8 locale reach the shell.
	shell.write("echo \"env=$TERM|$COLORTERM|$LANG\"\r")
	shell.waitFor("env=xterm-256color|truecolor|")
	if localeMissing(shell.all()) {
		t.Fatalf("no UTF-8 locale reached the shell: %q", shell.all())
	}

	// PS0 carries the C mark. Asserting the variable as well as the emitted
	// escape covers bash 3.2, where PS0 exists but is not evaluated.
	shell.write("printf 'ps0=%s\\n' \"$PS0\"; echo PS0-\"\"DONE\r")
	ps0Output := shell.waitFor("PS0-DONE\r\n")
	if !strings.Contains(ps0Output, "133;C") {
		t.Fatalf("PS0 does not carry the OSC 133 C mark: %q", ps0Output)
	}

	// A succeeding then a failing command: D carries the real status. The
	// follow-up command proves the prompt after `false` was rendered.
	shell.write("true\r")
	shell.write("false\r")
	shell.write("echo after-false\r")
	shell.waitFor("after-false\r\n")

	// Sourcing the same file again is a no-op (one mark set per prompt).
	shell.write(". '" + rcfile + "'\r")
	shell.write("echo idempotent-ok\r")
	shell.waitFor("idempotent-ok\r\n")

	output := shell.all()
	marks := scanPromptMarks(output)
	if len(marks) == 0 {
		t.Fatalf("no OSC 133 marks in output %q", output)
	}
	statusOne := false
	for _, mark := range marks {
		if mark.kind == 'D' && mark.status == "1" {
			statusOne = true
		}
	}
	if !statusOne {
		t.Fatalf("no D;1 mark for the failed command; marks %q", markKinds(marks))
	}

	kinds := markKinds(marks)
	t.Logf("bash OSC 133 marks: %q", kinds)
	if strings.Count(kinds, "AB") < 2 {
		t.Fatalf("marks %q; want at least two ordered A B prompt pairs", kinds)
	}
	if major > 4 || (major == 4 && minor >= 4) {
		if !strings.Contains(kinds, "ABC") {
			t.Fatalf("marks %q; bash %d.%d must emit C (PS0) after B", kinds, major, minor)
		}
	}
	for _, pair := range []string{"BA", "CB", "DC"} {
		if strings.Contains(kinds, pair) {
			t.Fatalf("marks %q are out of order (found %q)", kinds, pair)
		}
	}
	if strings.Count(kinds, "A") != strings.Count(kinds, "B") {
		t.Fatalf("marks %q; every A must have exactly one B", kinds)
	}
	if count := strings.Count(kinds, "A"); count > 12 {
		t.Fatalf("marks %q; %d A marks suggests the rcfile doubled PROMPT_COMMAND", kinds, count)
	}
}

// TestTerminalBashRcfileIsIdempotent proves the "already loaded" guard: sourcing
// the rcfile a second time does not re-run the operator's startup file and does
// not duplicate the per-prompt marks.
func TestTerminalBashRcfileIsIdempotent(t *testing.T) {
	bash := terminalTestBash(t)
	args, cleanup, err := terminalBashRcArgs(bash, true)
	if err != nil {
		t.Fatalf("terminalBashRcArgs: %v", err)
	}
	t.Cleanup(cleanup)
	rcfile := args[1]
	home := writeTempBashrc(t, `printf 'sourced\n' >> "$HOME/rc-sources"`)
	shell := startTerminalTestBash(t, bash, []string{"--rcfile", rcfile}, home)
	defer shell.stop()
	// The prompt mark is ready once the integration hook ran; the prompt text
	// itself differs for root ("# ") and non-root ("$ ").
	shell.waitFor("\x1b]133;B")
	shell.settle()

	shell.write(". '" + rcfile + "'; echo SOURCE-\"\"DONE\r")
	shell.waitFor("SOURCE-DONE\r\n")
	shell.write("echo AFTER-\"\"SOURCE\r")
	shell.waitFor("AFTER-SOURCE\r\n")

	sources, err := os.ReadFile(filepath.Join(home, "rc-sources"))
	if err != nil {
		t.Fatalf("read rc-sources: %v", err)
	}
	if got := strings.Count(string(sources), "sourced"); got != 1 {
		t.Fatalf("the operator rc ran %d times; the second source must be a no-op (%q)", got, sources)
	}

	marks := scanPromptMarks(shell.all())
	if len(marks) == 0 {
		t.Fatal("no prompt marks")
	}
	kinds := markKinds(marks)
	if strings.Count(kinds, "A") != strings.Count(kinds, "B") || strings.Count(kinds, "D") > strings.Count(kinds, "A") {
		t.Fatalf("marks %q; sourcing twice duplicated the per-prompt marks", kinds)
	}
	if strings.Contains(kinds, "DAD") {
		t.Fatalf("marks %q; a prompt emitted D and A twice", kinds)
	}
}

// TestTerminalBashErrexitUserRcKeepsSession proves a user rc that enables
// set -e/-u cannot kill the integration: the probes run with errexit disabled
// and the user's intent is restored afterwards.
func TestTerminalBashErrexitUserRcKeepsSession(t *testing.T) {
	bash := terminalTestBash(t)
	args, cleanup, err := terminalBashRcArgs(bash, true)
	if err != nil {
		t.Fatalf("terminalBashRcArgs: %v", err)
	}
	t.Cleanup(cleanup)
	home := writeTempBashrc(t, "set -u\nset -e\n")
	shell := startTerminalTestBash(t, bash, []string{"--rcfile", args[1]}, home)
	defer shell.stop()
	// The prompt mark is ready once the integration hook ran; the prompt text
	// itself differs for root ("# ") and non-root ("$ ").
	shell.waitFor("\x1b]133;B")
	shell.settle()

	shell.write("echo ERR-\"\"EXIT-OK\r")
	shell.waitFor("ERR-EXIT-OK\r\n")
	shell.write("false\r")
	shell.write("echo STILL-\"\"ALIVE\r")
	shell.waitFor("STILL-ALIVE\r\n")
}

// TestTerminalBashStartFailureRemovesRcfile proves the integration directory is
// removed when the PTY cannot be started (the shell binary is missing).
func TestTerminalBashStartFailureRemovesRcfile(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("TMPDIR", tmp)
	terminal := NewTerminalServer(TerminalConfig{Logger: discardLogger()})
	spec := terminalStart{
		Target: terminalTargetHost,
		Shell:  filepath.Join(tmp, "missing", "bash"), // basename bash, parent absent
		Dir:    tmp,
		Cols:   80,
		Rows:   24,
	}
	if _, err := terminal.startSession(spec); err == nil {
		t.Fatal("startSession succeeded with a missing shell binary")
	}
	entries, err := os.ReadDir(tmp)
	if err != nil {
		t.Fatalf("read TMPDIR: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("rcfile directory leaked after a failed start: %v", entries)
	}
}

// TestTerminalBashThemePromptKeepsMarks proves a theme that rewrites PS1 from
// its own PROMPT_COMMAND entry does not lose the B mark: the repair function
// runs last and only appends the zero-width mark.
func TestTerminalBashThemePromptKeepsMarks(t *testing.T) {
	bash := terminalTestBash(t)
	args, cleanup, err := terminalBashRcArgs(bash, true)
	if err != nil {
		t.Fatalf("terminalBashRcArgs: %v", err)
	}
	t.Cleanup(cleanup)
	home := writeTempBashrc(t, strings.Join([]string{
		"PS1='GOTHAMCUSTOM> '",
		`PROMPT_COMMAND='PS1="THEME> "'`,
	}, "\n"))
	shell := startTerminalTestBash(t, bash, []string{"--rcfile", args[1]}, home)
	defer shell.stop()
	shell.waitFor("THEME> ")
	shell.settle()
	shell.write("echo theme-''ok\r")
	text := shell.waitFor("theme-ok\r\n")
	if strings.Count(text, "THEME> ") < 2 {
		t.Fatalf("theme prompt missing from %q", text)
	}
	if kinds := markKinds(scanPromptMarks(text)); !strings.Contains(kinds, "AB") {
		t.Fatalf("marks %q; a theme rewrite lost the B mark", kinds)
	}
}

// localeMissing reports whether the rendered env line has an empty locale.
func localeMissing(text string) bool {
	return strings.Contains(text, "env=xterm-256color|truecolor|\r\n")
}

// TestTerminalBashColourAliases proves the default aliases are installed (when
// the operator did not set one) and that `ls` emits SGR colours on a PTY.
func TestTerminalBashColourAliases(t *testing.T) {
	bash := terminalTestBash(t)
	args, cleanup, err := terminalBashRcArgs(bash, true)
	if err != nil {
		t.Fatalf("terminalBashRcArgs: %v", err)
	}
	t.Cleanup(cleanup)
	home := writeTempBashrc(t, "")
	shell := startTerminalTestBash(t, bash, []string{"--rcfile", args[1]}, home)
	defer shell.stop()
	// The prompt mark is ready once the integration hook ran; the prompt text
	// itself differs for root ("# ") and non-root ("$ ").
	shell.waitFor("\x1b]133;B")
	shell.settle()
	shell.write("alias ls; alias grep; alias diff; echo ALIAS-''DONE\r")
	text := shell.waitFor("ALIAS-DONE\r\n")
	if !strings.Contains(text, "ls --color=auto") {
		t.Fatalf("no ls colour alias in %q", text)
	}
	if lsSupportsColour(bash) {
		shell.write("ls --color=auto /; echo COLOR-''DONE\r")
		out := shell.waitFor("COLOR-DONE\r\n")
		start := strings.LastIndex(out, "ls --color=auto /")
		if start < 0 {
			t.Fatalf("command echo not found in %q", out)
		}
		if segment := out[start:]; !strings.Contains(segment, "\x1b[") {
			t.Fatalf("ls --color=auto produced no SGR colour: %q", segment)
		}
	}
}

// lsSupportsColour reports whether the host's ls understands --color.
func lsSupportsColour(bash string) bool {
	return exec.Command(bash, "-c", "ls --color=auto -d / >/dev/null 2>&1").Run() == nil
}

// TestTerminalBashHostSessionIntegration proves the gRPC host path starts bash
// with the integration and reports it in the opened event.
func TestTerminalBashHostSessionIntegration(t *testing.T) {
	bash := terminalTestBash(t)
	home := writeTempBashrc(t, "")
	_, terminal, rootName, _ := terminalTestFixture(t)
	// The fixture's own temp dirs are created above; from here on the agent's
	// per-session integration directory lands in tmp and must be removed.
	tmp := t.TempDir()
	t.Setenv("TMPDIR", tmp)
	t.Setenv("HOME", home)
	terminal.shell = bash
	terminal.integration = true
	client := startTerminalServer(t, terminal)
	stream, err := client.OpenTerminal(context.Background())
	if err != nil {
		t.Fatalf("OpenTerminal: %v", err)
	}
	defer func() { _ = stream.CloseSend() }()
	if err := stream.Send(&agentv1.TerminalRequest{Part: &agentv1.TerminalRequest_Open{Open: &agentv1.TerminalOpen{
		Target: "host", Root: rootName, Cols: 100, Rows: 30,
	}}}); err != nil {
		t.Fatalf("send open: %v", err)
	}
	opened, err := stream.Recv()
	if err != nil {
		t.Fatalf("recv opened: %v", err)
	}
	if got := opened.GetOpened().GetShell(); got != bash {
		t.Fatalf("opened shell = %q; want %q", got, bash)
	}
	if err := stream.Send(&agentv1.TerminalRequest{Part: &agentv1.TerminalRequest_Input{Input: []byte("echo \"shellenv=$TERM|$COLORTERM\"\r")}}); err != nil {
		t.Fatalf("send env: %v", err)
	}
	waitForOutput(t, stream, "shellenv=xterm-256color|truecolor")
	if err := stream.Send(&agentv1.TerminalRequest{Part: &agentv1.TerminalRequest_Input{Input: []byte("true\r")}}); err != nil {
		t.Fatalf("send true: %v", err)
	}
	waitForOutput(t, stream, "\x1b]133;B")

	// The private rcfile is removed when the shell exits.
	if err := stream.Send(&agentv1.TerminalRequest{Part: &agentv1.TerminalRequest_Input{Input: []byte("exit\r")}}); err != nil {
		t.Fatalf("send exit: %v", err)
	}
	recvUntil(t, stream, func(response *agentv1.TerminalResponse) bool {
		return response.GetExited() != nil
	})
	deadline := time.Now().Add(10 * time.Second)
	for {
		entries, readErr := os.ReadDir(tmp)
		if readErr != nil {
			t.Fatalf("read integration temp dir: %v", readErr)
		}
		if len(entries) == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("integration rcfile leaked after the session ended: %v", entries)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// TestTerminalBashIntegrationOffSwitch proves the disabled integration keeps a
// plain bash session (no marks, no rcfile) while still starting bash.
func TestTerminalBashIntegrationOffSwitch(t *testing.T) {
	bash := terminalTestBash(t)
	_, terminal, rootName, _ := terminalTestFixture(t)
	terminal.shell = bash
	terminal.integration = false
	client := startTerminalServer(t, terminal)
	stream, err := client.OpenTerminal(context.Background())
	if err != nil {
		t.Fatalf("OpenTerminal: %v", err)
	}
	defer func() { _ = stream.CloseSend() }()
	if err := stream.Send(&agentv1.TerminalRequest{Part: &agentv1.TerminalRequest_Open{Open: &agentv1.TerminalOpen{
		Target: "host", Root: rootName, Cols: 100, Rows: 30,
	}}}); err != nil {
		t.Fatalf("send open: %v", err)
	}
	if _, err := stream.Recv(); err != nil {
		t.Fatalf("recv opened: %v", err)
	}
	if err := stream.Send(&agentv1.TerminalRequest{Part: &agentv1.TerminalRequest_Input{Input: []byte("gotham-no-marks\r")}}); err != nil {
		t.Fatalf("send input: %v", err)
	}
	if err := stream.Send(&agentv1.TerminalRequest{Part: &agentv1.TerminalRequest_Input{Input: []byte("echo second\r")}}); err != nil {
		t.Fatalf("send second: %v", err)
	}
	output := waitForOutputText(t, stream, "second")
	if strings.Contains(output, "\x1b]133;") {
		t.Fatalf("disabled integration still emitted OSC 133 marks: %q", output)
	}
}

// waitForOutputText accumulates output until marker appears and returns it.
func waitForOutputText(t *testing.T, stream agentv1.TerminalService_OpenTerminalClient, marker string) string {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	var accumulated strings.Builder
	for time.Now().Before(deadline) {
		response, err := stream.Recv()
		if err != nil {
			t.Fatalf("recv: %v; output %q", err, accumulated.String())
		}
		if chunk := response.GetOutput(); len(chunk) > 0 {
			accumulated.Write(chunk)
			if strings.Contains(accumulated.String(), marker) {
				return accumulated.String()
			}
		}
		if response.GetExited() != nil {
			t.Fatalf("shell exited before %q: %q", marker, accumulated.String())
		}
	}
	t.Fatalf("timed out waiting for %q; output %q", marker, accumulated.String())
	return ""
}

// TestTerminalShSessionHasNoIntegration proves a non-bash shell (sh) is
// unchanged: no marks, no forced arguments.
func TestTerminalShSessionHasNoIntegration(t *testing.T) {
	sh := "/bin/sh"
	if info, err := os.Stat(sh); err != nil || !info.Mode().IsRegular() {
		t.Skip("/bin/sh is not installed")
	}
	_, terminal, rootName, _ := terminalTestFixture(t)
	terminal.shell = sh
	terminal.integration = true
	client := startTerminalServer(t, terminal)
	stream, err := client.OpenTerminal(context.Background())
	if err != nil {
		t.Fatalf("OpenTerminal: %v", err)
	}
	defer func() { _ = stream.CloseSend() }()
	if err := stream.Send(&agentv1.TerminalRequest{Part: &agentv1.TerminalRequest_Open{Open: &agentv1.TerminalOpen{
		Target: "host", Root: rootName, Cols: 100, Rows: 30,
	}}}); err != nil {
		t.Fatalf("send open: %v", err)
	}
	opened, err := stream.Recv()
	if err != nil {
		t.Fatalf("recv opened: %v", err)
	}
	if got := opened.GetOpened().GetShell(); got != sh {
		t.Fatalf("opened shell = %q; want %q", got, sh)
	}
	if err := stream.Send(&agentv1.TerminalRequest{Part: &agentv1.TerminalRequest_Input{Input: []byte("gotham-sh-ok\r")}}); err != nil {
		t.Fatalf("send input: %v", err)
	}
	if err := stream.Send(&agentv1.TerminalRequest{Part: &agentv1.TerminalRequest_Input{Input: []byte("echo second\r")}}); err != nil {
		t.Fatalf("send second: %v", err)
	}
	output := waitForOutputText(t, stream, "second")
	if strings.Contains(output, "\x1b]133;") {
		t.Fatalf("sh emitted OSC 133 marks: %q", output)
	}
}

// containerExecInner strips the fixed pid/kill wrapper from a recorded exec
// command and returns the shell command the user gets.
func containerExecInner(t *testing.T, command []string) []string {
	t.Helper()
	if len(command) < 5 || command[0] != "/bin/sh" || command[1] != "-c" || command[2] != docker.ExecWrapperCommand || command[3] != "gotham-terminal" {
		t.Fatalf("exec command %v does not start with the fixed wrapper", command)
	}
	return command[4:]
}

// TestDockerExecBashIntegrationCommand pins the container command and env: a
// probed bash is started through the fixed wrapper with the constant script and
// a terminal description, while no curl/request data is involved.
func TestDockerExecBashIntegrationCommand(t *testing.T) {
	api := newFakeExecAPI(t)
	api.probeOutput = "/bin/bash\n"
	client, err := docker.NewDockerClient(api.server.URL)
	if err != nil {
		t.Fatalf("NewDockerClient: %v", err)
	}
	session, err := client.Exec(context.Background(), "c1", docker.ExecOptions{Cols: 100, Rows: 30, Integration: true})
	if err != nil {
		t.Fatalf("Exec: %v", err)
	}
	defer func() { _ = session.Close() }()
	if session.Shell() != "/bin/bash" {
		t.Fatalf("session shell = %q; want /bin/bash", session.Shell())
	}

	command := containerExecInner(t, api.command(2))
	if len(command) != 3 || command[0] != "/bin/bash" || command[1] != "-c" || command[2] != docker.ContainerBashRcCommand() {
		t.Fatalf("interactive command = %v; want the fixed bash wrapper", command)
	}
	if !strings.Contains(command[2], docker.BashIntegrationScript) {
		t.Fatal("the wrapper does not embed the constant script")
	}

	env := api.env(2)
	check := func(name, want string) {
		t.Helper()
		value, ok := lookupTerminalEnv(env, name)
		if !ok || value != want {
			t.Fatalf("%s = %q (present %v); want %q; env %v", name, value, ok, want, env)
		}
	}
	check("TERM", "xterm-256color")
	check("COLORTERM", "truecolor")
	check("LANG", "C.UTF-8")
	marker, _ := lookupTerminalEnv(env, "GOTHAM_TERMINAL_ID")
	if marker == "" {
		t.Fatal("GOTHAM_TERMINAL_ID marker is missing")
	}
	for _, secret := range []string{"GOTHAM_AGENT_ENROLLMENT_TOKEN", "GOTHAM_AGENT_KEY"} {
		if _, ok := lookupTerminalEnv(env, secret); ok {
			t.Fatalf("agent secret variable %s leaked into the container env", secret)
		}
	}
}

// TestDockerExecBashIntegrationDisabled proves the off switch keeps the plain
// shell command (no rcfile, no extra arguments).
func TestDockerExecBashIntegrationDisabled(t *testing.T) {
	api := newFakeExecAPI(t)
	api.probeOutput = "/bin/bash\n"
	client, err := docker.NewDockerClient(api.server.URL)
	if err != nil {
		t.Fatalf("NewDockerClient: %v", err)
	}
	session, err := client.Exec(context.Background(), "c1", docker.ExecOptions{Cols: 80, Rows: 24})
	if err != nil {
		t.Fatalf("Exec: %v", err)
	}
	defer func() { _ = session.Close() }()
	if command := containerExecInner(t, api.command(2)); len(command) != 1 || command[0] != "/bin/bash" {
		t.Fatalf("interactive command = %v; want the plain bash", command)
	}
	env := api.env(2)
	if value, _ := lookupTerminalEnv(env, "TERM"); value != "xterm-256color" {
		t.Fatalf("TERM = %q; want xterm-256color even with the integration off", value)
	}
	if value, _ := lookupTerminalEnv(env, "COLORTERM"); value != "truecolor" {
		t.Fatalf("COLORTERM = %q; want truecolor even with the integration off", value)
	}
}

// TestDockerExecWithoutBashUnchanged proves an image with no bash (alpine nginx)
// keeps its exact previous interactive command.
func TestDockerExecWithoutBashUnchanged(t *testing.T) {
	api := newFakeExecAPI(t)
	api.probeOutput = "/bin/sh\n"
	client, err := docker.NewDockerClient(api.server.URL)
	if err != nil {
		t.Fatalf("NewDockerClient: %v", err)
	}
	session, err := client.Exec(context.Background(), "c1", docker.ExecOptions{Cols: 80, Rows: 24, Integration: true})
	if err != nil {
		t.Fatalf("Exec: %v", err)
	}
	defer func() { _ = session.Close() }()
	if command := containerExecInner(t, api.command(2)); len(command) != 1 || command[0] != "/bin/sh" {
		t.Fatalf("interactive command = %v; want the plain /bin/sh", command)
	}
	if session.Shell() != "/bin/sh" {
		t.Fatalf("session shell = %q; want /bin/sh", session.Shell())
	}
}

// TestDockerExecKeepsImageLocale proves the container's own locale survives the
// agent's terminal environment.
func TestDockerExecKeepsImageLocale(t *testing.T) {
	api := newFakeExecAPI(t)
	api.probeOutput = "/bin/bash\n"
	api.imageEnv = []string{"LANG=ja_JP.UTF-8", "HOME=/home/app"}
	client, err := docker.NewDockerClient(api.server.URL)
	if err != nil {
		t.Fatalf("NewDockerClient: %v", err)
	}
	session, err := client.Exec(context.Background(), "c1", docker.ExecOptions{Cols: 80, Rows: 24, Integration: true})
	if err != nil {
		t.Fatalf("Exec: %v", err)
	}
	defer func() { _ = session.Close() }()
	if value, _ := lookupTerminalEnv(api.env(2), "LANG"); value != "" {
		t.Fatalf("LANG = %q; the image locale must not be overridden", value)
	}
}
