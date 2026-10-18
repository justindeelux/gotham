// Row controls (JUS-162): a press or key inside a row's own control must not
// drive the row's selection/activation. The folder-size cell (.fm-size) was
// missing from the selector, so Enter/Space on Calculate/Cancel/Retry bubbled
// to the list keydown handler (preventDefault + open/select). This pins the
// selector so that cannot regress.
//
// JUS-217: the selector alone is not enough. A control that replaces itself on
// activation (Calculate swaps its button for a spinner) detaches the event
// target before the bubbled press reaches the row, so a target.closest() guard
// misfires and the row selects anyway. The shared guard therefore reads the
// event's dispatch-time path (composedPath), which survives the detach. These
// tests pin that path-aware contract for click and Space/Enter on
// Calculate/Cancel/Retry.
/* global document, Element, Event, HTMLElement, MouseEvent, KeyboardEvent, window: readonly */
import { describe, expect, it } from "vitest";

import {
  ROW_CONTROL_SELECTOR,
  isRowControlEvent,
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

/** detachedEvent replays the JUS-217 race: the target unmounted mid-dispatch. */
function detachedEvent(
  row: Element,
  targetHtml: string,
  kind: "click" | "keydown",
  key?: string,
): Event {
  const rowEl = row as HTMLElement;
  const cell = rowEl.querySelector(".fm-size") as HTMLElement;
  cell.insertAdjacentHTML("beforeend", targetHtml);
  const target = cell.lastElementChild as Element;
  // The control activates and replaces itself before the press bubbles out,
  // exactly like Calculate swapping its button for the loading spinner.
  const path = [target, ...ancestors(target, rowEl)];
  target.remove();
  const event =
    kind === "click"
      ? new MouseEvent("click", { bubbles: true })
      : new KeyboardEvent("keydown", { bubbles: true, key });
  // jsdom freezes the dispatch-time path on composedPath, so stub it with the
  // ancestors captured while the target was still mounted.
  Object.defineProperty(event, "composedPath", {
    value: () => [...path, document, window],
  });
  Object.defineProperty(event, "target", { value: target });
  return event;
}

/** ancestors lists the element's ancestors up to (and including) stop. */
function ancestors(target: Element, stop: Element): Element[] {
  const chain: Element[] = [];
  let node = target.parentElement;
  while (node !== null) {
    chain.push(node);
    if (node === stop) {
      break;
    }
    node = node.parentElement;
  }
  return chain;
}

/** sizeRow builds a folder row whose .fm-size cell holds a live region. */
function sizeRow(): Element {
  const root = document.createElement("div");
  root.innerHTML =
    '<div class="fm-row" data-entry="apps">' +
    '<div class="fm-cell fm-cell--size"><span class="fm-size">' +
    '<span class="fm-sr-only" role="status"></span>' +
    "</span></div></div>";
  return root.firstElementChild as Element;
}

describe("isRowControlEvent", () => {
  it("keeps a click on the detached Calculate button off the row", () => {
    // Icon-only Calculate: the press lands on the lucide svg inside the button.
    const event = detachedEvent(
      sizeRow(),
      '<button class="fm-size-action fm-size-action--icon"><svg></svg></button>',
      "click",
    );
    expect(isRowControlEvent(event)).toBe(true);
  });

  it("keeps a click on the detached Cancel text control off the row", () => {
    const event = detachedEvent(
      sizeRow(),
      '<button class="fm-size-action">Cancel</button>',
      "click",
    );
    expect(isRowControlEvent(event)).toBe(true);
  });

  it("keeps a click on the detached Retry text control off the row", () => {
    const event = detachedEvent(
      sizeRow(),
      '<button class="fm-size-action fm-size-action--error">Failed</button>',
      "click",
    );
    expect(isRowControlEvent(event)).toBe(true);
  });

  it("keeps Space/Enter on a detached size control off the row", () => {
    const controls = [
      '<button class="fm-size-action fm-size-action--icon"><svg></svg></button>',
      '<button class="fm-size-action">Cancel</button>',
      '<button class="fm-size-action fm-size-action--error">Failed</button>',
    ];
    for (const control of controls) {
      for (const key of [" ", "Enter"]) {
        const event = detachedEvent(sizeRow(), control, "keydown", key);
        expect(isRowControlEvent(event)).toBe(true);
      }
    }
  });

  it("still selects a press whose path never entered a row control", () => {
    const root = document.createElement("div");
    root.innerHTML =
      '<div class="fm-row" data-entry="apps">' +
      '<div class="fm-name"><button class="fm-label"><b>apps</b></button></div>' +
      "</div>";
    const row = root.firstElementChild as Element;
    const label = row.querySelector(".fm-label") as Element;
    const event = new MouseEvent("click", { bubbles: true });
    Object.defineProperty(event, "composedPath", {
      value: () => [label, ...ancestors(label, row), document, window],
    });
    Object.defineProperty(event, "target", { value: label });
    expect(isRowControlEvent(event)).toBe(false);
  });

  it("falls back to the live target when no dispatch path is available", () => {
    const cell = el(
      '<div class="fm-cell fm-cell--size"><span class="fm-size"><button>Calculate</button></span></div>',
    );
    const event = new MouseEvent("click", { bubbles: true });
    Object.defineProperty(event, "composedPath", { value: () => [] });
    Object.defineProperty(event, "target", {
      value: cell.querySelector("button"),
    });
    expect(isRowControlEvent(event)).toBe(true);
  });
});
