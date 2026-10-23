package builds

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/justindeelux/gotham/buildtool"
)

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

const railpackCLI = buildtool.RailpackCLI

var ErrCLIMissing = buildtool.ErrCLIMissing

type RailpackEngine struct {
	builder ImageBuilder
}

func NewRailpackEngine(builder ImageBuilder) *RailpackEngine {
	return &RailpackEngine{builder: builder}
}

func (e *RailpackEngine) Kind() EngineKind { return EngineRailpack }

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
