package toolchain

import (
	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
)

type Tool struct {
	ID            string   `json:"id"`
	Name          string   `json:"name"`
	Description   string   `json:"description,omitempty"`
	Category      string   `json:"category,omitempty"`
	Installed     bool     `json:"installed"`
	Version       string   `json:"version,omitempty"`
	Path          string   `json:"path,omitempty"`
	Installable   bool     `json:"installable"`
	Uninstallable bool     `json:"uninstallable"`
	NeedsRoot     bool     `json:"needs_root"`
	Deps          []string `json:"deps,omitempty"`
	MinVersion    string   `json:"min_version,omitempty"`
	VersionOK     bool     `json:"version_ok"`
}

func toolFromProto(p *agentv1.ToolInfo) Tool {
	return Tool{
		ID:            p.GetId(),
		Name:          p.GetName(),
		Description:   p.GetDescription(),
		Category:      p.GetCategory(),
		Installed:     p.GetInstalled(),
		Version:       p.GetVersion(),
		Path:          p.GetPath(),
		Installable:   p.GetInstallable(),
		Uninstallable: p.GetUninstallable(),
		NeedsRoot:     p.GetNeedsRoot(),
		Deps:          p.GetDeps(),
		MinVersion:    p.GetMinVersion(),
		VersionOK:     p.GetVersionOk(),
	}
}
