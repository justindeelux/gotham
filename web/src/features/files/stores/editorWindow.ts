import { defineStore } from "pinia";
import { computed, ref } from "vue";

import {
  DEFAULT_EDITOR_WINDOW,
  EDITOR_WINDOW_MIN_HEIGHT,
  EDITOR_WINDOW_MIN_WIDTH,
} from "@/features/files/schemas/editorWindow";
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
 * Geometry persists per user in localStorage and is clamped back into the
 * viewport on restore.
 */
export const useEditorWindowStore = defineStore("filesEditorWindow", () => {
  // The key is per user; the id is read once because a different account
  // remounts the app shell (and must not inherit the previous geometry).
  const userId = getSession().user?.id ?? "";
  const storage = typeof localStorage === "undefined" ? null : localStorage;
  const initial = storage ? readEditorWindow(storage, userId) : DEFAULT_EDITOR_WINDOW;

  const width = ref<number>(initial.width);
  const height = ref<number>(initial.height);
  const x = ref<number | null>(initial.x);
  const y = ref<number | null>(initial.y);
  const minimized = ref<boolean>(initial.minimized);

  /** openCount is the rail badge: the number of open editor tabs. */
  const openCount = computed<number>(() => useEditorStore().tabs.length);

  /** persist writes the current geometry; storage failures keep memory intact. */
  function persist(): void {
    if (!storage) {
      return;
    }
    writeEditorWindow(storage, userId, {
      width: width.value,
      height: height.value,
      x: x.value,
      y: y.value,
      minimized: minimized.value,
    });
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
   * clampToViewport pulls restored geometry back on screen after a viewport
   * change (or a smaller screen loading stored desktop geometry). A narrow
   * viewport drops the dragged position to the pinned default.
   */
  function clampToViewport(viewportWidth: number, viewportHeight: number): void {
    const clamped = clampEditorSize(
      width.value,
      height.value,
      viewportWidth,
      viewportHeight,
      EDITOR_WINDOW_MIN_WIDTH,
      EDITOR_WINDOW_MIN_HEIGHT,
    );
    width.value = clamped.width;
    height.value = clamped.height;
    if (viewportWidth < 640) {
      x.value = null;
      y.value = null;
    } else if (x.value !== null && y.value !== null) {
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
    clampToViewport,
  };
});
