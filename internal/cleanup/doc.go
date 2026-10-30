// Package cleanup scans nodes for reclaimable Docker objects and root-only
// host data (the systemd journal and the package cache) and removes the ones
// an operator selected. Categories are the stable keys the HTTP API and the UI
// reuse.
package cleanup

import "sort"

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
	TierSafe      = "safe"
	TierCareful   = "careful"
	TierDangerous = "dangerous"
)

// Category describes one reclaimable category for the UI.
type Category struct {
	Key        string `json:"key"`
	Tier       string `json:"tier"`
	ManualOnly bool   `json:"manual_only"`
}

// categories is the single source of the risk tiers.
var categories = map[string]Category{
	CategoryBuildCache:        {Key: CategoryBuildCache, Tier: TierSafe},
	CategoryDanglingImages:    {Key: CategoryDanglingImages, Tier: TierSafe},
	CategoryStoppedContainers: {Key: CategoryStoppedContainers, Tier: TierSafe},
	CategoryJournal:           {Key: CategoryJournal, Tier: TierSafe},
	CategoryPackageCache:      {Key: CategoryPackageCache, Tier: TierSafe},
	CategoryUnusedImages:      {Key: CategoryUnusedImages, Tier: TierCareful},
	CategoryOrphanedVolumes:   {Key: CategoryOrphanedVolumes, Tier: TierDangerous, ManualOnly: true},
}

// CategoryTier returns the risk tier of a category; unknown keys are careful.
func CategoryTier(key string) string {
	if category, ok := categories[key]; ok {
		return category.Tier
	}
	return TierCareful
}

// Categories lists every known category in key order.
func Categories() []Category {
	list := make([]Category, 0, len(categories))
	for _, category := range categories {
		list = append(list, category)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Key < list[j].Key })
	return list
}

const (
	StatusOK      = "ok"
	StatusSkipped = "skipped"
	StatusFailed  = "failed"
)

const (
	ReasonOffline         = "offline"
	ReasonBusy            = "busy"
	ReasonUnsupported     = "unsupported"
	ReasonHelperOutdated  = "helper_outdated"
	ReasonAgentOutdated   = "agent_outdated"
	ReasonNotFound        = "not_found"
	ReasonJournalArchived = "journal_archived"
	ReasonPackageCache    = "package_cache"
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
