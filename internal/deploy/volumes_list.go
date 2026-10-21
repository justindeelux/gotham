package deploy

import (
	"context"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"gopkg.in/yaml.v3"

	"github.com/justindeelux/gotham/internal/files"
	"github.com/justindeelux/gotham/internal/services"
	agentv1 "github.com/justindeelux/gotham/proto/agent/v1"
)

// Volume kinds reported by the read-only listing. "managed" is a bind under
// <managed root>/<appID> (a derived storage or a rewritten compose volume),
// "named" a Docker named volume, and "unknown" a mount an older agent could
// not classify.
const (
	VolumeKindManaged = "managed"
	VolumeKindNamed   = "named"
	VolumeKindBind    = "bind"
	VolumeKindTmpfs   = "tmpfs"
	VolumeKindUnknown = "unknown"
)

// Volume states: the runtime state of a backing storage. "unverified" means
// no container was observed, so the listing cannot claim the data is gone.
const (
	VolumeStateRunning    = "running"
	VolumeStateStopped    = "stopped"
	VolumeStateUnverified = "unverified"
)

// Runtime states of the whole listing: whether the node answered at all.
const (
	VolumeRuntimeAvailable   = "available"
	VolumeRuntimeUnavailable = "unavailable"
	VolumeRuntimeLimited     = "limited"
)

// Open reasons on the wire. The backend never names a File Manager root; it
// resolves the node's own root list and reports one of these codes when no
// target can be built. The UI translates the codes (and falls back to the raw
// value for a newer server).
const (
	// VolumeOpenFilesDisabled: the File Manager is off for this control plane
	// or node (FEATURE_FILES=false, GOTHAM_AGENT_FILES=false, or an agent
	// predating the FileService).
	VolumeOpenFilesDisabled = "files_disabled"
	// VolumeOpenRootUnavailable: the node exposes no writable root that
	// contains the directory (or the root list could not be read).
	VolumeOpenRootUnavailable = "root_unavailable"
	// VolumeOpenNotBrowsable: the backing directory is outside the
	// application's managed storage, is a temporary filesystem, or is stored
	// in Docker's data directory.
	VolumeOpenNotBrowsable = "not_browsable"
	// VolumeOpenDeviceUnverified: the agent could not resolve the volume's
	// configured device, so its backing directory is unknown.
	VolumeOpenDeviceUnverified = "volume_unverified"
	// VolumeOpenNoNode: the application has no node assigned.
	VolumeOpenNoNode = "no_node"
)

// VolumeMount is one container-side mount of a volume entry.
type VolumeMount struct {
	Destination string
	ReadOnly    bool
	// Service is the compose service name; empty for a single-container
	// application.
	Service   string
	Container string
	State     string
}

// VolumeFileManager is a validated, root-relative File Manager target for one
// volume. ServerID is the application's own node, never a caller-supplied one.
type VolumeFileManager struct {
	ServerID uuid.UUID
	Root     string
	Path     string
}

// VolumeEntry is one backing storage of an application, grouped across every
// container that mounts it.
type VolumeEntry struct {
	Name        string
	Kind        string
	HostPath    string
	State       string
	Mounts      []VolumeMount
	FileManager *VolumeFileManager
	// OpenReason explains why no File Manager target is offered. Empty when
	// FileManager is set.
	OpenReason string
}

// VolumeListing is the read-only storage view of one application.
type VolumeListing struct {
	SourceType string
	// Runtime reports whether the node could be observed.
	Runtime       string
	RuntimeReason string
	Volumes       []VolumeEntry
}

