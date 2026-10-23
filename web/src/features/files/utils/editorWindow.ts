import {
  DEFAULT_EDITOR_WINDOW,
  EDITOR_WINDOW_VERSION,
  parseEditorWindow,
} from "@/features/files/schemas/editorWindow";
import type { EditorWindowState } from "@/features/files/schemas/editorWindow";
import { clampWindowSize } from "@/shared/window/geometry";

export type EditorWindowStorage = Pick<Storage, "getItem" | "setItem">;

export const EDITOR_WINDOW_MAX_BYTES = 4 * 1024;

export function editorWindowStorageKey(userId: string): string {
  return userId === "" ? "" : `gotham.files.editorWindow.v1.${encodeURIComponent(userId)}`;
}

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

export function clampEditorSize(
  width: number,
  height: number,
  viewportWidth: number,
  viewportHeight: number,
  minWidth: number,
  minHeight: number,
): { width: number; height: number } {
  return clampWindowSize(
    width,
    height,
    viewportWidth,
    viewportHeight,
    minWidth,
    minHeight,
  );
}
