// Files browser store (P18 step 6): selection, sort, navigation, pagination
// and the disabled/permission states. The API module is mocked so the store is
// exercised without axios or the DOM.
import { createPinia, setActivePinia } from "pinia";
import { beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("@/features/files/api/files", async (importOriginal) => {
  const actual =
    await importOriginal<typeof import("@/features/files/api/files")>();
  return {
    ...actual,
    listRoots: vi.fn(),
    listEntries: vi.fn(),
  };
});

import {
  listEntries,
  listRoots,
} from "@/features/files/api/files";
import type { FilesEntry, FilesRoot } from "@/features/files/schemas/files";
import { useFilesStore } from "@/features/files/stores/files";
import { useTeamsStore } from "@/features/teams";

const root: FilesRoot = {
  name: "volumes",
  path: "/var/lib/gotham/volumes",
  writable: true,
  free_bytes: 21 * 1024 ** 3,
};

const readOnlyRoot: FilesRoot = {
  name: "compose",
  path: "/var/lib/gotham-agent/compose",
  writable: false,
  free_bytes: 0,
};

/** entry builds one listing row with sensible defaults. */
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
    entry({ name: "app.yaml", kind: "file", size: 2457, etag: '"a"' }),
    entry({ name: "data", kind: "directory", size: 0, mode: 0o040755 }),
    entry({ name: "config", kind: "directory", size: 0, mode: 0o040755 }),
    entry({ name: "logo.png", kind: "file", size: 25190, etag: '"b"' }),
  ],
  truncated: false,
  cursor: "",
};

/** deferred returns a promise with its resolve exposed. */
function deferred<T>() {
  let resolve!: (_value: T) => void;
  const promise = new Promise<T>((resolvePromise) => {
    resolve = resolvePromise;
  });
  return { promise, resolve };
}

beforeEach(() => {
  setActivePinia(createPinia());
  vi.mocked(listRoots).mockReset();
  vi.mocked(listEntries).mockReset();
  const teams = useTeamsStore();
  teams.activeTeamId = "team-1";
  // read_only is the safe default; tests that need content opt in explicitly.
  teams.teams = [];
});

describe("files store loading", () => {
  it("loads roots and the first directory on init", async () => {
    vi.mocked(listRoots).mockResolvedValue([root, readOnlyRoot]);
    vi.mocked(listEntries).mockResolvedValue(listing);

    const store = useFilesStore();
    await store.init("server-1");

    expect(store.roots).toHaveLength(2);
    expect(store.rootName).toBe("volumes");
    expect(store.entries).toHaveLength(4);
    expect(store.loaded).toBe(true);
    expect(store.loading).toBe(false);
    expect(vi.mocked(listEntries)).toHaveBeenCalledWith("server-1", "team-1", {
      root: "volumes",
      path: "",
    });
  });

  it("surfaces a read_only member and hides content affordances", async () => {
    vi.mocked(listRoots).mockResolvedValue([root]);
    vi.mocked(listEntries).mockResolvedValue(listing);

    const teams = useTeamsStore();
    teams.teams = [{ id: "team-1", name: "A", is_personal: true, role: "read_only" }];

    const store = useFilesStore();
    await store.init("server-1");

    expect(store.canReadContent).toBe(false);
  });

  it("enables content affordances for an owner", async () => {
    vi.mocked(listRoots).mockResolvedValue([root]);
    vi.mocked(listEntries).mockResolvedValue(listing);

    const teams = useTeamsStore();
    teams.teams = [{ id: "team-1", name: "A", is_personal: true, role: "owner" }];

    const store = useFilesStore();
    await store.init("server-1");

    expect(store.canReadContent).toBe(true);
  });

  it("flags a disabled feature (503) instead of an error", async () => {
    const disabled = { message: "files are disabled", status: 503 };
    vi.mocked(listRoots).mockRejectedValue(disabled);
    vi.mocked(listEntries).mockResolvedValue(listing);

    const store = useFilesStore();
    await store.init("server-1");

    expect(store.disabled).toBe(true);
    expect(store.error).toBeNull();
  });
});

