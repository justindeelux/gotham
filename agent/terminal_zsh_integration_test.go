package agent

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
)

// This file is the JUS-252 proof for zsh: the same optional prompt-mark
// integration as bash, added through a private ZDOTDIR so the operator's
// .zshenv/.zshrc still run first and nothing is written to the PTY.

// terminalTestZsh returns an installed zsh or skips the test.
func terminalTestZsh(t *testing.T) string {
	t.Helper()
	for _, candidate := range []string{"/bin/zsh", "/usr/bin/zsh"} {
		if info, err := os.Stat(candidate); err == nil && info.Mode().IsRegular() && info.Mode().Perm()&0o111 != 0 {
			return candidate
		}
	}
	if path, err := exec.LookPath("zsh"); err == nil {
		return path
	}
	t.Skip("zsh is not installed")
	return ""
}

// TestTerminalShellSetupSelection pins which shells get an integration and
// which keep the plain command line.
func TestTerminalShellSetupSelection(t *testing.T) {
	t.Setenv("ZDOTDIR", "")
	for _, shell := range []string{"/bin/sh", "/usr/bin/dash", "/bin/ash", "/usr/bin/fish"} {
		setup, err := terminalShellSetupFor(shell, true)
		if err != nil || len(setup.args) != 0 || len(setup.env) != 0 || setup.cleanup != nil {
			t.Fatalf("%s: setup = %+v, err = %v; want no integration", shell, setup, err)
		}
	}
	for _, shell := range []string{"/bin/bash", "/bin/zsh"} {
		setup, err := terminalShellSetupFor(shell, false)
		if err != nil || len(setup.args) != 0 || len(setup.env) != 0 || setup.cleanup != nil {
			t.Fatalf("%s with the integration off: setup = %+v, err = %v; want no integration", shell, setup, err)
		}
	}
}

// TestTerminalZshSetupIsPrivateAndConstant proves the generated startup files
// live in a fresh 0700 directory, are 0600, carry only the agent constants, and
// pass the operator's ZDOTDIR as a quoted environment value.
func TestTerminalZshSetupIsPrivateAndConstant(t *testing.T) {
	t.Setenv("ZDOTDIR", "/home/operator/.config/zsh")
	setup, err := terminalZshSetup()
	if err != nil {
		t.Fatalf("terminalZshSetup: %v", err)
	}
	if len(setup.args) != 0 {
		t.Fatalf("zsh args = %v; zsh has no --rcfile and must use ZDOTDIR", setup.args)
	}
	t.Cleanup(setup.cleanup)

	dir, _ := lookupTerminalEnv(setup.env, "ZDOTDIR")
	if dir == "" {
		t.Fatalf("no ZDOTDIR in %v", setup.env)
	}
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("stat integration directory: %v", err)
	}
	if info.Mode().Perm() != 0o700 {
		t.Fatalf("integration directory mode = %o; want 700", info.Mode().Perm())
	}
	for name, want := range map[string]string{
		".zshenv": terminalZshEnvScript,
		".zshrc":  terminalZshRcScript,
	} {
		path := filepath.Join(dir, name)
		fileInfo, err := os.Stat(path)
		if err != nil {
			t.Fatalf("stat %s: %v", name, err)
		}
		if fileInfo.Mode().Perm() != 0o600 {
			t.Fatalf("%s mode = %o; want 600", name, fileInfo.Mode().Perm())
		}
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		if string(content) != want {
			t.Fatalf("%s differs from the agent constant", name)
		}
		for _, requestShaped := range []string{"gotham-request-cwd", "gotham-service", "/tmp/request-root"} {
			if strings.Contains(string(content), requestShaped) {
				t.Fatalf("%s contains request-shaped data %q", name, requestShaped)
			}
		}
	}
	if got, _ := lookupTerminalEnv(setup.env, terminalZshOriginalZdotdirVar); got != "/home/operator/.config/zsh" {
		t.Fatalf("original ZDOTDIR = %q; want the operator's absolute path", got)
	}
	if got, _ := lookupTerminalEnv(setup.env, terminalZshZdotdirVar); got != dir {
		t.Fatalf("integration ZDOTDIR = %q; want %q", got, dir)
	}

	// A second session gets its own directory with byte-identical files.
	second, err := terminalZshSetup()
	if err != nil {
		t.Fatalf("terminalZshSetup (second): %v", err)
	}
	secondDir, _ := lookupTerminalEnv(second.env, "ZDOTDIR")
	if secondDir == dir {
		t.Fatal("two sessions share a ZDOTDIR; want a per-session directory")
	}
	secondRc, err := os.ReadFile(filepath.Join(secondDir, ".zshrc"))
	if err != nil {
		t.Fatalf("read second .zshrc: %v", err)
	}
	if string(secondRc) != terminalZshRcScript {
		t.Fatal("the generated .zshrc depends on session data; it must be a constant")
	}
	second.cleanup()

	setup.cleanup()
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("integration directory still exists after cleanup: %v", err)
	}
}

