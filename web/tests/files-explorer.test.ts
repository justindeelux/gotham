// In-editor file explorer (JUS-223): lazy tree caching, inline errors, recursive
// search, rename/delete tab sync, ARIA keyboard navigation and windowing. The
// files API is mocked so the store and the panel run without axios.
/* global document:readonly */
import { flushPromises, mount } from "@vue/test-utils";
import { NMessageProvider } from "naive-ui";
import { createPinia, setActivePinia } from "pinia";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { defineComponent, h, nextTick } from "vue";

vi.mock("@/features/files/api/files", async (importOriginal) => {
  const actual =
    await importOriginal<typeof import("@/features/files/api/files")>();
  return {
    ...actual,
    listEntries: vi.fn(),
    searchEntries: vi.fn(),
    createFile: vi.fn(),
    createFolder: vi.fn(),
    renameEntry: vi.fn(),
    deleteEntry: vi.fn(),
  };
});

import {
  createFile,
  createFolder,
  deleteEntry,
  listEntries,
  renameEntry,
  searchEntries,
} from "@/features/files/api/files";
import EditorExplorer from "@/features/files/components/EditorExplorer.vue";
import type { EditorTab } from "@/features/files/stores/editor";
import { useEditorStore } from "@/features/files/stores/editor";
import { ExplorerDirtyError, useExplorerStore } from "@/features/files/stores/explorer";
import { useFilesStore } from "@/features/files/stores/files";
import type { FilesEntry, FilesRoot } from "@/features/files/schemas/files";
import type { ExplorerDirState, ExplorerRow } from "@/features/files/utils/explorer";
import { flattenTree } from "@/features/files/utils/explorer";
import { useTeamsStore } from "@/features/teams";
import { i18n, resetLocaleState, syncComposerLocale } from "@/shared/i18n";

const root: FilesRoot = {
  name: "volumes",
  path: "/var/lib/gotham/volumes",
  writable: true,
  free_bytes: 1,
};

/** entry builds one listing row with sensible defaults. */
function entry(
  overrides: Partial<FilesEntry> & { name: string },
): FilesEntry {
  return {
    kind: "file",
    size: 10,
    mode: 0o100644,
    modified_at: "2026-10-12T09:02:00Z",
    etag: '"1"',
    ...overrides,
  };
}

/** dir builds one folder row. */
function dir(name: string): FilesEntry {
  return entry({ name, kind: "directory", size: 0, mode: 0o040755 });
}

/** makeTab builds one ready editor tab at `path`. */
function makeTab(path: string, dirty = false): EditorTab {
  return {
    id: `tab-${path}`,
    root: "volumes",
    path,
    name: path.split("/").pop() ?? path,
    doc: dirty ? "changed" : "start",
    original: "start",
    etag: '"1"',
    eol: "\n",
    bom: false,
    size: 5,
    mode: 0o100644,
    language: "text",
    status: "ready",
    error: "",
    saving: false,
  };
}

/** dirState builds one cached directory for the pure flatten tests. */
function dirState(
  path: string,
  entries: FilesEntry[],
  overrides: Partial<ExplorerDirState> = {},
): ExplorerDirState {
  return {
    path,
    entries,
    loaded: true,
    loading: false,
    truncated: false,
    cursor: "",
    error: null,
    ...overrides,
  };
}

/** setupStores seeds the pinia stores the explorer reads (team, roots, editor). */
function setupStores(): {
  explorer: ReturnType<typeof useExplorerStore>;
  editor: ReturnType<typeof useEditorStore>;
  files: ReturnType<typeof useFilesStore>;
} {
  setActivePinia(createPinia());
  const teams = useTeamsStore();
  teams.activeTeamId = "team-1";
  teams.teams = [{ id: "team-1", name: "A", is_personal: true, role: "owner" }];
  const files = useFilesStore();
  files.roots = [root];
  files.rootName = "volumes";
  files.path = "www";
  return {
    explorer: useExplorerStore(),
    editor: useEditorStore(),
    files,
  };
}

