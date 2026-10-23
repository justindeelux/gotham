import {
  QUICK_COMMANDS_MAX_BYTES,
  QUICK_COMMANDS_MAX,
  QUICK_COMMAND_VERSION,
  quickCommandListSchema,
  parseQuickCommands,
} from "@/features/terminal/schemas/quickCommands";
import type { QuickCommand } from "@/features/terminal/schemas/quickCommands";

export type QuickCommandStorage = Pick<Storage, "getItem" | "setItem">;

export function quickCommandsStorageKey(userId: string): string {
  return userId === "" ? "" : `gotham.terminal.commands.v1.${encodeURIComponent(userId)}`;
}

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
