package hostbootstrap

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/justindeelux/gotham/deploy"
)

// fakeVisudo writes a visudo stub that succeeds (ok) or fails, so the tests
// exercise the validate-before-install path without the real sudoers tool.
func fakeVisudo(t *testing.T, ok bool) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "visudo")
	body := "#!/bin/sh\nexit 0\n"
	if !ok {
		body = "#!/bin/sh\necho 'syntax error' >&2\nexit 1\n"
	}
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestRunInstallsAllArtifacts(t *testing.T) {
	root := t.TempDir()
	if err := Run(Options{Root: root, Visudo: fakeVisudo(t, true)}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	for _, rel := range []string{HostctlPath, hostctlDistPath, ToolctlPath, HostctlSudoersDrop, ToolctlSudoersDrop} {
		fi, err := os.Lstat(filepath.Join(root, rel))
		if err != nil {
			t.Fatalf("%s not installed: %v", rel, err)
		}
		if !fi.Mode().IsRegular() {
			t.Errorf("%s is not a regular file", rel)
		}
	}
	got, err := os.ReadFile(filepath.Join(root, HostctlPath))
	if err != nil || string(got) != string(deploy.HostctlScript) {
		t.Errorf("hostctl content does not match the embedded script")
	}
	tool, err := os.ReadFile(filepath.Join(root, ToolctlPath))
	if err != nil || string(tool) != string(deploy.ToolctlScript) {
		t.Errorf("toolctl content does not match the embedded script")
	}

	hostSudoers, _ := os.ReadFile(filepath.Join(root, HostctlSudoersDrop))
	wantHost := []string{
		"gotham ALL=(root) NOPASSWD: " + HostctlPath + " status",
		"gotham ALL=(root) NOPASSWD: " + HostctlPath + " apply-network",
		"gotham ALL=(root) NOPASSWD: " + ToolctlPath + " install-tool gotham-hostctl",
	}
	for _, want := range wantHost {
		if !strings.Contains(string(hostSudoers), want) {
			t.Errorf("hostctl sudoers missing %q", want)
		}
	}
	agentSudoers, _ := os.ReadFile(filepath.Join(root, ToolctlSudoersDrop))
	for _, want := range []string{
		"gotham-agent ALL=(root) NOPASSWD: " + ToolctlPath + " install-tool railpack",
		"gotham-agent ALL=(root) NOPASSWD: " + ToolctlPath + " uninstall-tool git",
	} {
		if !strings.Contains(string(agentSudoers), want) {
			t.Errorf("toolctl sudoers missing %q", want)
		}
	}
	if fi, _ := os.Stat(filepath.Join(root, HostctlSudoersDrop)); fi.Mode().Perm() != 0o440 {
		t.Errorf("sudoers drop-in mode = %o, want 0440", fi.Mode().Perm())
	}
	if fi, _ := os.Stat(filepath.Join(root, HostctlPath)); fi.Mode().Perm() != 0o755 {
		t.Errorf("helper mode = %o, want 0755", fi.Mode().Perm())
	}
}

func TestRunHonorsCustomUsers(t *testing.T) {
	root := t.TempDir()
	if err := Run(Options{Root: root, Visudo: fakeVisudo(t, true), CpUser: "ops", AgentUser: "node"}); err != nil {
		t.Fatal(err)
	}
	body, _ := os.ReadFile(filepath.Join(root, HostctlSudoersDrop))
	if !strings.Contains(string(body), "ops ALL=(root)") || strings.Contains(string(body), "gotham ALL=") {
		t.Errorf("custom cp user not honored:\n%s", body)
	}
}

func TestRunIsIdempotent(t *testing.T) {
	root := t.TempDir()
	opts := Options{Root: root, Visudo: fakeVisudo(t, true)}
	if err := Run(opts); err != nil {
		t.Fatal(err)
	}
	first, err := os.Stat(filepath.Join(root, HostctlPath))
	if err != nil {
		t.Fatal(err)
	}
	if err := Run(opts); err != nil {
		t.Fatalf("second run: %v", err)
	}
	second, err := os.Stat(filepath.Join(root, HostctlPath))
	if err != nil {
		t.Fatal(err)
	}
	if !first.ModTime().Equal(second.ModTime()) {
		t.Error("a second run rewrote an unchanged artifact")
	}
}

func TestRunRefusesSymlinkedTarget(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, helperDir), 0o755); err != nil {
		t.Fatal(err)
	}
	elsewhere := filepath.Join(root, "elsewhere")
	if err := os.WriteFile(elsewhere, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(elsewhere, filepath.Join(root, HostctlPath)); err != nil {
		t.Fatal(err)
	}
	err := Run(Options{Root: root, Visudo: fakeVisudo(t, true)})
	if err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("symlinked target not refused: %v", err)
	}
}

func TestRunAbortsOnBadSudoers(t *testing.T) {
	root := t.TempDir()
	err := Run(Options{Root: root, Visudo: fakeVisudo(t, false)})
	if err == nil || !strings.Contains(err.Error(), "rejected") {
		t.Fatalf("bad sudoers not rejected: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, HostctlSudoersDrop)); err == nil {
		t.Error("a drop-in was left behind after failed validation")
	}
}

func TestRunRejectsInvalidUser(t *testing.T) {
	if err := Run(Options{Root: t.TempDir(), CpUser: "bad user", Visudo: fakeVisudo(t, true)}); err == nil {
		t.Fatal("invalid control-plane user accepted")
	}
}

func TestRunRequiresVisudo(t *testing.T) {
	t.Setenv("PATH", t.TempDir()) // no visudo anywhere
	if err := Run(Options{Root: t.TempDir()}); err == nil {
		t.Fatal("Run accepted a host without visudo")
	}
}