beforeEach(() => {
  resetLocaleState();
  syncComposerLocale("en");
  for (const fn of [
    listEntries,
    searchEntries,
    createFile,
    createFolder,
    renameEntry,
    deleteEntry,
  ]) {
    vi.mocked(fn).mockReset();
  }
  vi.mocked(listEntries).mockResolvedValue({
    entries: [],
    truncated: false,
    cursor: "",
  });
});

afterEach(() => {
  document.body.innerHTML = "";
});

describe("flattenTree", () => {
  it("walks only expanded folders, folders first, with row depth", () => {
    const dirs = {
      www: dirState("www", [entry({ name: "b.ts" }), dir("src")]),
      "www/src": dirState("www/src", [entry({ name: "main.ts" })]),
    };
    const collapsed = flattenTree("www", dirs, (path) => path === "www");
    expect(collapsed.map((row) => [row.path, row.type])).toEqual([
      ["www/src", "entry"],
      ["www/b.ts", "entry"],
    ]);
    expect(collapsed[0]).toMatchObject({ depth: 0, kind: "directory", expanded: false });

    const expanded = flattenTree(
      "www",
      dirs,
      (path) => path === "www" || path === "www/src",
    );
    expect(expanded.map((row) => [row.path, row.depth])).toEqual([
      ["www/src", 0],
      ["www/src/main.ts", 1],
      ["www/b.ts", 0],
    ]);
  });

  it("renders inline loading, error and empty rows in place of children", () => {
    const dirs = {
      www: dirState("www", [dir("a-loading"), dir("b-broken"), dir("c-empty")]),
      "www/a-loading": dirState("www/a-loading", [], { loaded: false, loading: true }),
      "www/b-broken": dirState("www/b-broken", [], { loaded: false, error: { status: 500 } }),
      "www/c-empty": dirState("www/c-empty", []),
    };
    const rows = flattenTree("www", dirs, () => true);
    const statuses = rows.filter((row): row is Extract<ExplorerRow, { type: "status" }> =>
      row.type === "status",
    );
    expect(statuses.map((row) => row.status)).toEqual(["loading", "error", "empty"]);
    expect(statuses.every((row) => row.depth === 1)).toBe(true);
    expect(statuses[1]?.error).toMatchObject({ status: 500 });
  });
});

