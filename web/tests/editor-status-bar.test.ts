// Editor status bar (JUS-221): the footer reflects the active tab, the
// line-ending and tab-size pickers emit their choice, and read-only/loading
// tabs hide the edit affordances while keeping an accessible name on each
// clickable item.
import { mount } from "@vue/test-utils";
import { NPopselect } from "naive-ui";
import { describe, expect, it } from "vitest";

import EditorStatusBar from "@/features/files/components/EditorStatusBar.vue";
// Side-effect import: merges the lazy editor catalogs under `files.editor`.
import "@/features/files/locales/editorMessages";
import type { EditorTab } from "@/features/files/stores/editor";
import { i18n, resetLocaleState, syncComposerLocale } from "@/shared/i18n";

resetLocaleState();
syncComposerLocale("en");

/** tab builds one ready file tab, overridable per case. */
function tab(overrides: Partial<EditorTab> = {}): EditorTab {
  return {
    id: "tab-1",
    kind: "file",
    root: "www",
    path: "app/index.php",
    name: "index.php",
    doc: "<?php\n",
    original: "<?php\n",
    etag: '"v1"',
    eol: "\n",
    originalEol: "\n",
    bom: false,
    encoding: "utf-8",
    size: 1536,
    mode: 0o100644,
    language: "php",
    languageManual: false,
    status: "ready",
    error: "",
    saving: false,
    ...overrides,
  };
}

/** mountBar mounts the footer with an English catalog and default props. */
function mountBar(
  overrides: Partial<EditorTab> = {},
  props: Partial<{
    canWrite: boolean;
    tabSize: number;
    dirty: boolean;
    historyCount: number;
  }> = {},
) {
  return mount(EditorStatusBar, {
    props: {
      tab: tab(overrides),
      cursor: { line: 3, column: 7 },
      historyCount: 2,
      canWrite: true,
      tabSize: 4,
      dirty: false,
      ...props,
    },
    global: { plugins: [i18n] },
  });
}

/** item finds a footer button by its accessible name. */
function item(wrapper: ReturnType<typeof mountBar>, label: string) {
  const button = wrapper
    .findAll("button")
    .find((candidate) => candidate.attributes("aria-label") === label);
  if (!button) {
    throw new Error(`status item not found: ${label}`);
  }
  return button;
}

/** byText finds a footer button by its visible text (its accessible name). */
function byText(wrapper: ReturnType<typeof mountBar>, text: string) {
  const button = wrapper.findAll("button").find((candidate) => candidate.text() === text);
  if (!button) {
    throw new Error(`status item not found: ${text}`);
  }
  return button;
}

describe("EditorStatusBar", () => {
  it("shows the full path, cursor, history, encoding and language", () => {
    const wrapper = mountBar();

    expect(wrapper.text()).toContain("www/app/index.php");
    expect(wrapper.text()).toContain("Ln 3, Col 7");
    expect(wrapper.text()).toContain("2 versions");
    expect(wrapper.text()).toContain("UTF-8");
    expect(wrapper.text()).toContain("PHP");
    expect(wrapper.text()).toContain("Tab 4");
    expect(wrapper.text()).toContain("1.50 KiB");
    // Every clickable item carries an accessible name; the text items take
    // theirs from their visible label, so the name and content agree.
    expect(byText(wrapper, "2 versions")).toBeTruthy();
    expect(item(wrapper, "Save encoding: UTF-8")).toBeTruthy();
    expect(item(wrapper, "Line ending: LF")).toBeTruthy();
    expect(byText(wrapper, "Tab 4")).toBeTruthy();
    expect(wrapper.find('button[aria-label="Language: PHP"]').exists()).toBe(true);
  });

  it("opens the history and encoding dialogs", async () => {
    const wrapper = mountBar();

    await byText(wrapper, "2 versions").trigger("click");
    await item(wrapper, "Save encoding: UTF-8").trigger("click");

    expect(wrapper.emitted("openHistory")).toEqual([[]]);
    expect(wrapper.emitted("openEncoding")).toEqual([[]]);
  });

  it("forwards the line-ending and tab-size choices", async () => {
    const wrapper = mountBar();
    const pickers = wrapper.findAllComponents(NPopselect);

    pickers[0].vm.$emit("update:value", "\r\n");
    pickers[1].vm.$emit("update:value", 2);

    expect(wrapper.emitted("setEol")).toEqual([["\r\n"]]);
    expect(wrapper.emitted("setTabSize")).toEqual([[2]]);
  });

  it("shows the dirty marker and the read-only badge on a writable tab", () => {
    const wrapper = mountBar({}, { dirty: true });

    expect(wrapper.text()).toContain("Unsaved changes");
    expect(wrapper.find(".ed-readonly").exists()).toBe(false);
  });

  it("disables edits on a read-only root but keeps the values visible", () => {
    const wrapper = mountBar({}, { canWrite: false });

    expect(wrapper.find(".ed-readonly").text()).toBe("Read-only");
    expect(wrapper.text()).toContain("UTF-8");
    expect(wrapper.text()).toContain("LF");
    expect(
      wrapper.findAll("button").some(
        (button) => button.attributes("aria-label") === "Save encoding: UTF-8",
      ),
    ).toBe(false);
    // The line-ending switch is an edit, so only the view-only tab-size and
    // language pickers stay available.
    expect(wrapper.findAllComponents(NPopselect)).toHaveLength(2);
    expect(byText(wrapper, "Tab 4")).toBeTruthy();
  });

  it("hides the edit items while a tab is loading", () => {
    const wrapper = mountBar({ status: "loading" });

    expect(wrapper.text()).toContain("www/app/index.php");
    expect(wrapper.text()).not.toContain("Ln 3, Col 7");
    expect(wrapper.findAll("button")).toHaveLength(0);
    expect(wrapper.find(".ed-readonly").exists()).toBe(false);
  });

  it("marks a history diff read-only and hides the edit items", () => {
    const wrapper = mountBar({ kind: "diff", name: "History" });

    expect(wrapper.find(".ed-readonly").text()).toBe("Read-only");
    expect(wrapper.text()).toContain("PHP");
    expect(wrapper.text()).not.toContain("Ln 3, Col 7");
    expect(wrapper.findAllComponents(NPopselect)).toHaveLength(0);
  });

  it("marks a mixed-EOL tab read-only", () => {
    const wrapper = mountBar({ status: "mixed" });

    expect(wrapper.find(".ed-readonly").text()).toBe("Read-only");
    expect(wrapper.findAllComponents(NPopselect)).toHaveLength(0);
  });
});
