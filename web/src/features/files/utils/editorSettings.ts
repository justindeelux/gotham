import {
  DEFAULT_EDITOR_SETTINGS,
  EDITOR_SETTINGS_VERSION,
  parseEditorSettings,
} from "@/features/files/schemas/editorSettings";
import type { EditorSettings } from "@/features/files/schemas/editorSettings";

/**
 * Per-user localStorage persistence for the editor preferences. Every access
 * is wrapped in try/catch: private mode, quota and blocked storage must never
 * break the editor. A corrupt payload is read as defaults and simply
 * overwritten by the next change.
 */
export type EditorSettingsStorage = Pick<Storage, "getItem" | "setItem">;

/** EDITOR_SETTINGS_MAX_BYTES caps the raw payload; a runaway entry is ignored. */
export const EDITOR_SETTINGS_MAX_BYTES = 4 * 1024;

/** editorSettingsStorageKey scopes preferences to one user on this browser. */
export function editorSettingsStorageKey(userId: string): string {
  return userId === "" ? "" : `gotham.files.editor.v1.${encodeURIComponent(userId)}`;
}

/** readEditorSettings loads one user's preferences, falling back to defaults. */
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

/** writeEditorSettings stores one user's preferences; false means not stored. */
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
