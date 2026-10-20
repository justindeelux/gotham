import {
  DEFAULT_TERMINAL_APPEARANCE,
  TERMINAL_APPEARANCE_VERSION,
  parseTerminalAppearance,
} from "@/features/terminal/schemas/terminalAppearance";
import type { TerminalAppearance } from "@/features/terminal/schemas/terminalAppearance";

/**
 * Per-user localStorage persistence for the terminal theme, following the
 * editorSettings pattern: every access is wrapped in try/catch, a corrupt or
 * oversized payload reads as defaults, and a broken storage only means the
 * preference is not saved (the live renderer keeps working).
 */
export type TerminalAppearanceStorage = Pick<Storage, "getItem" | "setItem">;

/** TERMINAL_APPEARANCE_MAX_BYTES caps the raw payload; a runaway entry is ignored. */
export const TERMINAL_APPEARANCE_MAX_BYTES = 1024;

/** terminalAppearanceStorageKey scopes the theme to one user on this browser. */
export function terminalAppearanceStorageKey(userId: string): string {
  return userId === ""
    ? ""
    : `gotham.terminal.appearance.v1.${encodeURIComponent(userId)}`;
}

/** readTerminalAppearance loads one user's theme, falling back to the default. */
export function readTerminalAppearance(
  storage: TerminalAppearanceStorage,
  userId: string,
): TerminalAppearance {
  const key = terminalAppearanceStorageKey(userId);
  if (key === "") {
    return DEFAULT_TERMINAL_APPEARANCE;
  }
  let raw: string | null;
  try {
    raw = storage.getItem(key);
  } catch {
    return DEFAULT_TERMINAL_APPEARANCE;
  }
  if (raw === null || raw === "" || raw.length > TERMINAL_APPEARANCE_MAX_BYTES) {
    return DEFAULT_TERMINAL_APPEARANCE;
  }
  try {
    return parseTerminalAppearance(JSON.parse(raw));
  } catch {
    return DEFAULT_TERMINAL_APPEARANCE;
  }
}

/** writeTerminalAppearance stores one user's theme; false means not stored. */
export function writeTerminalAppearance(
  storage: TerminalAppearanceStorage,
  userId: string,
  appearance: TerminalAppearance,
): boolean {
  const key = terminalAppearanceStorageKey(userId);
  if (key === "") {
    return false;
  }
  try {
    storage.setItem(
      key,
      JSON.stringify({
        version: TERMINAL_APPEARANCE_VERSION,
        ...appearance,
      }),
    );
    return true;
  } catch {
    return false;
  }
}
