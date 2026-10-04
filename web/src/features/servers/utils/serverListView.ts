// Pure view helpers for the servers list page (extracted from ServersPage).
//
// These format and filter servers for display only; they never touch the
// store or the network.

import type { Server } from "@/features/servers/api/servers";

/** Filter chip keys mirroring the servers.html toolbar. */
export type ServerFilter = "all" | "ready" | "offline" | "update";

/**
 * needsAgentUpdate reports whether a server needs an agent update. The
 * control-plane API exposes no agent-version field on Server, so there is
 * currently no data to derive this from — the chip renders with a live count
 * of zero until the backend provides the signal. Never invented.
 */
export function needsAgentUpdate(_server: Server): boolean {
  return false;
}

/** matchesFilter applies the active status chip to one server. */
export function matchesFilter(server: Server, filter: ServerFilter): boolean {
  switch (filter) {
    case "ready":
      return server.status === "ready";
    case "offline":
      return server.status === "offline";
    case "update":
      return needsAgentUpdate(server);
    case "all":
    default:
      return true;
  }
}

/** matchesSearch applies the name/IP/OS query to one server. */
export function matchesSearch(server: Server, query: string): boolean {
  const needle = query.trim().toLowerCase();
  if (needle === "") {
    return true;
  }
  const haystacks = [server.name, server.ip, server.os ?? ""];
  return haystacks.some((field) => field.toLowerCase().includes(needle));
}

/**
 * initials builds the node avatar label: the first letters of up to two words
 * ("gotham-prod-01" -> "GP"). Ported from the server-detail avatar.
 */
export function initials(name: string): string {
  const parts = name.split(/[-_.\s]+/).filter(Boolean);
  const letters = parts.slice(0, 2).map((part) => part[0] ?? "");
  return (letters.join("") || name.slice(0, 2)).toUpperCase();
}

/** keyLabel identifies the stored credential without revealing any secret. */
export function keyLabel(server: Server): string {
  if (server.ssh_key_id) {
    return server.ssh_key_id.slice(0, 8);
  }
  if (server.has_password) {
    return "password stored";
  }
  return "no credentials";
}

/** containerLabel keeps an unknown count (no heartbeat yet) distinct from zero. */
export function containerLabel(server: Server): string {
  if (server.container_count === null || server.container_count === undefined) {
    return "—";
  }
  return `${server.container_count} container${server.container_count === 1 ? "" : "s"}`;
}

/**
 * nodeMeta is the head sub-line: address, OS and architecture. A non-default
 * SSH port is shown with the address (the table always did), so two nodes on
 * the same IP stay distinguishable.
 */
export function nodeMeta(server: Server): string {
  const endpoint = server.port && server.port !== 22 ? `${server.ip}:${server.port}` : server.ip;
  return [endpoint, server.os ?? "—", server.arch ?? "—"].join(" · ");
}
