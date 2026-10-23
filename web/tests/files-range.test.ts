import { createPinia, setActivePinia } from "pinia";
import { beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("@/features/files/api/files", async (importOriginal) => {
  const actual =
    await importOriginal<typeof import("@/features/files/api/files")>();
  return {
    ...actual,
    listRoots: vi.fn(),
    listEntries: vi.fn(),
    renameEntry: vi.fn(),
  };
});

import {
  listEntries,
  listRoots,
  renameEntry,
} from "@/features/files/api/files";
import type { FilesEntry, FilesRoot } from "@/features/files/schemas/files";
import { useFilesStore } from "@/features/files/stores/files";
import { rangeNames } from "@/features/files/utils/files";
import { useTeamsStore } from "@/features/teams";

const root: FilesRoot = {
  name: "volumes",
  path: "/var/lib/gotham/volumes",
  writable: true,
  free_bytes: 1024 ** 3,
};

function entry(
  overrides: Partial<FilesEntry> & { name: string },
): FilesEntry {
  return {
    kind: "file",
    size: 1024,
    mode: 0o100644,
    modified_at: "2026-10-12T09:02:00Z",
    etag: '"1"',
    ...overrides,
  };
}

const listing = {
  entries: [
    entry({ name: "alpha", kind: "directory", size: 0, mode: 0o040755 }),
    entry({ name: "beta", kind: "directory", size: 0, mode: 0o040755 }),
    entry({ name: "charlie", size: 300 }),
    entry({ name: "delta", size: 100 }),
    entry({ name: "echo", size: 200 }),
    entry({ name: "link", kind: "symlink", symlink_target: "alpha" }),
    entry({ name: "sock", kind: "special", mode: 0o140755 }),
  ],
  truncated: false,
  cursor: "",
};

beforeEach(() => {
  setActivePinia(createPinia());
  vi.mocked(listRoots).mockReset();
  vi.mocked(listEntries).mockReset();
  vi.mocked(renameEntry).mockReset();
  const teams = useTeamsStore();
  teams.activeTeamId = "team-1";
  teams.teams = [];
});

describe("rangeNames", () => {
  const ordered = ["a", "b", "c", "d", "e", "f"].map((name) =>
    entry({ name }),
  );

  it("returns the inclusive run in listing order, whichever end is the anchor", () => {
    expect(rangeNames(ordered, "b", "e")).toEqual(["b", "c", "d", "e"]);
    expect(rangeNames(ordered, "e", "b")).toEqual(["b", "c", "d", "e"]);
  });

  it("returns just the clicked row when anchor and target agree", () => {
    expect(rangeNames(ordered, "c", "c")).toEqual(["c"]);
  });

  it("returns nothing when the anchor is absent", () => {
    expect(rangeNames(ordered, "gone", "c")).toEqual([]);
  });

  it("skips entries that carry no checkbox (symlinks, special files)", () => {
    const mixed = [
      entry({ name: "one" }),
      entry({ name: "link", kind: "symlink", symlink_target: "one" }),
      entry({ name: "two" }),
      entry({ name: "sock", kind: "special", mode: 0o140755 }),
      entry({ name: "three" }),
    ];
    expect(rangeNames(mixed, "one", "three")).toEqual(["one", "two", "three"]);
  });

  it("spans rows a windowed list did not render", () => {
    const many = Array.from({ length: 5000 }, (_, index) =>
      entry({ name: `f${String(index).padStart(4, "0")}` }),
    );
    const range = rangeNames(many, "f0010", "f4990");
    expect(range).toHaveLength(4981);
    expect(range[0]).toBe("f0010");
    expect(range[range.length - 1]).toBe("f4990");
  });
});

describe("files store range selection", () => {
  beforeEach(async () => {
    vi.mocked(listRoots).mockResolvedValue([root]);
    vi.mocked(listEntries).mockResolvedValue(listing);
  });

  it("shift-click selects the contiguous range in sorted+filtered order", async () => {
    const store = useFilesStore();
    await store.init("server-1");

    store.selectOnly("beta");
    store.selectRange("echo", false);

    expect(store.selectedNames.sort()).toEqual([
      "beta",
      "charlie",
      "delta",
      "echo",
    ]);
  });

  it("ctrl/cmd+shift adds the range to the existing selection", async () => {
    const store = useFilesStore();
    await store.init("server-1");

    store.selectOnly("echo");
    store.selectRange("alpha", true);

    expect(store.selectedNames.sort()).toEqual([
      "alpha",
      "beta",
      "charlie",
      "delta",
      "echo",
    ]);
  });

  it("keeps the anchor so repeated shift-clicks grow and shrink one run", async () => {
    const store = useFilesStore();
    await store.init("server-1");

    store.selectOnly("alpha");
    store.selectRange("charlie", false);
    expect(store.selectedNames).toHaveLength(3);

    store.selectRange("beta", false);
    expect(store.selectedNames.sort()).toEqual(["alpha", "beta"]);

    store.selectRange("echo", false);
    expect(store.selectedNames.sort()).toEqual([
      "alpha",
      "beta",
      "charlie",
      "delta",
      "echo",
    ]);
  });

  it("selects just the clicked row when a shift-click has no anchor", async () => {
    const store = useFilesStore();
    await store.init("server-1");

    store.selectRange("delta", false);

    expect(store.selectedNames).toEqual(["delta"]);
    store.selectRange("echo", false);
    expect(store.selectedNames.sort()).toEqual(["delta", "echo"]);
  });

  it("follows the sorted order after a sort change", async () => {
    const store = useFilesStore();
    await store.init("server-1");

    store.setSort("size");
    expect(store.visibleEntries.map((item) => item.name)).toEqual([
      "alpha",
      "beta",
      "delta",
      "echo",
      "charlie",
      "link",
      "sock",
    ]);

    store.selectOnly("alpha");
    store.selectRange("echo", false);
    expect(store.selectedNames.sort()).toEqual([
      "alpha",
      "beta",
      "delta",
      "echo",
    ]);
  });

  it("moves the anchor on a plain click and a ctrl-click", async () => {
    const store = useFilesStore();
    await store.init("server-1");

    store.selectOnly("alpha");
    expect(store.activeTab.anchor).toBe("alpha");

    store.toggleSelection("echo");
    expect(store.activeTab.anchor).toBe("echo");
  });

  it("resets the anchor on a filter change", async () => {
    const store = useFilesStore();
    await store.init("server-1");

    store.selectOnly("alpha");
    store.setQuery("a");

    expect(store.activeTab.anchor).toBeNull();
  });

  it("resets the anchor on a sort change", async () => {
    const store = useFilesStore();
    await store.init("server-1");

    store.selectOnly("alpha");
    store.setSort("size");

    expect(store.activeTab.anchor).toBeNull();
  });

  it("resets the anchor on a folder change", async () => {
    const store = useFilesStore();
    await store.init("server-1");

    store.selectOnly("alpha");
    await store.openDirectory("beta");

    expect(store.activeTab.anchor).toBeNull();
  });

  it("resets the anchor on a tab switch", async () => {
    const store = useFilesStore();
    await store.init("server-1");
    const first = store.activeTabId;

    store.selectOnly("alpha");
    await store.openTab();
    await store.switchTab(first);

    expect(store.activeTab.anchor).toBeNull();
  });

  it("ends the range when the selection is cleared", async () => {
    const store = useFilesStore();
    await store.init("server-1");

    store.selectOnly("alpha");
    store.clearSelection();

    expect(store.activeTab.anchor).toBeNull();
  });

  it("moves the anchor on a right-click selection so the next range follows", async () => {
    const store = useFilesStore();
    await store.init("server-1");

    store.selectOnly("echo");
    const alpha = store.entries.find((item) => item.name === "alpha")!;
    store.selectForRowContext(alpha);

    expect(store.selectedNames).toEqual(["alpha"]);
    expect(store.activeTab.anchor).toBe("alpha");

    store.selectRange("charlie", false);
    expect(store.selectedNames.sort()).toEqual(["alpha", "beta", "charlie"]);
  });

  it("keeps the anchor on a right-click inside an existing selection", async () => {
    const store = useFilesStore();
    await store.init("server-1");

    store.selectOnly("alpha");
    store.selectRange("echo", false);
    const charlie = store.entries.find((item) => item.name === "charlie")!;
    store.selectForRowContext(charlie);

    expect(store.selectedNames).toHaveLength(5);
    expect(store.activeTab.anchor).toBe("alpha");
  });

  it("seeds the anchor on select-all so a shift-click extends the run", async () => {
    const store = useFilesStore();
    await store.init("server-1");

    store.toggleSelectAll();
    expect(store.selectedNames).toHaveLength(5);
    expect(store.activeTab.anchor).toBe("alpha");

    store.selectRange("beta", false);
    expect(store.selectedNames.sort()).toEqual(["alpha", "beta"]);
  });

  it("drops a dangling anchor when a refresh removes the entry", async () => {
    const store = useFilesStore();
    await store.init("server-1");

    store.selectOnly("echo");
    vi.mocked(listEntries).mockResolvedValue({
      entries: [entry({ name: "alpha", kind: "directory", size: 0, mode: 0o040755 })],
      truncated: false,
      cursor: "",
    });

    await store.refresh();

    expect(store.activeTab.anchor).toBeNull();
  });

  it("carries the anchor across a rename", async () => {
    const store = useFilesStore();
    await store.init("server-1");

    const charlie = store.entries.find((item) => item.name === "charlie")!;
    store.selectOnly("charlie");
    vi.mocked(renameEntry).mockResolvedValue(entry({ name: "charlie2" }));
    vi.mocked(listEntries).mockResolvedValue({
      entries: [
        ...listing.entries.filter((item) => item.name !== "charlie"),
        entry({ name: "charlie2" }),
      ],
      truncated: false,
      cursor: "",
    });

    await store.rename(charlie, "charlie2");

    expect(store.activeTab.anchor).toBe("charlie2");
  });

  it("resets the anchor when goToPath re-clears the current folder's filter", async () => {
    const store = useFilesStore();
    await store.init("server-1");

    store.selectOnly("alpha");
    await store.goToPath(store.path);

    expect(store.activeTab.anchor).toBeNull();
  });

  it("refuses a non-selectable range target", async () => {
    const store = useFilesStore();
    await store.init("server-1");

    store.selectOnly("alpha");
    store.selectRange("link", false);

    expect(store.selectedNames).toEqual(["alpha"]);
  });
});