// ListVolumes returns the application's volumes as configured and as observed
// on its node. Reading never creates a directory, remounts storage, or
// recreates a container: it lists the configured rows and the containers of
// this application only, then groups their mounts by backing storage.
func (s *Service) ListVolumes(ctx context.Context, userID, appID uuid.UUID) (VolumeListing, error) {
	app, err := s.application(ctx, userID, appID, false)
	if err != nil {
		return VolumeListing{}, err
	}
	listing := VolumeListing{
		SourceType: NormalizeSourceType(app.SourceType, app.Provider),
		Runtime:    VolumeRuntimeAvailable,
		Volumes:    []VolumeEntry{},
	}
	configured, err := s.configuredVolumes(ctx, app)
	if err != nil {
		return VolumeListing{}, err
	}
	listing.Volumes = configured
	observation := volumeObservation{
		runtime:     VolumeRuntimeAvailable,
		rootsReason: VolumeOpenRootUnavailable,
	}
	if app.ServerID == uuid.Nil {
		observation.runtime = VolumeRuntimeUnavailable
		observation.runtimeReason = "the application has no node assigned"
		observation.rootsReason = VolumeOpenNoNode
	} else {
		observed, err := s.observeVolumes(ctx, app)
		if err != nil {
			return VolumeListing{}, err
		}
		observation = observed
	}
	listing.Runtime = observation.runtime
	listing.RuntimeReason = observation.runtimeReason
	if app.ServerID != uuid.Nil {
		listing.Volumes = mergeVolumeMounts(listing.Volumes, observation.containers, services.ProjectName(app.ID))
	}
	for index := range listing.Volumes {
		entry := &listing.Volumes[index]
		entry.State = volumeEntryState(*entry)
		if entry.OpenReason == VolumeOpenDeviceUnverified {
			entry.State = VolumeStateUnverified
		}
		entry.FileManager, entry.OpenReason = fileManagerTarget(app, *entry, observation.roots, observation.rootsReason)
	}
	return listing, nil
}

