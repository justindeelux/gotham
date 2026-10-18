// Files editor tab management (JUS-220): Save All with per-tab failures, the
// tab context menu, the dirty-close prompt, middle-click close, Alt+W and
// Ctrl+Tab. The CodeMirror chunk and the file API are mocked so the component
// runs without a real editor or network.
/* global document:readonly, KeyboardEvent:readonly, MouseEvent:readonly, window:readonly */
import { flushPromises, mount } from "@vue/test-utils";
import { NMessageProvider, NDropdown } from "naive-ui";
import { createPinia, setActivePinia } from "pinia";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { defineComponent, h } from "vue";

const codemirror = vi.hoisted(() => ({
  sessions: [] as Array<Record<string, unknown>>,
}));

vi.mock("@/features/files/composables/useCodeMirror", () => ({
  createEditorSession: vi.fn(async () => {
    const session = {
      getDoc: () => "",
      setDoc: vi.fn(),
      setLanguage: vi.fn(),
      setWrap: vi.fn(),
      setReadOnly: vi.fn(),
      focus: vi.fn(),
      requestMeasure: vi.fn(),
      openSearch: vi.fn(),
      goToLine: vi.fn(),
      destroy: vi.fn(),
    };
    codemirror.sessions.push(session);
    return session;
  }),
}));

vi.mock("@/features/files/composables/useFileDownload", () => ({
  useFileDownload: () => ({ pending: { value: false }, download: vi.fn() }),
}));

const api = vi.hoisted(() => ({ putContent: vi.fn() }));

vi.mock("@/features/files/api/files", async (importOriginal) => {
  const actual = await importOriginal<Record<string, unknown>>();
  return { ...actual, putContent: api.putContent };
});

import FilesEditor from "@/features/files/components/FilesEditor.vue";
import { useEditorStore } from "@/features/files/stores/editor";
import type { EditorTab } from "@/features/files/stores/editor";
import { useFilesStore } from "@/features/files/stores/files";
import { scrollTabIntoView, tabsToClose } from "@/features/files/utils/editor";
import { useTeamsStore } from "@/features/teams";
import type { Team } from "@/features/teams/api/teams";
import { i18n, resetLocaleState, syncComposerLocale } from "@/shared/i18n";

const wrappers: Array<ReturnType<typeof mount>> = [];

/** makeTab builds one ready tab; `dirty` sets doc !== original. */
function makeTab(id: string, dirty: boolean): EditorTab {
  return {
    id,
    kind: "file",
    root: "volumes",
    path: `${id}.yaml`,
    name: `${id}.yaml`,
    doc: dirty ? "changed" : "start",
    original: "start",
    etag: '"v1"',
    eol: "\n",
    bom: false,
    size: 6,
    mode: 0o100644,
    language: "yaml",
    languageManual: false,
    status: "ready",
    error: "",
    saving: false,
  };
}

function team(id: string, role: Team["role"]): Team {
  return {
    id,
    name: id,
    is_personal: false,
    role,
    created_at: "2026-10-01T00:00:00Z",
    updated_at: "2026-10-01T00:00:00Z",
  };
}

/** mountEditor mounts FilesEditor under the message provider with one team. */
function mountEditor() {
  setActivePinia(createPinia());
  const teams = useTeamsStore();
  teams.teams = [team("team-a", "owner")];
  teams.activeTeamId = "team-a";

  const files = useFilesStore();
  files.roots = [{ name: "volumes", path: "/v", writable: true, free_bytes: 1 }];
  files.rootName = "volumes";

  const editor = useEditorStore();
  const shell = defineComponent({
    render: () =>
      h(NMessageProvider, null, {
        default: () => h(FilesEditor, { serverId: "srv" }),
      }),
  });
  const wrapper = mount(shell, {
    attachTo: document.body,
    global: { plugins: [i18n], stubs: { teleport: true } },
  });
  wrappers.push(wrapper);
  return { wrapper, editor, files, teams };
}

/** buttonByText finds a rendered button by its trimmed label. */
function buttonByText(wrapper: ReturnType<typeof mount>, text: string) {
  return wrapper.findAll("button").find((button) => button.text().trim() === text);
}

/** closePromptButton finds a button inside the dirty-close prompt. */
function closePromptButton(wrapper: ReturnType<typeof mount>, text: string) {
  const modal = wrapper.find(".files-editor-close-modal");
  return modal.findAll("button").find((button) => button.text().trim() === text);
}

