package builds

import (
	"context"
	"path/filepath"
)

// buildpacksMarkers are the project descriptors that identify a Cloud Native
// Buildpacks (herokuish-style) project.
var buildpacksMarkers = []string{
	"project.toml",
	"Procfile",
	"app.json",
	"heroku.yml",
	"buildpack.toml",
}

const (
	// packCLI is the Cloud Native Buildpacks command-line tool the engine
	// shells out to; it drives the lifecycle inside the builder container.
	packCLI = "pack"
	// packInstallHint tells an operator how to put pack on PATH. It is quoted
	// in every ErrCLIMissing error so the deploy log is actionable.
	packInstallHint = "install it with `brew install buildpacks/pack/pack` or from https://github.com/buildpacks/pack/releases (https://buildpacks.io/docs/install-pack/)"
)

// BuildpacksEngine detects Cloud Native Buildpacks projects and builds them by
// shelling out to the pack CLI.
type BuildpacksEngine struct{}

// NewBuildpacksEngine returns the Buildpacks engine.
func NewBuildpacksEngine() *BuildpacksEngine { return &BuildpacksEngine{} }

// Kind implements BuildEngine.
func (e *BuildpacksEngine) Kind() EngineKind { return EngineBuildpacks }

// Detect implements BuildEngine. An explicit Buildpacks hint is honoured
// without inspecting the tree.
func (e *BuildpacksEngine) Detect(repoDir string, hint EngineKind) bool {
	if hint != EngineAuto {
		return hint == EngineBuildpacks
	}
	for _, marker := range buildpacksMarkers {
		if exists(filepath.Join(repoDir, marker)) {
			return true
		}
	}
	return false
}

// Build implements BuildEngine by shelling out to `pack build`, which runs the
// Cloud Native Buildpacks lifecycle inside a builder container and exports the
// resulting image into the local Docker daemon under the standardized tag from
// ImageTag.
//
// Contract notes:
//
//   - The toolchain is checked first: a pack binary missing from PATH fails
//     with ErrCLIMissing and an install hint instead of a fabricated image.
//   - No --builder flag is passed on purpose: pack then falls back to the
//     operator's configured default (`pack config default-builder`) or to the
//     builder declared in the repository's project.toml, and reports a clear
//     error of its own when neither exists.
//   - Pushing is deliberately not part of this call. `pack build --publish`
//     would push in one step, but it needs a registry-qualified reference while
//     ImageTag produces the daemon-local name `gotham/{appID}:{deployID}`, and
//     the ImageBuilder seam exposes Build only. The image stays local and the
//     deploy state machine's `pushing` step (BE-4.3) moves it to the node's
//     internal registry.
//   - BuildArgs are passed as build-time environment (`--env`), the pack
//     equivalent of a Dockerfile --build-arg. Labels and Target are Dockerfile
//     concepts that pack cannot express (no label flag, and a buildpack group
//     owns its own build stages), so they are not applied here; the deploy
//     layer applies labels when it pushes the image.
//   - The CLI reports no digest, so ImageRef.Digest stays empty; the digest is
//     recorded when the deploy layer pushes the image.
func (e *BuildpacksEngine) Build(ctx context.Context, opts BuildOptions) (ImageRef, error) {
	if err := validateOptions(opts); err != nil {
		return ImageRef{}, err
	}
	tag := ImageTag(opts.AppID, opts.DeployID)
	args := []string{"build", tag, "--path", "."}
	args = append(args, buildEnvFlags(opts.BuildArgs)...)

	run := cliRun{name: packCLI, installHint: packInstallHint, dir: opts.RepoDir, args: args}
	if err := run.available(); err != nil {
		return ImageRef{}, err
	}
	if err := run.exec(ctx, opts); err != nil {
		return ImageRef{}, err
	}
	return ImageRef{Kind: EngineBuildpacks, Tag: tag}, nil
}