// configuredVolumes builds the entries that exist before any runtime read: the
// saved storage rows of a single-container application, or the named-volume
// declarations of a compose source. Compose rows are rewritten to managed
// directories by RewriteComposeManagedVolumes, so their mapped path is
// <root>/<appID>/<name> — derived, never read from the document.
func (s *Service) configuredVolumes(ctx context.Context, app Application) ([]VolumeEntry, error) {
	if isComposeApp(app) {
		return composeConfiguredVolumes(app), nil
	}
	storages, err := s.repo.ListStorages(ctx, app.ID)
	if err != nil {
		return nil, err
	}
	entries := make([]VolumeEntry, 0, len(storages))
	for _, storage := range storages {
		entry := VolumeEntry{
			Name:     storage.Name,
			Kind:     storageKind(storage.HostPath),
			HostPath: storage.HostPath,
			State:    VolumeStateUnverified,
			Mounts:   []VolumeMount{{Destination: storage.ContainerPath, State: VolumeStateUnverified}},
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

// storageKind classifies a stored host path: a non-absolute value is a
// namespaced Docker named volume, an absolute one is a managed bind.
func storageKind(hostPath string) string {
	if strings.HasPrefix(hostPath, "/") {
		return VolumeKindManaged
	}
	return VolumeKindNamed
}

// composeVolumeMount is one named-volume use parsed out of a compose source.
type composeVolumeMount struct {
	service     string
	destination string
	readOnly    bool
}

// composeConfiguredVolumes extracts the named-volume declarations of a pasted
// compose source: every service mount plus a top-level volume nobody mounts.
// A repo-backed compose file is not cloned here, so it contributes no
// configured rows; the runtime read still lists its volumes.
func composeConfiguredVolumes(app Application) []VolumeEntry {
	content := strings.TrimSpace(app.ComposeContent)
	if content == "" {
		return nil
	}
	var doc struct {
		Services map[string]struct {
			Volumes []any `yaml:"volumes"`
		} `yaml:"services"`
		Volumes map[string]any `yaml:"volumes"`
	}
	if err := yaml.Unmarshal([]byte(content), &doc); err != nil {
		return nil
	}
	names := make([]string, 0, len(doc.Services))
	for name := range doc.Services {
		names = append(names, name)
	}
	sort.Strings(names)
	mounts := map[string][]composeVolumeMount{}
	for _, service := range names {
		for _, raw := range doc.Services[service].Volumes {
			name, mount, ok := composeNamedVolumeMount(raw)
			if !ok {
				continue
			}
			mount.service = service
			mounts[name] = append(mounts[name], mount)
		}
	}
	declared := make([]string, 0, len(doc.Volumes))
	for name := range doc.Volumes {
		declared = append(declared, name)
	}
	sort.Strings(declared)
	for _, name := range declared {
		if _, ok := mounts[name]; !ok {
			mounts[name] = nil
		}
	}
	keys := make([]string, 0, len(mounts))
	for name := range mounts {
		keys = append(keys, name)
	}
	sort.Strings(keys)
	root := managedVolumeRoot()
	appDir := appVolumeDir(root, app.ID)
	entries := make([]VolumeEntry, 0, len(keys))
	for _, name := range keys {
		if !composeNamedVolumePattern.MatchString(name) {
			continue
		}
		hostPath := filepath.Join(appDir, name)
		if !withinPath(appDir, hostPath) || hostPath == appDir {
			continue
		}
		entry := VolumeEntry{
			Name:     name,
			Kind:     VolumeKindManaged,
			HostPath: hostPath,
			State:    VolumeStateUnverified,
		}
		for _, mount := range mounts[name] {
			entry.Mounts = append(entry.Mounts, VolumeMount{
				Destination: mount.destination,
				ReadOnly:    mount.readOnly,
				Service:     mount.service,
				State:       VolumeStateUnverified,
			})
		}
		entries = append(entries, entry)
	}
	return entries
}

// composeNamedVolumeMount reads one compose service volume entry in either the
// short "name:/path[:ro]" or the long mapping form, answering false for a bind,
// an anonymous volume or an unparseable entry.
func composeNamedVolumeMount(raw any) (string, composeVolumeMount, bool) {
	switch value := raw.(type) {
	case string:
		parts := strings.Split(value, ":")
		if len(parts) < 2 {
			return "", composeVolumeMount{}, false
		}
		source := strings.TrimSpace(parts[0])
		if !isComposeVolumeName(source) {
			return "", composeVolumeMount{}, false
		}
		mount := composeVolumeMount{destination: strings.TrimSpace(parts[1])}
		if len(parts) > 2 {
			for _, mode := range strings.Split(parts[2], ",") {
				if strings.TrimSpace(mode) == "ro" {
					mount.readOnly = true
				}
			}
		}
		return source, mount, true
	case map[string]any:
		kind := strings.TrimSpace(asString(value["type"]))
		if kind != "" && kind != "volume" {
			return "", composeVolumeMount{}, false
		}
		source := strings.TrimSpace(asString(value["source"]))
		if !isComposeVolumeName(source) {
			return "", composeVolumeMount{}, false
		}
		return source, composeVolumeMount{
			destination: strings.TrimSpace(asString(value["target"])),
			readOnly:    asBool(value["read_only"]),
		}, true
	}
	return "", composeVolumeMount{}, false
}

// isComposeVolumeName rejects a host path, a relative path and an empty source;
// a valid name is a single segment the managed path can safely append.
func isComposeVolumeName(source string) bool {
	return source != "" &&
		!strings.HasPrefix(source, "/") &&
		!strings.HasPrefix(source, ".") &&
		!strings.HasPrefix(source, "~") &&
		composeNamedVolumePattern.MatchString(source)
}

// asString reads a scalar from a decoded YAML mapping.
func asString(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case int:
		return strconv.Itoa(typed)
	case bool:
		return strconv.FormatBool(typed)
	}
	return ""
}

// asBool reads a boolean from a decoded YAML mapping.
func asBool(value any) bool {
	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		parsed, _ := strconv.ParseBool(typed)
		return parsed
	}
	return false
}

// volumeObservation is one node read: the application's containers, the File
// Manager roots the node exposes, and the runtime state to report.
type volumeObservation struct {
	containers    []*agentv1.ContainerInfo
	roots         []*agentv1.FileRoot
	rootsReason   string
	runtime       string
	runtimeReason string
}

// observeVolumes lists the containers of this application only — by its
// gotham.app_id label, or by its exact compose project identity — including
// stopped ones, and resolves the node's actual File Manager roots in the same
// connection. A node failure is reported in the listing, not as an error, so
// configured storage is never hidden because the runtime is unknown; a
// cancelled request is returned as an error instead of a misleading
// "unavailable".
func (s *Service) observeVolumes(ctx context.Context, app Application) (volumeObservation, error) {
	observation := volumeObservation{
		runtime:     VolumeRuntimeAvailable,
		rootsReason: VolumeOpenRootUnavailable,
	}
	callCtx, cancel := context.WithTimeout(ctx, s.controlTimeout)
	defer cancel()
	node, err := s.dialNode(callCtx, app.ServerID)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return observation, ctxErr
		}
		s.logger.Debug("deploy: volume listing could not dial the node",
			"application_id", app.ID, "error", err)
		observation.runtime = VolumeRuntimeUnavailable
		observation.runtimeReason = "the node agent is unavailable"
		return observation, nil
	}
	defer func() {
		if closeErr := node.Close(); closeErr != nil {
			s.logger.Debug("deploy: close volume node", "application_id", app.ID, "error", closeErr)
		}
	}()
	labels := map[string]string{labelAppID: app.ID.String()}
	if isComposeApp(app) {
		labels = map[string]string{composeProjectLabel: services.ProjectName(app.ID)}
	}
	containers, err := node.ContainersByLabel(callCtx, labels, true, true)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return observation, ctxErr
		}
		s.logger.Debug("deploy: volume listing container read failed",
			"application_id", app.ID, "error", err)
		observation.runtime = VolumeRuntimeUnavailable
		observation.runtimeReason = "the node agent did not answer the container listing"
		return observation, nil
	}
	sort.Slice(containers, func(i, j int) bool {
		return containers[i].GetName() < containers[j].GetName()
	})
	observation.containers = containers
	for _, container := range containers {
		for _, mount := range container.GetMounts() {
			if mount.GetType() == "" {
				observation.runtime = VolumeRuntimeLimited
				observation.runtimeReason = "the node agent does not report mount types; update the agent to identify every mount"
			}
		}
	}
	observation.roots, observation.rootsReason = s.fileRoots(callCtx, app, node)
	return observation, nil
}

