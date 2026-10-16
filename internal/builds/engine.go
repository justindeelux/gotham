package builds

import (
	"context"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/google/uuid"

	"github.com/justindeelux/gotham/toolcatalog"
)

// EngineKind identifies one of the four build engines. EngineAuto is the
// sentinel a caller passes when the build pack should be detected from the
// repository; it is never a valid engine identity.
type EngineKind string

const (
	// EngineAuto asks Detect to pick an engine from the repository contents.
	EngineAuto EngineKind = ""
	// EngineDockerfile builds from a Dockerfile committed to the repository.
	EngineDockerfile EngineKind = "dockerfile"
	// EngineRailpack builds without a Dockerfile using Railpack.
	EngineRailpack EngineKind = "railpack"
	// EngineBuildpacks builds without a Dockerfile using Cloud Native
	// Buildpacks (herokuish-style).
	EngineBuildpacks EngineKind = "buildpacks"
	// EngineStatic serves a pre-built static site from an nginx image.
	EngineStatic EngineKind = "static"
)

// EngineKinds lists the canonical engines in auto-detection priority order.
var EngineKinds = []EngineKind{EngineDockerfile, EngineRailpack, EngineBuildpacks, EngineStatic}

// EngineTools returns the catalog tool ids a build engine needs installed on
// the node, dependencies first (the same order toolcatalog.InstallOrder
// produces). It is empty for the engines that shell out to no external CLI:
// Dockerfile builds through the node Docker daemon and static is served by
// nginx, so both always run; EngineAuto resolves at build time and names no
// single tool. The deploy gate and the Web picker share this mapping.
func EngineTools(kind EngineKind) []string {
	var toolID string
	switch kind {
	case EngineRailpack:
		toolID = toolcatalog.IDRailpack
	case EngineBuildpacks:
		toolID = toolcatalog.IDPack
	default:
		return nil
	}
	order, err := toolcatalog.InstallOrder(toolID)
	if err != nil {
		return nil
	}
	return order
}

// ParseEngineKind normalises a build_pack value from a request or the database
// into an EngineKind. Nixpacks and herokuish are accepted as aliases for their
// replacements. Unknown values return ErrValidation.
func ParseEngineKind(s string) (EngineKind, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "auto", "detect":
		return EngineAuto, nil
	case string(EngineDockerfile):
		return EngineDockerfile, nil
	case string(EngineRailpack), "nixpacks":
		return EngineRailpack, nil
	case string(EngineBuildpacks), "buildpack", "herokuish", "cnb":
		return EngineBuildpacks, nil
	case string(EngineStatic):
		return EngineStatic, nil
	default:
		return EngineAuto, fmt.Errorf("%w: unknown build pack %q", ErrValidation, s)
	}
}

// BuildOptions is the input to an engine Build. RepoDir points at the
// already-cloned source tree on the control plane; AppID and DeployID seed the
// standardized image tag.
type BuildOptions struct {
	RepoDir   string
	AppID     uuid.UUID
	DeployID  uuid.UUID
	BuildPack EngineKind
	BuildArgs map[string]string
	Labels    map[string]string
	Target    string
	// LogWriter, when non-nil, receives the builder's captured build output.
	LogWriter io.Writer
	// InstalledTools, when non-nil, is the target node's catalog tool state
	// (id -> present). EngineAuto then only considers engines whose tools are
	// all present, so a build pack missing on the node is never auto-selected.
	// nil keeps marker-only detection (the control-plane local dev path).
	InstalledTools map[string]bool
}

// ImageRef is the result of a successful engine Build.
type ImageRef struct {
	Kind   EngineKind
	Tag    string
	Digest string
}

// ImageTag returns the standardized image tag for an application deployment.
func ImageTag(appID, deployID uuid.UUID) string {
	return fmt.Sprintf("gotham/%s:%s", appID, deployID)
}

// BuildEngine turns a cloned source tree into a runnable image. Detect reports
// whether the engine recognises the repository: when hint names the engine
// explicitly the caller has already chosen it, so Detect returns true without
// inspecting the tree.
type BuildEngine interface {
	Kind() EngineKind
	Detect(repoDir string, hint EngineKind) bool
	Build(ctx context.Context, opts BuildOptions) (ImageRef, error)
}

// Registry holds the known build engines and dispatches detection and builds.
type Registry struct {
	engines map[EngineKind]BuildEngine
	order   []EngineKind
}

