import { defineStore } from "pinia";
import { computed, ref } from "vue";

import {
  DEFAULT_EDITOR_WINDOW,
  EDITOR_WINDOW_MIN_HEIGHT,
  EDITOR_WINDOW_MIN_WIDTH,
  EDITOR_WINDOW_NARROW_VIEWPORT,
} from "@/features/files/schemas/editorWindow";
import type { EditorWindowState } from "@/features/files/schemas/editorWindow";
import { useEditorStore } from "@/features/files/stores/editor";
import {
  clampEditorSize,
  readEditorWindow,
  writeEditorWindow,
} from "@/features/files/utils/editorWindow";
import { clampEditorOffset } from "@/features/files/utils/editor";
import { getSession } from "@/shared/api/token";

/**
 * Floating editor window host store (JUS-232). Owns the window geometry
 * (size + dragged position) and the minimized flag so the editor survives
 * route navigation: the window component mounts once in the app shell and
 * reads everything here, while the tabs themselves stay in the editor store.
 * Geometry persists per user in localStorage on user drag/resize only; the
 * viewport fit is derived for display and never overwrites the saved size.
 */
export const useEditorWindowStore = defineStore("filesEditorWindow", () => {
  const storage = typeof localStorage === "undefined" ? null : localStorage;
  // userId rebinds on account change (see reloadForUser): capturing it once
  // would keep writing the previous account's key after an in-SPA login.
  const userId = ref<string>(getSession().user?.id ?? "");
  const initial = storage
    ? readEditorWindow(storage, userId.value)
    : DEFAULT_EDITOR_WINDOW;

  const width = ref<number>(initial.width);
  const height = ref<number>(initial.height);
  const x = ref<number | null>(initial.x);
  const y = ref<number | null>(initial.y);
  const minimized = ref<boolean>(initial.minimized);

  /** openCount is the rail badge: the number of open editor tabs. */
  const openCount = computed<number>(() => useEditorStore().tabs.length);

  /** snapshot reads the persistable geometry. */
  function snapshot(): EditorWindowState {
    return {
      width: width.value,
      height: height.value,
      x: x.value,
      y: y.value,
      minimized: minimized.value,
    };
  }

  /** apply writes one loaded snapshot into memory (no storage touch). */
  function apply(next: EditorWindowState): void {
    width.value = next.width;
    height.value = next.height;
    x.value = next.x;
    y.value = next.y;
    minimized.value = next.minimized;
  }

  /** persist writes the current geometry; storage failures keep memory intact. */
  function persist(): void {
    if (!storage || userId.value === "") {
      return;
    }
    writeEditorWindow(storage, userId.value, snapshot());
  }

  /**
   * reloadForUser rebinds the store to another account (in-SPA login or
   * account switch) and loads that account's persisted geometry, so one
   * browser never leaks window state across accounts.
   */
  function reloadForUser(nextUserId: string): void {
    userId.value = nextUserId;
    apply(storage ? readEditorWindow(storage, nextUserId) : DEFAULT_EDITOR_WINDOW);
  }

  /**
   * reset drops the in-memory geometry and the user binding (logout). It
   * never persists: wiping storage would destroy the signed-out account's
   * saved size, which is still theirs on the next sign-in.
   */
  function reset(): void {
    userId.value = "";
    apply(DEFAULT_EDITOR_WINDOW);
  }

  /** setSize applies a clamped resize and persists it. */
  function setSize(nextWidth: number, nextHeight: number): void {
    const viewportWidth = typeof window === "undefined" ? nextWidth : window.innerWidth;
    const viewportHeight = typeof window === "undefined" ? nextHeight : window.innerHeight;
    const clamped = clampEditorSize(
      nextWidth,
      nextHeight,
      viewportWidth,
      viewportHeight,
      EDITOR_WINDOW_MIN_WIDTH,
      EDITOR_WINDOW_MIN_HEIGHT,
    );
    width.value = clamped.width;
    height.value = clamped.height;
    // A resize can strand a dragged position off-screen; pull it back in.
    if (x.value !== null && y.value !== null) {
      const position = clampEditorOffset(
        x.value,
        y.value,
        width.value,
        height.value,
        viewportWidth,
        viewportHeight,
      );
      x.value = position.x;
      y.value = position.y;
    }
    persist();
  }

  /** setPosition applies a clamped drag position and persists it. */
  function setPosition(nextX: number, nextY: number): void {
    const viewportWidth = typeof window === "undefined" ? 0 : window.innerWidth;
    const viewportHeight = typeof window === "undefined" ? 0 : window.innerHeight;
    const position = clampEditorOffset(
      nextX,
      nextY,
      width.value,
      height.value,
      viewportWidth,
      viewportHeight,
    );
    x.value = position.x;
    y.value = position.y;
    persist();
  }

  /** resetPosition returns the window to its pinned default and persists it. */
  function resetPosition(): void {
    x.value = null;
    y.value = null;
    persist();
  }

  /** setMinimized collapses or restores the window and persists it. */
  function setMinimized(next: boolean): void {
    minimized.value = next;
    persist();
  }

  /**
   * displayGeometry fits the persisted geometry into a viewport for display
   * only (a narrow viewport drops the dragged offset to the pinned default).
   * It never writes state or storage, so a briefly small window cannot shrink
   * the saved desktop size.
   */
  function displayGeometry(
    viewportWidth: number,
    viewportHeight: number,
  ): { width: number; height: number; x: number | null; y: number | null } {
    const clamped = clampEditorSize(
      width.value,
      height.value,
      viewportWidth,
      viewportHeight,
      EDITOR_WINDOW_MIN_WIDTH,
      EDITOR_WINDOW_MIN_HEIGHT,
    );
    if (viewportWidth < EDITOR_WINDOW_NARROW_VIEWPORT) {
      return { width: clamped.width, height: clamped.height, x: null, y: null };
    }
    if (x.value === null || y.value === null) {
      return { width: clamped.width, height: clamped.height, x: null, y: null };
    }
    const position = clampEditorOffset(
      x.value,
      y.value,
      clamped.width,
      clamped.height,
      viewportWidth,
      viewportHeight,
    );
    return { width: clamped.width, height: clamped.height, x: position.x, y: position.y };
  }

  return {
    width,
    height,
    x,
    y,
    minimized,
    openCount,
    setSize,
    setPosition,
    resetPosition,
    setMinimized,
    displayGeometry,
    reloadForUser,
    reset,
  };
});
