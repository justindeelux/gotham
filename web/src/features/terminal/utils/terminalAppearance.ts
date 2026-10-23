import {
  DEFAULT_TERMINAL_APPEARANCE,
  TERMINAL_APPEARANCE_VERSION,
  parseTerminalAppearance,
} from "@/features/terminal/schemas/terminalAppearance";
import type { TerminalAppearance } from "@/features/terminal/schemas/terminalAppearance";

export type TerminalAppearanceStorage = Pick<Storage, "getItem" | "setItem">;

export const TERMINAL_APPEARANCE_MAX_BYTES = 1024;

export function terminalAppearanceStorageKey(userId: string): string {
  return userId === ""
    ? ""
    : `gotham.terminal.appearance.v1.${encodeURIComponent(userId)}`;
}

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
