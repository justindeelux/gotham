package toolcatalog

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// Installer errors. The agent maps them onto gRPC codes: an unknown tool is
// InvalidArgument and everything else is FailedPrecondition, so a request the
// node will never satisfy is refused before any command runs.
var (
	// ErrUnknownTool reports an id that is not a catalog entry.
	ErrUnknownTool = errors.New("toolcatalog: unknown tool")
	// ErrNotInstallable reports a detect-only tool (a systemd unit) or one the
	// platform does not manage. Installing it is refused.
	ErrNotInstallable = errors.New("toolcatalog: tool is not installable")
	// ErrNotUninstallable reports a tool the platform keeps (docker) or one it
	// never removes (systemd units, systemctl). Removing it is refused.
	ErrNotUninstallable = errors.New("toolcatalog: tool is not uninstallable")
	// ErrNoInstaller reports a catalog entry that declares itself installable
	// but has no installer registered. It is a programming error surfaced as a
	// refusal, never a silent success.
	ErrNoInstaller = errors.New("toolcatalog: no installer registered")
)

// Distro names the host's package manager family. It selects the argv the
// docker and git installers run; an unknown distro refuses those installs
// rather than guessing a command that would not exist.
type Distro string

// Supported package manager families.
const (
	DistroApt Distro = "apt"
	DistroDNF Distro = "dnf"
	DistroAPK Distro = "apk"
)

// defaultBinDir is where a host binary installer puts a tool. It is the first
// directory on the systemd unit's default PATH, so an installed tool is found
// by the agent and by the toolchain process it starts.
const defaultBinDir = "/usr/local/bin"

// Pinned install material. Bumping a version is a deliberate change: the pin is
// what makes an install reproducible, and it matches the version the catalog
// detects.
const (
	railpackVersion      = "0.42.0"
	railpackInstallerURL = "https://railpack.com/install.sh"
	buildkitImage        = "moby/buildkit:v0.34.0"
	// buildkitDetectRef is the untagged reference the catalog's detection probe
	// inspects; the installer tags the pinned image with it too.
	buildkitDetectRef = "moby/buildkit"
	// buildkitContainerName is the container buildtool/ensureBuildKit also
	// starts, so both paths address the same container.
	buildkitContainerName = "buildkit"
	packVersion           = "0.40.9"
	// packReleaseBase is the release download prefix; the arch asset name is
	// appended (pack names the amd64 asset without a suffix).
	packReleaseBase = "https://github.com/buildpacks/pack/releases/download/v" + packVersion + "/pack-v" + packVersion + "-linux"
)

// Step is one command in an install or uninstall plan. Argv is a complete
// argument vector run without a shell, and every element is owned by this
// package: a tool id is only ever used to look a plan up, never interpolated
// into a command, so a caller cannot inject an argument through the id.
type Step struct {
	// Argv is the command line; Argv[0] is the executable.
	Argv []string
	// Env is appended to the child's environment ("KEY=VALUE").
	Env []string
	// Dir is the working directory; empty inherits the agent's.
	Dir string
	// Root is true when the step needs root. A privileged runner elevates it
	// through the node's fixed sudo policy; the executor never calls sudo.
	Root bool
}

// Env is the host facts an installer reads to build its argv. It is a value so
// a test pins arch, distro and directories without touching the host.
type Env struct {
	// Arch is a GOARCH value ("amd64" or "arm64").
	Arch string
	// Distro selects the package manager for docker and git.
	Distro Distro
	// BinDir is the directory host binaries are installed into; it must be on
	// the agent's PATH.
	BinDir string
	// TempDir is the scratch directory downloads and archives use.
	TempDir string
}

// DefaultEnv returns the environment for the host the agent runs on.
func DefaultEnv() Env {
	return Env{
		Arch:    runtime.GOARCH,
		Distro:  DetectDistro(),
		BinDir:  defaultBinDir,
		TempDir: os.TempDir(),
	}
}

