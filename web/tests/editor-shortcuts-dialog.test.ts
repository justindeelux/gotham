// Shortcut sheet rendering (JUS-228): the dialog is generated from the shared
// binding table, grouped Common/Cursor/Selection/Edit, and read-only.
/* global document:readonly */
import { mount } from "@vue/test-utils";
import { afterEach, beforeEach, describe, expect, it } from "vitest";

import EditorShortcutsDialog from "@/features/files/components/EditorShortcutsDialog.vue";
import {
  EDITOR_SHORTCUT_GROUPS,
  editorShortcuts,
  shortcutKeysFor,
} from "@/features/files/editor/shortcuts";
// Side-effect import: merges the lazy editor catalogs under `files.editor`.
import "@/features/files/locales/editorMessages";
import { i18n, resetLocaleState, syncComposerLocale } from "@/shared/i18n";

beforeEach(() => {
  resetLocaleState();
  syncComposerLocale("en");
});

afterEach(() => {
  document.body.innerHTML = "";
});

/** mountSheet mounts the dialog open with its teleported body inline. */
function mountSheet() {
  return mount(EditorShortcutsDialog, {
    props: { show: true },
    attachTo: document.body,
    global: { plugins: [i18n], stubs: { teleport: true } },
  });
}

describe("EditorShortcutsDialog", () => {
  it("renders every shared row in the four groups", () => {
    const wrapper = mountSheet();

    const groups = wrapper.findAll(".ed-shortcut-group");
    expect(groups).toHaveLength(EDITOR_SHORTCUT_GROUPS.length);
    expect(groups.map((group) => group.find("h3").text())).toEqual([
      "Common",
      "Cursor",
      "Selection",
      "Edit",
    ]);
    expect(wrapper.findAll(".ed-shortcut-row")).toHaveLength(editorShortcuts.length);

    const expectedKeys = editorShortcuts.reduce(
      (total, shortcut) => total + shortcutKeysFor(shortcut, "other").length,
      0,
    );
    expect(wrapper.findAll(".ed-shortcut-key")).toHaveLength(expectedKeys);

    // The sheet is read-only documentation: no form controls to submit.
    expect(wrapper.findAll("input, textarea, select")).toHaveLength(0);
    wrapper.unmount();
  });

  it("shows platform labels and the save/save-all region", () => {
    const wrapper = mountSheet();
    expect(wrapper.text()).toContain("Keyboard shortcuts");
    expect(wrapper.text()).toContain("Save file");
    expect(wrapper.text()).toContain("Save all files");
    expect(wrapper.text()).toContain("Ctrl+Shift+S");
    wrapper.unmount();
  });
});
