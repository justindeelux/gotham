// Package buildtool runs the external language build toolchains (Railpack and
// Cloud Native Buildpacks) and extracts a received build context. It is shared
// by the control-plane build engines and the node agent so the same invocation
// runs wherever a build is dispatched. It deliberately has no dependency on
// either scope: agent/ must not import internal/, and the control plane must be
// free to route a build to a node.
package buildtool

import (
	"archive/tar"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Engine identifies a toolchain. It mirrors the control plane's EngineKind for
// the two engines that shell out to a CLI; the Dockerfile and static engines do
// not use this package.
type Engine string

const (
	// Railpack builds a language project from a generated plan.
	Railpack Engine = "railpack"
	// Buildpacks builds a project with the Cloud Native Buildpacks lifecycle.
	Buildpacks Engine = "buildpacks"
)

// CLI executable names.
const (
	RailpackCLI   = "railpack"
	BuildpacksCLI = "pack"
)

// Install and prerequisite hints. They are quoted in every ErrCLIMissing error
// so the deploy log is actionable.
const (
	railpackInstallHint   = "install it with `curl -sSL https://railpack.com/install.sh | sh` (https://railpack.com/installation)"
	railpackBuildKitHint  = "install Docker so Gotham can start its BuildKit container, or export BUILDKIT_HOST to a running BuildKit daemon (`docker run --rm --privileged -d --name buildkit moby/buildkit`, `export BUILDKIT_HOST=docker-container://buildkit`)"
	buildpacksInstallHint = "install it with `brew install buildpacks/pack/pack` or from https://github.com/buildpacks/pack/releases (https://buildpacks.io/docs/install-pack/)"
)

// ErrCLIMissing reports a build whose external toolchain is unavailable on this
// host: the CLI binary is not on PATH, or a prerequisite the CLI cannot run
// without (Railpack's BUILDKIT_HOST) is not configured. Nothing is fabricated;
// the build simply cannot run here.
var ErrCLIMissing = errors.New("builds: build toolchain unavailable")

// Options describes one toolchain invocation.
type Options struct {
	// Dir is the working directory, the extracted source tree.
	Dir string
	// Tag is the image reference the toolchain must build.
	Tag string
	// BuildArgs are passed as build-time environment (`--env KEY=VALUE`).
	BuildArgs map[string]string
	// LogWriter receives the tool's combined output. Nil discards it.
	LogWriter io.Writer
	// DockerHost, when non-empty, is exported to the child as DOCKER_HOST. The
	// node agent sets its daemon endpoint; the control plane leaves it empty so
	// the CLI inherits the process environment.
	DockerHost string

	// buildKitHost is the BUILDKIT_HOST exported to Railpack; Run fills it.
	buildKitHost string
}

// Available reports whether the toolchain can run at all, returning an
// ErrCLIMissing error with the install hint when it cannot. Callers check it
// before doing any work so a missing toolchain never looks like a failed build.
func Available(engine Engine) error {
	switch engine {
	case Railpack:
		if _, err := exec.LookPath(RailpackCLI); err != nil {
			return fmt.Errorf("%w: %q is not on PATH; %s", ErrCLIMissing, RailpackCLI, railpackInstallHint)
		}
		if strings.TrimSpace(os.Getenv("BUILDKIT_HOST")) == "" {
			if _, err := exec.LookPath("docker"); err != nil {
				return fmt.Errorf("%w: %s also needs a running BuildKit daemon: %s",
					ErrCLIMissing, RailpackCLI, railpackBuildKitHint)
			}
		}
		return nil
	case Buildpacks:
		if _, err := exec.LookPath(BuildpacksCLI); err != nil {
			return fmt.Errorf("%w: %q is not on PATH; %s", ErrCLIMissing, BuildpacksCLI, buildpacksInstallHint)
		}
		return nil
	default:
		return fmt.Errorf("%w: unknown engine %q", ErrCLIMissing, engine)
	}
}

// Run invokes the toolchain in opts.Dir, tagging the resulting image opts.Tag
// and streaming the combined output to opts.LogWriter. It checks availability
// first so a missing toolchain fails with ErrCLIMissing and an install hint.
func Run(ctx context.Context, engine Engine, opts Options) error {
	if err := Available(engine); err != nil {
		return err
	}
	if strings.TrimSpace(opts.Dir) == "" {
		return fmt.Errorf("%w: empty build directory", ErrCLIMissing)
	}
	if strings.TrimSpace(opts.Tag) == "" {
		return fmt.Errorf("%w: empty image tag", ErrCLIMissing)
	}

	var name string
	var args []string
	switch engine {
	case Railpack:
		host, err := ensureBuildKit(ctx, opts)
		if err != nil {
			return err
		}
		opts.buildKitHost = host
		name = RailpackCLI
		args = []string{"build", "--name", opts.Tag, "--progress", "plain"}
		args = append(args, buildEnvFlags(opts.BuildArgs)...)
		args = append(args, ".")
	case Buildpacks:
		name = BuildpacksCLI
		args = []string{"build", opts.Tag, "--path", "."}
		args = append(args, buildEnvFlags(opts.BuildArgs)...)
	default:
		return fmt.Errorf("%w: unknown engine %q", ErrCLIMissing, engine)
	}
	return run(ctx, name, args, opts)
}

// run executes the CLI in its working directory and streams stdout and stderr
// to opts.LogWriter as they are produced, prefixed with the command line so the
// deploy log shows what ran. A cancelled context stops the tool and reports the
// context error.
func run(ctx context.Context, name string, args []string, opts Options) error {
	sink := opts.LogWriter
	if sink == nil {
		sink = io.Discard
	}
	command := exec.CommandContext(ctx, name, args...)
	command.Dir = opts.Dir
	// The toolchain is tenant-influenced, so it runs with a stripped
	// environment: only what the CLI needs, never the agent's own secrets
	// (the registry credential and the agent TLS key live in the state dir the
	// process also reads). A toolchain compromise still means node compromise
	// because it runs as the agent user; dedicated-user/userns sandboxing is a
	// tracked follow-up.
	command.Env = toolchainEnv(opts.DockerHost, opts.buildKitHost)
	// The same writer on both streams keeps the tool's output together;
	// os/exec serialises the writes when Stdout and Stderr are equal.
	command.Stdout = sink
	command.Stderr = sink
	_, _ = io.WriteString(sink, redactedCommandLine(name, args)+"\n")
	if err := command.Run(); err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return fmt.Errorf("%s build: %w", name, ctxErr)
		}
		return fmt.Errorf("%s build: %w", name, err)
	}
	return nil
}

