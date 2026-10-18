import {
  DEFAULT_EDITOR_WINDOW,
  EDITOR_WINDOW_VERSION,
  parseEditorWindow,
} from "@/features/files/schemas/editorWindow";
import type { EditorWindowState } from "@/features/files/schemas/editorWindow";

/**
 * Per-user localStorage persistence for the floating editor window geometry
 * (JUS-232). Every access is wrapped in try/catch: private mode, quota and
 * blocked storage must never break the editor. A corrupt payload reads as
 * defaults and is overwritten by the next change.
 */
export type EditorWindowStorage = Pick<Storage, "getItem" | "setItem">;

/** EDITOR_WINDOW_MAX_BYTES caps the raw payload; a runaway entry is ignored. */
export const EDITOR_WINDOW_MAX_BYTES = 4 * 1024;

/** editorWindowStorageKey scopes geometry to one user on this browser. */
export function editorWindowStorageKey(userId: string): string {
  return userId === "" ? "" : `gotham.files.editorWindow.v1.${encodeURIComponent(userId)}`;
}

/** readEditorWindow loads one user's geometry, falling back to defaults. */
export function readEditorWindow(
  storage: EditorWindowStorage,
  userId: string,
): EditorWindowState {
  const key = editorWindowStorageKey(userId);
  if (key === "") {
    return DEFAULT_EDITOR_WINDOW;
  }
  let raw: string | null;
  try {
    raw = storage.getItem(key);
  } catch {
    return DEFAULT_EDITOR_WINDOW;
  }
  if (raw === null || raw === "" || raw.length > EDITOR_WINDOW_MAX_BYTES) {
    return DEFAULT_EDITOR_WINDOW;
  }
  try {
    return parseEditorWindow(JSON.parse(raw));
  } catch {
    return DEFAULT_EDITOR_WINDOW;
  }
}

/** writeEditorWindow stores one user's geometry; false means not stored. */
export function writeEditorWindow(
  storage: EditorWindowStorage,
  userId: string,
  state: EditorWindowState,
): boolean {
  const key = editorWindowStorageKey(userId);
  if (key === "") {
    return false;
  }
  try {
    storage.setItem(
      key,
      JSON.stringify({ version: EDITOR_WINDOW_VERSION, ...state }),
    );
    return true;
  } catch {
    return false;
  }
}

/**
 * clampEditorSize keeps a resize drag inside the usable band: never below the
 * minimum, never larger than the viewport. On a viewport narrower than the
 * minimum (a phone), the floor yields to the viewport so the window always
 * fits with its margins.
 */
export function clampEditorSize(
  width: number,
  height: number,
  viewportWidth: number,
  viewportHeight: number,
  minWidth: number,
  minHeight: number,
): { width: number; height: number } {
  return {
    width: Math.min(
      Math.max(Math.min(minWidth, viewportWidth), Math.round(width)),
      Math.max(Math.min(minWidth, viewportWidth), viewportWidth),
    ),
    height: Math.min(
      Math.max(Math.min(minHeight, viewportHeight), Math.round(height)),
      Math.max(Math.min(minHeight, viewportHeight), viewportHeight),
    ),
  };
}
