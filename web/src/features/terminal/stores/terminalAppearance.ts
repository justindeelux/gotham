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

/**
 * Terminal appearance store (JUS-250). One theme preference per user in this
 * browser, read from localStorage when the store is created and persisted on
 * every change (the editorSettings pattern). An in-SPA account switch rebinds
 * it to the new account, so one user never inherits another's theme; a broken
 * or corrupt entry falls back to the factory theme. A `storage` event keeps a
 * second browser tab on the same account in sync without a reload.
 */
export const useTerminalAppearanceStore = defineStore(
  "terminalAppearance",
  () => {
    const storage =
      typeof localStorage === "undefined" ? null : localStorage;
    const userId = ref<string>(getSession().user?.id ?? "");

    /** readFor loads one account's theme, or the factory theme without one. */
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
      // Signing out drops the in-memory theme without overwriting the saved
      // preference; signing in loads that account's own choice.
      theme.value = readFor(nextUserId);
    });

    /** onStorage follows a theme written by another tab of this account. */
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

    // Pinia stores live for the app's lifetime, but HMR and test disposals
    // must not leave a listener behind.
    if (getCurrentScope()) {
      onScopeDispose(() => {
        unsubscribe();
        if (typeof window !== "undefined") {
          window.removeEventListener("storage", onStorage);
        }
      });
    }

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
      theme.value = readFor(nextUserId);
    }

    return { theme, setTheme, reloadForUser };
  },
);
