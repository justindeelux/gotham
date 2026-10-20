import logoSource from "@/features/terminal/assets/gotham-ascii-logo.txt?raw";
import { formatVersionTag, getVersion } from "@/features/version";

/**
 * Gotham welcome banner (JUS-250). The logo is a committed text asset imported
 * into the bundle, so its Unicode block characters and every trailing space
 * are preserved and the running binary never reads the original workstation
 * path. The banner is terminal DISPLAY output written to xterm; it is never
 * sent through the PTY input channel.
 */

/** TERMINAL_LOGO is the exact supplied asset (19 lines, last line no newline). */
export const TERMINAL_LOGO = logoSource;

/** TERMINAL_LOGO_SHA256 is the original file's digest, documented for reviewers. */
export const TERMINAL_LOGO_SHA256 =
  "76a2f2e19ef0f58326c1f79c5ac837cc25df96f40b7a6d606d292f27b177a274";

/**
 * TERMINAL_LOGO_DISPLAY is the asset with terminal line endings. The committed
 * file keeps plain LF (and no final newline); xterm needs CRLF or every line
 * continues at the previous cursor column.
 */
export const TERMINAL_LOGO_DISPLAY = TERMINAL_LOGO.replace(/\r?\n/g, "\r\n");

/**
 * logoColumns is the widest logo line in xterm cells. U+2593/U+2591 are
 * single-width in xterm's Unicode tables (verified in Chromium at 63, 80 and
 * 100 columns), so the code-point count of a line is its cell width; there is
 * no double-width assumption here.
 */
export const TERMINAL_LOGO_COLUMNS = Math.max(
  ...TERMINAL_LOGO.split("\n").map((line) => [...line].length),
);

/**
 * TERMINAL_LOGO_MIN_COLS is the narrowest renderer that can show the complete
 * logo on one line per row. Below it the banner falls back to text only, so a
 * narrow pane never wraps the artwork into the prompt.
 */
export const TERMINAL_LOGO_MIN_COLS = TERMINAL_LOGO_COLUMNS;

/** TERMINAL_BANNER_LINES is the logo height plus its title, label and gap. */
export const TERMINAL_BANNER_LINES = TERMINAL_LOGO.split("\n").length + 3;

/**
 * TERMINAL_LOGO_MIN_ROWS keeps the logo plus a prompt line inside a short
 * pane; below it the compact banner is used instead of scrolling it away.
 */
export const TERMINAL_LOGO_MIN_ROWS = TERMINAL_BANNER_LINES + 2;

/** TerminalBannerInfo is the display-only metadata for one new session. */
export interface TerminalBannerInfo {
  /** version is GET /version, or null when metadata is unavailable. */
  version: string | null;
  /** serverName is the target server's display name (fallback: its id). */
  serverName: string;
  /** serverId is the stable server identity shown when no name exists. */
  serverId: string;
  /** target is the session target; only a host target gets a banner. */
  target: string;
  /** serverLabel is the localized metadata label (fallback: "server"). */
  serverLabel?: string;
}

/** isHostTerminalTarget reports whether a session targets the server host. */
export function isHostTerminalTarget(target: string): boolean {
  return target === "" || target === "host";
}

/**
 * BIDI_CONTROLS are Unicode formatting controls that can reorder the banner
 * around a server-supplied name (Trojan Source class attacks).
 */
const BIDI_CONTROLS =
  /[\u061c\u200e\u200f\u202a-\u202e\u2066-\u2069\ufeff]/g;

/**
 * sanitizeTerminalText removes control characters, escape/OSC payloads and
 * bidi overrides, then folds whitespace, so no server name, version or other
 * metadata can inject terminal control sequences or reorder the banner.
 */
export function sanitizeTerminalText(value: string, maxLength: number): string {
  const cleaned = value
    // OSC strings (title, clipboard, links): drop the whole payload.
    // eslint-disable-next-line no-control-regex -- stripping ESC/OSC is the point
    .replace(/\u001b\][^\u0007\u001b]*(?:\u0007|\u001b\\)?/g, "")
    // DCS/SOS/PM/APC strings: drop the whole payload.
    // eslint-disable-next-line no-control-regex -- stripping ESC/DCS is the point
    .replace(/\u001b[P^_][^\u001b]*(?:\u001b\\)?/g, "")
    // CSI sequences (SGR colors, erases, cursor movement).
    // eslint-disable-next-line no-control-regex -- stripping ESC/CSI is the point
    .replace(/\u001b\[[0-?]*[ -/]*[@-~]/g, "")
    // Remaining two-byte escape sequences.
    // eslint-disable-next-line no-control-regex -- stripping ESC is the point
    .replace(/\u001b[@-_]|\u001b./g, "")
    .replace(BIDI_CONTROLS, "")
    // C0/C1 controls (CR, LF, TAB, DEL) become spaces before whitespace folds.
    // eslint-disable-next-line no-control-regex -- stripping controls is the point
    .replace(/[\u0000-\u001f\u007f-\u009f]/g, " ")
    .replace(/\s+/g, " ")
    .trim();
  if (cleaned.length <= maxLength) {
    return cleaned;
  }
  return `${cleaned.slice(0, Math.max(0, maxLength - 1))}…`;
}

/** DEFAULT_SERVER_LABEL is used by non-Vue callers; the store localizes it. */
const DEFAULT_SERVER_LABEL = "server";

/** bannerLabel is the server line shared by the wide and compact banners. */
function bannerLabel(info: TerminalBannerInfo, columns: number): string {
  const prefix = `${sanitizeTerminalText(
    info.serverLabel ?? DEFAULT_SERVER_LABEL,
    24,
  )}: `;
  // Leave room for the prefix so the rendered line never exceeds the pane.
  const room = Math.max(1, columns - prefix.length);
  const name =
    sanitizeTerminalText(info.serverName, room) ||
    sanitizeTerminalText(info.serverId, room);
  return `${prefix}${name}`;
}

/**
 * buildTerminalBanner returns the exact bytes written to xterm once a new
 * host session is confirmed. A renderer wide and tall enough gets the complete
 * asset (63 columns, deterministic rows); anything smaller gets the compact
 * two-line banner so nothing wraps into the prompt. Every dynamic value is
 * sanitized on BOTH paths, and an unknown version only omits the version tag.
 */
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

/**
 * knownGothamVersion returns the already-resolved version without awaiting, so
 * a new session can render the banner immediately when metadata is warm.
 */
export function knownGothamVersion(): string | null {
  return knownVersion;
}

/**
 * runningGothamVersion resolves the running control-plane version through the
 * existing metadata endpoint and caches a SUCCESS for the page's lifetime:
 * opening ten terminals must not issue ten /version requests. A failure is not
 * cached forever (a later session may succeed); it resolves to null so the
 * banner still renders without a version.
 */
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
