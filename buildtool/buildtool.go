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
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/justindeelux/gotham/toolcatalog"
)

type Engine string

const (
	Railpack   Engine = "railpack"
	Buildpacks Engine = "buildpacks"
)

const (
	RailpackCLI   = "railpack"
	BuildpacksCLI = "pack"
)

const (
	railpackInstallHint   = "install it from the App Store"
	railpackBuildKitHint  = "install Docker from the App Store so Gotham can run its BuildKit container"
	buildpacksInstallHint = "install it from the App Store"
)

var ErrCLIMissing = errors.New("builds: build toolchain unavailable")

type Options struct {
	Dir        string
	Tag        string
	BuildArgs  map[string]string
	LogWriter  io.Writer
	DockerHost string
	StateDir   string

	buildKitHost string
}

func Available(engine Engine) error {
	switch engine {
	case Railpack:
		if !toolcatalog.Installed(toolcatalog.IDRailpack) {
			return fmt.Errorf("%w: %q is not on PATH; %s", ErrCLIMissing, RailpackCLI, railpackInstallHint)
		}
		if strings.TrimSpace(os.Getenv("BUILDKIT_HOST")) == "" {
			if !toolcatalog.Installed(toolcatalog.IDDocker) {
				return fmt.Errorf("%w: %s also needs a running BuildKit daemon: %s",
					ErrCLIMissing, RailpackCLI, railpackBuildKitHint)
			}
		}
		return nil
	case Buildpacks:
		if !toolcatalog.Installed(toolcatalog.IDPack) {
			return fmt.Errorf("%w: %q is not on PATH; %s", ErrCLIMissing, BuildpacksCLI, buildpacksInstallHint)
		}
		return nil
	default:
		return fmt.Errorf("%w: unknown engine %q", ErrCLIMissing, engine)
	}
}

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
	home := toolchainHome(opts.StateDir)
	logToolchainHomeFallback(opts.LogWriter, os.Getenv("HOME"), home)
	switch engine {
	case Railpack:
		host, err := ensureBuildKit(ctx, opts, home)
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
	return run(ctx, name, args, opts, home)
}

func run(ctx context.Context, name string, args []string, opts Options, home string) error {
	sink := opts.LogWriter
	if sink == nil {
		sink = io.Discard
	}
	tail := newOutputTail(buildArgValues(opts.BuildArgs))
	tee := io.MultiWriter(tail, sink)
	command := exec.CommandContext(ctx, name, args...)
	command.Dir = opts.Dir
	command.Env = toolchainEnv(opts.DockerHost, opts.buildKitHost, home)
	command.Stdout = tee
	command.Stderr = tee
	_, _ = io.WriteString(sink, redactedCommandLine(name, args)+"\n")
	if err := command.Run(); err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return fmt.Errorf("%s build: %w", name, ctxErr)
		}
		if last := tail.String(); last != "" {
			return fmt.Errorf("%s build: %w\n--- last output ---\n%s", name, err, last)
		}
		return fmt.Errorf("%s build: %w", name, err)
	}
	return nil
}

const (
	tailMaxLines = 20
	tailMaxBytes = 3 << 10
)

const pendingMaxBytes = 8 << 10

var (
	ansiEscape = regexp.MustCompile("\x1b\\[[0-9;?]*[a-zA-Z]")
	oscEscape  = regexp.MustCompile("\x1b\\][^\x07\x1b]*(?:\x07|\x1b\\\\)")
)

type outputTail struct {
	lines   []string
	pending strings.Builder
	size    int
	masks   []string
}

func newOutputTail(secrets []string) *outputTail {
	masks := make([]string, 0, len(secrets))
	for _, secret := range secrets {
		for line := range strings.Lines(strings.ReplaceAll(secret, "\r", "\n")) {
			if strings.TrimSpace(line) != "" {
				masks = append(masks, strings.TrimRight(line, "\n"))
			}
		}
	}
	sort.Slice(masks, func(i, j int) bool { return len(masks[i]) > len(masks[j]) })
	return &outputTail{masks: masks}
}

func (t *outputTail) Write(p []byte) (int, error) {
	t.pending.Write(p)
	if t.pending.Len() > pendingMaxBytes {
		s := t.pending.String()
		s = s[len(s)-pendingMaxBytes:]
		for len(s) > 0 && s[0] >= 0x80 && s[0] < 0xC0 {
			s = s[1:]
		}
		t.pending.Reset()
		t.pending.WriteString(s)
	}
	s := t.pending.String()
	start := 0
	i := 0
	for i < len(s) {
		if s[i] != '\n' && s[i] != '\r' {
			i++
			continue
		}
		t.push(s[start:i])
		first := s[i]
		i++
		if i < len(s) && (s[i] == '\n' || s[i] == '\r') && s[i] != first {
			i++
		}
		start = i
	}
	if start > 0 {
		rest := s[start:]
		t.pending.Reset()
		t.pending.WriteString(rest)
	}
	return len(p), nil
}

func (t *outputTail) String() string {
	if rest := t.pending.String(); rest != "" {
		t.push(rest)
		t.pending.Reset()
	}
	return strings.Join(t.lines, "\n")
}