beforeEach(() => {
  resetLocaleState();
  syncComposerLocale("en");
  codemirror.sessions.length = 0;
  api.putContent.mockReset();
  api.putContent.mockImplementation(async () => ({
    etag: '"v2"',
    size: 7,
    mode: 0o100644,
  }));
});

afterEach(() => {
  for (const wrapper of wrappers.splice(0)) {
    wrapper.unmount();
  }
  document.body.innerHTML = "";
  vi.restoreAllMocks();
});

describe("editor Save All", () => {
  it("saves every dirty tab and keeps only the failed one dirty", async () => {
    const { wrapper, editor } = mountEditor();
    editor.tabs = [makeTab("t1", true), makeTab("t2", true), makeTab("t3", false)];
    editor.activeId = "t1";
    await flushPromises();

    api.putContent.mockImplementation(async (_server, _team, body: { path: string }) => {
      if (body.path === "t2.yaml") {
        throw new Error("disk full");
      }
      return { etag: '"v2"', size: 7, mode: 0o100644 };
    });

    window.dispatchEvent(
      new KeyboardEvent("keydown", {
        key: "S",
        code: "KeyS",
        ctrlKey: true,
        shiftKey: true,
        bubbles: true,
      }),
    );
    await flushPromises();

    // Both dirty tabs were written; the clean tab was skipped.
    expect(api.putContent).toHaveBeenCalledTimes(2);
    const t1 = editor.tabs.find((tab) => tab.id === "t1")!;
    const t2 = editor.tabs.find((tab) => tab.id === "t2")!;
    expect(editor.isDirty(t1)).toBe(false);
    expect(editor.isDirty(t2)).toBe(true);
    // The failure is reported for its own tab, by name.
    expect(document.body.textContent).toContain("Could not save t2.yaml");
    // No blanket "all saved" toast on a partial run.
    expect(document.body.textContent).not.toContain("All changes saved");
    wrapper.unmount();
  });

  it("reports success once when every dirty tab saves", async () => {
    const { wrapper, editor } = mountEditor();
    editor.tabs = [makeTab("t1", true), makeTab("t2", true)];
    editor.activeId = "t1";
    await flushPromises();

    await wrapper.find(".ed-toolbar").findAll("button")
      .find((button) => button.text().includes("Save all"))!
      .trigger("click");
    await flushPromises();

    expect(api.putContent).toHaveBeenCalledTimes(2);
    expect(editor.hasDirty).toBe(false);
    expect(document.body.textContent).toContain("All changes saved");
    wrapper.unmount();
  });

  it("does not fire writes when the root is read-only", async () => {
    const { wrapper, editor, files } = mountEditor();
    editor.tabs = [makeTab("t1", true)];
    editor.activeId = "t1";
    files.roots = [{ name: "volumes", path: "/v", writable: false, free_bytes: 1 }];
    await flushPromises();

    window.dispatchEvent(
      new KeyboardEvent("keydown", {
        key: "S",
        code: "KeyS",
        metaKey: true,
        shiftKey: true,
        bubbles: true,
      }),
    );
    await flushPromises();

    expect(api.putContent).not.toHaveBeenCalled();
    expect(editor.hasDirty).toBe(true);
    wrapper.unmount();
  });

  it("queues every conflict from Save All and names each in its toast", async () => {
    const { wrapper, editor } = mountEditor();
    editor.tabs = [makeTab("t1", true), makeTab("t2", true)];
    editor.activeId = "t1";
    await flushPromises();

    api.putContent.mockRejectedValue({ message: "conflict", status: 409 });
    window.dispatchEvent(
      new KeyboardEvent("keydown", {
        key: "S",
        code: "KeyS",
        ctrlKey: true,
        shiftKey: true,
        bubbles: true,
      }),
    );
    await flushPromises();

    // Both conflicts are reported by name; the dialog walks them in turn.
    expect(document.body.textContent).toContain("t1.yaml changed on the server");
    expect(document.body.textContent).toContain("t2.yaml changed on the server");
    expect(wrapper.text()).toContain("t1.yaml changed since you opened it");
    expect(editor.conflictId).toBe("t1");

    editor.dismissConflict();
    await flushPromises();
    expect(wrapper.text()).toContain("t2.yaml changed since you opened it");
    expect(editor.conflictId).toBe("t2");

    editor.dismissConflict();
    await flushPromises();
    expect(editor.conflictId).toBeNull();
    wrapper.unmount();
  });
});

