import type { Server } from "@/features/servers";

export interface ServerMetricView {
  label: string;
  percentage: number;
  color: string;
}

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

export function nodeInitials(name: string): string {
  const parts = name.replace(/[^a-zA-Z0-9]+/g, " ").trim().split(/\s+/);
  if (parts.length === 1) {
    return name.slice(0, 2).toUpperCase();
  }
  return (parts[0][0] + parts[1][0]).toUpperCase();
}

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