// TestTerminalZshSetupIgnoresRelativeZdotdir proves a relative operator
// ZDOTDIR cannot make the integration read startup files from the shell's
// operator-chosen cwd; the generated .zshenv falls back to HOME instead.
func TestTerminalZshSetupIgnoresRelativeZdotdir(t *testing.T) {
	t.Setenv("ZDOTDIR", "relative/zsh")
	setup, err := terminalZshSetup()
	if err != nil {
		t.Fatalf("terminalZshSetup: %v", err)
	}
	t.Cleanup(setup.cleanup)
	if got, _ := lookupTerminalEnv(setup.env, terminalZshOriginalZdotdirVar); got != "" {
		t.Fatalf("original ZDOTDIR = %q; a relative path must be ignored", got)
	}
}

// TestTerminalZshIntegrationScript runs the generated files on a real zsh PTY:
// the operator's files are sourced first, the prompt is preserved, marks are
// ordered and carry the real exit status, and re-sourcing is a no-op.
func TestTerminalZshIntegrationScript(t *testing.T) {
	zsh := terminalTestZsh(t)
	t.Setenv("ZDOTDIR", "")
	setup, err := terminalZshSetup()
	if err != nil {
		t.Fatalf("terminalZshSetup: %v", err)
	}
	t.Cleanup(setup.cleanup)
	dir, _ := lookupTerminalEnv(setup.env, "ZDOTDIR")

	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, ".zshenv"), []byte(strings.Join([]string{
		`if [ -z "${ZDOTDIR-}" ]; then export GOTHAM_TEST_ZSHENV_UNSET=1; fi`,
		`export GOTHAM_TEST_ZSHENV=1`,
	}, "\n")), 0o600); err != nil {
		t.Fatalf("write .zshenv: %v", err)
	}
	if err := os.WriteFile(filepath.Join(home, ".zshrc"), []byte(strings.Join([]string{
		`export GOTHAM_TEST_ZSHRC=1`,
		`if [ -z "${ZDOTDIR-}" ]; then export GOTHAM_TEST_ZSHRC_ZDOTDIR_UNSET=1; fi`,
		`printf 'sourced\n' >> "$HOME/rc-sources"`,
		`PROMPT='GOTHAMCUSTOM> '`,
		`alias gotham-user-alias='echo GOTHAM_USER_ALIAS'`,
		`__gotham_test_theme_precmd() { :; }`,
		`autoload -Uz add-zsh-hook`,
		`add-zsh-hook precmd __gotham_test_theme_precmd`,
	}, "\n")), 0o600); err != nil {
		t.Fatalf("write .zshrc: %v", err)
	}

	env := terminalEnv()
	env = terminalEnvSet(env, "HOME", home)
	env = append(env, setup.env...)
	shell := startTerminalTestShell(t, zsh, setup.args, env)
	defer shell.stop()
	shell.waitFor("GOTHAMCUSTOM> ")
	shell.settle()

	// Both operator startup files ran before the integration, their prompt and
	// alias win, and they saw ZDOTDIR exactly as a plain zsh would (unset here).
	// After startup the private registry is no longer exported: the session has
	// the operator's value (unset) and no Gotham variables.
	shell.write("echo \"userrc=$GOTHAM_TEST_ZSHRC shenv=$GOTHAM_TEST_ZSHENV unset=$GOTHAM_TEST_ZSHENV_UNSET rcunset=$GOTHAM_TEST_ZSHRC_ZDOTDIR_UNSET\"\r")
	shell.waitFor("userrc=1 shenv=1 unset=1 rcunset=1")
	shell.write("echo \"zdotdir=${ZDOTDIR-<unset>} userzdot=${GOTHAM_TERMINAL_USER_ZDOTDIR-<unset>}\"\r")
	shell.waitFor("zdotdir=<unset> userzdot=<unset>")
	shell.write("gotham-user-alias\r")
	shell.waitFor("GOTHAM_USER_ALIAS")
	if strings.Contains(shell.all(), "read-only") {
		t.Fatal("the integration used zsh's read-only status parameter")
	}

	// A succeeding then a failing command: D carries the real status even though
	// the operator's own precmd hook runs first.
	shell.write("true\r")
	shell.write("false\r")
	shell.write("echo after-false\r")
	shell.waitFor("after-false\r\n")

	// Sourcing the generated .zshrc again is a no-op: no second operator rc, no
	// duplicated hooks. The private path is known to the agent, not to the live
	// session (which now has the operator's ZDOTDIR).
	shell.write(". '" + filepath.Join(dir, ".zshrc") + "'\r")
	shell.write(". '" + filepath.Join(dir, ".zshrc") + "'\r")
	shell.write("echo idempotent-ok\r")
	shell.waitFor("idempotent-ok\r\n")
	sources, err := os.ReadFile(filepath.Join(home, "rc-sources"))
	if err != nil {
		t.Fatalf("read rc-sources: %v", err)
	}
	if got := strings.Count(string(sources), "sourced"); got != 1 {
		t.Fatalf("the operator .zshrc ran %d times; re-sourcing must be a no-op (%q)", got, sources)
	}

	output := shell.all()
	marks := scanPromptMarks(output)
	if len(marks) == 0 {
		t.Fatalf("no OSC 133 marks in output %q", output)
	}
	kinds := markKinds(marks)
	t.Logf("zsh OSC 133 marks: %q", kinds)
	if !strings.Contains(kinds, "DAB") {
		t.Fatalf("marks %q; want ordered D A B", kinds)
	}
	// zsh emits B twice on a fresh prompt: once at the end of PROMPT and once
	// from the zle line-init widget that recovers after Ctrl-C.
	if !strings.Contains(kinds, "BBC") {
		t.Fatalf("marks %q; zsh must emit C after B", kinds)
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
	for _, pair := range []string{"BA", "CB", "DC"} {
		if strings.Contains(kinds, pair) {
			t.Fatalf("marks %q are out of order (found %q)", kinds, pair)
		}
	}
	if count := strings.Count(kinds, "A"); count > 12 {
		t.Fatalf("marks %q; %d A marks suggests duplicated hooks", kinds, count)
	}
}

