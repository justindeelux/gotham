package builds

import (
	"context"
	"fmt"
	"path/filepath"
)

const (
	staticDockerfile   = "Dockerfile.gotham"
	staticBaseImage    = "nginx:1.27-alpine"
	staticDockerIgnore = "Dockerfile.gotham\n.dockerignore\n"
)

func staticDockerfileContent() []byte {
	return []byte("FROM " + staticBaseImage + "\n" +
		"COPY . /usr/share/nginx/html\n" +
		"EXPOSE 80\n")
}

type StaticEngine struct {
	builder ImageBuilder
}

func NewStaticEngine(builder ImageBuilder) *StaticEngine {
	return &StaticEngine{builder: builder}
}

func (e *StaticEngine) Kind() EngineKind { return EngineStatic }

func (e *StaticEngine) Detect(repoDir string, hint EngineKind) bool {
	if hint != EngineAuto {
		return hint == EngineStatic
	}
	_, ok := staticRoot(repoDir)
	return ok
}

func (e *StaticEngine) Build(ctx context.Context, opts BuildOptions) (ImageRef, error) {
	if err := validateOptions(opts); err != nil {
		return ImageRef{}, err
	}
	root, ok := staticRoot(opts.RepoDir)
	if !ok {
		return ImageRef{}, fmt.Errorf("%w: no index.html in %s", ErrValidation, opts.RepoDir)
	}
	if e.builder == nil {
		return ImageRef{}, fmt.Errorf("%w: no image builder configured", ErrValidation)
	}
	contextTar, err := buildContextTar(contextSpec{
		root:      root,
		ignoreDir: opts.RepoDir,
		extra: map[string][]byte{
			staticDockerfile: staticDockerfileContent(),
			".dockerignore":  []byte(staticDockerIgnore),
		},
	})
	if err != nil {
		return ImageRef{}, err
	}
	tag := ImageTag(opts.AppID, opts.DeployID)
	result, err := e.builder.Build(ctx, contextTar, ImageBuildOptions{
		Tag:        tag,
		Dockerfile: staticDockerfile,
		BuildArgs:  opts.BuildArgs,
		Labels:     opts.Labels,
	})
	writeLogs(opts.LogWriter, result.Logs)
	if err != nil {
		return ImageRef{}, fmt.Errorf("static build: %w", err)
	}
	return ImageRef{Kind: EngineStatic, Tag: tag, Digest: result.Digest}, nil
}

func staticRoot(repoDir string) (string, bool) {
	if isRegular(filepath.Join(repoDir, "index.html")) {
		return repoDir, true
	}
	public := filepath.Join(repoDir, "public")
	if isRegular(filepath.Join(public, "index.html")) {
		return public, true
	}
	return "", false
}