// fileRoots reads the node's real File Manager roots. The root names are never
// assumed: the agent may expose one writable "system" root (full access) or a
// confined "volumes" plus a read-only "compose" root. An absent FileService
// (files disabled or an older agent) and an unreadable list answer a reason
// instead of a guessed target.
func (s *Service) fileRoots(ctx context.Context, app Application, node Node) ([]*agentv1.FileRoot, string) {
	if !files.Enabled() {
		return nil, VolumeOpenFilesDisabled
	}
	roots, err := node.FileRoots(ctx)
	if err != nil {
		if status.Code(err) == codes.Unimplemented {
			return nil, VolumeOpenFilesDisabled
		}
		s.logger.Debug("deploy: volume listing could not read the node file roots",
			"application_id", app.ID, "error", err)
		return nil, VolumeOpenRootUnavailable
	}
	if len(roots) == 0 {
		return nil, VolumeOpenRootUnavailable
	}
	return roots, ""
}

// mergeVolumeMounts folds observed container mounts into the configured
// entries, grouping every consumer of one backing storage into a single row.
// A mount with no configured match is added as its own entry.
func mergeVolumeMounts(entries []VolumeEntry, containers []*agentv1.ContainerInfo, project string) []VolumeEntry {
	byVolume := map[string]int{}
	byPath := map[string]int{}
	for index, entry := range entries {
		if entry.Kind == VolumeKindNamed {
			byVolume[entry.HostPath] = index
		}
		if entry.HostPath != "" {
			byPath[filepath.Clean(entry.HostPath)] = index
		}
	}
	for _, container := range containers {
		service := container.GetLabels()[composeServiceLabel]
		for _, mount := range container.GetMounts() {
			mountEntry := VolumeMount{
				Destination: mount.GetDestination(),
				ReadOnly:    mount.GetReadOnly(),
				Service:     service,
				Container:   container.GetName(),
				State:       containerVolumeState(container.GetState()),
			}
			if index, ok := matchVolumeEntry(entries, byVolume, byPath, mount, project); ok {
				ensureVolumeHostPath(&entries[index], mount)
				entries[index].Mounts = appendMount(entries[index].Mounts, mountEntry)
				continue
			}
			if index, ok := matchUnbackedEntry(entries, mount); ok {
				entries[index].Mounts = appendMount(entries[index].Mounts, mountEntry)
				continue
			}
			entry := runtimeVolumeEntry(mount)
			entry.Mounts = []VolumeMount{mountEntry}
			entries = append(entries, entry)
			last := len(entries) - 1
			if entry.Kind == VolumeKindNamed && mount.GetVolumeName() != "" {
				byVolume[mount.GetVolumeName()] = last
			}
			if entry.HostPath != "" {
				byPath[filepath.Clean(entry.HostPath)] = last
			}
		}
	}
	return entries
}

