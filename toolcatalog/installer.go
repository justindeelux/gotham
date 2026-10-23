package toolcatalog

import (
	"errors"
	"fmt"
)

var (
	ErrUnknownTool      = errors.New("toolcatalog: unknown tool")
	ErrNotInstallable   = errors.New("toolcatalog: tool is not installable")
	ErrNotUninstallable = errors.New("toolcatalog: tool is not uninstallable")
)

func InstallOrder(id string) ([]string, error) {
	tool, ok := Lookup(id)
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrUnknownTool, id)
	}
	if !tool.Installable {
		return nil, fmt.Errorf("%w: %q", ErrNotInstallable, id)
	}

	var order []string
	seen := make(map[string]bool)
	var visit func(string) error
	visit = func(current string) error {
		if seen[current] {
			return nil
		}
		seen[current] = true
		entry, ok := Lookup(current)
		if !ok {
			return fmt.Errorf("%w: %q (a dependency of %q)", ErrUnknownTool, current, id)
		}
		for _, dep := range entry.Deps {
			if err := visit(dep); err != nil {
				return err
			}
		}
		order = append(order, current)
		return nil
	}
	if err := visit(id); err != nil {
		return nil, err
	}

	for _, toolID := range order {
		entry, _ := Lookup(toolID)
		if !entry.Installable {
			return nil, fmt.Errorf("%w: %q is a dependency of %q", ErrNotInstallable, toolID, id)
		}
	}
	return order, nil
}

func UninstallOrder(id string) ([]string, error) {
	tool, ok := Lookup(id)
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrUnknownTool, id)
	}
	if !tool.Uninstallable {
		if id == IDGit || id == IDDocker {
			return nil, fmt.Errorf("%w: %q is required by the platform; %s", ErrNotUninstallable, id, tool.Description)
		}
		return nil, fmt.Errorf("%w: %q", ErrNotUninstallable, id)
	}
	return []string{id}, nil
}

func Dependents(id string) []string {
	var dependents []string
	for _, tool := range Catalog {
		if tool.ID == id {
			continue
		}
		for _, dep := range tool.Deps {
			if dep == id {
				dependents = append(dependents, tool.ID)
				break
			}
		}
	}
	return dependents
}
