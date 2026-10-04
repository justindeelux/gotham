// Pure view helpers for the server detail header and tabs
// (extracted from ServerDetailPage).

import type { Server } from "@/features/servers/api/servers";
import { toPercent } from "@/shared/utils/format";

/** initials derives a two-letter avatar from the server name. */
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

/** summaryLine renders the one-line node summary under the title. */
export function summaryLine(server: Server): string {
  const parts = [
    `${server.ip}:${server.port}`,
    server.os ?? "Unknown OS",
    server.arch ?? "Unknown arch",
    server.docker_version ?? "Docker unknown",
  ];
  return parts.join(" · ");
}

/** fallback renders a nullable string field as display text. */
export function fallback(value: string | null): string {
  return value ?? "—";
}

/** authLabel names the stored credential without revealing any secret. */
export function authLabel(server: Server): string {
  if (server.ssh_key_id) {
    return server.ssh_key_id;
  }
  if (server.has_password) {
    return "password stored";
  }
  return "—";
}

/** usageText renders a nullable usage reading as display text.
 *
 * Heartbeat usage arrives as a fraction 0..1 (see toPercent), so the raw
 * reading is normalized before display.
 */
export function usageText(value: number | null): string {
  if (value === null || value === undefined) {
    return "—";
  }
  return `${toPercent(value)}%`;
}
