package builds

import (
	"context"
	"path/filepath"

	"github.com/justindeelux/gotham/buildtool"
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

// packCLI is the Cloud Native Buildpacks command-line tool the engine drives.
// It is re-exported so package tests keep naming the CLI.
const packCLI = buildtool.BuildpacksCLI

// BuildpacksEngine detects Cloud Native Buildpacks projects and builds them. A
// build is dispatched to the configured ImageBuilder when one is set (the node
// agent runs the toolchain on the node); with no builder the toolchain runs in
// the control-plane process, which is the dev/E2E fallback.
type BuildpacksEngine struct {
	builder ImageBuilder
}

// NewBuildpacksEngine returns the Buildpacks engine. builder may be nil.
func NewBuildpacksEngine(builder ImageBuilder) *BuildpacksEngine {
	return &BuildpacksEngine{builder: builder}
}

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

// Build implements BuildEngine. When a builder is configured the source tree is
// packaged and streamed to it (the node agent), which runs `pack build` on the
// node and pushes the image to the node-local registry; otherwise the CLI runs
// in this process.
func (e *BuildpacksEngine) Build(ctx context.Context, opts BuildOptions) (ImageRef, error) {
	if err := validateOptions(opts); err != nil {
		return ImageRef{}, err
	}
	tag := ImageTag(opts.AppID, opts.DeployID)
	if e.builder == nil {
		if err := buildtool.Run(ctx, buildtool.Buildpacks, buildtool.Options{
			Dir:       opts.RepoDir,
			Tag:       tag,
			BuildArgs: opts.BuildArgs,
			LogWriter: opts.LogWriter,
		}); err != nil {
			return ImageRef{}, err
		}
		return ImageRef{Kind: EngineBuildpacks, Tag: tag}, nil
	}
	return runToolchainOnBuilder(ctx, e.builder, EngineBuildpacks, opts, tag)
}