describe("explorer store", () => {
  it("loads the root once and caches nested folders after first expand", async () => {
    vi.mocked(listEntries).mockImplementation(async (_server, _team, options) => ({
      entries:
        (options.path ?? "") === "www"
          ? [dir("src"), entry({ name: "index.ts" })]
          : [entry({ name: "main.ts" })],
      truncated: false,
      cursor: "",
    }));
    const { explorer } = setupStores();
    explorer.bind("server-1", "volumes", "www");
    await explorer.init();

    expect(vi.mocked(listEntries)).toHaveBeenCalledTimes(1);
    await explorer.toggle("www/src");
    expect(vi.mocked(listEntries)).toHaveBeenCalledWith("server-1", "team-1", {
      root: "volumes",
      path: "www/src",
    });
    expect(explorer.dirs["www/src"]?.entries).toHaveLength(1);

    explorer.collapse("www/src");
    await explorer.toggle("www/src");
    expect(vi.mocked(listEntries)).toHaveBeenCalledTimes(2);
  });

  it("keeps a failed folder inline and retries it", async () => {
    vi.mocked(listEntries)
      .mockRejectedValueOnce({ status: 502, message: "down" })
      .mockResolvedValueOnce({
        entries: [entry({ name: "ok.ts" })],
        truncated: false,
        cursor: "",
      });
    const { explorer } = setupStores();
    explorer.bind("server-1", "volumes", "www");

    await explorer.load("www/broken");
    expect(explorer.dirs["www/broken"]?.error).not.toBeNull();
    expect(explorer.dirs["www/broken"]?.loaded).toBe(false);

    await explorer.load("www/broken", true);
    expect(explorer.dirs["www/broken"]?.error).toBeNull();
    expect(explorer.dirs["www/broken"]?.entries[0]?.name).toBe("ok.ts");
  });

  it("searches recursively through the include-subdirectories option", async () => {
    vi.mocked(searchEntries).mockResolvedValue({
      results: [
        { path: "www/deep/app.ts", entry: entry({ name: "app.ts" }) },
      ],
      truncated: true,
    });
    const { explorer } = setupStores();
    explorer.bind("server-1", "volumes", "www");
    explorer.setSearchQuery("app");
    explorer.setSearchRecursive(true);
    await explorer.runSearch();

    expect(vi.mocked(searchEntries)).toHaveBeenCalledWith(
      "server-1",
      "team-1",
      expect.objectContaining({
        root: "volumes",
        path: "www",
        query: "app",
        recursive: true,
      }),
    );
    expect(explorer.searchHits).toHaveLength(1);
    expect(explorer.searchTruncated).toBe(true);

    explorer.clearSearch();
    expect(explorer.searchActive).toBe(false);
    expect(explorer.searchHits).toHaveLength(0);
  });

  it("renames an open clean file and rebases its tab", async () => {
    vi.mocked(renameEntry).mockResolvedValue(entry({ name: "b.ts" }));
    const { explorer, editor } = setupStores();
    explorer.bind("server-1", "volumes", "www");
    explorer.select("www/a.ts");
    editor.tabs = [makeTab("www/a.ts")];
    editor.activeId = editor.tabs[0]!.id;

    await explorer.rename("www/a.ts", "b.ts");

    expect(vi.mocked(renameEntry)).toHaveBeenCalledWith("server-1", "team-1", {
      root: "volumes",
      source: "www/a.ts",
      destination: "www/b.ts",
    });
    expect(editor.tabs[0]?.path).toBe("www/b.ts");
    expect(explorer.selectedPath).toBe("www/b.ts");
  });

  it("refuses to rename an entry with an unsaved tab", async () => {
    const { explorer, editor } = setupStores();
    explorer.bind("server-1", "volumes", "www");
    editor.tabs = [makeTab("www/a.ts", true)];
    editor.activeId = editor.tabs[0]!.id;

    await expect(explorer.rename("www/a.ts", "b.ts")).rejects.toBeInstanceOf(
      ExplorerDirtyError,
    );
    expect(vi.mocked(renameEntry)).not.toHaveBeenCalled();
  });

  it("deletes recursively and closes every tab under the folder", async () => {
    vi.mocked(listEntries).mockResolvedValue({
      entries: [dir("src")],
      truncated: false,
      cursor: "",
    });
    vi.mocked(deleteEntry).mockResolvedValue(undefined);
    const { explorer, editor } = setupStores();
    explorer.bind("server-1", "volumes", "www");
    await explorer.init();
    editor.tabs = [makeTab("www/src/a.ts"), makeTab("www/other.ts")];
    editor.activeId = editor.tabs[0]!.id;

    await explorer.remove("www/src");

    expect(vi.mocked(deleteEntry)).toHaveBeenCalledWith(
      "server-1",
      "team-1",
      "volumes",
      "www/src",
      true,
    );
    expect(editor.tabs.map((tab) => tab.path)).toEqual(["www/other.ts"]);
  });

  it("creates an entry and refreshes its folder", async () => {
    vi.mocked(createFolder).mockResolvedValue(dir("assets"));
    const { explorer } = setupStores();
    explorer.bind("server-1", "volumes", "www");
    await explorer.init();

    const target = await explorer.create("directory", "  assets  ", "www");

    expect(target).toBe("www/assets");
    expect(vi.mocked(createFolder)).toHaveBeenCalledWith("server-1", "team-1", {
      root: "volumes",
      path: "www/assets",
    });
    expect(explorer.selectedPath).toBe("www/assets");
  });
});