describe("editor tab context menu", () => {
  it("offers close scopes plus copy path, disabling the empty ones", async () => {
    const { wrapper, editor } = mountEditor();
    editor.tabs = [makeTab("t1", false), makeTab("t2", false), makeTab("t3", false)];
    editor.activeId = "t3";
    await flushPromises();

    await wrapper.findAll(".ed-tab")[2]!.trigger("contextmenu", {
      clientX: 12,
      clientY: 8,
    });
    await flushPromises();

    const dropdown = wrapper.findComponent(NDropdown);
    const options = dropdown.props("options") as Array<{
      key: string;
      disabled?: boolean;
    }>;
    expect(options.map((option) => option.key)).toEqual([
      "close",
      "others",
      "right",
      "all",
      "divider",
      "copyPath",
    ]);
    // The last tab has nothing to its right.
    expect(options.find((option) => option.key === "right")?.disabled).toBe(true);
    expect(dropdown.props("show")).toBe(true);
    wrapper.unmount();
  });

  it("closes to the right and keeps the target tab active", async () => {
    const { wrapper, editor } = mountEditor();
    editor.tabs = [makeTab("t1", false), makeTab("t2", false), makeTab("t3", false)];
    editor.activeId = "t2";
    await flushPromises();

    await wrapper.findAll(".ed-tab")[0]!.trigger("contextmenu");
    await flushPromises();
    wrapper.findComponent(NDropdown).vm.$emit("select", "right");
    await flushPromises();

    expect(editor.tabs.map((tab) => tab.id)).toEqual(["t1"]);
    expect(editor.activeId).toBe("t1");
    wrapper.unmount();
  });

  it("closes others without dropping a clean set", async () => {
    const { wrapper, editor } = mountEditor();
    editor.tabs = [makeTab("t1", false), makeTab("t2", false), makeTab("t3", false)];
    editor.activeId = "t1";
    await flushPromises();

    await wrapper.findAll(".ed-tab")[1]!.trigger("contextmenu");
    await flushPromises();
    wrapper.findComponent(NDropdown).vm.$emit("select", "others");
    await flushPromises();

    expect(editor.tabs.map((tab) => tab.id)).toEqual(["t2"]);
    expect(editor.activeId).toBe("t2");
    wrapper.unmount();
  });

  it("prompts before close-all discards a dirty buffer", async () => {
    const { wrapper, editor } = mountEditor();
    editor.tabs = [makeTab("t1", true), makeTab("t2", false)];
    editor.activeId = "t1";
    await flushPromises();

    await wrapper.findAll(".ed-tab")[0]!.trigger("contextmenu");
    await flushPromises();
    wrapper.findComponent(NDropdown).vm.$emit("select", "all");
    await flushPromises();

    expect(wrapper.text()).toContain("Close unsaved files?");
    expect(wrapper.find(".files-editor-close-modal").exists()).toBe(true);
    expect(wrapper.findAll(".ed-unsaved li").map((item) => item.text())).toEqual([
      "t1.yaml",
    ]);
    // Nothing closes until the user chooses.
    expect(editor.tabs).toHaveLength(2);

    await buttonByText(wrapper, "Don't save")!.trigger("click");
    await flushPromises();
    expect(editor.tabs).toHaveLength(0);
    wrapper.unmount();
  });
});