func (t *outputTail) push(raw string) {
	line := oscEscape.ReplaceAllString(ansiEscape.ReplaceAllString(strings.TrimSuffix(raw, "\r"), ""), "")
	for _, secret := range t.masks {
		line = strings.ReplaceAll(line, secret, "***")
	}
	line = strings.ToValidUTF8(line, "\uFFFD")
	if len(line) > tailMaxBytes {
		line = line[len(line)-tailMaxBytes:]
		for len(line) > 0 && line[0] >= 0x80 && line[0] < 0xC0 {
			line = line[1:]
		}
	}
	t.lines = append(t.lines, line)
	t.size += len(line)
	for len(t.lines) > tailMaxLines || t.size > tailMaxBytes {
		t.size -= len(t.lines[0])
		t.lines = t.lines[1:]
	}
}

func buildArgValues(args map[string]string) []string {
	values := make([]string, 0, len(args))
	for _, value := range args {
		if value != "" {
			values = append(values, value)
		}
	}
	sort.Slice(values, func(i, j int) bool { return len(values[i]) > len(values[j]) })
	return values
}

func toolchainEnv(dockerHost, buildKitHost, home string) []string {
	env := make([]string, 0, 6)
	if buildKitHost != "" {
		env = append(env, "BUILDKIT_HOST="+buildKitHost)
	}
	keys := []string{"PATH", "TMPDIR"}
	if buildKitHost == "" {
		keys = append(keys, "BUILDKIT_HOST")
	}
	for _, key := range keys {
		if value := os.Getenv(key); value != "" {
			env = append(env, key+"="+value)
		}
	}
	if home != "" {
		env = append(env, "HOME="+home)
	}
	if strings.TrimSpace(dockerHost) != "" {
		env = append(env, "DOCKER_HOST="+dockerHost)
	}
	return env
}

func toolchainHome(stateDir string) string {
	if home := os.Getenv("HOME"); homeUsable(home) {
		return home
	}
	if stateDir = strings.TrimSpace(stateDir); stateDir != "" {
		if dir := toolchainHomeUnder(filepath.Join(stateDir, "toolchain-home")); dir != "" {
			return dir
		}
	}
	return tempFallbackHome()
}

var (
	tempFallbackMu  sync.Mutex
	tempFallbackDir string
	tempFallbackTmp string
)

func tempFallbackHome() string {
	tempFallbackMu.Lock()
	defer tempFallbackMu.Unlock()
	tmp := os.TempDir()
	if tempFallbackDir != "" && tempFallbackTmp == tmp {
		if info, err := os.Lstat(tempFallbackDir); err == nil &&
			info.Mode()&os.ModeSymlink == 0 && info.IsDir() {
			return tempFallbackDir
		}
	}
	dir, err := os.MkdirTemp(tmp, "gotham-toolchain-home-*")
	if err != nil {
		return ""
	}
	tempFallbackDir, tempFallbackTmp = dir, tmp
	return dir
}

func toolchainHomeUnder(dir string) string {
	if strings.TrimSpace(dir) == "" {
		return ""
	}
	if info, err := os.Lstat(dir); err == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return ""
		}
		if err := os.Chmod(dir, 0o700); err != nil {
			return ""
		}
		return dir
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return ""
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return ""
	}
	return dir
}

func homeUsable(home string) bool {
	if strings.TrimSpace(home) == "" || !filepath.IsAbs(home) {
		return false
	}
	info, err := os.Stat(home)
	if err != nil || !info.IsDir() {
		return false
	}
	probe, err := os.CreateTemp(home, ".gotham-home-probe-*")
	if err != nil {
		return false
	}
	_ = probe.Close()
	_ = os.Remove(probe.Name())
	return true
}

func logToolchainHomeFallback(w io.Writer, processHome, home string) {
	if w == nil || home == "" || home == processHome {
		return
	}
	_, _ = io.WriteString(w, "toolchain HOME="+home+" (process HOME unusable, using fallback)\n")
}

const buildKitContainer = "buildkit"

var buildKitMu sync.Mutex

func ensureBuildKit(ctx context.Context, opts Options, home string) (string, error) {
	if host := strings.TrimSpace(os.Getenv("BUILDKIT_HOST")); host != "" {
		return host, nil
	}
	buildKitMu.Lock()
	defer buildKitMu.Unlock()
	toolEnv := toolchainEnv(opts.DockerHost, "", home)
	docker := func(args ...string) (string, error) {
		command := exec.CommandContext(ctx, "docker", args...)
		command.Env = toolEnv
		out, err := command.CombinedOutput()
		return strings.TrimSpace(string(out)), err
	}
	fail := func(err error, out string) (string, error) {
		return "", fmt.Errorf("%w: start BuildKit container: %v: %s", ErrCLIMissing, err, out)
	}
	out, err := docker("inspect", "-f", "{{.State.Running}}", buildKitContainer)
	switch {
	case err != nil && !strings.Contains(out, "No such object"):
		return fail(err, out)
	case err != nil:
		out, err = docker("run", "-d", "--privileged", "--restart", "unless-stopped",
			"--name", buildKitContainer, "moby/buildkit")
		if err != nil {
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

func redactBuildArg(arg string) string {
	key, _, ok := strings.Cut(arg, "=")
	if !ok {
		return arg
	}
	return key + "=***"
}

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
		}
	}
}

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
