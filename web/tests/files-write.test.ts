// Files write surface (P18 step 7): create-over-existing, batch delete with a
// partial failure, and the clipboard 409 replace/skip flow. The API module is
// mocked so the store runs without axios or the DOM.
import { createPinia, setActivePinia } from "pinia";
import { beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("@/features/files/api/files", async (importOriginal) => {
  const actual =
    await importOriginal<typeof import("@/features/files/api/files")>();
  return {
    ...actual,
    listRoots: vi.fn(),
    listEntries: vi.fn(),
    createFolder: vi.fn(),
    createFile: vi.fn(),
    renameEntry: vi.fn(),
    moveEntry: vi.fn(),
    copyEntry: vi.fn(),
    deleteEntry: vi.fn(),
  };
});

import {
  copyEntry,
  createFile,
  createFolder,
  deleteEntry,
  listEntries,
  listRoots,
  moveEntry,
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

const conflict = { message: "conflict", status: 409 };

beforeEach(() => {
  setActivePinia(createPinia());
  for (const fn of [listRoots, listEntries, createFolder, createFile, copyEntry, moveEntry, deleteEntry]) {
    vi.mocked(fn).mockReset();
  }
  const teams = useTeamsStore();
  teams.activeTeamId = "team-1";
  teams.teams = [{ id: "team-1", name: "A", is_personal: true, role: "owner" }];
  vi.mocked(listRoots).mockResolvedValue([root]);
  vi.mocked(listEntries).mockResolvedValue({
    entries: [
      entry({ name: "app.yaml" }),
      entry({ name: "config", kind: "directory", size: 0, mode: 0o040755 }),
    ],
    truncated: false,
    cursor: "",
  });
});

describe("files store create", () => {
  it("fails a create over an existing name and keeps the listing", async () => {
    vi.mocked(createFolder).mockRejectedValue(conflict);

    const store = useFilesStore();
    await store.init("server-1");
    const before = store.entries.map((item) => item.name);

    await expect(store.createEntry("config", "directory")).rejects.toMatchObject({
      status: 409,
    });

    expect(vi.mocked(createFolder)).toHaveBeenCalledWith("server-1", "team-1", {
      root: "volumes",
      path: "config",
    });
    expect(store.entries.map((item) => item.name)).toEqual(before);
    expect(store.acting).toBe(false);
  });

  it("trims the submitted name and creates exclusively", async () => {
    vi.mocked(createFolder).mockResolvedValue(
      entry({ name: "uploads", kind: "directory", mode: 0o040755 }),
    );

    const store = useFilesStore();
    await store.init("server-1");
    await store.createEntry("  uploads  ", "directory");

    expect(vi.mocked(createFolder)).toHaveBeenCalledWith("server-1", "team-1", {
      root: "volumes",
      path: "uploads",
    });
  });
});

describe("files store delete", () => {
  it("reports a per-name partial failure instead of aborting", async () => {
    vi.mocked(deleteEntry)
      .mockResolvedValueOnce(undefined)
      .mockRejectedValueOnce(conflict);

    const store = useFilesStore();
    await store.init("server-1");
    const summary = await store.remove([
      entry({ name: "app.yaml" }),
      entry({ name: "config", kind: "directory", mode: 0o040755 }),
    ]);

    expect(summary.done).toEqual(["app.yaml"]);
    expect(summary.failed).toHaveLength(1);
    expect(summary.failed[0]?.name).toBe("config");
    expect(summary.failed[0]?.error).toBeTruthy();
    // A folder is deleted recursively; a file is not.
    expect(vi.mocked(deleteEntry)).toHaveBeenNthCalledWith(
      2,
      "server-1",
      "team-1",
      "volumes",
      "config",
      true,
    );
  });
});

describe("files store clipboard", () => {
  it("opens the replace/skip prompt on a 409 and retries with overwrite", async () => {
    vi.mocked(copyEntry).mockRejectedValueOnce(conflict).mockResolvedValueOnce(
      entry({ name: "config", kind: "directory", mode: 0o040755 }),
    );

    const store = useFilesStore();
    await store.init("server-1");
    store.toggleSelection("config");
    store.copySelection();
    expect(store.hasClipboard).toBe(true);
    expect(store.clipboardMode).toBe("copy");

    const summary = await store.paste();
    expect(summary.conflicts).toEqual(["config"]);
    expect(store.conflictPrompt?.names).toEqual(["config"]);

    const resolved = await store.resolveConflictPrompt(true);
    expect(resolved.done).toEqual(["config"]);
    expect(store.conflictPrompt).toBeNull();
    expect(vi.mocked(copyEntry)).toHaveBeenLastCalledWith("server-1", "team-1", {
      root: "volumes",
      source: "config",
      destination: "config",
      overwrite: true,
    });
  });

  it("skips conflicting names and leaves the destination untouched", async () => {
    vi.mocked(copyEntry).mockRejectedValue(conflict);

    const store = useFilesStore();
    await store.init("server-1");
    store.toggleSelection("config");
    store.copySelection();

    await store.paste();
    const resolved = await store.resolveConflictPrompt(false);

    expect(resolved.done).toEqual([]);
    expect(store.conflictPrompt).toBeNull();
    // Only the initial, non-overwriting attempt ran.
    expect(vi.mocked(copyEntry)).toHaveBeenCalledTimes(1);
  });

  it("consumes a cut clipboard once the move lands", async () => {
    vi.mocked(moveEntry).mockResolvedValue(entry({ name: "app.yaml" }));

    const store = useFilesStore();
    await store.init("server-1");
    store.toggleSelection("app.yaml");
    store.cutSelection();
    expect(store.clipboardMode).toBe("cut");

    await store.paste();
    expect(store.hasClipboard).toBe(false);
    expect(vi.mocked(moveEntry)).toHaveBeenCalledWith("server-1", "team-1", {
      root: "volumes",
      source: "app.yaml",
      destination: "app.yaml",
      overwrite: false,
    });
  });
});
