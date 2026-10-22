// Files browser tabs (JUS-123): each tab keeps its own root, path, listing,
// selection, sort/filter and scroll offset; the clipboard is shared across tabs
// (cut in one tab, paste in another); switching node resets to a single default
// tab. The API module is mocked so the store is exercised without axios.
import { createPinia, setActivePinia } from "pinia";
import { beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("@/features/files/api/files", async (importOriginal) => {
  const actual =
    await importOriginal<typeof import("@/features/files/api/files")>();
  return {
    ...actual,
    listRoots: vi.fn(),
    listEntries: vi.fn(),
    moveEntry: vi.fn(),
  };
});

import { listEntries, listRoots, moveEntry } from "@/features/files/api/files";
import type { FilesEntry, FilesRoot } from "@/features/files/schemas/files";
import { useFilesStore } from "@/features/files/stores/files";
import { disambiguatedTabLabels } from "@/features/files/utils/files";
import { useTeamsStore } from "@/features/teams";

const volumes: FilesRoot = {
  name: "volumes",
  path: "/var/lib/gotham/volumes",
  writable: true,
  free_bytes: 21 * 1024 ** 3,
};

const compose: FilesRoot = {
  name: "compose",
  path: "/var/lib/gotham-agent/compose",
  writable: false,
  free_bytes: 0,
};

/** entry builds one listing row with sensible defaults. */
function entry(overrides: Partial<FilesEntry> & { name: string }): FilesEntry {
  return {
    kind: "file",
    size: 1024,
    mode: 0o100644,
    modified_at: "2026-10-12T09:02:00Z",
    etag: '"1"',
    ...overrides,
  };
}

/** listingFor returns one distinguishable row per (root, path) directory. */
function listingFor(root: string, path: string) {
  return {
    entries: [entry({ name: `${root}:${path === "" ? "/" : path}` })],
    truncated: false,
    cursor: "",
  };
}

/** deferred returns a promise with its resolve exposed, for race tests. */
function deferred<T>() {
  let resolve!: (_value: T) => void;
  const promise = new Promise<T>((resolvePromise) => {
    resolve = resolvePromise;
  });
  return { promise, resolve };
}

type Listing = { entries: FilesEntry[]; truncated: boolean; cursor: string };

beforeEach(() => {
  setActivePinia(createPinia());
  vi.mocked(listRoots).mockReset();
  vi.mocked(listEntries).mockReset();
  vi.mocked(moveEntry).mockReset();

  vi.mocked(listRoots).mockResolvedValue([volumes, compose]);
  vi.mocked(listEntries).mockImplementation(
    async (_serverId, _teamId, options) =>
      listingFor(options.root, options.path ?? ""),
  );
  vi.mocked(moveEntry).mockResolvedValue(entry({ name: "moved" }));

  const teams = useTeamsStore();
  teams.activeTeamId = "team-1";
  teams.teams = [
    { id: "team-1", name: "A", is_personal: true, role: "owner" },
  ];
});

describe("files browser deep links", () => {
  it("opens the linked directory in a new tab and reuses it on repeat", async () => {
    const store = useFilesStore();
    await store.init("server-1");
    const previousId = store.tabs[0].id;

    // The deep link opens its own tab and leaves the open one untouched.
    await store.openAt("volumes", "app-1/data");
    expect(store.tabs).toHaveLength(2);
    expect(store.tabs[0].id).toBe(previousId);
    expect(store.tabs[0].path).toBe("");
    const linkedId = store.activeTabId;
    expect(linkedId).not.toBe(previousId);
    expect(store.activeTab.path).toBe("app-1/data");

    await store.openTab({ root: "compose" });
    expect(store.tabs).toHaveLength(3);

    // Re-opening the same link focuses the existing tab instead of stacking
    // another one.
    await store.openAt("volumes", "app-1/data");
    expect(store.tabs).toHaveLength(3);
    expect(store.activeTabId).toBe(linkedId);
    expect(store.activeTab.path).toBe("app-1/data");
  });

  it("records the server its roots belong to", async () => {
    const store = useFilesStore();
    await store.init("server-1");
    expect(store.rootsServerId).toBe("server-1");

    await store.init("server-2");
    expect(store.rootsServerId).toBe("server-2");
    expect(store.tabs).toHaveLength(1);
  });
});

describe("files browser tabs", () => {
  it("starts with one tab and opens a focused second tab", async () => {
    const store = useFilesStore();
    await store.init("server-1");

    expect(store.tabs).toHaveLength(1);
    expect(store.activeTabId).toBe(store.tabs[0].id);
    expect(store.entries[0]?.name).toBe("volumes:/");

    await store.openTab();

    expect(store.tabs).toHaveLength(2);
    expect(store.activeTabId).toBe(store.tabs[1].id);
    expect(store.tabs[0].id).not.toBe(store.tabs[1].id);
    expect(store.activeTab.rootName).toBe("volumes");
    expect(store.activeTab.path).toBe("");
  });

  it("keeps path, root, selection, sort and scroll independent per tab", async () => {
    const store = useFilesStore();
    await store.init("server-1");

    // Tab A: descend, sort, select and scroll.
    await store.openDirectory("data");
    store.setSort("size");
    store.toggleSelection(store.entries[0].name);
    store.setScrollTop(120);
    const tabA = store.activeTabId;

    // Tab B: a fresh root-level tab with its own root and filter.
    await store.openTab();
    expect(store.path).toBe("");
    store.setQuery("volumes");
    await store.setRoot("compose");
    expect(store.rootName).toBe("compose");

    // Back to A: every bit of its state is intact.
    await store.switchTab(tabA);
    expect(store.path).toBe("data");
    expect(store.rootName).toBe("volumes");
    expect(store.sortKey).toBe("size");
    expect(store.query).toBe("");
    expect(store.selectedNames).toHaveLength(1);
    expect(store.scrollTop).toBe(120);
    expect(store.entries[0]?.name).toBe("volumes:data");
  });

  it("keeps each tab's own folder filter across a switch", async () => {
    const store = useFilesStore();
    await store.init("server-1");

    // Tab A filters to "app" (JUS-141: a tab switch is not a filter reset).
    store.setQuery("app");
    const tabA = store.activeTabId;

    // Tab B carries its own needle.
    await store.openTab();
    const tabB = store.activeTabId;
    store.setQuery("data");
    expect(store.query).toBe("data");

    // Switching back and forth restores each tab's own filter.
    await store.switchTab(tabA);
    expect(store.query).toBe("app");
    await store.switchTab(tabB);
    expect(store.query).toBe("data");
  });

  it("closes a tab onto a neighbour and keeps one default tab at the end", async () => {
    const store = useFilesStore();
    await store.init("server-1");
    await store.openTab();
    const [a, b] = store.tabs;
    expect(store.activeTabId).toBe(b.id);

    await store.closeTab(b.id);
    expect(store.tabs).toHaveLength(1);
    expect(store.activeTabId).toBe(a.id);

    // Closing the last tab leaves a single default tab, never an empty browser.
    await store.closeTab(a.id);
    expect(store.tabs).toHaveLength(1);
    expect(store.activeTabId).toBe(store.tabs[0].id);
    expect(store.activeTab.rootName).toBe("volumes");
    expect(store.loaded).toBe(true);
  });

  it("cycles tabs with next/prev and wraps around", async () => {
    const store = useFilesStore();
    await store.init("server-1");
    await store.openTab();
    const [a, b] = store.tabs;
    expect(store.activeTabId).toBe(b.id);

    await store.nextTab();
    expect(store.activeTabId).toBe(a.id);
    await store.prevTab();
    expect(store.activeTabId).toBe(b.id);
  });

  it("shares the clipboard across tabs so a cut pastes in another tab", async () => {
    const store = useFilesStore();
    await store.init("server-1");
    await store.openDirectory("data");
    const tabA = store.activeTabId;
    await store.openTab();
    const tabB = store.activeTabId;

    // Cut in A.
    await store.switchTab(tabA);
    const name = store.entries[0].name;
    store.cutSelection([name]);
    expect(store.clipboardMode).toBe("cut");

    // The clipboard survives the switch and lands in B's directory.
    await store.switchTab(tabB);
    expect(store.hasClipboard).toBe(true);
    expect(store.pasteProblem).toBeNull();
    await store.paste();

    expect(vi.mocked(moveEntry)).toHaveBeenCalledWith("server-1", "team-1", {
      root: "volumes",
      source: `data/${name}`,
      destination: name,
      overwrite: false,
    });
    // A completed cut clears the clipboard for the moved name.
    expect(store.hasClipboard).toBe(false);
  });

  it("resets to a single default tab when the node (server) changes", async () => {
    const store = useFilesStore();
    await store.init("server-1");
    await store.openTab();
    await store.openTab();
    expect(store.tabs).toHaveLength(3);

    await store.init("server-2");

    expect(store.serverId).toBe("server-2");
    expect(store.tabs).toHaveLength(1);
    expect(store.activeTabId).toBe(store.tabs[0].id);
    expect(store.entries[0]?.name).toBe("volumes:/");
  });
});

describe("files browser tab races", () => {
  it("drops a stale listRoots when the node changed while it was loading", async () => {
    const store = useFilesStore();
    const slow = deferred<FilesRoot[]>();
    vi.mocked(listRoots)
      .mockReset()
      .mockReturnValueOnce(slow.promise) // node-1 roots arrive late
      .mockResolvedValue([compose]); // node-2 (and later) arrive first
    vi.mocked(listEntries).mockResolvedValue({
      entries: [],
      truncated: false,
      cursor: "",
    });

    const first = store.init("server-1");
    await store.init("server-2");
    expect(store.serverId).toBe("server-2");
    expect(store.roots.map((root) => root.name)).toEqual(["compose"]);

    // The node-1 response lands after the switch and must NOT replace the shared
    // roots of the node now on screen (regression vs the pre-tab store).
    slow.resolve([volumes]);
    await first;
    expect(store.roots.map((root) => root.name)).toEqual(["compose"]);
  });

  it("drops a stale listing when the team switched while it was loading", async () => {
    const store = useFilesStore();
    await store.init("server-1");

    const slow = deferred<Listing>();
    vi.mocked(listEntries).mockReturnValueOnce(slow.promise);
    const inflight = store.openDirectory("data");

    await store.reload(); // resetBrowser bumps the discarded tab's generation
    slow.resolve({
      entries: [entry({ name: "late.txt" })],
      truncated: false,
      cursor: "",
    });
    await inflight;

    expect(store.tabs).toHaveLength(1);
    expect(store.path).toBe("");
    expect(store.entries.map((item) => item.name)).toEqual(["volumes:/"]);
  });

  it("applies a slow listing to its own tab after a switch, never another tab", async () => {
    const store = useFilesStore();
    await store.init("server-1");
    await store.openTab();
    const [a, b] = store.tabs;

    const slow = deferred<Listing>();
    vi.mocked(listEntries).mockReturnValueOnce(slow.promise);
    const inflight = store.openDirectory("data"); // in B
    expect(store.path).toBe("data");

    await store.switchTab(a.id);
    slow.resolve({
      entries: [entry({ name: "b-data.txt" })],
      truncated: false,
      cursor: "",
    });
    await inflight;

    // The landed listing belongs to B; the tab switched to is untouched.
    expect(store.activeTabId).toBe(a.id);
    expect(store.entries.map((item) => item.name)).toEqual(["volumes:/"]);
    expect(b.path).toBe("data");
    expect(b.entries.map((item) => item.name)).toEqual(["b-data.txt"]);
  });

  it("does not write anywhere when a tab is closed while it is loading", async () => {
    const store = useFilesStore();
    await store.init("server-1");
    await store.openTab();
    const [a, b] = store.tabs;

    const slow = deferred<Listing>();
    vi.mocked(listEntries).mockReturnValueOnce(slow.promise);
    const inflight = store.openDirectory("data"); // in B

    await store.closeTab(b.id);
    expect(store.activeTabId).toBe(a.id);

    slow.resolve({
      entries: [entry({ name: "late.txt" })],
      truncated: false,
      cursor: "",
    });
    await inflight; // must neither throw nor touch tab A

    expect(store.tabs).toHaveLength(1);
    expect(store.entries.map((item) => item.name)).toEqual(["volumes:/"]);
  });

  it("drops a loadMore page that lost to a navigation in the same tab", async () => {
    const store = useFilesStore();
    vi.mocked(listEntries)
      .mockResolvedValueOnce({
        entries: [entry({ name: "page1" })],
        truncated: true,
        cursor: "c1",
      })
      .mockResolvedValue({
        entries: [entry({ name: "page2" })],
        truncated: false,
        cursor: "",
      });
    await store.init("server-1");
    expect(store.hasMore).toBe(true);

    const slow = deferred<Listing>();
    vi.mocked(listEntries).mockReturnValueOnce(slow.promise);
    const more = store.loadMore();
    const nav = store.openDirectory("data");
    slow.resolve({
      entries: [entry({ name: "late" })],
      truncated: false,
      cursor: "",
    });
    await more;
    await nav;

    expect(store.path).toBe("data");
    expect(store.entries.map((item) => item.name)).toEqual(["page2"]);
  });

  it("resolves a conflict prompt against the tab that started the transfer", async () => {
    const store = useFilesStore();
    vi.mocked(listEntries).mockImplementation(async (_serverId, _teamId, options) =>
      options.path === "data"
        ? {
            entries: [
              entry({ name: "a.txt" }),
              entry({ name: "dest", kind: "directory", mode: 0o040755 }),
            ],
            truncated: false,
            cursor: "",
          }
        : {
            entries: [entry({ name: "data", kind: "directory", mode: 0o040755 })],
            truncated: false,
            cursor: "",
          },
    );
    vi.mocked(moveEntry)
      .mockReset()
      .mockRejectedValueOnce({ message: "conflict", status: 409 })
      .mockResolvedValue(entry({ name: "a.txt" }));

    await store.init("server-1");
    await store.openDirectory("data");
    const tabA = store.activeTabId;
    store.cutSelection(["a.txt"]);

    const first = await store.pasteInto("data/dest");
    expect(first.conflicts).toEqual(["a.txt"]);
    expect(store.conflictPrompt?.tabId).toBe(tabA);
    expect(store.conflictPrompt?.root).toBe("volumes");

    // Switch tabs, then resolve: the retry must still target tab A's directory.
    await store.openTab();
    expect(store.path).toBe("");
    const summary = await store.resolveConflictPrompt(true);

    expect(summary.done).toEqual(["a.txt"]);
    expect(vi.mocked(moveEntry)).toHaveBeenLastCalledWith("server-1", "team-1", {
      root: "volumes",
      source: "data/a.txt",
      destination: "data/dest/a.txt",
      overwrite: true,
    });
    expect(store.activeTab.rootName).toBe("volumes");
    expect(store.path).toBe("");
  });

  it("refreshes the source tab after a cross-tab cut paste", async () => {
    const store = useFilesStore();
    await store.init("server-1");
    await store.openDirectory("data");
    const tabA = store.activeTabId;
    await store.openTab();
    const tabB = store.activeTabId;

    await store.switchTab(tabA);
    const name = store.entries[0].name;
    store.cutSelection([name]);

    await store.switchTab(tabB);
    vi.mocked(listEntries).mockClear();
    await store.paste();

    // Both the destination tab (B, root "") and the source tab (A, "data") were
    // reloaded, so neither keeps a stale listing after the move.
    const paths = vi
      .mocked(listEntries)
      .mock.calls.map(([, , options]) => options.path);
    expect(paths).toContain("");
    expect(paths).toContain("data");
  });
});

describe("disambiguatedTabLabels", () => {
  it("uses each tab's basename when the labels are already unique", () => {
    const labels = disambiguatedTabLabels([
      { id: "a", rootName: "volumes", path: "" },
      { id: "b", rootName: "volumes", path: "alpha" },
    ]);
    expect(labels.get("a")).toBe("volumes");
    expect(labels.get("b")).toBe("alpha");
  });

  it("prepends parent segments until the labels differ", () => {
    const labels = disambiguatedTabLabels([
      { id: "a", rootName: "r", path: "a/x/config" },
      { id: "b", rootName: "r", path: "b/x/config" },
    ]);
    expect(labels.get("a")).toBe("a/x/config");
    expect(labels.get("b")).toBe("b/x/config");
  });

  it("prepends only as far as needed and falls back to the root", () => {
    const shared = disambiguatedTabLabels([
      { id: "a", rootName: "r", path: "x/config" },
      { id: "b", rootName: "r", path: "other/config" },
    ]);
    expect(shared.get("a")).toBe("x/config");
    expect(shared.get("b")).toBe("other/config");

    const acrossRoots = disambiguatedTabLabels([
      { id: "a", rootName: "volumes", path: "config" },
      { id: "b", rootName: "compose", path: "config" },
    ]);
    expect(acrossRoots.get("a")).toBe("volumes/config");
    expect(acrossRoots.get("b")).toBe("compose/config");
  });
});
