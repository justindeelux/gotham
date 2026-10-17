import { computed, ref } from "vue";

/**
 * Cursor-centered zoom/pan math behind the image viewer (JUS-204). The pure
 * helpers (clampZoom, zoomAtPoint, panBy, resetView) are DOM-free so the unit
 * harness covers them; {@link useImageZoom} is the thin reactive shell the
 * dialog drives. Coordinates are stage-centered pixels: the cursor point is
 * measured from the stage center, matching `translate(x, y) scale(s)` with a
 * centered transform origin, so the image point under the cursor stays put.
 */

/** ZoomView is the viewer's transform: scale plus stage-centered offset. */
export interface ZoomView {
  scale: number;
  x: number;
  y: number;
}

/** MIN_ZOOM / MAX_ZOOM clamp the wheel zoom at both ends. */
export const MIN_ZOOM = 0.25;
/** MAX_ZOOM is 8x: enough to inspect pixels without float noise. */
export const MAX_ZOOM = 8;
/** WHEEL_FACTOR is one wheel notch's scale step. */
export const WHEEL_FACTOR = 1.15;

/** clampZoom pins a scale to the viewer's [MIN_ZOOM, MAX_ZOOM] range. */
export function clampZoom(scale: number): number {
  if (!Number.isFinite(scale)) {
    return 1;
  }
  return Math.min(MAX_ZOOM, Math.max(MIN_ZOOM, scale));
}

/**
 * zoomAtPoint scales `view` by `factor` around `point` (stage-centered cursor),
 * clamped. The image point under the cursor is preserved: the offset moves so
 * `point` maps to the same image pixel before and after.
 */
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

/** panBy shifts the view by a drag delta in stage pixels. */
export function panBy(view: ZoomView, deltaX: number, deltaY: number): ZoomView {
  return { ...view, x: view.x + deltaX, y: view.y + deltaY };
}

/** resetView returns the fitted view (scale 1, centered). */
export function resetView(): ZoomView {
  return { scale: 1, x: 0, y: 0 };
}

/** zoomPercent renders a scale as "100%". */
export function zoomPercent(scale: number): string {
  return `${Math.round(scale * 100)}%`;
}

/**
 * useImageZoom owns the dialog's reactive view. Wheel and drag handlers call
 * zoomAt/panBy; a new image or the reset control calls reset.
 */
export function useImageZoom() {
  const view = ref<ZoomView>(resetView());

  /** transform is the img's CSS transform (translate, then scale). */
  const transform = computed<string>(
    () => `translate(${view.value.x}px, ${view.value.y}px) scale(${view.value.scale})`,
  );

  /** zoomAt zooms around a stage-centered cursor point by a factor. */
  function zoomAt(pointX: number, pointY: number, factor: number): void {
    view.value = zoomAtPoint(view.value, pointX, pointY, factor);
  }

  /** pan shifts the view by a drag delta. */
  function pan(deltaX: number, deltaY: number): void {
    view.value = panBy(view.value, deltaX, deltaY);
  }

  /** reset restores the fitted view. */
  function reset(): void {
    view.value = resetView();
  }

  return { view, transform, zoomAt, pan, reset };
}
