package deploy

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func runToolctl(t *testing.T, root string, extra []string, args ...string) (string, error) {
	t.Helper()
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh not available")
	}
	cmd := exec.Command("sh", append([]string{"gotham-toolctl.sh"}, args...)...)
	cmd.Env = append(os.Environ(),
		"GOTHAM_TOOLCTL_ROOT="+root,
		"GOTHAM_TOOLCTL_DRYRUN=1",
		"GOTHAM_TOOLCTL_ARCH=amd64",
		"GOTHAM_TOOLCTL_DISTRO=apt",
		// Pin the host's network manager so the test does not depend on the
		// machine running it.
		"GOTHAM_TOOLCTL_NETWORK_MANAGER=none",
	)
	cmd.Env = append(cmd.Env, extra...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// aptCacheStub gives the helper an apt-cache with a fixed answer about the
// standalone systemd network packages. The helper pins its PATH for hardening,
// so this goes through its test seam.
func aptCacheStub(t *testing.T, found bool) []string {
	t.Helper()
	status := "1"
	if found {
		status = "0"
	}
	path := filepath.Join(t.TempDir(), "apt-cache")
	if err := os.WriteFile(path, []byte("#!/bin/sh\nexit "+status+"\n"), 0o755); err != nil {
		t.Fatalf("write fake apt-cache: %v", err)
	}
	return []string{"GOTHAM_TOOLCTL_APT_CACHE=" + path}
}

// aptCacheListsStub gives the helper an apt-cache that finds the packages only
// once the returned lists file exists. It stands for the package lists a minimal
// image does not have until `apt-get update` refreshes them.
func aptCacheListsStub(t *testing.T) ([]string, string) {
	t.Helper()
	lists := filepath.Join(t.TempDir(), "lists")
	path := filepath.Join(t.TempDir(), "apt-cache")
	body := "#!/bin/sh\n[ -f \"$GOTHAM_TEST_APT_LISTS\" ] || exit 1\nexit 0\n"
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatalf("write fake apt-cache: %v", err)
	}
	return []string{
		"GOTHAM_TOOLCTL_APT_CACHE=" + path,
		"GOTHAM_TEST_APT_LISTS=" + lists,
	}, lists
}

func TestToolctlRefusesUnlistedTools(t *testing.T) {
	root := t.TempDir()
	for _, args := range [][]string{
		{"install-tool", "networkctl"},
		{"install-tool", "nope"},
		{"uninstall-tool", "docker"},
		{"uninstall-tool", "systemctl"},
		{"install-tool", "railpack extra"},
	} {
		out, err := runToolctl(t, root, nil, args...)
		if err == nil {
			t.Errorf("%v = %q; want a refusal", args, out)
		}
		if strings.Contains(out, "+ ") {
			t.Errorf("%v planned a command: %q", args, out)
		}
	}
}

func TestToolctlUsage(t *testing.T) {
	root := t.TempDir()
	if out, err := runToolctl(t, root, nil); err == nil {
		t.Errorf("no args = %q; want a usage error", out)
	}
	if out, err := runToolctl(t, root, nil, "frobnicate", "git"); err == nil {
		t.Errorf("unknown verb = %q; want a usage error", out)
	}
}

func TestToolctlRefusesPlatformToolUninstall(t *testing.T) {
	for _, id := range []string{"git", "docker", "gotham-hostctl"} {
		out, err := runToolctl(t, t.TempDir(), nil, "uninstall-tool", id)
		if err == nil || !strings.Contains(out, "required by the platform") || strings.Contains(out, "+ ") {
			t.Errorf("uninstall %s = %q, %v; want refusal without executing commands", id, out, err)
		}
	}
}