describe("files store navigation", () => {
  beforeEach(() => {
    vi.mocked(listRoots).mockResolvedValue([root, readOnlyRoot]);
    vi.mocked(listEntries).mockResolvedValue(listing);
  });

  it("descends, builds breadcrumbs and goes back up", async () => {
    const store = useFilesStore();
    await store.init("server-1");

    await store.openDirectory("config");
    expect(store.path).toBe("config");
    expect(store.breadcrumb.map((crumb) => crumb.name)).toEqual([
      "volumes",
      "config",
    ]);
    expect(store.breadcrumb.at(-1)).toMatchObject({
      path: "config",
      isRoot: false,
    });

    await store.openDirectory("app");
    expect(store.path).toBe("config/app");

    await store.goUp();
    expect(store.path).toBe("config");

    await store.goToPath("");
    expect(store.path).toBe("");
  });

  it("switches the root and returns to its top level", async () => {
    const store = useFilesStore();
    await store.init("server-1");
    await store.openDirectory("config");

    await store.setRoot("compose");
    expect(store.rootName).toBe("compose");
    expect(store.path).toBe("");
    expect(store.writable).toBe(false);
  });

  it("drops a listing that lost to a newer navigation", async () => {
    const store = useFilesStore();
    await store.init("server-1");

    const slow = deferred<typeof listing>();
    const fast = deferred<typeof listing>();
    vi.mocked(listEntries)
      .mockReturnValueOnce(slow.promise)
      .mockReturnValueOnce(fast.promise);

    const first = store.openDirectory("config");
    const second = store.openDirectory("data");

    fast.resolve({
      entries: [entry({ name: "keep.txt" })],
      truncated: false,
      cursor: "",
    });
    await second;
    slow.resolve(listing);
    await first;

    expect(store.entries.map((item) => item.name)).toEqual(["keep.txt"]);
  });

  it("resets the browser on sign-out", async () => {
    const store = useFilesStore();
    await store.init("server-1");
    store.toggleSelection("app.yaml");

    store.reset();

    expect(store.serverId).toBe("");
    expect(store.entries).toHaveLength(0);
    expect(store.selectedNames).toEqual([]);
    expect(store.loaded).toBe(false);
  });
});

describe("files store sort and selection", () => {
  beforeEach(async () => {
    vi.mocked(listRoots).mockResolvedValue([root]);
    vi.mocked(listEntries).mockResolvedValue(listing);
  });

  it("leads with directories, then name ascending", async () => {
    const store = useFilesStore();
    await store.init("server-1");

    expect(store.visibleEntries.map((item) => item.name)).toEqual([
      "config",
      "data",
      "app.yaml",
      "logo.png",
    ]);
  });

  it("toggles direction on the active column and resets on a new one", async () => {
    const store = useFilesStore();
    await store.init("server-1");

    store.setSort("name");
    expect(store.sortDir).toBe("desc");
    expect(store.visibleEntries.map((item) => item.name)).toEqual([
      "data",
      "config",
      "logo.png",
      "app.yaml",
    ]);

    store.setSort("size");
    expect(store.sortKey).toBe("size");
    expect(store.sortDir).toBe("asc");
  });

  it("selects only selectable rows and totals their bytes", async () => {
    const store = useFilesStore();
    await store.init("server-1");

    store.toggleSelectAll();
    expect(store.selectedNames.sort()).toEqual([
      "app.yaml",
      "config",
      "data",
      "logo.png",
    ]);
    expect(store.selectedBytes).toBe(2457 + 25190);
    expect(store.allVisibleSelected).toBe(true);

    store.toggleSelectAll();
    expect(store.selectedNames).toEqual([]);
  });

  it("filters the visible entries by name", async () => {
    const store = useFilesStore();
    await store.init("server-1");

    store.setQuery("conf");
    expect(store.visibleEntries.map((item) => item.name)).toEqual(["config"]);
  });
});

describe("files store pagination", () => {
  it("appends the next cursor page", async () => {
    vi.mocked(listRoots).mockResolvedValue([root]);
    vi.mocked(listEntries)
      .mockResolvedValueOnce({
        entries: [entry({ name: "a.txt" })],
        truncated: true,
        cursor: "page-2",
      })
      .mockResolvedValueOnce({
        entries: [entry({ name: "b.txt" })],
        truncated: false,
        cursor: "",
      });

    const store = useFilesStore();
    await store.init("server-1");
    expect(store.hasMore).toBe(true);

    await store.loadMore();

    expect(store.entries.map((item) => item.name)).toEqual(["a.txt", "b.txt"]);
    expect(store.hasMore).toBe(false);
    expect(vi.mocked(listEntries)).toHaveBeenLastCalledWith(
      "server-1",
      "team-1",
      { root: "volumes", path: "", cursor: "page-2" },
    );
  });
});
