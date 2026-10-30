// Package cleanup scans nodes for reclaimable Docker objects and removes the
// ones an operator selected. Categories are the stable keys the HTTP API and
// the UI reuse.
package cleanup

const (
	CategoryBuildCache        = "build_cache"
	CategoryDanglingImages    = "dangling_images"
	CategoryStoppedContainers = "stopped_containers"
	CategoryUnusedImages      = "unused_images"
	CategoryOrphanedVolumes   = "orphaned_volumes"
)

const (
	StatusOK      = "ok"
	StatusSkipped = "skipped"
	StatusFailed  = "failed"
)

const (
	ReasonOffline     = "offline"
	ReasonBusy        = "busy"
	ReasonUnsupported = "unsupported"
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
	idBuildCache      = "build-cache"
)
