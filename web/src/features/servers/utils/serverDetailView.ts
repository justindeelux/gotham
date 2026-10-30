
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

export type UsageBlockLevel = "success" | "warning" | "danger";

export interface UsageBlock {
  filled: boolean;
  level: UsageBlockLevel;
}

export interface UsageSegments {
  percentage: number;
  filled: number;
  blocks: UsageBlock[];
}

export const usageBlockCount = 30;

/** Fixed gauge mapping: blocks 1-18 success, 19-24 warning, 25-30 danger. */
export function usageBlockLevel(index: number): UsageBlockLevel {
  if (index < 18) {
    return "success";
  }
  return index < 24 ? "warning" : "danger";
}

export function usageSegments(
  value: number | null | undefined,
): UsageSegments | null {
  if (value === null || value === undefined || Number.isNaN(value)) {
    return null;
  }
  const percentage = toPercent(value);
  const filled = Math.min(
    usageBlockCount,
    Math.round((percentage * usageBlockCount) / 100),
  );
  return {
    percentage,
    filled,
    blocks: Array.from({ length: usageBlockCount }, (_, index) => ({
      filled: index < filled,
      level: usageBlockLevel(index),
    })),
  };
}

export const usageStaleAfterMs = 3 * 60_000;

export function isUsageStale(
  lastSeen: string | null | undefined,
  now = Date.now(),
): boolean {
  if (!lastSeen) {
    return false;
  }
  const seen = Date.parse(lastSeen);
  return !Number.isNaN(seen) && now - seen > usageStaleAfterMs;
}