// DetectDistro reports the host's package manager family from /etc/os-release,
// falling back to whichever manager resolves on PATH. It returns "" when none
// is recognised.
func DetectDistro() Distro {
	for _, id := range osReleaseIDs() {
		switch id {
		case "debian", "ubuntu", "raspbian", "linuxmint", "pop", "kali":
			return DistroApt
		case "fedora", "rhel", "centos", "rocky", "almalinux", "amzn", "oracle":
			return DistroDNF
		case "alpine":
			return DistroAPK
		}
	}
	for _, candidate := range []struct {
		binary string
		distro Distro
	}{{"apt-get", DistroApt}, {"dnf", DistroDNF}, {"apk", DistroAPK}} {
		if _, err := exec.LookPath(candidate.binary); err == nil {
			return candidate.distro
		}
	}
	return ""
}

// osReleaseIDs returns the lowercase ID and ID_LIKE tokens from /etc/os-release.
func osReleaseIDs() []string {
	data, err := os.ReadFile("/etc/os-release")
	if err != nil {
		return nil
	}
	var ids []string
	for _, line := range strings.Split(string(data), "\n") {
		key, value, ok := strings.Cut(strings.TrimSpace(line), "=")
		if !ok {
			continue
		}
		switch key {
		case "ID", "ID_LIKE":
			ids = append(ids, strings.Fields(strings.ToLower(strings.Trim(value, `"'`)))...)
		}
	}
	return ids
}

// installer is one tool's install and uninstall step builders. A nil uninstall
// means the tool cannot be removed (its catalog entry is Uninstallable=false);
// a nil install means it cannot be added.
type installer struct {
	install   func(Env) ([]Step, error)
	uninstall func(Env) ([]Step, error)
}

// installers maps a catalog id to its installers. An entry here is the only
// way a tool can be installed or removed: a catalog entry with no installer is
// refused, so detect-only host units never fall through to a command.
var installers = map[string]installer{
	IDRailpack: {install: railpackInstallSteps, uninstall: railpackUninstallSteps},
	IDBuildKit: {install: buildkitInstallSteps, uninstall: buildkitUninstallSteps},
	IDPack:     {install: packInstallSteps, uninstall: packUninstallSteps},
	IDDocker:   {install: dockerInstallSteps},
	IDGit:      {install: gitInstallSteps, uninstall: gitUninstallSteps},
}

// PlanItem is one tool's contribution to an install or uninstall: the tool and
// the steps that act on it.
type PlanItem struct {
	Tool  Tool
	Steps []Step
}

// InstallPlan returns the dependency-first plan to install id: every catalog
// dependency the tool needs is planned before it, so railpack is preceded by
// buildkit, which is preceded by docker. Each tool appears once even when two
// branches share a dependency. The caller skips a tool that is already
// installed, so applying a plan twice is idempotent.
func InstallPlan(id string, env Env) ([]PlanItem, error) {
	tool, ok := Lookup(id)
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrUnknownTool, id)
	}
	if !tool.Installable {
		return nil, fmt.Errorf("%w: %q", ErrNotInstallable, id)
	}

	var order []string
	seen := make(map[string]bool)
	var visit func(string) error
	visit = func(current string) error {
		if seen[current] {
			return nil
		}
		seen[current] = true
		entry, ok := Lookup(current)
		if !ok {
			return fmt.Errorf("%w: %q (a dependency of %q)", ErrUnknownTool, current, id)
		}
		for _, dep := range entry.Deps {
			if err := visit(dep); err != nil {
				return err
			}
		}
		order = append(order, current)
		return nil
	}
	if err := visit(id); err != nil {
		return nil, err
	}

	items := make([]PlanItem, 0, len(order))
	for _, toolID := range order {
		entry, _ := Lookup(toolID)
		if !entry.Installable {
			return nil, fmt.Errorf("%w: %q is a dependency of %q", ErrNotInstallable, toolID, id)
		}
		steps, err := stepsFor(toolID, env, true)
		if err != nil {
			return nil, err
		}
		items = append(items, PlanItem{Tool: entry, Steps: steps})
	}
	return items, nil
}

// UninstallPlan returns the plan to remove id. Dependencies are never removed
// with it: another installed tool may still need them, which the caller checks
// before running the plan.
func UninstallPlan(id string, env Env) ([]PlanItem, error) {
	tool, ok := Lookup(id)
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrUnknownTool, id)
	}
	if !tool.Uninstallable {
		return nil, fmt.Errorf("%w: %q", ErrNotUninstallable, id)
	}
	steps, err := stepsFor(id, env, false)
	if err != nil {
		return nil, err
	}
	return []PlanItem{{Tool: tool, Steps: steps}}, nil
}

