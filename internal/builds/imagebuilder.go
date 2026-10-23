package builds

import "context"

type ImageBuildOptions struct {
	Tag        string
	Engine     EngineKind
	Dockerfile string
	BuildArgs  map[string]string
	Labels     map[string]string
	Target     string
}

type ImageBuildResult struct {
	Digest string
	Logs   string
}

type ImageBuilder interface {
	Build(ctx context.Context, contextTar []byte, opts ImageBuildOptions) (ImageBuildResult, error)
}
