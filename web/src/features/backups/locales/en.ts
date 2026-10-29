const en = {
  page: {
    eyebrow: "Operations · Data protection",
    title: "Backups",
    description:
      "Every backup run and schedule across your projects, and the storage " +
      "destinations the control plane writes to.",
  },
  tabs: {
    backups: "Backups",
    storage: "Storage",
  },
  filters: {
    allTypes: "All resource types",
    allStatuses: "All statuses",
    typeAria: "Filter by resource type",
    statusAria: "Filter by backup status",
    refresh: "Refresh",
  },
  runs: {
    title: "Recent backups",
    empty: "No backups yet",
    emptyFiltered: "No backups match these filters",
    emptyHint:
      "Queue a backup now, or open a resource and add a schedule so its data is captured automatically.",
    restore: "Restore",
    cleanupPending: "Cleanup pending",
    retryCleanup: "Retry cleanup",
    discardCleanup: "Discard",
    discardCleanupConfirm:
      "Forget this row without deleting its artifact? The artifact stays in the provider and cannot be managed from here.",
    cleanupDone: "Backup cleanup completed.",
    cleanupDiscarded: "Pending cleanup row discarded.",
    inProgress: "in progress",
    seconds: "{count} s",
    minutes: "{count} min",
    minutesSeconds: "{minutes} min {seconds} s",
    columns: {
      resource: "Resource",
      status: "Status",
      size: "Size",
      provider: "Storage",
      started: "Started",
      duration: "Duration",
      actions: "Actions",
    },
  },
  schedules: {
    title: "Schedules",
    add: "New schedule",
    empty: "No schedules yet — automatic backups are off",
    keepLastLabel: "Keep the last scheduled backups",
    keepLastHint:
      "Older scheduled backups are deleted automatically. Manual backups are never deleted automatically.",
    keepAll:
      "Keeps every scheduled backup — edit the schedule to set a limit",
    keepLast: "Keeps the last {count} scheduled backups",
    keepLastInvalid: "Keep the last must be between 1 and 365 backups.",
  },
  errors: {
    noSpace: "Not enough free space on the backup disk",
  },
  backupNow: {
    action: "Backup now",
    title: "Back up a resource",
    resource: "Resource",
    provider: "Storage",
    note: "The resource is briefly unavailable while the backup runs.",
    submit: "Start backup",
    chooseResource: "Choose a resource to back up.",
  },
  schedule: {
    chooseResource: "Choose a resource",
    preset: "How often",
  },
  storage: {
    title: "Storage providers",
    add: "Add storage provider",
    addTitle: "Add storage provider",
    editTitle: "Edit storage provider",
    name: "Name",
    kind: "Kind",
    endpoint: "Endpoint",
    region: "Region",
    bucket: "Bucket",
    prefix: "Prefix",
    accessKey: "Access key",
    secretKey: "Secret key",
    localNote:
      "Local disk keeps artifacts on the control plane. It needs no setup and cannot be deleted.",
    builtin: "Built in",
    empty: "No storage providers yet",
    onlyBuiltin:
      "Backups are kept on the control plane disk. Add an S3-compatible provider to keep copies somewhere else.",
    localDisk: "Local disk",
    deletedProvider: "Deleted storage",
    providerRemoved: "Provider removed",
    unknownProvider: "Storage",
    usage: "{count} backups · {size}",
    freeSpace: "{free} free of {total}",
    deleteConfirm:
      "Delete {name}? Backups stored there become unreadable and cannot be restored. A provider still used by a schedule cannot be deleted.",
  },
  resourceTypes: {
    application: "Application",
    database: "Database",
    service: "Service",
  },
  resources: {
    unknown: "Unknown resource ({id})",
    shortId: "Resource {id}",
  },
  cron: {
    dailyAtTwo: "Every day at 02:00",
    everySixHours: "Every 6 hours",
    weeklySunday: "Every Sunday at 03:00",
    custom: "Custom schedule ({cron})",
  },
  restore: {
    title: "Restore backup",
    prompt: "Restore {name} from the backup {backup} ({when}, {size})?",
    confirm: "Restore · overwrite data",
    warningDatabase:
      "The restore overwrites the current data of this database. This cannot be undone — back up first if the live data still matters.",
    warningApplication:
      "The restore overwrites the stored application data. This cannot be undone — back up first if the live data still matters.",
    warningService:
      "The restore overwrites the stored service volumes. This cannot be undone — back up first if the live data still matters.",
  },
  resourceTab: {
    storageHint: "Storage providers are managed in one place.",
    openStorage: "Open Backups → Storage",
    contentsTitle: "What is included",
    contentsEmptyApplication:
      "This application has no persistent storage to back up. Add a storage mapping first.",
    contentsEmptyService:
      "This service has no named volumes or bind mounts to back up.",
    contentsKind: {
      volume: "Volume",
      bind: "Bind mount",
      dump: "Database dump",
    },
    pauseDuringBackup: "Stop the resource while the archive is taken",
    pauseHelper:
      "The backup is consistent, but the resource is unavailable until it finishes.",
    noPause: "Do not stop the resource",
    noPauseHelper:
      "No downtime, but files written during the backup may be inconsistent.",
    noPauseTag: "Did not stop",
  },
};

export default en;
