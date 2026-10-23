import { defineStore } from "pinia";
import { getCurrentScope, onScopeDispose, ref } from "vue";

import {
  DEFAULT_TERMINAL_APPEARANCE,
  isTerminalThemeName,
} from "@/features/terminal/schemas/terminalAppearance";
import type { TerminalThemeName } from "@/features/terminal/schemas/terminalAppearance";
import {
  readTerminalAppearance,
  terminalAppearanceStorageKey,
  writeTerminalAppearance,
} from "@/features/terminal/utils/terminalAppearance";
import { getSession, subscribeSession } from "@/shared/api/token";

export const useTerminalAppearanceStore = defineStore(
  "terminalAppearance",
  () => {
    const storage =
      typeof localStorage === "undefined" ? null : localStorage;
    const userId = ref<string>(getSession().user?.id ?? "");

    function readFor(id: string): TerminalThemeName {
      if (id === "" || storage === null) {
        return DEFAULT_TERMINAL_APPEARANCE.theme;
      }
      return readTerminalAppearance(storage, id).theme;
    }

    const theme = ref<TerminalThemeName>(readFor(userId.value));

    const unsubscribe = subscribeSession((session) => {
      const nextUserId = session.user?.id ?? "";
      if (nextUserId === userId.value) {
        return;
      }
      userId.value = nextUserId;
      theme.value = readFor(nextUserId);
    });

    function onStorage(event: StorageEvent): void {
      if (
        storage === null ||
        userId.value === "" ||
        event.key !== terminalAppearanceStorageKey(userId.value)
      ) {
        return;
      }
      theme.value = readFor(userId.value);
    }

    if (typeof window !== "undefined") {
      window.addEventListener("storage", onStorage);
    }

    if (getCurrentScope()) {
      onScopeDispose(() => {
        unsubscribe();
        if (typeof window !== "undefined") {
          window.removeEventListener("storage", onStorage);
        }
      });
    }

    function setTheme(next: TerminalThemeName): void {
      if (!isTerminalThemeName(next)) {
        return;
      }
      theme.value = next;
      if (storage) {
        writeTerminalAppearance(storage, userId.value, { theme: next });
      }
    }

    function reloadForUser(nextUserId: string): void {
      userId.value = nextUserId;
      theme.value = readFor(nextUserId);
    }

    return { theme, setTheme, reloadForUser };
  },
);
