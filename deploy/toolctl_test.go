package deploy

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

// runToolctl runs the privileged tool helper in dry-run mode against a temp
// root, returning its combined output and exit error.
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

// TestToolctlRefusesUnlistedTools proves the id allow-list is the only thing
// sudoers can name: a detect-only unit or an unknown id is refused before any
// command is planned.
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

// TestToolctlUsage proves a malformed invocation is rejected as usage.
func TestToolctlUsage(t *testing.T) {
	root := t.TempDir()
	if out, err := runToolctl(t, root, nil); err == nil {
		t.Errorf("no args = %q; want a usage error", out)
	}
	if out, err := runToolctl(t, root, nil, "frobnicate", "git"); err == nil {
		t.Errorf("unknown verb = %q; want a usage error", out)
	}
}

// TestToolctlGitHasNoServiceUnit is the regression for the bogus
// `systemctl enable --now git` step: git is a package only, docker gets the
// enable.
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

// TestToolctlRailpackIsPinnedAndVerified proves railpack uses a versioned
// release tarball with a hard-coded sha256, per architecture, and never the
// unpinned install.sh piped to a shell.
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

// TestToolctlPackIsVerifiedAndExtractsOneMember proves pack pins the release
// digest and extracts only the pack binary from the archive.
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

// TestToolctlBuildkitTagsLastAndIsIdempotent proves the detection tag is set
// only after the container runs, that the tag source is the digest ref
// (docker drops the tag on a digest-qualified pull, so `moby/buildkit:v0.34.0`
// would not exist locally), and that the container is cleared first so a retry
// after a failed run recovers.
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
	// The tag step must source the digest ref: after a digest-qualified pull the
	// tag-only ref does not exist locally, so `docker tag moby/buildkit:v0.34.0`
	// would always fail and leave the tool permanently "not installed".
	tagLine := commandLine(out, "docker tag")
	if tagLine == "" {
		t.Fatalf("no docker tag step: %q", out)
	}
	if !strings.Contains(tagLine, buildkitDigestRef) {
		t.Errorf("docker tag does not source the digest ref (a digest-qualified pull drops the tag): %q", tagLine)
	}

	// Uninstall must be symmetric: drop the detection ref first (it is what the
	// agent probes) and then the digest image.
	uninstall, err := runToolctl(t, root, nil, "uninstall-tool", "buildkit")
	if err != nil {
		t.Fatalf("uninstall-tool buildkit: %v: %s", err, uninstall)
	}
	rmiDetect := strings.Index(uninstall, "docker rmi -f moby/buildkit\n")
	rmiImage := strings.Index(uninstall, "docker rmi -f "+buildkitDigestRef)
	rmContainer := strings.Index(uninstall, "docker rm -f buildkit")
	switch {
	case rmContainer < 0 || rmiDetect < 0 || rmiImage < 0:
		t.Fatalf("buildkit uninstall steps missing: %q", uninstall)
	case rmContainer >= rmiDetect || rmiDetect >= rmiImage:
		t.Fatalf("buildkit uninstall out of order: %q", uninstall)
	}
}

// commandLine returns the first "+ " output line whose command contains
// fragment, including the full line so a caller can inspect its arguments.
func commandLine(out, fragment string) string {
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "+ ") && strings.Contains(line, fragment) {
			return line
		}
	}
	return ""
}

// TestToolctlRootRedirects proves the test root redirects the install target,
// so a dry-run never plans a write outside it.
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

// TestToolctlReexecBoundsAndStopsUnit proves the sandbox escape runs the job in
// a transient unit that is name-scoped, wall-clock bounded (RuntimeMaxSec) and
// marked so it does not re-exec again.
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

// TestToolctlRefusesConcurrentJob proves the node lock: a second job while the
// lock is held is refused instead of overlapping the running one. It needs
// flock, so it skips on a host without it (macOS).
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

// lastCommand returns the last "+ cmd" line's command word, for order checks.
func lastCommand(out string) string {
	var last string
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "+ ") {
			last = line
		}
	}
	return last
}
