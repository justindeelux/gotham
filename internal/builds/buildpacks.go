package builds

import (
	"context"
	"path/filepath"

	"github.com/justindeelux/gotham/buildtool"
)

var buildpacksMarkers = []string{
	"project.toml",
	"Procfile",
	"app.json",
	"heroku.yml",
	"buildpack.toml",
}

const packCLI = buildtool.BuildpacksCLI

type BuildpacksEngine struct {
	builder ImageBuilder
}

func NewBuildpacksEngine(builder ImageBuilder) *BuildpacksEngine {
	return &BuildpacksEngine{builder: builder}
}

func (e *BuildpacksEngine) Kind() EngineKind { return EngineBuildpacks }

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
