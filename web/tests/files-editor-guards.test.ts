/* global document:readonly, Element:readonly, Event:readonly, HTMLElement:readonly, MouseEvent:readonly, window:readonly */
import { flushPromises, mount } from "@vue/test-utils";
import { NDropdown, NMessageProvider } from "naive-ui";
import { createPinia, setActivePinia } from "pinia";
import { createMemoryHistory, createRouter } from "vue-router";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { defineComponent, h, nextTick } from "vue";

interface FakeSession {
  getDoc: () => string;
  setDoc: ReturnType<typeof vi.fn>;
  setLanguage: ReturnType<typeof vi.fn>;
  setSettings: ReturnType<typeof vi.fn>;
  setReadOnly: ReturnType<typeof vi.fn>;
  focus: ReturnType<typeof vi.fn>;
  requestMeasure: ReturnType<typeof vi.fn>;
  search: {
    setOptions: ReturnType<typeof vi.fn>;
    status: ReturnType<typeof vi.fn>;
    next: ReturnType<typeof vi.fn>;
    previous: ReturnType<typeof vi.fn>;
    replace: ReturnType<typeof vi.fn>;
    replaceAll: ReturnType<typeof vi.fn>;
    clear: ReturnType<typeof vi.fn>;
  };
  lineInfo: ReturnType<typeof vi.fn>;
  gotoLine: ReturnType<typeof vi.fn>;
  destroy: ReturnType<typeof vi.fn>;
}

const codemirror = vi.hoisted(() => ({
  sessions: [] as FakeSession[],
}));

vi.mock("@/features/files/composables/useCodeMirror", () => ({
  createEditorSession: vi.fn(async () => {
    const status = { total: 0, current: 0, capped: false };
    const session: FakeSession = {
      getDoc: () => "",
      setDoc: vi.fn(),
      setLanguage: vi.fn(),
      setSettings: vi.fn(),
      setReadOnly: vi.fn(),
      focus: vi.fn(),
      requestMeasure: vi.fn(),
      search: {
        setOptions: vi.fn(() => status),
        status: vi.fn(() => status),
        next: vi.fn(() => status),
        previous: vi.fn(() => status),
        replace: vi.fn(() => status),
        replaceAll: vi.fn(() => status),
        clear: vi.fn(),
      },
      lineInfo: vi.fn(() => ({ line: 1, total: 10 })),
      gotoLine: vi.fn(() => null),
      destroy: vi.fn(),
    };
    codemirror.sessions.push(session);
    return session;
  }),
}));

vi.mock("@/features/files/composables/useFileDownload", () => ({
  useFileDownload: () => ({ pending: { value: false }, download: vi.fn() }),
}));

interface FakeMergeSession {
  setSettings: ReturnType<typeof vi.fn>;
  destroy: ReturnType<typeof vi.fn>;
  requestMeasure: ReturnType<typeof vi.fn>;
}

const mergeView = vi.hoisted(() => ({
  sessions: [] as FakeMergeSession[],
}));

vi.mock("@/features/files/composables/useMergeView", () => ({
  createMergeSession: vi.fn(async () => {
    const session: FakeMergeSession = {
      setSettings: vi.fn(),
      destroy: vi.fn(),
      requestMeasure: vi.fn(),
    };
    mergeView.sessions.push(session);
    return session;
  }),
}));

import FilesEditor from "@/features/files/components/FilesEditor.vue";
import EditorShortcutsDialog from "@/features/files/components/EditorShortcutsDialog.vue";
import { createEditorSession } from "@/features/files/composables/useCodeMirror";
import { createMergeSession } from "@/features/files/composables/useMergeView";
import { useEditorStore } from "@/features/files/stores/editor";
import type { EditorTab } from "@/features/files/stores/editor";
import { useEditorWindowStore } from "@/features/files/stores/editorWindow";
import { useEditorSettingsStore } from "@/features/files/stores/editorSettings";
import { useFilesStore } from "@/features/files/stores/files";
import { i18n, resetLocaleState, syncComposerLocale } from "@/shared/i18n";

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
    truncated: false,
    language: "yaml",
    languageManual: false,
    status: "ready",
    error: "",
    saving: false,
  };
}

function makeDiffTab(id: string): EditorTab {
  return {
    ...makeTab(id, false),
    kind: "diff",
    diff: { versionId: "v1", base: "a", current: "b" },
  };
}

function toolbarButton(
  wrapper: ReturnType<typeof mountEditor>["wrapper"],
  label: string,
) {
  const button = wrapper
    .find(".ed-toolbar")
    .findAll("button")
    .find((item) => item.text().includes(label));
  if (!button) {
    throw new Error(`toolbar button not found: ${label}`);
  }
  return button;
}

