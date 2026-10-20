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

/**
 * Resize geometry for the floating editor window (JUS-241). The window resizes
 * from all four edges and all four corners, so every grip needs the same
 * answer to two questions: which edges move, and how far the box may grow.
 * The math is shared with the terminal window (JUS-245) in
 * `@/shared/window/geometry`; this module only pins the editor's limits and
 * keeps the editor-facing names.
 */

/** EditorResizeDirection names the eight grips: four edges, four corners. */
export type EditorResizeDirection = WindowResizeDirection;

/** EDITOR_RESIZE_DIRECTIONS lists every grip of the window frame. */
export const EDITOR_RESIZE_DIRECTIONS = WINDOW_RESIZE_DIRECTIONS;

/** EditorBox is a rectangle in viewport coordinates. */
export type EditorBox = WindowBox;

/** EditorResizeLimits is the size band a resize may never leave. */
export type EditorResizeLimits = WindowResizeLimits;

/**
 * editorResizeLimits derives the resize band from the viewport: the same
 * margins the window fits with, so a resize can never grow past the screen.
 * On a viewport narrower than the minimum (a phone) the floor yields to the
 * viewport, exactly like the size clamp does.
 */
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

/**
 * resizeEditorBox applies a pointer delta to the window box for one grip.
 * See {@link resizeWindowBox} for the anchoring rules.
 */
export function resizeEditorBox(
  direction: EditorResizeDirection,
  origin: EditorBox,
  deltaX: number,
  deltaY: number,
  limits: EditorResizeLimits,
): EditorBox {
  return resizeWindowBox(direction, origin, deltaX, deltaY, limits);
}