// matchVolumeEntry finds the configured row a runtime mount belongs to: a
// named volume by its Docker name, a bind by its host path, and an older
// agent's untyped mount by its destination.
func matchVolumeEntry(entries []VolumeEntry, byVolume, byPath map[string]int, mount *agentv1.ContainerMount, project string) (int, bool) {
	switch mount.GetType() {
	case "volume":
		if index, ok := byVolume[mount.GetVolumeName()]; ok {
			return index, true
		}
		if mount.GetVolumeDevice() != "" {
			if index, ok := byPath[filepath.Clean(mount.GetVolumeDevice())]; ok {
				return index, true
			}
		}
		// A compose volume keeps its declared name inside the project
		// namespace: match the FULL project-prefixed name, never a suffix, so
		// "my_data" can not fold into "data" (or the reverse). The device
		// stays the authority for the host path; this only links the consumer
		// to its configured row.
		if project != "" {
			for index, entry := range entries {
				if entry.Kind == VolumeKindManaged && mount.GetVolumeName() == project+"_"+entry.Name {
					return index, true
				}
			}
		}
	case "bind":
		if index, ok := byPath[filepath.Clean(mount.GetSource())]; ok {
			return index, true
		}
	case "":
		for index, entry := range entries {
			for _, configured := range entry.Mounts {
				if configured.Destination != "" && configured.Destination == mount.GetDestination() {
					return index, true
				}
			}
		}
	}
	return 0, false
}

// runtimeVolumeEntry describes a mount no configured row covers, keeping an
// unreadable or temporary mount visible without inventing a host path.
func runtimeVolumeEntry(mount *agentv1.ContainerMount) VolumeEntry {
	entry := VolumeEntry{State: VolumeStateUnverified}
	switch mount.GetType() {
	case "volume":
		entry.Kind = runtimeVolumeKind(mount)
		entry.Name = mount.GetVolumeName()
		switch {
		case mount.GetVolumeDeviceUnverified():
			// The engine could not answer: do not present its mountpoint as
			// the backing directory.
			entry.OpenReason = VolumeOpenDeviceUnverified
		case mount.GetVolumeDevice() != "":
			entry.HostPath = filepath.Clean(mount.GetVolumeDevice())
		case mount.GetSource() != "":
			// No configured device: Docker's mountpoint is the backing store.
			// It is informational only (never openable: no File Manager root
			// contains Docker's data directory).
			entry.HostPath = filepath.Clean(mount.GetSource())
			entry.OpenReason = VolumeOpenNotBrowsable
		default:
			entry.OpenReason = VolumeOpenNotBrowsable
		}
		if entry.Name == "" {
			entry.Name = filepath.Base(entry.HostPath)
		}
	case "bind":
		entry.Kind = runtimeVolumeKind(mount)
		host := filepath.Clean(mount.GetSource())
		entry.Name = filepath.Base(host)
		if entry.Kind == VolumeKindManaged && host != filepath.Clean(managedVolumeRoot()) {
			entry.HostPath = host
		} else {
			entry.Kind = VolumeKindBind
			entry.OpenReason = VolumeOpenNotBrowsable
		}
	case "tmpfs":
		entry.Kind = VolumeKindTmpfs
		entry.Name = filepath.Base(mount.GetDestination())
		entry.OpenReason = VolumeOpenNotBrowsable
	default:
		entry.Kind = VolumeKindUnknown
		entry.OpenReason = VolumeOpenNotBrowsable
	}
	if entry.Name == "" {
		entry.Name = "unnamed"
	}
	return entry
}

