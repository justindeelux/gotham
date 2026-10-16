/**
 * Toolchain English catalog: App Store screen copy. Display copy only:
 * tool ids, versions, paths and API payloads stay raw values interpolated
 * as parameters.
 */
const en = {
  page: {
    eyebrow: "Operations · Host tools",
    title: "App Store",
    description:
      "Browse the tool catalog of one node, install or remove tools, and queue several at once. Statuses update live while jobs run.",
    selectNode: "Select a node",
    selectNodePlaceholder: "Pick a node to manage its tools",
    noNodes: "No nodes yet. Add a server first.",
    searchPlaceholder: "Search tools…",
    installSelected: "Install selected",
    retry: "Retry",
    queue: "Queue",
    emptyTools: "No tools reported for this node",
    emptyToolsHint: "The agent on this node did not report any tools.",
    emptySearch: "No tools match the current search",
    emptySearchHint: "Try a different search term.",
    detectOnlyNote: "Detect-only: the agent reports this tool but cannot install it.",
    required: {
      git: "Required for source deployments. Cannot be uninstalled.",
      docker: "Required to run application, database and proxy containers. Cannot be uninstalled.",
    },
    skipped: "Skipped {tools}: already queued",
    enqueued: "Queued {count}",
    uncategorized: "Other",
  },
  columns: {
    tool: "Tool",
    status: "Status",
    queue: "Queue",
    actions: "Actions",
  },
  status: {
    installed: "Installed {version}",
    installedUnknown: "Installed",
    missing: "Not installed",
    outdated: "Update available",
  },
  actions: {
    install: "Install",
    installUpdate: "Install / Update",
    uninstall: "Uninstall",
    cancel: "Cancel",
    details: "Details",
  },
  queue: {
    queued: "Queued",
    running: "Running",
    succeeded: "Succeeded",
    failed: "Failed",
    canceled: "Canceled",
  },
  modal: {
    title: "Install queue",
    queueLabel: "Queue",
    logLabel: "Install output",
    emptyQueue: "No jobs queued yet",
    emptyQueueHint: "Install a tool to watch its progress here.",
    selectJobHint: "Select a job to see its output.",
    loadingLog: "Loading output…",
    logError: "Could not load the output. Retry to try again.",
    logEmpty: "No output yet. Lines appear here while the job runs.",
    logWaiting: "Waiting for output…",
    follow: "Follow",
    following: "Following",
    jumpToLatest: "Jump to latest",
    refreshLog: "Refresh output",
    close: "Close",
    closeHint: "Closing keeps the queue running in the background.",
    retryQueued: "Queued {count}",
    selectedJob: "{tool} · {action}",
    jobStatus: "{job}: {status}",
  },
  errors: {
    sessionExpired: "Your session expired. Please sign in again.",
    unexpected: "Something went wrong. Please try again.",
    notFound: "Node not found. It may have been removed — pick another node.",
    conflict: "That job already left the queue, so it cannot be canceled.",
    agentUnavailable: "The node agent is unreachable. Check that it is online and retry.",
  },
};

export default en;
