package deploy

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func firewallStubs(t *testing.T, root string) {
	t.Helper()
	bin := filepath.Join(root, "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	ufw := `#!/bin/sh
case "$*" in
  "status") echo "Status: active" ;;
  "status numbered")
    echo "Status: active"
    echo
    echo "     To                         Action      From"
    echo "     --                         ------      ----"
    echo "[ 1] 22/tcp                     ALLOW IN    Anywhere"
    ;;
  "show added")
    echo "Added user rules (see 'ufw status' for running firewall):"
    echo "ufw allow 22/tcp comment 'SSH'"
    ;;
esac
exit 0
`
	firewallCmd := `#!/bin/sh
case "$*" in
  "--state") echo running ;;
  "--permanent --list-rich-rules") echo 'rule family="ipv4" port port="22" protocol="tcp" accept' ;;
esac
exit 0
`
	systemctl := "#!/bin/sh\nexit 0\n"
	sshd := "#!/bin/sh\n" +
		"if [ \"$1\" = \"-t\" ]; then exit 0; fi\n" +
		"if [ \"$1\" = \"-T\" ]; then echo 'port 22'; echo 'passwordauthentication yes'; exit 0; fi\n"
	for name, body := range map[string]string{
		"ufw": ufw, "firewall-cmd": firewallCmd, "systemctl": systemctl, "sshd": sshd,
	} {
		writeStub(t, bin, name, body)
	}
	t.Setenv("PATH", bin+":"+os.Getenv("PATH"))
}

func TestHostctlFirewallStatus(t *testing.T) {
	root := t.TempDir()
	firewallStubs(t, root)
	out, err := runHostctlEnv(t, root, "", "firewall-status", nil)
	if err != nil {
		t.Fatalf("firewall-status: %v\n%s", err, out)
	}
	for _, want := range []string{"backend=ufw", "enabled=true", "manageable=true", "ssh_ports=22"} {
		if !strings.Contains(out, want) {
			t.Errorf("output %q does not contain %q", out, want)
		}
	}
}

func TestHostctlFirewallList(t *testing.T) {
	root := t.TempDir()
	firewallStubs(t, root)
	out, err := runHostctlEnv(t, root, "", "firewall-list", nil)
	if err != nil {
		t.Fatalf("firewall-list: %v\n%s", err, out)
	}
	for _, want := range []string{"backend=ufw", "--", "22/tcp", "ufw allow 22/tcp"} {
		if !strings.Contains(out, want) {
			t.Errorf("output %q does not contain %q", out, want)
		}
	}
}

func TestHostctlFirewallAddAndDelete(t *testing.T) {
	root := t.TempDir()
	firewallStubs(t, root)
	input := "action=allow\ndirection=in\nprotocol=tcp\nport=22\nsource=\ncomment=SSH\n"
	out, err := runHostctlEnv(t, root, input, "firewall-add", nil)
	if err != nil {
		t.Fatalf("firewall-add: %v\n%s", err, out)
	}
	if !strings.Contains(out, "+ ufw allow in proto tcp to any port 22 comment SSH") {
		t.Fatalf("add command = %q", out)
	}

	deny := "action=deny\ndirection=in\nprotocol=tcp\nport=3306\nsource=\ncomment=\n"
	out, err = runHostctlEnv(t, root, deny, "firewall-add", nil)
	if err != nil {
		t.Fatalf("firewall-add deny: %v\n%s", err, out)
	}
	if !strings.Contains(out, "+ ufw insert 1 deny in proto tcp to any port 3306") {
		t.Fatalf("deny must be inserted before allows: %q", out)
	}

	out, err = runHostctlEnv(t, root, input, "firewall-delete", nil)
	if err != nil {
		t.Fatalf("firewall-delete: %v\n%s", err, out)
	}
	if !strings.Contains(out, "+ ufw --force delete allow in proto tcp to any port 22") {
		t.Fatalf("delete command = %q", out)
	}
}

func TestHostctlFirewallRejectsUnsafeValues(t *testing.T) {
	root := t.TempDir()
	firewallStubs(t, root)
	for name, input := range map[string]string{
		"port":     "action=allow\ndirection=in\nprotocol=tcp\nport=70000\nsource=\ncomment=\n",
		"source":   "action=allow\ndirection=in\nprotocol=tcp\nport=22\nsource=example.com\ncomment=\n",
		"comment":  "action=allow\ndirection=in\nprotocol=tcp\nport=22\nsource=\ncomment=bad;comment\n",
		"dash":     "action=allow\ndirection=in\nprotocol=tcp\nport=22\nsource=\ncomment=-ssh\n",
		"outbound": "action=allow\ndirection=out\nprotocol=tcp\nport=53\nsource=1.2.3.4\ncomment=\n",
		"action":   "action=block\ndirection=in\nprotocol=tcp\nport=22\nsource=\ncomment=\n",
	} {
		out, err := runHostctlEnv(t, root, input, "firewall-add", nil)
		if err == nil {
			t.Errorf("%s: expected an error, got %q", name, out)
		}
	}
}

func TestHostctlFirewallNoBackend(t *testing.T) {
	root := t.TempDir()
	out, err := runHostctlOnly(t, root, "", "firewall-status", t.TempDir())
	if err != nil {
		t.Fatalf("firewall-status: %v\n%s", err, out)
	}
	if !strings.Contains(out, "backend=none") || !strings.Contains(out, "manageable=false") {
		t.Fatalf("output = %q", out)
	}
}

func runHostctlIsolated(t *testing.T, bin, root, stdin, verb string, extra []string) (string, error) {
	t.Helper()
	cmd := exec.Command("sh", "gotham-hostctl.sh", verb)
	cmd.Env = append([]string{"PATH=" + bin, "GOTHAM_HOSTCTL_ROOT=" + root}, extra...)
	cmd.Stdin = strings.NewReader(stdin)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func symlinkTools(t *testing.T, bin string, names ...string) {
	t.Helper()
	for _, tool := range names {
		path, err := exec.LookPath(tool)
		if err != nil {
			t.Fatalf("look up %s: %v", tool, err)
		}
		if err := os.Symlink(path, filepath.Join(bin, tool)); err != nil {
			t.Fatal(err)
		}
	}
}

func osRelease(t *testing.T, root, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(root, "etc"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "etc/os-release"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestHostctlFirewallInstallOnDebianInstallsUfw(t *testing.T) {
	root := t.TempDir()
	osRelease(t, root, "ID=ubuntu\nID_LIKE=debian\n")
	bin := filepath.Join(root, "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	log := filepath.Join(root, "install.log")
	writeStub(t, bin, "apt-get", `#!/bin/sh
