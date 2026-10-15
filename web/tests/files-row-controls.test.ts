// Row controls (JUS-162): a press or key inside a row's own control must not
// drive the row's selection/activation. The folder-size cell (.fm-size) was
// missing from the selector, so Enter/Space on Calculate/Cancel/Retry bubbled
// to the list keydown handler (preventDefault + open/select). This pins the
// selector so that cannot regress.
/* global document, Element: readonly */
import { describe, expect, it } from "vitest";

import {
  ROW_CONTROL_SELECTOR,
  isRowControlTarget,
} from "@/features/files/utils/rowControls";

/** el builds one detached element tree from html and returns its first child. */
function el(html: string): Element {
  const root = document.createElement("div");
  root.innerHTML = html;
  return root.firstElementChild as Element;
}

describe("isRowControlTarget", () => {
  it("treats the folder-size cell as a row control", () => {
    const cell = el(
      '<div class="fm-cell fm-cell--size"><span class="fm-size"><button>Calculate</button></span></div>',
    );
    expect(isRowControlTarget(cell.querySelector("button"))).toBe(true);
    expect(isRowControlTarget(cell.querySelector(".fm-size"))).toBe(true);
  });

  it("still matches the other row controls", () => {
    for (const cls of [
      "fm-select",
      "fm-actions",
      "fm-tile-check",
      "fm-tile-actions",
    ]) {
      const node = el(`<span class="${cls}"><button>x</button></span>`);
      expect(isRowControlTarget(node.querySelector("button"))).toBe(true);
    }
  });

  it("does not match a plain row target", () => {
    const name = el('<div class="fm-name"><button class="fm-label">a</button></div>');
    expect(isRowControlTarget(name.querySelector(".fm-label"))).toBe(false);
    expect(isRowControlTarget(name)).toBe(false);
    expect(isRowControlTarget(null)).toBe(false);
  });

  it("keeps .fm-size in the shared selector", () => {
    expect(ROW_CONTROL_SELECTOR).toContain(".fm-size");
  });
});
