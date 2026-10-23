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
	)
	cmd.Env = append(cmd.Env, extra...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func TestToolctlRefusesUnlistedTools(t *testing.T) {
	root := t.TempDir()
	for _, args := range [][]string{
		{"install-tool", "systemd-networkd"},
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
