// History diff views (JUS-219 fix round 2): the merge panes must be built with
// the current editor settings and follow later changes through their own
// compartments, like the editable tabs. JUS-231 fix round: the shared
// find/replace and go-to-line shortcuts must reach the custom UI with focus in
// a read-only diff pane.
/* global document:readonly, Element:readonly, HTMLElement:readonly, KeyboardEvent:readonly */
import { afterEach, describe, expect, it, vi } from "vitest";

import { createMergeSession } from "@/features/files/editor/merge";
import { DEFAULT_EDITOR_SETTINGS } from "@/features/files/schemas/editorSettings";

/** makeHost appends a detached diff host to the document. */
function makeHost(): HTMLElement {
  const host = document.createElement("div");
  document.body.append(host);
  return host;
}

/** shortcuts are the custom UI callbacks a diff session wires into its panes. */
function shortcuts() {
  return { onFind: vi.fn(), onReplace: vi.fn(), onGoToLine: vi.fn() };
}

/** press dispatches one keydown at a pane's content element. */
function press(content: Element, key: string, code: string): void {
  content.dispatchEvent(
    new KeyboardEvent("keydown", {
      key,
      code,
      ctrlKey: true,
      bubbles: true,
      cancelable: true,
    }),
  );
}

describe("merge view settings", () => {
  afterEach(() => {
    document.body.innerHTML = "";
  });

  it("builds both panes with the current settings", async () => {
    const host = makeHost();
    const session = await createMergeSession({
      parent: host,
      base: "a\nb",
      current: "a\nc",
      language: "plain",
      settings: { ...DEFAULT_EDITOR_SETTINGS, lineNumbers: false },
      ...shortcuts(),
    });

    expect(host.querySelectorAll(".cm-editor")).toHaveLength(2);
    expect(host.querySelectorAll(".cm-lineNumbers")).toHaveLength(0);
    session.destroy();
  });

  it("reconfigures both panes when the settings change", async () => {
    const host = makeHost();
    const session = await createMergeSession({
      parent: host,
      base: "a\nb",
      current: "a\nc",
      language: "plain",
      settings: DEFAULT_EDITOR_SETTINGS,
      ...shortcuts(),
    });

    expect(host.querySelectorAll(".cm-lineNumbers")).toHaveLength(2);

    session.setSettings({
      ...DEFAULT_EDITOR_SETTINGS,
      lineNumbers: false,
      wrap: true,
    });

    expect(host.querySelectorAll(".cm-lineNumbers")).toHaveLength(0);
    expect(host.querySelectorAll(".cm-content.cm-lineWrapping")).toHaveLength(2);

    session.setSettings({ ...DEFAULT_EDITOR_SETTINGS, lineNumbers: true });
    expect(host.querySelectorAll(".cm-lineNumbers")).toHaveLength(2);
    session.destroy();
  });

  it("routes Mod-F, Mod-H and Mod-L from a read-only pane to the custom UI", async () => {
    const host = makeHost();
    const callbacks = shortcuts();
    const session = await createMergeSession({
      parent: host,
      base: "a\nb",
      current: "a\nc",
      language: "plain",
      settings: DEFAULT_EDITOR_SETTINGS,
      ...callbacks,
    });
    const panes = host.querySelectorAll(".cm-content");
    expect(panes).toHaveLength(2);

    press(panes[1]!, "f", "KeyF");
    press(panes[1]!, "h", "KeyH");
    press(panes[1]!, "l", "KeyL");

    expect(callbacks.onFind).toHaveBeenCalledTimes(1);
    expect(callbacks.onReplace).toHaveBeenCalledTimes(1);
    expect(callbacks.onGoToLine).toHaveBeenCalledTimes(1);
    session.destroy();
  });
});
