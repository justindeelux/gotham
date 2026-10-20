import { defineStore } from "pinia";
import { ref } from "vue";

import {
  QUICK_COMMANDS_MAX,
  newQuickCommandId,
  quickCommandDraftSchema,
} from "@/features/terminal/schemas/quickCommands";
import type { QuickCommand, QuickCommandDraft } from "@/features/terminal/schemas/quickCommands";
import {
  readQuickCommands,
  writeQuickCommands,
} from "@/features/terminal/utils/quickCommandStorage";
import { getSession, subscribeSession } from "@/shared/api/token";

/**
 * Per-user saved quick commands (JUS-249).
 *
 * The list is read from this browser's storage once per signed-in account and
 * written back on every mutation. `subscribeSession` reloads it on an in-SPA
 * account switch, so the previous account's commands are never shown. Storage
 * is best-effort: a blocked/quota write leaves the in-memory list working and
 * `saveFailed` tells the UI to warn. Commands are NOT encrypted and must not
 * contain secrets; the panel says so.
 */
export const useQuickCommandsStore = defineStore("terminalQuickCommands", () => {
  const storage = typeof localStorage === "undefined" ? null : localStorage;
  const userId = ref(getSession().user?.id ?? "");
  const commands = ref<QuickCommand[]>(
    storage && userId.value !== ""
      ? readQuickCommands(storage, userId.value)
      : [],
  );
  const saveFailed = ref(false);

  /** load replaces the list with one account's stored commands. */
  function load(nextUserId: string): void {
    userId.value = nextUserId;
    commands.value =
      storage && nextUserId !== ""
        ? readQuickCommands(storage, nextUserId)
        : [];
    saveFailed.value = false;
  }

  // An account switch (or sign-out) must not keep the previous list in memory.
  subscribeSession((session) => {
    const nextUserId = session.user?.id ?? "";
    if (nextUserId !== userId.value) {
      load(nextUserId);
    }
  });

  /** persist writes the current list and records a storage failure. */
  function persist(): void {
    if (!storage || userId.value === "") {
      saveFailed.value = true;
      return;
    }
    saveFailed.value = !writeQuickCommands(storage, userId.value, commands.value);
  }

  /** add validates and appends one command; null means it was rejected. */
  function add(draft: QuickCommandDraft): QuickCommand | null {
    if (commands.value.length >= QUICK_COMMANDS_MAX) {
      return null;
    }
    const parsed = quickCommandDraftSchema(commands.value).safeParse(draft);
    if (!parsed.success) {
      return null;
    }
    const command: QuickCommand = {
      id: newQuickCommandId(),
      label: parsed.data.label,
      command: parsed.data.command,
      ...(parsed.data.abbreviation !== undefined
        ? { abbreviation: parsed.data.abbreviation }
        : {}),
    };
    commands.value = [...commands.value, command];
    persist();
    return command;
  }

  /** update validates and replaces one command; false means it was rejected. */
  function update(id: string, draft: QuickCommandDraft): boolean {
    const index = commands.value.findIndex((command) => command.id === id);
    if (index === -1) {
      return false;
    }
    const parsed = quickCommandDraftSchema(commands.value, id).safeParse(draft);
    if (!parsed.success) {
      return false;
    }
    const next = [...commands.value];
    next[index] = {
      id,
      label: parsed.data.label,
      command: parsed.data.command,
      ...(parsed.data.abbreviation !== undefined
        ? { abbreviation: parsed.data.abbreviation }
        : {}),
    };
    commands.value = next;
    persist();
    return true;
  }

  /** remove deletes one command and persists the remaining list. */
  function remove(id: string): void {
    const next = commands.value.filter((command) => command.id !== id);
    if (next.length === commands.value.length) {
      return;
    }
    commands.value = next;
    persist();
  }

  /** reloadForUser is the explicit account-switch hook for callers/tests. */
  function reloadForUser(nextUserId: string): void {
    load(nextUserId);
  }

  return {
    userId,
    commands,
    saveFailed,
    add,
    update,
    remove,
    reloadForUser,
  };
});
