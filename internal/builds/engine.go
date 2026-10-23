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

type EngineKind string

const (
	EngineAuto       EngineKind = ""
	EngineDockerfile EngineKind = "dockerfile"
	EngineRailpack   EngineKind = "railpack"
	EngineBuildpacks EngineKind = "buildpacks"
	EngineStatic     EngineKind = "static"
)

var EngineKinds = []EngineKind{EngineDockerfile, EngineRailpack, EngineBuildpacks, EngineStatic}

func EngineTool(kind EngineKind) string {
	switch kind {
	case EngineRailpack:
		return toolcatalog.IDRailpack
	case EngineBuildpacks:
		return toolcatalog.IDPack
	default:
		return ""
	}
}

func EngineTools(kind EngineKind) []string {
	toolID := EngineTool(kind)
	if toolID == "" {
		return nil
	}
	order, err := toolcatalog.InstallOrder(toolID)
	if err != nil {
		return nil
	}
	return order
}

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

type BuildOptions struct {
	RepoDir        string
	AppID          uuid.UUID
	DeployID       uuid.UUID
	BuildPack      EngineKind
	BuildArgs      map[string]string
	Labels         map[string]string
	Target         string
	LogWriter      io.Writer
	InstalledTools map[string]bool
}

type ImageRef struct {
	Kind   EngineKind
	Tag    string
	Digest string
}

func ImageTag(appID, deployID uuid.UUID) string {
	return fmt.Sprintf("gotham/%s:%s", appID, deployID)
}

type BuildEngine interface {
	Kind() EngineKind
	Detect(repoDir string, hint EngineKind) bool
	Build(ctx context.Context, opts BuildOptions) (ImageRef, error)
}

type Registry struct {
	engines map[EngineKind]BuildEngine
	order   []EngineKind
}

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

func (r *Registry) Register(engine BuildEngine) {
	if engine == nil {
		panic("builds: Register called with nil engine")
	}
	r.engines[engine.Kind()] = engine
}

func (r *Registry) Engine(kind EngineKind) (BuildEngine, bool) {
	engine, ok := r.engines[kind]
	return engine, ok
}

func (r *Registry) Detect(repoDir string, hint EngineKind) (BuildEngine, bool) {
	return r.detect(repoDir, hint, nil)
}

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

func (r *Registry) Build(ctx context.Context, opts BuildOptions) (ImageRef, error) {
	if err := validateOptions(opts); err != nil {
		return ImageRef{}, err
	}
	engine, ok := r.detect(opts.RepoDir, opts.BuildPack, opts.InstalledTools)
	if !ok {
		if opts.BuildPack != EngineAuto {
			return ImageRef{}, fmt.Errorf("%w: engine %q cannot build %s", ErrNoEngine, opts.BuildPack, opts.RepoDir)
		}
		if engine, missing, found := r.autoMissingTool(opts.RepoDir, opts.InstalledTools); found {
			return ImageRef{}, fmt.Errorf("%w: %s is not installed on this node; install it from the App Store (or pick a different build pack) to build with the %s engine",
				ErrNoEngine, missing, engine)
		}
		return ImageRef{}, fmt.Errorf("%w: %s", ErrNoEngine, opts.RepoDir)
	}
	opts.BuildPack = engine.Kind()
	return engine.Build(ctx, opts)
}

func (r *Registry) autoMissingTool(repoDir string, installed map[string]bool) (EngineKind, string, bool) {
	if installed == nil {
		return EngineAuto, "", false
	}
	engine, ok := r.detect(repoDir, EngineAuto, nil)
	if !ok {
		return EngineAuto, "", false
	}
	toolID := EngineTool(engine.Kind())
	if toolID == "" || installed[toolID] {
		return EngineAuto, "", false
	}
	return engine.Kind(), toolID, true
}

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

func writeLogs(w io.Writer, logs string) {
	if w == nil || logs == "" {
		return
	}
	_, _ = io.WriteString(w, logs)
}

func sortedKeys(m map[string][]byte) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
