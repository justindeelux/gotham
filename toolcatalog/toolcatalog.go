package toolcatalog

import (
	"context"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type Category string

const (
	CategoryBuild   Category = "build"
	CategoryRuntime Category = "runtime"
	CategoryNetwork Category = "network"
)

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

type Detect struct {
	Binaries []string
	Version  []string
	Probe    []string
}

type Tool struct {
	ID            string
	Name          string
	Description   string
	Category      Category
	Detect        Detect
	Deps          []string
	NeedsRoot     bool
	Installable   bool
	Uninstallable bool
	MinVersion    string
}

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
		Installable: true, Uninstallable: false,
		MinVersion: "2.37",
	},
	{
		ID:            IDDocker,
		Name:          "Docker",
		Description:   "Runs application, database and proxy containers; every node needs it.",
		Category:      CategoryRuntime,
		Detect:        Detect{Binaries: []string{"docker"}, Version: []string{"--version"}},
		NeedsRoot:     true,
		Installable:   true,
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

type Detection struct {
	ID        string
	Installed bool
	Version   string
	Path      string
	VersionOK bool
}

var versionPattern = regexp.MustCompile(`\d+(?:\.\d+)+`)

const probeTimeout = 5 * time.Second

func List() []Tool { return Catalog }

func Lookup(id string) (Tool, bool) {
	for _, tool := range Catalog {
		if tool.ID == id {
			return tool, true
		}
	}
	return Tool{}, false
}

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

func DetectAll() []Detection {
	detections := make([]Detection, 0, len(Catalog))
	for _, tool := range Catalog {
		detections = append(detections, DetectTool(tool))
	}
	return detections
}

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

func resolve(tool Tool) (string, bool) {
	for _, name := range tool.Detect.Binaries {
		if path, err := exec.LookPath(name); err == nil {
			return path, true
		}
	}
	return "", false
}

func probeSucceeds(tool Tool) bool {
	ctx, cancel := context.WithTimeout(context.Background(), probeTimeout)
	defer cancel()
	return exec.CommandContext(ctx, tool.Detect.Probe[0], tool.Detect.Probe[1:]...).Run() == nil
}

func runVersion(path string, args []string) string {
	ctx, cancel := context.WithTimeout(context.Background(), probeTimeout)
	defer cancel()
	out, _ := exec.CommandContext(ctx, path, args...).CombinedOutput()
	return versionPattern.FindString(string(out))
}

func meetsMinVersion(observed, minimum string) bool {
	if minimum == "" {
		return true
	}
	if observed == "" {
		return false
	}
	return compareVersions(observed, minimum) >= 0
}

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

func versionComponent(parts []string, i int) int {
	if i >= len(parts) {
		return 0
	}
	n, _ := strconv.Atoi(strings.TrimSpace(parts[i]))
	return n
}