// TestTerminalZshSetupDoesNotInterpolateOperatorPaths proves the operator's
// ZDOTDIR is only an environment value: characters that would be shell syntax
// never reach the generated scripts.
func TestTerminalZshSetupDoesNotInterpolateOperatorPaths(t *testing.T) {
	odd := "/tmp/gotham odd/$(touch pwn)/; echo pwned"
	t.Setenv("ZDOTDIR", odd)
	setup, err := terminalZshSetup()
	if err != nil {
		t.Fatalf("terminalZshSetup: %v", err)
	}
	t.Cleanup(setup.cleanup)
	if got, _ := lookupTerminalEnv(setup.env, terminalZshOriginalZdotdirVar); got != odd {
		t.Fatalf("original ZDOTDIR = %q; want the exact operator value", got)
	}
	dir, _ := lookupTerminalEnv(setup.env, "ZDOTDIR")
	for _, name := range []string{".zshenv", ".zshrc"} {
		content, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		if strings.Contains(string(content), "touch pwn") || strings.Contains(string(content), "pwned") {
			t.Fatalf("%s interpolated the operator ZDOTDIR into shell code: %q", name, content)
		}
	}
}

// TestTerminalZshUnusualHomeStillIntegrates proves a HOME and temp directory
// with spaces, a dollar sign and no operator rc files neither break the
// integration nor get executed as shell code.
func TestTerminalZshUnusualHomeStillIntegrates(t *testing.T) {
	zsh := terminalTestZsh(t)
	tmpRoot := filepath.Join(t.TempDir(), "tmp dir $x")
	if err := os.MkdirAll(tmpRoot, 0o700); err != nil {
		t.Fatalf("create TMPDIR: %v", err)
	}
	t.Setenv("TMPDIR", tmpRoot)
	t.Setenv("ZDOTDIR", "")
	setup, err := terminalZshSetup()
	if err != nil {
		t.Fatalf("terminalZshSetup: %v", err)
	}
	t.Cleanup(setup.cleanup)
	home := filepath.Join(t.TempDir(), "home dir $x")
	if err := os.MkdirAll(home, 0o700); err != nil {
		t.Fatalf("create HOME: %v", err)
	}
	env := terminalEnv()
	env = terminalEnvSet(env, "HOME", home)
	env = append(env, setup.env...)
	shell := startTerminalTestShell(t, zsh, setup.args, env)
	defer shell.stop()
	shell.waitFor("\x1b]133;B")
	shell.settle()
	shell.write("echo \"odd-home=$HOME\"\r")
	shell.waitFor("odd-home=" + home)
	shell.write("echo ODD-\"\"HOME-OK\r")
	shell.waitFor("ODD-HOME-OK\r\n")
	if !strings.Contains(markKinds(scanPromptMarks(shell.all())), "AB") {
		t.Fatalf("no prompt marks with an unusual HOME: %q", shell.all())
	}
}

