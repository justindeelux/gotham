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
