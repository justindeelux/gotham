import { beforeEach, describe, expect, it, vi } from "vitest";

const messages = {
  error: vi.fn(),
  success: vi.fn(),
  warning: vi.fn(),
  info: vi.fn(),
};

vi.mock("naive-ui", async (importOriginal) => {
  const actual = await importOriginal<typeof import("naive-ui")>();
  return { ...actual, useMessage: () => messages };
});

vi.mock("@/features/files/api/files", async (importOriginal) => {
  const actual =
    await importOriginal<typeof import("@/features/files/api/files")>();
  return {
    ...actual,
    listRoots: vi.fn(),
    listEntries: vi.fn(),
    createFile: vi.fn(),
    createFolder: vi.fn(),
    renameEntry: vi.fn(),
    moveEntry: vi.fn(),
    copyEntry: vi.fn(),
    deleteEntry: vi.fn(),
  };
});

import { createPinia, setActivePinia } from "pinia";
import { flushPromises, mount, type VueWrapper } from "@vue/test-utils";

import {
  copyEntry,
  listEntries,
  listRoots,
  moveEntry,
} from "@/features/files/api/files";
import FilesBrowser from "@/features/files/components/FilesBrowser.vue";
import type { FilesEntry, FilesRoot } from "@/features/files/schemas/files";
import { useEditorStore } from "@/features/files/stores/editor";
import type { EditorTab } from "@/features/files/stores/editor";
import { useFilesStore } from "@/features/files/stores/files";
import { filesPane } from "@/features/files/stores/filesPane";
import { i18n, registerDiscoveredCatalogs, resetLocaleState, syncComposerLocale } from "@/shared/i18n";

const root: FilesRoot = {
  name: "volumes",
  path: "/var/lib/gotham/volumes",
  writable: true,
  free_bytes: 1,
};

function entry(overrides: Partial<FilesEntry> & { name: string }): FilesEntry {
  return {
    kind: "file",
    size: 0,
    mode: 0o100644,
    modified_at: "2026-10-12T09:02:00Z",
    etag: '"1"',
    ...overrides,
  };
}

beforeEach(() => {
  setActivePinia(createPinia());
  registerDiscoveredCatalogs();
  resetLocaleState();
  syncComposerLocale("en");
  vi.clearAllMocks();
  vi.mocked(listRoots).mockResolvedValue([root]);
  vi.mocked(listEntries).mockResolvedValue({
    entries: [
      entry({ name: "app.yaml" }),
      entry({ name: "data", kind: "directory", size: 0, mode: 0o040755 }),
    ],
    truncated: false,
    cursor: "",
  });
});

describe("files pane clipboard", () => {
  it("pastes a copy taken in another pane on the same node", async () => {
    vi.mocked(copyEntry).mockResolvedValue(entry({ name: "app.yaml" }));
    const left = filesPane("left").files;
    const right = filesPane("right").files;
    await left.init("server-1");
    await right.init("server-1");
    left.selectOnly("app.yaml");
    left.copySelection();
    await right.openDirectory("data");

    const summary = await right.paste();

    expect(vi.mocked(copyEntry)).toHaveBeenCalledWith("server-1", {
      root: "volumes",
      source: "app.yaml",
      destination: "data/app.yaml",
      overwrite: false,
    });
    expect(summary.done).toEqual(["app.yaml"]);
  });
});

describe("files browser paste feedback", () => {
  async function openFileOps(
    wrapper: VueWrapper,
  ): Promise<ReturnType<VueWrapper["findAll"]>> {
    const trigger = wrapper
      .findAll("button")
      .find((button) => button.attributes("aria-label") === "File operations");
    expect(trigger).toBeTruthy();
    await trigger!.trigger("click");
    await flushPromises();
    return wrapper.findAll(".n-dropdown-option-body");
  }

  it("toasts the transfer summary after a cut paste", async () => {
    vi.mocked(moveEntry).mockResolvedValue(entry({ name: "app.yaml" }));

    const store = useFilesStore();
    await store.init("server-1");
    store.toggleSelection("app.yaml");
    store.cutSelection();
    await store.openDirectory("data");

    const wrapper = mount(FilesBrowser, {
      props: { serverId: "server-1" },
      global: { plugins: [i18n], stubs: { teleport: true } },
    });
    await flushPromises();

    const pasteItem = (await openFileOps(wrapper)).find((item) =>
      item.text().includes("Paste"),
    );
    expect(pasteItem).toBeTruthy();
    await pasteItem!.trigger("click");
    await flushPromises();

    expect(vi.mocked(moveEntry)).toHaveBeenCalledWith("server-1", {
      root: "volumes",
      source: "app.yaml",
      destination: "data/app.yaml",
      overwrite: false,
    });
    expect(messages.success).toHaveBeenCalledWith("1 moved.");
  });

  it("disables paste when the current folder is not a valid destination", async () => {
    const store = useFilesStore();
    await store.init("server-1");
    store.toggleSelection("app.yaml");
    store.cutSelection();

    const wrapper = mount(FilesBrowser, {
      props: { serverId: "server-1" },
      global: { plugins: [i18n], stubs: { teleport: true } },
    });
    await flushPromises();

    const pasteItem = (await openFileOps(wrapper)).find((item) =>
      item.text().includes("Paste"),
    );
    expect(pasteItem).toBeTruthy();
    expect(pasteItem!.classes()).toContain(
      "n-dropdown-option-body--disabled",
    );
    expect(store.pasteProblem).toBe("same-folder");
  });

  it("disables paste when the clipboard is empty", async () => {
    const store = useFilesStore();
    await store.init("server-1");

    const wrapper = mount(FilesBrowser, {
      props: { serverId: "server-1" },
      global: { plugins: [i18n], stubs: { teleport: true } },
    });
    await flushPromises();

    expect(store.hasClipboard).toBe(false);
    const pasteItem = (await openFileOps(wrapper)).find((item) =>
      item.text().includes("Paste"),
    );
    expect(pasteItem).toBeTruthy();
    expect(pasteItem!.classes()).toContain(
      "n-dropdown-option-body--disabled",
    );
  });

  it("refuses a cut paste while the source has unsaved editor changes", async () => {
    const store = useFilesStore();
    await store.init("server-1");
    store.toggleSelection("app.yaml");
    store.cutSelection();
    await store.openDirectory("data");

    const editor = useEditorStore();
    const dirty: EditorTab = {
      id: "t1",
      kind: "file",
      root: "volumes",
      path: "app.yaml",
      name: "app.yaml",
      doc: "changed",
      original: "start",
      etag: '"1"',
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
    editor.tabs = [dirty];

    const wrapper = mount(FilesBrowser, {
      props: { serverId: "server-1" },
      global: { plugins: [i18n], stubs: { teleport: true } },
    });
    await flushPromises();

    const pasteItem = (await openFileOps(wrapper)).find((item) =>
      item.text().includes("Paste"),
    );
    expect(pasteItem).toBeTruthy();
    await pasteItem!.trigger("click");
    await flushPromises();

    expect(vi.mocked(moveEntry)).not.toHaveBeenCalled();
    expect(messages.warning).toHaveBeenCalledTimes(1);
    expect(store.hasClipboard).toBe(true);
  });
});
