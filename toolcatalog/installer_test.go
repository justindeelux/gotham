package toolcatalog

import (
	"errors"
	"slices"
	"strings"
	"testing"
)

func TestPlatformToolsCannotBeUninstalled(t *testing.T) {
	for _, id := range []string{IDGit, IDDocker} {
		tool := mustLookup(t, id)
		if !tool.Installable || tool.Uninstallable {
			t.Errorf("%s must remain installable and not uninstallable", id)
		}
		_, err := UninstallOrder(id)
		if !errors.Is(err, ErrNotUninstallable) || !strings.Contains(err.Error(), "required by the platform") || !strings.Contains(err.Error(), tool.Description) {
			t.Errorf("UninstallOrder(%s) = %v; want refusal with platform impact", id, err)
		}
	}
}

func TestInstallOrderOrdersDependencies(t *testing.T) {
	order, err := InstallOrder(IDRailpack)
	if err != nil {
		t.Fatalf("InstallOrder(railpack): %v", err)
	}
	want := []string{IDDocker, IDBuildKit, IDRailpack}
	if !slices.Equal(order, want) {
		t.Fatalf("InstallOrder(railpack) = %v; want %v", order, want)
	}

	order, err = InstallOrder(IDDocker)
	if err != nil {
		t.Fatalf("InstallOrder(docker): %v", err)
	}
	if !slices.Equal(order, []string{IDDocker}) {
		t.Fatalf("InstallOrder(docker) = %v; want [docker]", order)
	}
}

func TestInstallOrderRefusesDetectOnly(t *testing.T) {
	for _, id := range []string{IDSystemdNetworkd, IDSystemdResolved, IDNetworkctl, IDSystemctl} {
		if _, err := InstallOrder(id); !errors.Is(err, ErrNotInstallable) {
			t.Errorf("InstallOrder(%s) = %v; want ErrNotInstallable", id, err)
		}
	}
}

func TestUninstallOrderRefusesUndeletable(t *testing.T) {
	for _, id := range []string{IDDocker, IDSystemdNetworkd, IDNetworkctl, IDSystemctl, IDCurl} {
		if _, err := UninstallOrder(id); !errors.Is(err, ErrNotUninstallable) {
			t.Errorf("UninstallOrder(%s) = %v; want ErrNotUninstallable", id, err)
		}
	}
	if order, err := UninstallOrder(IDRailpack); err != nil || !slices.Equal(order, []string{IDRailpack}) {
		t.Fatalf("UninstallOrder(railpack) = %v, %v; want [railpack], nil", order, err)
	}
}

func TestOrdersRefuseUnknownTool(t *testing.T) {
	if _, err := InstallOrder("nope"); !errors.Is(err, ErrUnknownTool) {
		t.Errorf("InstallOrder(nope) = %v; want ErrUnknownTool", err)
	}
	if _, err := UninstallOrder("nope"); !errors.Is(err, ErrUnknownTool) {
		t.Errorf("UninstallOrder(nope) = %v; want ErrUnknownTool", err)
	}
}

func TestDependents(t *testing.T) {
	if got := Dependents(IDDocker); !slices.Equal(got, []string{IDBuildKit}) {
		t.Errorf("Dependents(docker) = %v; want [buildkit]", got)
	}
	if got := Dependents(IDBuildKit); !slices.Equal(got, []string{IDRailpack}) {
		t.Errorf("Dependents(buildkit) = %v; want [railpack]", got)
	}
	if got := Dependents(IDRailpack); len(got) != 0 {
		t.Errorf("Dependents(railpack) = %v; want none", got)
	}
}
