const en = {
  header: {
    eyebrow: "Overview",
    title: "Dashboard",
    description: "One control plane for every node, application, and database.",
    addServer: "Add server",
  },
  status: {
    allClear: "Everything is running",
    attentionOne: "{count} thing needs attention",
    attentionOther: "{count} things need attention",
    unavailable: "Status unavailable",
    noServers: "No servers yet",
    starting: "Servers are starting",
    serversReady: "servers ready",
    appsRunning: "apps running",
    appsLoadFailed: "Could not load applications",
    retry: "Retry",
  },
  tiles: {
    incompleteHint: "Some states could not be read",
  },
  health: {
    title: "Server health",
    moreOne: "+{count} more node",
    moreOther: "+{count} more nodes",
    viewAll: "view all servers",
    empty: "No servers yet",
    emptyHint: "Add your first node to see CPU, RAM, and disk health here.",
    heartbeatMeta: "Live status, updated every 10 seconds",
    addServer: "Add server",
    containersOne: "{count} container",
    containersOther: "{count} containers",
  },
  empty: {
    title: "Your dashboard is ready",
    body: "Add your first server to start deploying applications.",
    action: "Add your first server",
  },
  deploys: {
    title: "Recent deploys",
    empty: "No deployments yet",
    emptyHint: "Push an application to see build history, durations, and statuses here.",
    loadFailed: "Could not load recent deploys",
    retry: "Retry",
  },
};

export default en;

export type DashboardMessages = typeof en;