// TestTerminalZshRestoresUserZdotdirForChildren proves the private ZDOTDIR is
// only used for startup. In the live session `$ZDOTDIR` is again the operator's
// value (or unset, which falls back to HOME), a nested `zsh -i` reads the
// operator's own rc files, and no Gotham variable leaks into children.
func TestTerminalZshRestoresUserZdotdirForChildren(t *testing.T) {
	zsh := terminalTestZsh(t)

	t.Run("unset ZDOTDIR", func(t *testing.T) {
		t.Setenv("ZDOTDIR", "")
		setup, err := terminalZshSetup()
		if err != nil {
			t.Fatalf("terminalZshSetup: %v", err)
		}
		t.Cleanup(setup.cleanup)
		home := t.TempDir()
		if err := os.WriteFile(filepath.Join(home, ".zshrc"), []byte(strings.Join([]string{
			`printf 'sourced\n' >> "$HOME/rc-sources"`,
			`export GOTHAM_HOME_RC=1`,
		}, "\n")), 0o600); err != nil {
			t.Fatalf("write .zshrc: %v", err)
		}
		env := terminalEnv()
		env = terminalEnvSet(env, "HOME", home)
		env = append(env, setup.env...)
		shell := startTerminalTestShell(t, zsh, setup.args, env)
		defer shell.stop()
		shell.waitFor("\x1b]133;B")
		shell.settle()

		shell.write("echo \"outerzd=${ZDOTDIR-<unset>}\"\r")
		shell.waitFor("outerzd=<unset>")
		shell.write("zsh -i -c 'print -r -- \"nestedzd=${ZDOTDIR-<unset>} nestedhome=${GOTHAM_HOME_RC-<unset>}\"'\r")
		shell.waitFor("nestedzd=<unset> nestedhome=1")
		sources, err := os.ReadFile(filepath.Join(home, "rc-sources"))
		if err != nil {
			t.Fatalf("read rc-sources: %v", err)
		}
		if got := strings.Count(string(sources), "sourced"); got != 2 {
			t.Fatalf("HOME .zshrc ran %d times; want the outer and the nested shell (%q)", got, sources)
		}
	})

	t.Run("custom ZDOTDIR with spaces", func(t *testing.T) {
		custom := filepath.Join(t.TempDir(), "zdot dir $x")
		if err := os.MkdirAll(custom, 0o700); err != nil {
			t.Fatalf("create custom ZDOTDIR: %v", err)
		}
		if err := os.WriteFile(filepath.Join(custom, ".zshenv"), []byte("export GOTHAM_CUSTOM_ZSHENV=1\n"), 0o600); err != nil {
			t.Fatalf("write custom .zshenv: %v", err)
		}
		if err := os.WriteFile(filepath.Join(custom, ".zshrc"), []byte(strings.Join([]string{
			`printf 'sourced\n' >> "$HOME/rc-sources"`,
			`export GOTHAM_CUSTOM_ZSHRC=1`,
			`PROMPT='CUSTOMZD> '`,
		}, "\n")), 0o600); err != nil {
			t.Fatalf("write custom .zshrc: %v", err)
		}
		home := t.TempDir()
		if err := os.WriteFile(filepath.Join(home, ".zshrc"), []byte(strings.Join([]string{
			`printf 'home-rc\n' >> "$HOME/home-rc-sources"`,
			`export GOTHAM_HOME_RC=1`,
		}, "\n")), 0o600); err != nil {
			t.Fatalf("write HOME .zshrc: %v", err)
		}

		t.Setenv("ZDOTDIR", custom)
		setup, err := terminalZshSetup()
		if err != nil {
			t.Fatalf("terminalZshSetup: %v", err)
		}
		t.Cleanup(setup.cleanup)
		env := terminalEnv()
		env = terminalEnvSet(env, "HOME", home)
		env = append(env, setup.env...)
		shell := startTerminalTestShell(t, zsh, setup.args, env)
		defer shell.stop()
		shell.waitFor("CUSTOMZD> ")
		shell.settle()

		shell.write("echo \"outerzd=$ZDOTDIR custom=$GOTHAM_CUSTOM_ZSHRC shenv=$GOTHAM_CUSTOM_ZSHENV\"\r")
		shell.waitFor("outerzd=" + custom + " custom=1 shenv=1")
		shell.write("zsh -i -c 'print -r -- \"nestedzd=${ZDOTDIR-<unset>} nestedcustom=${GOTHAM_CUSTOM_ZSHRC-<unset>} nestedhome=${GOTHAM_HOME_RC-<unset>}\"'\r")
		shell.waitFor("nestedzd=" + custom + " nestedcustom=1 nestedhome=<unset>")
		sources, err := os.ReadFile(filepath.Join(home, "rc-sources"))
		if err != nil {
			t.Fatalf("read rc-sources: %v", err)
		}
		if got := strings.Count(string(sources), "sourced"); got != 2 {
			t.Fatalf("custom .zshrc ran %d times; want the outer and the nested shell (%q)", got, sources)
		}
		if _, err := os.Stat(filepath.Join(home, "home-rc-sources")); !os.IsNotExist(err) {
			t.Fatalf("HOME .zshrc ran despite the custom ZDOTDIR: %v", err)
		}
	})
}