// stepsFor builds one tool's steps, filling Env defaults first.
func stepsFor(id string, env Env, install bool) ([]Step, error) {
	entry, ok := installers[id]
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrNoInstaller, id)
	}
	build := entry.install
	if !install {
		build = entry.uninstall
	}
	if build == nil {
		return nil, fmt.Errorf("%w: %q", ErrNoInstaller, id)
	}
	return build(withDefaults(env))
}

// withDefaults fills the zero fields of env from the host.
func withDefaults(env Env) Env {
	if env.Arch == "" {
		env.Arch = runtime.GOARCH
	}
	if env.Distro == "" {
		env.Distro = DetectDistro()
	}
	if env.BinDir == "" {
		env.BinDir = defaultBinDir
	}
	if env.TempDir == "" {
		env.TempDir = os.TempDir()
	}
	return env
}

// railpackInstallSteps downloads the official Railpack installer and runs it
// non-interactively into BinDir. The script is fetched over TLS and executed as
// the official installer; it is the same material an operator would pipe to sh
// by hand, run root so it never escalates to sudo itself.
func railpackInstallSteps(env Env) ([]Step, error) {
	script := filepath.Join(env.TempDir, "gotham-railpack-install.sh")
	return []Step{
		downloadStep(script, railpackInstallerURL),
		{
			Argv: []string{"sh", script, "-y", "-b", env.BinDir},
			// Pin the release the installer fetches; detection reports the same
			// version so a re-install is a no-op.
			Env:  []string{"RAILPACK_VERSION=" + railpackVersion},
			Root: true,
		},
		cleanupStep(script),
	}, nil
}

// railpackUninstallSteps runs the same installer with its remove flag, so the
// binary is removed by the official tool rather than by a guessed path.
func railpackUninstallSteps(env Env) ([]Step, error) {
	script := filepath.Join(env.TempDir, "gotham-railpack-install.sh")
	return []Step{
		downloadStep(script, railpackInstallerURL),
		{Argv: []string{"sh", script, "-y", "-r", "-b", env.BinDir}, Root: true},
		cleanupStep(script),
	}, nil
}

// packInstallSteps downloads the arch-matched pack release, verifies its
// published sha256 and installs the binary into BinDir.
func packInstallSteps(env Env) ([]Step, error) {
	asset, err := packAsset(env.Arch)
	if err != nil {
		return nil, err
	}
	archive := filepath.Join(env.TempDir, asset)
	checksum := archive + ".sha256"
	return []Step{
		downloadStep(archive, packReleaseBase+"/"+asset),
		downloadStep(checksum, packReleaseBase+"/"+asset+".sha256"),
		// The published checksum names the asset file, so it is verified from
		// the directory the archive was written to.
		{Argv: []string{"sha256sum", "-c", checksum}, Dir: env.TempDir},
		{Argv: []string{"tar", "-xzf", archive, "-C", env.TempDir}},
		{Argv: []string{"install", "-m", "0755", filepath.Join(env.TempDir, "pack"), filepath.Join(env.BinDir, "pack")}, Root: true},
		cleanupStep(archive, checksum),
	}, nil
}

// packUninstallSteps removes the binary packInstallSteps placed.
func packUninstallSteps(env Env) ([]Step, error) {
	return []Step{{Argv: []string{"rm", "-f", filepath.Join(env.BinDir, "pack")}, Root: true}}, nil
}

// packAsset maps a GOARCH value to the pack release asset. Only the
// architectures Gotham ships on are supported: pack names the amd64 asset
// "linux.tgz" and every other arch with its suffix.
func packAsset(arch string) (string, error) {
	switch arch {
	case "amd64":
		return "pack-v" + packVersion + "-linux.tgz", nil
	case "arm64":
		return "pack-v" + packVersion + "-linux-arm64.tgz", nil
	default:
		return "", fmt.Errorf("%w: pack has no release for %s", ErrNotInstallable, arch)
	}
}

