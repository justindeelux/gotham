import { defineStore } from "pinia";
import { computed, ref } from "vue";

import {
  DEFAULT_EDITOR_WINDOW,
  EDITOR_WINDOW_CHROME_MARGIN,
  EDITOR_WINDOW_MIN_HEIGHT,
  EDITOR_WINDOW_MIN_WIDTH,
  EDITOR_WINDOW_NARROW_VIEWPORT,
  EDITOR_WINDOW_SIDE_MARGIN,
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

  /** viewportSize reads the live window, defaulting for SSR/tests. */
  function viewportSize(): { width: number; height: number } {
    if (typeof window === "undefined") {
      return { width: 0, height: 0 };
    }
    return { width: window.innerWidth, height: window.innerHeight };
  }

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

  /**
   * fitSize clamps a size into a viewport honoring the display margins, so
   * what is persisted is always what is shown: the side gutters for the
   * width, the shell chrome for the height.
   */
  function fitSize(
    nextWidth: number,
    nextHeight: number,
    viewportWidth: number,
    viewportHeight: number,
  ): { width: number; height: number } {
    return clampEditorSize(
      nextWidth,
      nextHeight,
      Math.max(viewportWidth - EDITOR_WINDOW_SIDE_MARGIN, 1),
      Math.max(viewportHeight - EDITOR_WINDOW_CHROME_MARGIN, 1),
      EDITOR_WINDOW_MIN_WIDTH,
      EDITOR_WINDOW_MIN_HEIGHT,
    );
  }

  /**
   * setSize applies an explicit user resize, clamped to the viewport display
   * limits, and persists it — replacing the saved size. Viewport-only changes
   * never call this, so merely narrowing or growing the window cannot rewrite
   * the saved size.
   */
  function setSize(
    nextWidth: number,
    nextHeight: number,
    viewportWidth: number = viewportSize().width,
    viewportHeight: number = viewportSize().height,
  ): void {
    const fitted = fitSize(nextWidth, nextHeight, viewportWidth, viewportHeight);
    width.value = fitted.width;
    height.value = fitted.height;
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

  /**
   * resizeTo applies an explicit edge or corner resize (JUS-241): the size and
   * the position land together, so the edge opposite the grip stays put, and
   * the result is persisted as one geometry. The size is clamped to the
   * viewport display limits and the position keeps the window on screen.
   */
  function resizeTo(
    next: { width: number; height: number; x: number; y: number },
    viewportWidth: number = viewportSize().width,
    viewportHeight: number = viewportSize().height,
  ): void {
    const fitted = fitSize(next.width, next.height, viewportWidth, viewportHeight);
    const position = clampEditorOffset(
      next.x,
      next.y,
      fitted.width,
      fitted.height,
      viewportWidth,
      viewportHeight,
    );
    width.value = fitted.width;
    height.value = fitted.height;
    // Whole pixels only: the persisted schema keeps integer geometry.
    x.value = Math.round(position.x);
    y.value = Math.round(position.y);
    persist();
  }

  /**
   * setPosition applies a clamped drag position and persists it. The size is
   * the DISPLAYED one (callers pass the fitted size): clamping with the
   * persisted size would snap the window to the origin whenever the saved
   * size overflows the viewport.
   */
  function setPosition(
    nextX: number,
    nextY: number,
    widthValue: number = width.value,
    heightValue: number = height.value,
  ): void {
    const viewport = viewportSize();
    const position = clampEditorOffset(
      nextX,
      nextY,
      widthValue,
      heightValue,
      viewport.width,
      viewport.height,
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
   * resizeBy grows or shrinks the window by a delta from its DISPLAYED size
   * (keyboard arrows), then persists the fitted result like any explicit
   * resize. Seeding from the display means a fitted window never jumps back
   * to the saved size on the first press.
   */
  function resizeBy(
    deltaWidth: number,
    deltaHeight: number,
    viewportWidth: number = viewportSize().width,
    viewportHeight: number = viewportSize().height,
  ): void {
    const displayed = displayGeometry(viewportWidth, viewportHeight);
    setSize(
      displayed.width + deltaWidth,
      displayed.height + deltaHeight,
      viewportWidth,
      viewportHeight,
    );
  }

  /**
   * displayGeometry fits the persisted geometry into a viewport for display
   * only (a narrow viewport drops the dragged offset to the pinned default).
   * It never writes state or storage, so narrowing or growing the viewport
   * alone cannot rewrite the saved size; a zero viewport yields a 1px floor,
   * never a 0px window.
   */
  function displayGeometry(
    viewportWidth: number,
    viewportHeight: number,
  ): { width: number; height: number; x: number | null; y: number | null } {
    const vw = Math.max(1, viewportWidth);
    const vh = Math.max(1, viewportHeight);
    const fitted = fitSize(width.value, height.value, vw, vh);
    if (vw < EDITOR_WINDOW_NARROW_VIEWPORT) {
      return { width: fitted.width, height: fitted.height, x: null, y: null };
    }
    if (x.value === null || y.value === null) {
      return { width: fitted.width, height: fitted.height, x: null, y: null };
    }
    const position = clampEditorOffset(
      x.value,
      y.value,
      fitted.width,
      fitted.height,
      vw,
      vh,
    );
    return { width: fitted.width, height: fitted.height, x: position.x, y: position.y };
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
    resizeTo,
    resetPosition,
    setMinimized,
    resizeBy,
    displayGeometry,
    reloadForUser,
    reset,
  };
});
