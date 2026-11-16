package deploy

import (
	"os"
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
	for _, want := range []string{"backend=ufw", "enabled=true", "manageable=true", "ssh_port=22"} {
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
		"port":    "action=allow\ndirection=in\nprotocol=tcp\nport=70000\nsource=\ncomment=\n",
		"source":  "action=allow\ndirection=in\nprotocol=tcp\nport=22\nsource=example.com\ncomment=\n",
		"comment": "action=allow\ndirection=in\nprotocol=tcp\nport=22\nsource=\ncomment=bad;comment\n",
		"action":  "action=block\ndirection=in\nprotocol=tcp\nport=22\nsource=\ncomment=\n",
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

func TestHostctlFirewalldUsesTheOfflineClientWhenStopped(t *testing.T) {
	root := t.TempDir()
	bin := filepath.Join(root, "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
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
	t.Setenv("PATH", bin+":"+os.Getenv("PATH"))

	out, err := runHostctlReal(t, root, "", "firewall-list", []string{"OFFLINE_LOG=" + log})
	if err != nil {
		t.Fatalf("firewall-list: %v\n%s", err, out)
	}
	if !strings.Contains(out, `port port="22"`) {
		t.Fatalf("offline listing = %q", out)
	}

	input := "action=allow\ndirection=in\nprotocol=tcp\nport=8080\nsource=\ncomment=\n"
	out, err = runHostctlReal(t, root, input, "firewall-add", []string{"OFFLINE_LOG=" + log})
	if err != nil {
		t.Fatalf("firewall-add: %v\n%s", err, out)
	}
	data, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `--add-rich-rule=rule family="ipv4" port port="8080" protocol="tcp" accept`) {
		t.Fatalf("offline add = %q", data)
	}
}