// ensureVolumeHostPath replaces a configured named-volume label with the
// backing directory the runtime resolved, so the mapped path is truthful
// instead of the bare volume name. A managed device becomes a managed bind.
func ensureVolumeHostPath(entry *VolumeEntry, mount *agentv1.ContainerMount) {
	if entry.Kind != VolumeKindNamed {
		return
	}
	if mount.GetVolumeDeviceUnverified() {
		entry.HostPath = ""
		entry.OpenReason = VolumeOpenDeviceUnverified
		return
	}
	if mount.GetVolumeDevice() != "" {
		entry.HostPath = filepath.Clean(mount.GetVolumeDevice())
		if withinPath(managedVolumeRoot(), entry.HostPath) {
			entry.Kind = VolumeKindManaged
		}
		return
	}
	if source := filepath.Clean(mount.GetSource()); mount.GetSource() != "" && filepath.IsAbs(source) {
		entry.HostPath = source
		entry.OpenReason = VolumeOpenNotBrowsable
	}
}

// matchUnbackedEntry finds the existing row of a mount with no backing
// directory (tmpfs, an untyped mount): one row per destination, never a
// duplicate per container listing.
func matchUnbackedEntry(entries []VolumeEntry, mount *agentv1.ContainerMount) (int, bool) {
	for index, entry := range entries {
		if entry.HostPath != "" || entry.Kind != runtimeVolumeKind(mount) {
			continue
		}
		for _, existing := range entry.Mounts {
			if existing.Destination == mount.GetDestination() {
				return index, true
			}
		}
	}
	return 0, false
}

// runtimeVolumeKind classifies a mount no configured row covers.
func runtimeVolumeKind(mount *agentv1.ContainerMount) string {
	switch mount.GetType() {
	case "volume":
		if mount.GetVolumeDevice() != "" && withinPath(managedVolumeRoot(), mount.GetVolumeDevice()) {
			return VolumeKindManaged
		}
		return VolumeKindNamed
	case "bind":
		if withinPath(managedVolumeRoot(), mount.GetSource()) {
			return VolumeKindManaged
		}
		return VolumeKindBind
	case "tmpfs":
		return VolumeKindTmpfs
	}
	return VolumeKindUnknown
}

// appendMount keeps one row per destination per consumer, filling the
// configured placeholder (no container) instead of adding a second row.
func appendMount(mounts []VolumeMount, next VolumeMount) []VolumeMount {
	for index, mount := range mounts {
		if mount.Destination != next.Destination {
			continue
		}
		if mount.Container == "" && next.Container != "" && (mount.Service == "" || mount.Service == next.Service) {
			mounts[index] = next
			return mounts
		}
		if mount.Container == next.Container && mount.Service == next.Service {
			return mounts
		}
	}
	return append(mounts, next)
}

// volumeEntryState reports the aggregate runtime state: running when any
// consumer runs, stopped when only stopped consumers were observed, and
// unverified when nothing was observed.
func volumeEntryState(entry VolumeEntry) string {
	state := VolumeStateUnverified
	for _, mount := range entry.Mounts {
		switch mount.State {
		case VolumeStateRunning:
			return VolumeStateRunning
		case VolumeStateStopped:
			state = VolumeStateStopped
		}
	}
	return state
}

