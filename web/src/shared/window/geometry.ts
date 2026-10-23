
export type WindowResizeDirection =
  | "n"
  | "ne"
  | "e"
  | "se"
  | "s"
  | "sw"
  | "w"
  | "nw";

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

export interface WindowBox {
  x: number;
  y: number;
  width: number;
  height: number;
}

export interface WindowResizeLimits {
  minWidth: number;
  minHeight: number;
  maxWidth: number;
  maxHeight: number;
  viewportWidth: number;
  viewportHeight: number;
}

export interface WindowMargins {
  side: number;
  chrome: number;
}

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

function clampSize(value: number, min: number, max: number): number {
  const floor = Math.min(min, max);
  return Math.min(Math.max(value, floor), max);
}