function lastSessionOptions(): Record<string, unknown> {
  const calls = vi.mocked(createEditorSession).mock.calls;
  const options = calls.at(-1)?.[0];
  if (!options) {
    throw new Error("createEditorSession was not called");
  }
  return options as Record<string, unknown>;
}

function mountEditor() {
  setActivePinia(createPinia());

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
  return { wrapper, files, editor };
}

async function mountEditorWithRouter(initialPath = "/servers/srv-a/files") {
  setActivePinia(createPinia());

  const files = useFilesStore();
  files.roots = [{ name: "volumes", path: "/v", writable: true, free_bytes: 1 }];
  files.rootName = "volumes";

  const editor = useEditorStore();
  const stub = { template: "<div />" };
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: "/dashboard", name: "dashboard", component: stub },
      { path: "/servers/:id/files", name: "server-files", component: stub },
    ],
  });
  await router.push(initialPath);
  await router.isReady();
  const shell = defineComponent({
    render: () =>
      h(NMessageProvider, null, {
        default: () => h(FilesEditor, { serverId: "srv" }),
      }),
  });
  const wrapper = mount(shell, {
    attachTo: document.body,
    global: { plugins: [router, i18n], stubs: { teleport: true } },
  });
  await flushPromises();
  return { wrapper, files, editor, router };
}

function setViewport(width: number, height: number): void {
  Object.defineProperty(window, "innerWidth", { configurable: true, value: width });
  Object.defineProperty(window, "innerHeight", { configurable: true, value: height });
}

function stubPointerCapture(): () => void {
  const holder = window.HTMLElement.prototype as unknown as {
    setPointerCapture?: (_id: number) => void;
  };
  const original = holder.setPointerCapture;
  holder.setPointerCapture = () => {};
  return () => {
    if (original === undefined) {
      delete holder.setPointerCapture;
    } else {
      holder.setPointerCapture = original;
    }
  };
}

function firePointerEvent(
  target: Element,
  type: string,
  init: {
    button?: number;
    clientX?: number;
    clientY?: number;
    pointerId?: number;
  },
): void {
  const event = new MouseEvent(type, {
    bubbles: true,
    cancelable: true,
    button: init.button ?? 0,
    clientX: init.clientX ?? 0,
    clientY: init.clientY ?? 0,
  });
  Object.defineProperty(event, "pointerId", { value: init.pointerId ?? 0 });
  target.dispatchEvent(event);
}

function leaveDialog(wrapper: ReturnType<typeof mountEditor>["wrapper"]) {
  return wrapper
    .findAll(".app-modal")
    .find((node) => node.text().includes("Discard unsaved changes?"));
}

beforeEach(() => {
  resetLocaleState();
  syncComposerLocale("en");
  codemirror.sessions.length = 0;
});

afterEach(() => {
  document.body.innerHTML = "";
  vi.restoreAllMocks();
});

