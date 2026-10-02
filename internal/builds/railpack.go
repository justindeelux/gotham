package builds

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/justindeelux/gotham/buildtool"
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

// railpackCLI is the Railpack command-line tool the engine drives. It is
// re-exported so package tests keep naming the CLI.
const railpackCLI = buildtool.RailpackCLI

// ErrCLIMissing reports a build whose external toolchain is unavailable on the
// host that runs it. It aliases the shared buildtool error so both the
// control-plane local path and the node agent report the same sentinel.
var ErrCLIMissing = buildtool.ErrCLIMissing

// RailpackEngine detects language projects for Railpack and builds them. A
// build is dispatched to the configured ImageBuilder when one is set (the node
// agent runs the toolchain on the node); with no builder the toolchain runs in
// the control-plane process, which is the dev/E2E fallback.
type RailpackEngine struct {
	builder ImageBuilder
}

// NewRailpackEngine returns the Railpack engine. builder may be nil.
func NewRailpackEngine(builder ImageBuilder) *RailpackEngine {
	return &RailpackEngine{builder: builder}
}

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

// Build implements BuildEngine. When a builder is configured the source tree is
// packaged and streamed to it (the node agent), which runs `railpack build`
// on the node and pushes the image to the node-local registry; otherwise the
// CLI runs in this process. In both cases the resulting image carries the
// standardized tag from ImageTag.
func (e *RailpackEngine) Build(ctx context.Context, opts BuildOptions) (ImageRef, error) {
	if err := validateOptions(opts); err != nil {
		return ImageRef{}, err
	}
	tag := ImageTag(opts.AppID, opts.DeployID)
	if e.builder == nil {
		if err := buildtool.Run(ctx, buildtool.Railpack, buildtool.Options{
			Dir:       opts.RepoDir,
			Tag:       tag,
			BuildArgs: opts.BuildArgs,
			LogWriter: opts.LogWriter,
		}); err != nil {
			return ImageRef{}, err
		}
		return ImageRef{Kind: EngineRailpack, Tag: tag}, nil
	}
	return runToolchainOnBuilder(ctx, e.builder, EngineRailpack, opts, tag)
}

// runToolchainOnBuilder packages the repository as a raw source context and
// hands it to builder with the engine named. The builder (the node agent)
// extracts the context, runs the toolchain and pushes the image, so a
// control-plane host does not need the CLI installed.
func runToolchainOnBuilder(ctx context.Context, builder ImageBuilder, kind EngineKind, opts BuildOptions, tag string) (ImageRef, error) {
	contextTar, err := buildContextTar(contextSpec{root: opts.RepoDir})
	if err != nil {
		return ImageRef{}, err
	}
	result, err := builder.Build(ctx, contextTar, ImageBuildOptions{
		Tag:       tag,
		Engine:    kind,
		BuildArgs: opts.BuildArgs,
		Labels:    opts.Labels,
	})
	writeLogs(opts.LogWriter, result.Logs)
	if err != nil {
		return ImageRef{}, fmt.Errorf("%s build: %w", strings.TrimSpace(string(kind)), err)
	}
	return ImageRef{Kind: kind, Tag: tag, Digest: result.Digest}, nil
}
