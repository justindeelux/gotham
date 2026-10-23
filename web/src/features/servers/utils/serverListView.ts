
import type { Server } from "@/features/servers/api/servers";
import { activeLocale } from "@/shared/i18n/locale";

import enCatalog from "../locales/en";
import viCatalog from "../locales/vi";

function catalogFor(locale?: string | null): typeof enCatalog {
  return (locale ?? activeLocale.value) === "vi" ? viCatalog : enCatalog;
}

export type ServerFilter = "all" | "ready" | "offline" | "update";

export function needsAgentUpdate(_server: Server): boolean {
  return false;
}

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

export function matchesSearch(server: Server, query: string): boolean {
  const needle = query.trim().toLowerCase();
  if (needle === "") {
    return true;
  }
  const haystacks = [server.name, server.ip, server.os ?? ""];
  return haystacks.some((field) => field.toLowerCase().includes(needle));
}

export function initials(name: string): string {
  const parts = name.split(/[-_.\s]+/).filter(Boolean);
  const letters = parts.slice(0, 2).map((part) => part[0] ?? "");
  return (letters.join("") || name.slice(0, 2)).toUpperCase();
}

export function keyLabel(server: Server, locale?: string | null): string {
  if (server.ssh_key_id) {
    return server.ssh_key_id.slice(0, 8);
  }
  if (server.has_password) {
    return catalogFor(locale).credential.passwordStored;
  }
  return catalogFor(locale).credential.none;
}

export function containerLabel(server: Server, locale?: string | null): string {
  if (server.container_count === null || server.container_count === undefined) {
    return "—";
  }
  const word =
    server.container_count === 1
      ? catalogFor(locale).list.containerOne
      : catalogFor(locale).list.containerOther;
  return `${server.container_count} ${word}`;
}

export function nodeMeta(server: Server): string {
  const endpoint = server.port && server.port !== 22 ? `${server.ip}:${server.port}` : server.ip;
  return [endpoint, server.os ?? "—", server.arch ?? "—"].join(" · ");
}
