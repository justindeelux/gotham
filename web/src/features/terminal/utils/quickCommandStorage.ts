import {
  QUICK_COMMANDS_MAX_BYTES,
  QUICK_COMMANDS_MAX,
  QUICK_COMMAND_VERSION,
  quickCommandListSchema,
  parseQuickCommands,
} from "@/features/terminal/schemas/quickCommands";
import type { QuickCommand } from "@/features/terminal/schemas/quickCommands";

/**
 * Per-user localStorage persistence for terminal quick commands (JUS-249),
 * following the editor-settings approach: every access is guarded, a corrupt
 * or oversized payload reads as an empty list, and a quota/blocked write is
 * reported instead of thrown. Commands are scoped by account id, so an
 * in-SPA account switch never shows the previous account's list.
 *
 * These commands are ordinary browser storage, NOT a secret store. The UI
 * says so; never persist a transcript or terminal credential here.
 */
export type QuickCommandStorage = Pick<Storage, "getItem" | "setItem">;

/** quickCommandsStorageKey scopes commands to one user on this browser. */
export function quickCommandsStorageKey(userId: string): string {
  return userId === "" ? "" : `gotham.terminal.commands.v1.${encodeURIComponent(userId)}`;
}

/** readQuickCommands loads one user's commands, falling back to an empty list. */
export function readQuickCommands(
  storage: QuickCommandStorage,
  userId: string,
): QuickCommand[] {
  const key = quickCommandsStorageKey(userId);
  if (key === "") {
    return [];
  }
  let raw: string | null;
  try {
    raw = storage.getItem(key);
  } catch {
    return [];
  }
  if (raw === null || raw === "" || raw.length > QUICK_COMMANDS_MAX_BYTES) {
    return [];
  }
  try {
    return parseQuickCommands(JSON.parse(raw));
  } catch {
    return [];
  }
}

/** writeQuickCommands stores one user's list; false means nothing was stored. */
export function writeQuickCommands(
  storage: QuickCommandStorage,
  userId: string,
  commands: QuickCommand[],
): boolean {
  const key = quickCommandsStorageKey(userId);
  if (key === "" || commands.length > QUICK_COMMANDS_MAX) {
    return false;
  }
  if (!quickCommandListSchema.safeParse(commands).success) {
    return false;
  }
  let payload: string;
  try {
    payload = JSON.stringify({ version: QUICK_COMMAND_VERSION, commands });
  } catch {
    return false;
  }
  if (payload.length > QUICK_COMMANDS_MAX_BYTES) {
    return false;
  }
  try {
    storage.setItem(key, payload);
    return true;
  } catch {
    return false;
  }
}
