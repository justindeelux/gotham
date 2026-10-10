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
	t.Helper()
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh not available")
	}
	cmd := exec.Command("sh", "gotham-hostctl.sh", verb)
	cmd.Env = append(os.Environ(), "GOTHAM_HOSTCTL_ROOT="+root, "GOTHAM_HOSTCTL_DRYRUN=1", "GOTHAM_HOSTCTL_IFACE=eth0")
	cmd.Stdin = strings.NewReader(stdin)
	out, err := cmd.CombinedOutput()
	return string(out), err
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