describe("FilesEditor guards", () => {
  it("renders no error state and unique ids on a ready tab", async () => {
    const { wrapper, editor } = mountEditor();
    const tab = makeTab("t1", false);
    editor.tabs = [tab];
    editor.activeId = tab.id;
    await flushPromises();

    expect(wrapper.findAll(".ed-state")).toHaveLength(0);
    const panels = wrapper
      .findAll('[role="tabpanel"]')
      .map((node) => node.attributes("id"));
    expect(panels).toHaveLength(1);
    expect(new Set(panels).size).toBe(panels.length);
    wrapper.unmount();
  });

  it("reconfigures the live session grammar in place on a manual override", async () => {
    const { wrapper, editor } = mountEditor();
    const tab = makeTab("t1", false);
    editor.tabs = [tab];
    editor.activeId = tab.id;
    await flushPromises();
    expect(codemirror.sessions).toHaveLength(1);

    editor.setLanguage(tab.id, "php");
    await flushPromises();

    expect(codemirror.sessions).toHaveLength(1);
    expect(codemirror.sessions[0].setLanguage).toHaveBeenLastCalledWith("php");
    expect(wrapper.find(".ed-language").text()).toBe("PHP");
    wrapper.unmount();
  });

  it("keeps dirty buffers when unmounted without a resolved discard", async () => {
    const { wrapper, editor } = mountEditor();
    const tab = makeTab("t1", true);
    editor.tabs = [tab];
    editor.activeId = tab.id;
    await flushPromises();
    expect(editor.hasDirty).toBe(true);

    wrapper.unmount();
    expect(editor.tabs).toHaveLength(1);
    expect(editor.hasDirty).toBe(true);
  });

  it("resets clean buffers on unmount", async () => {
    const { wrapper, editor } = mountEditor();
    const tab = makeTab("t1", false);
    editor.tabs = [tab];
    editor.activeId = tab.id;
    await flushPromises();

    wrapper.unmount();
    expect(editor.tabs).toHaveLength(0);
  });

  it("minimizes to the rail icon and restores with the dirty buffer intact", async () => {
    const { wrapper, editor } = mountEditor();
    const tab = makeTab("t1", true);
    editor.tabs = [tab];
    editor.activeId = tab.id;
    await flushPromises();

    expect(wrapper.find(".ed-window").attributes("style") ?? "").not.toContain("display: none");
    expect(editor.hasDirty).toBe(true);

    await wrapper.find("button.ed-window-min").trigger("click");
    await flushPromises();

    expect(wrapper.find(".ed-window").attributes("style")).toContain("display: none");
    expect(useEditorWindowStore().minimized).toBe(true);
    expect(editor.tabs).toHaveLength(1);
    expect(editor.hasDirty).toBe(true);

    useEditorWindowStore().setMinimized(false);
    await flushPromises();
    expect(wrapper.find(".ed-window").attributes("style") ?? "").not.toContain("display: none");
    expect(useEditorWindowStore().minimized).toBe(false);
    expect(editor.hasDirty).toBe(true);
    wrapper.unmount();
  });

  it("does not collapse the window on Escape while a dialog is open", async () => {
    const { wrapper, editor } = mountEditor();
    const tab = makeTab("t1", false);
    editor.tabs = [tab];
    editor.activeId = tab.id;
    await flushPromises();

    await wrapper.find("button.ed-window-shortcuts").trigger("click");
    await flushPromises();
    expect(wrapper.find(".ed-shortcuts").exists()).toBe(true);

    await wrapper.find(".ed-window").trigger("keydown", { key: "Escape" });
    await flushPromises();
    expect(wrapper.find(".ed-window").attributes("style") ?? "").not.toContain(
      "display: none",
    );

    wrapper.findComponent(EditorShortcutsDialog).vm.$emit("update:show", false);
    await flushPromises();
    await wrapper.find(".ed-window").trigger("keydown", { key: "Escape" });
    await flushPromises();
    expect(wrapper.find(".ed-window").attributes("style")).toContain("display: none");
    wrapper.unmount();
  });

  it("does not collapse the window on Escape while a popover is open", async () => {
    const { wrapper, editor } = mountEditor();
    const tab = makeTab("t1", false);
    editor.tabs = [tab];
    editor.activeId = tab.id;
    await flushPromises();

    const popover = document.createElement("div");
    popover.className = "n-popover";
    document.body.append(popover);

    await wrapper.find(".ed-window").trigger("keydown", { key: "Escape" });
    await flushPromises();
    expect(wrapper.find(".ed-window").attributes("style") ?? "").not.toContain(
      "display: none",
    );

    popover.remove();
    await wrapper.find(".ed-window").trigger("keydown", { key: "Escape" });
    await flushPromises();
    expect(wrapper.find(".ed-window").attributes("style")).toContain("display: none");
    wrapper.unmount();
  });

  it("closes the find bar on Escape without minimizing the window", async () => {
    const { wrapper, editor } = mountEditor();
    const tab = makeTab("t1", false);
    editor.tabs = [tab];
    editor.activeId = tab.id;
    await flushPromises();

    await toolbarButton(wrapper, "Find / replace").trigger("click");
    await flushPromises();
    expect(wrapper.find(".ed-find").exists()).toBe(true);
    const session = codemirror.sessions[0]!;
    expect(session.search.setOptions).toHaveBeenCalled();

    session.focus.mockClear();
    await wrapper.find(".ed-window").trigger("keydown", { key: "Escape" });
    await flushPromises();

    expect(wrapper.find(".ed-find").exists()).toBe(false);
    expect(wrapper.find(".ed-window").attributes("style") ?? "").not.toContain(
      "display: none",
    );
    expect(session.search.clear).toHaveBeenCalled();
    expect(session.focus).toHaveBeenCalledTimes(1);
    wrapper.unmount();
  });

  it("opens the find bar collapsed and never recounts on a cursor move", async () => {
    const { wrapper, editor } = mountEditor();
    const tab = makeTab("t1", false);
    editor.tabs = [tab];
    editor.activeId = tab.id;
    await flushPromises();

    await toolbarButton(wrapper, "Find / replace").trigger("click");
    await flushPromises();
    expect(wrapper.findAll(".ed-find input")).toHaveLength(1);

    const options = lastSessionOptions();
    const session = codemirror.sessions[0]!;
    session.search.setOptions.mockClear();
    (options.onCursor as (_line: number, _column: number) => void)(2, 3);
    await flushPromises();
    expect(session.search.setOptions).not.toHaveBeenCalled();

    (options.onReplace as () => void)();
    await flushPromises();
    expect(wrapper.findAll(".ed-find input")).toHaveLength(2);
    wrapper.unmount();
  });

  it("rejects an out-of-range go-to-line entry and jumps when valid", async () => {
    const { wrapper, editor } = mountEditor();
    const tab = makeTab("t1", false);
    editor.tabs = [tab];
    editor.activeId = tab.id;
    await flushPromises();

    await toolbarButton(wrapper, "Go to line").trigger("click");
    await flushPromises();
    const panel = wrapper.find(".ed-goto");
    expect(panel.exists()).toBe(true);

    const input = panel.find("input");
    await input.setValue("0");
    await input.trigger("keydown", { key: "Enter" });
    expect(panel.find('[role="alert"]').text()).toBe("Enter a line from 1 to 10");
    expect(codemirror.sessions[0]!.gotoLine).not.toHaveBeenCalled();

    await input.setValue("2:3");
    await input.trigger("keydown", { key: "Enter" });
    await flushPromises();
    expect(codemirror.sessions[0]!.gotoLine).toHaveBeenCalledWith(2, 3);
    expect(wrapper.find(".ed-goto").exists()).toBe(false);
    wrapper.unmount();
  });

  it("closes the tab menu on Escape without minimizing the window", async () => {
    const { wrapper, editor } = mountEditor();
    const tab = makeTab("t1", false);
    editor.tabs = [tab];
    editor.activeId = tab.id;
    await flushPromises();

    await wrapper
      .find(".ed-tab")
      .trigger("contextmenu", { clientX: 24, clientY: 24 });
    await flushPromises();
    const openMenu = () =>
      wrapper.findAllComponents(NDropdown).find((menu) => menu.props("show") === true);
    expect(openMenu()).toBeTruthy();

    await wrapper.find(".ed-window").trigger("keydown", { key: "Escape" });
    await flushPromises();
    expect(openMenu()).toBeUndefined();
    expect(wrapper.find(".ed-window").attributes("style") ?? "").not.toContain(
      "display: none",
    );

    await wrapper.find(".ed-window").trigger("keydown", { key: "Escape" });
    await flushPromises();
    expect(wrapper.find(".ed-window").attributes("style")).toContain("display: none");
    wrapper.unmount();
  });

  it("does not collapse the window on Escape while completion is open", async () => {
    const { wrapper, editor } = mountEditor();
    const tab = makeTab("t1", false);
    editor.tabs = [tab];
    editor.activeId = tab.id;
    await flushPromises();

    const tooltip = document.createElement("div");
    tooltip.className = "cm-tooltip cm-tooltip-autocomplete";
    document.body.append(tooltip);

    await wrapper.find(".ed-window").trigger("keydown", { key: "Escape" });
    await flushPromises();
    expect(wrapper.find(".ed-window").attributes("style") ?? "").not.toContain(
      "display: none",
    );

    tooltip.remove();
    await wrapper.find(".ed-window").trigger("keydown", { key: "Escape" });
    await flushPromises();
    expect(wrapper.find(".ed-window").attributes("style")).toContain("display: none");
    wrapper.unmount();
  });

  it("reopens the window when a new file is opened while minimized", async () => {
    const { wrapper, editor } = mountEditor();
    const first = makeTab("t1", false);
    editor.tabs = [first];
    editor.activeId = first.id;
    await flushPromises();

    await wrapper.find("button.ed-window-min").trigger("click");
    await flushPromises();
    expect(wrapper.find(".ed-window").attributes("style")).toContain("display: none");

    const second = makeTab("t2", false);
    editor.tabs = [first, second];
    editor.activeId = second.id;
    await flushPromises();

    expect(wrapper.find(".ed-window").attributes("style") ?? "").not.toContain("display: none");
    expect(useEditorWindowStore().minimized).toBe(false);
    wrapper.unmount();
  });

  it("reopens the window when an already-open file is activated while minimized", async () => {
    const { wrapper, editor } = mountEditor();
    const first = makeTab("t1", false);
    const second = makeTab("t2", false);
    editor.tabs = [first, second];
    editor.activeId = first.id;
    await flushPromises();

    await wrapper.find("button.ed-window-min").trigger("click");
    await flushPromises();
    expect(wrapper.find(".ed-window").attributes("style")).toContain("display: none");

    editor.activeId = second.id;
    await flushPromises();

    expect(editor.tabs).toHaveLength(2);
    expect(wrapper.find(".ed-window").attributes("style") ?? "").not.toContain("display: none");
    wrapper.unmount();
  });

  it("returns focus to the active tab on restore", async () => {    const { wrapper, editor } = mountEditor();
    const tab = makeTab("t1", true);
    editor.tabs = [tab];
    editor.activeId = tab.id;
    await flushPromises();

    await wrapper.find("button.ed-window-min").trigger("click");
    await flushPromises();
    expect(wrapper.find(".ed-window").attributes("style")).toContain("display: none");

    useEditorWindowStore().setMinimized(false);
    await flushPromises();
    await nextTick();
    expect(document.activeElement?.id).toBe(`ed-tab-${tab.id}`);
    wrapper.unmount();
  });

  it("never prompts on route navigation: dirty tabs survive the move", async () => {
    const { wrapper, editor, router } = await mountEditorWithRouter();
    const tab = makeTab("t1", true);
    editor.tabs = [tab];
    editor.activeId = tab.id;
    await flushPromises();

    await router.push("/dashboard");
    await flushPromises();

    expect(leaveDialog(wrapper)).toBeUndefined();
    expect(editor.tabs).toHaveLength(1);
    expect(editor.hasDirty).toBe(true);
    expect(wrapper.find(".ed-window").attributes("style") ?? "").not.toContain(
      "display: none",
    );
    wrapper.unmount();
  });

  it("discards the window state when a server switch is confirmed", async () => {
    const { wrapper, files, editor } = await mountEditorWithRouter();
    files.serverId = "srv-a";
    editor.serverId = "srv-a";
    const tab = makeTab("t1", true);
    editor.tabs = [tab];
    editor.activeId = tab.id;
    await flushPromises();

    files.serverId = "srv-b";
    await flushPromises();
    expect(leaveDialog(wrapper)).toBeTruthy();

    const discard = wrapper
      .findAll("button")
      .find((button) => button.text() === "Discard and leave");
    expect(discard).toBeTruthy();
    await discard!.trigger("click");
    await flushPromises();

    expect(editor.tabs).toHaveLength(0);
    expect(leaveDialog(wrapper)).toBeUndefined();
    wrapper.unmount();
  });

  it("resizes from the keyboard with arrows and Shift", async () => {
    const { wrapper } = mountEditor();
    const tab = makeTab("t1", false);
    const editor = useEditorStore();
    editor.tabs = [tab];
    editor.activeId = tab.id;
    await flushPromises();

    const windows = useEditorWindowStore();
    windows.setSize(800, 600);
    const grip = wrapper.find(".ed-resize--se");
    expect(grip.attributes("tabindex")).toBe("0");
    expect(grip.attributes("role")).toBe("slider");

    await grip.trigger("keydown", { key: "ArrowLeft" });
    expect(windows.width).toBe(800 - 16);
    await grip.trigger("keydown", { key: "ArrowUp", shiftKey: true });
    expect(windows.height).toBe(600 - 64);
    await grip.trigger("keydown", { key: "Enter" });
    expect(windows.width).toBe(800 - 16);
    wrapper.unmount();
  });

  it("persists a pointer drag seeded from the displayed size", async () => {
    Object.defineProperty(window, "innerWidth", { configurable: true, value: 1600 });
    Object.defineProperty(window, "innerHeight", { configurable: true, value: 1000 });
    const captureHolder = window.HTMLElement.prototype as unknown as {
      setPointerCapture?: (_id: number) => void;
    };
    const originalCapture = captureHolder.setPointerCapture;
    captureHolder.setPointerCapture = () => {};
    function firePointer(
      target: Element,
      type: string,
      init: { button?: number; clientX?: number; clientY?: number; pointerId?: number },
    ): void {
      const event = new MouseEvent(type, {
        bubbles: true,
        cancelable: true,
        button: init.button ?? 0,
        clientX: init.clientX ?? 0,
        clientY: init.clientY ?? 0,
      });
      Object.defineProperty(event, "pointerId", { value: init.pointerId ?? 0 });
      target.dispatchEvent(event);
    }
    try {
      const { wrapper } = mountEditor();
      const tab = makeTab("t1", false);
      const editor = useEditorStore();
      editor.tabs = [tab];
      editor.activeId = tab.id;
      await flushPromises();

      const windows = useEditorWindowStore();
      windows.setSize(1331, 936);
      Object.defineProperty(window, "innerWidth", { configurable: true, value: 800 });
      Object.defineProperty(window, "innerHeight", { configurable: true, value: 600 });
      window.dispatchEvent(new Event("resize"));
      await flushPromises();

      const grip = wrapper.find(".ed-resize--se").element;
      firePointer(grip, "pointerdown", { button: 0, clientX: 200, clientY: 200, pointerId: 7 });
      firePointer(grip, "pointermove", { clientX: 184, clientY: 200, pointerId: 7 });
      firePointer(grip, "pointerup", { pointerId: 7 });
      await flushPromises();

      expect(windows.width).toBe(736);
      expect(wrapper.find(".ed-window").attributes("style") ?? "").toContain(
        "width: 736px",
      );
      wrapper.unmount();
    } finally {
      Object.defineProperty(window, "innerWidth", { configurable: true, value: 1024 });
      Object.defineProperty(window, "innerHeight", { configurable: true, value: 768 });
      if (originalCapture === undefined) {
        delete captureHolder.setPointerCapture;
      } else {
        captureHolder.setPointerCapture = originalCapture;
      }
    }
  });

  it("reports the displayed size and viewport max on the resize handle", async () => {
    const { wrapper } = mountEditor();
    const tab = makeTab("t1", false);
    const editor = useEditorStore();
    editor.tabs = [tab];
    editor.activeId = tab.id;
    await flushPromises();

    const windows = useEditorWindowStore();
    windows.setSize(800, 600);
    await flushPromises();
    const grip = wrapper.find(".ed-resize--se");
    expect(grip.attributes("aria-valuemax")).toBe(String(window.innerWidth));
    expect(grip.attributes("aria-valuenow")).toBe("800");
    await grip.trigger("keydown", { key: "ArrowLeft" });
    expect(grip.attributes("aria-valuenow")).toBe("784");
    wrapper.unmount();
  });

  it("exposes a resize grip on every edge and corner (JUS-241)", async () => {
    const { wrapper } = mountEditor();
    const tab = makeTab("t1", false);
    const editor = useEditorStore();
    editor.tabs = [tab];
    editor.activeId = tab.id;
    await flushPromises();

    expect(wrapper.findAll(".ed-resize")).toHaveLength(8);
    for (const direction of ["n", "ne", "e", "se", "s", "sw", "w", "nw"]) {
      expect(wrapper.find(`.ed-resize--${direction}`).exists()).toBe(true);
    }
    const slider = wrapper.find(".ed-resize--se");
    expect(slider.attributes("role")).toBe("slider");
    expect(slider.attributes("tabindex")).toBe("0");
    for (const direction of ["n", "ne", "e", "s", "sw", "w", "nw"]) {
      const grip = wrapper.find(`.ed-resize--${direction}`);
      expect(grip.attributes("aria-hidden")).toBe("true");
      expect(grip.attributes("tabindex")).toBeUndefined();
    }
    wrapper.unmount();
  });

  it("resizes from the west grip, keeping the right edge in place (JUS-241)", async () => {
    setViewport(800, 600);
    const restoreCapture = stubPointerCapture();
    try {
      const { wrapper } = mountEditor();
      const tab = makeTab("t1", false);
      const editor = useEditorStore();
      editor.tabs = [tab];
      editor.activeId = tab.id;
      await flushPromises();

      const windows = useEditorWindowStore();
      windows.setSize(800, 600);
      await flushPromises();

      const grip = wrapper.find(".ed-resize--w").element;
      firePointerEvent(grip, "pointerdown", {
        button: 0,
        clientX: 200,
        clientY: 200,
        pointerId: 11,
      });
      firePointerEvent(grip, "pointermove", {
        clientX: 240,
        clientY: 200,
        pointerId: 11,
      });
      firePointerEvent(grip, "pointerup", { pointerId: 11 });
      await flushPromises();

      expect(windows.width).toBe(712);
      expect(windows.height).toBe(496);
      expect(windows.x).toBe(40);
      expect(wrapper.find(".ed-window").attributes("style") ?? "").toContain(
        "width: 712px",
      );
      wrapper.unmount();
    } finally {
      restoreCapture();
      setViewport(1024, 768);
    }
  });

  it("resizes from the north grip, keeping the bottom edge in place (JUS-241)", async () => {
    setViewport(1600, 1000);
    const restoreCapture = stubPointerCapture();
    try {
      const { wrapper } = mountEditor();
      const tab = makeTab("t1", false);
      const editor = useEditorStore();
      editor.tabs = [tab];
      editor.activeId = tab.id;
      await flushPromises();

      const windows = useEditorWindowStore();
      windows.setSize(1331, 936);
      await flushPromises();

      const grip = wrapper.find(".ed-resize--n").element;
      firePointerEvent(grip, "pointerdown", {
        button: 0,
        clientX: 200,
        clientY: 200,
        pointerId: 12,
      });
      firePointerEvent(grip, "pointermove", {
        clientX: 200,
        clientY: 240,
        pointerId: 12,
      });
      firePointerEvent(grip, "pointerup", { pointerId: 12 });
      await flushPromises();

      expect(windows.width).toBe(1331);
      expect(windows.height).toBe(856);
      expect(windows.y).toBe(40);
      wrapper.unmount();
    } finally {
      restoreCapture();
      setViewport(1024, 768);
    }
  });

  it("minimizes the window on Escape", async () => {
    const { wrapper, editor } = mountEditor();
    const tab = makeTab("t1", false);
    editor.tabs = [tab];
    editor.activeId = tab.id;
    await flushPromises();

    await wrapper.find(".ed-window").trigger("keydown", { key: "Escape" });
    await flushPromises();

    expect(wrapper.find(".ed-window").attributes("style")).toContain("display: none");
    expect(useEditorWindowStore().minimized).toBe(true);
    wrapper.unmount();
  });

  it("reconfigures a live session read-only when the write gate drops", async () => {
    const { wrapper, files, editor } = mountEditor();
    const tab = makeTab("t1", false);
    editor.tabs = [tab];
    editor.activeId = tab.id;
    await flushPromises();
    await nextTick();
    expect(codemirror.sessions).toHaveLength(1);
    const session = codemirror.sessions[0];

    files.roots = [{ name: "volumes", path: "/v", writable: false, free_bytes: 1 }];
    await flushPromises();
    expect(session.setReadOnly).toHaveBeenCalledWith(true);
    wrapper.unmount();
  });

  it("shows a truncated tail read-only with a banner, badge and download", async () => {
    const { wrapper, editor } = mountEditor();
    const tab = {
      ...makeTab("t1", false),
      path: "server.log",
      name: "server.log",
      truncated: true,
      size: 5_000_000,
    };
    editor.tabs = [tab];
    editor.activeId = tab.id;
    await flushPromises();

    expect(codemirror.sessions).toHaveLength(1);
    expect(createEditorSession).toHaveBeenCalledWith(
      expect.objectContaining({ readOnly: true }),
    );
    const banner = wrapper.find(".ed-truncated");
    expect(banner.text()).toContain("2 MiB");
    expect(
      banner.findAll("button").some((button) => button.text().includes("Download")),
    ).toBe(true);
    expect(wrapper.find(".ed-readonly").text()).toBe("Read-only");

    expect(wrapper.findAll("button").some((button) => button.text() === "History")).toBe(
      false,
    );

    const save = wrapper.findAll("button").find((button) => button.text() === "Save");
    expect(save?.attributes("disabled")).toBeDefined();

    const saveAll = wrapper.findAll("button").find(
      (button) => button.text() === "Save all",
    );
    expect(saveAll?.attributes("disabled")).toBeDefined();

    editor.tabs = [...editor.tabs, makeTab("t2", true)];
    editor.activate(tab.id);
    await flushPromises();
    const liveSaveAll = wrapper.findAll("button").find(
      (button) => button.text() === "Save all",
    );
    expect(liveSaveAll?.attributes("disabled")).toBeUndefined();
    wrapper.unmount();
  });

  it("applies a settings change to every live session", async () => {
    const { wrapper, editor } = mountEditor();
    const first = makeTab("t1", false);
    const second = makeTab("t2", false);
    editor.tabs = [first, second];
    editor.activeId = first.id;
    await flushPromises();

    editor.activate(second.id);
    await flushPromises();
    expect(codemirror.sessions).toHaveLength(2);

    useEditorSettingsStore().update({ wrap: true });
    await flushPromises();

    for (const session of codemirror.sessions) {
      expect(session.setSettings).toHaveBeenLastCalledWith(
        expect.objectContaining({ wrap: true }),
      );
    }
    wrapper.unmount();
  });

  it("applies the current settings to a tab opened after a change", async () => {
    const { wrapper, editor } = mountEditor();
    useEditorSettingsStore().update({ wrap: true });

    const tab = makeTab("t1", false);
    editor.tabs = [tab];
    editor.activeId = tab.id;
    await flushPromises();

    expect(codemirror.sessions).toHaveLength(1);
    expect(codemirror.sessions[0].setSettings).toHaveBeenLastCalledWith(
      expect.objectContaining({ wrap: true }),
    );
    wrapper.unmount();
  });

  it("applies editor settings to history diff merge views", async () => {
    const { wrapper, editor } = mountEditor();
    const tab = makeDiffTab("d1");
    editor.tabs = [tab];
    editor.activeId = tab.id;
    await flushPromises();

    expect(mergeView.sessions).toHaveLength(1);
    expect(createMergeSession).toHaveBeenCalledWith(
      expect.objectContaining({
        settings: expect.objectContaining({ theme: "dark", lineNumbers: true }),
      }),
    );

    useEditorSettingsStore().update({ lineNumbers: false, theme: "light" });
    await flushPromises();

    expect(mergeView.sessions[0].setSettings).toHaveBeenLastCalledWith(
      expect.objectContaining({ lineNumbers: false, theme: "light" }),
    );
    wrapper.unmount();
  });
});

