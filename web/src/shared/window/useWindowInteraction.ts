import { ref } from "vue";

import {
  WINDOW_RESIZE_DIRECTIONS,
  resizeWindowBox,
} from "@/shared/window/geometry";
import type {
  WindowBox,
  WindowResizeDirection,
  WindowResizeLimits,
} from "@/shared/window/geometry";

/**
 * Shared floating-window interaction (JUS-275).
 *
 * Both floating tools drag by their title bar, resize from eight grips and
 * resize from the keyboard through the south-east slider. The mechanics are
 * identical; only the feature store behind them differs. This composable owns
 * the pointer lifecycle, the rendered-box seeding and the keyboard steps, and
 * reports the resulting geometry to the feature — it never imports a feature
 * or its store.
 */

/** DRAG_EXCLUSIONS are the interactive targets a title-bar drag never starts on. */
const DRAG_EXCLUSIONS = [
  "button",
  "a",
  "input",
  "textarea",
  "select",
  "[role='tab']",
  "[role='button']",
  "[contenteditable='true']",
  ".n-popover",
  ".n-dropdown",
].join(", ");

/** RESIZE_KEY_STEP is the keyboard resize step; Shift multiplies it. */
const RESIZE_KEY_STEP = 16;
const RESIZE_KEY_STEP_LARGE = 64;

/** WindowInteractionOptions is the geometry contract the frame reports into. */
export interface WindowInteractionOptions {
  /** size is the DISPLAYED window size, the seed for every interaction. */
  size: () => { width: number; height: number };
  /** limits is the resize band for the current viewport. */
  limits: () => WindowResizeLimits;
  onDrag: (_x: number, _y: number) => void;
  onResize: (_box: WindowBox) => void;
  onResizeBy: (_deltaWidth: number, _deltaHeight: number) => void;
}

/** Pointer grips, minus the south-east corner that also carries the slider. */
export const RESIZE_GRIPS = WINDOW_RESIZE_DIRECTIONS.filter(
  (direction) => direction !== "se",
);

export function useWindowInteraction(options: WindowInteractionOptions) {
  /** frame is the floating window root, measured for drag and resize. */
  const frame = ref<HTMLElement | null>(null);

  let dragState: {
    pointerId: number;
    startX: number;
    startY: number;
    originX: number;
    originY: number;
  } | null = null;

  let resizeState: {
    pointerId: number;
    direction: WindowResizeDirection;
    startX: number;
    startY: number;
    originX: number;
    originY: number;
    originWidth: number;
    originHeight: number;
  } | null = null;

  /** onBarPointerDown starts a title-bar drag from the empty bar surface. */
  function onBarPointerDown(event: PointerEvent): void {
    if (event.button !== 0) {
      return;
    }
    const bar = event.currentTarget;
    const element = frame.value;
    if (!(bar instanceof HTMLElement) || !element) {
      return;
    }
    if (
      event.target instanceof Element &&
      event.target.closest(DRAG_EXCLUSIONS) !== null
    ) {
      return;
    }
    const rect = element.getBoundingClientRect();
    dragState = {
      pointerId: event.pointerId,
      startX: event.clientX,
      startY: event.clientY,
      originX: rect.left,
      originY: rect.top,
    };
    bar.setPointerCapture(event.pointerId);
  }

  /** onBarPointerMove reports the clamped-by-store drag position. */
  function onBarPointerMove(event: PointerEvent): void {
    if (!dragState || event.pointerId !== dragState.pointerId) {
      return;
    }
    options.onDrag(
      dragState.originX + (event.clientX - dragState.startX),
      dragState.originY + (event.clientY - dragState.startY),
    );
  }

  /** onBarPointerUp ends the drag (also on pointercancel). */
  function onBarPointerUp(event: PointerEvent): void {
    if (dragState && event.pointerId === dragState.pointerId) {
      dragState = null;
    }
  }

  /** onResizePointerDown seeds a resize from the rendered box. */
  function onResizePointerDown(
    event: PointerEvent,
    direction: WindowResizeDirection,
  ): void {
    if (event.button !== 0) {
      return;
    }
    const handle = event.currentTarget;
    const element = frame.value;
    if (!(handle instanceof HTMLElement) || !element) {
      return;
    }
    const rect = element.getBoundingClientRect();
    const size = options.size();
    resizeState = {
      pointerId: event.pointerId,
      direction,
      startX: event.clientX,
      startY: event.clientY,
      originX: rect.left,
      originY: rect.top,
      originWidth: size.width,
      originHeight: size.height,
    };
    handle.setPointerCapture(event.pointerId);
    event.preventDefault();
  }

  /** onResizePointerMove applies the grip delta inside the viewport band. */
  function onResizePointerMove(event: PointerEvent): void {
    if (!resizeState || event.pointerId !== resizeState.pointerId) {
      return;
    }
    options.onResize(
      resizeWindowBox(
        resizeState.direction,
        {
          x: resizeState.originX,
          y: resizeState.originY,
          width: resizeState.originWidth,
          height: resizeState.originHeight,
        },
        event.clientX - resizeState.startX,
        event.clientY - resizeState.startY,
        options.limits(),
      ),
    );
  }

  /** onResizePointerUp ends the resize (also on pointercancel). */
  function onResizePointerUp(event: PointerEvent): void {
    if (resizeState && event.pointerId === resizeState.pointerId) {
      resizeState = null;
    }
  }

  /** onResizeKeydown resizes from the keyboard slider grip. */
  function onResizeKeydown(event: KeyboardEvent): void {
    const step = event.shiftKey ? RESIZE_KEY_STEP_LARGE : RESIZE_KEY_STEP;
    let deltaWidth = 0;
    let deltaHeight = 0;
    switch (event.key) {
      case "ArrowRight":
        deltaWidth = step;
        break;
      case "ArrowLeft":
        deltaWidth = -step;
        break;
      case "ArrowDown":
        deltaHeight = step;
        break;
      case "ArrowUp":
        deltaHeight = -step;
        break;
      default:
        return;
    }
    event.preventDefault();
    options.onResizeBy(deltaWidth, deltaHeight);
  }

  return {
    frame,
    onBarPointerDown,
    onBarPointerMove,
    onBarPointerUp,
    onResizePointerDown,
    onResizePointerMove,
    onResizePointerUp,
    onResizeKeydown,
  };
}