// NewRegistry wires the four engines around an ImageBuilder. The builder is the
// shared seam to the node agent (or to a local Docker daemon in dev/test); a
// nil builder still allows detection, but any Build fails with ErrValidation.
func NewRegistry(builder ImageBuilder) *Registry {
	r := &Registry{
		engines: make(map[EngineKind]BuildEngine, len(EngineKinds)),
		order:   append([]EngineKind(nil), EngineKinds...),
	}
	r.Register(NewDockerfileEngine(builder))
	r.Register(NewRailpackEngine(builder))
	r.Register(NewBuildpacksEngine(builder))
	r.Register(NewStaticEngine(builder))
	return r
}

// Register adds or replaces an engine. A nil engine is a programming error.
func (r *Registry) Register(engine BuildEngine) {
	if engine == nil {
		panic("builds: Register called with nil engine")
	}
	r.engines[engine.Kind()] = engine
}

// Engine returns the engine registered for kind.
func (r *Registry) Engine(kind EngineKind) (BuildEngine, bool) {
	engine, ok := r.engines[kind]
	return engine, ok
}

// Detect returns the engine that should build repoDir with marker-only
// detection (no target-node tool state). An explicit hint is honoured
// directly; EngineAuto walks the priority order in EngineKinds.
func (r *Registry) Detect(repoDir string, hint EngineKind) (BuildEngine, bool) {
	return r.detect(repoDir, hint, nil)
}

// detect is Detect with the target node's installed tools. When installed is
// non-nil, the auto walk skips engines whose tools are not all present, so a
// pack missing on the node is never auto-selected; an explicit hint is still
// honoured (buildtool.Available is the last guard at build time). A nil set
// keeps marker-only detection (the control-plane local dev path).
func (r *Registry) detect(repoDir string, hint EngineKind, installed map[string]bool) (BuildEngine, bool) {
	if strings.TrimSpace(repoDir) == "" {
		return nil, false
	}
	if hint != EngineAuto {
		engine, ok := r.engines[hint]
		if !ok || !engine.Detect(repoDir, hint) {
			return nil, false
		}
		return engine, true
	}
	for _, kind := range r.order {
		if !engineToolsInstalled(kind, installed) {
			continue
		}
		if engine, ok := r.engines[kind]; ok && engine.Detect(repoDir, EngineAuto) {
			return engine, true
		}
	}
	return nil, false
}

// engineToolsInstalled reports whether every tool an engine needs is present.
// A nil set means "unknown": the engine is considered, so marker-only
// detection is unchanged. Engines with no external CLI (Dockerfile, static)
// are always considered.
func engineToolsInstalled(kind EngineKind, installed map[string]bool) bool {
	if installed == nil {
		return true
	}
	for _, toolID := range EngineTools(kind) {
		if !installed[toolID] {
			return false
		}
	}
	return true
}

// Build resolves an engine from opts.BuildPack (auto-detecting when empty) and
// runs it, returning the standardized image reference.
func (r *Registry) Build(ctx context.Context, opts BuildOptions) (ImageRef, error) {
	if err := validateOptions(opts); err != nil {
		return ImageRef{}, err
	}
	engine, ok := r.detect(opts.RepoDir, opts.BuildPack, opts.InstalledTools)
	if !ok {
		if opts.BuildPack != EngineAuto {
			return ImageRef{}, fmt.Errorf("%w: engine %q cannot build %s", ErrNoEngine, opts.BuildPack, opts.RepoDir)
		}
		return ImageRef{}, fmt.Errorf("%w: %s", ErrNoEngine, opts.RepoDir)
	}
	opts.BuildPack = engine.Kind()
	return engine.Build(ctx, opts)
}

// validateOptions rejects a build request that cannot produce a tag.
func validateOptions(opts BuildOptions) error {
	if strings.TrimSpace(opts.RepoDir) == "" {
		return fmt.Errorf("%w: empty repo directory", ErrValidation)
	}
	if opts.AppID == uuid.Nil {
		return fmt.Errorf("%w: empty app id", ErrValidation)
	}
	if opts.DeployID == uuid.Nil {
		return fmt.Errorf("%w: empty deploy id", ErrValidation)
	}
	return nil
}

// writeLogs forwards captured build output to the caller's log stream.
func writeLogs(w io.Writer, logs string) {
	if w == nil || logs == "" {
		return
	}
	_, _ = io.WriteString(w, logs)
}

// sortedKeys returns the map keys in deterministic order.
func sortedKeys(m map[string][]byte) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