describe("EditorExplorer panel", () => {
  /** mountPanel mounts the panel under the message provider. */
  function mountPanel() {
    const stores = setupStores();
    const shell = defineComponent({
      render: () =>
        h(NMessageProvider, null, {
          default: () => h(EditorExplorer, { serverId: "server-1" }),
        }),
    });
    const wrapper = mount(shell, {
      attachTo: document.body,
      global: { plugins: [i18n], stubs: { teleport: true } },
    });
    return { ...stores, wrapper };
  }

  it("renders an ARIA tree with levels and expansion state", async () => {
    vi.mocked(listEntries).mockResolvedValue({
      entries: [dir("src"), entry({ name: "index.ts" })],
      truncated: false,
      cursor: "",
    });
    const { wrapper } = mountPanel();
    await flushPromises();
    await nextTick();

    const tree = wrapper.find('[role="tree"]');
    expect(tree.exists()).toBe(true);
    const items = wrapper.findAll('[role="treeitem"]');
    expect(items).toHaveLength(2);
    expect(items[0]?.attributes("aria-level")).toBe("1");
    expect(items[0]?.attributes("aria-expanded")).toBe("false");
    expect(items[1]?.attributes("aria-expanded")).toBeUndefined();
    expect(tree.attributes("aria-activedescendant")).toBe("ex-row-0");
    wrapper.unmount();
  });

  it("expands with ArrowRight, walks into the child and opens it with Enter", async () => {
    vi.mocked(listEntries).mockImplementation(async (_server, _team, options) => ({
      entries:
        (options.path ?? "") === "www"
          ? [dir("src"), entry({ name: "index.ts" })]
          : [entry({ name: "main.ts" })],
      truncated: false,
      cursor: "",
    }));
    const { wrapper, editor } = mountPanel();
    await flushPromises();
    const openFile = vi.spyOn(editor, "openFile").mockResolvedValue();

    const tree = wrapper.find('[role="tree"]');
    await tree.trigger("keydown", { key: "ArrowRight" });
    await flushPromises();
    await nextTick();
    expect(vi.mocked(listEntries)).toHaveBeenCalledWith("server-1", "team-1", {
      root: "volumes",
      path: "www/src",
    });
    expect(wrapper.findAll('[role="treeitem"]')).toHaveLength(3);

    await tree.trigger("keydown", { key: "ArrowDown" });
    expect(tree.attributes("aria-activedescendant")).toBe("ex-row-1");
    await tree.trigger("keydown", { key: "Enter" });
    await flushPromises();
    expect(openFile).toHaveBeenCalledWith("server-1", "team-1", "volumes", "www/src/main.ts");
    wrapper.unmount();
  });

  it("highlights the active file and marks other open files", async () => {
    vi.mocked(listEntries).mockResolvedValue({
      entries: [entry({ name: "a.ts" }), entry({ name: "b.ts" })],
      truncated: false,
      cursor: "",
    });
    const { wrapper, editor } = mountPanel();
    await flushPromises();
    editor.tabs = [makeTab("www/a.ts"), makeTab("www/b.ts")];
    editor.activeId = editor.tabs[0]!.id;
    await flushPromises();

    const active = wrapper.find('[data-path="www/a.ts"]');
    expect(active.classes()).toContain("is-open");
    expect(active.find(".ex-open-dot").exists()).toBe(true);
    wrapper.unmount();
  });

  it("windows a 1,000-entry folder instead of rendering every row", async () => {
    const entries = Array.from({ length: 1000 }, (_, index) =>
      entry({ name: `file-${String(index).padStart(4, "0")}.txt` }),
    );
    vi.mocked(listEntries).mockResolvedValue({
      entries,
      truncated: false,
      cursor: "",
    });
    const { wrapper } = mountPanel();
    await flushPromises();
    await nextTick();

    const rendered = wrapper.findAll('[role="treeitem"]');
    expect(rendered.length).toBeGreaterThan(0);
    expect(rendered.length).toBeLessThan(100);
    expect(wrapper.find('[role="tree"]').element.children.length).toBeLessThan(100);
    wrapper.unmount();
  });
});
