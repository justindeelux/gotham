import { computed, ref } from "vue";

export interface ZoomView {
  scale: number;
  x: number;
  y: number;
}

export const MIN_ZOOM = 0.25;
export const MAX_ZOOM = 8;
export const WHEEL_FACTOR = 1.15;

export function clampZoom(scale: number): number {
  if (!Number.isFinite(scale)) {
    return 1;
  }
  return Math.min(MAX_ZOOM, Math.max(MIN_ZOOM, scale));
}

export function zoomAtPoint(
  view: ZoomView,
  pointX: number,
  pointY: number,
  factor: number,
): ZoomView {
  if (!Number.isFinite(factor) || factor <= 0) {
    return { ...view };
  }
  const scale = clampZoom(view.scale * factor);
  if (scale === view.scale) {
    return { ...view };
  }
  const ratio = scale / view.scale;
  return {
    scale,
    x: pointX - (pointX - view.x) * ratio,
    y: pointY - (pointY - view.y) * ratio,
  };
}

export function panBy(view: ZoomView, deltaX: number, deltaY: number): ZoomView {
  return { ...view, x: view.x + deltaX, y: view.y + deltaY };
}

export function resetView(): ZoomView {
  return { scale: 1, x: 0, y: 0 };
}

export function zoomPercent(scale: number): string {
  return `${Math.round(scale * 100)}%`;
}

export function useImageZoom() {
  const view = ref<ZoomView>(resetView());

  const transform = computed<string>(
    () => `translate(${view.value.x}px, ${view.value.y}px) scale(${view.value.scale})`,
  );

  function zoomAt(pointX: number, pointY: number, factor: number): void {
    view.value = zoomAtPoint(view.value, pointX, pointY, factor);
  }

  function pan(deltaX: number, deltaY: number): void {
    view.value = panBy(view.value, deltaX, deltaY);
  }

  function reset(): void {
    view.value = resetView();
  }

  return { view, transform, zoomAt, pan, reset };
}
