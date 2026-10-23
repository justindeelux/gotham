import { ref } from "vue";

export interface ResizeHandleOptions {
  width: () => number;
  min: number;
  max: number;
  onChange: (_width: number) => void;
}

export function useResizeHandle(options: ResizeHandleOptions) {
  const dragging = ref(false);
  let drag: { pointerId: number; startX: number; startWidth: number } | null = null;

  function clamp(value: number): number {
    return Math.min(options.max, Math.max(options.min, value));
  }

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

  function onPointerMove(event: PointerEvent): void {
    if (drag === null || event.pointerId !== drag.pointerId) {
      return;
    }
    options.onChange(clamp(drag.startWidth + event.clientX - drag.startX));
  }

  function onPointerUp(event: PointerEvent): void {
    if (drag !== null && event.pointerId === drag.pointerId) {
      drag = null;
      dragging.value = false;
    }
  }

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
