import {
  DEFAULT_EDITOR_SETTINGS,
  EDITOR_SETTINGS_VERSION,
  parseEditorSettings,
} from "@/features/files/schemas/editorSettings";
import type { EditorSettings } from "@/features/files/schemas/editorSettings";

export type EditorSettingsStorage = Pick<Storage, "getItem" | "setItem">;

export const EDITOR_SETTINGS_MAX_BYTES = 4 * 1024;

export function editorSettingsStorageKey(userId: string): string {
  return userId === "" ? "" : `gotham.files.editor.v1.${encodeURIComponent(userId)}`;
}

export function readEditorSettings(
  storage: EditorSettingsStorage,
  userId: string,
): EditorSettings {
  const key = editorSettingsStorageKey(userId);
  if (key === "") {
    return DEFAULT_EDITOR_SETTINGS;
  }
  let raw: string | null;
  try {
    raw = storage.getItem(key);
  } catch {
    return DEFAULT_EDITOR_SETTINGS;
  }
  if (raw === null || raw === "" || raw.length > EDITOR_SETTINGS_MAX_BYTES) {
    return DEFAULT_EDITOR_SETTINGS;
  }
  try {
    return parseEditorSettings(JSON.parse(raw));
  } catch {
    return DEFAULT_EDITOR_SETTINGS;
  }
}

export function writeEditorSettings(
  storage: EditorSettingsStorage,
  userId: string,
  settings: EditorSettings,
): boolean {
  const key = editorSettingsStorageKey(userId);
  if (key === "") {
    return false;
  }
  try {
    storage.setItem(
      key,
      JSON.stringify({ version: EDITOR_SETTINGS_VERSION, ...settings }),
    );
    return true;
  } catch {
    return false;
  }
}
