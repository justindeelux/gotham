// Image viewer zoom/pan math (JUS-204): the DOM-free helpers behind the
// viewer dialog. Wheel zoom stays centered on the cursor and clamped;
// a drag shifts the view; reset restores the fitted view. No DOM, no store.
import { describe, expect, it } from "vitest";

import {
  MAX_ZOOM,
  MIN_ZOOM,
  clampZoom,
  panBy,
  resetView,
  zoomAtPoint,
  zoomPercent,
} from "@/features/files/composables/useImageZoom";
import type { ZoomView } from "@/features/files/composables/useImageZoom";

import { isImageFile } from "@/features/files/utils/files";
import type { FilesEntry } from "@/features/files/schemas/files";

/** view builds a ZoomView with sensible defaults. */
function view(overrides: Partial<ZoomView> = {}): ZoomView {
  return { scale: 1, x: 0, y: 0, ...overrides };
}

/** entry builds one listing row with sensible defaults. */
function entry(overrides: Partial<FilesEntry> & { name: string }): FilesEntry {
  return {
    kind: "file",
    size: 2048,
    mode: 0o100644,
    modified_at: "2026-10-12T09:02:00Z",
    etag: '"1"',
    ...overrides,
  };
}

describe("clampZoom", () => {
  it("pins the scale to the [MIN_ZOOM, MAX_ZOOM] range", () => {
    expect(clampZoom(0.01)).toBe(MIN_ZOOM);
    expect(clampZoom(100)).toBe(MAX_ZOOM);
    expect(clampZoom(2)).toBe(2);
  });

  it("falls back to 1 for a non-finite scale", () => {
    expect(clampZoom(Number.NaN)).toBe(1);
  });
});

describe("zoomAtPoint", () => {
  it("keeps the image point under the cursor fixed", () => {
    // A cursor 100px right of center over a view panned 20px: zooming 2x
    // moves the offset so the same image pixel stays under the cursor.
    const next = zoomAtPoint(view({ x: 20 }), 100, 0, 2);
    expect(next.scale).toBe(2);
    expect(next.x).toBe(100 - (100 - 20) * 2);
    expect(next.y).toBe(0);
  });

  it("clamps at the bounds without moving the offset", () => {
    const top = zoomAtPoint(view({ scale: MAX_ZOOM, x: 5 }), 100, 50, 2);
    expect(top).toEqual({ scale: MAX_ZOOM, x: 5, y: 0 });
    const bottom = zoomAtPoint(view({ scale: MIN_ZOOM, x: 5 }), 100, 50, 0.5);
    expect(bottom).toEqual({ scale: MIN_ZOOM, x: 5, y: 0 });
  });

  it("ignores a non-positive or non-finite factor", () => {
    const current = view({ x: 7 });
    expect(zoomAtPoint(current, 10, 10, 0)).toEqual(current);
    expect(zoomAtPoint(current, 10, 10, Number.NaN)).toEqual(current);
  });
});

describe("panBy and resetView", () => {
  it("shifts the view by the drag delta, keeping the scale", () => {
    expect(panBy(view({ scale: 2, x: 10 }), 5, -8)).toEqual({
      scale: 2,
      x: 15,
      y: -8,
    });
  });

  it("resets to the fitted view", () => {
    expect(resetView()).toEqual({ scale: 1, x: 0, y: 0 });
  });

  it("renders a scale as a percent label", () => {
    expect(zoomPercent(1)).toBe("100%");
    expect(zoomPercent(2.5)).toBe("250%");
  });
});

describe("isImageFile", () => {
  it("accepts the viewer formats, case-insensitively", () => {
    for (const name of [
      "photo.png",
      "photo.jpg",
      "photo.jpeg",
      "anim.gif",
      "shot.webp",
      "icon.svg",
      "scan.bmp",
      "frame.avif",
      "LOGO.PNG",
    ]) {
      expect(isImageFile(entry({ name }))).toBe(true);
    }
  });

  it("rejects non-images and non-files", () => {
    expect(isImageFile(entry({ name: "notes.txt" }))).toBe(false);
    expect(isImageFile(entry({ name: "clip.mp4" }))).toBe(false);
    expect(isImageFile(entry({ name: "backup.zip" }))).toBe(false);
    expect(
      isImageFile(entry({ name: "gallery", kind: "directory", mode: 0o040755 })),
    ).toBe(false);
  });
});