describe("editor dirty close prompt", () => {
  it("keeps the tab on cancel and closes it on discard", async () => {
    const { wrapper, editor } = mountEditor();
    editor.tabs = [makeTab("t1", true), makeTab("t2", false)];
    editor.activeId = "t1";
    await flushPromises();

    await wrapper.findAll(".ed-tab-close")[0]!.trigger("click");
    await flushPromises();
    expect(wrapper.text()).toContain("Close unsaved files?");
    expect(editor.tabs).toHaveLength(2);

    await buttonByText(wrapper, "Cancel")!.trigger("click");
    await flushPromises();
    expect(wrapper.text()).not.toContain("Close unsaved files?");
    expect(editor.tabs).toHaveLength(2);

    await wrapper.findAll(".ed-tab-close")[0]!.trigger("click");
    await flushPromises();
    await buttonByText(wrapper, "Don't save")!.trigger("click");
    await flushPromises();
    expect(editor.tabs.map((tab) => tab.id)).toEqual(["t2"]);
    expect(api.putContent).not.toHaveBeenCalled();
    wrapper.unmount();
  });

  it("saves then closes, and keeps a failed tab open", async () => {
    const { wrapper, editor } = mountEditor();
    editor.tabs = [makeTab("t1", true)];
    editor.activeId = "t1";
    await flushPromises();

    api.putContent.mockRejectedValue(new Error("read-only filesystem"));
    await wrapper.findAll(".ed-tab-close")[0]!.trigger("click");
    await flushPromises();
    await closePromptButton(wrapper, "Save")!.trigger("click");
    await flushPromises();

    // A failed save must not close (and discard) the buffer.
    expect(editor.tabs).toHaveLength(1);
    expect(editor.hasDirty).toBe(true);
    expect(wrapper.text()).not.toContain("Close unsaved files?");
    expect(document.body.textContent).toContain("Could not save t1.yaml");

    // The retry succeeds and closes the tab.
    api.putContent.mockImplementation(async () => ({
      etag: '"v2"',
      size: 7,
      mode: 0o100644,
    }));
    await wrapper.findAll(".ed-tab-close")[0]!.trigger("click");
    await flushPromises();
    await closePromptButton(wrapper, "Save")!.trigger("click");
    await flushPromises();
    expect(editor.tabs).toHaveLength(0);
    wrapper.unmount();
  });

  it("never replaces an open prompt with a later Alt+W or middle click", async () => {
    const { wrapper, editor } = mountEditor();
    editor.tabs = [makeTab("t1", true), makeTab("t2", true)];
    editor.activeId = "t2";
    await flushPromises();

    await wrapper.findAll(".ed-tab-close")[1]!.trigger("click");
    await flushPromises();
    expect(wrapper.findAll(".ed-unsaved li").map((item) => item.text())).toEqual([
      "t2.yaml",
    ]);

    // Alt+W and a middle click while the prompt is open are ignored: the
    // dialog keeps acting on the tab it was opened for.
    window.dispatchEvent(
      new KeyboardEvent("keydown", {
        key: "w",
        code: "KeyW",
        altKey: true,
        bubbles: true,
      }),
    );
    await flushPromises();
    await wrapper.findAll(".ed-tab")[0]!.trigger("auxclick", { button: 1 });
    await flushPromises();
    expect(wrapper.findAll(".ed-unsaved li").map((item) => item.text())).toEqual([
      "t2.yaml",
    ]);
    expect(editor.tabs).toHaveLength(2);

    await closePromptButton(wrapper, "Don't save")!.trigger("click");
    await flushPromises();
    expect(editor.tabs.map((tab) => tab.id)).toEqual(["t1"]);
    wrapper.unmount();
  });

  it("keeps edits typed while the close-save is in flight", async () => {
    const { wrapper, editor } = mountEditor();
    editor.tabs = [makeTab("t1", true)];
    editor.activeId = "t1";
    await flushPromises();

    let resolveWrite!: (_value: unknown) => void;
    api.putContent.mockImplementation(
      () => new Promise((resolve) => {
        resolveWrite = resolve;
      }),
    );

    await wrapper.findAll(".ed-tab-close")[0]!.trigger("click");
    await flushPromises();
    await closePromptButton(wrapper, "Save")!.trigger("click");
    await flushPromises();

    // The save was handed the old buffer; the new text must not be dropped by
    // the close that follows it.
    editor.setDoc("t1", "typed during save");
    resolveWrite({ etag: '"v2"', size: 7, mode: 0o100644 });
    await flushPromises();

    expect(editor.tabs).toHaveLength(1);
    expect(editor.isDirty(editor.tabs[0]!)).toBe(true);
    expect(wrapper.text()).not.toContain("Close unsaved files?");
    wrapper.unmount();
  });
});