// TestTerminalZshHostSessionIntegration proves the gRPC host path starts zsh
// with the private ZDOTDIR integration, reports the configured shell, and
// removes the directory when the session ends.
func TestTerminalZshHostSessionIntegration(t *testing.T) {
	zsh := terminalTestZsh(t)
	t.Setenv("ZDOTDIR", "")
	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, ".zshrc"), []byte("PROMPT='GOTHAMZSH> '\n"), 0o600); err != nil {
		t.Fatalf("write .zshrc: %v", err)
	}
	_, terminal, rootName, _ := terminalTestFixture(t)
	tmp := t.TempDir()
	t.Setenv("TMPDIR", tmp)
	t.Setenv("HOME", home)
	terminal.shell = zsh
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
	if got := opened.GetOpened().GetShell(); got != zsh {
		t.Fatalf("opened shell = %q; want %q", got, zsh)
	}
	if err := stream.Send(&agentv1.TerminalRequest{Part: &agentv1.TerminalRequest_Input{Input: []byte("echo \"shellenv=$TERM|$COLORTERM\"\r")}}); err != nil {
		t.Fatalf("send env: %v", err)
	}
	waitForOutput(t, stream, "shellenv=xterm-256color|truecolor")
	if err := stream.Send(&agentv1.TerminalRequest{Part: &agentv1.TerminalRequest_Input{Input: []byte("true\r")}}); err != nil {
		t.Fatalf("send true: %v", err)
	}
	waitForOutput(t, stream, "\x1b]133;B")

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
			t.Fatalf("integration directory leaked after the session ended: %v", entries)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// TestTerminalZshFailingUserRcKeepsSession proves a broken operator .zshrc does
