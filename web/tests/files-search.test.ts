// Files browser name search (JUS-163): the nav-row input's two modes. With
// "Include subdirectories" off it is the client-side folder filter; with it on,
// runSearch runs a bounded server-side search. The API module is mocked so the
// store is exercised without axios or the DOM.
import { createPinia, setActivePinia } from "pinia";
import { beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("@/features/files/api/files", async (importOriginal) => {
  const actual =
    await importOriginal<typeof import("@/features/files/api/files")>();
  return {
    ...actual,
    listRoots: vi.fn(),
    listEntries: vi.fn(),
    searchEntries: vi.fn(),
  };
});

import {
  listEntries,
  listRoots,
  searchEntries,
} from "@/features/files/api/files";
import type {
  FilesEntry,
  FilesListing,
  FilesRoot,
  FilesSearchHit,
  FilesSearchListing,
} from "@/features/files/schemas/files";
import { useFilesStore } from "@/features/files/stores/files";
import { useTeamsStore } from "@/features/teams";

const root: FilesRoot = {
  name: "volumes",
  path: "/var/lib/gotham/volumes",
  writable: true,
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
    entry({ name: "app.yaml", kind: "file" }),
    entry({ name: "config", kind: "directory", mode: 0o040755 }),
    entry({ name: "logo.png", kind: "file" }),
  ],
  truncated: false,
  cursor: "",
};

/** seedStore loads a store bound to one server with a fixture listing. */
async function seedStore() {
  vi.mocked(listRoots).mockResolvedValue([root]);
  vi.mocked(listEntries).mockResolvedValue(listing);
  const store = useFilesStore();
  await store.init("server-1");
  return store;
}

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
  vi.mocked(searchEntries).mockReset();
  const teams = useTeamsStore();
  teams.activeTeamId = "team-1";
  teams.teams = [];
});

