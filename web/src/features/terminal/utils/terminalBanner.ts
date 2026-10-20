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
 * BLOCK_CELL_WIDTH is the cell width xterm's DOM renderer gives the logo's
 * block glyphs. They are font-fallback glyphs in the mono stack and occupy two
 * cells, so a 63-character line needs 121 cells (blank lines stay 63).
 */
const BLOCK_CELL_WIDTH = 2;

/** logoCellWidth measures one logo line in terminal cells, not characters. */
function logoCellWidth(line: string): number {
  return [...line].reduce(
    (width, char) => width + (char === " " ? 1 : BLOCK_CELL_WIDTH),
    0,
  );
}

/** logoColumns is the widest logo line, in terminal cells. */
export const TERMINAL_LOGO_COLUMNS = Math.max(
  ...TERMINAL_LOGO.split("\n").map(logoCellWidth),
);

/**
 * TERMINAL_LOGO_MIN_COLS is the narrowest renderer that can show the complete
 * logo on one line per row. Below it the banner falls back to text only, so a
 * narrow pane never wraps the artwork into the prompt.
 */
export const TERMINAL_LOGO_MIN_COLS = TERMINAL_LOGO_COLUMNS;

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
}

/** isHostTerminalTarget reports whether a session targets the server host. */
export function isHostTerminalTarget(target: string): boolean {
  return target === "" || target === "host";
}

/**
 * sanitizeTerminalText removes control characters (including ESC) and folds
 * whitespace, so a server name or version can never inject terminal control
 * sequences or break the banner into extra rows.
 */
export function sanitizeTerminalText(value: string, maxLength: number): string {
  const cleaned = value
    // Drop complete CSI sequences first (SGR colors, erases), then any
    // remaining C0/C1 control characters so no escape can reach xterm.
    // eslint-disable-next-line no-control-regex -- stripping ESC is the point
    .replace(/\u001b\[[0-9;?]*[ -/]*[@-~]/g, "")
    // eslint-disable-next-line no-control-regex -- stripping controls is the point
    .replace(/[\u0000-\u001f\u007f-\u009f]/g, " ")
    .replace(/\s+/g, " ")
    .trim();
  if (cleaned.length <= maxLength) {
    return cleaned;
  }
  return `${cleaned.slice(0, Math.max(0, maxLength - 1))}…`;
}

/** BANNER_LABEL_PREFIX is the fixed server metadata prefix. */
const BANNER_LABEL_PREFIX = "server: ";

/** bannerLabel is the server line shared by the wide and compact banners. */
function bannerLabel(info: TerminalBannerInfo, columns: number): string {
  // Leave room for the prefix so the rendered line never exceeds the pane.
  const room = Math.max(1, columns - BANNER_LABEL_PREFIX.length);
  const name =
    sanitizeTerminalText(info.serverName, room) ||
    sanitizeTerminalText(info.serverId, room);
  return `${BANNER_LABEL_PREFIX}${name}`;
}

/**
 * buildTerminalBanner returns the exact bytes written to xterm once a new
 * host session is confirmed. Wide renderers get the complete asset; narrow
 * renderers get a two-line banner so nothing wraps into the prompt. An
 * unknown version simply omits the version tag.
 */
export function buildTerminalBanner(
  info: TerminalBannerInfo,
  columns: number,
): string {
  const width = Math.max(1, Math.floor(columns));
  const tag = formatVersionTag(info.version);
  const title = tag === null ? "Gotham" : `Gotham ${tag}`;
  const label = bannerLabel(info, width);
  if (width >= TERMINAL_LOGO_MIN_COLS) {
    return `${TERMINAL_LOGO}\r\n${title} · ${label}\r\n\r\n`;
  }
  return `${sanitizeTerminalText(title, width)}\r\n${sanitizeTerminalText(
    label,
    width,
  )}\r\n\r\n`;
}

let runningVersionRequest: Promise<string | null> | null = null;

/**
 * runningGothamVersion resolves the running control-plane version through the
 * existing metadata endpoint and caches it for the page's lifetime: opening
 * ten terminals must not issue ten /version requests. A failed lookup resolves
 * to null so the banner still renders without a version.
 */
export function runningGothamVersion(): Promise<string | null> {
  if (runningVersionRequest === null) {
    runningVersionRequest = getVersion()
      .then((version) => version)
      .catch(() => null);
  }
  return runningVersionRequest;
}
