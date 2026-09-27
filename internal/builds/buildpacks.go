package builds

import (
	"context"
	"fmt"
	"path/filepath"
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

// BuildpacksEngine detects Cloud Native Buildpacks projects. Build is a
// follow-up work package: it returns ErrEngineNotWired rather than a
// fabricated image.
type BuildpacksEngine struct{}

// NewBuildpacksEngine returns the Buildpacks engine.
func NewBuildpacksEngine() *BuildpacksEngine { return &BuildpacksEngine{} }

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

// Build implements BuildEngine. Buildpacks wiring is a follow-up package; the
// error is deliberate so callers fail loudly instead of shipping a fake image.
func (e *BuildpacksEngine) Build(context.Context, BuildOptions) (ImageRef, error) {
	return ImageRef{}, fmt.Errorf("%w: %s", ErrEngineNotWired, EngineBuildpacks)
}
