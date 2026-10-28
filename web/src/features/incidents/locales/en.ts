const en = {
  title: "Needs attention",
  subtitle: "Active problems, why they matter, and the next useful step.",
  countOne: "{count} active issue",
  countOther: "{count} active issues",
  loading: "Loading issues",
  healthy: {
    title: "Nothing needs attention",
    body: "Every monitored resource is reporting healthy.",
  },
  noData: {
    title: "No monitoring data yet",
    body: "Add a server or an application to start watching its health here.",
  },
  filtered: {
    title: "No matching issues",
    body: "Try a different severity or resource filter.",
  },
  unavailable: {
    title: "Monitoring is unavailable",
    body: "Incident data cannot be read on this instance. Other monitoring details may still work.",
  },
  error: {
    title: "Could not load issues",
    body: "The incidents service is not responding. Nothing has been changed.",
    retry: "Retry",
  },
  filters: {
    severity: "Severity",
    resource: "Resource",
    allSeverities: "All severities",
    allResources: "All resources",
  },
  severity: {
    critical: "Critical",
    warning: "Warning",
  },
  lifecycle: {
    pending: "Confirming",
  },
  coverage: {
    fresh: "Fresh data",
    partial: "Partial data",
    stale: "Stale data",
    unknown: "No current data",
    expected: "Expected maintenance",
  },
  resourceTypes: {
    application: "Application",
    database: "Database",
    service: "Service",
    node: "Server",
    filesystem: "Filesystem",
    domain: "Website",
    unknown: "Resource",
  },
  unknownResourceName: "Unnamed resource",
  reasons: {
    unreachable: "The server is not responding.",
    cpu: "CPU usage is too high.",
    memory: "Memory usage is too high.",
    space: "Disk space is running low.",
    inode: "The filesystem is running out of inodes.",
    restart: "The application keeps restarting.",
    oom: "The application ran out of memory.",
    health: "The application is not passing its health check.",
    website: "The application domain is not answering as expected.",
    unknown: "Gotham detected a problem with this resource.",
  },
  started: "Started {time}",
  duration: "Active for {duration}",
  lastValid: "Last confirmed {time}",
  noValid: "No valid observation yet",
  affectedOne: "{count} affected resource",
  affectedOther: "{count} affected resources",
  groupedHint:
    "These resources sit on this server. Gotham cannot confirm that the server caused them.",
  actions: {
    acknowledge: "Acknowledge",
    snooze: "Snooze",
    snoozeHour: "1 hour",
    snoozeDay: "24 hours",
    open: "Open",
    logs: "View logs",
    server: "View server",
    metrics: "View metrics",
  },
  status: {
    acknowledged: "Acknowledged",
    snoozed: "Snoozed until {time}",
  },
  hold: {
    nodeUnavailable: "Notifications are held while the server is unavailable.",
    expectedMaintenance: "Notifications are held during expected maintenance.",
    generic: "Notifications for this issue are currently held.",
    settings: "Notification settings",
  },
  resolved: {
    title: "Recently resolved",
    empty: "No recently resolved issues.",
    at: "Resolved {time}",
    loadFailed: "Could not load resolved issues.",
  },
  loadMore: "Load more",
  actionsFor: "Actions for {name}",
  snoozeMenu: "Snooze options",
};

export default en;

export type IncidentsMessages = typeof en;