describe("files search", () => {
  it("filters the current folder client-side when subdirectories are off", async () => {
    const store = await seedStore();
    store.setQuery("conf");
    expect(store.recursive).toBe(false);
    expect(store.visibleEntries.map((item) => item.name)).toEqual(["config"]);
    // With the switch off, submitting is not a server round trip.
    await store.runSearch();
    expect(searchEntries).not.toHaveBeenCalled();
  });

  it("runs a bounded server-side search when subdirectories are on", async () => {
    const store = await seedStore();
    store.setRecursive(true);
    store.setQuery("conf");
    // With the switch on the plain listing shows until a search runs.
    expect(store.visibleEntries.map((item) => item.name)).toEqual([
      "config",
      "app.yaml",
      "logo.png",
    ]);
    const hit: FilesSearchHit = {
      path: "config/app.yaml",
      entry: entry({ name: "app.yaml" }),
    };
    vi.mocked(searchEntries).mockResolvedValue({
      results: [hit],
      truncated: true,
    });

    await store.runSearch();

    expect(searchEntries).toHaveBeenCalledWith("server-1", "team-1", {
      root: "volumes",
      path: "",
      query: "conf",
      recursive: true,
      signal: expect.any(globalThis.AbortSignal),
    });
    expect(store.searchActive).toBe(true);
    expect(store.searchQuery).toBe("conf");
    expect(store.searchResults).toEqual([hit]);
    expect(store.searchTruncated).toBe(true);
    expect(store.searchLoading).toBe(false);
  });

  it("clears the search and its results", async () => {
    const store = await seedStore();
    store.setRecursive(true);
    store.setQuery("conf");
    vi.mocked(searchEntries).mockResolvedValue({
      results: [{ path: "config/app.yaml", entry: entry({ name: "app.yaml" }) }],
      truncated: false,
    });
    await store.runSearch();
    store.clearSearch();
    expect(store.searchActive).toBe(false);
    expect(store.searchResults).toEqual([]);
    expect(store.searchQuery).toBe("");
    expect(store.searchTruncated).toBe(false);
  });

  it("ignores a blank query", async () => {
    const store = await seedStore();
    store.setRecursive(true);
    store.setQuery("   ");
    await store.runSearch();
    expect(searchEntries).not.toHaveBeenCalled();
    expect(store.searchActive).toBe(false);
  });

  it("turning subdirectories off ends the search and restores the filter", async () => {
    const store = await seedStore();
    store.setRecursive(true);
    store.setQuery("conf");
    vi.mocked(searchEntries).mockResolvedValue({ results: [], truncated: false });
    await store.runSearch();
    expect(store.searchActive).toBe(true);
    store.setRecursive(false);
    expect(store.searchActive).toBe(false);
    expect(store.query).toBe("");
    expect(store.recursive).toBe(false);
  });

  it("opens a file hit's containing folder and clears the search", async () => {
    const store = await seedStore();
    store.setRecursive(true);
    store.setQuery("conf");
    vi.mocked(searchEntries).mockResolvedValue({ results: [], truncated: false });
    await store.runSearch();
    vi.mocked(listEntries).mockResolvedValue(listing);

    await store.openSearchHit({
      path: "config/app.yaml",
      entry: entry({ name: "app.yaml" }),
    });

    expect(store.searchActive).toBe(false);
    expect(store.path).toBe("config");
  });

  it("opens a directory hit itself and clears the search", async () => {
    const store = await seedStore();
    store.setRecursive(true);
    store.setQuery("sub");
    vi.mocked(searchEntries).mockResolvedValue({ results: [], truncated: false });
    await store.runSearch();
    vi.mocked(listEntries).mockResolvedValue(listing);

    await store.openSearchHit({
      path: "config/sub",
      entry: entry({ name: "sub", kind: "directory", mode: 0o040755 }),
    });

    expect(store.searchActive).toBe(false);
    expect(store.path).toBe("config/sub");
  });

  it("clears an active search on navigation", async () => {
    const store = await seedStore();
    store.setRecursive(true);
    store.setQuery("conf");
    vi.mocked(searchEntries).mockResolvedValue({ results: [], truncated: false });
    await store.runSearch();
    expect(store.searchActive).toBe(true);

    vi.mocked(listEntries).mockResolvedValue(listing);
    await store.goToPath("config");

    expect(store.searchActive).toBe(false);
    expect(store.searchQuery).toBe("");
  });

  it("keeps the plain listing on screen when a search fails", async () => {
    const store = await seedStore();
    store.setRecursive(true);
    store.setQuery("conf");
    vi.mocked(searchEntries).mockRejectedValue(
      Object.assign(new Error("boom"), { status: 502 }),
    );
    await store.runSearch();
    expect(store.searchActive).toBe(false);
    expect(store.searchResults).toEqual([]);
    expect(store.error).not.toBeNull();
  });

  it("a refresh during a search clears it instead of leaving 'Searching…' stuck", async () => {
    const store = await seedStore();
    store.setRecursive(true);
    store.setQuery("conf");
    const pending = deferred<FilesSearchListing>();
    vi.mocked(searchEntries).mockReturnValue(pending.promise);

    const search = store.runSearch();
    expect(store.searchActive).toBe(true);
    expect(store.searchLoading).toBe(true);

    vi.mocked(listEntries).mockResolvedValue(listing);
    await store.refresh();
    expect(store.searchActive).toBe(false);
    expect(store.searchLoading).toBe(false);

    // A late search response that lost to the refresh must not resurrect it.
    pending.resolve({ results: [], truncated: false });
    await search;
    expect(store.searchActive).toBe(false);
    expect(store.searchLoading).toBe(false);
  });

  it("a search during a listing load does not leave the listing spinner stuck", async () => {
    const store = await seedStore();
    const pendingListing = deferred<FilesListing>();
    vi.mocked(listEntries).mockReturnValue(pendingListing.promise);

    const load = store.refresh();
    expect(store.loading).toBe(true);

    store.setRecursive(true);
    store.setQuery("conf");
    vi.mocked(searchEntries).mockResolvedValue({ results: [], truncated: false });
    await store.runSearch();
    expect(store.searchActive).toBe(true);

    pendingListing.resolve(listing);
    await load;
    // The search did not cancel the listing's own generation, and the listing
    // did not cancel the search.
    expect(store.loading).toBe(false);
    expect(store.searchActive).toBe(true);
  });

  it("passes an abort signal and aborts it when the search is cleared", async () => {
    const store = await seedStore();
    store.setRecursive(true);
    store.setQuery("conf");
    const pending = deferred<FilesSearchListing>();
    vi.mocked(searchEntries).mockImplementation((_server, _team, _options) => {
      return pending.promise;
    });

    const search = store.runSearch();
    const signal = vi.mocked(searchEntries).mock.calls[0][2]?.signal;
    expect(signal).toBeInstanceOf(globalThis.AbortSignal);
    expect(signal?.aborted).toBe(false);

    store.clearSearch();
    expect(signal?.aborted).toBe(true);

    pending.resolve({ results: [], truncated: false });
    await search;
    expect(store.searchActive).toBe(false);
  });
});
