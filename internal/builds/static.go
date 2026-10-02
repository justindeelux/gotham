package builds

import (
	"context"
	"fmt"
	"path/filepath"
)

const (
	// staticDockerfile is the synthesized Dockerfile name inside the context.
	// It is named to avoid clashing with a user Dockerfile (in which case the
	// Dockerfile engine would have matched instead).
	staticDockerfile = "Dockerfile.gotham"
	// staticBaseImage is the nginx image a static site is served from.
	staticBaseImage = "nginx:1.27-alpine"
	// staticDockerIgnore keeps the synthesized files out of the served site.
	staticDockerIgnore = "Dockerfile.gotham\n.dockerignore\n"
)

// staticDockerfileContent is the nginx image definition the static engine
// synthesizes for every static site.
func staticDockerfileContent() []byte {
	return []byte("FROM " + staticBaseImage + "\n" +
		"COPY . /usr/share/nginx/html\n" +
		"EXPOSE 80\n")
}

// StaticEngine serves a pre-built static site from an nginx image.
type StaticEngine struct {
	builder ImageBuilder
}

// NewStaticEngine returns a static engine that delegates builds to builder.
func NewStaticEngine(builder ImageBuilder) *StaticEngine {
	return &StaticEngine{builder: builder}
}

// Kind implements BuildEngine.
func (e *StaticEngine) Kind() EngineKind { return EngineStatic }

// Detect implements BuildEngine. An explicit static hint is honoured without
// inspecting the tree.
func (e *StaticEngine) Detect(repoDir string, hint EngineKind) bool {
	if hint != EngineAuto {
		return hint == EngineStatic
	}
	_, ok := staticRoot(repoDir)
	return ok
}

// Build implements BuildEngine. The site is packed into a context whose root
// is the directory that owns index.html, alongside a synthesized nginx
// Dockerfile.
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
	// The repository's .dockerignore is honoured from the repository root even
	// when the site is rooted at public/: patterns are matched repository-
	// relative, so `public/.env` excludes public/.env while a bare `index.html`
	// matches only the repository root and cannot break a public/ site. The
	// synthesized .dockerignore keeps Dockerfile.gotham (and itself) out of the
	// served site: the daemon does not exclude a named Dockerfile from COPY on
	// its own, but it does when the file is listed in the context's
	// .dockerignore.
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

// staticRoot returns the directory holding index.html: the repository root when
// it has index.html, otherwise a public/ subdirectory.
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
