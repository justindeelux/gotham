import { defineStore } from "pinia";
import { ref } from "vue";

import {
  DEFAULT_TERMINAL_WINDOW,
  TERMINAL_WINDOW_CHROME_MARGIN,
  TERMINAL_WINDOW_MIN_HEIGHT,
  TERMINAL_WINDOW_MIN_WIDTH,
  TERMINAL_WINDOW_NARROW_VIEWPORT,
  TERMINAL_WINDOW_SIDE_MARGIN,
} from "@/features/terminal/schemas/terminal";
import type { TerminalWindowState } from "@/features/terminal/schemas/terminal";
import {
  readTerminalWindow,
  writeTerminalWindow,
} from "@/features/terminal/utils/terminalWindow";
import { getSession } from "@/shared/api/token";
import {
  clampWindowOffset,
  clampWindowSize,
} from "@/shared/window/geometry";

export const useTerminalWindowStore = defineStore("terminalWindow", () => {
  const storage = typeof localStorage === "undefined" ? null : localStorage;
  const userId = ref<string>(getSession().user?.id ?? "");
  const initial = storage
    ? readTerminalWindow(storage, userId.value)
    : DEFAULT_TERMINAL_WINDOW;

  const width = ref<number>(initial.width);
  const height = ref<number>(initial.height);
  const x = ref<number | null>(initial.x);
  const y = ref<number | null>(initial.y);
  const minimized = ref<boolean>(initial.minimized);
  const raised = ref<boolean>(true);

  function viewportSize(): { width: number; height: number } {
    if (typeof window === "undefined") {
      return { width: 0, height: 0 };
    }
    return { width: window.innerWidth, height: window.innerHeight };
  }

  function snapshot(): TerminalWindowState {
    return {
      width: width.value,
      height: height.value,
      x: x.value,
      y: y.value,
      minimized: minimized.value,
    };
  }

  function apply(next: TerminalWindowState): void {
    width.value = next.width;
    height.value = next.height;
    x.value = next.x;
    y.value = next.y;
    minimized.value = next.minimized;
  }

  function persist(): void {
    if (!storage || userId.value === "") {
      return;
    }
    writeTerminalWindow(storage, userId.value, snapshot());
  }

  function reloadForUser(nextUserId: string): void {
    userId.value = nextUserId;
    apply(
      storage
        ? readTerminalWindow(storage, nextUserId)
        : DEFAULT_TERMINAL_WINDOW,
    );
  }

  function reset(): void {
    userId.value = "";
    raised.value = true;
    apply(DEFAULT_TERMINAL_WINDOW);
  }

  function fitSize(
    nextWidth: number,
    nextHeight: number,
    viewportWidth: number,
    viewportHeight: number,
  ): { width: number; height: number } {
    return clampWindowSize(
      nextWidth,
      nextHeight,
      Math.max(viewportWidth - TERMINAL_WINDOW_SIDE_MARGIN, 1),
      Math.max(viewportHeight - TERMINAL_WINDOW_CHROME_MARGIN, 1),
      TERMINAL_WINDOW_MIN_WIDTH,
      TERMINAL_WINDOW_MIN_HEIGHT,
    );
  }

  function setSize(
    nextWidth: number,
    nextHeight: number,
    viewportWidth: number = viewportSize().width,
    viewportHeight: number = viewportSize().height,
  ): void {
    const fitted = fitSize(nextWidth, nextHeight, viewportWidth, viewportHeight);
    width.value = fitted.width;
    height.value = fitted.height;
    if (x.value !== null && y.value !== null) {
      const position = clampWindowOffset(
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

  function resizeTo(
    next: { width: number; height: number; x: number; y: number },
    viewportWidth: number = viewportSize().width,
    viewportHeight: number = viewportSize().height,
  ): void {
    const fitted = fitSize(next.width, next.height, viewportWidth, viewportHeight);
    const position = clampWindowOffset(
      next.x,
      next.y,
      fitted.width,
      fitted.height,
      viewportWidth,
      viewportHeight,
    );
    width.value = fitted.width;
    height.value = fitted.height;
    x.value = Math.round(position.x);
    y.value = Math.round(position.y);
    persist();
  }

  function setPosition(
    nextX: number,
    nextY: number,
    widthValue: number = width.value,
    heightValue: number = height.value,
  ): void {
    const viewport = viewportSize();
    const position = clampWindowOffset(
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

  function resetPosition(): void {
    x.value = null;
    y.value = null;
    persist();
  }

  function setMinimized(next: boolean): void {
    minimized.value = next;
    if (!next) {
      raised.value = true;
    }
    persist();
  }

  function raise(): void {
    raised.value = true;
  }

  function lower(): void {
    raised.value = false;
  }

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

  function displayGeometry(
    viewportWidth: number,
    viewportHeight: number,
  ): { width: number; height: number; x: number | null; y: number | null } {
    const vw = Math.max(1, viewportWidth);
    const vh = Math.max(1, viewportHeight);
    const fitted = fitSize(width.value, height.value, vw, vh);
    if (vw < TERMINAL_WINDOW_NARROW_VIEWPORT) {
      return { width: fitted.width, height: fitted.height, x: null, y: null };
    }
    if (x.value === null || y.value === null) {
      return { width: fitted.width, height: fitted.height, x: null, y: null };
    }
    const position = clampWindowOffset(
      x.value,
      y.value,
      fitted.width,
      fitted.height,
      vw,
      vh,
    );
    return {
      width: fitted.width,
      height: fitted.height,
      x: position.x,
      y: position.y,
    };
  }

  return {
    width,
    height,
    x,
    y,
    minimized,
    raised,
    raise,
    lower,
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
