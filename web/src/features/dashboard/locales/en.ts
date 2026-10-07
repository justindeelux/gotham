/**
 * Dashboard English catalog: header, KPI tiles, server health grid and
 * the heartbeat/components/activity/alerts aside. Pure helpers read this
 * dictionary through an explicit locale parameter (English when omitted),
 * so unit harnesses keep byte-identical output without a provider.
 */
const en = {
  header: {
    eyebrow: "Overview",
    title: "Dashboard",
    description: "One control plane for every node, application, and database.",
    viewServers: "View servers",
    addServer: "Add server",
  },
  kpi: {
    serversReady: "Servers ready",
    noServers: "No servers yet — add one to begin.",
    unreachable: "{names} unreachable",
    applications: "Running applications",
    viewProjects: "View projects",
    deploys: "Deploys in 24h",
    noDeploys: "No deploy data yet",
    ssl: "SSL certificates",
    noSsl: "No certificate data yet",
    loadFailed: "Could not load applications",
    retry: "Retry",
    noApplications: "No applications yet",
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
    heartbeatMeta: "heartbeat every 10s over gRPC server-authenticated TLS",
    addServer: "Add server",
    containersOne: "{count} container",
    containersOther: "{count} containers",
  },
  aside: {
    heartbeat: "Heartbeat",
    live: "live",
    noHeartbeats: "No heartbeats yet — add a server",
    moreOne: "+{count} more node",
    moreOther: "+{count} more nodes",
    components: "Control-plane components",
    noTelemetry: "No component telemetry yet",
    telemetryNote: "Database, cache, and gateway health is not reported yet.",
    teamActivity: "Team activity",
    noActivity: "No team activity yet",
    alerts: "Alerts",
    unreachableTitle: "{name} unreachable",
    heartbeatLost: "Agent heartbeat lost. Last seen {time}.",
    noAlerts: "No alerts — all nodes healthy",
  },
  deploys: {
    title: "Recent deploys",
    empty: "No deployments yet",
    emptyHint: "Push an application to see build history, durations, and statuses here.",
    queue: "Queue: no data yet",
    noHistory: "No build history to show",
  },
};

export default en;

/** DashboardMessages is the shape every dashboard locale must satisfy. */
export type DashboardMessages = typeof en;
