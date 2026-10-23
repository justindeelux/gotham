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

export const useQuickCommandsStore = defineStore("terminalQuickCommands", () => {
  const storage = typeof localStorage === "undefined" ? null : localStorage;
  const userId = ref(getSession().user?.id ?? "");
  const commands = ref<QuickCommand[]>(
    storage && userId.value !== ""
      ? readQuickCommands(storage, userId.value)
      : [],
  );
  const saveFailed = ref(false);

  function load(nextUserId: string): void {
    userId.value = nextUserId;
    commands.value =
      storage && nextUserId !== ""
        ? readQuickCommands(storage, nextUserId)
        : [];
    saveFailed.value = false;
  }

  subscribeSession((session) => {
    const nextUserId = session.user?.id ?? "";
    if (nextUserId !== userId.value) {
      load(nextUserId);
    }
  });

  function persist(): void {
    if (!storage || userId.value === "") {
      saveFailed.value = true;
      return;
    }
    saveFailed.value = !writeQuickCommands(storage, userId.value, commands.value);
  }

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

  function remove(id: string): void {
    const next = commands.value.filter((command) => command.id !== id);
    if (next.length === commands.value.length) {
      return;
    }
    commands.value = next;
    persist();
  }

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
