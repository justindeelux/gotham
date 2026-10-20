/**
 * Shared floating-window geometry (JUS-232/JUS-241/JUS-245).
 *
 * The app shell hosts more than one floating surface (the code editor and the
 * terminal window). Their chrome differs, but the geometry contract is the
 * same: a size that never leaves the viewport band, a position that keeps the
 * title bar reachable, and eight resize grips whose opposite edge stays put.
 * The math lives here, free of DOM and store state, so both windows clamp
 * identically and the rules can be tested on their own.
 */

/** WindowResizeDirection names the eight grips: four edges, four corners. */
export type WindowResizeDirection =
  | "n"
  | "ne"
  | "e"
  | "se"
  | "s"
  | "sw"
  | "w"
  | "nw";

/** WINDOW_RESIZE_DIRECTIONS lists every grip of a floating window frame. */
export const WINDOW_RESIZE_DIRECTIONS: readonly WindowResizeDirection[] = [
  "n",
  "ne",
  "e",
  "se",
  "s",
  "sw",
  "w",
  "nw",
];

/** WindowBox is a rectangle in viewport coordinates. */
export interface WindowBox {
  x: number;
  y: number;
  width: number;
  height: number;
}

/** WindowResizeLimits is the size band a resize may never leave. */
export interface WindowResizeLimits {
  minWidth: number;
  minHeight: number;
  maxWidth: number;
  maxHeight: number;
  viewportWidth: number;
  viewportHeight: number;
}

/** WindowMargins reserves the shell chrome a floating window never covers. */
export interface WindowMargins {
  side: number;
  chrome: number;
}

/**
 * clampWindowSize keeps a size inside the usable band: never below the
 * minimum, never larger than the viewport. On a viewport narrower than the
 * minimum (a phone), the floor yields to the viewport so the window always
 * fits with its margins.
 */
export function clampWindowSize(
  width: number,
  height: number,
  viewportWidth: number,
  viewportHeight: number,
  minWidth: number,
  minHeight: number,
): { width: number; height: number } {
  return {
    width: Math.min(
      Math.max(Math.min(minWidth, viewportWidth), Math.round(width)),
      Math.max(Math.min(minWidth, viewportWidth), viewportWidth),
    ),
    height: Math.min(
      Math.max(Math.min(minHeight, viewportHeight), Math.round(height)),
      Math.max(Math.min(minHeight, viewportHeight), viewportHeight),
    ),
  };
}

/**
 * clampWindowOffset keeps a floating window fully inside the viewport after a
 * title-bar drag: the window may never hang off an edge, so its title bar (and
 * the controls in it) can always be grabbed again. A window larger than the
 * viewport (a small screen) pins to the top-left corner instead of producing
 * a negative maximum.
 */
export function clampWindowOffset(
  x: number,
  y: number,
  width: number,
  height: number,
  viewportWidth: number,
  viewportHeight: number,
): { x: number; y: number } {
  return {
    x: Math.min(Math.max(0, x), Math.max(0, viewportWidth - width)),
    y: Math.min(Math.max(0, y), Math.max(0, viewportHeight - height)),
  };
}

/**
 * windowResizeLimits derives the resize band from the viewport: the same
 * margins a window fits with, so a resize can never grow past the screen.
 * On a viewport narrower than the minimum (a phone) the floor yields to the
 * viewport, exactly like the size clamp does.
 */
export function windowResizeLimits(
  viewportWidth: number,
  viewportHeight: number,
  margins: WindowMargins,
  minWidth: number,
  minHeight: number,
): WindowResizeLimits {
  const maxWidth = Math.max(viewportWidth - margins.side, 1);
  const maxHeight = Math.max(viewportHeight - margins.chrome, 1);
  return {
    minWidth: Math.min(minWidth, maxWidth),
    minHeight: Math.min(minHeight, maxHeight),
    maxWidth,
    maxHeight,
    viewportWidth,
    viewportHeight,
  };
}

/**
 * resizeWindowBox applies a pointer delta to a window box for one grip.
 *
 * The edges opposite the grip stay put: dragging the left edge moves the left
 * border and keeps the right one, dragging a corner keeps the opposite
 * corner. The size never leaves the limits band, and a size that is clamped
 * keeps its anchor: the opposite edge still does not move. The band itself
 * follows the anchor, so a window that is not flush with the viewport edge
 * cannot grow past it.
 */
export function resizeWindowBox(
  direction: WindowResizeDirection,
  origin: WindowBox,
  deltaX: number,
  deltaY: number,
  limits: WindowResizeLimits,
): WindowBox {
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
