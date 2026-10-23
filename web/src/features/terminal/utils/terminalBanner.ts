import logoSource from "@/features/terminal/assets/gotham-ascii-logo.txt?raw";
import { formatVersionTag, getVersion } from "@/features/version";

export const TERMINAL_LOGO = logoSource;

export const TERMINAL_LOGO_SHA256 =
  "76a2f2e19ef0f58326c1f79c5ac837cc25df96f40b7a6d606d292f27b177a274";

export const TERMINAL_LOGO_DISPLAY = TERMINAL_LOGO.replace(/\r?\n/g, "\r\n");

export const TERMINAL_LOGO_COLUMNS = Math.max(
  ...TERMINAL_LOGO.split("\n").map((line) => [...line].length),
);

export const TERMINAL_LOGO_MIN_COLS = TERMINAL_LOGO_COLUMNS;

export const TERMINAL_BANNER_LINES = TERMINAL_LOGO.split("\n").length + 3;

export const TERMINAL_LOGO_MIN_ROWS = TERMINAL_BANNER_LINES + 2;

export interface TerminalBannerInfo {
  version: string | null;
  serverName: string;
  serverId: string;
  target: string;
  serverLabel?: string;
}

export function isHostTerminalTarget(target: string): boolean {
  return target === "" || target === "host";
}

const BIDI_CONTROLS =
  /[\u061c\u200e\u200f\u202a-\u202e\u2066-\u2069\ufeff]/g;

export function sanitizeTerminalText(value: string, maxLength: number): string {
  const cleaned = value
    // eslint-disable-next-line no-control-regex -- stripping ESC/OSC is the point
    .replace(/\u001b\][^\u0007\u001b]*(?:\u0007|\u001b\\)?/g, "")
    // eslint-disable-next-line no-control-regex -- stripping ESC/DCS is the point
    .replace(/\u001b[P^_][^\u001b]*(?:\u001b\\)?/g, "")
    // eslint-disable-next-line no-control-regex -- stripping ESC/CSI is the point
    .replace(/\u001b\[[0-?]*[ -/]*[@-~]/g, "")
    // eslint-disable-next-line no-control-regex -- stripping ESC is the point
    .replace(/\u001b[@-_]|\u001b./g, "")
    .replace(BIDI_CONTROLS, "")
    // eslint-disable-next-line no-control-regex -- stripping controls is the point
    .replace(/[\u0000-\u001f\u007f-\u009f]/g, " ")
    .replace(/\s+/g, " ")
    .trim();
  if (cleaned.length <= maxLength) {
    return cleaned;
  }
  return `${cleaned.slice(0, Math.max(0, maxLength - 1))}…`;
}

const DEFAULT_SERVER_LABEL = "server";

function bannerLabel(info: TerminalBannerInfo, columns: number): string {
  const prefix = `${sanitizeTerminalText(
    info.serverLabel ?? DEFAULT_SERVER_LABEL,
    24,
  )}: `;
  const room = Math.max(1, columns - prefix.length);
  const name =
    sanitizeTerminalText(info.serverName, room) ||
    sanitizeTerminalText(info.serverId, room);
  return `${prefix}${name}`;
}

export function buildTerminalBanner(
  info: TerminalBannerInfo,
  columns: number,
  rows: number = Number.MAX_SAFE_INTEGER,
): string {
  const width = Math.max(1, Math.floor(columns));
  const height = Math.max(1, Math.floor(rows));
  const tag = formatVersionTag(info.version);
  const safeTag =
    tag === null ? null : sanitizeTerminalText(tag, Math.max(1, width - 7));
  const title = sanitizeTerminalText(
    safeTag === null ? "Gotham" : `Gotham ${safeTag}`,
    width,
  );
  const label = sanitizeTerminalText(bannerLabel(info, width), width);
  if (width >= TERMINAL_LOGO_MIN_COLS && height >= TERMINAL_LOGO_MIN_ROWS) {
    return `${TERMINAL_LOGO_DISPLAY}\r\n${title} · ${label}\r\n\r\n`;
  }
  return `${title}\r\n${label}\r\n\r\n`;
}

let runningVersionRequest: Promise<string | null> | null = null;
let knownVersion: string | null = null;

export function knownGothamVersion(): string | null {
  return knownVersion;
}

export function runningGothamVersion(): Promise<string | null> {
  if (runningVersionRequest === null) {
    runningVersionRequest = getVersion()
      .then((version) => {
        knownVersion = version;
        return version;
      })
      .catch(() => {
        runningVersionRequest = null;
        return null;
      });
  }
  return runningVersionRequest;
}
