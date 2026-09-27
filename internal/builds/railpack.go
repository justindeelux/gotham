package builds

import (
	"context"
	"fmt"
	"path/filepath"
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

// RailpackEngine detects language projects for Railpack. Build is a follow-up
// work package: it returns ErrEngineNotWired rather than a fabricated image.
type RailpackEngine struct{}

// NewRailpackEngine returns the Railpack engine.
func NewRailpackEngine() *RailpackEngine { return &RailpackEngine{} }

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

// Build implements BuildEngine. Railpack wiring is a follow-up package; the
// error is deliberate so callers fail loudly instead of shipping a fake image.
func (e *RailpackEngine) Build(context.Context, BuildOptions) (ImageRef, error) {
	return ImageRef{}, fmt.Errorf("%w: %s", ErrEngineNotWired, EngineRailpack)
}