// buildkitInstallSteps pulls the pinned BuildKit image and starts it as the
// privileged container the Railpack build path expects. The tag step gives the
// image the untagged reference detection probes, so an install is observable
// through ListTools. Docker access is the agent's docker group, so no step
// needs root.
func buildkitInstallSteps(_ Env) ([]Step, error) {
	return []Step{
		{Argv: []string{"docker", "pull", buildkitImage}},
		{Argv: []string{"docker", "tag", buildkitImage, buildkitDetectRef}},
		{Argv: []string{"docker", "run", "-d", "--privileged", "--restart", "unless-stopped", "--name", buildkitContainerName, buildkitImage}},
	}, nil
}

// buildkitUninstallSteps removes the managed BuildKit container. The image is
// left in place: removing it would force a full re-pull on the next install and
// is not what "uninstall the tool" means here.
func buildkitUninstallSteps(_ Env) ([]Step, error) {
	return []Step{{Argv: []string{"docker", "rm", "-f", buildkitContainerName}}}, nil
}

// dockerInstallSteps installs docker through the distro's package manager and
// enables its service where the distro uses systemd.
func dockerInstallSteps(env Env) ([]Step, error) {
	return packageInstallSteps(env,
		packageNames{apt: "docker.io", dnf: "docker", apk: "docker"},
		"docker")
}

// docker has no uninstaller: the catalog marks it Uninstallable=false, because
// removing it would destroy every workload on the node.

// gitInstallSteps installs git through the distro's package manager.
func gitInstallSteps(env Env) ([]Step, error) {
	return packageInstallSteps(env, packageNames{apt: "git", dnf: "git", apk: "git"}, "git")
}

// gitUninstallSteps removes the git package.
func gitUninstallSteps(env Env) ([]Step, error) {
	switch env.Distro {
	case DistroApt:
		return []Step{{Argv: []string{"apt-get", "remove", "-y", "git"}, Root: true}}, nil
	case DistroDNF:
		return []Step{{Argv: []string{"dnf", "remove", "-y", "git"}, Root: true}}, nil
	case DistroAPK:
		return []Step{{Argv: []string{"apk", "del", "git"}, Root: true}}, nil
	default:
		return nil, fmt.Errorf("%w: git has no uninstaller for distro %q", ErrNotUninstallable, env.Distro)
	}
}

// packageNames is the package each supported distro calls a tool.
type packageNames struct {
	apt string
	dnf string
	apk string
}

// packageInstallSteps builds the install-and-enable steps for one package. The
// apt path refreshes the package index first; the systemd enable step is only
// added where the distro runs systemd.
func packageInstallSteps(env Env, names packageNames, label string) ([]Step, error) {
	var steps []Step
	switch env.Distro {
	case DistroApt:
		steps = []Step{
			{Argv: []string{"apt-get", "update"}, Root: true},
			{Argv: []string{"apt-get", "install", "-y", names.apt}, Root: true},
			{Argv: []string{"systemctl", "enable", "--now", label}, Root: true},
		}
	case DistroDNF:
		steps = []Step{
			{Argv: []string{"dnf", "install", "-y", names.dnf}, Root: true},
			{Argv: []string{"systemctl", "enable", "--now", label}, Root: true},
		}
	case DistroAPK:
		// Alpine starts services through openrc, not systemd; installing the
		// package is all this step can portably do.
		steps = []Step{{Argv: []string{"apk", "add", "--no-cache", names.apk}, Root: true}}
	default:
		return nil, fmt.Errorf("%w: %s has no installer for distro %q", ErrNotInstallable, label, env.Distro)
	}
	return steps, nil
}

// downloadStep is a curl download with the retry policy the installers rely on
// so a transient network error does not fail an install.
func downloadStep(dest, url string) Step {
	return Step{Argv: []string{"curl", "-fsSL", "--retry", "3", "--retry-all-errors", "--retry-delay", "2", "-o", dest, url}}
}

// cleanupStep removes download scratch. Cleanup is best effort: a failed
// remove must not fail an install that already succeeded.
func cleanupStep(paths ...string) Step {
	return Step{Argv: append([]string{"rm", "-f"}, paths...)}
}
