/* global window:readonly */
import { NMessageProvider } from "naive-ui";

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
import { createPinia, setActivePinia } from "pinia";
import { defineComponent, h, onMounted } from "vue";
import { flushPromises, mount } from "@vue/test-utils";
import { beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("@/features/files/api/files", async (importOriginal) => {
  const actual =
    await importOriginal<typeof import("@/features/files/api/files")>();
  return {
    ...actual,
    copyEntry: vi.fn(),
    createFile: vi.fn(),
    createFolder: vi.fn(),
    getContent: vi.fn(),
    listEntries: vi.fn(),
    listRoots: vi.fn(),
    renameEntry: vi.fn(),
  };
});

import {
  copyEntry,
  createFolder,
  getContent,
  listEntries,
  listRoots,
  renameEntry,
} from "@/features/files/api/files";
import CreateEntryDialog from "@/features/files/components/CreateEntryDialog.vue";
import FilesBrowser from "@/features/files/components/FilesBrowser.vue";
import { useFilesTabsPersist } from "@/features/files/composables/useFilesTabsPersist";
import type { FilesEntry, FilesRoot } from "@/features/files/schemas/files";
import { useAuthStore } from "@/features/auth/stores/auth";
import { filesPane, provideFilesPane } from "@/features/files/stores/filesPane";
import {
  useFilesPaneSession,
  type FilesPaneId,
} from "@/features/files/stores/paneSession";
import {
  filesTabsStorageKey,
  legacyFilesTabsStorageKey,
  writeFilesTabsSnapshot,
} from "@/features/files/utils/tabsPersist";
import { setSession } from "@/shared/api/token";
import {
  i18n,
  registerDiscoveredCatalogs,
  resetLocaleState,
  syncComposerLocale,
} from "@/shared/i18n";

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

const listing = {
  entries: [entry({ name: "app.yaml" })],
  truncated: false,
  cursor: "",
};

function deferred<T>() {
  let resolve!: (_value: T) => void;
  const promise = new Promise<T>((resolvePromise) => {
    resolve = resolvePromise;
  });
  return { promise, resolve };
}

function signIn(id: string): void {
  setSession({
    user: { id, email: `${id}@gotham.dev`, created_at: "2026-01-01T00:00:00Z" },
    accessToken: "a",
    refreshToken: "r",
  });
}

beforeEach(() => {
  window.localStorage.clear();
  setActivePinia(createPinia());
  registerDiscoveredCatalogs();
  resetLocaleState();
  syncComposerLocale("en");
  vi.clearAllMocks();
  vi.mocked(listRoots).mockResolvedValue([root]);
  vi.mocked(listEntries).mockResolvedValue(listing);
});

describe("files panes are independent contexts", () => {
  it("keeps node, root, path, tabs, selection and scroll per pane", async () => {
    const left = filesPane("left");
    const right = filesPane("right");
    expect(left.files).not.toBe(right.files);

    await left.files.init("server-left");
    await right.files.init("server-right");
    await left.files.openTab({ path: "one" });
    left.files.selectOnly("app.yaml");
    left.files.setScrollTop(120);
    right.files.setViewMode("grid");

    expect(left.files.serverId).toBe("server-left");
    expect(right.files.serverId).toBe("server-right");
    expect(left.files.tabs.map((tab) => tab.path)).toEqual(["", "one"]);
    expect(right.files.tabs.map((tab) => tab.path)).toEqual([""]);
    expect(left.files.selectedNames).toEqual(["app.yaml"]);
    expect(right.files.selectedNames).toEqual([]);
    expect(left.files.scrollTop).toBe(120);
    expect(right.files.viewMode).toBe("grid");
    expect(left.files.viewMode).toBe("list");
  });

  it("keeps editor contents per pane", async () => {
    vi.mocked(getContent).mockResolvedValue({
      content: "left doc",
      etag: '"1"',
      size: 8,
      mode: 0o100644,
      encoding: "utf-8",
    });
    const left = filesPane("left");
    const right = filesPane("right");

    await left.editor.openFile("server-1", "volumes", "note.md");

    expect(left.editor.tabs).toHaveLength(1);
    expect(right.editor.tabs).toHaveLength(0);
    expect(left.editor.activeTab?.doc).toBe("left doc");
  });

  it("does not let a delayed listing of a previous node reach the pane", async () => {
    const gate = deferred<typeof listing>();
    const left = filesPane("left");
    const right = filesPane("right");
    await right.files.init("server-2");
    vi.mocked(listEntries).mockImplementation(async (serverId) =>
      serverId === "server-1"
        ? gate.promise
        : {
            entries: [entry({ name: `${serverId}.txt` })],
            truncated: false,
            cursor: "",
          },
    );

    const stale = left.files.init("server-1");
    await flushPromises();
    await left.files.init("server-3");
    gate.resolve(listing);
    await stale;
    await flushPromises();

    expect(left.files.serverId).toBe("server-3");
    expect(left.files.entries.map((item) => item.name)).toEqual([
      "server-3.txt",
    ]);
    expect(right.files.serverId).toBe("server-2");
  });

  it("refreshes the other pane's listing of the same node and directory", async () => {
    vi.mocked(createFolder).mockResolvedValue(
      entry({ name: "made", kind: "directory", mode: 0o040755 }),
    );
    const left = filesPane("left");
    const right = filesPane("right");
    await left.files.init("server-1");
    await right.files.init("server-1");
    vi.mocked(listEntries).mockClear();

    await left.files.createEntry("made", "directory");

    expect(vi.mocked(listEntries)).toHaveBeenCalledTimes(2);
    expect(vi.mocked(listEntries)).toHaveBeenCalledWith("server-1", {
      root: "volumes",
      path: "",
    });
  });
});

describe("two mounted browsers", () => {
  it("renders, selects and edits in its own pane", async () => {
    vi.mocked(listEntries).mockImplementation(async (serverId) => ({
      entries: [entry({ name: `${serverId}.txt` })],
      truncated: false,
      cursor: "",
    }));
    vi.mocked(getContent).mockResolvedValue({
      content: "left",
      etag: '"1"',
      size: 4,
      mode: 0o100644,
      encoding: "utf-8",
    });
    const wrapper = mount(
      defineComponent({
        render: () =>
          h("div", [
            h(FilesBrowser, { serverId: "server-left", pane: "left" }),
            h(FilesBrowser, { serverId: "server-right", pane: "right" }),
          ]),
      }),
      { global: { plugins: [i18n], stubs: { teleport: true } } },
    );
    await flushPromises();

    const rows = wrapper.findAll(".fm-row[data-entry]");
    expect(rows.map((row) => row.attributes("data-entry"))).toEqual([
      "server-left.txt",
      "server-right.txt",
    ]);

    await rows[0]!.trigger("click");
    await flushPromises();
    expect(
      wrapper.findAll(".fm-row.is-selected").map((row) => row.attributes("data-entry")),
    ).toEqual(["server-left.txt"]);

    await rows[0]!.trigger("dblclick");
    await flushPromises();
    expect(filesPane("left").editor.tabs).toHaveLength(1);
    expect(filesPane("right").editor.tabs).toHaveLength(0);
  });
});

describe("files pane clipboard", () => {
  it("blocks a paste copied on another node without touching the clipboard", async () => {
    const left = filesPane("left");
    const right = filesPane("right");
    await left.files.init("server-1");
    await right.files.init("server-2");

    left.files.selectOnly("app.yaml");
    left.files.copySelection();

    expect(right.files.pasteProblem).toBe("cross-node");
    expect(right.files.hasClipboard).toBe(true);
    await right.files.paste();
    expect(vi.mocked(copyEntry)).not.toHaveBeenCalled();
    expect(left.files.pasteProblem).not.toBe("cross-node");
  });
});

describe("files pane rename", () => {
  it("keeps a clipboard taken on another node through a same-named rename", async () => {
    vi.mocked(renameEntry).mockResolvedValue(entry({ name: "renamed.yaml" }));
    const left = filesPane("left").files;
    const right = filesPane("right").files;
    await left.init("server-1");
    await right.init("server-2");
    left.rootName = "volumes";
    right.rootName = "volumes";
    left.selectOnly("app.yaml");
    left.copySelection();

    await right.rename(entry({ name: "app.yaml" }), "renamed.yaml");

    expect(right.clipboard?.serverId).toBe("server-1");
    expect(right.clipboard?.names).toEqual(["app.yaml"]);
    expect(left.pasteProblem).not.toBe("cross-node");
  });
});

describe("files pane persistence", () => {
  function persistHost(pane: FilesPaneId, serverId: string) {
    return defineComponent({
      setup() {
        const { initBrowser } = useFilesTabsPersist(() => serverId, pane);
        onMounted(() => void initBrowser(serverId));
        return () => h("div");
      },
    });
  }

  it("persists tabs per pane key and restores the legacy snapshot into the left pane", async () => {
    signIn("u-1");
    const legacyKey = legacyFilesTabsStorageKey("server-1", "u-1");
    expect(
      writeFilesTabsSnapshot(window.localStorage, legacyKey, {
        version: 1,
        tabs: [{ id: "legacy", root: "volumes", path: "www" }],
        activeTabId: "legacy",
        viewMode: "list",
      }),
    ).toBe(true);

    mount(persistHost("left", "server-1"), {
      global: { plugins: [i18n] },
    });
    await flushPromises();

    const left = filesPane("left");
    expect(left.files.tabs.map((tab) => tab.path)).toEqual(["www"]);

    const right = filesPane("right");
    mount(persistHost("right", "server-1"), {
      global: { plugins: [i18n] },
    });
    await flushPromises();
    expect(right.files.tabs.map((tab) => tab.path)).toEqual([""]);

    expect(filesTabsStorageKey("server-1", "u-1", "left")).not.toBe(
      filesTabsStorageKey("server-1", "u-1", "right"),
    );
    expect(useAuthStore().user?.id).toBe("u-1");
  });

  it("falls back safely on a malformed legacy snapshot", async () => {
    signIn("u-1");
    window.localStorage.setItem(
      legacyFilesTabsStorageKey("server-1", "u-1"),
      "{not json",
    );

    mount(persistHost("left", "server-1"), {
      global: { plugins: [i18n] },
    });
    await flushPromises();

    expect(filesPane("left").files.tabs.map((tab) => tab.path)).toEqual([""]);
  });
});

describe("files pane dirty guards and logout", () => {
  it("cancelling a close keeps each pane's editor intact", async () => {
    vi.mocked(getContent).mockResolvedValue({
      content: "dirty",
      etag: '"1"',
      size: 5,
      mode: 0o100644,
      encoding: "utf-8",
    });
    const left = filesPane("left");
    const right = filesPane("right");
    await left.editor.openFile("server-1", "volumes", "a.md");
    await right.editor.openFile("server-1", "volumes", "b.md");
    left.editor.setDoc(left.editor.tabs[0]!.id, "changed left");
    right.editor.setDoc(right.editor.tabs[0]!.id, "changed right");

    const stay = left.editor.confirmDiscard();
    left.editor.resolveLeave(false);
    expect(await stay).toBe("stay");
    expect(left.editor.tabs).toHaveLength(1);
    expect(right.editor.tabs).toHaveLength(1);

    const leave = right.editor.confirmDiscard();
    right.editor.resolveLeave(true);
    expect(await leave).toBe("leave");
  });

  it("clears both panes, editors and the clipboard on logout", async () => {
    signIn("u-1");
    const left = filesPane("left");
    const right = filesPane("right");
    await left.files.init("server-1");
    await right.files.init("server-1");
    left.files.selectOnly("app.yaml");
    left.files.copySelection();
    left.editor.tabs = [
      mkEditorTab("left-tab"),
    ];
    right.editor.tabs = [
      mkEditorTab("right-tab"),
    ];

    useAuthStore().clearSession();

    expect(left.files.serverId).toBe("");
    expect(right.files.serverId).toBe("");
    expect(left.files.tabs).toHaveLength(1);
    expect(right.files.tabs).toHaveLength(1);
    expect(left.editor.tabs).toHaveLength(0);
    expect(right.editor.tabs).toHaveLength(0);
    expect(useFilesPaneSession().clipboard).toBeNull();
  });
});

describe("files pane dialogs", () => {
  it("sends the dialog request to the pane that opened it after focus moves", async () => {
    vi.mocked(createFolder).mockResolvedValue(
      entry({ name: "made", kind: "directory", mode: 0o040755 }),
    );
    const left = filesPane("left");
    const right = filesPane("right");
    await left.files.init("server-left");
    await right.files.init("server-right");
    const session = useFilesPaneSession();

    const PaneHost = defineComponent({
      props: { pane: { type: String, required: true } },
      setup(props) {
        provideFilesPane(props.pane as FilesPaneId);
        return () =>
          h(NMessageProvider, null, {
            default: () =>
              h(CreateEntryDialog, { show: true, kind: "directory" }),
          });
      },
    });
    const wrapper = mount(
      defineComponent({
        render: () =>
          h("div", [
            h(PaneHost, { pane: "left" }),
            h(PaneHost, { pane: "right" }),
          ]),
      }),
      { global: { plugins: [i18n], stubs: { teleport: true } } },
    );
    await flushPromises();

    session.activate("right");
    const inputs = wrapper.findAll("input");
    expect(inputs).toHaveLength(2);
    const leftInput = wrapper
      .findAllComponents(CreateEntryDialog)[0]!
      .find("input");
    await leftInput.setValue("from-left");
    leftInput.trigger("keydown.enter");
    await flushPromises();

    expect(vi.mocked(createFolder)).toHaveBeenCalledWith("server-left", {
      root: "volumes",
      path: "from-left",
    });
    expect(right.files.activeTab.path).toBe("");
  });
});

function mkEditorTab(id: string) {
  return {
    id,
    kind: "file" as const,
    root: "volumes",
    path: `${id}.md`,
    name: `${id}.md`,
    doc: "changed",
    original: "start",
    etag: '"1"',
    eol: "\n" as const,
    bom: false,
    encoding: "utf-8" as const,
    size: 7,
    mode: 0o100644,
    truncated: false,
    language: "markdown" as const,
    languageManual: false,
    status: "ready" as const,
    error: "",
    saving: false,
  };
}
