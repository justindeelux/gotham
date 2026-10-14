/**
 * Windowing maths for the files list and grid (JUS-137).
 *
 * A directory with thousands of entries used to render every row into the DOM,
 * which made scrolling and select-all slow. These DOM-free helpers compute, from
 * the scroll offset and the viewport size, the slice of items to render plus the
 * pixel padding that stands in for the unrendered ends — so the scrollbar still
 * reflects the whole listing. They are pure so the range is unit-tested directly.
 */

/** WindowRange is a rendered slice `[start, end)` plus its leading/trailing pad. */
export interface WindowRange {
  /** first rendered item index (inclusive) */
  start: number;
  /** last rendered item index (exclusive) */
  end: number;
  /** px spacer above the rendered slice */
  padTop: number;
  /** px spacer below the rendered slice */
  padBottom: number;
}

/** clamp keeps `value` inside [min, max]. */
function clamp(value: number, min: number, max: number): number {
  return Math.min(max, Math.max(min, value));
}

/**
 * windowRange windows `count` uniformly-sized items.
 *
 * `lead` is the height of any sticky element that shares the scroll container
 * above the items (the list header), so the first row is not treated as visible
 * while it still sits under the header. `overscan` renders extra items past each
 * edge so a fast scroll never flashes a blank band.
 */
export function windowRange(
  count: number,
  itemSize: number,
  scrollTop: number,
  viewport: number,
  lead = 0,
  overscan = 4,
): WindowRange {
  if (count <= 0) {
    return { start: 0, end: 0, padTop: 0, padBottom: 0 };
  }
  if (itemSize <= 0) {
    // No measurable item size yet: render everything rather than guess.
    return { start: 0, end: count, padTop: 0, padBottom: 0 };
  }
  const offset = Math.max(0, scrollTop - lead);
  const first = Math.floor(offset / itemSize);
  const last = Math.ceil((offset + Math.max(viewport, 0)) / itemSize);
  const start = clamp(first - overscan, 0, count);
  const end = clamp(Math.max(last + overscan, start + 1), 0, count);
  return {
    start,
    end,
    padTop: start * itemSize,
    padBottom: (count - end) * itemSize,
  };
}

/**
 * gridColumnCount mirrors the CSS `repeat(auto-fill, minmax(minItem, 1fr))`
 * column count for a container `width` px wide with `gap` px between cells.
 */
export function gridColumnCount(
  width: number,
  minItem: number,
  gap: number,
): number {
  if (width <= 0 || minItem <= 0) {
    return 1;
  }
  return Math.max(1, Math.floor((width + gap) / (minItem + gap)));
}

/**
 * clampScrollTop keeps a scroll offset inside the content a listing actually
 * has. After a filter shrinks a long directory the stored offset can point far
 * past the new end; without this the window maths would render an empty slice
 * for one frame (a full-height top spacer, no rows) before the browser clamps
 * the scroller and its scroll event corrects the window.
 */
export function clampScrollTop(
  top: number,
  contentHeight: number,
  viewport: number,
): number {
  return Math.min(Math.max(top, 0), Math.max(0, contentHeight - viewport));
}

/**
 * gridWindow windows a row-major grid. Whole rows are always included so the
 * rendered window lines up with the un-windowed grid's columns; the returned pad
 * is row-aligned too.
 */
export function gridWindow(
  count: number,
  columns: number,
  rowHeight: number,
  scrollTop: number,
  viewport: number,
  overscan = 1,
): WindowRange {
  const cols = Math.max(1, Math.floor(columns));
  const rows = Math.ceil(count / cols);
  const win = windowRange(rows, rowHeight, scrollTop, viewport, 0, overscan);
  return {
    start: win.start * cols,
    end: Math.min(count, win.end * cols),
    padTop: win.padTop,
    padBottom: win.padBottom,
  };
}

/**
 * renderedKey is a collision-safe identity for a rendered row-name sequence.
 * JSON escaping makes it injective, so two different rendered sets can never
 * share a key — a filename may itself contain the separator a plain join would
 * use (a name may hold any byte but "/").
 */
export function renderedKey(names: string[]): string {
  return JSON.stringify(names);
}