printf '%s\n' "$*" >>"$INSTALL_LOG"
printf '#!/bin/sh\nexit 0\n' >"$BIN/ufw"
chmod +x "$BIN/ufw"
`)
	symlinkTools(t, bin, "sed", "tr", "grep", "chmod")

	out, err := runHostctlIsolated(t, bin, root, "", "firewall-install",
		[]string{"INSTALL_LOG=" + log, "BIN=" + bin})
	if err != nil {
		t.Fatalf("firewall-install: %v\n%s", err, out)
	}
	data, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "install -y --no-install-recommends ufw") {
		t.Fatalf("apt-get calls = %q", data)
	}
}

func TestHostctlFirewallInstallOnRHELInstallsAndEnablesFirewalld(t *testing.T) {
	root := t.TempDir()
	osRelease(t, root, "ID=fedora\n")
	bin := filepath.Join(root, "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	log := filepath.Join(root, "install.log")
	writeStub(t, bin, "dnf", `#!/bin/sh
printf 'dnf %s\n' "$*" >>"$INSTALL_LOG"
printf '#!/bin/sh\nexit 0\n' >"$BIN/firewall-cmd"
chmod +x "$BIN/firewall-cmd"
`)
	writeStub(t, bin, "systemctl", `#!/bin/sh
printf 'systemctl %s\n' "$*" >>"$INSTALL_LOG"
exit 0
`)
	symlinkTools(t, bin, "sed", "tr", "grep", "chmod")

	out, err := runHostctlIsolated(t, bin, root, "", "firewall-install",
		[]string{"INSTALL_LOG=" + log, "BIN=" + bin})
	if err != nil {
		t.Fatalf("firewall-install: %v\n%s", err, out)
	}
	data, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"dnf install -y firewalld", "systemctl enable firewalld"} {
		if !strings.Contains(string(data), want) {
			t.Errorf("install log %q does not contain %q", data, want)
		}
	}
}

func TestHostctlFirewallInstallLeavesAnExistingBackendUntouched(t *testing.T) {
	root := t.TempDir()
	osRelease(t, root, "ID=debian\n")
	bin := filepath.Join(root, "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	log := filepath.Join(root, "install.log")
	writeStub(t, bin, "ufw", "#!/bin/sh\nexit 0\n")
	writeStub(t, bin, "apt-get", `#!/bin/sh
printf '%s\n' "$*" >>"$INSTALL_LOG"
`)
	symlinkTools(t, bin, "grep")

	out, err := runHostctlIsolated(t, bin, root, "", "firewall-install",
		[]string{"INSTALL_LOG=" + log})
	if err != nil {
		t.Fatalf("firewall-install: %v\n%s", err, out)
	}
	if data, err := os.ReadFile(log); err == nil && len(data) > 0 {
		t.Fatalf("an existing backend must not install a package: %q", data)
	}
}

func TestHostctlFirewallInstallRefusesAnUnknownDistribution(t *testing.T) {
	root := t.TempDir()
	osRelease(t, root, "ID=arch\n")
	bin := filepath.Join(root, "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	writeStub(t, bin, "apt-get", "#!/bin/sh\nexit 0\n")
	writeStub(t, bin, "dnf", "#!/bin/sh\nexit 0\n")
	symlinkTools(t, bin, "sed", "tr", "grep")

	out, err := runHostctlIsolated(t, bin, root, "", "firewall-install", nil)
	if err == nil {
		t.Fatalf("expected a refusal, got %q", out)
	}
	exit, ok := err.(*exec.ExitError)
	if !ok || exit.ExitCode() != 3 {
		t.Fatalf("exit = %v, want 3", err)
	}
	if !strings.Contains(out, "not supported on this distribution") {
		t.Fatalf("output = %q", out)
	}
}

func TestHostctlFirewalldUsesTheOfflineClientWhenStopped(t *testing.T) {
	root := t.TempDir()
	bin := filepath.Join(root, "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	// Isolate PATH: a CI runner ships a real ufw, which would otherwise win
	// backend detection before the firewalld stubs.
	writeStub(t, bin, "systemctl", "#!/bin/sh\nexit 1\n")
	writeStub(t, bin, "firewall-cmd", "#!/bin/sh\nexit 1\n")
	log := filepath.Join(root, "offline.log")
	writeStub(t, bin, "firewall-offline-cmd", `#!/bin/sh
