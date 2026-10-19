// JUS-241: the floating editor window resizes from all four edges and all
// four corners. The anchoring rules live in a pure helper so they can be
// checked without a browser: the edge opposite the grip stays put, and the
// size never leaves the viewport band.
import { describe, expect, it } from "vitest";

import {
  EDITOR_RESIZE_DIRECTIONS,
  editorResizeLimits,
  resizeEditorBox,
} from "@/features/files/utils/editorResize";
import type {
  EditorBox,
  EditorResizeDirection,
} from "@/features/files/utils/editorResize";

/** origin is the window box a resize starts from. */
const origin: EditorBox = { x: 100, y: 120, width: 800, height: 600 };

/** limits is the size band of a 1280x800 viewport. */
const limits = editorResizeLimits(1280, 800);

describe("editorResizeLimits", () => {
  it("keeps the window inside the viewport margins", () => {
    expect(editorResizeLimits(1280, 800)).toEqual({
      minWidth: 480,
      minHeight: 320,
      maxWidth: 1232,
      maxHeight: 696,
      viewportWidth: 1280,
      viewportHeight: 800,
    });
    expect(editorResizeLimits(1366, 800)).toEqual({
      minWidth: 480,
      minHeight: 320,
      maxWidth: 1318,
      maxHeight: 696,
      viewportWidth: 1366,
      viewportHeight: 800,
    });
  });

  it("yields the floor to a viewport narrower than the minimum", () => {
    expect(editorResizeLimits(390, 844)).toEqual({
      minWidth: 342,
      minHeight: 320,
      maxWidth: 342,
      maxHeight: 740,
      viewportWidth: 390,
      viewportHeight: 844,
    });
  });
});

describe("resizeEditorBox", () => {
  it("covers the four edges and the four corners", () => {
    expect(EDITOR_RESIZE_DIRECTIONS).toHaveLength(8);
    for (const direction of ["n", "ne", "e", "se", "s", "sw", "w", "nw"]) {
      expect(EDITOR_RESIZE_DIRECTIONS).toContain(direction);
    }
  });

  it("moves only the edges the grip owns", () => {
    const cases: Array<{
      direction: EditorResizeDirection;
      deltaX: number;
      deltaY: number;
      expected: EditorBox;
    }> = [
      // East: the right edge follows the pointer, the left one stays.
      {
        direction: "e",
        deltaX: 50,
        deltaY: 50,
        expected: { x: 100, y: 120, width: 850, height: 600 },
      },
      // West: the left edge follows, the right one stays.
      {
        direction: "w",
        deltaX: 50,
        deltaY: 50,
        expected: { x: 150, y: 120, width: 750, height: 600 },
      },
      // South: the bottom edge follows, the top one stays.
      {
        direction: "s",
        deltaX: 50,
        deltaY: 50,
        expected: { x: 100, y: 120, width: 800, height: 650 },
      },
      // North: the top edge follows, the bottom one stays.
      {
        direction: "n",
        deltaX: 50,
        deltaY: 50,
        expected: { x: 100, y: 170, width: 800, height: 550 },
      },
      // The corners move both of their own edges.
      {
        direction: "ne",
        deltaX: 50,
        deltaY: 50,
        expected: { x: 100, y: 170, width: 850, height: 550 },
      },
      {
        direction: "nw",
        deltaX: 50,
        deltaY: 50,
        expected: { x: 150, y: 170, width: 750, height: 550 },
      },
      {
        direction: "se",
        deltaX: 50,
        deltaY: 50,
        expected: { x: 100, y: 120, width: 850, height: 650 },
      },
      {
        direction: "sw",
        deltaX: 50,
        deltaY: 50,
        expected: { x: 150, y: 120, width: 750, height: 650 },
      },
    ];
    for (const testCase of cases) {
      expect(
        resizeEditorBox(
          testCase.direction,
          origin,
          testCase.deltaX,
          testCase.deltaY,
          limits,
        ),
      ).toEqual(testCase.expected);
    }
  });

  it("keeps the anchored edge in place for every grip", () => {
    const anchors: Record<
      EditorResizeDirection,
      { left: boolean; right: boolean; top: boolean; bottom: boolean }
    > = {
      e: { left: true, right: false, top: true, bottom: true },
      w: { left: false, right: true, top: true, bottom: true },
      s: { left: true, right: true, top: true, bottom: false },
      n: { left: true, right: true, top: false, bottom: true },
      ne: { left: true, right: false, top: false, bottom: true },
      nw: { left: false, right: true, top: false, bottom: true },
      se: { left: true, right: false, top: true, bottom: false },
      sw: { left: false, right: true, top: true, bottom: false },
    };
    for (const direction of EDITOR_RESIZE_DIRECTIONS) {
      const next = resizeEditorBox(direction, origin, 40, 30, limits);
      const anchor = anchors[direction];
      expect(next.x === origin.x).toBe(anchor.left);
      expect(next.x + next.width === origin.x + origin.width).toBe(anchor.right);
      expect(next.y === origin.y).toBe(anchor.top);
      expect(next.y + next.height === origin.y + origin.height).toBe(anchor.bottom);
    }
  });

  it("never shrinks below the minimum size", () => {
    const next = resizeEditorBox("w", origin, 5000, 0, limits);
    expect(next.width).toBe(480);
    // The right edge stays anchored even though the drag overshot.
    expect(next.x + next.width).toBe(origin.x + origin.width);
    const short = resizeEditorBox("n", origin, 0, 5000, limits);
    expect(short.height).toBe(320);
    expect(short.y + short.height).toBe(origin.y + origin.height);
  });

  it("never grows past the viewport", () => {
    // The window is 100px from the left edge, so it can only grow to the
    // right edge of the screen: 1280 - 100.
    const next = resizeEditorBox("e", origin, 5000, 0, limits);
    expect(next.width).toBe(1180);
    expect(next.x + next.width).toBe(1280);
    const tall = resizeEditorBox("s", origin, 0, 5000, limits);
    expect(tall.height).toBe(680);
    expect(tall.y + tall.height).toBe(800);
  });

  it("keeps the window on screen when it is dragged past the edge", () => {
    const moved: EditorBox = { x: 1000, y: 500, width: 400, height: 300 };
    const next = resizeEditorBox("e", moved, 5000, 0, limits);
    expect(next.x + next.width).toBeLessThanOrEqual(1280);
    const tall = resizeEditorBox("s", moved, 0, 5000, limits);
    expect(tall.y + tall.height).toBeLessThanOrEqual(800);
  });
});
