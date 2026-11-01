
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

export const usageBlockCount = 50;

/** Fixed gauge block size; ServerUsageSegments.css keeps the same via --space-1/--space-2. */
export const usageBlockWidth = 4;
export const usageBlockGap = 2;

/** Blocks that fit a measured width; falls back to the default before mount/SSR. */
export function usageBlockCountForWidth(width: number): number {
  if (!(width > 0)) {
    return usageBlockCount;
  }
  return Math.max(1, Math.floor(width / (usageBlockWidth + usageBlockGap)));
}

/** Fixed gauge mapping by position: first 60% success, next 20% warning, last 20% danger. */
export function usageBlockLevel(
  index: number,
  count = usageBlockCount,
): UsageBlockLevel {
  if (index < count * 0.6) {
    return "success";
  }
  return index < count * 0.8 ? "warning" : "danger";
}

export function usageSegments(
  value: number | null | undefined,
  count = usageBlockCount,
): UsageSegments | null {
  if (value === null || value === undefined || Number.isNaN(value)) {
    return null;
  }
  const total = Math.max(1, Math.floor(count));
  const percentage = toPercent(value);
  const filled = Math.min(total, Math.round((percentage * total) / 100));
  return {
    percentage,
    filled,
    blocks: Array.from({ length: total }, (_, index) => ({
      filled: index < filled,
      level: usageBlockLevel(index, total),
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
