import type { Server } from "@/features/servers";

/** One rendered metric bar of a server node card. */
export interface ServerMetricView {
  label: string;
  percentage: number;
  color: string;
}

/** Presentational model for one server node card. */
export interface ServerNodeCardModel {
  id: string;
  name: string;
  subtitle: string;
  initials: string;
  status: Server["status"];
  cpu: ServerMetricView;
  ram: ServerMetricView;
  disk: ServerMetricView;
  containerCount: number | null;
  arch: string | null;
  sshUser: string;
}

/** nodeInitials derives a two-letter node avatar from the server name. */
export function nodeInitials(name: string): string {
  const parts = name.replace(/[^a-zA-Z0-9]+/g, " ").trim().split(/\s+/);
  if (parts.length === 1) {
    return name.slice(0, 2).toUpperCase();
  }
  return (parts[0][0] + parts[1][0]).toUpperCase();
}

/** nodeSubtitle summarizes address, OS, and Docker version. */
export function nodeSubtitle(server: Server): string {
  const bits = [`${server.ip}:${server.port}`];
  if (server.os) {
    bits.push(server.os);
  }
  if (server.docker_version) {
    bits.push(`Docker ${server.docker_version}`);
  }
  return bits.join(" · ");
}

/**
 * buildServerNodeCard maps a server plus its precomputed metric views into
 * the presentational card model. Metric views stay with the caller (the page
 * renders them through the shared usageView threshold), so this helper owns
 * only the pure text derivations.
 */
export function buildServerNodeCard(
  server: Server,
  metrics: { cpu: ServerMetricView; ram: ServerMetricView; disk: ServerMetricView },
): ServerNodeCardModel {
  return {
    id: server.id,
    name: server.name,
    subtitle: nodeSubtitle(server),
    initials: nodeInitials(server.name),
    status: server.status,
    cpu: metrics.cpu,
    ram: metrics.ram,
    disk: metrics.disk,
    containerCount: server.container_count,
    arch: server.arch,
    sshUser: server.ssh_user,
  };
}
