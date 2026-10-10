package deploy

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// runHostctl executes the helper in dry-run mode against a temp root.
func runHostctl(t *testing.T, root, stdin string, verb string) (string, error) {
	return runHostctlEnv(t, root, stdin, verb, nil)
}

// runHostctlEnv is runHostctl with extra environment entries (e.g. a PATH
// stub dir for the `ip`/`resolvectl` probes).
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

// stubProbes writes fake `ip`/`resolvectl` binaries reporting a static host
// (JUS-100: netplan-owned 103.176.22.225/24 via 103.176.22.1; JUS-101: the
// netplan file lives in /run like the real generator and carries per-link
// DNS 8.8.8.8 8.8.4.4) and returns a PATH entry for them.
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
	for name, body := range map[string]string{"ip": ip, "resolvectl": resolvectl} {
		if err := os.WriteFile(filepath.Join(bin, name), []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	netplan := "[Match]\nName=eth0\n\n[Network]\nDHCP=no\nAddress=103.176.22.225/24\nDNS=8.8.8.8 8.8.4.4\n\n[Route]\nGateway=103.176.22.1\n"
	if err := os.WriteFile(filepath.Join(root, "run/systemd/network/10-netplan-eth0.network"), []byte(netplan), 0o644); err != nil {
		t.Fatal(err)
	}
	return []string{"PATH=" + bin + ":" + os.Getenv("PATH")}
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
	} {
		if !strings.Contains(out, want) {
			t.Errorf("status misses %s:\n%s", want, out)
		}
	}
}

// TestHostctlDNSOnlyOverridesLinkDNS is the JUS-101 regression test: on a
// netplan-style host with per-link DNS the DNS-only apply must set the link
// DNS too (global drop-in + DNS-only networkd drop-in + runtime resolvectl),
// without touching Address/Gateway/routes, and revert must restore the link
// and global DNS exactly.
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
	if strings.Contains(out, "reconfigure") {
		t.Errorf("dns-only revert must not reconfigure the interface:\n%s", out)
	}

	// A pre-existing link drop-in is restored byte for byte.
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

	// Clearing the servers removes the link drop-in and reverts the runtime.
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

// TestHostctlDNSOnlyLeavesInterfaceAlone is the JUS-100 regression test: a
// resolver-only apply on a static host must write only the resolved drop-in
// and never touch the interface file, even when the request carries DHCP
// values (the old form defaults).
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

	// With a pre-existing interface file the DNS-only revert keeps it intact.
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
