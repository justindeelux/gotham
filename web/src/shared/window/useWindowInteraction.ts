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

const RESIZE_KEY_STEP = 16;
const RESIZE_KEY_STEP_LARGE = 64;

export interface WindowInteractionOptions {
  size: () => { width: number; height: number };
  limits: () => WindowResizeLimits;
  onDrag: (_x: number, _y: number) => void;
  onResize: (_box: WindowBox) => void;
  onResizeBy: (_deltaWidth: number, _deltaHeight: number) => void;
}

export const RESIZE_GRIPS = WINDOW_RESIZE_DIRECTIONS.filter(
  (direction) => direction !== "se",
);

export function useWindowInteraction(options: WindowInteractionOptions) {
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

  function onBarPointerMove(event: PointerEvent): void {
    if (!dragState || event.pointerId !== dragState.pointerId) {
      return;
    }
    options.onDrag(
      dragState.originX + (event.clientX - dragState.startX),
      dragState.originY + (event.clientY - dragState.startY),
    );
  }

  function onBarPointerUp(event: PointerEvent): void {
    if (dragState && event.pointerId === dragState.pointerId) {
      dragState = null;
    }
  }

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

  function onResizePointerUp(event: PointerEvent): void {
    if (resizeState && event.pointerId === resizeState.pointerId) {
      resizeState = null;
    }
  }

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