describe("FilesEditor close-all", () => {
  it("closes every tab with no dialog when nothing is dirty", async () => {
    const { wrapper, editor } = mountEditor();
    editor.tabs = [makeTab("t1", false), makeTab("t2", false)];
    editor.activeId = "t1";
    await flushPromises();

    await wrapper.find("button.ed-window-close").trigger("click");
    await flushPromises();

    expect(wrapper.text()).not.toContain("Close all files?");
    expect(editor.tabs).toHaveLength(0);
    expect(wrapper.find(".ed-window").exists()).toBe(false);
    wrapper.unmount();
  });

  it("asks before discarding and lists the dirty files", async () => {
    const { wrapper, editor } = mountEditor();
    editor.tabs = [makeTab("t1", true), makeTab("t2", false), makeTab("t3", true)];
    editor.activeId = "t1";
    await flushPromises();

    await wrapper.find("button.ed-window-close").trigger("click");
    await flushPromises();

    expect(wrapper.text()).toContain("Close all files?");
    const list = wrapper.find(".ed-unsaved");
    expect(list.text()).toContain("t1.yaml");
    expect(list.text()).toContain("t3.yaml");
    expect(list.text()).not.toContain("t2.yaml");
    expect(editor.tabs).toHaveLength(3);
    wrapper.unmount();
  });

  it("keeps every tab on cancel", async () => {
    const { wrapper, editor } = mountEditor();
    editor.tabs = [makeTab("t1", true)];
    editor.activeId = "t1";
    await flushPromises();

    await wrapper.find("button.ed-window-close").trigger("click");
    await flushPromises();
    const cancel = wrapper
      .findAll("button")
      .find((button) => button.text() === "Cancel");
    expect(cancel).toBeTruthy();
    await cancel!.trigger("click");
    await flushPromises();

    expect(wrapper.text()).not.toContain("Close all files?");
    expect(editor.tabs).toHaveLength(1);
    expect(editor.hasDirty).toBe(true);
    wrapper.unmount();
  });

  it("discards every dirty buffer on confirm", async () => {
    const { wrapper, editor } = mountEditor();
    editor.tabs = [makeTab("t1", true), makeTab("t2", true)];
    editor.activeId = "t1";
    await flushPromises();

    await wrapper.find("button.ed-window-close").trigger("click");
    await flushPromises();
    const discard = wrapper
      .findAll("button")
      .find((button) => button.text() === "Discard and close");
    expect(discard).toBeTruthy();
    await discard!.trigger("click");
    await flushPromises();

    expect(editor.tabs).toHaveLength(0);
    expect(editor.hasDirty).toBe(false);
    wrapper.unmount();
  });

  it("resets the dragged position when the editor is closed and reopened", async () => {
    const { wrapper, editor } = mountEditor();
    editor.tabs = [makeTab("t1", false)];
    editor.activeId = "t1";
    await flushPromises();

    const bar = wrapper.find(".ed-window-bar").element as HTMLElement;
    bar.setPointerCapture = vi.fn();
    const down = new Event("pointerdown");
    Object.assign(down, { button: 0, pointerId: 1, clientX: 10, clientY: 10 });
    bar.dispatchEvent(down);
    const move = new Event("pointermove");
    Object.assign(move, { pointerId: 1, clientX: 210, clientY: 110 });
    bar.dispatchEvent(move);
    await flushPromises();
    expect(wrapper.find(".ed-window").attributes("style")).toContain("left:");

    await wrapper.find("button.ed-window-close").trigger("click");
    await flushPromises();
    expect(wrapper.find(".ed-window").exists()).toBe(false);

    editor.tabs = [makeTab("t2", false)];
    editor.activeId = "t2";
    await flushPromises();
    expect(wrapper.find(".ed-window").attributes("style") ?? "").not.toContain("left:");
    wrapper.unmount();
  });

  it("does not re-anchor the window on a bare title-bar click", async () => {
    const { wrapper, editor } = mountEditor();
    editor.tabs = [makeTab("t1", false)];
    editor.activeId = "t1";
    await flushPromises();

    const bar = wrapper.find(".ed-window-bar").element as HTMLElement;
    bar.setPointerCapture = vi.fn();
    const down = new Event("pointerdown");
    Object.assign(down, { button: 0, pointerId: 1, clientX: 10, clientY: 10 });
    bar.dispatchEvent(down);
    await flushPromises();

    expect(wrapper.find(".ed-window").attributes("style") ?? "").not.toContain("left:");
    wrapper.unmount();
  });
});