// not stop the integration or the shell: zsh reports the parse error, the marks
// are still installed and commands run.
func TestTerminalZshFailingUserRcKeepsSession(t *testing.T) {
	zsh := terminalTestZsh(t)
	t.Setenv("ZDOTDIR", "")
	setup, err := terminalZshSetup()
	if err != nil {
		t.Fatalf("terminalZshSetup: %v", err)
	}
	t.Cleanup(setup.cleanup)
	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, ".zshrc"), []byte("if [\n"), 0o600); err != nil {
		t.Fatalf("write .zshrc: %v", err)
	}
	env := terminalEnv()
	env = terminalEnvSet(env, "HOME", home)
	env = append(env, setup.env...)
	shell := startTerminalTestShell(t, zsh, setup.args, env)
	defer shell.stop()
	shell.waitFor("\x1b]133;B")
	shell.settle()

	shell.write("echo RC-\"\"FAIL-OK\r")
	shell.waitFor("RC-FAIL-OK\r\n")
	if kinds := markKinds(scanPromptMarks(shell.all())); !strings.Contains(kinds, "AB") {
		t.Fatalf("marks %q; a failing operator rc must not disable the integration", kinds)
	}
}

// TestTerminalZshDisabledIntegrationIsUnchanged proves the off switch restores
// the pre-JUS-252 behaviour: the plain zsh command line, no ZDOTDIR override
// and no prompt marks.
func TestTerminalZshDisabledIntegrationIsUnchanged(t *testing.T) {
	zsh := terminalTestZsh(t)
	setup, err := terminalShellSetupFor(zsh, false)
	if err != nil {
		t.Fatalf("terminalShellSetupFor: %v", err)
	}
	if len(setup.args) != 0 || len(setup.env) != 0 || setup.cleanup != nil {
		t.Fatalf("disabled setup = %+v; want the plain shell", setup)
	}
	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, ".zshrc"), []byte("PROMPT='PLAINZSH> '\n"), 0o600); err != nil {
		t.Fatalf("write .zshrc: %v", err)
	}
	env := terminalEnv()
	env = terminalEnvSet(env, "HOME", home)
	shell := startTerminalTestShell(t, zsh, setup.args, env)
	defer shell.stop()
	shell.waitFor("PLAINZSH> ")
	shell.settle()
	shell.write("echo \"zd=${ZDOTDIR-<unset>}\"\r")
	shell.waitFor("zd=<unset>")
	shell.write("echo DISABLED-\"\"OK\r")
	shell.waitFor("DISABLED-OK\r\n")
	if strings.Contains(shell.all(), "\x1b]133;") {
		t.Fatalf("disabled integration still emitted OSC 133 marks: %q", shell.all())
	}
}

// TestTerminalZshStartFailureRemovesIntegrationDir proves the private directory
// is removed when the PTY cannot be started (the shell binary is missing).
func TestTerminalZshStartFailureRemovesIntegrationDir(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("TMPDIR", tmp)
	t.Setenv("ZDOTDIR", "")
	terminal := NewTerminalServer(TerminalConfig{Logger: discardLogger()})
	spec := terminalStart{
		Target: terminalTargetHost,
		Shell:  filepath.Join(tmp, "missing", "zsh"), // basename zsh, parent absent
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
		t.Fatalf("integration directory leaked after a failed start: %v", entries)
	}
}