// toolchainEnv builds the minimal environment passed to a toolchain process.
// Only PATH (to resolve the CLI and its own helpers), HOME and TMPDIR, the
// BuildKit address Railpack needs, and the explicit Docker host are inherited;
// every other variable — in particular agent/control-plane secrets — is
// stripped. Env is never nil, so the child does not inherit the parent's
// environment by default.
func toolchainEnv(dockerHost, buildKitHost string) []string {
	env := make([]string, 0, 5)
	if buildKitHost != "" {
		env = append(env, "BUILDKIT_HOST="+buildKitHost)
	}
	keys := []string{"PATH", "HOME", "TMPDIR"}
	if buildKitHost == "" {
		keys = append(keys, "BUILDKIT_HOST")
	}
	for _, key := range keys {
		if value := os.Getenv(key); value != "" {
			env = append(env, key+"="+value)
		}
	}
	if strings.TrimSpace(dockerHost) != "" {
		env = append(env, "DOCKER_HOST="+dockerHost)
	}
	return env
}

// buildKitContainer is the container ensureBuildKit creates on the node.
const buildKitContainer = "buildkit"

// buildKitMu serialises ensureBuildKit inside one process so two first builds
// never both try to create the container.
var buildKitMu sync.Mutex

// ensureBuildKit returns the BUILDKIT_HOST Railpack should use. An address the
// operator exported wins; otherwise it starts (or creates) a privileged
// moby/buildkit container through the node's Docker so no manual setup is
// needed, and waits until the daemon answers. The container restarts with
// Docker, so this is a one-time cost.
// ponytail: unpinned moby/buildkit image; pin a tag/digest for reproducibility.
func ensureBuildKit(ctx context.Context, opts Options) (string, error) {
	if host := strings.TrimSpace(os.Getenv("BUILDKIT_HOST")); host != "" {
		return host, nil
	}
	buildKitMu.Lock()
	defer buildKitMu.Unlock()
	docker := func(args ...string) (string, error) {
		command := exec.CommandContext(ctx, "docker", args...)
		command.Env = toolchainEnv(opts.DockerHost, "")
		out, err := command.CombinedOutput()
		return strings.TrimSpace(string(out)), err
	}
	fail := func(err error, out string) (string, error) {
		return "", fmt.Errorf("%w: start BuildKit container: %v: %s", ErrCLIMissing, err, out)
	}
	out, err := docker("inspect", "-f", "{{.State.Running}}", buildKitContainer)
	switch {
	case err != nil && !strings.Contains(out, "No such object"):
		return fail(err, out) // daemon down or permission denied: do not try to create
	case err != nil:
		out, err = docker("run", "-d", "--privileged", "--restart", "unless-stopped",
			"--name", buildKitContainer, "moby/buildkit")
		if err != nil { // another process may have created it first
			if state, inspectErr := docker("inspect", "-f", "{{.State.Running}}", buildKitContainer); inspectErr != nil {
				return fail(err, out)
			} else if state != "true" {
				out, err = docker("start", buildKitContainer)
			} else {
				err = nil
			}
		}
	case out != "true":
		out, err = docker("start", buildKitContainer)
	}
	if err != nil {
		return fail(err, out)
	}
	// The daemon needs a moment after create/start before it accepts builds.
	for i := 0; i < 30; i++ {
		if _, err = docker("exec", buildKitContainer, "buildctl", "debug", "workers"); err == nil {
			return "docker-container://" + buildKitContainer, nil
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(time.Second):
		}
	}
	return fail(err, "BuildKit did not become ready within 30s")
}

