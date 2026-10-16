package toolcatalog

import (
	"errors"
	"fmt"
)

// Installer errors. The node agent maps them onto gRPC codes: an unknown tool
// is InvalidArgument and everything else is FailedPrecondition, so a request
// the node will never satisfy is refused before any privileged command runs.
var (
	// ErrUnknownTool reports an id that is not a catalog entry.
	ErrUnknownTool = errors.New("toolcatalog: unknown tool")
	// ErrNotInstallable reports a detect-only tool (a systemd unit) or one the
	// platform does not manage. Installing it is refused.
	ErrNotInstallable = errors.New("toolcatalog: tool is not installable")
	// ErrNotUninstallable reports a tool the platform keeps (docker) or one it
	// never removes (systemd units, systemctl). Removing it is refused.
	ErrNotUninstallable = errors.New("toolcatalog: tool is not uninstallable")
)

// InstallOrder returns the ids to install for id, dependencies first: every
// catalog dependency precedes the tool that needs it, each id appears once even
// when two branches share a dependency, and the tool and its dependencies must
// be installable. The order is the whole plan the agent needs: the privileged
// argv for each id lives in the root-owned gotham-toolctl helper, never here, so
// a caller can only name a catalog id.
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

// UninstallOrder validates that id may be removed and returns it as the single
// entry to act on. Dependencies are never removed with it: another installed
// tool may still need them, which the caller checks before running the helper.
func UninstallOrder(id string) ([]string, error) {
	tool, ok := Lookup(id)
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrUnknownTool, id)
	}
	if !tool.Uninstallable {
		return nil, fmt.Errorf("%w: %q", ErrNotUninstallable, id)
	}
	return []string{id}, nil
}

// Dependents returns the ids of catalog tools, other than id itself, that list
// id in their Deps.
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