describe("editor tab ergonomics", () => {
  it("closes a tab on middle click", async () => {
    const { wrapper, editor } = mountEditor();
    editor.tabs = [makeTab("t1", false), makeTab("t2", false)];
    editor.activeId = "t1";
    await flushPromises();

    await wrapper.findAll(".ed-tab")[1]!.trigger("auxclick", { button: 1 });
    await flushPromises();

    expect(editor.tabs.map((tab) => tab.id)).toEqual(["t1"]);
    wrapper.unmount();
  });

  it("cancels the browser autoscroll on a middle mousedown", async () => {
    const { wrapper, editor } = mountEditor();
    editor.tabs = [makeTab("t1", false), makeTab("t2", false)];
    editor.activeId = "t1";
    await flushPromises();

    const tab = wrapper.findAll(".ed-tab")[1]!.element;
    const middle = new MouseEvent("mousedown", {
      button: 1,
      bubbles: true,
      cancelable: true,
    });
    tab.dispatchEvent(middle);
    expect(middle.defaultPrevented).toBe(true);

    const left = new MouseEvent("mousedown", {
      button: 0,
      bubbles: true,
      cancelable: true,
    });
    tab.dispatchEvent(left);
    expect(left.defaultPrevented).toBe(false);
    // A middle mousedown never closes by itself; auxclick does.
    expect(editor.tabs).toHaveLength(2);
    wrapper.unmount();
  });

  it("closes the active tab with Alt+W after prompting when dirty", async () => {
    const { wrapper, editor } = mountEditor();
    editor.tabs = [makeTab("t1", false), makeTab("t2", true)];
    editor.activeId = "t2";
    await flushPromises();

    window.dispatchEvent(
      new KeyboardEvent("keydown", {
        key: "w",
        code: "KeyW",
        altKey: true,
        bubbles: true,
      }),
    );
    await flushPromises();

    expect(wrapper.text()).toContain("Close unsaved files?");
    expect(editor.tabs).toHaveLength(2);
    await buttonByText(wrapper, "Don't save")!.trigger("click");
    await flushPromises();
    expect(editor.tabs.map((tab) => tab.id)).toEqual(["t1"]);
    expect(editor.activeId).toBe("t1");
    wrapper.unmount();
  });

  it("cycles tabs with Ctrl+Tab and Ctrl+Shift+Tab", async () => {
    const { wrapper, editor } = mountEditor();
    editor.tabs = [makeTab("t1", false), makeTab("t2", false), makeTab("t3", false)];
    editor.activeId = "t1";
    await flushPromises();

    window.dispatchEvent(
      new KeyboardEvent("keydown", { key: "Tab", ctrlKey: true, bubbles: true }),
    );
    await flushPromises();
    expect(editor.activeId).toBe("t2");

    window.dispatchEvent(
      new KeyboardEvent("keydown", {
        key: "Tab",
        ctrlKey: true,
        shiftKey: true,
        bubbles: true,
      }),
    );
    await flushPromises();
    expect(editor.activeId).toBe("t1");
    wrapper.unmount();
  });
});

describe("tab helpers", () => {
  const tabs = [{ id: "a" }, { id: "b" }, { id: "c" }];

  it("resolves every close scope around the target", () => {
    expect(tabsToClose(tabs, "self", "b")).toEqual(["b"]);
    expect(tabsToClose(tabs, "others", "b")).toEqual(["a", "c"]);
    expect(tabsToClose(tabs, "all", "b")).toEqual(["a", "b", "c"]);
    expect(tabsToClose(tabs, "right", "a")).toEqual(["b", "c"]);
    expect(tabsToClose(tabs, "right", "c")).toEqual([]);
    expect(tabsToClose(tabs, "self", "missing")).toEqual([]);
  });

  it("scrolls an off-screen tab into view and leaves a visible one alone", () => {
    const strip = { left: 100, right: 300, scrollLeft: 0 };
    // Already inside the padded viewport.
    expect(scrollTabIntoView(strip, { left: 150, right: 220 })).toBe(0);
    // Past the right edge: scroll just enough to clear the padding.
    expect(scrollTabIntoView(strip, { left: 280, right: 340 })).toBe(48);
    // Before the left edge: scroll back towards zero.
    expect(
      scrollTabIntoView({ left: 100, right: 300, scrollLeft: 80 }, { left: 90, right: 140 }),
    ).toBe(62);
    // Never negative.
    expect(
      scrollTabIntoView({ left: 100, right: 300, scrollLeft: 4 }, { left: 90, right: 140 }),
    ).toBe(0);
  });
});
