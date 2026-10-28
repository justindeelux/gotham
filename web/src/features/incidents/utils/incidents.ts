import type { Incident } from "@/features/incidents/api/incidents";

const knownResourceTypes = new Set([
  "application",
  "database",
  "service",
  "node",
  "filesystem",
  "domain",
]);

const reasonKeys: Record<string, string> = {
  connectivity: "incidents.reasons.unreachable",
  host_cpu: "incidents.reasons.cpu",
  host_memory: "incidents.reasons.memory",
  filesystem_space: "incidents.reasons.space",
  filesystem_inode: "incidents.reasons.inode",
  runtime_restart: "incidents.reasons.restart",
  runtime_oom: "incidents.reasons.oom",
  runtime_health: "incidents.reasons.health",
  domain_reachability: "incidents.reasons.website",
};

export interface IncidentGroup {
  incident: Incident;
  children: Incident[];
}

export function isKnownResourceType(value: string | undefined): boolean {
  return value !== undefined && knownResourceTypes.has(value);
}

export function resourceTypeKey(resourceType: string | undefined): string {
  return isKnownResourceType(resourceType)
    ? `incidents.resourceTypes.${resourceType}`
    : "incidents.resourceTypes.unknown";
}

export function reasonKey(detector: string): string {
  return reasonKeys[detector] ?? "incidents.reasons.unknown";
}

export function coverageKey(coverage: string): string {
  switch (coverage) {
    case "fresh":
    case "partial":
    case "stale":
    case "expected":
      return `incidents.coverage.${coverage}`;
    default:
      return "incidents.coverage.unknown";
  }
}

export function severityKey(severity: string): string {
  return severity === "critical"
    ? "incidents.severity.critical"
    : "incidents.severity.warning";
}

export function severityTagType(
  severity: string,
): "error" | "warning" {
  return severity === "critical" ? "error" : "warning";
}

// Compact elapsed time for a row; the exact start is shown separately.
export function elapsedLabel(iso: string, now = Date.now()): string {
  const started = Date.parse(iso);
  if (Number.isNaN(started)) {
    return "—";
  }
  const minutes = Math.max(0, Math.floor((now - started) / 60_000));
  if (minutes < 1) {
    return "<1m";
  }
  if (minutes < 60) {
    return `${minutes}m`;
  }
  const hours = Math.floor(minutes / 60);
  if (hours < 24) {
    const rest = minutes % 60;
    return rest === 0 ? `${hours}h` : `${hours}h ${rest}m`;
  }
  const days = Math.floor(hours / 24);
  const rest = hours % 24;
  return rest === 0 ? `${days}d` : `${days}d ${rest}h`;
}

export function resourceName(incident: Incident, fallback: string): string {
  if (!isKnownResourceType(incident.resource_type)) {
    return fallback;
  }
  const name = incident.resource_name?.trim();
  return name ? name : fallback;
}

export function groupIncidents(incidents: Incident[]): IncidentGroup[] {
  const byId = new Map(incidents.map((incident) => [incident.id, incident]));
  const children = new Map<string, Incident[]>();
  const primaries: Incident[] = [];
  for (const incident of incidents) {
    const parent = incident.grouped_under;
    if (parent && parent !== incident.id && byId.has(parent)) {
      const rows = children.get(parent) ?? [];
      rows.push(incident);
      children.set(parent, rows);
      continue;
    }
    primaries.push(incident);
  }
  return primaries.map((incident) => ({
    incident,
    children: children.get(incident.id) ?? [],
  }));
}

export type DeliveryHold =
  | { kind: "none" }
  | { kind: "known"; key: string };

// The API may hold delivery for producer-supplied reasons. Only the reasons
// Gotham itself defines are rendered; anything else stays a generic label so a
// raw error or path never reaches a member.
export function deliveryHold(hold: string | undefined): DeliveryHold {
  if (!hold) {
    return { kind: "none" };
  }
  if (
    hold === "acknowledged" ||
    hold.startsWith("snoozed until ") ||
    hold.startsWith("coverage ")
  ) {
    return { kind: "none" };
  }
  switch (hold) {
    case "node unavailable":
      return { kind: "known", key: "incidents.hold.nodeUnavailable" };
    case "expected maintenance":
      return { kind: "known", key: "incidents.hold.expectedMaintenance" };
    default:
      return { kind: "known", key: "incidents.hold.generic" };
  }
}
