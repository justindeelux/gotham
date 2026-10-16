// Package toolcatalog is the single catalog of host tools the Gotham platform
// needs, with per-tool detection.
//
// It is deliberately scope-neutral: the node agent serves the catalog over gRPC
// and the shared buildtool package uses it to decide whether a build toolchain
// is present on a host, so it imports neither gotham/internal nor gotham/agent.
package toolcatalog

import (
	"context"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Category groups catalog entries for the UI. The set is fixed so a client can
// render a stable grouping.
type Category string

const (
	// CategoryBuild covers source and image build toolchains.
	CategoryBuild Category = "build"
	// CategoryRuntime covers tools the agent or control plane runs directly.
	CategoryRuntime Category = "runtime"
	// CategoryNetwork covers host network configuration tools.
	CategoryNetwork Category = "network"
)

// Catalog entry ids. They are the stable handles a client passes back to the
// install/uninstall RPCs.
const (
	IDRailpack        = "railpack"
	IDBuildKit        = "buildkit"
	IDPack            = "pack"
	IDDocker          = "docker"
	IDGit             = "git"
	IDSystemdNetworkd = "systemd-networkd"
	IDSystemdResolved = "systemd-resolved"
	IDNetworkctl      = "networkctl"
	IDSSH             = "ssh"
	IDSudo            = "sudo"
	IDSystemctl       = "systemctl"
	IDCurl            = "curl"
	IDResolvectl      = "resolvectl"
	IDIP              = "ip"
)

// Detect describes how one tool is found on the host. Every rule resolves
// through PATH (or runs the whole Probe argv through PATH), never a hard-coded
// absolute path, so a test that points PATH at a stub directory sees exactly
// the catalog entry it stubbed and nothing from the host it runs on.
type Detect struct {
	// Binaries are executable names resolved on PATH in order; the first hit
	// wins.
	Binaries []string
	// Version is the argv run against the resolved executable to read its
	// version; nil skips the probe.
	Version []string
	// Probe, when set, is a whole argv run instead of a binary lookup, for a
	// tool that is not a command of its own (the BuildKit container image, the
	// systemd units checked through systemctl). Its success means installed.
	Probe []string
}

// Tool is one catalog entry.
type Tool struct {
	// ID is the stable handle for this tool.
	ID string
	// Name is the human-readable name.
	Name string
	// Description explains what the platform uses the tool for.
	Description string
	Category    Category
	Detect      Detect
	// Deps are the catalog ids this tool needs to be usable.
	Deps []string
	// NeedsRoot is true when installing or removing the tool needs root.
	NeedsRoot bool
	// Installable and Uninstallable gate the manager. Both are false for a
	// detect-only host unit (systemd-networkd/resolved, networkctl) the
	// platform never installs or removes.
	Installable   bool
	Uninstallable bool
	// MinVersion is the minimum version the platform needs; detection compares
	// the observed version against it and reports the result in
	// Detection.VersionOK. Empty when no minimum is required.
	MinVersion string
}

// Catalog is the platform's host tool catalog, in display order. It is the
// single source of truth: detection, the agent's ListTools RPC and the
// build-toolchain gate all read it.
var Catalog = []Tool{
	{
		ID:          IDRailpack,
		Name:        "Railpack",
		Description: "Builds an image from a generated language plan (the \"auto\" build pack).",
		Category:    CategoryBuild,
		Detect:      Detect{Binaries: []string{"railpack"}, Version: []string{"--version"}},
		Deps:        []string{IDBuildKit},
		NeedsRoot:   true,
		Installable: true, Uninstallable: true,
	},
	{
		ID:          IDBuildKit,
		Name:        "BuildKit",
		Description: "Container image Railpack builds with; started through Docker, never installed as a host binary.",
		Category:    CategoryBuild,
		Detect:      Detect{Probe: []string{"docker", "image", "inspect", "--format", "{{.Id}}", "moby/buildkit"}},
		Deps:        []string{IDDocker},
		NeedsRoot:   true,
		Installable: true, Uninstallable: true,
	},
	{
		ID:          IDPack,
		Name:        "Cloud Native Buildpacks (pack)",
		Description: "Builds an image with the Cloud Native Buildpacks lifecycle (the buildpacks build pack).",
		Category:    CategoryBuild,
		Detect:      Detect{Binaries: []string{"pack"}, Version: []string{"--version"}},
		NeedsRoot:   true,
		Installable: true, Uninstallable: true,
	},
	{
		ID:          IDGit,
		Name:        "Git",
		Description: "Clones application source and commits; the platform needs 2.37 or newer for DNS pinning.",
		Category:    CategoryBuild,
		Detect:      Detect{Binaries: []string{"git"}, Version: []string{"--version"}},
		NeedsRoot:   true,
		Installable: true, Uninstallable: true,
		MinVersion: "2.37",
	},
	{
		ID:          IDDocker,
		Name:        "Docker",
		Description: "Runs application, database and proxy containers; every node needs it.",
		Category:    CategoryRuntime,
		Detect:      Detect{Binaries: []string{"docker"}, Version: []string{"--version"}},
		NeedsRoot:   true,
		Installable: true,
		// Docker is never removed: doing so would destroy every workload on the
		// node. The UI must not offer it.
		Uninstallable: false,
	},
	{
		ID:          IDSystemdNetworkd,
		Name:        "systemd-networkd",
		Description: "Host network configuration; Gotham writes a drop-in and reloads it. Detect-only: never installed or removed.",
		Category:    CategoryNetwork,
		Detect:      Detect{Probe: []string{"systemctl", "is-active", "--quiet", "systemd-networkd"}},
		Deps:        []string{IDSystemctl},
		NeedsRoot:   true,
		Installable: false, Uninstallable: false,
	},
	{
		ID:          IDSystemdResolved,
		Name:        "systemd-resolved",
		Description: "Host DNS resolution; Gotham writes a drop-in and reloads it. Detect-only: never installed or removed.",
		Category:    CategoryNetwork,
		Detect:      Detect{Probe: []string{"systemctl", "is-active", "--quiet", "systemd-resolved"}},
		Deps:        []string{IDSystemctl},
		NeedsRoot:   true,
		Installable: false, Uninstallable: false,
	},
	{
		ID:          IDNetworkctl,
		Name:        "networkctl",
		Description: "Inspects and reloads systemd-networkd links. Detect-only: never installed or removed.",
		Category:    CategoryNetwork,
		Detect:      Detect{Binaries: []string{"networkctl"}, Version: []string{"--version"}},
		Deps:        []string{IDSystemdNetworkd},
		NeedsRoot:   true,
		Installable: false, Uninstallable: false,
	},
	{
		ID:          IDSSH,
		Name:        "OpenSSH client",
		Description: "Git clones a private repository over SSH.",
		Category:    CategoryRuntime,
		Detect:      Detect{Binaries: []string{"ssh"}, Version: []string{"-V"}},
		NeedsRoot:   true,
		Installable: true, Uninstallable: false,
	},
	{
		ID:          IDSudo,
		Name:        "sudo",
		Description: "The agent and control plane run privileged host helpers through sudo -n.",
		Category:    CategoryRuntime,
		Detect:      Detect{Binaries: []string{"sudo"}, Version: []string{"--version"}},
		NeedsRoot:   true,
		Installable: true, Uninstallable: false,
	},
	{
		ID:          IDSystemctl,
		Name:        "systemctl",
		Description: "Manages the agent, proxy and database systemd units.",
		Category:    CategoryRuntime,
		Detect:      Detect{Binaries: []string{"systemctl"}, Version: []string{"--version"}},
		NeedsRoot:   true,
		Installable: false, Uninstallable: false,
	},
	{
		ID:          IDCurl,
		Name:        "curl",
		Description: "Downloads releases and installer material on the host.",
		Category:    CategoryRuntime,
		Detect:      Detect{Binaries: []string{"curl"}, Version: []string{"--version"}},
		NeedsRoot:   true,
		Installable: true, Uninstallable: false,
	},
	{
		ID:          IDResolvectl,
		Name:        "resolvectl",
		Description: "Reads and sets per-link DNS through systemd-resolved.",
		Category:    CategoryNetwork,
		Detect:      Detect{Binaries: []string{"resolvectl"}, Version: []string{"--version"}},
		Deps:        []string{IDSystemdResolved},
		NeedsRoot:   true,
		Installable: false, Uninstallable: false,
	},
	{
		ID:          IDIP,
		Name:        "ip",
		Description: "Reads the host's live addresses and default route.",
		Category:    CategoryNetwork,
		Detect:      Detect{Binaries: []string{"ip"}, Version: []string{"-V"}},
		NeedsRoot:   true,
		Installable: false, Uninstallable: false,
	},
}

// Detection is the observed state of one tool on the host.
type Detection struct {
	// ID is the catalog entry id.
	ID string
	// Installed reports whether the tool resolved on this host.
	Installed bool
	// Version is the version read from the tool, empty when unknown.
	Version string
	// Path is the resolved executable path; empty for a tool that is not a
	// command of its own (the BuildKit container, the systemd units probed
	// through systemctl) or that is not installed.
	Path string
	// VersionOK reports whether the observed version meets the catalog entry's
	// MinVersion. It is true when the entry declares no minimum, and false when
	// a minimum is declared but the observed version is missing or lower.
	VersionOK bool
}

// versionPattern extracts the first dotted numeric version from a probe's
// output, for example "2.43.0" from "git version 2.43.0".
var versionPattern = regexp.MustCompile(`\d+(?:\.\d+)+`)

// probeTimeout bounds a single version or probe command so a wedged executable
// cannot stall a ListTools call.
const probeTimeout = 5 * time.Second

// List returns the catalog.
func List() []Tool { return Catalog }

// Lookup returns the catalog entry with id.
func Lookup(id string) (Tool, bool) {
	for _, tool := range Catalog {
		if tool.ID == id {
			return tool, true
		}
	}
	return Tool{}, false
}

// Installed reports whether the tool with id resolves on this host. It is the
// cheap presence check the build-toolchain gate uses: no version probe is run.
// An unknown id is not installed.
func Installed(id string) bool {
	tool, ok := Lookup(id)
	if !ok {
		return false
	}
	if len(tool.Detect.Probe) > 0 {
		return probeSucceeds(tool)
	}
	_, ok = resolve(tool)
	return ok
}

// DetectAll resolves every catalog entry against the host, in catalog order.
func DetectAll() []Detection {
	detections := make([]Detection, 0, len(Catalog))
	for _, tool := range Catalog {
		detections = append(detections, DetectTool(tool))
	}
	return detections
}

// DetectTool resolves one catalog entry against the host. A tool that is
// present but does not answer its version probe is still reported installed,
// with an empty version.
func DetectTool(tool Tool) Detection {
	detection := Detection{ID: tool.ID}
	if len(tool.Detect.Probe) > 0 {
		detection.Installed = probeSucceeds(tool)
	} else if path, ok := resolve(tool); ok {
		detection.Installed = true
		detection.Path = path
		if len(tool.Detect.Version) > 0 {
			detection.Version = runVersion(path, tool.Detect.Version)
		}
	}
	detection.VersionOK = meetsMinVersion(detection.Version, tool.MinVersion)
	return detection
}

// resolve returns the executable one tool's Detect rule points at: the first
// binary on PATH.
func resolve(tool Tool) (string, bool) {
	for _, name := range tool.Detect.Binaries {
		if path, err := exec.LookPath(name); err == nil {
			return path, true
		}
	}
	return "", false
}

// probeSucceeds runs a tool's Probe argv and reports whether it succeeded.
func probeSucceeds(tool Tool) bool {
	ctx, cancel := context.WithTimeout(context.Background(), probeTimeout)
	defer cancel()
	return exec.CommandContext(ctx, tool.Detect.Probe[0], tool.Detect.Probe[1:]...).Run() == nil
}

// runVersion runs a tool's version argv and returns the first dotted numeric
// version in its combined output, or "" when it answers nothing parseable.
func runVersion(path string, args []string) string {
	ctx, cancel := context.WithTimeout(context.Background(), probeTimeout)
	defer cancel()
	out, _ := exec.CommandContext(ctx, path, args...).CombinedOutput()
	return versionPattern.FindString(string(out))
}

// meetsMinVersion reports whether observed is at least minimum. An entry with
// no minimum is always met; a minimum that is declared but has no parseable
// observed version is not, because the platform does not assume a version it
// could not read is new enough.
func meetsMinVersion(observed, minimum string) bool {
	if minimum == "" {
		return true
	}
	if observed == "" {
		return false
	}
	return compareVersions(observed, minimum) >= 0
}

// compareVersions compares two dotted numeric versions, treating a missing
// component as zero, so "2.43" equals "2.43.0". It returns -1, 0 or 1.
func compareVersions(a, b string) int {
	as, bs := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < len(as) || i < len(bs); i++ {
		switch ai, bi := versionComponent(as, i), versionComponent(bs, i); {
		case ai < bi:
			return -1
		case ai > bi:
			return 1
		}
	}
	return 0
}

// versionComponent returns the i-th dotted component as an int, or 0 when it is
// missing or not a number.
func versionComponent(parts []string, i int) int {
	if i >= len(parts) {
		return 0
	}
	n, _ := strconv.Atoi(strings.TrimSpace(parts[i]))
	return n
}