// buildEnvFlags renders build arguments as sorted `--env KEY=VALUE` flag pairs.
// Both CLIs take build-time environment that way, and sorting keeps the command
// line deterministic in the deploy log.
func buildEnvFlags(args map[string]string) []string {
	if len(args) == 0 {
		return nil
	}
	keys := make([]string, 0, len(args))
	for key := range args {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	flags := make([]string, 0, len(keys)*2)
	for _, key := range keys {
		flags = append(flags, "--env", key+"="+args[key])
	}
	return flags
}

// redactedCommandLine renders a command for the deploy log with build-arg
// values masked. Build args may carry secrets, so only the key is logged; the
// real value is still passed to the toolchain on the command line.
func redactedCommandLine(name string, args []string) string {
	redacted := make([]string, len(args))
	copy(redacted, args)
	for i := range redacted {
		switch {
		case redacted[i] == "--env" && i+1 < len(redacted):
			redacted[i+1] = redactBuildArg(redacted[i+1])
		case strings.HasPrefix(redacted[i], "--env="):
			redacted[i] = "--env=" + redactBuildArg(strings.TrimPrefix(redacted[i], "--env="))
		}
	}
	return "$ " + name + " " + strings.Join(redacted, " ")
}

// redactBuildArg masks the value of a KEY=VALUE build argument, leaving the key
// visible.
func redactBuildArg(arg string) string {
	key, _, ok := strings.Cut(arg, "=")
	if !ok {
		return arg
	}
	return key + "=***"
}

// ExtractTar writes an uncompressed build-context tar into dir. Entries that
// escape dir (an absolute path or a `..` component) are rejected, so a
// compromised control plane cannot write outside the extraction root. Symlinks
// and other non-regular entries are skipped: the Dockerfile/static context may
// carry symlinks for the Docker daemon, but recreating one here before later
// entries are written would allow a tar-slip through the link target, so the
// language-toolchain path deliberately drops them.
func ExtractTar(dir string, source io.Reader) error {
	if strings.TrimSpace(dir) == "" {
		return errors.New("buildtool: empty extraction directory")
	}
	reader := tar.NewReader(source)
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("buildtool: read context: %w", err)
		}
		name, err := safeArchivePath(header.Name)
		if err != nil {
			return err
		}
		if name == "" {
			continue
		}
		target := filepath.Join(dir, name)
		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return fmt.Errorf("buildtool: create %s: %w", target, err)
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return fmt.Errorf("buildtool: create %s: %w", filepath.Dir(target), err)
			}
			mode := os.FileMode(header.Mode).Perm()
			if mode == 0 {
				mode = 0o644
			}
			file, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
			if err != nil {
				return fmt.Errorf("buildtool: create %s: %w", target, err)
			}
			if _, err := io.Copy(file, reader); err != nil {
				_ = file.Close()
				return fmt.Errorf("buildtool: write %s: %w", target, err)
			}
			if err := file.Close(); err != nil {
				return fmt.Errorf("buildtool: close %s: %w", target, err)
			}
		default:
			// Directories, regular files and skipped symlinks/devices only.
		}
	}
}

// safeArchivePath validates one archive entry name and returns it cleaned as a
// slash-separated relative path, or an error when it escapes the root.
func safeArchivePath(name string) (string, error) {
	cleaned := filepath.ToSlash(strings.TrimSpace(name))
	cleaned = strings.TrimPrefix(cleaned, "./")
	if cleaned == "" || cleaned == "." {
		return "", nil
	}
	if strings.HasPrefix(cleaned, "/") {
		return "", fmt.Errorf("buildtool: context entry %q is absolute", name)
	}
	for _, segment := range strings.Split(cleaned, "/") {
		if segment == ".." {
			return "", fmt.Errorf("buildtool: context entry %q escapes the root", name)
		}
	}
	return cleaned, nil
}
