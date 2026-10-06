// Pure view helpers for the server rail (extracted from ServerRail).

import type { Server, ServerStatus } from "@/features/servers/api/servers";
import { activeLocale } from "@/shared/i18n/locale";

import enCatalog from "../locales/en";
import viCatalog from "../locales/vi";

/** catalogFor selects the servers display dictionary for one locale. */
function catalogFor(locale?: string | null): typeof enCatalog {
  return (locale ?? activeLocale.value) === "vi" ? viCatalog : enCatalog;
}

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

/** statusLabelFor renders one lifecycle state in the display locale. */
export function statusLabelFor(status: ServerStatus, locale?: string | null): string {
  return catalogFor(locale).status[status] ?? status;
}

/** serverTip builds the tooltip for one rail avatar. */
export function serverTip(server: Server, locale?: string | null): string {
  return `${server.name} · ${statusLabelFor(server.status, locale)}`;
}

/** countAlerts counts servers needing operator attention. */
export function countAlerts(servers: Server[]): number {
  return servers.filter(
    (server) => server.status === "offline" || server.status === "error",
  ).length;
}

/** alertsLabel renders the alert count as screen-reader copy. */
export function alertsLabel(alertCount: number, locale?: string | null): string {
  const rail = catalogFor(locale).rail;
  if (alertCount === 0) {
    return rail.noAlerts;
  }
  if (alertCount === 1) {
    return rail.alertOne;
  }
  return rail.alertOther.replace("{count}", String(alertCount));
}
