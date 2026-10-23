import {
  EDITOR_WINDOW_CHROME_MARGIN,
  EDITOR_WINDOW_MIN_HEIGHT,
  EDITOR_WINDOW_MIN_WIDTH,
  EDITOR_WINDOW_SIDE_MARGIN,
} from "@/features/files/schemas/editorWindow";
import {
  WINDOW_RESIZE_DIRECTIONS,
  resizeWindowBox,
  windowResizeLimits,
} from "@/shared/window/geometry";
import type {
  WindowBox,
  WindowResizeDirection,
  WindowResizeLimits,
} from "@/shared/window/geometry";

export type EditorResizeDirection = WindowResizeDirection;

export const EDITOR_RESIZE_DIRECTIONS = WINDOW_RESIZE_DIRECTIONS;

export type EditorBox = WindowBox;

export type EditorResizeLimits = WindowResizeLimits;

export function editorResizeLimits(
  viewportWidth: number,
  viewportHeight: number,
): EditorResizeLimits {
  return windowResizeLimits(
    viewportWidth,
    viewportHeight,
    { side: EDITOR_WINDOW_SIDE_MARGIN, chrome: EDITOR_WINDOW_CHROME_MARGIN },
    EDITOR_WINDOW_MIN_WIDTH,
    EDITOR_WINDOW_MIN_HEIGHT,
  );
}

export function resizeEditorBox(
  direction: EditorResizeDirection,
  origin: EditorBox,
  deltaX: number,
  deltaY: number,
  limits: EditorResizeLimits,
): EditorBox {
  return resizeWindowBox(direction, origin, deltaX, deltaY, limits);
}
