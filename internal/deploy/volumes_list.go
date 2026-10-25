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

const (
	VolumeKindManaged = "managed"
	VolumeKindNamed   = "named"
	VolumeKindBind    = "bind"
	VolumeKindTmpfs   = "tmpfs"
	VolumeKindUnknown = "unknown"
)

const (
	VolumeStateRunning    = "running"
	VolumeStateStopped    = "stopped"
	VolumeStateUnverified = "unverified"
)

const (
	VolumeRuntimeAvailable   = "available"
	VolumeRuntimeUnavailable = "unavailable"
	VolumeRuntimeLimited     = "limited"
)

const (
	VolumeOpenFilesDisabled    = "files_disabled"
	VolumeOpenRootUnavailable  = "root_unavailable"
	VolumeOpenNotBrowsable     = "not_browsable"
	VolumeOpenDeviceUnverified = "volume_unverified"
	VolumeOpenNoNode           = "no_node"
)

type VolumeMount struct {
	Destination string
	ReadOnly    bool
	Service     string
	Container   string
	State       string
}

type VolumeFileManager struct {
	ServerID uuid.UUID
	Root     string
	Path     string
}

type VolumeEntry struct {
	Name        string
	Kind        string
	HostPath    string
	State       string
	Mounts      []VolumeMount
	FileManager *VolumeFileManager
	OpenReason  string
}

type VolumeListing struct {
	SourceType    string
	Runtime       string
	RuntimeReason string
	Volumes       []VolumeEntry
}

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

func storageKind(hostPath string) string {
	if strings.HasPrefix(hostPath, "/") {
		return VolumeKindManaged
	}
	return VolumeKindNamed
}

type composeVolumeMount struct {
	service     string
	destination string
	readOnly    bool
}

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

func isComposeVolumeName(source string) bool {
	return source != "" &&
		!strings.HasPrefix(source, "/") &&
		!strings.HasPrefix(source, ".") &&
		!strings.HasPrefix(source, "~") &&
		composeNamedVolumePattern.MatchString(source)
}

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

type volumeObservation struct {
	containers    []*agentv1.ContainerInfo
	roots         []*agentv1.FileRoot
	rootsReason   string
	runtime       string
	runtimeReason string
}

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

func (s *Service) fileRoots(ctx context.Context, app Application, node Node) ([]*agentv1.FileRoot, string) {
	if !files.Enabled() {
		return nil, VolumeOpenFilesDisabled
	}
	roots, err := node.FileRoots(ctx)
	if err != nil {
		if status.Code(err) == codes.Unimplemented || files.IsDisabledOnNode(err) {
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

func runtimeVolumeEntry(mount *agentv1.ContainerMount) VolumeEntry {
	entry := VolumeEntry{State: VolumeStateUnverified}
	switch mount.GetType() {
	case "volume":
		entry.Kind = runtimeVolumeKind(mount)
		entry.Name = mount.GetVolumeName()
		switch {
		case mount.GetVolumeDeviceUnverified():
			entry.OpenReason = VolumeOpenDeviceUnverified
		case mount.GetVolumeDevice() != "":
			entry.HostPath = filepath.Clean(mount.GetVolumeDevice())
		case mount.GetSource() != "":
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

func containerVolumeState(state string) string {
	if state == "running" {
		return VolumeStateRunning
	}
	return VolumeStateStopped
}

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
