package deploy

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/google/uuid"
)

// fakeToolStatus returns a fixed installed-tool set (or an error) for any node.
type fakeToolStatus struct {
	installed map[string]bool
	err       error
}

func (f fakeToolStatus) InstalledToolIDs(_ context.Context, _ uuid.UUID) (map[string]bool, error) {
	return f.installed, f.err
}

// gateService is a Service with just enough wired to exercise the build-pack
// gate: the node-tool lookup and a logger for the fail-open warning path.
func gateService(status ToolStatus) *Service {
	return &Service{
		Orchestrator: &Orchestrator{logger: slog.Default(), toolStatus: status},
	}
}

// TestValidateBuildToolchain checks the JUS-179 deploy gate: a build pack whose
// external tool is absent on the target node is refused with ErrValidation
// (400), the engines that need no CLI always pass, and an inconclusive lookup
// fails open so buildtool.Available stays the last guard.
func TestValidateBuildToolchain(t *testing.T) {
	node := uuid.New()
	app := func(pack string) Application {
		return Application{ID: uuid.New(), ServerID: node, BuildPack: pack}
	}
	cases := []struct {
		name    string
		status  ToolStatus
		pack    string
		wantErr bool
	}{
		{"no toolchain surface allows", nil, "railpack", false},
		{"dockerfile needs no CLI", fakeToolStatus{installed: map[string]bool{}}, "dockerfile", false},
		{"static needs no CLI", fakeToolStatus{installed: map[string]bool{}}, "static", false},
		{"auto resolves at build time", fakeToolStatus{installed: map[string]bool{}}, "", false},
		{"railpack and deps present", fakeToolStatus{installed: map[string]bool{"docker": true, "buildkit": true, "railpack": true}}, "railpack", false},
		{"railpack missing", fakeToolStatus{installed: map[string]bool{"docker": true, "buildkit": true}}, "railpack", true},
		{"railpack present but buildkit missing", fakeToolStatus{installed: map[string]bool{"docker": true, "railpack": true}}, "railpack", true},
		{"buildpacks present", fakeToolStatus{installed: map[string]bool{"pack": true}}, "buildpacks", false},
		{"buildpacks missing", fakeToolStatus{installed: map[string]bool{}}, "buildpacks", true},
		{"lookup error fails open", fakeToolStatus{err: errors.New("agent unreachable")}, "railpack", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := gateService(tc.status).validateBuildToolchain(context.Background(), app(tc.pack))
			if tc.wantErr {
				if !errors.Is(err, ErrValidation) {
					t.Fatalf("validateBuildToolchain(%q) = %v, want ErrValidation", tc.pack, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("validateBuildToolchain(%q) = %v, want nil", tc.pack, err)
			}
		})
	}
}

// TestValidateBuildToolchainSkipsUnassignedNode keeps the gate out of the way
// for an application with no node: validateDeployTarget reports that shape.
func TestValidateBuildToolchainSkipsUnassignedNode(t *testing.T) {
	svc := gateService(fakeToolStatus{installed: map[string]bool{}})
	if err := svc.validateBuildToolchain(context.Background(), Application{BuildPack: "railpack"}); err != nil {
		t.Fatalf("validateBuildToolchain with no node = %v, want nil", err)
	}
}

// TestValidateBuildToolchainSkipsNonBuildSources checks the JUS-179 review
// follow-up: a dockerfile/compose/image row that still carries a stale
// railpack/buildpacks build_pack (e.g. after a source-type change) must not get
// a false 4xx, because those sources never build with that pack.
func TestValidateBuildToolchainSkipsNonBuildSources(t *testing.T) {
	node := uuid.New()
	svc := gateService(fakeToolStatus{installed: map[string]bool{}})
	for _, sourceType := range []string{SourceDockerfile, SourceCompose, SourceImage} {
		app := Application{ID: uuid.New(), ServerID: node, SourceType: sourceType, BuildPack: "railpack"}
		if err := svc.validateBuildToolchain(context.Background(), app); err != nil {
			t.Errorf("validateBuildToolchain(source %q, railpack) = %v, want nil", sourceType, err)
		}
	}
}
