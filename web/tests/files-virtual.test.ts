// JUS-137 windowing maths. The list/grid render only the visible slice plus
// overscan; these pure helpers decide that slice and the pixel spacers that
// stand in for the unrendered rows. The tests pin the invariants the component
// relies on: the rendered slice is bounded on a huge directory, the spacers plus
// the rendered items always sum to the whole listing (so the scrollbar and
// cross-row select-all stay honest), the sticky-header lead is not counted as
// visible, and the grid window is always row-aligned.
import { describe, expect, it } from "vitest";

import {
  clampScrollTop,
  gridColumnCount,
  gridWindow,
  renderedKey,
  windowRange,
} from "@/features/files/utils/virtual";

describe("windowRange", () => {
  it("renders a bounded slice at the top of a 5,000-row directory", () => {
    const range = windowRange(5000, 36, 0, 500, 34);
    expect(range.start).toBe(0);
    // ~14 visible rows + overscan, nowhere near 5,000.
    expect(range.end).toBe(18);
    expect(range.padTop).toBe(0);
    // The spacer covers every unrendered row.
    expect(range.padBottom).toBe((5000 - 18) * 36);
  });

  it("keeps the item count honest mid-scroll (spacers + rows = whole list)", () => {
    const range = windowRange(5000, 36, 36_000, 500, 34);
    const rendered = range.end - range.start;
    expect(rendered).toBeLessThan(40);
    expect(range.padTop + rendered * 36 + range.padBottom).toBe(5000 * 36);
  });

  it("clamps the window to the end of the list", () => {
    const range = windowRange(200, 36, 199 * 36, 500, 34);
    expect(range.end).toBe(200);
    expect(range.padBottom).toBe(0);
  });

  it("does not count a row hidden under the sticky header as visible", () => {
    // scrollTop is under the 34px lead: the first row is still rendered.
    expect(windowRange(1000, 36, 20, 500, 34).start).toBe(0);
    // Past the lead the first row scrolls away but overscan keeps a buffer.
    expect(windowRange(1000, 36, 34 + 36 * 10, 500, 34).start).toBe(6);
  });

  it("renders everything when the item size is not yet measurable", () => {
    const range = windowRange(50, 0, 0, 0);
    expect(range).toEqual({ start: 0, end: 50, padTop: 0, padBottom: 0 });
  });

  it("handles an empty directory", () => {
    expect(windowRange(0, 36, 0, 500)).toEqual({
      start: 0,
      end: 0,
      padTop: 0,
      padBottom: 0,
    });
  });
});

describe("clampScrollTop", () => {
  it("keeps an in-range offset", () => {
    expect(clampScrollTop(500, 5000, 400)).toBe(500);
  });

  it("clamps a stale deep offset after the listing shrank", () => {
    // 5,000 rows of 36px -> 150 rows: the old offset is far past the new end.
    expect(clampScrollTop(180_000, 150 * 36, 400)).toBe(150 * 36 - 400);
  });

  it("never goes negative on an empty or short listing", () => {
    expect(clampScrollTop(300, 0, 400)).toBe(0);
    expect(clampScrollTop(-40, 10_000, 400)).toBe(0);
  });
});

describe("gridColumnCount", () => {
  it("mirrors the CSS auto-fill minmax formula", () => {
    expect(gridColumnCount(1440, 176, 12)).toBe(7);
    expect(gridColumnCount(390, 176, 12)).toBe(2);
  });

  it("never drops below one column", () => {
    expect(gridColumnCount(100, 176, 12)).toBe(1);
    expect(gridColumnCount(0, 176, 12)).toBe(1);
  });
});

describe("gridWindow", () => {
  it("always windows whole rows", () => {
    const columns = 7;
    const range = gridWindow(5000, columns, 144, 36 * 144, 600);
    expect(range.start % columns).toBe(0);
    expect(range.end % columns).toBe(0);
    expect(range.end - range.start).toBeLessThan(140);
  });

  it("keeps the row count honest (row-aligned spacers)", () => {
    const columns = 7;
    const rows = Math.ceil(5000 / columns);
    const rowHeight = 144;
    const range = gridWindow(5000, columns, rowHeight, 100_000, 600);
    const renderedRows = (range.end - range.start) / columns;
    expect(range.padTop / rowHeight + renderedRows + range.padBottom / rowHeight).toBe(
      rows,
    );
  });

  it("covers the whole grid when every row fits", () => {
    const range = gridWindow(10, 4, 144, 0, 1000);
    expect(range).toEqual({ start: 0, end: 10, padTop: 0, padBottom: 0 });
  });
});

describe("renderedKey", () => {
  it("never collides across a separator a filename could contain", () => {
    // A filename may hold any byte but "/", so a plain join would alias these.
    expect(renderedKey(["a\nb"])).not.toBe(renderedKey(["a", "b"]));
    expect(renderedKey(["a|b"])).not.toBe(renderedKey(["a", "b"]));
    expect(renderedKey(["a", "b"])).toBe(renderedKey(["a", "b"]));
    expect(renderedKey([])).toBe(renderedKey([]));
  });
});
