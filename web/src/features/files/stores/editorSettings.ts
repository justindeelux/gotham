import { defineStore } from "pinia";
import { ref } from "vue";

import { DEFAULT_EDITOR_SETTINGS } from "@/features/files/schemas/editorSettings";
import type { EditorSettings } from "@/features/files/schemas/editorSettings";
import {
  readEditorSettings,
  writeEditorSettings,
} from "@/features/files/utils/editorSettings";
import { getSession } from "@/shared/api/token";

/**
 * Editor preferences store (JUS-219). The settings are one global preference
 * applied to every open tab; they are read once per mount from the signed-in
 * user's localStorage key and written back on each change. Storage failures
 * leave the in-memory value intact, so the editor keeps working.
 */
export const useEditorSettingsStore = defineStore("filesEditorSettings", () => {
  // The key is per user; the id is read once because a different account
  // remounts the page (and must not inherit the previous account's prefs).
  const userId = getSession().user?.id ?? "";
  const storage = typeof localStorage === "undefined" ? null : localStorage;
  const settings = ref<EditorSettings>(
    storage ? readEditorSettings(storage, userId) : DEFAULT_EDITOR_SETTINGS,
  );

  /** update merges one patch into the preferences and persists the result. */
  function update(patch: Partial<EditorSettings>): void {
    settings.value = { ...settings.value, ...patch };
    if (storage) {
      writeEditorSettings(storage, userId, settings.value);
    }
  }

  return { settings, update };
});
