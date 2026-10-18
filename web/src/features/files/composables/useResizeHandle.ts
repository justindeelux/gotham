import { ref } from "vue";

/**
 * Pointer/keyboard drag maths for the editor explorer's vertical splitter
 * (JUS-223). The panel owns the width; this composable only turns a drag or an
 * arrow-key press into a clamped width callback.
 */

export interface ResizeHandleOptions {
  /** width reads the panel's current width. */
  width: () => number;
  min: number;
  max: number;
  /** onChange receives the clamped width (the parent applies it). */
  onChange: (_width: number) => void;
}

export function useResizeHandle(options: ResizeHandleOptions) {
  const dragging = ref(false);
  let drag: { pointerId: number; startX: number; startWidth: number } | null = null;

  /** clamp keeps the panel usable without swallowing the editor. */
  function clamp(value: number): number {
    return Math.min(options.max, Math.max(options.min, value));
  }

  /** onPointerDown starts a drag on the splitter. */
  function onPointerDown(event: PointerEvent): void {
    if (event.button !== 0) {
      return;
    }
    const handle = event.currentTarget;
    if (!(handle instanceof HTMLElement)) {
      return;
    }
    drag = {
      pointerId: event.pointerId,
      startX: event.clientX,
      startWidth: options.width(),
    };
    dragging.value = true;
    handle.setPointerCapture(event.pointerId);
    event.preventDefault();
  }

  /** onPointerMove reports the clamped width while dragging. */
  function onPointerMove(event: PointerEvent): void {
    if (drag === null || event.pointerId !== drag.pointerId) {
      return;
    }
    options.onChange(clamp(drag.startWidth + event.clientX - drag.startX));
  }

  /** onPointerUp ends the resize drag. */
  function onPointerUp(event: PointerEvent): void {
    if (drag !== null && event.pointerId === drag.pointerId) {
      drag = null;
      dragging.value = false;
    }
  }

  /** onKeydown resizes from the keyboard (16 px per press). */
  function onKeydown(event: KeyboardEvent): void {
    if (event.key !== "ArrowLeft" && event.key !== "ArrowRight") {
      return;
    }
    event.preventDefault();
    const delta = event.key === "ArrowRight" ? 16 : -16;
    options.onChange(clamp(options.width() + delta));
  }

  return { dragging, onPointerDown, onPointerMove, onPointerUp, onKeydown };
}
