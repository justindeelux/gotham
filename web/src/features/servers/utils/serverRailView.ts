// Pure view helpers for the server rail (extracted from ServerRail).

import type { Server, ServerStatus } from "@/features/servers/api/servers";

export const statusDots: Record<ServerStatus, string> = {
  ready: "dot--online",
  validating: "dot--idle",
  pending: "dot--idle",
  offline: "dot--offline",
  error: "dot--dnd",
};

export const statusLabels: Record<ServerStatus, string> = {
  ready: "Ready",
  validating: "Validating",
  pending: "Pending",
  offline: "Offline",
  error: "Error",
};

/** serverInitials derives a two-letter avatar from the server name. */
export function serverInitials(name: string): string {
  const parts = name.split(/[^A-Za-z0-9]+/).filter((part) => part.length > 0);
  if (parts.length === 0) {
    return "?";
  }
  if (parts.length === 1) {
    return parts[0].slice(0, 2).toUpperCase();
  }
  return `${parts[0][0]}${parts[1][0]}`.toUpperCase();
}

/** serverTip builds the English tooltip for one rail avatar. */
export function serverTip(server: Server): string {
  return `${server.name} · ${statusLabels[server.status] ?? server.status}`;
}

/** countAlerts counts servers needing operator attention. */
export function countAlerts(servers: Server[]): number {
  return servers.filter(
    (server) => server.status === "offline" || server.status === "error",
  ).length;
}

/** alertsLabel renders the alert count as English screen-reader copy. */
export function alertsLabel(alertCount: number): string {
  return alertCount === 0
    ? "No new alerts"
    : `${alertCount} new alert${alertCount === 1 ? "" : "s"}`;
}
