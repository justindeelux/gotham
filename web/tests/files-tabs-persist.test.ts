// File manager tab persistence (JUS-209): versioned, zod-validated snapshots
// round-trip through storage; corrupt, wrong-version and oversized payloads
// are ignored; keys isolate user+server; missing paths fall back silently;
// storage failures never throw; restore keeps existing tab behavior.
import { createPinia, setActivePinia } from "pinia";
import { beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("@/features/files/api/files", async (importOriginal) => {
  const actual =
    await importOriginal<typeof import("@/features/files/api/files")>();
  return { ...actual, listRoots: vi.fn(), listEntries: vi.fn() };
});

import { listEntries, listRoots } from "@/features/files/api/files";
import type { FilesEntry, FilesRoot } from "@/features/files/schemas/files";
import {
  FILES_TABS_MAX_TABS,
  filesTabsSnapshotSchema,
} from "@/features/files/schemas/tabsPersist";
import type { FilesTabsSnapshot } from "@/features/files/schemas/tabsPersist";
import { useFilesStore } from "@/features/files/stores/files";
import {
  filesTabsStorageKey,
  readFilesTabsSnapshot,
  writeFilesTabsSnapshot,
} from "@/features/files/utils/tabsPersist";
import type { FilesTabsStorage } from "@/features/files/utils/tabsPersist";
import { useTeamsStore } from "@/features/teams";

/** memoryStorage builds an in-memory Storage double. */
function memoryStorage(): FilesTabsStorage & { dump: Map<string, string> } {
  const dump = new Map<string, string>();
  return {
    dump,
    getItem: (key: string) => dump.get(key) ?? null,
    setItem: (key: string, value: string) => void dump.set(key, value),
    removeItem: (key: string) => void dump.delete(key),
  };
}

/** throwingStorage fails every access (private mode / blocked storage). */
function throwingStorage(): FilesTabsStorage {
  return {
    getItem: () => {
      throw new Error("denied");
    },
    setItem: () => {
      throw new Error("denied");
    },
    removeItem: () => {
      throw new Error("denied");
    },
  };
}

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

/** snapshot builds a valid snapshot with sensible defaults. */
function snapshot(overrides: Partial<FilesTabsSnapshot> = {}): FilesTabsSnapshot {
  return {
    version: 1,
    tabs: [
      { id: "tab-a", root: "volumes", path: "data" },
      { id: "tab-b", root: "compose", path: "" },
    ],
    activeTabId: "tab-b",
    viewMode: "grid",
    ...overrides,
  };
}

beforeEach(() => {
  setActivePinia(createPinia());
  vi.mocked(listRoots).mockReset();
  vi.mocked(listEntries).mockReset();
  vi.mocked(listRoots).mockResolvedValue([volumes, compose]);
  vi.mocked(listEntries).mockImplementation(
    async (_serverId, _teamId, options) => ({
      entries: [entry({ name: `${options.root}:${options.path ?? ""}` })],
      truncated: false,
      cursor: "",
    }),
  );

  const teams = useTeamsStore();
  teams.activeTeamId = "team-1";
  teams.teams = [{ id: "team-1", name: "A", is_personal: true, role: "owner" }];
});

describe("files tabs storage key", () => {
  it("scopes the key by server and user", () => {
    const key = filesTabsStorageKey("server-1", "user-1");
    expect(key).toContain("server-1");
    expect(key).toContain("user-1");
    expect(filesTabsStorageKey("server-2", "user-1")).not.toBe(key);
    expect(filesTabsStorageKey("server-1", "user-2")).not.toBe(key);
  });

  it("refuses an empty server or user instead of pooling state", () => {
    expect(filesTabsStorageKey("", "user-1")).toBe("");
    expect(filesTabsStorageKey("server-1", "")).toBe("");
    expect(filesTabsStorageKey("", "")).toBe("");
  });
});

describe("files tabs snapshot round trip", () => {
  it("saves and restores tabs, the active tab and the view mode", () => {
    const store = memoryStorage();
    const key = filesTabsStorageKey("server-1", "user-1");
    expect(writeFilesTabsSnapshot(store, key, snapshot())).toBe(true);

    const loaded = readFilesTabsSnapshot(store, key);
    expect(loaded).toMatchObject({
      version: 1,
      activeTabId: "tab-b",
      viewMode: "grid",
    });
    expect(loaded?.tabs).toHaveLength(2);
  });

  it("returns null for a missing or empty key without throwing", () => {
    const store = memoryStorage();
    expect(readFilesTabsSnapshot(store, "missing")).toBeNull();
    expect(readFilesTabsSnapshot(store, "")).toBeNull();
    expect(writeFilesTabsSnapshot(store, "", snapshot())).toBe(false);
  });

  it("ignores corrupt JSON and drops the bad entry", () => {
    const store = memoryStorage();
    const key = filesTabsStorageKey("server-1", "user-1");
    store.dump.set(key, "{not json");
    expect(readFilesTabsSnapshot(store, key)).toBeNull();
    expect(store.dump.has(key)).toBe(false);
  });

  it("ignores a wrong version and drops the bad entry", () => {
    const store = memoryStorage();
    const key = filesTabsStorageKey("server-1", "user-1");
    store.dump.set(
      key,
      JSON.stringify({ ...snapshot(), version: 2 }),
    );
    expect(readFilesTabsSnapshot(store, key)).toBeNull();
    expect(store.dump.has(key)).toBe(false);
  });

  it("ignores an empty tab list", () => {
    const store = memoryStorage();
    const key = filesTabsStorageKey("server-1", "user-1");
    store.dump.set(key, JSON.stringify(snapshot({ tabs: [] })));
    expect(readFilesTabsSnapshot(store, key)).toBeNull();
  });

  it("ignores an oversized payload", () => {
    const store = memoryStorage();
    const key = filesTabsStorageKey("server-1", "user-1");
    store.dump.set(key, `{"version":1,"tabs":[],"pad":"${"x".repeat(40 * 1024)}"}`);
    expect(readFilesTabsSnapshot(store, key)).toBeNull();
  });

  it("caps the stored tabs", () => {
    const store = memoryStorage();
    const key = filesTabsStorageKey("server-1", "user-1");
    const many = Array.from({ length: FILES_TABS_MAX_TABS + 5 }, (_, index) => ({
      id: `tab-${index}`,
      root: "volumes",
      path: `dir-${index}`,
    }));
    expect(
      writeFilesTabsSnapshot(store, key, snapshot({ tabs: many })),
    ).toBe(true);
    expect(readFilesTabsSnapshot(store, key)?.tabs).toHaveLength(
      FILES_TABS_MAX_TABS,
    );
  });

  it("survives storage failures without throwing", () => {
    const store = throwingStorage();
    const key = filesTabsStorageKey("server-1", "user-1");
    expect(readFilesTabsSnapshot(store, key)).toBeNull();
    expect(writeFilesTabsSnapshot(store, key, snapshot())).toBe(false);
  });
});

describe("files store tab restore", () => {
  it("reopens tabs, paths, the active tab and the view mode", async () => {
    const files = useFilesStore();
    await files.init("server-1");
    const restored = await files.restorePersistedTabs(snapshot());

    expect(restored).toBe(true);
    expect(files.tabs.map((tab) => [tab.rootName, tab.path])).toEqual([
      ["volumes", "data"],
      ["compose", ""],
    ]);
    expect(files.activeTabId).toBe("tab-b");
    expect(files.tabs.every((tab) => tab.viewMode === "grid")).toBe(true);
    expect(files.entries[0]?.name).toBe("compose:");
  });

  it("maps an unknown root to the default root", async () => {
    const files = useFilesStore();
    await files.init("server-1");
    const restored = await files.restorePersistedTabs(
      snapshot({
        tabs: [{ id: "tab-x", root: "gone", path: "deep" }],
        activeTabId: "tab-x",
      }),
    );

    expect(restored).toBe(true);
    expect(files.tabs).toHaveLength(1);
    expect(files.activeTab.rootName).toBe("volumes");
    expect(files.activeTab.path).toBe("");
  });

  it("falls back to the first tab for an unknown active id", async () => {
    const files = useFilesStore();
    await files.init("server-1");
    await files.restorePersistedTabs(snapshot({ activeTabId: "missing" }));
    expect(files.activeTabId).toBe("tab-a");
  });

  it("falls back to the nearest existing parent for a missing path", async () => {
    vi.mocked(listEntries).mockImplementation(
      async (_serverId, _teamId, options) => {
        if (options.path === "data/nested/gone") {
          throw { message: "not found", status: 404 };
        }
        if (options.path === "data/nested") {
          throw { message: "not found", status: 404 };
        }
        return {
          entries: [entry({ name: `hit:${options.path ?? ""}` })],
          truncated: false,
          cursor: "",
        };
      },
    );
    const files = useFilesStore();
    await files.init("server-1");
    await files.restorePersistedTabs(
      snapshot({
        tabs: [{ id: "tab-a", root: "volumes", path: "data/nested/gone" }],
        activeTabId: "tab-a",
        viewMode: "list",
      }),
    );

    expect(files.activeTab.path).toBe("data");
    expect(files.error).toBeNull();
    expect(files.entries[0]?.name).toBe("hit:data");
  });

  it("validates a background tab silently on its first switch", async () => {
    vi.mocked(listEntries).mockImplementation(
      async (_serverId, _teamId, options) => {
        if (options.path === "renamed-away") {
          throw { message: "not found", status: 404 };
        }
        return {
          entries: [entry({ name: `hit:${options.path ?? ""}` })],
          truncated: false,
          cursor: "",
        };
      },
    );
    const files = useFilesStore();
    await files.init("server-1");
    await files.restorePersistedTabs(
      snapshot({
        tabs: [
          { id: "tab-a", root: "volumes", path: "" },
          { id: "tab-b", root: "volumes", path: "renamed-away" },
        ],
        activeTabId: "tab-a",
        viewMode: "list",
      }),
    );
    expect(files.activeTabId).toBe("tab-a");

    await files.switchTab("tab-b");
    expect(files.activeTab.path).toBe("");
    expect(files.error).toBeNull();
  });

  it("keeps the current tabs when there is nothing to restore", async () => {
    const files = useFilesStore();
    await files.init("server-1");
    const before = files.tabs.map((tab) => tab.id);
    const restored = await files.restorePersistedTabs(
      snapshot({ tabs: [], activeTabId: "tab-a" }),
    );
    // An empty list never validates (min(1)), so the store refuses it as well
    // when it arrives without validation: no rebuild, defaults kept.
    expect(restored).toBe(false);
    expect(files.tabs.map((tab) => tab.id)).toEqual(before);
  });

  it("collects a snapshot of the live tabs for persistence", async () => {
    const files = useFilesStore();
    await files.init("server-1");
    await files.openDirectory("data");
    await files.openTab({ root: "compose" });
    files.setViewMode("grid");

    const collected = files.collectTabsSnapshot();
    expect(collected).toMatchObject({
      version: 1,
      activeTabId: files.activeTabId,
      viewMode: "grid",
    });
    expect(collected?.tabs).toHaveLength(2);
    expect(collected?.tabs[0]).toMatchObject({ root: "volumes", path: "data" });
  });

  it("collects nothing while the browser has no roots", () => {
    const files = useFilesStore();
    expect(files.collectTabsSnapshot()).toBeNull();
  });

  it("schema rejects non-conforming payloads", () => {
    expect(
      filesTabsSnapshotSchema.safeParse({ version: 1 }).success,
    ).toBe(false);
    expect(
      filesTabsSnapshotSchema.safeParse(
        snapshot({ viewMode: "columns" }),
      ).success,
    ).toBe(false);
  });
});
