package builds

import "context"

// ImageBuildOptions describes a single image build on the builder side.
type ImageBuildOptions struct {
	// Tag is the fully qualified image tag the builder must apply.
	Tag string
	// Engine names the build engine the builder must run. The zero value and
	// EngineDockerfile/EngineStatic mean "build the context as a Dockerfile";
	// EngineRailpack and EngineBuildpacks mean "run the toolchain in the
	// extracted context".
	Engine EngineKind
	// Dockerfile is the Dockerfile path relative to the context root.
	Dockerfile string
	// BuildArgs are passed as --build-arg pairs.
	BuildArgs map[string]string
	// Labels are applied to the resulting image.
	Labels map[string]string
	// Target selects a multi-stage build target; empty builds the last stage.
	Target string
}

// ImageBuildResult is the builder's output for a single build.
type ImageBuildResult struct {
	// Digest identifies the built image (a content digest when the builder
	// reports one, otherwise the image ID).
	Digest string
	// Logs is the captured build output, forwarded to the deploy log stream.
	Logs string
}

// ImageBuilder is the seam between the build engines and the node that runs
// the build. The control plane clones the repository, packages the context as
// a tarball, and hands it to the builder together with the target options.
//
// The production implementation is the agent BuildImage RPC added by the
// sibling P4-BAGENT work package; declaring the contract here keeps
// internal/builds independent of the agent protobuf and mergeable in any
// order. Dev and test use LocalDockerBuilder.
type ImageBuilder interface {
	Build(ctx context.Context, contextTar []byte, opts ImageBuildOptions) (ImageBuildResult, error)
}
