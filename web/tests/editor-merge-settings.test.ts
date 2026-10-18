// History diff views (JUS-219 fix round 2): the merge panes must be built with
// the current editor settings and follow later changes through their own
// compartments, like the editable tabs.
/* global document:readonly, HTMLElement:readonly */
import { afterEach, describe, expect, it } from "vitest";

import { createMergeSession } from "@/features/files/editor/merge";
import { DEFAULT_EDITOR_SETTINGS } from "@/features/files/schemas/editorSettings";

/** makeHost appends a detached diff host to the document. */
function makeHost(): HTMLElement {
  const host = document.createElement("div");
  document.body.append(host);
  return host;
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
});
