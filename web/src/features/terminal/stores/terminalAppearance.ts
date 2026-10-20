import { defineStore } from "pinia";
import { ref } from "vue";

import {
  DEFAULT_TERMINAL_APPEARANCE,
  isTerminalThemeName,
} from "@/features/terminal/schemas/terminalAppearance";
import type { TerminalThemeName } from "@/features/terminal/schemas/terminalAppearance";
import {
  readTerminalAppearance,
  writeTerminalAppearance,
} from "@/features/terminal/utils/terminalAppearance";
import { getSession, subscribeSession } from "@/shared/api/token";

/**
 * Terminal appearance store (JUS-250). One theme preference per user in this
 * browser, read from localStorage when the store is created and persisted on
 * every change (the editorSettings pattern). An in-SPA account switch rebinds
 * it to the new account, so one user never inherits another's theme; a broken
 * or corrupt entry falls back to the factory theme.
 */
export const useTerminalAppearanceStore = defineStore(
  "terminalAppearance",
  () => {
    const storage =
      typeof localStorage === "undefined" ? null : localStorage;
    const userId = ref<string>(getSession().user?.id ?? "");
    const initial = storage
      ? readTerminalAppearance(storage, userId.value)
      : DEFAULT_TERMINAL_APPEARANCE;
    const theme = ref<TerminalThemeName>(initial.theme);

    subscribeSession((session) => {
      const nextUserId = session.user?.id ?? "";
      if (nextUserId === userId.value) {
        return;
      }
      userId.value = nextUserId;
      // Signing out drops the in-memory theme without overwriting the saved
      // preference; signing in loads that account's own choice.
      theme.value =
        nextUserId === "" || storage === null
          ? DEFAULT_TERMINAL_APPEARANCE.theme
          : readTerminalAppearance(storage, nextUserId).theme;
    });

    /** setTheme applies one named theme and persists it for this account. */
    function setTheme(next: TerminalThemeName): void {
      if (!isTerminalThemeName(next)) {
        return;
      }
      theme.value = next;
      if (storage) {
        writeTerminalAppearance(storage, userId.value, { theme: next });
      }
    }

    /** reloadForUser rebinds the store to an explicit account (tests/admin). */
    function reloadForUser(nextUserId: string): void {
      userId.value = nextUserId;
      theme.value =
        storage === null
          ? DEFAULT_TERMINAL_APPEARANCE.theme
          : readTerminalAppearance(storage, nextUserId).theme;
    }

    return { theme, setTheme, reloadForUser };
  },
);
