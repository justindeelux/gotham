const en = {
  page: {
    eyebrow: "Operations · Host pressure",
    title: "Monitoring rules",
    description:
      "CPU, memory and filesystem pressure alerts evaluated from the observations nodes already push. The instance defaults apply to every node; a node override wins for that node.",
  },
  scope: {
    label: "Rule scope",
    instance: "Instance defaults",
    hint: "Changing rules applies to the next observations. No history is re-evaluated and no recovery is invented.",
  },
  source: {
    default: "Built-in defaults",
    instance: "Instance defaults",
    node: "Node override",
  },
  rules: {
    cpuTitle: "CPU",
    cpuHint: "Warning at or above the threshold, clear at or below the clear value.",
    memoryTitle: "Memory",
    memoryHint:
      "Used memory as a percentage of total memory. Gotham does not infer OOM kills from this value.",
    spaceTitle: "Filesystem space",
    spaceHint:
      "Covers root, Docker data, managed storage and backups. A full data disk alerts even when root is healthy.",
    inodeTitle: "Filesystem inodes",
    inodeHint:
      "Inode pressure can stop writes while bytes are still free. Unsupported inode counters stay unknown.",
    enabled: "Enabled",
    warning: "Warning",
    critical: "Critical",
    downgrade: "Downgrade critical",
    clear: "Clear",
    confirm: "Warning for",
    criticalFor: "Critical for",
    downgradeFor: "Downgrade for",
    clearFor: "Clear for",
    defaultsNote:
      "Defaults: CPU and memory warn at 90% for 5 minutes and clear at 85% for 1 minute. Filesystems warn at 90% for 1 minute, go critical at 95% for 1 minute, downgrade at 90% for 1 minute and clear at 85% for 1 minute.",
  },
  actions: {
    save: "Save rules",
    reset: "Restore defaults",
    resetNode: "Use instance defaults",
    saved: "Monitoring rules saved",
    resetDone: "Monitoring rules restored",
  },
  validation: {
    range: "Enter a percentage above 0 and at most 100.",
    duration: "Enter a duration between 10 and 3600 seconds.",
    order: "Values must be ordered: clear < downgrade ≤ warning < critical.",
  },
  status: {
    title: "Host pressure",
    refresh: "Refresh",
    fresh: "Fresh observation",
    stale: "Stale observation",
    unknown: "No observation",
    lastObservation: "Last observation:",
    unknownHint:
      "The node has no fresh observation. An unknown or missing sample is not a recovery; the incident stays open until fresh evidence clears it.",
    noActive: "No active CPU, memory or filesystem pressure.",
    suggestion:
      "Inspect storage and logs, or reduce load. Gotham never deletes data or restarts workloads automatically.",
    openMetrics: "Open metrics",
  },
  detectors: {
    cpu: "CPU pressure",
    memory: "Memory pressure",
    space: "Filesystem space",
    inode: "Filesystem inodes",
  },
  severity: {
    warning: "Warning",
    critical: "Critical",
  },
  filesystems: {
    mount: "Mount",
    purpose: "Purpose",
    space: "Space",
    inodes: "Inodes",
    available: "{available} free of {total}",
    unsupported: "Unknown",
  },
};

export type MonitoringMessages = typeof en;
export default en;
