// Files editor window geometry (JUS-129): the drag clamp must keep the whole
// floating window inside the viewport, so a drag can never strand the title
// bar (and its controls) off-screen. Pure helper, no DOM.
import { describe, expect, it } from "vitest";

import { clampEditorOffset } from "@/features/files/utils/editor";

describe("clampEditorOffset", () => {
  it("leaves an in-viewport position untouched", () => {
    expect(clampEditorOffset(100, 80, 400, 300, 1440, 900)).toEqual({ x: 100, y: 80 });
  });

  it("clamps a drag past the top/left edge to the origin", () => {
    expect(clampEditorOffset(-50, -20, 400, 300, 1440, 900)).toEqual({ x: 0, y: 0 });
  });

  it("clamps a drag past the bottom/right edge to the viewport", () => {
    expect(clampEditorOffset(2000, 2000, 400, 300, 1440, 900)).toEqual({
      x: 1440 - 400,
      y: 900 - 300,
    });
  });

  it("pins to the top-left when the window is larger than the viewport", () => {
    expect(clampEditorOffset(120, 90, 900, 700, 480, 640)).toEqual({ x: 0, y: 0 });
  });

  it("keeps the window flush right/bottom after a viewport shrink", () => {
    // A position valid at 1440x900 must be pulled back into a 800x600 viewport.
    expect(clampEditorOffset(1000, 560, 1024, 720, 1440, 900)).toEqual({ x: 416, y: 180 });
    expect(clampEditorOffset(1000, 560, 1024, 720, 800, 600)).toEqual({ x: 0, y: 0 });
  });
});
