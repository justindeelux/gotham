
import type { Server } from "@/features/servers/api/servers";
import { activeLocale } from "@/shared/i18n/locale";
import { toPercent } from "@/shared/utils/format";

import enCatalog from "../locales/en";
import viCatalog from "../locales/vi";

function catalogFor(locale?: string | null): typeof enCatalog {
  return (locale ?? activeLocale.value) === "vi" ? viCatalog : enCatalog;
}

export function detailInitials(name: string): string {
  const letters = name.replace(/[^A-Za-z0-9]/g, "");
  if (letters.length >= 2) {
    return letters.slice(0, 2).toUpperCase();
  }
  if (letters.length === 1) {
    return letters.toUpperCase();
  }
  return "ND";
}

export function summaryLine(server: Server, locale?: string | null): string {
  const list = catalogFor(locale).list;
  const parts = [
    `${server.ip}:${server.port}`,
    server.os ?? list.unknownOs,
    server.arch ?? list.unknownArch,
    server.docker_version ?? list.unknownDocker,
  ];
  return parts.join(" · ");
}

export function fallback(value: string | null): string {
  return value ?? "—";
}

export function authLabel(server: Server, locale?: string | null): string {
  if (server.ssh_key_id) {
    return server.ssh_key_id;
  }
  if (server.has_password) {
    return catalogFor(locale).credential.passwordStored;
  }
  return "—";
}

export function usageText(value: number | null): string {
  if (value === null || value === undefined) {
    return "—";
  }
  return `${toPercent(value)}%`;
}
