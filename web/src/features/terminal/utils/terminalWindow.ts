import {
  DEFAULT_TERMINAL_WINDOW,
  TERMINAL_WINDOW_VERSION,
  parseTerminalWindow,
} from "@/features/terminal/schemas/terminal";
import type { TerminalWindowState } from "@/features/terminal/schemas/terminal";

export type TerminalWindowStorage = Pick<Storage, "getItem" | "setItem">;

export const TERMINAL_WINDOW_MAX_BYTES = 4 * 1024;

export function terminalWindowStorageKey(userId: string): string {
  return userId === ""
    ? ""
    : `gotham.terminal.window.v1.${encodeURIComponent(userId)}`;
}

export function readTerminalWindow(
  storage: TerminalWindowStorage,
  userId: string,
): TerminalWindowState {
  const key = terminalWindowStorageKey(userId);
  if (key === "") {
    return DEFAULT_TERMINAL_WINDOW;
  }
  let raw: string | null;
  try {
    raw = storage.getItem(key);
  } catch {
    return DEFAULT_TERMINAL_WINDOW;
  }
  if (raw === null || raw === "" || raw.length > TERMINAL_WINDOW_MAX_BYTES) {
    return DEFAULT_TERMINAL_WINDOW;
  }
  try {
    return parseTerminalWindow(JSON.parse(raw));
  } catch {
    return DEFAULT_TERMINAL_WINDOW;
  }
}

export function writeTerminalWindow(
  storage: TerminalWindowStorage,
  userId: string,
  state: TerminalWindowState,
): boolean {
  const key = terminalWindowStorageKey(userId);
  if (key === "") {
    return false;
  }
  try {
    storage.setItem(
      key,
      JSON.stringify({ version: TERMINAL_WINDOW_VERSION, ...state }),
    );
    return true;
  } catch {
    return false;
  }
}
