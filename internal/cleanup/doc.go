// Package cleanup scans nodes for reclaimable Docker objects and root-only
// host data (the systemd journal and the package cache) and removes the ones
// an operator selected. Categories are the stable keys the HTTP API and the UI
// reuse.
package cleanup

const (
	CategoryBuildCache        = "build_cache"
	CategoryDanglingImages    = "dangling_images"
	CategoryStoppedContainers = "stopped_containers"
	CategoryUnusedImages      = "unused_images"
	CategoryOrphanedVolumes   = "orphaned_volumes"
	CategoryJournal           = "journal"
	CategoryPackageCache      = "package_cache"
)

const (
	StatusOK      = "ok"
	StatusSkipped = "skipped"
	StatusFailed  = "failed"
)

const (
	ReasonOffline        = "offline"
	ReasonBusy           = "busy"
	ReasonUnsupported    = "unsupported"
	ReasonHelperOutdated = "helper_outdated"
	ReasonAgentOutdated  = "agent_outdated"
)

const (
	TriggerManual    = "manual"
	TriggerSchedule  = "schedule"
	TriggerThreshold = "threshold"
)

const (
	statusRemoved = "removed"
)

const (
	idImagePrefix     = "image:"
	idContainerPrefix = "container:"
	idVolumePrefix    = "volume:"
	idHostPrefix      = "host:"
	idBuildCache      = "build-cache"
)