func TestToolctlHostctlInstall(t *testing.T) {
	missing, err := runToolctl(t, t.TempDir(), nil, "install-tool", "gotham-hostctl")
	if err == nil || !strings.Contains(missing, "payload is missing") {
		t.Fatalf("missing payload = %q, %v; want refusal", missing, err)
	}

	root := t.TempDir()
	payload := filepath.Join(root, "usr/libexec/gotham/hostbootstrap")
	if err := os.MkdirAll(payload, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"gotham-hostctl", "gotham-hostctl.sudoers"} {
		if err := os.WriteFile(filepath.Join(payload, name), []byte("x"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	out, err := runToolctl(t, root, nil, "install-tool", "gotham-hostctl")
	if err != nil {
		t.Fatalf("install-tool gotham-hostctl: %v: %s", err, out)
	}
	if !strings.Contains(out, "hostbootstrap/gotham-hostctl") ||
		!strings.Contains(out, "gotham-hostctl.sudoers") ||
		!strings.Contains(out, "visudo -cf") {
		t.Errorf("host helper install did not stage, validate and install: %q", out)
	}
}

func TestToolctlGitHasNoServiceUnit(t *testing.T) {
	root := t.TempDir()
	out, err := runToolctl(t, root, nil, "install-tool", "git")
	if err != nil {
		t.Fatalf("install-tool git: %v: %s", err, out)
	}
	if strings.Contains(out, "systemctl enable") {
		t.Errorf("git install enables a service unit: %q", out)
	}
	if !strings.Contains(out, "apt-get install -y git") {
		t.Errorf("git install did not install the package: %q", out)
	}

	out, err = runToolctl(t, root, nil, "install-tool", "docker")
	if err != nil {
		t.Fatalf("install-tool docker: %v: %s", err, out)
	}
	if !strings.Contains(out, "systemctl enable --now docker") {
		t.Errorf("docker install does not enable its unit: %q", out)
	}
	if !strings.Contains(out, "DEBIAN_FRONTEND=noninteractive") {
		t.Errorf("docker install is not non-interactive for apt: %q", out)
	}
}

func TestToolctlGitPackageUpgradeCommands(t *testing.T) {
	for distro, command := range map[string]string{
		"apt": "apt-get install -y git",
		"dnf": "dnf upgrade -y git",
		"apk": "apk add --no-cache --upgrade git",
	} {
		out, err := runToolctl(t, t.TempDir(), []string{"GOTHAM_TOOLCTL_DISTRO=" + distro}, "install-tool", "git")
		if err != nil || !strings.Contains(out, command) {
			t.Errorf("git install/update on %s = %q, %v; want %q", distro, out, err, command)
		}
	}
}

func TestToolctlInstallsSystemdNetworkStack(t *testing.T) {
	t.Run("units inside systemd", func(t *testing.T) {
		// Ubuntu 22.04 and older Debian ship no systemd-networkd or
		// systemd-resolved package: apt has neither, and the units belong to
		// `systemd`, so the install must not name a package.
		root := t.TempDir()
		noPackages := aptCacheStub(t, false)
		for id, unit := range map[string]string{
			"systemd-networkd": "systemd-networkd",
			"systemd-resolved": "systemd-resolved",
			"resolvectl":       "systemd-resolved",
		} {
			out, err := runToolctl(t, root, noPackages, "install-tool", id)
			if err != nil {
				t.Fatalf("install-tool %s: %v: %s", id, err, out)
			}
			if !strings.Contains(out, "apt-get update -qq") {
				t.Errorf("%s does not refresh the package lists: %q", id, out)
			}
			if strings.Contains(out, "apt-get install") {
				t.Errorf("%s installs a package apt does not offer: %q", id, out)
			}
			if !strings.Contains(out, "systemctl enable "+unit) ||
				!strings.Contains(out, "systemctl restart "+unit) {
				t.Errorf("%s does not enable and restart %s: %q", id, unit, out)
			}
		}
	})

	t.Run("standalone packages", func(t *testing.T) {
		root := t.TempDir()
		knownPackages := aptCacheStub(t, true)
		for id, pkg := range map[string]string{
			"systemd-networkd": "systemd-networkd",
			"systemd-resolved": "systemd-resolved",
			"resolvectl":       "systemd-resolved",
		} {
			out, err := runToolctl(t, root, knownPackages, "install-tool", id)
			if err != nil {
				t.Fatalf("install-tool %s: %v: %s", id, err, out)
			}
			if !strings.Contains(out, "DEBIAN_FRONTEND=noninteractive apt-get install -y "+pkg) {
				t.Errorf("%s does not install %s: %q", id, pkg, out)
			}
			if !strings.Contains(out, "systemctl enable ") ||
				!strings.Contains(out, "systemctl restart ") {
				t.Errorf("%s does not enable and restart its unit: %q", id, out)
			}
		}
	})

	t.Run("refreshes empty lists before the query", func(t *testing.T) {
		// A fresh minimal image (Debian 12 cloud) has no package lists, so the
		// query would miss a split package the node really needs. The refresh
		// must run first and the install follow it.
		extra, lists := aptCacheListsStub(t)
		root := t.TempDir()

		empty, err := runToolctl(t, root, extra, "install-tool", "systemd-resolved")
		if err != nil {
			t.Fatalf("install-tool systemd-resolved: %v: %s", err, empty)
		}
		if strings.Contains(empty, "apt-get install") {
			t.Errorf("empty lists install a package apt cannot see yet: %q", empty)
		}
		if !strings.Contains(empty, "systemctl restart systemd-resolved") {
			t.Errorf("empty lists skip the unit: %q", empty)
		}

		if err := os.WriteFile(lists, nil, 0o644); err != nil {
			t.Fatalf("create lists: %v", err)
		}
		out, err := runToolctl(t, root, extra, "install-tool", "systemd-resolved")
		if err != nil {
			t.Fatalf("install-tool systemd-resolved: %v: %s", err, out)
		}
		update := strings.Index(out, "apt-get update -qq")
		query := strings.Index(out, "apt-cache show systemd-resolved")
		install := strings.Index(out, "apt-get install -y systemd-resolved")
		if update < 0 || query < 0 || install < 0 {
			t.Fatalf("refresh, query or install step missing: %q", out)
		}
		if update > query || query > install {
			t.Errorf("want update before query before install, got %q", out)
		}
	})

	t.Run("fedora", func(t *testing.T) {
		root := t.TempDir()
		for id, want := range map[string]string{
			"systemd-networkd": "dnf install -y systemd-networkd",
			"systemd-resolved": "dnf install -y systemd-resolved",
			"resolvectl":       "dnf install -y systemd-resolved",
		} {
			out, err := runToolctl(t, root, []string{"GOTHAM_TOOLCTL_DISTRO=dnf"}, "install-tool", id)
			if err != nil {
				t.Fatalf("install-tool %s: %v: %s", id, err, out)
			}
			if !strings.Contains(out, want) {
				t.Errorf("%s install on dnf = %q; want %q", id, out, want)
			}
		}
	})

	t.Run("claims the D-Bus name", func(t *testing.T) {
		// A dbus-daemon started before the package added its policy keeps the
		// name unowned, so resolvectl and networkctl wait 25s. The install must
		// reload dbus, then restart the unit, then prove it answers.
		root := t.TempDir()
		for _, tc := range []struct{ id, unit, probe string }{
			{"systemd-networkd", "systemd-networkd", "timeout 10 networkctl status"},
			{"systemd-resolved", "systemd-resolved", "timeout 10 resolvectl status"},
			{"resolvectl", "systemd-resolved", "timeout 10 resolvectl status"},
		} {
			out, err := runToolctl(t, root, nil, "install-tool", tc.id)
			if err != nil {
				t.Fatalf("install-tool %s: %v: %s", tc.id, err, out)
			}
			reload := strings.Index(out, "systemctl reload dbus")
			enable := strings.Index(out, "systemctl enable "+tc.unit)
			restart := strings.Index(out, "systemctl restart "+tc.unit)
			probe := strings.Index(out, tc.probe)
			switch {
			case reload < 0 || enable < 0 || restart < 0 || probe < 0:
				t.Fatalf("%s misses reload, enable, restart or the bounded probe: %q", tc.id, out)
			case reload >= enable || enable >= restart || restart >= probe:
				t.Errorf("%s steps out of order (reload=%d enable=%d restart=%d probe=%d): %q",
					tc.id, reload, enable, restart, probe, out)
			}
		}
	})

	t.Run("unsupported distribution", func(t *testing.T) {
		out, err := runToolctl(t, t.TempDir(), []string{"GOTHAM_TOOLCTL_DISTRO=apk"}, "install-tool", "systemd-resolved")
		if err == nil || !strings.Contains(out, "unsupported distribution") {
			t.Fatalf("install on apk = %q, %v; want an unsupported-distribution refusal", out, err)
		}
		if strings.Contains(out, "+ ") {
			t.Errorf("an unsupported distribution still planned a command: %q", out)
		}
	})
}

// writeResolvConf plants the static /etc/resolv.conf a fresh Debian node has
// before systemd-resolved replaces it with the 127.0.0.53 stub.
func writeResolvConf(t *testing.T, root, body string) {
	t.Helper()
	etc := filepath.Join(root, "etc")
	if err := os.MkdirAll(etc, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(etc, "resolv.conf"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestToolctlCarriesResolvConfNameservers(t *testing.T) {
	root := t.TempDir()
	writeResolvConf(t, root, "nameserver 127.0.0.1\nnameserver 9.9.9.9\nnameserver ::1\nnameserver 2001:4860:4860::8888\n")
	out, err := runToolctl(t, root, aptCacheStub(t, true), "install-tool", "systemd-resolved")
	if err != nil {
		t.Fatalf("install-tool systemd-resolved: %v: %s", err, out)
	}
	carry := strings.Index(out, "DNS=9.9.9.9 2001:4860:4860::8888")
	install := strings.Index(out, "apt-get install -y systemd-resolved")
	if carry < 0 || install < 0 {
		t.Fatalf("carry-over or package install missing: %q", out)
	}
	if carry > install {
		t.Errorf("resolv.conf nameservers must be captured before the package replaces it: %q", out)
	}
}

func TestToolctlResolvConfFallback(t *testing.T) {
	root := t.TempDir()
	writeResolvConf(t, root, "nameserver 127.0.0.1\nnameserver ::1\n")
	out, err := runToolctl(t, root, nil, "install-tool", "systemd-resolved")
	if err != nil {
		t.Fatalf("install-tool systemd-resolved: %v: %s", err, out)
	}
	if !strings.Contains(out, "no usable nameserver") {
		t.Errorf("the fallback is silent: %q", out)
	}
	if !strings.Contains(out, "DNS=1.1.1.1 8.8.8.8") {
		t.Errorf("loopback-only resolv.conf does not fall back to public resolvers: %q", out)
	}
}

func TestToolctlKeepsExistingResolvedDropIn(t *testing.T) {
	root := t.TempDir()
	writeResolvConf(t, root, "nameserver 9.9.9.9\n")
	dropin := filepath.Join(root, "etc/systemd/resolved.conf.d/gotham-dns.conf")
	if err := os.MkdirAll(filepath.Dir(dropin), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dropin, []byte("[Resolve]\nDNS=10.0.0.53\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := runToolctl(t, root, nil, "install-tool", "systemd-resolved")
	if err != nil {
		t.Fatalf("install-tool systemd-resolved: %v: %s", err, out)
	}
	if strings.Contains(out, "DNS=") {
		t.Errorf("an existing DNS= drop-in is overwritten: %q", out)
	}
}

func TestToolctlSkipsResolvedStubSymlink(t *testing.T) {
	root := t.TempDir()
	etc := filepath.Join(root, "etc")
	if err := os.MkdirAll(etc, 0o755); err != nil {
		t.Fatal(err)
	}
	stub := filepath.Join(root, "run/systemd/resolve/stub-resolv.conf")
	if err := os.MkdirAll(filepath.Dir(stub), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(stub, []byte("nameserver 127.0.0.53\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(stub, filepath.Join(etc, "resolv.conf")); err != nil {
		t.Skipf("symlinks are not available: %v", err)
	}
	out, err := runToolctl(t, root, nil, "install-tool", "systemd-resolved")
	if err != nil {
		t.Fatalf("install-tool systemd-resolved: %v: %s", err, out)
	}
	if strings.Contains(out, "write ") {
		t.Errorf("a resolv.conf already pointing at the resolved stub is rewritten: %q", out)
	}
}

func TestToolctlWritesResolvedDropIn(t *testing.T) {
	root := t.TempDir()
	writeResolvConf(t, root, "nameserver 127.0.0.1\nnameserver 192.0.2.53\n")
	cmd := exec.Command("sh", "-c", `
systemctl() { return 0; }
timeout() { shift; "$@"; }
resolvectl() { return 0; }
networkctl() { return 0; }
flock() { return 0; }
. ./gotham-toolctl.sh
`, "toolctl", "install-tool", "systemd-resolved")
	cmd.Env = append(os.Environ(),
		"GOTHAM_TOOLCTL_INNER=1",
		"GOTHAM_TOOLCTL_DRYRUN=0",
		"GOTHAM_TOOLCTL_ROOT="+root,
		"GOTHAM_TOOLCTL_LOCK="+filepath.Join(root, "toolctl.lock"),
		"GOTHAM_TOOLCTL_ARCH=amd64",
		"GOTHAM_TOOLCTL_DISTRO=apt",
		"GOTHAM_TOOLCTL_NETWORK_MANAGER=none",
	)
	cmd.Env = append(cmd.Env, aptCacheStub(t, false)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("install-tool systemd-resolved: %v: %s", err, out)
	}
	data, err := os.ReadFile(filepath.Join(root, "etc/systemd/resolved.conf.d/gotham-dns.conf"))
	if err != nil {
		t.Fatalf("resolved drop-in not written: %v: %s", err, out)
	}
	for _, want := range []string{"[Resolve]", "DNS=192.0.2.53"} {
		if !strings.Contains(string(data), want) {
			t.Errorf("resolved drop-in misses %q:\n%s", want, data)
		}
	}
}

func TestToolctlAptUpdateFailureIsReported(t *testing.T) {
	if _, err := exec.LookPath("flock"); err != nil {
		t.Skip("flock is not available on this host")
	}
	// An unreachable mirror: the refresh fails, the helper says why it continues,
	// and the step after it still runs.
	cmd := exec.Command("sh", "-c", `
env() { return 100; }
. ./gotham-toolctl.sh
`, "toolctl", "install-tool", "systemd-resolved")
	cmd.Env = append(os.Environ(),
		"GOTHAM_TOOLCTL_INNER=1",
		"GOTHAM_TOOLCTL_DRYRUN=0",
		"GOTHAM_TOOLCTL_ROOT="+t.TempDir(),
		"GOTHAM_TOOLCTL_LOCK="+filepath.Join(t.TempDir(), "lock"),
		"GOTHAM_TOOLCTL_ARCH=amd64",
		"GOTHAM_TOOLCTL_DISTRO=apt",
		"GOTHAM_TOOLCTL_NETWORK_MANAGER=none",
	)
	cmd.Env = append(cmd.Env, aptCacheStub(t, false)...)
	out, _ := cmd.CombinedOutput()
	if !strings.Contains(string(out), "gotham-toolctl: apt-get update failed") {
		t.Fatalf("a failed refresh is silent: %s", out)
	}
}

func TestToolctlRefusesNetworkManagerHosts(t *testing.T) {
	for _, id := range []string{"systemd-networkd", "systemd-resolved", "resolvectl"} {
		out, err := runToolctl(t, t.TempDir(), []string{"GOTHAM_TOOLCTL_NETWORK_MANAGER=networkmanager"}, "install-tool", id)
		if err == nil || !strings.Contains(out, "NetworkManager") || !strings.Contains(out, "Nothing was changed") {
			t.Errorf("install %s on a NetworkManager host = %q, %v; want a refusal", id, out, err)
		}
		if strings.Contains(out, "+ ") {
			t.Errorf("a refused network install still planned a command: %q", out)
		}
	}
}

func TestToolctlRefusesNetplanNetworkManagerRenderer(t *testing.T) {
	root := t.TempDir()
	netplan := filepath.Join(root, "etc/netplan")
	if err := os.MkdirAll(netplan, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(netplan, "50-cloud-init.yaml"),
		[]byte("network:\n  version: 2\n  renderer: NetworkManager\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("sh", "-c", `
systemctl() { return 3; }
. ./gotham-toolctl.sh
`, "toolctl", "install-tool", "systemd-resolved")
	cmd.Env = append(os.Environ(),
		"GOTHAM_TOOLCTL_ROOT="+root,
		"GOTHAM_TOOLCTL_DRYRUN=1",
		"GOTHAM_TOOLCTL_ARCH=amd64",
		"GOTHAM_TOOLCTL_DISTRO=apt",
	)
	out, err := cmd.CombinedOutput()
	if err == nil || !strings.Contains(string(out), "NetworkManager") {
		t.Fatalf("netplan NetworkManager renderer = %q, %v; want a refusal", out, err)
	}
	if strings.Contains(string(out), "+ ") {
		t.Errorf("a refused network install still planned a command: %s", out)
	}
}

func TestToolctlRailpackIsPinnedAndVerified(t *testing.T) {
	root := t.TempDir()
	out, err := runToolctl(t, root, nil, "install-tool", "railpack")
	if err != nil {
		t.Fatalf("install-tool railpack: %v: %s", err, out)
	}
	if strings.Contains(out, "railpack.com/install.sh") {
		t.Errorf("railpack install still fetches install.sh: %q", out)
	}
	if !strings.Contains(out, "railpack-v0.42.0-x86_64-unknown-linux-musl.tar.gz") {
		t.Errorf("railpack amd64 asset missing: %q", out)
	}
	if !strings.Contains(out, "81bca91d0577c7650378762609116d12d8630e2cfc1779e41d3ac3964fc7194b") {
		t.Errorf("railpack amd64 sha256 missing: %q", out)
	}
	if !strings.Contains(out, "tar -xzf") || !strings.Contains(out, "railpack") {
		t.Errorf("railpack tarball is not extracted: %q", out)
	}

	out, err = runToolctl(t, root, []string{"GOTHAM_TOOLCTL_ARCH=arm64"}, "install-tool", "railpack")
	if err != nil {
		t.Fatalf("install-tool railpack arm64: %v: %s", err, out)
	}
	if !strings.Contains(out, "railpack-v0.42.0-arm64-unknown-linux-musl.tar.gz") ||
		!strings.Contains(out, "63c0417d6c0a437162fa2c688ddf58aabf9e4433eeffb69d8c3fb498603e2c3b") {
		t.Errorf("railpack arm64 asset/digest missing: %q", out)
	}
}

func TestToolctlPackIsVerifiedAndExtractsOneMember(t *testing.T) {
	root := t.TempDir()
	out, err := runToolctl(t, root, nil, "install-tool", "pack")
	if err != nil {
		t.Fatalf("install-tool pack: %v: %s", err, out)
	}
	if !strings.Contains(out, "pack-v0.40.9-linux.tgz") {
		t.Errorf("pack amd64 asset missing: %q", out)
	}
	if !strings.Contains(out, "dc0ee1e931cf8a106d7555a01a214864f9acb60b77adf15d69b74df4404758e9") {
		t.Errorf("pack amd64 sha256 missing: %q", out)
	}
	if !strings.Contains(out, "+ tar -xzf") || !strings.HasSuffix(lastCommand(out), "pack") {
		t.Errorf("pack tarball is not extracted to one member: %q", out)
	}
}

func TestToolctlBuildkitTagsLastAndIsIdempotent(t *testing.T) {
	root := t.TempDir()
	out, err := runToolctl(t, root, nil, "install-tool", "buildkit")
	if err != nil {
		t.Fatalf("install-tool buildkit: %v: %s", err, out)
	}
	pull := strings.Index(out, "docker pull")
	rm := strings.Index(out, "docker rm -f buildkit")
	run := strings.Index(out, "docker run -d --privileged")
	tag := strings.Index(out, "docker tag")
	switch {
	case pull < 0 || rm < 0 || run < 0 || tag < 0:
		t.Fatalf("buildkit steps missing: %q", out)
	case pull >= rm || rm >= run || run >= tag:
		t.Fatalf("buildkit steps out of order (pull=%d rm=%d run=%d tag=%d): %q", pull, rm, run, tag, out)
	}
	const buildkitTagOnly = "moby/buildkit:v0.34.0"
	const buildkitDigestRef = buildkitTagOnly + "@sha256:b059f8d7226d0b326bc871af489a5dddf65e36af4d20d14251b072974d9ee87c"
	if !strings.Contains(out, buildkitDigestRef) {
		t.Errorf("buildkit image is not digest-pinned: %q", out)
	}
	tagLine := commandLine(out, "docker tag")
	if tagLine == "" {
		t.Fatalf("no docker tag step: %q", out)
	}
	if !strings.Contains(tagLine, buildkitDigestRef) {
		t.Errorf("docker tag does not source the digest ref (a digest-qualified pull drops the tag): %q", tagLine)
	}

	uninstall, err := runToolctl(t, root, nil, "uninstall-tool", "buildkit")
	if err != nil {
		t.Fatalf("uninstall-tool buildkit: %v: %s", err, uninstall)
	}
	rmiDetect := strings.Index(uninstall, "docker rmi -f moby/buildkit:latest\n")
	rmiImage := strings.Index(uninstall, "docker rmi -f moby/buildkit@"+strings.SplitN(buildkitDigestRef, "@", 2)[1])
	rmContainer := strings.Index(uninstall, "docker rm -f buildkit")
	switch {
	case rmContainer < 0 || rmiDetect < 0 || rmiImage < 0:
		t.Fatalf("buildkit uninstall steps missing: %q", uninstall)
	case rmContainer >= rmiDetect || rmiDetect >= rmiImage:
		t.Fatalf("buildkit uninstall out of order: %q", uninstall)
	}
}

func TestToolctlBuildkitUninstallMissingImageIsQuiet(t *testing.T) {
	for _, mode := range []string{"full-install", "daemon-error"} {
		t.Run(mode, func(t *testing.T) {
			dir := t.TempDir()
			record := filepath.Join(dir, "docker-argv")
			cmd := exec.Command("sh", "-c", `
flock() { return 0; }
docker() {
    printf '%s\n' "$*" >> "$FAKE_RECORD"
    [ "$1" = rmi ] || return 0
    if [ "$FAKE_MODE" = daemon-error ]; then
        echo 'Cannot connect to the Docker daemon' >&2; return 1
    fi
    # A full install: the detection tag exists and removing it also drops the
    # digest ref, so the second rmi gets the exit-0 "No such image" report.
    case "$3" in
        *@*) echo "Error response from daemon: No such image: $3" >&2 ;;
        *) echo "Untagged: $3"; echo "Deleted: sha256:deadbeef" ;;
    esac
}
. ./gotham-toolctl.sh
`, "toolctl", "uninstall-tool", "buildkit")
			cmd.Env = append(os.Environ(),
				"GOTHAM_TOOLCTL_INNER=1",
				"GOTHAM_TOOLCTL_DRYRUN=0",
				"GOTHAM_TOOLCTL_LOCK="+filepath.Join(dir, "lock"),
				"GOTHAM_TOOLCTL_ARCH=amd64",
				"GOTHAM_TOOLCTL_DISTRO=apt",
				"FAKE_RECORD="+record,
				"FAKE_MODE="+mode,
			)
			out, err := cmd.CombinedOutput()
			if mode == "daemon-error" {
				if err == nil || !strings.Contains(string(out), "Cannot connect") {
					t.Fatalf("daemon failure must remain visible and fatal: %v: %s", err, out)
				}
				return
			}
			if err != nil {
				t.Fatalf("full uninstall must succeed: %v: %s", err, out)
			}
			for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
				if strings.Contains(line, "Error") || strings.Contains(line, "No such image") {
					t.Fatalf("full uninstall log carries an error line %q: %s", line, out)
				}
			}
			if !strings.Contains(string(out), "Untagged: moby/buildkit:latest") {
				t.Fatalf("a real removal must stay visible: %s", out)
			}
			argv, err := os.ReadFile(record)
			if err != nil {
				t.Fatal(err)
			}
			want := "rm -f buildkit\nrmi -f moby/buildkit:latest\n" +
				"rmi -f moby/buildkit@sha256:b059f8d7226d0b326bc871af489a5dddf65e36af4d20d14251b072974d9ee87c\n"
			if string(argv) != want {
				t.Fatalf("removal refs = %q; want %q", argv, want)
			}
		})
	}
}

func commandLine(out, fragment string) string {
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "+ ") && strings.Contains(line, fragment) {
			return line
		}
	}
	return ""
}

func TestToolctlRootRedirects(t *testing.T) {
	root := t.TempDir()
	out, err := runToolctl(t, root, nil, "install-tool", "pack")
	if err != nil {
		t.Fatalf("install-tool pack: %v: %s", err, out)
	}
	want := filepath.Join(root, "usr", "local", "bin", "pack")
	if !strings.Contains(out, want) {
		t.Errorf("install target is not under the root %s: %q", root, out)
	}
}

func TestToolctlReexecBoundsAndStopsUnit(t *testing.T) {
	dir := t.TempDir()
	record := filepath.Join(dir, "argv")
	fake := filepath.Join(dir, "fake-systemd-run")
	body := "#!/bin/sh\nprintf '%s\\n' \"$@\" > \"$FAKE_RECORD\"\nexit 0\n"
	if err := os.WriteFile(fake, []byte(body), 0o755); err != nil {
		t.Fatalf("write fake systemd-run: %v", err)
	}
	self, err := filepath.Abs("gotham-toolctl.sh")
	if err != nil {
		t.Fatalf("abs path: %v", err)
	}

	cmd := exec.Command("sh", self, "install-tool", "buildkit")
	cmd.Env = append(os.Environ(),
		"GOTHAM_TOOLCTL_SYSTEMD_RUN="+fake,
		"FAKE_RECORD="+record,
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("helper re-exec: %v: %s", err, out)
	}
	recorded, err := os.ReadFile(record)
	if err != nil {
		t.Fatalf("fake systemd-run did not run: %v", err)
	}
	got := string(recorded)
	for _, want := range []string{
		"--unit=gotham-toolctl-install-tool-buildkit-",
		"--property=RuntimeMaxSec=840",
		"--setenv=GOTHAM_TOOLCTL_INNER=1",
		self,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("systemd-run argv missing %q: %q", want, got)
		}
	}
}

func TestToolctlRefusesConcurrentJob(t *testing.T) {
	if _, err := exec.LookPath("flock"); err != nil {
		t.Skip("flock is not available on this host")
	}
	root := t.TempDir()
	lock := filepath.Join(root, "toolctl.lock")
	file, err := os.OpenFile(lock, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		t.Fatalf("open lock: %v", err)
	}
	defer func() { _ = file.Close() }()
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		t.Fatalf("hold lock: %v", err)
	}

	out, err := runToolctl(t, root, []string{"GOTHAM_TOOLCTL_LOCK=" + lock}, "install-tool", "pack")
	if err == nil {
		t.Fatalf("install with the lock held = %q; want a busy refusal", out)
	}
	if !strings.Contains(out, "already running") {
		t.Errorf("busy refusal did not name the running job: %q", out)
	}
	if strings.Contains(out, "+ ") {
		t.Errorf("a locked-out job still planned a command: %q", out)
	}
}

func lastCommand(out string) string {
	var last string
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "+ ") {
			last = line
		}
	}
	return last
}
