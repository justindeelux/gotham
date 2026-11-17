package builds

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

var dockerfileNames = []string{"Dockerfile", "dockerfile"}

type DockerfileEngine struct {
	builder ImageBuilder
}

func NewDockerfileEngine(builder ImageBuilder) *DockerfileEngine {
	return &DockerfileEngine{builder: builder}
}

func (e *DockerfileEngine) Kind() EngineKind { return EngineDockerfile }

func (e *DockerfileEngine) Detect(repoDir string, hint EngineKind) bool {
	if hint != EngineAuto {
		return hint == EngineDockerfile
	}
	_, ok := findDockerfile(repoDir)
	return ok
}

func (e *DockerfileEngine) Build(ctx context.Context, opts BuildOptions) (ImageRef, error) {
	if err := validateOptions(opts); err != nil {
		return ImageRef{}, err
	}
	dockerfile, ok := findDockerfile(opts.RepoDir)
	if name := strings.TrimSpace(opts.Dockerfile); name != "" {
		if err := checkDockerfilePath(opts.RepoDir, name); err != nil {
			return ImageRef{}, err
		}
		dockerfile, ok = filepath.ToSlash(filepath.Clean(name)), true
	}
	if !ok {
		return ImageRef{}, fmt.Errorf("%w: no Dockerfile in %s", ErrValidation, opts.RepoDir)
	}
	contextTar, err := buildContextTar(contextSpec{root: opts.RepoDir, keep: dockerfile})
	if err != nil {
		return ImageRef{}, err
	}
	return e.run(ctx, opts, contextTar, dockerfile)
}

func (e *DockerfileEngine) run(ctx context.Context, opts BuildOptions, contextTar []byte, dockerfile string) (ImageRef, error) {
	if e.builder == nil {
		return ImageRef{}, fmt.Errorf("%w: no image builder configured", ErrValidation)
	}
	tag := opts.Tag
	if tag == "" {
		tag = ImageTag(opts.AppID, opts.DeployID)
	}
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

// checkDockerfilePath keeps the path inside the build context: a compose
// service names its Dockerfile relative to its own build context.
func checkDockerfilePath(dir, name string) error {
	cleaned := filepath.Clean(name)
	if filepath.IsAbs(cleaned) || cleaned == ".." ||
		strings.HasPrefix(cleaned, ".."+string(filepath.Separator)) {
		return fmt.Errorf("%w: Dockerfile path %q escapes the build context", ErrValidation, name)
	}
	info, err := os.Lstat(filepath.Join(dir, cleaned))
	if err != nil || !info.Mode().IsRegular() {
		return fmt.Errorf("%w: Dockerfile %q is not a file in the build context", ErrValidation, name)
	}
	return nil
}

func findDockerfile(dir string) (string, bool) {
	for _, name := range dockerfileNames {
		if isRegular(filepath.Join(dir, name)) {
			return name, true
		}
	}
	return "", false
}
