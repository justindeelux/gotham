package builds

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// railpackMarkers are the project descriptors that identify a language Railpack
// can build without a Dockerfile.
var railpackMarkers = []string{
	"package.json",
	"go.mod",
	"requirements.txt",
	"pyproject.toml",
	"Pipfile",
	"Gemfile",
	"composer.json",
	"Cargo.toml",
	"pom.xml",
	"build.gradle",
	"build.gradle.kts",
	"mix.exs",
	"deno.json",
}

const (
	// railpackCLI is the Railpack command-line tool the engine shells out to.
	railpackCLI = "railpack"
	// railpackInstallHint tells an operator how to put railpack on PATH. It is
	// quoted in every ErrCLIMissing error so the deploy log is actionable.
	railpackInstallHint = "install it with `curl -sSL https://railpack.com/install.sh | sh` (https://railpack.com/installation)"
	// railpackBuildKitHint explains the BuildKit prerequisite `railpack build`
	// refuses to run without.
	railpackBuildKitHint = "start BuildKit and export its address: `docker run --rm --privileged -d --name buildkit moby/buildkit` then `export BUILDKIT_HOST=docker-container://buildkit`"
)

// ErrCLIMissing reports a build whose external toolchain is unavailable on this
// node: the CLI binary is not on PATH, or a prerequisite the CLI cannot run
// without (Railpack's BUILDKIT_HOST) is not configured. The engine itself is
// wired — nothing is fabricated, the build simply cannot run here.
//
// It is declared next to the Railpack engine rather than in errors.go because
// only the two toolchain engines return it and this work package is scoped to
// railpack.go, buildpacks.go and their tests.
var ErrCLIMissing = errors.New("builds: build toolchain unavailable")

// cliRun is one invocation of an external build CLI. The Railpack and
// Buildpacks engines share it: availability checking, log streaming and
// cancellation behave identically for both.
type cliRun struct {
	// name is the executable looked up on PATH.
	name string
	// installHint tells the operator how to install name; it is quoted in the
	// ErrCLIMissing error.
	installHint string
	// dir is the working directory of the command, the cloned repository.
	dir string
	// args are the arguments passed to name.
	args []string
}

// available reports whether the CLI can run at all, returning an
// ErrCLIMissing error with the install hint when it cannot. Engines call it
// before doing any work so a missing toolchain never looks like a failed build.
func (c cliRun) available() error {
	if _, err := exec.LookPath(c.name); err != nil {
		return fmt.Errorf("%w: %q is not on PATH; %s", ErrCLIMissing, c.name, c.installHint)
	}
	return nil
}

// exec runs the CLI in its working directory and streams stdout and stderr to
// opts.LogWriter as they are produced, prefixed with the command line so the
// deploy log shows what ran. A cancelled context stops the tool and reports the
// context error. Callers must have passed available() first.
func (c cliRun) exec(ctx context.Context, opts BuildOptions) error {
	sink := opts.LogWriter
	if sink == nil {
		sink = io.Discard
	}
	command := exec.CommandContext(ctx, c.name, c.args...)
	command.Dir = c.dir
	// The same writer on both streams keeps the tool's output together;
	// os/exec serialises the writes when Stdout and Stderr are equal.
	command.Stdout = sink
	command.Stderr = sink
	writeLogs(sink, "$ "+c.name+" "+strings.Join(c.args, " ")+"\n")
	if err := command.Run(); err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return fmt.Errorf("%s build: %w", c.name, ctxErr)
		}
		return fmt.Errorf("%s build: %w", c.name, err)
	}
	return nil
}

// buildEnvFlags renders build arguments as sorted `--env KEY=VALUE` flag pairs.
// Both CLIs take build-time environment that way, and sorting keeps the
// command line deterministic in the deploy log.
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

// RailpackEngine detects language projects for Railpack and builds them by
// shelling out to the railpack CLI.
type RailpackEngine struct{}

// NewRailpackEngine returns the Railpack engine.
func NewRailpackEngine() *RailpackEngine { return &RailpackEngine{} }

// Kind implements BuildEngine.
func (e *RailpackEngine) Kind() EngineKind { return EngineRailpack }

// Detect implements BuildEngine. An explicit Railpack hint is honoured without
// inspecting the tree.
func (e *RailpackEngine) Detect(repoDir string, hint EngineKind) bool {
	if hint != EngineAuto {
		return hint == EngineRailpack
	}
	for _, marker := range railpackMarkers {
		if exists(filepath.Join(repoDir, marker)) {
			return true
		}
	}
	return false
}

// Build implements BuildEngine by shelling out to `railpack build`. Railpack
// generates a Dockerfile for the detected language and builds it with
// BuildKit, then loads the image into the local Docker daemon under the
// standardized tag from ImageTag.
//
// Contract notes:
//
//   - The toolchain is checked before anything else: a missing railpack binary
//     or an unset BUILDKIT_HOST fails with ErrCLIMissing and an install hint
//     instead of a fabricated image.
//   - Pushing is deliberately not part of this call. The ImageBuilder seam
//     exposes Build only, and `railpack build` has no publish flag, so there is
//     nothing here that could push: the image stays in the local daemon and the
//     deploy state machine's `pushing` step (BE-4.3) moves it to the node's
//     internal registry.
//   - BuildArgs are passed as build-time environment (`--env`), Railpack's
//     counterpart to a Dockerfile --build-arg. Labels and Target have no CLI
//     equivalent (there is no label flag, and a Railpack plan owns its own
//     stages), so they are not applied here; the deploy layer applies labels
//     when it pushes the image.
//   - The CLI reports no digest, so ImageRef.Digest stays empty; the digest is
//     recorded when the deploy layer pushes the image.
func (e *RailpackEngine) Build(ctx context.Context, opts BuildOptions) (ImageRef, error) {
	if err := validateOptions(opts); err != nil {
		return ImageRef{}, err
	}
	tag := ImageTag(opts.AppID, opts.DeployID)
	args := []string{"build", "--name", tag, "--progress", "plain"}
	args = append(args, buildEnvFlags(opts.BuildArgs)...)
	args = append(args, ".")

	run := cliRun{name: railpackCLI, installHint: railpackInstallHint, dir: opts.RepoDir, args: args}
	if err := run.available(); err != nil {
		return ImageRef{}, err
	}
	if strings.TrimSpace(os.Getenv("BUILDKIT_HOST")) == "" {
		return ImageRef{}, fmt.Errorf("%w: %s also needs a running BuildKit daemon: %s",
			ErrCLIMissing, railpackCLI, railpackBuildKitHint)
	}
	if err := run.exec(ctx, opts); err != nil {
		return ImageRef{}, err
	}
	return ImageRef{Kind: EngineRailpack, Tag: tag}, nil
}
