import {
  EDITOR_WINDOW_CHROME_MARGIN,
  EDITOR_WINDOW_MIN_HEIGHT,
  EDITOR_WINDOW_MIN_WIDTH,
  EDITOR_WINDOW_SIDE_MARGIN,
} from "@/features/files/schemas/editorWindow";

/**
 * Resize geometry for the floating editor window (JUS-241). The window resizes
 * from all four edges and all four corners, so every grip needs the same
 * answer to two questions: which edges move, and how far the box may grow.
 * The math lives here, free of DOM and store state, so it can be reasoned
 * about (and tested) on its own.
 */

/** EditorResizeDirection names the eight grips: four edges, four corners. */
export type EditorResizeDirection =
  | "n"
  | "ne"
  | "e"
  | "se"
  | "s"
  | "sw"
  | "w"
  | "nw";

/** EDITOR_RESIZE_DIRECTIONS lists every grip of the window frame. */
export const EDITOR_RESIZE_DIRECTIONS: readonly EditorResizeDirection[] = [
  "n",
  "ne",
  "e",
  "se",
  "s",
  "sw",
  "w",
  "nw",
];

/** EditorBox is a rectangle in viewport coordinates. */
export interface EditorBox {
  x: number;
  y: number;
  width: number;
  height: number;
}

/** EditorResizeLimits is the size band a resize may never leave. */
export interface EditorResizeLimits {
  minWidth: number;
  minHeight: number;
  maxWidth: number;
  maxHeight: number;
  viewportWidth: number;
  viewportHeight: number;
}

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
  const maxWidth = Math.max(viewportWidth - EDITOR_WINDOW_SIDE_MARGIN, 1);
  const maxHeight = Math.max(viewportHeight - EDITOR_WINDOW_CHROME_MARGIN, 1);
  return {
    minWidth: Math.min(EDITOR_WINDOW_MIN_WIDTH, maxWidth),
    minHeight: Math.min(EDITOR_WINDOW_MIN_HEIGHT, maxHeight),
    maxWidth,
    maxHeight,
    viewportWidth,
    viewportHeight,
  };
}

/**
 * resizeEditorBox applies a pointer delta to the window box for one grip.
 *
 * The edges opposite the grip stay put: dragging the left edge moves the left
 * border and keeps the right one, dragging a corner keeps the opposite
 * corner. The size never leaves the limits band, and a size that is clamped
 * keeps its anchor: the opposite edge still does not move. The band itself
 * follows the anchor, so a window that is not flush with the viewport edge
 * cannot grow past it.
 */
export function resizeEditorBox(
  direction: EditorResizeDirection,
  origin: EditorBox,
  deltaX: number,
  deltaY: number,
  limits: EditorResizeLimits,
): EditorBox {
  const movesLeft = direction === "w" || direction === "nw" || direction === "sw";
  const movesRight =
    direction === "e" || direction === "ne" || direction === "se";
  const movesTop = direction === "n" || direction === "ne" || direction === "nw";
  const movesBottom =
    direction === "s" || direction === "sw" || direction === "se";

  // The room a grip has: the room between the anchored edge and the viewport
  // edge it may grow towards.
  const roomWidth = movesRight
    ? limits.viewportWidth - origin.x
    : origin.x + origin.width;
  const roomHeight = movesBottom
    ? limits.viewportHeight - origin.y
    : origin.y + origin.height;

  const maxWidth = Math.min(limits.maxWidth, roomWidth);
  const maxHeight = Math.min(limits.maxHeight, roomHeight);

  const width = movesRight
    ? clampSize(origin.width + deltaX, limits.minWidth, maxWidth)
    : movesLeft
      ? clampSize(origin.width - deltaX, limits.minWidth, maxWidth)
      : origin.width;
  const height = movesBottom
    ? clampSize(origin.height + deltaY, limits.minHeight, maxHeight)
    : movesTop
      ? clampSize(origin.height - deltaY, limits.minHeight, maxHeight)
      : origin.height;

  return {
    x: movesLeft ? origin.x + (origin.width - width) : origin.x,
    y: movesTop ? origin.y + (origin.height - height) : origin.y,
    width,
    height,
  };
}

/**
 * clampSize keeps a size inside its band. The floor yields to the ceiling, so
 * a viewport too small for the minimum size still fits the window on screen.
 */
function clampSize(value: number, min: number, max: number): number {
  const floor = Math.min(min, max);
  return Math.min(Math.max(value, floor), max);
}
