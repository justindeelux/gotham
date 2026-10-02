package builds

import (
	"context"
	"fmt"
	"path/filepath"
)

// dockerfileNames are the conventional Dockerfile filenames searched in the
// repository root, in priority order.
var dockerfileNames = []string{"Dockerfile", "dockerfile"}

// DockerfileEngine builds repositories that commit their own Dockerfile.
type DockerfileEngine struct {
	builder ImageBuilder
}

// NewDockerfileEngine returns a Dockerfile engine that delegates builds to
// builder.
func NewDockerfileEngine(builder ImageBuilder) *DockerfileEngine {
	return &DockerfileEngine{builder: builder}
}

// Kind implements BuildEngine.
func (e *DockerfileEngine) Kind() EngineKind { return EngineDockerfile }

// Detect implements BuildEngine. An explicit Dockerfile hint is honoured
// without inspecting the tree.
func (e *DockerfileEngine) Detect(repoDir string, hint EngineKind) bool {
	if hint != EngineAuto {
		return hint == EngineDockerfile
	}
	_, ok := findDockerfile(repoDir)
	return ok
}

// Build implements BuildEngine.
func (e *DockerfileEngine) Build(ctx context.Context, opts BuildOptions) (ImageRef, error) {
	if err := validateOptions(opts); err != nil {
		return ImageRef{}, err
	}
	dockerfile, ok := findDockerfile(opts.RepoDir)
	if !ok {
		return ImageRef{}, fmt.Errorf("%w: no Dockerfile in %s", ErrValidation, opts.RepoDir)
	}
	contextTar, err := buildContextTar(contextSpec{root: opts.RepoDir, keep: dockerfile})
	if err != nil {
		return ImageRef{}, err
	}
	return e.run(ctx, opts, contextTar, dockerfile)
}

// run delegates a prepared context to the image builder.
func (e *DockerfileEngine) run(ctx context.Context, opts BuildOptions, contextTar []byte, dockerfile string) (ImageRef, error) {
	if e.builder == nil {
		return ImageRef{}, fmt.Errorf("%w: no image builder configured", ErrValidation)
	}
	tag := ImageTag(opts.AppID, opts.DeployID)
	result, err := e.builder.Build(ctx, contextTar, ImageBuildOptions{
		Tag:        tag,
		Dockerfile: dockerfile,
		BuildArgs:  opts.BuildArgs,
		Labels:     opts.Labels,
		Target:     opts.Target,
	})
	writeLogs(opts.LogWriter, result.Logs)
	if err != nil {
		return ImageRef{}, fmt.Errorf("dockerfile build: %w", err)
	}
	return ImageRef{Kind: EngineDockerfile, Tag: tag, Digest: result.Digest}, nil
}

// findDockerfile returns the Dockerfile path relative to dir, if present.
func findDockerfile(dir string) (string, bool) {
	for _, name := range dockerfileNames {
		if isRegular(filepath.Join(dir, name)) {
			return name, true
		}
	}
	return "", false
}