// containerVolumeState maps Docker's container state onto the listing's
// running/stopped pair; "created" and "paused" are not running consumers.
func containerVolumeState(state string) string {
	if state == "running" {
		return VolumeStateRunning
	}
	return VolumeStateStopped
}

// fileManagerTarget builds the File Manager navigation target of one entry from
// the node's ACTUAL roots: the most specific writable root whose real path
// contains the directory wins (full-access agents expose "system" => "/",
// confined agents expose "volumes" => the managed root; the read-only compose
// root is never chosen for volume data). The result stays confined to this
// application's managed directory even though a full-access root could browse
// much more. Containment here is lexical: the control plane cannot resolve
// node-side symlinks, so the File Manager's own root-relative confinement is
// what refuses them after navigation.
func fileManagerTarget(app Application, entry VolumeEntry, roots []*agentv1.FileRoot, rootsReason string) (*VolumeFileManager, string) {
	if entry.OpenReason != "" {
		return nil, entry.OpenReason
	}
	if entry.HostPath == "" {
		return nil, VolumeOpenNotBrowsable
	}
	if app.ServerID == uuid.Nil {
		return nil, VolumeOpenNoNode
	}
	root := filepath.Clean(managedVolumeRoot())
	host := filepath.Clean(entry.HostPath)
	// The target is confined to THIS application's directory: application
	// read access never reaches a sibling application's data.
	appDir := appVolumeDir(root, app.ID)
	if host == appDir || !withinPath(appDir, host) {
		return nil, VolumeOpenNotBrowsable
	}
	if rootsReason != "" {
		return nil, rootsReason
	}
	managed := filepath.Clean(managedVolumeRoot())
	var best *agentv1.FileRoot
	bestBase := ""
	bestLen := -1
	for _, candidate := range roots {
		if candidate == nil || !candidate.GetWritable() {
			continue
		}
		candidatePath := filepath.Clean(candidate.GetPath())
		if !filepath.IsAbs(candidatePath) {
			continue
		}
		for _, base := range rootBases(candidatePath, managed) {
			if !withinPath(base, host) {
				continue
			}
			// Most specific base wins: a nested operator root beats "/".
			if len(base) > bestLen {
				best, bestBase, bestLen = candidate, base, len(base)
			}
		}
	}
	if best == nil {
		return nil, VolumeOpenRootUnavailable
	}
	rel, err := filepath.Rel(bestBase, host)
	if err != nil || filepath.IsAbs(rel) || rel == ".." ||
		strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return nil, VolumeOpenRootUnavailable
	}
	if rel == "." {
		rel = ""
	}
	return &VolumeFileManager{
		ServerID: app.ServerID,
		Root:     best.GetName(),
		Path:     filepath.ToSlash(rel),
	}, ""
}

// rootBases returns the base paths a root may be addressed through. The agent
// reports resolved paths (macOS /tmp -> /private/tmp, for example), while the
// control plane derives the managed path from its configured root; when the
// two name the same directory, both forms are accepted. A path that cannot be
// resolved on this host contributes only its literal form.
//
// EvalSymlinks runs on the CONTROL PLANE's filesystem, so this reconciliation
// assumes the CP and node see the same path for the managed root (the documented
// deployment, and the all-in-one/dev case). On a node with a different layout
// only the literal root path matches; nothing widens beyond appDir either way.
func rootBases(rootPath, managed string) []string {
	bases := []string{rootPath}
	if rootPath == managed {
		return bases
	}
	resolvedRoot, rootErr := filepath.EvalSymlinks(rootPath)
	resolvedManaged, managedErr := filepath.EvalSymlinks(managed)
	same := rootErr == nil && (resolvedRoot == managed ||
		(managedErr == nil && resolvedRoot == resolvedManaged))
	if same {
		bases = append(bases, managed)
	}
	return bases
}

// withinPath reports whether path is base or a descendant of base, comparing
// path components rather than a string prefix.
func withinPath(base, path string) bool {
	if base == "" || path == "" || !filepath.IsAbs(base) || !filepath.IsAbs(path) {
		return false
	}
	rel, err := filepath.Rel(base, path)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}