case "$*" in
  "--list-rich-rules") echo 'rule family="ipv4" port port="22" protocol="tcp" accept' ;;
  *) printf '%s\n' "$*" >>"$OFFLINE_LOG" ;;
esac
exit 0
`)
	for _, tool := range []string{"awk", "grep", "sort", "uniq", "tr", "sed"} {
		path, err := exec.LookPath(tool)
		if err != nil {
			t.Fatalf("look up %s: %v", tool, err)
		}
		if err := os.Symlink(path, filepath.Join(bin, tool)); err != nil {
			t.Fatal(err)
		}
	}
	extra := []string{"OFFLINE_LOG=" + log}

	out, err := runHostctlIsolated(t, bin, root, "", "firewall-list", extra)
	if err != nil {
		t.Fatalf("firewall-list: %v\n%s", err, out)
	}
	if !strings.Contains(out, `port port="22"`) || strings.Contains(out, "backend=ufw") {
		t.Fatalf("offline listing = %q", out)
	}

	input := "action=allow\ndirection=in\nprotocol=tcp\nport=8080\nsource=\ncomment=\n"
	out, err = runHostctlIsolated(t, bin, root, input, "firewall-add", extra)
	if err != nil {
		t.Fatalf("firewall-add: %v\n%s", err, out)
	}
	data, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `--add-rich-rule=rule port port="8080" protocol="tcp" accept`) {
		t.Fatalf("offline add = %q", data)
	}
	if strings.Contains(string(data), "family=") {
		t.Fatalf("a rule without a source must not pin IPv4: %q", data)
	}
}

func TestHostctlFirewallStatusReadsAllSSHPorts(t *testing.T) {
	root := t.TempDir()
	firewallStubs(t, root)
	dropin := filepath.Join(root, "etc/ssh/sshd_config.d/05-gotham.conf")
	if err := os.MkdirAll(filepath.Dir(dropin), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dropin, []byte("Port 2222\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := runHostctlEnv(t, root, "", "firewall-status", nil)
	if err != nil {
		t.Fatalf("firewall-status: %v\n%s", err, out)
	}
	if !strings.Contains(out, "ssh_ports=22,2222") {
		t.Fatalf("ssh ports = %q", out)
	}
}

func TestHostctlFirewallStatusFallsBackTo22(t *testing.T) {
	root := t.TempDir()
	firewallStubs(t, root)
	writeStub(t, filepath.Join(root, "bin"), "sshd", "#!/bin/sh\nexit 0\n")
	out, err := runHostctlEnv(t, root, "", "firewall-status", nil)
	if err != nil {
		t.Fatalf("firewall-status: %v\n%s", err, out)
	}
	if !strings.Contains(out, "ssh_ports=22") {
		t.Fatalf("fallback = %q", out)
	}
}

func TestHostctlFirewallDenyFallsBackWhenInsertFails(t *testing.T) {
	root := t.TempDir()
	bin := filepath.Join(root, "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	log := filepath.Join(root, "ufw.log")
	writeStub(t, bin, "ufw", `#!/bin/sh
case "$1" in
  status) echo "Status: active"; exit 0 ;;
esac
printf '%s\n' "$*" >>"$UFW_LOG"
[ "$1" = "insert" ] && exit 1
exit 0
`)
	for _, tool := range []string{"grep", "tr"} {
		path, err := exec.LookPath(tool)
		if err != nil {
			t.Fatalf("look up %s: %v", tool, err)
		}
		if err := os.Symlink(path, filepath.Join(bin, tool)); err != nil {
			t.Fatal(err)
		}
	}
	input := "action=deny\ndirection=in\nprotocol=tcp\nport=22\nsource=\ncomment=\n"
	out, err := runHostctlIsolated(t, bin, root, input, "firewall-add", []string{"UFW_LOG=" + log})
	if err != nil {
		t.Fatalf("firewall-add: %v\n%s", err, out)
	}
	data, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 2 {
		t.Fatalf("ufw calls = %q, want the insert and the plain retry", data)
	}
	if !strings.HasPrefix(lines[0], "insert 1 deny in ") {
		t.Fatalf("first call = %q", lines[0])
	}
	if !strings.HasPrefix(lines[1], "deny in ") {
		t.Fatalf("retry call = %q", lines[1])
	}
}
