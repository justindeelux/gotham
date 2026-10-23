import { defineStore } from "pinia";
import { ref } from "vue";

import { DEFAULT_EDITOR_SETTINGS } from "@/features/files/schemas/editorSettings";
import type { EditorSettings } from "@/features/files/schemas/editorSettings";
import {
  readEditorSettings,
  writeEditorSettings,
} from "@/features/files/utils/editorSettings";
import { getSession } from "@/shared/api/token";

export const useEditorSettingsStore = defineStore("filesEditorSettings", () => {
  const userId = getSession().user?.id ?? "";
  const storage = typeof localStorage === "undefined" ? null : localStorage;
  const settings = ref<EditorSettings>(
    storage ? readEditorSettings(storage, userId) : DEFAULT_EDITOR_SETTINGS,
  );

  function update(patch: Partial<EditorSettings>): void {
    settings.value = { ...settings.value, ...patch };
    if (storage) {
      writeEditorSettings(storage, userId, settings.value);
    }
  }

  return { settings, update };
});
