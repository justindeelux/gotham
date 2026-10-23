
export interface WindowRange {
  start: number;
  end: number;
  padTop: number;
  padBottom: number;
}

function clamp(value: number, min: number, max: number): number {
  return Math.min(max, Math.max(min, value));
}

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

export function clampScrollTop(
  top: number,
  contentHeight: number,
  viewport: number,
): number {
  return Math.min(Math.max(top, 0), Math.max(0, contentHeight - viewport));
}

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

export function renderedKey(names: string[]): string {
  return JSON.stringify(names);
}
