package builds

import (
	"context"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/google/uuid"
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
	r.Register(NewRailpackEngine())
	r.Register(NewBuildpacksEngine())
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

// Detect returns the engine that should build repoDir. An explicit hint is
// honoured directly; EngineAuto walks the priority order in EngineKinds.
func (r *Registry) Detect(repoDir string, hint EngineKind) (BuildEngine, bool) {
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
		if engine, ok := r.engines[kind]; ok && engine.Detect(repoDir, EngineAuto) {
			return engine, true
		}
	}
	return nil, false
}

// Build resolves an engine from opts.BuildPack (auto-detecting when empty) and
// runs it, returning the standardized image reference.
func (r *Registry) Build(ctx context.Context, opts BuildOptions) (ImageRef, error) {
	if err := validateOptions(opts); err != nil {
		return ImageRef{}, err
	}
	engine, ok := r.Detect(opts.RepoDir, opts.BuildPack)
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
