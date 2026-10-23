import { createPinia, setActivePinia } from "pinia";
import { beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("@/shared/api/http", () => ({
  http: { get: vi.fn(), post: vi.fn(), put: vi.fn(), delete: vi.fn() },
  teamHeaders: (teamId: string) => (teamId ? { "X-Team-Id": teamId } : {}),
}));

import { http } from "@/shared/api/http";
import {
  deleteEntry,
  emptyTrash,
  listTrash,
  purgeTrash,
  restoreTrash,
} from "@/features/files/api/files";
import {
  emptyTrashEnvelopeSchema,
  trashEntrySchema,
  trashListingSchema,
} from "@/features/files/schemas/files";
import type { TrashEntry } from "@/features/files/schemas/files";
import { useFilesStore } from "@/features/files/stores/files";
import { useTrashStore } from "@/features/files/stores/trash";
import { useTeamsStore } from "@/features/teams";

const root = {
  name: "volumes",
  path: "/var/lib/gotham/volumes",
  writable: true,
  free_bytes: 21 * 1024 ** 3,
};

const otherRoot = {
  name: "compose",
  path: "/var/lib/gotham-agent/compose",
  writable: true,
  free_bytes: 1,
};

function deferred<T>() {
  let resolve!: (_value: T) => void;
  const promise = new Promise<T>((resolvePromise) => {
    resolve = resolvePromise;
  });
  return { promise, resolve };
}

const restoredEntry = {
  name: "app.yaml",
  kind: "file",
  size: 2458,
  mode: 0o100644,
  modified_at: "2026-10-13T10:00:00Z",
  etag: '"x"',
};

function binEntry(overrides: Partial<TrashEntry> & { id: string }): TrashEntry {
  return {
    name: "app.yaml",
    original_path: "app.yaml",
    deleted_at: "2026-10-13T10:00:00Z",
    size: 2458,
    kind: "file",
    ...overrides,
  };
}

function routeGet(
  trash?: { entries: TrashEntry[]; truncated: boolean },
  roots: (typeof root)[] = [root],
): void {
  vi.mocked(http.get).mockImplementation((async (url: string) => {
    if (url.endsWith("/files/roots")) {
      return { data: { roots } };
    }
    if (url.endsWith("/files/trash")) {
      return {
        data: trash ?? { entries: [binEntry({ id: "t1" })], truncated: true },
      };
    }
    if (url.endsWith("/files/list")) {
      return { data: { entries: [], truncated: false, cursor: "" } };
    }
    throw new Error(`unexpected GET ${url}`);
  }) as never);
}

beforeEach(() => {
  setActivePinia(createPinia());
  vi.clearAllMocks();
  const teams = useTeamsStore();
  teams.activeTeamId = "team-1";
  teams.teams = [{ id: "team-1", name: "A", is_personal: true, role: "owner" }];
});

describe("trash schemas", () => {
  it("accepts a bin listing and rejects an unknown kind", () => {
    expect(
      trashListingSchema.safeParse({
        entries: [binEntry({ id: "t1" })],
        truncated: false,
      }).success,
    ).toBe(true);
    expect(
      trashEntrySchema.safeParse({
        id: "t1",
        name: "x",
        original_path: "x",
        deleted_at: "2026-10-13T10:00:00Z",
        size: 1,
        kind: "wormhole",
      }).success,
    ).toBe(false);
  });

  it("requires the empty envelope's removed count", () => {
    expect(emptyTrashEnvelopeSchema.safeParse({ removed: 3 }).success).toBe(true);
    expect(emptyTrashEnvelopeSchema.safeParse({}).success).toBe(false);
  });
});

describe("trash api", () => {
  it("lists the bin with the team header", async () => {
    vi.mocked(http.get).mockResolvedValue({
      data: { entries: [binEntry({ id: "t1" })], truncated: true },
    });

    const listing = await listTrash("server-1", "team-1", "volumes");

    expect(http.get).toHaveBeenCalledWith("/servers/server-1/files/trash", {
      params: { root: "volumes" },
      headers: { "X-Team-Id": "team-1" },
    });
    expect(listing.entries).toHaveLength(1);
    expect(listing.truncated).toBe(true);
  });

  it("restores to the recorded path, and to an explicit destination with overwrite", async () => {
    vi.mocked(http.post).mockResolvedValue({ data: { entry: restoredEntry } });

    const restored = await restoreTrash("server-1", "team-1", {
      root: "volumes",
      id: "t1",
    });
    expect(http.post).toHaveBeenCalledWith(
      "/servers/server-1/files/trash/restore",
      { root: "volumes", id: "t1", destination: "", overwrite: false },
      { headers: { "X-Team-Id": "team-1" } },
    );
    expect(restored.name).toBe("app.yaml");

    await restoreTrash("server-1", "team-1", {
      root: "volumes",
      id: "t1",
      destination: "moved/app.yaml",
      overwrite: true,
    });
    expect(http.post).toHaveBeenLastCalledWith(
      "/servers/server-1/files/trash/restore",
      { root: "volumes", id: "t1", destination: "moved/app.yaml", overwrite: true },
      { headers: { "X-Team-Id": "team-1" } },
    );
  });

  it("purges one entry and empties the bin", async () => {
    vi.mocked(http.post).mockResolvedValue({ data: {} });

    await purgeTrash("server-1", "team-1", "volumes", "t1");
    expect(http.post).toHaveBeenCalledWith(
      "/servers/server-1/files/trash/purge",
      { root: "volumes", id: "t1" },
      { headers: { "X-Team-Id": "team-1" } },
    );

    vi.mocked(http.post).mockResolvedValue({ data: { removed: 2 } });
    await expect(emptyTrash("server-1", "team-1", "volumes")).resolves.toBe(2);
    expect(http.post).toHaveBeenLastCalledWith(
      "/servers/server-1/files/trash/empty",
      { root: "volumes" },
      { headers: { "X-Team-Id": "team-1" } },
    );
  });

  it("deletes through the bin (the normal delete sends no permanent flag)", async () => {
    vi.mocked(http.delete).mockResolvedValue({ data: {} });

    await deleteEntry("server-1", "team-1", "volumes", "config", true);
    expect(http.delete).toHaveBeenCalledWith("/servers/server-1/files/entry", {
      params: { root: "volumes", path: "config", recursive: "true" },
      headers: { "X-Team-Id": "team-1" },
    });
  });
});

describe("trash store", () => {
  it("loads the bound root's bin and surfaces truncation", async () => {
    routeGet();
    const files = useFilesStore();
    await files.init("server-1");

    const trash = useTrashStore();
    await trash.load();

    expect(trash.entries.map((entry) => entry.id)).toEqual(["t1"]);
    expect(trash.truncated).toBe(true);
    expect(trash.loaded).toBe(true);
    expect(trash.error).toBeNull();
    expect(http.get).toHaveBeenCalledWith("/servers/server-1/files/trash", {
      params: { root: "volumes" },
      headers: { "X-Team-Id": "team-1" },
    });
  });

  it("retains a load failure as a localized error", async () => {
    routeGet();
    const files = useFilesStore();
    await files.init("server-1");
    vi.mocked(http.get).mockRejectedValue({ message: "boom", status: 500 });

    const trash = useTrashStore();
    await trash.load();

    expect(trash.error).toBeTruthy();
    expect(trash.entries).toEqual([]);
  });

  it("restores an entry, reloads the bin and refreshes the browser", async () => {
    routeGet();
    const files = useFilesStore();
    await files.init("server-1");
    const trash = useTrashStore();
    await trash.load();
    vi.mocked(http.post).mockResolvedValue({ data: { entry: restoredEntry } });
    routeGet({ entries: [], truncated: false });

    await trash.restore(binEntry({ id: "t1" }));

    expect(http.post).toHaveBeenCalledWith(
      "/servers/server-1/files/trash/restore",
      { root: "volumes", id: "t1", destination: "", overwrite: false },
      { headers: { "X-Team-Id": "team-1" } },
    );
    expect(trash.entries).toEqual([]);
    expect(trash.acting).toBe(false);
    expect(http.get).toHaveBeenCalledWith(
      "/servers/server-1/files/list",
      expect.anything(),
    );
  });

  it("rethrows a 409 so the caller can explain an occupied destination", async () => {
    routeGet();
    const files = useFilesStore();
    await files.init("server-1");
    const trash = useTrashStore();
    await trash.load();
    vi.mocked(http.post).mockRejectedValue({ message: "conflict", status: 409 });

    await expect(trash.restore(binEntry({ id: "t1" }))).rejects.toMatchObject({
      status: 409,
    });
    expect(trash.acting).toBe(false);
  });

  it("purges one entry and reloads the bin", async () => {
    routeGet();
    const files = useFilesStore();
    await files.init("server-1");
    const trash = useTrashStore();
    await trash.load();
    vi.mocked(http.post).mockResolvedValue({ data: {} });
    routeGet({ entries: [], truncated: false });

    await trash.purge(binEntry({ id: "t1" }));

    expect(http.post).toHaveBeenCalledWith(
      "/servers/server-1/files/trash/purge",
      { root: "volumes", id: "t1" },
      { headers: { "X-Team-Id": "team-1" } },
    );
    expect(trash.entries).toEqual([]);
  });

  it("empties the bin and reports how many went", async () => {
    routeGet();
    const files = useFilesStore();
    await files.init("server-1");
    const trash = useTrashStore();
    await trash.load();
    vi.mocked(http.post).mockResolvedValue({ data: { removed: 4 } });
    routeGet({ entries: [], truncated: false });

    const removed = await trash.empty();

    expect(removed).toBe(4);
    expect(http.post).toHaveBeenCalledWith(
      "/servers/server-1/files/trash/empty",
      { root: "volumes" },
      { headers: { "X-Team-Id": "team-1" } },
    );
  });

  it("clears its state on reset", async () => {
    routeGet();
    const files = useFilesStore();
    await files.init("server-1");
    const trash = useTrashStore();
    await trash.load();

    trash.reset();

    expect(trash.entries).toEqual([]);
    expect(trash.truncated).toBe(false);
    expect(trash.loaded).toBe(false);
    expect(trash.scopeMatches).toBe(false);
  });

  it("never shows another root's bin and blocks its actions", async () => {
    routeGet(undefined, [root, otherRoot]);
    const files = useFilesStore();
    await files.init("server-1");
    const trash = useTrashStore();
    await trash.load();
    expect(trash.entries.map((entry) => entry.id)).toEqual(["t1"]);
    expect(trash.scopeMatches).toBe(true);

    await files.setRoot("compose");

    expect(trash.scopeMatches).toBe(false);
    await trash.restore(binEntry({ id: "t1" }));
    await trash.purge(binEntry({ id: "t1" }));
    await expect(trash.empty()).resolves.toBe(0);
    expect(http.post).not.toHaveBeenCalled();
  });

  it("clears the previous bin the moment a new scope loads", async () => {
    routeGet(undefined, [root, otherRoot]);
    const files = useFilesStore();
    await files.init("server-1");
    const trash = useTrashStore();
    await trash.load();
    expect(trash.entries).toHaveLength(1);

    await files.setRoot("compose");
    const gate = deferred<{ data: unknown }>();
    vi.mocked(http.get).mockImplementation((async (url: string) => {
      if (url.endsWith("/files/trash")) {
        return gate.promise;
      }
      return { data: {} };
    }) as never);

    const pending = trash.load();
    expect(trash.entries).toEqual([]);
    expect(trash.scopeMatches).toBe(false);

    gate.resolve({
      data: { entries: [binEntry({ id: "t2", name: "notes.txt" })], truncated: false },
    });
    await pending;
    expect(trash.entries.map((entry) => entry.id)).toEqual(["t2"]);
    expect(trash.scopeMatches).toBe(true);
  });

  it("clears the bin and shows the error when a load fails", async () => {
    routeGet();
    const files = useFilesStore();
    await files.init("server-1");
    const trash = useTrashStore();
    await trash.load();
    expect(trash.entries).toHaveLength(1);

    vi.mocked(http.get).mockRejectedValue({ message: "boom", status: 500 });
    await trash.load();

    expect(trash.entries).toEqual([]);
    expect(trash.error).toBeTruthy();
    expect(trash.scopeMatches).toBe(false);
  });
});
