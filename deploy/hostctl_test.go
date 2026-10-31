package deploy

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func runHostctl(t *testing.T, root, stdin string, verb string) (string, error) {
	return runHostctlEnv(t, root, stdin, verb, nil)
}

// runHostctlReal runs a verb with the dry-run seam off, so a test can observe
// what the helper actually executes. It is only used with stubs first on PATH.
func runHostctlReal(t *testing.T, root, stdin, verb string, extra []string) (string, error) {
	t.Helper()
	cmd := exec.Command("sh", "gotham-hostctl.sh", verb)
	cmd.Env = append(os.Environ(), "GOTHAM_HOSTCTL_ROOT="+root)
	cmd.Env = append(cmd.Env, extra...)
	cmd.Stdin = strings.NewReader(stdin)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// runHostctlOnly runs the helper with exactly this PATH, so a probe that looks
// for a tool sees neither the host's tools nor the test stubs.
func runHostctlOnly(t *testing.T, root, stdin, verb, path string) (string, error) {
	t.Helper()
	cmd := exec.Command("sh", "gotham-hostctl.sh", verb)
	cmd.Env = []string{"PATH=" + path, "GOTHAM_HOSTCTL_ROOT=" + root, "GOTHAM_HOSTCTL_DRYRUN=1"}
	cmd.Stdin = strings.NewReader(stdin)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func exitCode(t *testing.T, err error) int {
	t.Helper()
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return exit.ExitCode()
	}
	return -1
}

func writeStub(t *testing.T, bin, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(bin, name), []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
}

func runHostctlEnv(t *testing.T, root, stdin, verb string, extra []string) (string, error) {
	t.Helper()
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh not available")
	}
	cmd := exec.Command("sh", "gotham-hostctl.sh", verb)
	cmd.Env = append(os.Environ(), "GOTHAM_HOSTCTL_ROOT="+root, "GOTHAM_HOSTCTL_DRYRUN=1", "GOTHAM_HOSTCTL_IFACE=eth0")
	cmd.Env = append(cmd.Env, extra...)
	cmd.Stdin = strings.NewReader(stdin)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func stubProbes(t *testing.T, root string) []string {
	t.Helper()
	bin := filepath.Join(root, "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "etc/systemd/network"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "run/systemd/network"), 0o755); err != nil {
		t.Fatal(err)
	}
	ip := "#!/bin/sh\n" +
		"if [ \"$2\" = \"-4\" ] && [ \"$3\" = \"addr\" ]; then echo \"2: eth0 inet 103.176.22.225/24 brd x scope global eth0\"; fi\n" +
		"if [ \"$2\" = \"-4\" ] && [ \"$3\" = \"route\" ]; then echo \"default via 103.176.22.1 dev eth0 proto static\"; fi\n"
	resolvectl := "#!/bin/sh\necho \"Link 2 (eth0): 8.8.8.8 8.8.4.4\"\n"
	timedatectl := "#!/bin/sh\n" +
		"case \"$*\" in\n" +
		"  *Timezone*) echo \"Europe/Berlin\" ;;\n" +
		"  *ServerName*) echo \"time.cloudflare.com\" ;;\n" +
		"  *NTP*) echo \"yes\" ;;\n" +
		"esac\n"
	hostnamectl := "#!/bin/sh\necho \"node-1\"\n"
	for name, body := range map[string]string{
		"ip": ip, "resolvectl": resolvectl, "timedatectl": timedatectl, "hostnamectl": hostnamectl,
	} {
		if err := os.WriteFile(filepath.Join(bin, name), []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	// tzdata files for the zones the stubs report and the tests apply.
	for _, zone := range []string{"Europe/Berlin", "America/Argentina/Buenos_Aires"} {
		path := filepath.Join(root, "usr/share/zoneinfo", zone)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("fixture"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	netplan := "[Match]\nName=eth0\n\n[Network]\nDHCP=no\nAddress=103.176.22.225/24\nDNS=8.8.8.8 8.8.4.4\n\n[Route]\nGateway=103.176.22.1\n"
	if err := os.WriteFile(filepath.Join(root, "run/systemd/network/10-netplan-eth0.network"), []byte(netplan), 0o644); err != nil {
		t.Fatal(err)
	}
	return []string{"PATH=" + bin + ":" + os.Getenv("PATH")}
}

func sshTestStubs(t *testing.T, root string) ([]string, string) {
	t.Helper()
	bin := filepath.Join(root, "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	dropin := filepath.Join(root, "etc/ssh/sshd_config.d/05-gotham.conf")
	sshd := "#!/bin/sh\n" +
		"if [ \"$1\" = \"-t\" ]; then exit 0; fi\n" +
		"if [ \"$1\" = \"-T\" ]; then\n" +
		"  if [ -f \"$SSHD_DROPIN\" ]; then\n" +
		"    grep -i '^Port ' \"$SSHD_DROPIN\"\n" +
		"    grep -i '^PasswordAuthentication ' \"$SSHD_DROPIN\"\n" +
		"  else\n" +
		"    echo 'port 22'\n" +
		"    echo 'passwordauthentication yes'\n" +
		"  fi\n" +
		"  echo 'pubkeyauthentication yes'\n" +
		"  [ -n \"${SSHD_AUTHKEYS:-}\" ] && echo \"authorizedkeysfile ${SSHD_AUTHKEYS}\"\n" +
		"  [ -n \"${SSHD_ROOTLOGIN:-}\" ] && echo \"permitrootlogin ${SSHD_ROOTLOGIN}\"\n" +
		"  exit 0\n" +
		"fi\n"
	writeStub(t, bin, "sshd", sshd)
	ufwLog := filepath.Join(root, "ufw.log")
	ufw := "#!/bin/sh\n" +
		"case \"$1\" in\n" +
		"  status) echo 'Status: active' ;;\n" +
		"  allow|delete) printf '%s\\n' \"$*\" >>\"$UFW_LOG\" ;;\n" +
		"esac\n" +
		"exit 0\n"
	writeStub(t, bin, "ufw", ufw)
	systemctlLog := filepath.Join(root, "systemctl.log")
	systemctl := "#!/bin/sh\n" +
		"case \"$*\" in\n" +
		"  'is-enabled --quiet ssh.socket'|'is-active --quiet ssh.socket')\n" +
		"    [ \"${SSH_SOCKET:-0}\" = '1' ]; exit $? ;;\n" +
		"  'is-active --quiet ssh'|'is-active --quiet sshd')\n" +
		"    [ \"${SSH_SOCKET:-0}\" = '1' ] && exit 1; exit 0 ;;\n" +
		"  'show -p MainPID --value gotham-agent') echo 0; exit 0 ;;\n" +
		"esac\n" +
		"printf '%s\\n' \"$*\" >>\"$SYSTEMCTL_LOG\"\n" +
		"exit 0\n"
	writeStub(t, bin, "systemctl", systemctl)
	systemdLog := filepath.Join(root, "systemd-run.log")
	writeStub(t, bin, "systemd-run", "#!/bin/sh\nprintf '%s\\n' \"$*\" >>\"$SYSTEMD_LOG\"\nexit 0\n")
	writeStub(t, bin, "ps", "#!/bin/sh\necho gotham-agent\n")
	return []string{
		"PATH=" + bin + ":" + os.Getenv("PATH"),
		"SSHD_DROPIN=" + dropin,
		"UFW_LOG=" + ufwLog,
		"SYSTEMD_LOG=" + systemdLog,
		"SYSTEMCTL_LOG=" + systemctlLog,
	}, dropin
}

func TestHostctlSSHStatusReportsState(t *testing.T) {
	root := t.TempDir()
	extra, _ := sshTestStubs(t, root)
	if err := os.MkdirAll(filepath.Join(root, "root/.ssh"), 0o700); err != nil {
		t.Fatal(err)
	}
	key := "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAITestKey gotham@test\n"
	if err := os.WriteFile(filepath.Join(root, "root/.ssh/authorized_keys"), []byte(key), 0o600); err != nil {
		t.Fatal(err)
	}
	out, err := runHostctlEnv(t, root, "", "status", extra)
	if err != nil {
		t.Fatalf("status failed: %v\n%s", err, out)
	}
	for _, want := range []string{
		`"ssh":{"available":true,"port":22,"password_authentication":true,"key_auth_available":true}`,
		`"agent_root":false`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("status misses %s:\n%s", want, out)
		}
	}
}

func TestHostctlApplySSHKeepsOldPortUntilConfirm(t *testing.T) {
	root := t.TempDir()
	extra, dropin := sshTestStubs(t, root)
	if err := os.MkdirAll(filepath.Join(root, "root/.ssh"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "root/.ssh/authorized_keys"),
		[]byte("ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAITestKey gotham@test\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	out, err := runHostctlReal(t, root, "port=2222\npassword_authentication=true\n", "apply-ssh", extra)
	if err != nil {
		t.Fatalf("apply-ssh failed: %v\n%s", err, out)
	}
	data, err := os.ReadFile(dropin)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "Port 22\n") || !strings.Contains(string(data), "Port 2222\n") {
		t.Fatalf("a trial must keep both ports:\n%s", data)
	}
	pending := filepath.Join(root, "var/lib/gotham-hostctl/ssh-pending")
	if _, err := os.Stat(pending); err != nil {
		t.Fatalf("apply-ssh must record a pending change: %v", err)
	}
	systemdLog, _ := os.ReadFile(filepath.Join(root, "systemd-run.log"))
	if !strings.Contains(string(systemdLog), "--unit=gotham-hostctl-ssh-revert") {
		t.Fatalf("apply-ssh must arm the revert timer: %s", systemdLog)
	}

	if out, err := runHostctlReal(t, root, "", "confirm-ssh", extra); err != nil {
		t.Fatalf("confirm-ssh failed: %v\n%s", err, out)
	}
	data, err = os.ReadFile(dropin)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "Port 22\n") || !strings.Contains(string(data), "Port 2222\n") {
		t.Fatalf("confirm must keep only the new port:\n%s", data)
	}
	if _, err := os.Stat(pending); !os.IsNotExist(err) {
		t.Fatalf("confirm left the pending file: %v", err)
	}

	out, err = runHostctlReal(t, root, "port=2223\npassword_authentication=true\n", "apply-ssh", extra)
	if err != nil {
		t.Fatalf("second apply-ssh failed: %v\n%s", err, out)
	}
	if out, err := runHostctlReal(t, root, "", "revert-ssh", extra); err != nil {
		t.Fatalf("revert-ssh failed: %v\n%s", err, out)
	}
	data, err = os.ReadFile(dropin)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "Port 2223\n") || !strings.Contains(string(data), "Port 2222\n") {
		t.Fatalf("revert must restore the previous port:\n%s", data)
	}
	log, err := os.ReadFile(filepath.Join(root, "ufw.log"))
	if err != nil || !strings.Contains(string(log), "allow 2222/tcp") {
		t.Fatalf("the new port was not opened in the firewall: %v %s", err, log)
	}
}

func TestHostctlSSHReloadUsesSocketActivation(t *testing.T) {
	root := t.TempDir()
	extra, _ := sshTestStubs(t, root)
	extra = append(extra, "SSH_SOCKET=1")
	if err := os.MkdirAll(filepath.Join(root, "root/.ssh"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "root/.ssh/authorized_keys"),
		[]byte("ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAITestKey gotham@test\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	out, err := runHostctlReal(t, root, "port=2222\npassword_authentication=true\n", "apply-ssh", extra)
	if err != nil {
		t.Fatalf("apply-ssh on a socket-activated host failed: %v\n%s", err, out)
	}
	log, _ := os.ReadFile(filepath.Join(root, "systemctl.log"))
	for _, want := range []string{"daemon-reload", "restart ssh.socket"} {
		if !strings.Contains(string(log), want) {
			t.Fatalf("socket reload misses %q:\n%s", want, log)
		}
	}
	if strings.Contains(string(log), "reload ssh\n") {
		t.Fatalf("socket activation must not use ssh.service:\n%s", log)
	}
	if out, err := runHostctlReal(t, root, "", "confirm-ssh", extra); err != nil {
		t.Fatalf("confirm-ssh failed: %v\n%s", err, out)
	}
	log, _ = os.ReadFile(filepath.Join(root, "systemctl.log"))
	if got := strings.Count(string(log), "restart ssh.socket"); got < 2 {
		t.Fatalf("confirm must reload the socket too, got %d:\n%s", got, log)
	}
}

func TestHostctlSSHRefusesExternalPort(t *testing.T) {
	root := t.TempDir()
	extra, dropin := sshTestStubs(t, root)
	if err := os.MkdirAll(filepath.Join(root, "root/.ssh"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "root/.ssh/authorized_keys"),
		[]byte("ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAITestKey gotham@test\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "etc/ssh"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "etc/ssh/sshd_config"), []byte("Port 22\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := runHostctlReal(t, root, "port=2222\npassword_authentication=true\n", "apply-ssh", extra)
	if err == nil {
		t.Fatalf("apply-ssh must refuse an external Port:\n%s", out)
	}
	if code := exitCode(t, err); code != 5 {
		t.Fatalf("exit = %d, want 5:\n%s", code, out)
	}
	if !strings.Contains(out, "another sshd configuration sets Port 22") {
		t.Fatalf("refusal does not name the external port:\n%s", out)
	}
	if _, err := os.Stat(dropin); !os.IsNotExist(err) {
		t.Fatalf("a refused change wrote the drop-in: %v", err)
	}
}

func TestHostctlSSHDisablePasswordChecksServerUser(t *testing.T) {
	newRoot := func(t *testing.T) (string, []string, string) {
		t.Helper()
		root := t.TempDir()
		extra, dropin := sshTestStubs(t, root)
		if err := os.MkdirAll(filepath.Join(root, "etc"), 0o755); err != nil {
			t.Fatal(err)
		}
		passwd := "root:x:0:0:root:" + root + "/root:/bin/bash\n" +
			"deploy:x:1000:1000::" + root + "/home/deploy:/bin/bash\n" +
			"nologin:x:1001:1001::" + root + "/home/nologin:/usr/sbin/nologin\n"
		if err := os.WriteFile(filepath.Join(root, "etc/passwd"), []byte(passwd), 0o644); err != nil {
			t.Fatal(err)
		}
		return root, extra, dropin
	}
	writeKey := func(t *testing.T, path string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAITestKey gotham@test\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	t.Run("key for another user does not count", func(t *testing.T) {
		root, extra, dropin := newRoot(t)
		writeKey(t, filepath.Join(root, "root/.ssh/authorized_keys"))
		out, err := runHostctlReal(t, root, "port=22\npassword_authentication=false\nssh_user=deploy\n", "apply-ssh", extra)
		if err == nil || exitCode(t, err) != 6 {
			t.Fatalf("another user's key must not count: %v\n%s", err, out)
		}
		if _, err := os.Stat(dropin); !os.IsNotExist(err) {
			t.Fatalf("a refused change wrote the drop-in: %v", err)
		}
		writeKey(t, filepath.Join(root, "home/deploy/.ssh/authorized_keys"))
		if out, err := runHostctlReal(t, root, "port=22\npassword_authentication=false\nssh_user=deploy\n", "apply-ssh", extra); err != nil {
			t.Fatalf("the server user's key was not accepted: %v\n%s", err, out)
		}
	})

	t.Run("nologin shell does not count", func(t *testing.T) {
		root, extra, _ := newRoot(t)
		writeKey(t, filepath.Join(root, "home/nologin/.ssh/authorized_keys"))
		out, err := runHostctlReal(t, root, "port=22\npassword_authentication=false\nssh_user=nologin\n", "apply-ssh", extra)
		if err == nil || exitCode(t, err) != 6 {
			t.Fatalf("a nologin user must not count: %v\n%s", err, out)
		}
	})

	t.Run("PermitRootLogin no is honoured", func(t *testing.T) {
		root, extra, _ := newRoot(t)
		writeKey(t, filepath.Join(root, "root/.ssh/authorized_keys"))
		extra = append(extra, "SSHD_ROOTLOGIN=no")
		out, err := runHostctlReal(t, root, "port=22\npassword_authentication=false\nssh_user=root\n", "apply-ssh", extra)
		if err == nil || exitCode(t, err) != 6 {
			t.Fatalf("PermitRootLogin=no must block the root key: %v\n%s", err, out)
		}
		extra = append(extra[:len(extra)-1], "SSHD_ROOTLOGIN=prohibit-password")
		if out, err := runHostctlReal(t, root, "port=22\npassword_authentication=false\nssh_user=root\n", "apply-ssh", extra); err != nil {
			t.Fatalf("root key with prohibit-password rejected: %v\n%s", err, out)
		}
	})

	t.Run("AuthorizedKeysFile is honoured", func(t *testing.T) {
		root, extra, _ := newRoot(t)
		writeKey(t, filepath.Join(root, "etc/ssh/authorized_keys/deploy"))
		extra = append(extra, "SSHD_AUTHKEYS="+filepath.Join(root, "etc/ssh/authorized_keys")+"/%u")
		if out, err := runHostctlReal(t, root, "port=22\npassword_authentication=false\nssh_user=deploy\n", "apply-ssh", extra); err != nil {
			t.Fatalf("AuthorizedKeysFile override not honoured: %v\n%s", err, out)
		}
	})
}

func TestHostctlSSHDisablePasswordNeedsKeys(t *testing.T) {
	root := t.TempDir()
	extra, dropin := sshTestStubs(t, root)
	out, err := runHostctlReal(t, root, "port=22\npassword_authentication=false\n", "apply-ssh", extra)
	if err == nil {
		t.Fatalf("disabling password login without an authorized key must be refused:\n%s", out)
	}
	if code := exitCode(t, err); code != 6 {
		t.Fatalf("exit = %d, want 6:\n%s", code, out)
	}
	if _, err := os.Stat(dropin); !os.IsNotExist(err) {
		t.Fatalf("a refused change wrote the drop-in: %v", err)
	}

	if err := os.MkdirAll(filepath.Join(root, "root/.ssh"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "root/.ssh/authorized_keys"),
		[]byte("ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAITestKey gotham@test\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	out, err = runHostctlReal(t, root, "port=22\npassword_authentication=false\n", "apply-ssh", extra)
	if err != nil {
		t.Fatalf("apply with key auth failed: %v\n%s", err, out)
	}
	data, readErr := os.ReadFile(dropin)
	if readErr != nil || !strings.Contains(string(data), "PasswordAuthentication no") {
		t.Fatalf("password login not disabled: %v\n%s", readErr, data)
	}
}

func TestHostctlAgentRootToggle(t *testing.T) {
	root := t.TempDir()
	bin := filepath.Join(root, "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	writeStub(t, bin, "systemctl", "#!/bin/sh\nexit 0\n")
	writeStub(t, bin, "ps", "#!/bin/sh\nprintf '%s\\n' \"$PS_USER\"\n")
	restartLog := filepath.Join(root, "restart.log")
	writeStub(t, bin, "systemd-run", "#!/bin/sh\nprintf '%s\\n' \"$*\" >>\"$RESTART_LOG\"\nexit 0\n")
	envFile := filepath.Join(root, "etc/gotham/agent.env")
	if err := os.MkdirAll(filepath.Dir(envFile), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(envFile, []byte("GOTHAM_AGENT_CP_ADDR=127.0.0.1:9442\nGOTHAM_AGENT_FILES=false\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	extra := []string{
		"PATH=" + bin + ":" + os.Getenv("PATH"),
		"PS_USER=gotham-agent", "RESTART_LOG=" + restartLog,
	}

	out, err := runHostctlReal(t, root, "enabled=true\n", "apply-agent-root", extra)
	if err != nil {
		t.Fatalf("enable root failed: %v\n%s", err, out)
	}
	dropin := filepath.Join(root, "etc/systemd/system/gotham-agent.service.d/file-manager.conf")
	data, err := os.ReadFile(dropin)
	if err != nil || !strings.Contains(string(data), "User=root") {
		t.Fatalf("root drop-in: %v\n%s", err, data)
	}
	env, _ := os.ReadFile(envFile)
	if strings.Contains(string(env), "GOTHAM_AGENT_FILES=false") ||
		!strings.Contains(string(env), "GOTHAM_AGENT_FILES=true") {
		t.Fatalf("agent env not switched on:\n%s", env)
	}
	if log, _ := os.ReadFile(restartLog); !strings.Contains(string(log), "restart") {
		t.Fatalf("agent restart not scheduled: %s", log)
	}

	extra[1] = "PS_USER=root"
	out, err = runHostctlReal(t, root, "enabled=false\n", "apply-agent-root", extra)
	if err != nil {
		t.Fatalf("disable root failed: %v\n%s", err, out)
	}
	if _, err := os.Stat(dropin); !os.IsNotExist(err) {
		t.Fatalf("root drop-in not removed: %v", err)
	}
	env, _ = os.ReadFile(envFile)
	if strings.Contains(string(env), "GOTHAM_AGENT_FILES=true") ||
		!strings.Contains(string(env), "GOTHAM_AGENT_FILES=false") {
		t.Fatalf("disabling root must restore the previous file-manager answer:\n%s", env)
	}
}

func TestHostctlApplyRevertAndRejects(t *testing.T) {
	root := t.TempDir()
	good := "dns=1.1.1.1\nipv4_mode=static\nipv4_address=10.0.0.5/24\nipv4_gateway=10.0.0.1\n" +
		"ipv6_enabled=true\nipv6_mode=dhcp\nipv6_address=\nipv6_gateway=\nrevert_after=120\n"
	if out, err := runHostctl(t, root, good, "apply-network"); err != nil {
		t.Fatalf("apply failed: %v\n%s", err, out)
	}
	netFile := filepath.Join(root, "etc/systemd/network/05-gotham.network")
	data, err := os.ReadFile(netFile)
	if err != nil || !strings.Contains(string(data), "Address=10.0.0.5/24") {
		t.Fatalf("network file: %v %s", err, data)
	}
	if _, err := runHostctl(t, root, good, "apply-network"); err == nil {
		t.Fatal("second apply while pending must fail")
	}
	if out, err := runHostctl(t, root, "", "revert-network"); err != nil {
		t.Fatalf("revert failed: %v\n%s", err, out)
	}
	if _, err := os.Stat(netFile); !os.IsNotExist(err) {
		t.Fatal("revert must remove a file that did not exist before")
	}

	for name, in := range map[string]string{
		"injection": strings.Replace(good, "1.1.1.1", "1.1.1.1;reboot", 1),
		"bad key":   good + "evil=1\n",
		"bad gw":    strings.Replace(good, "10.0.0.1", "10.0.0.1 x", 1),
	} {
		if _, err := runHostctl(t, root, in, "apply-network"); err == nil {
			t.Errorf("%s: helper accepted invalid input", name)
		}
	}
	if _, err := runHostctl(t, root, "hostname=a;b\nntp_enabled=true\nntp_servers=\n", "apply-system"); err == nil {
		t.Error("invalid hostname accepted")
	}
	if _, err := runHostctl(t, root, "", "rm-rf"); err == nil {
		t.Error("unknown verb accepted")
	}
}

func TestHostctlStatusReportsHostTruth(t *testing.T) {
	root := t.TempDir()
	extra := stubProbes(t, root)
	out, err := runHostctlEnv(t, root, "", "status", extra)
	if err != nil {
		t.Fatalf("status failed: %v\n%s", err, out)
	}
	for _, want := range []string{
		`"interface":"eth0"`, `"mode":"static"`, `"address":"103.176.22.225/24"`,
		`"gateway":"103.176.22.1"`, `"dns_servers":["8.8.8.8","8.8.4.4"]`,
		`"hostname":"node-1"`, `"ntp_enabled":true`,
		`"ntp_servers":["time.cloudflare.com"]`, `"timezone":"Europe/Berlin"`,
		`"cleanup":true`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("status misses %s:\n%s", want, out)
		}
	}
}

func TestHostctlApplySystemTimezone(t *testing.T) {
	root := t.TempDir()
	extra := stubProbes(t, root)
	good := "hostname=node-1\nntp_enabled=true\nntp_servers=time.cloudflare.com\n" +
		"timezone=America/Argentina/Buenos_Aires\n"
	out, err := runHostctlEnv(t, root, good, "apply-system", extra)
	if err != nil {
		t.Fatalf("apply-system failed: %v\n%s", err, out)
	}
	if !strings.Contains(out, "timedatectl set-timezone America/Argentina/Buenos_Aires") {
		t.Errorf("apply-system misses the timezone command:\n%s", out)
	}
	if data, err := os.ReadFile(filepath.Join(root, "etc/systemd/timesyncd.conf.d/05-gotham.conf")); err != nil ||
		!strings.Contains(string(data), "NTP=time.cloudflare.com") {
		t.Fatalf("timesyncd drop-in: %v %s", err, data)
	}

	// The helper itself rejects a timezone that is not a real IANA zone, before
	// it touches the host: a traversal-looking value, a shell-looking value and
	// a well-formed name tzdata does not ship all fail with nothing applied.
	for name, zone := range map[string]string{
		"traversal":     "../../etc/passwd",
		"dot segment":   "Europe/../Berlin",
		"absolute":      "/etc/passwd",
		"shell":         "Europe/Berlin;reboot",
		"empty segment": "Europe//Berlin",
		"trailing sep":  "Europe/Berlin/",
		"unknown zone":  "Not/AZone",
		"trailing gap":  "Europe/Berlin ",
		"substitution":  "$(reboot)",
	} {
		t.Run(name, func(t *testing.T) {
			in := "hostname=\nntp_enabled=true\nntp_servers=\ntimezone=" + zone + "\n"
			out, err := runHostctlEnv(t, root, in, "apply-system", extra)
			if err == nil {
				t.Fatalf("helper accepted invalid timezone %q:\n%s", zone, out)
			}
			if strings.Contains(out, "set-timezone") {
				t.Fatalf("helper reached timedatectl with %q:\n%s", zone, out)
			}
		})
	}
}

func TestHostctlApplySystemTimezoneNeedsNoNtpDirectory(t *testing.T) {
	root := t.TempDir()
	extra := stubProbes(t, root)
	// Make the NTP drop-in directory impossible to create (its parent is a
	// regular file), regardless of whether the test runs as root.
	if err := os.RemoveAll(filepath.Join(root, "etc")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "etc"), []byte("not a directory"), 0o644); err != nil {
		t.Fatal(err)
	}

	in := "hostname=\nntp_enabled=true\nntp_servers=\ntimezone=Europe/Berlin\n"
	out, err := runHostctlEnv(t, root, in, "apply-system", extra)
	if err != nil {
		t.Fatalf("timezone-only apply must not need the NTP drop-in directory: %v\n%s", err, out)
	}
	if !strings.Contains(out, "timedatectl set-timezone Europe/Berlin") {
		t.Errorf("apply-system misses the timezone command:\n%s", out)
	}

	// A real NTP server change still owns that directory and must fail loudly
	// instead of pretending it was written.
	withNtp := "hostname=\nntp_enabled=true\nntp_servers=time.cloudflare.com\ntimezone=Europe/Berlin\n"
	if out, err := runHostctlEnv(t, root, withNtp, "apply-system", extra); err == nil {
		t.Fatalf("NTP change into an unusable directory must fail:\n%s", out)
	}
}

func TestHostctlReexecsWriteVerbsOnly(t *testing.T) {
	dir := t.TempDir()
	record := filepath.Join(dir, "argv")
	fake := filepath.Join(dir, "fake-systemd-run")
	body := "#!/bin/sh\nprintf '%s\\n' \"$@\" > \"$FAKE_RECORD\"\nexit 0\n"
	if err := os.WriteFile(fake, []byte(body), 0o755); err != nil {
		t.Fatalf("write fake systemd-run: %v", err)
	}
	self, err := filepath.Abs("gotham-hostctl.sh")
	if err != nil {
		t.Fatalf("abs path: %v", err)
	}

	for _, verb := range []string{
		"apply-system", "apply-network", "revert-network", "cleanup-journal", "cleanup-package-cache",
		"apply-ssh", "confirm-ssh", "revert-ssh", "apply-agent-root",
	} {
		if err := os.Remove(record); err != nil && !os.IsNotExist(err) {
			t.Fatal(err)
		}
		cmd := exec.Command("sh", self, verb)
		cmd.Env = append(os.Environ(), "GOTHAM_HOSTCTL_SYSTEMD_RUN="+fake, "FAKE_RECORD="+record)
		cmd.Stdin = strings.NewReader("hostname=\nntp_enabled=true\nntp_servers=\ntimezone=Europe/Berlin\n")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%s re-exec: %v: %s", verb, err, out)
		}
		recorded, err := os.ReadFile(record)
		if err != nil {
			t.Fatalf("%s did not use systemd-run: %v", verb, err)
		}
		for _, want := range []string{
			"--unit=gotham-hostctl-" + verb + "-",
			"--property=RuntimeMaxSec=120",
			"--setenv=GOTHAM_HOSTCTL_INNER=1",
			self,
		} {
			if !strings.Contains(string(recorded), want) {
				t.Errorf("%s systemd-run argv missing %q: %q", verb, want, recorded)
			}
		}
	}

	// Read-only verbs stay in-process: the fake must never run.
	if err := os.Remove(record); err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	root := t.TempDir()
	extra := stubProbes(t, root)
	cmd := exec.Command("sh", self, "status")
	cmd.Env = append(os.Environ(), "GOTHAM_HOSTCTL_SYSTEMD_RUN="+fake, "FAKE_RECORD="+record, "GOTHAM_HOSTCTL_ROOT="+root)
	cmd.Env = append(cmd.Env, extra...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("status: %v: %s", err, out)
	}
	if _, err := os.Stat(record); !os.IsNotExist(err) {
		t.Fatalf("status must not re-exec through systemd-run: %s", out)
	}
	if !strings.Contains(string(out), `"hostname":"node-1"`) {
		t.Errorf("status did not run in-process: %s", out)
	}

	// cleanup-status is read-only too, even on a host with no cleanup tools.
	if err := os.Remove(record); err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	cmd = exec.Command("sh", self, "cleanup-status")
	cmd.Env = []string{
		"PATH=" + t.TempDir(), "GOTHAM_HOSTCTL_SYSTEMD_RUN=" + fake,
		"FAKE_RECORD=" + record, "GOTHAM_HOSTCTL_ROOT=" + root,
	}
	out, err = cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("cleanup-status: %v: %s", err, out)
	}
	if _, err := os.Stat(record); !os.IsNotExist(err) {
		t.Fatalf("cleanup-status must not re-exec through systemd-run: %s", out)
	}

	// The inner marker stops a second re-exec (loop guard).
	cmd = exec.Command("sh", self, "apply-system")
	cmd.Env = append(os.Environ(), "GOTHAM_HOSTCTL_SYSTEMD_RUN="+fake, "FAKE_RECORD="+record,
		"GOTHAM_HOSTCTL_INNER=1", "GOTHAM_HOSTCTL_DRYRUN=1", "GOTHAM_HOSTCTL_ROOT="+root)
	cmd.Env = append(cmd.Env, extra...)
	cmd.Stdin = strings.NewReader("hostname=\nntp_enabled=true\nntp_servers=\ntimezone=Europe/Berlin\n")
	out, err = cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("inner apply-system: %v: %s", err, out)
	}
	if _, err := os.Stat(record); !os.IsNotExist(err) {
		t.Fatalf("inner run must not re-exec again: %s", out)
	}
	if !strings.Contains(string(out), "timedatectl set-timezone Europe/Berlin") {
		t.Errorf("inner run did not apply in-process: %s", out)
	}
}

func TestHostctlDNSOnlyOverridesLinkDNS(t *testing.T) {
	root := t.TempDir()
	extra := stubProbes(t, root)
	dropin := filepath.Join(root, "etc/systemd/network/10-netplan-eth0.network.d/05-gotham.conf")
	resFile := filepath.Join(root, "etc/systemd/resolved.conf.d/05-gotham.conf")
	dnsOnly := "dns=1.1.1.1,1.0.0.1\nipv4_mode=dhcp\nipv4_address=\nipv4_gateway=\n" +
		"ipv6_enabled=false\nipv6_mode=dhcp\nipv6_address=\nipv6_gateway=\nrevert_after=120\ndns_only=true\n"
	out, err := runHostctlEnv(t, root, dnsOnly, "apply-network", extra)
	if err != nil {
		t.Fatalf("dns-only apply failed: %v\n%s", err, out)
	}
	data, err := os.ReadFile(dropin)
	if err != nil || !strings.Contains(string(data), "DNS=1.1.1.1 1.0.0.1") {
		t.Fatalf("link drop-in: %v %s", err, data)
	}
	for _, bad := range []string{"Address=", "Gateway=", "[Route]"} {
		if strings.Contains(string(data), bad) {
			t.Fatalf("link drop-in must carry DNS only, found %q:\n%s", bad, data)
		}
	}
	if data, err := os.ReadFile(resFile); err != nil || !strings.Contains(string(data), "DNS=1.1.1.1 1.0.0.1") {
		t.Fatalf("resolved drop-in: %v %s", err, data)
	}
	for _, want := range []string{"networkctl reload", "resolvectl dns eth0 1.1.1.1 1.0.0.1"} {
		if !strings.Contains(out, want) {
			t.Errorf("apply misses %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "reconfigure") {
		t.Errorf("dns-only apply must not reconfigure the interface:\n%s", out)
	}

	out, err = runHostctlEnv(t, root, "", "revert-network", extra)
	if err != nil {
		t.Fatalf("revert failed: %v\n%s", err, out)
	}
	if _, err := os.Stat(dropin); !os.IsNotExist(err) {
		t.Fatal("revert must remove a link drop-in that did not exist before")
	}
	if _, err := os.Stat(resFile); !os.IsNotExist(err) {
		t.Fatal("revert must remove a drop-in that did not exist before")
	}
	for _, want := range []string{"networkctl reload", "resolvectl dns eth0 8.8.8.8 8.8.4.4"} {
		if !strings.Contains(out, want) {
			t.Errorf("revert misses %q:\n%s", want, out)
		}
	}

	old := "[Network]\nDNS=8.8.8.8 8.8.4.4\n"
	if err := os.MkdirAll(filepath.Dir(dropin), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dropin, []byte(old), 0o644); err != nil {
		t.Fatal(err)
	}
	if out, err := runHostctlEnv(t, root, dnsOnly, "apply-network", extra); err != nil {
		t.Fatalf("dns-only apply failed: %v\n%s", err, out)
	}
	if _, err := runHostctlEnv(t, root, "", "revert-network", extra); err != nil {
		t.Fatalf("revert failed: %v\n%s", err, out)
	}
	if data, err := os.ReadFile(dropin); err != nil || string(data) != old {
		t.Fatalf("revert altered the link drop-in: %v %q", err, data)
	}

	clear := strings.Replace(dnsOnly, "dns=1.1.1.1,1.0.0.1", "dns=", 1)
	out, err = runHostctlEnv(t, root, clear, "apply-network", extra)
	if err != nil {
		t.Fatalf("dns-clear apply failed: %v\n%s", err, out)
	}
	if _, err := os.Stat(dropin); !os.IsNotExist(err) {
		t.Fatal("clearing DNS must remove the link drop-in")
	}
	if !strings.Contains(out, "resolvectl revert eth0") {
		t.Errorf("clear misses runtime revert:\n%s", out)
	}
	if _, err := runHostctlEnv(t, root, "", "revert-network", extra); err != nil {
		t.Fatalf("revert failed: %v\n%s", err, out)
	}
	if data, err := os.ReadFile(dropin); err != nil || string(data) != old {
		t.Fatalf("revert after clear altered the link drop-in: %v %q", err, data)
	}
}

func TestHostctlCleanupStatusReportsSizes(t *testing.T) {
	root := t.TempDir()
	extra := stubProbes(t, root)
	bin := filepath.Join(root, "bin")
	writeStub(t, bin, "journalctl", "#!/bin/sh\n"+
		"case \"$1\" in\n"+
		"  --disk-usage) echo \"Archived and active journals take up 1.2G in the file system.\" ;;\n"+
		"esac\n")
	// One .deb file and the partial download directory: two du calls of 100.
	writeStub(t, bin, "du", "#!/bin/sh\nprintf '100\\t%s\\n' \"$*\"\n")
	writeStub(t, bin, "apt-get", "#!/bin/sh\nexit 0\n")
	archives := filepath.Join(root, "var/cache/apt/archives")
	if err := os.MkdirAll(filepath.Join(archives, "partial"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(archives, "app.deb"), []byte("deb"), 0o644); err != nil {
		t.Fatal(err)
	}

	out, err := runHostctlEnv(t, root, "", "cleanup-status", extra)
	if err != nil {
		t.Fatalf("cleanup-status failed: %v\n%s", err, out)
	}
	for _, want := range []string{
		`"journal":{"supported":true,"bytes":1288490189}`,
		`"package_cache":{"supported":true,"manager":"apt","bytes":200}`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("cleanup-status misses %s:\n%s", want, out)
		}
	}
}

func TestHostctlCleanupStatusUnsupportedHost(t *testing.T) {
	root := t.TempDir()
	out, err := runHostctlOnly(t, root, "", "cleanup-status", t.TempDir())
	if err != nil {
		t.Fatalf("cleanup-status on an unsupported host must still succeed: %v\n%s", err, out)
	}
	for _, want := range []string{
		`"journal":{"supported":false,"bytes":0}`,
		`"package_cache":{"supported":false,"manager":"","bytes":0}`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("cleanup-status misses %s:\n%s", want, out)
		}
	}
}

func TestHostctlCleanupStatusNeedsRunningJournald(t *testing.T) {
	root := t.TempDir()
	extra := stubProbes(t, root)
	bin := filepath.Join(root, "bin")
	writeStub(t, bin, "journalctl", "#!/bin/sh\necho \"take up 1G in the file system\"\n")
	writeStub(t, bin, "systemctl", "#!/bin/sh\nexit 3\n")

	out, err := runHostctlReal(t, root, "", "cleanup-status", extra)
	if err != nil {
		t.Fatalf("cleanup-status failed: %v\n%s", err, out)
	}
	if !strings.Contains(out, `"journal":{"supported":false,"bytes":0}`) {
		t.Errorf("journalctl without journald must be unsupported: %s", out)
	}
}

func TestHostctlCleanupJournalClampsMinAge(t *testing.T) {
	root := t.TempDir()
	extra := stubProbes(t, root)
	args := filepath.Join(root, "journal.args")
	writeStub(t, filepath.Join(root, "bin"), "systemctl", "#!/bin/sh\nexit 0\n")
	writeStub(t, filepath.Join(root, "bin"), "journalctl", "#!/bin/sh\n"+
		"printf '%s\\n' \"$*\" >> \"$JOURNAL_ARGS\"\n"+
		"case \"$1\" in\n"+
		"  --disk-usage) echo \"take up 1G in the file system\" ;;\n"+
		"esac\n")
	extra = append(extra, "JOURNAL_ARGS="+args)

	out, err := runHostctlReal(t, root, "min_age_seconds=3600\n", "cleanup-journal", extra)
	if err != nil {
		t.Fatalf("cleanup-journal failed: %v\n%s", err, out)
	}
	if !strings.Contains(out, `{"before":1073741824,"after":1073741824}`) {
		t.Errorf("cleanup-journal output = %s", out)
	}
	recorded, err := os.ReadFile(args)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(recorded), "--vacuum-time=86400s") {
		t.Errorf("min_age_seconds was not clamped to one day: %s", recorded)
	}
}

func TestHostctlCleanupPackageCacheUsesManager(t *testing.T) {
	for _, tc := range []struct {
		name    string
		tool    string
		cacheY  string
		wantArg string
		wantMgr string
	}{
		{"apt", "apt-get", "var/cache/apt", "clean", "apt"},
		{"dnf", "dnf", "var/cache/dnf", "clean packages", "dnf"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			extra := stubProbes(t, root)
			bin := filepath.Join(root, "bin")
			args := filepath.Join(root, "pkg.args")
			writeStub(t, bin, tc.tool, "#!/bin/sh\nprintf '%s\\n' \"$*\" >> \"$PKG_ARGS\"\n")
			writeStub(t, bin, "du", "#!/bin/sh\nprintf '100\\t%s\\n' \"$*\"\n")
			archives := filepath.Join(root, tc.cacheY)
			if tc.name == "apt" {
				archives = filepath.Join(archives, "archives")
			}
			if err := os.MkdirAll(archives, 0o755); err != nil {
				t.Fatal(err)
			}
			extra = append(extra, "PKG_ARGS="+args)

			out, err := runHostctlReal(t, root, "", "cleanup-package-cache", extra)
			if err != nil {
				t.Fatalf("cleanup-package-cache failed: %v\n%s", err, out)
			}
			if !strings.Contains(out, `"before":`) || !strings.Contains(out, `"after":`) {
				t.Errorf("output = %s; want before/after", out)
			}
			recorded, err := os.ReadFile(args)
			if err != nil {
				t.Fatal(err)
			}
			if got := strings.TrimSpace(string(recorded)); got != tc.wantArg {
				t.Errorf("manager call = %q; want %q", got, tc.wantArg)
			}
			status, err := runHostctlEnv(t, root, "", "cleanup-status", extra)
			if err != nil {
				t.Fatalf("cleanup-status failed: %v\n%s", err, status)
			}
			if !strings.Contains(status, `"manager":"`+tc.wantMgr+`"`) {
				t.Errorf("cleanup-status misses manager %s: %s", tc.wantMgr, status)
			}
		})
	}
}

func TestHostctlCleanupRejectsInvalidInput(t *testing.T) {
	root := t.TempDir()
	extra := stubProbes(t, root)
	bin := filepath.Join(root, "bin")
	writeStub(t, bin, "journalctl", "#!/bin/sh\nexit 0\n")
	writeStub(t, bin, "apt-get", "#!/bin/sh\nexit 0\n")
	if err := os.MkdirAll(filepath.Join(root, "var/cache/apt"), 0o755); err != nil {
		t.Fatal(err)
	}
	for name, stdin := range map[string]string{
		"letters":   "min_age_seconds=soon\n",
		"negative":  "min_age_seconds=-1\n",
		"extra key": "min_age_seconds=86400\nevil=1\n",
		"missing":   "",
	} {
		t.Run(name, func(t *testing.T) {
			out, err := runHostctlEnv(t, root, stdin, "cleanup-journal", extra)
			if code := exitCode(t, err); code != 2 {
				t.Fatalf("cleanup-journal exit = %d, want 2:\n%s", code, out)
			}
		})
	}
	out, err := runHostctlEnv(t, root, "evil=1\n", "cleanup-package-cache", extra)
	if code := exitCode(t, err); code != 2 {
		t.Fatalf("cleanup-package-cache exit = %d, want 2:\n%s", code, out)
	}
}

func TestHostctlCleanupReportsUnsupportedExit3(t *testing.T) {
	root := t.TempDir()
	empty := t.TempDir()
	for _, verb := range []string{"cleanup-journal", "cleanup-package-cache"} {
		out, err := runHostctlOnly(t, root, "", verb, empty)
		if code := exitCode(t, err); code != 3 {
			t.Fatalf("%s exit = %d, want 3:\n%s", verb, code, out)
		}
		if !strings.Contains(out, "unsupported") {
			t.Errorf("%s does not say unsupported:\n%s", verb, out)
		}
	}
}

func TestHostctlCleanupNeverUsesFreeFormArguments(t *testing.T) {
	root := t.TempDir()
	for _, verb := range []string{"cleanup-status", "cleanup-journal", "cleanup-package-cache"} {
		if _, err := runHostctl(t, root, "", verb+" now"); err == nil {
			t.Errorf("%s accepted a free-form argument", verb)
		}
	}
}

func TestHostctlDNSOnlyLeavesInterfaceAlone(t *testing.T) {
	root := t.TempDir()
	extra := stubProbes(t, root)
	netFile := filepath.Join(root, "etc/systemd/network/05-gotham.network")
	resFile := filepath.Join(root, "etc/systemd/resolved.conf.d/05-gotham.conf")
	dnsOnly := "dns=9.9.9.9\nipv4_mode=dhcp\nipv4_address=\nipv4_gateway=\n" +
		"ipv6_enabled=false\nipv6_mode=dhcp\nipv6_address=\nipv6_gateway=\nrevert_after=120\ndns_only=true\n"
	if out, err := runHostctlEnv(t, root, dnsOnly, "apply-network", extra); err != nil {
		t.Fatalf("dns-only apply failed: %v\n%s", err, out)
	}
	if _, err := os.Stat(netFile); !os.IsNotExist(err) {
		t.Fatal("dns-only apply must not create the interface file")
	}
	data, err := os.ReadFile(resFile)
	if err != nil || !strings.Contains(string(data), "DNS=9.9.9.9") {
		t.Fatalf("resolved drop-in: %v %s", err, data)
	}
	if _, err := runHostctlEnv(t, root, dnsOnly, "apply-network", extra); err == nil {
		t.Fatal("second apply while pending must fail")
	}
	out, err := runHostctlEnv(t, root, "", "revert-network", extra)
	if err != nil {
		t.Fatalf("revert failed: %v\n%s", err, out)
	}
	if _, err := os.Stat(netFile); !os.IsNotExist(err) {
		t.Fatal("revert must not create the interface file either")
	}
	if _, err := os.Stat(resFile); !os.IsNotExist(err) {
		t.Fatal("revert must remove a drop-in that did not exist before")
	}
	if strings.Contains(out, "reconfigure") {
		t.Errorf("dns-only revert must not reconfigure the interface:\n%s", out)
	}

	managed := "# Managed by Gotham (gotham-hostctl).\n[Match]\nName=eth0\n"
	if err := os.WriteFile(netFile, []byte(managed), 0o644); err != nil {
		t.Fatal(err)
	}
	if out, err := runHostctlEnv(t, root, dnsOnly, "apply-network", extra); err != nil {
		t.Fatalf("dns-only apply failed: %v\n%s", err, out)
	}
	if _, err := runHostctlEnv(t, root, "", "revert-network", extra); err != nil {
		t.Fatalf("revert failed: %v\n%s", err, out)
	}
	if data, err := os.ReadFile(netFile); err != nil || string(data) != managed {
		t.Fatalf("revert altered the interface file: %v %q", err, data)
	}
}
