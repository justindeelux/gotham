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
    queue: "Queue",
    emptyTools: "No tools reported for this node",
    emptyToolsHint: "The agent on this node did not report any tools.",
    emptySearch: "No tools match the current search",
    emptySearchHint: "Try a different search term.",
    detectOnlyNote: "Detect-only: the agent reports this tool but cannot install it.",
    skipped: "Skipped {tools}: already queued",
    enqueued: "Queued {count}",
    uncategorized: "Other",
  },
  status: {
    installed: "Installed {version}",
    installedUnknown: "Installed",
    missing: "Not installed",
    outdated: "Update available",
  },
  actions: {
    install: "Install",
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
  errors: {
    sessionExpired: "Your session expired. Please sign in again.",
    unexpected: "Something went wrong. Please try again.",
    notFound: "Node not found. It may have been removed — pick another node.",
    conflict: "That job already left the queue, so it cannot be canceled.",
  },
};

export default en;
